package main

// 对应 services/media/media-summary.ts：视频/音频内容总结。
//
// 以口播文字稿为素材，生成结构化 Markdown 总结（总起段 + 加粗小节标题 + 嵌套要点），写入条目正文的
// 「视频/音频总结」小节。短文字稿单发；长文字稿分块提笔记再综合（map-reduce）。
// 输出做确定性清洗（去代码围栏 / 分隔线，# 级标题降级为加粗行），保证写回后不破坏小节的边界锚点。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	summarySingleShotMaxChars = 10_000
	summaryChunkMaxChars      = 10_000
	summaryMaxChunks          = 12
	summaryContextMaxChars    = 500
	summaryMaxTokens          = 6144
	summaryTimeout            = 180 * time.Second
	summaryTemperature        = 0.3
	titleMaxChars             = 60
	titleMaxTokens            = 256
	titleSourceMaxChars       = 4000
)

// 篇幅按文字稿长度分档：短视频不注水，长视频不丢细节。
func summaryLengthGuidance(transcriptChars int) string {
	switch {
	case transcriptChars <= 2000:
		return "总结篇幅 150-300 字，精简即可；"
	case transcriptChars <= 5000:
		return "总结篇幅 400-800 字；"
	case transcriptChars <= 10000:
		return "总结篇幅 800-1500 字，确保各小节的具体方法、步骤与判断标准都保留；"
	}
	return "总结篇幅 1500-2500 字，这是一条长内容，确保每个小节的论点、论据、具体做法与关键数据都充分展开，不要为了压缩而丢掉细节；"
}

const summarySystemBase = "你是视频内容总结助手。用户会提供视频（或音频）的标题、简介与完整口播文字稿，" +
	"请为这条内容拟一个准确的标题，并生成一份结构化的内容总结，让用户不看视频也能掌握核心内容。要求：\n" +
	"1. 第一行以「标题：」开头单独成行，输出你拟定的标题：15-30 字，准确概括核心内容，" +
	"书面语、不用夸张修辞与营销话术，不加书名号或引号；空一行后开始总结正文；\n" +
	"2. 总结开头用 2-3 个短句概括主旨（每句一句一事）：先点主题，再说核心问题或现象，" +
	"最后给出主要结论；不要用顿号或分号把多件事塞进同一句；\n" +
	"3. 正文按内容脉络分 3-6 个小节（信息极少时可 2 个）：小节标题用「**加粗**」独立成行" +
	"（可用 一、二、… 编号），其下用列表展开要点；\n" +
	"4. 列表结构：顶层列表项对应小节标题承诺的分类（如标题写「五类效果」则恰好 5 条顶层项），" +
	"每条用一到两句说完核心含义；同一类有多个方面或实例时用子列表展开，而不是全部拍平成顶层条目；" +
	"全文最多两层列表；\n" +
	"5. 加粗只用于：列表项开头的简短标签（如 **核心痛点**）、数字、专有名词与关键结论词；" +
	"同一行内除标签外不要再加粗；禁止把整句或半句加粗；\n" +
	"6. 忠实于文字稿：保留具体的方法、步骤、参数与判断标准，不编造、不泛泛而谈；口语转为书面语；" +
	"如果作者明确声明某个话题「下一期再讲」「这里不展开」，在总结中只需一句话提及即可，" +
	"不要把未展开的话题升格成与已充分讲解的话题同等篇幅的小节，" +
	"小节标题里的分类数（如「五种用法」）应与原文实际充分讲解的分类数一致；\n"

const summarySystemTail = "8. 输出简体中文 Markdown；不要使用 #/## 级标题或 --- 分隔线；" +
	"不要输出「视频总结」这类总标题，也不要任何前言或结尾说明；\n" +
	"9. 内容中涉及多项对比（如方案优劣、参数差异、价格对照）时可用 Markdown 表格呈现，" +
	"列数控制在 3-5 列、行数不超过 8 行，能用列表讲清的不要为凑格式硬上表格；\n" +
	"10. 技术类内容中出现的命令、代码片段、配置示例或 API 调用可用围栏代码块展示，" +
	"标注语言名（如 ```python），仅保留原文中真实出现的片段，不要自行编造代码。"

func buildSummarySystemPrompt(transcriptChars int) string {
	return summarySystemBase +
		fmt.Sprintf("7. 篇幅与信息量匹配：%s", summaryLengthGuidance(transcriptChars)) +
		"宁缺毋滥，不要为凑格式重复或注水；\n" +
		summarySystemTail
}

const summaryMapSystemPrompt = "你是视频内容总结助手。下面是视频口播文字稿的一个片段，请按内容脉络提取这一段的结构化笔记：" +
	"小节标题用「**加粗**」独立成行，要点用列表（最多两层）；" +
	"顶层条目对应原文的分类或分点，同一类的多个方面用子列表展开。" +
	"加粗只标列表标签、数字与专名；保留具体的方法、步骤、数据与结论。" +
	"涉及多项对比时可用表格，技术类内容中的命令或代码片段可用围栏代码块。" +
	"输出简体中文 Markdown，不要使用 #/## 级标题或 --- 分隔线；" +
	"只输出笔记本身，不要前言或结尾说明。"

const titleSystemPrompt = "你是内容标题助手。用户会提供一条图文内容的文案与图中文字，请为它拟一个准确的标题。要求：\n" +
	"1. 15-30 字，准确概括核心内容；\n" +
	"2. 书面语，不用夸张修辞与营销话术；不加书名号或引号；\n" +
	"3. 只输出标题本身，不要任何解释、前缀或后缀。"

// JS 正则 \s 的字符集（RE2 的 \s 只含 ASCII 空白）。
const jsWS = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var (
	reCodeFenceOpen  = regexp.MustCompile("^```[a-zA-Z]*[" + jsWS + "]*$")
	reRedundantTitle = regexp.MustCompile(`^\*{0,2}(?:视频|音频|内容)?总结\*{0,2}[:：]?$`)
	reTitleLine      = regexp.MustCompile(`^\*{0,2}标题[:：][` + jsWS + `]*(.+?)\*{0,2}$`)
	reHRuleLine      = regexp.MustCompile(`^[` + jsWS + `]*-{3,}[` + jsWS + `]*$`)
	reHeadingLine    = regexp.MustCompile(`^[` + jsWS + `]*#{1,6}[` + jsWS + `]+(.+)$`)
	reWSRun          = regexp.MustCompile(`[` + jsWS + `]+`)
	reTitleEdgeLead  = regexp.MustCompile(`^[《"“'‘]+`)
	reTitleEdgeTrail = regexp.MustCompile(`[》"”'’]+$`)
	reNewlines3      = regexp.MustCompile(`\n{3,}`)
)

// stripWrappingCodeFence 去掉包裹整段输出的 ``` 围栏（部分模型无视指令输出代码块）。
func stripWrappingCodeFence(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) >= 2 && reCodeFenceOpen.MatchString(lines[0]) && jsTrim(lines[len(lines)-1]) == "```" {
		return jsTrim(strings.Join(lines[1:len(lines)-1], "\n"))
	}
	return text
}

// normalizeAiTitle 压缩空白、去首尾书名号/引号、限长；无效返回 ok=false。
func normalizeAiTitle(raw string) (string, bool) {
	t := jsTrim(reWSRun.ReplaceAllString(raw, " "))
	t = jsTrim(reTitleEdgeTrail.ReplaceAllString(reTitleEdgeLead.ReplaceAllString(t, ""), ""))
	if t == "" {
		return "", false
	}
	if utf16Len(t) > titleMaxChars {
		t = utf16Head(t, titleMaxChars)
	}
	return t, true
}

// splitTitleFromSummary 按输出协议拆出首行「标题：xxx」；未匹配则整段视为总结正文。
// 论坛讨论总结（forum-summary）用的是同一套输出协议。
func splitTitleFromSummary(raw string) (title string, hasTitle bool, body string) {
	lines := strings.Split(raw, "\n")
	m := reTitleLine.FindStringSubmatch(jsTrim(lines[0]))
	if m == nil {
		return "", false, raw
	}
	title, hasTitle = normalizeAiTitle(m[1])
	return title, hasTitle, strings.Join(lines[1:], "\n")
}

// sanitizeMediaSummary 确定性清洗：# 级标题降级为加粗行、删除 --- 分隔线、去冗余总标题。
func sanitizeMediaSummary(raw string) string {
	var lines []string
	for _, line := range strings.Split(stripWrappingCodeFence(jsTrim(raw)), "\n") {
		if reHRuleLine.MatchString(line) {
			continue
		}
		if m := reHeadingLine.FindStringSubmatch(line); m != nil {
			line = "**" + jsTrim(m[1]) + "**"
		}
		lines = append(lines, line)
	}
	for len(lines) > 0 && jsTrim(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) > 0 && reRedundantTitle.MatchString(jsTrim(lines[0])) {
		lines = lines[1:]
	}
	// 删行会留下相邻空行，折叠为单个空行
	return jsTrim(reNewlines3.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

type mediaSummaryInput struct {
	Title      string
	Context    string // 补充上下文（视频简介等），可选
	Transcript string
}

type mediaSummaryResult struct {
	Summary  string // 结构化总结正文（不含小节标题本身）
	Title    string
	HasTitle bool // 模型未按协议输出时为 false，调用方保留原标题
}

// resolveMediaSummaryConfig 总结用 chat 模型（mainText 优先，回退 fastText）；未配置返回 nil。
func (l *Library) resolveMediaSummaryConfig() *aiClientConfig {
	return l.resolveChatModel("内容总结", "mainText", "fastText")
}

func buildSummaryUserPrompt(in mediaSummaryInput, body string) string {
	title := jsTrim(in.Title)
	if title == "" {
		title = "无标题"
	}
	parts := []string{"标题：《" + title + "》"}
	if c := jsTrim(in.Context); c != "" {
		parts = append(parts, "简介："+utf16Head(c, summaryContextMaxChars))
	}
	parts = append(parts, "", body)
	return strings.Join(parts, "\n")
}

func buildMapUserPrompt(title, chunk string, index, total int) string {
	t := jsTrim(title)
	if t == "" {
		t = "无标题"
	}
	return fmt.Sprintf("《%s》文字稿片段 %d/%d：\n%s", t, index, total, chunk)
}

// summaryChat 一次总结调用：逐次记账（一篇长稿最多十几次调用，逐次记才对得上账单）。
func (l *Library) summaryChat(ctx context.Context, chat aiChatFunc, cfg aiClientConfig, messages []aiChatMessage, label string) (string, error) {
	res, err := chat(ctx, cfg, messages, aiChatOptions{Temperature: f64(summaryTemperature), MaxTokens: summaryMaxTokens, Timeout: summaryTimeout})
	if err != nil {
		l.recordMainAIUsage(ctx, mainAIUsage{Scenario: "summary", Model: cfg.Model, Failed: true})
		return "", err
	}
	u := mainAIUsage{Scenario: "summary", Model: cfg.Model}
	if res.Usage != nil {
		u.PromptTokens, u.CompletionTokens = res.Usage.PromptTokens, res.Usage.CompletionTokens
	}
	l.recordMainAIUsage(ctx, u)
	if res.FinishReason == "length" {
		fmt.Printf("[media] 内容总结%s输出被 max_tokens 截断\n", label)
	}
	content := jsTrim(res.Content)
	if content == "" {
		return "", fmt.Errorf("模型未返回内容总结（%s输出为空）", label)
	}
	return content, nil
}

func finalizeSummary(content string) (mediaSummaryResult, error) {
	title, has, body := splitTitleFromSummary(stripWrappingCodeFence(jsTrim(content)))
	summary := sanitizeMediaSummary(body)
	if summary == "" {
		return mediaSummaryResult{}, errors.New("模型未返回有效的内容总结")
	}
	return mediaSummaryResult{Summary: summary, Title: title, HasTitle: has}, nil
}

// generateMediaSummary 生成结构化内容总结与 AI 标题（输出协议：首行「标题：xxx」+ 总结正文）。
// chat 为空时用 l.chatCompletion；文字稿为空或模型输出为空时返回错误。
func (l *Library) generateMediaSummary(ctx context.Context, in mediaSummaryInput, cfg aiClientConfig, chat aiChatFunc) (mediaSummaryResult, error) {
	if chat == nil {
		chat = l.chatCompletion
	}
	transcript := jsTrim(in.Transcript)
	if transcript == "" {
		return mediaSummaryResult{}, errors.New("文字稿为空，无法生成内容总结")
	}
	n := utf16Len(transcript)
	if n <= summarySingleShotMaxChars {
		content, err := l.summaryChat(ctx, chat, cfg, []aiChatMessage{
			{Role: "system", Content: buildSummarySystemPrompt(n)},
			{Role: "user", Content: buildSummaryUserPrompt(in, "文字稿：\n"+transcript)},
		}, "")
		if err != nil {
			return mediaSummaryResult{}, err
		}
		return finalizeSummary(content)
	}

	// map：分块提取结构化笔记（保持时间顺序）
	chunks := splitTranscriptChunks(transcript, summaryChunkMaxChars)
	if len(chunks) > summaryMaxChunks {
		fmt.Printf("[media] 文字稿过长（%d 块），仅总结前 %d 块\n", len(chunks), summaryMaxChunks)
		chunks = chunks[:summaryMaxChunks]
	}
	notes := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return mediaSummaryResult{}, err
		}
		note, err := l.summaryChat(ctx, chat, cfg, []aiChatMessage{
			{Role: "system", Content: summaryMapSystemPrompt},
			{Role: "user", Content: buildMapUserPrompt(in.Title, chunk, i+1, len(chunks))},
		}, fmt.Sprintf("（片段 %d/%d）", i+1, len(chunks)))
		if err != nil {
			return mediaSummaryResult{}, err
		}
		notes = append(notes, note)
	}

	// reduce：综合各片段笔记输出完整总结
	if err := ctx.Err(); err != nil {
		return mediaSummaryResult{}, err
	}
	blocks := make([]string, len(notes))
	for i, note := range notes {
		blocks[i] = fmt.Sprintf("【片段 %d 笔记】\n%s", i+1, note)
	}
	reduced, err := l.summaryChat(ctx, chat, cfg, []aiChatMessage{
		{Role: "system", Content: buildSummarySystemPrompt(n)},
		{Role: "user", Content: buildSummaryUserPrompt(in,
			"以下是文字稿各片段的结构化笔记（按时间顺序），请综合去重，按要求输出完整的内容总结：\n\n"+strings.Join(blocks, "\n\n"))},
	}, "（综合）")
	if err != nil {
		return mediaSummaryResult{}, err
	}
	return finalizeSummary(reduced)
}

// generateContentTitle 为图文内容拟标题（无总结）；模型输出无效时 ok=false，调用方保留原标题。
func (l *Library) generateContentTitle(ctx context.Context, source string, cfg aiClientConfig, chat aiChatFunc) (string, bool, error) {
	material := utf16Head(jsTrim(source), titleSourceMaxChars)
	if material == "" {
		return "", false, nil
	}
	if chat == nil {
		chat = l.chatCompletion
	}
	res, err := chat(ctx, cfg, []aiChatMessage{
		{Role: "system", Content: titleSystemPrompt},
		{Role: "user", Content: material},
	}, aiChatOptions{Temperature: f64(summaryTemperature), MaxTokens: titleMaxTokens, Timeout: summaryTimeout})
	if err != nil {
		l.recordMainAIUsage(ctx, mainAIUsage{Scenario: "summary", Model: cfg.Model, Failed: true})
		return "", false, err
	}
	u := mainAIUsage{Scenario: "summary", Model: cfg.Model}
	if res.Usage != nil {
		u.PromptTokens, u.CompletionTokens = res.Usage.PromptTokens, res.Usage.CompletionTokens
	}
	l.recordMainAIUsage(ctx, u)
	// 部分模型仍会按「标题：xxx」作答，一并归一
	raw := stripWrappingCodeFence(jsTrim(res.Content))
	line := ""
	for _, entry := range strings.Split(raw, "\n") {
		if jsTrim(entry) != "" {
			line = entry
			break
		}
	}
	if m := reTitleLine.FindStringSubmatch(jsTrim(line)); m != nil {
		line = m[1]
	}
	title, ok := normalizeAiTitle(line)
	return title, ok, nil
}
