package main

// 对应 services/import/forum-summary.ts：论坛讨论总结（按方案 / 观点归类，而非按楼层复述）。
// 与视频总结共用「标题：」输出协议与 mainText 路由，但提示词、清洗与分块各自独立。

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	forumSingleShotMaxChars = 12_000 // 单发上限：超过则按回复边界分块走 map-reduce
	forumChunkMaxChars      = 12_000
	forumMaxChunks          = 8 // 约束超长帖的调用次数与成本，超出部分截断
	forumSummaryMaxTokens   = 6144
	forumSummaryTimeout     = 180 * time.Second
	forumSummaryTemperature = 0.3
)

const forumSummarySystemPrompt = "你是论坛讨论总结助手。用户会提供一个论坛帖子的标题、主楼内容与回复，" +
	"请把讨论里真正有用的信息提炼出来，让用户不看原帖也能拿到结论。要求：\n" +
	"1. 开头用 2-3 个短句（不加标题）说明：帖子在讨论什么问题、讨论整体倾向于什么结论。" +
	"这段之后正文里不要再出现成段的大段落；\n" +
	"2. 主体按「方案 / 观点」归类，不要按楼层顺序复述。每一类用「### 方案名称」" +
	"作为独立一行的小标题；\n" +
	"3. 小标题下一律用「- 」无序列表展开，一条讲清一件事，不要把几件事塞进一条长句。" +
	"子列表仅用于真正的从属关系，全文最多两层列表。" +
	"按实际有的内容取舍这些角度：具体做法、多少人支持（如「多人推荐」「2 人提到」）、" +
	"给出的理由与实测数据、缺点或适用前提。原帖没提到的角度就不写，不要为凑格式硬编；\n" +
	"4. 加粗只用于工具名、参数、价格、延迟等关键信息，以及列表项开头的简短标签；" +
	"同一行内除这些词外不要再加粗；\n" +
	"5. 末尾用「### 共识与分歧」小节收尾，同样用列表分别写清：哪些结论大家看法一致，" +
	"哪些存在明显争论；\n" +
	"6. 忠实于原帖，不要编造原帖没有的内容；灌水、玩梗、纯附和与人身攻击的回复直接忽略；\n" +
	"7. 篇幅与信息量匹配：讨论充分的长帖 400-700 字，回复稀少的短帖精简即可；\n" +
	"8. 输出简体中文 Markdown；小标题一律用 ###，不要用 # 或 ##，" +
	"不要 --- 分隔线；不要输出「讨论总结」这类总标题，也不要任何前言或结尾说明；\n" +
	"9. 讨论中涉及多项对比（如方案优劣、参数差异）时可用 Markdown 表格呈现，" +
	"列数 3-5 列、行数不超过 8 行，能用列表讲清的不要硬上表格。"

const forumTitleRule = "10. 这个帖子的标题没说清它在讲什么，请顺便替它拟一个：在上述总结正文之前，" +
	"第一行以「标题：」开头单独成行，15-30 字，用讨论真正要解决的问题或得出的结论" +
	"来概括，书面语、不加书名号或引号，之后空一行再开始总结正文。这一行不算前言。"

func buildForumSystemPrompt(withTitle bool) string {
	if withTitle {
		return forumSummarySystemPrompt + "\n" + forumTitleRule
	}
	return forumSummarySystemPrompt
}

const forumMapSystemPrompt = "你是论坛讨论总结助手。下面是某个论坛帖子回复的一个片段，" +
	"请按「方案 / 观点」归类提取这一段里的有效信息：每类用「### 方案名称」独立成行，" +
	"其下用「- 」列表写要点（每条一事、最多两层），加粗只标标签与专名。" +
	"涉及多项对比时可用表格。" +
	"保留具体的工具名、参数、理由与踩坑经验，忽略灌水与玩梗。" +
	"输出简体中文 Markdown，小标题一律用 ###，不要用 # 或 ##，不要 --- 分隔线；" +
	"只输出笔记本身，不要前言或结尾说明。"

var (
	reForumRedundantTitle = regexp.MustCompile(`^#{0,6}` + fsp + `*\*{0,2}(?:讨论|内容)?总结\*{0,2}[:：]?$`)
	reForumBoldOnly       = regexp.MustCompile(`^` + fsp + `*\*\*(.+?)\*\*[：:]?` + fsp + `*$`)
	reForumHashHeading    = regexp.MustCompile(`^` + fsp + `*#{1,6}` + fsp + `+(.+?)` + fsp + `*$`)
	reForumHRule          = regexp.MustCompile(`^` + fsp + `*-{3,}` + fsp + `*$`)
	reForumPlaceholder    = regexp.MustCompile(`(?i)^v2ex` + fsp + `*(?:帖子|主题)` + fsp + `*\d+$`)
	reForumFiller         = regexp.MustCompile(`求助|求推荐|求方案|求教|求指点|求解答|求个|跪求|请教|请问|想问|问一下|问个问题|问问|咨询|有没有人|有人|大家|各位|在线等|万分感谢|谢谢|急|吗|呢|吧`)
	reForumNonSubstantive = regexp.MustCompile(`[` + jsSpaceClass + `\p{P}]`)
	reForumStarStar       = regexp.MustCompile(`\*\*`)
	reForumTrailColon     = regexp.MustCompile(`[：:]$`)
	reForumWSAll          = regexp.MustCompile(fsp + `+`)
)

func cleanForumHeadingText(text string) string {
	t := jsTrim(reForumStarStar.ReplaceAllString(text, ""))
	return jsTrim(reForumTrailColon.ReplaceAllString(t, ""))
}

// normalizeForumHeadingLine 把 # / ## 拉平成 ###，并把独占一行的加粗转成真标题。
func normalizeForumHeadingLine(line string) string {
	if m := reForumHashHeading.FindStringSubmatch(line); m != nil {
		return "### " + cleanForumHeadingText(m[1])
	}
	if m := reForumBoldOnly.FindStringSubmatch(line); m != nil {
		return "### " + cleanForumHeadingText(m[1])
	}
	return line
}

// sanitizeForumSummary 输出协议的确定性清洗：标题规范化、删分隔线、去冗余总标题、折叠空行。
func sanitizeForumSummary(raw string) string {
	var lines []string
	for _, line := range strings.Split(stripWrappingCodeFence(jsTrim(raw)), "\n") {
		if reForumHRule.MatchString(line) {
			continue
		}
		lines = append(lines, normalizeForumHeadingLine(line))
	}
	for len(lines) > 0 && jsTrim(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) > 0 && reForumRedundantTitle.MatchString(jsTrim(lines[0])) {
		lines = lines[1:]
	}
	return jsTrim(reNewlines3.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

const (
	forumMinTitleLength       = 8
	forumMinSubstantiveLength = 6
)

// needsForumAiTitle 判断是否需要让模型重拟标题：只在标题压根没描述任何东西时才重拟。
func needsForumAiTitle(title string) bool {
	normalized := jsTrim(reForumWSAll.ReplaceAllString(title, " "))
	if normalized == "" || reForumPlaceholder.MatchString(normalized) {
		return true
	}
	if utf8.RuneCountInString(reForumWSAll.ReplaceAllString(normalized, "")) < forumMinTitleLength {
		return true
	}
	substantive := reForumNonSubstantive.ReplaceAllString(reForumFiller.ReplaceAllString(normalized, ""), "")
	return utf8.RuneCountInString(substantive) < forumMinSubstantiveLength
}

// forumSummaryReply 总结只用得上这三个字段。
type forumSummaryReply struct {
	Floor   int
	Author  string
	Content string
}

type forumSummaryInput struct {
	Title   string
	Content string // 主楼正文
	Replies []forumSummaryReply
}

type forumSummaryResult struct {
	Summary  string
	Title    string
	HasTitle bool // 模型没按协议输出或原标题够用时为 false
}

func formatForumSummaryReply(r forumSummaryReply) string {
	return strconv.Itoa(r.Floor) + " 楼 " + r.Author + "：" + r.Content
}

// splitForumReplyChunks 按回复边界分块：一条回复不能被切成两半，单条超长的回复自成一块。
func splitForumReplyChunks(replies []forumSummaryReply, maxChars int) []string {
	var chunks []string
	var current []string
	length := 0
	for _, r := range replies {
		line := formatForumSummaryReply(r)
		if len(current) > 0 && length+utf16Len(line) > maxChars {
			chunks = append(chunks, strings.Join(current, "\n"))
			current, length = nil, 0
		}
		current = append(current, line)
		length += utf16Len(line) + 1
	}
	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, "\n"))
	}
	return chunks
}

func buildForumHeader(in forumSummaryInput) string {
	title := jsTrim(in.Title)
	if title == "" {
		title = "无标题"
	}
	parts := []string{"帖子标题：《" + title + "》"}
	if c := jsTrim(in.Content); c != "" {
		parts = append(parts, "主楼内容：\n"+c)
	}
	return strings.Join(parts, "\n")
}

func (l *Library) forumSummaryChat(ctx context.Context, chat aiChatFunc, cfg aiClientConfig, messages []aiChatMessage, label string) (string, error) {
	res, err := chat(ctx, cfg, messages, aiChatOptions{Temperature: f64(forumSummaryTemperature), MaxTokens: forumSummaryMaxTokens, Timeout: forumSummaryTimeout})
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
		fmt.Printf("[import] 论坛讨论总结%s输出被 max_tokens 截断\n", label)
	}
	content := jsTrim(res.Content)
	if content == "" {
		return "", fmt.Errorf("模型未返回讨论总结（%s输出为空）", label)
	}
	return content, nil
}

// finalizeForumSummary 按是否请求了拟题拆出标题，再清洗正文；清洗后为空视为没生成（返回 nil）。
func finalizeForumSummary(raw string, withTitle bool) *forumSummaryResult {
	body, title, has := raw, "", false
	if withTitle {
		title, has, body = splitTitleFromSummary(stripWrappingCodeFence(jsTrim(raw)))
	}
	summary := sanitizeForumSummary(body)
	if summary == "" {
		return nil
	}
	return &forumSummaryResult{Summary: summary, Title: title, HasTitle: has}
}

// generateForumSummary 生成讨论总结。没有回复时返回 nil——只有主楼的帖子等同于一篇短文。
func (l *Library) generateForumSummary(ctx context.Context, in forumSummaryInput, cfg aiClientConfig, chat aiChatFunc) (*forumSummaryResult, error) {
	if len(in.Replies) == 0 {
		return nil, nil
	}
	if chat == nil {
		chat = l.chatCompletion
	}
	withTitle := needsForumAiTitle(in.Title)
	header := buildForumHeader(in)
	lines := make([]string, len(in.Replies))
	for i, r := range in.Replies {
		lines[i] = formatForumSummaryReply(r)
	}
	replyText := strings.Join(lines, "\n")

	if utf16Len(header)+utf16Len(replyText) <= forumSingleShotMaxChars {
		content, err := l.forumSummaryChat(ctx, chat, cfg, []aiChatMessage{
			{Role: "system", Content: buildForumSystemPrompt(withTitle)},
			{Role: "user", Content: fmt.Sprintf("%s\n\n以下是全部 %d 条回复：\n%s", header, len(in.Replies), replyText)},
		}, "")
		if err != nil {
			return nil, err
		}
		return finalizeForumSummary(content, withTitle), nil
	}

	chunks := splitForumReplyChunks(in.Replies, forumChunkMaxChars)
	truncated := len(chunks) > forumMaxChunks
	if truncated {
		fmt.Printf("[import] 讨论过长（%d 块），仅总结前 %d 块\n", len(chunks), forumMaxChunks)
		chunks = chunks[:forumMaxChunks]
	}
	notes := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		note, err := l.forumSummaryChat(ctx, chat, cfg, []aiChatMessage{
			{Role: "system", Content: forumMapSystemPrompt},
			{Role: "user", Content: fmt.Sprintf("%s\n\n回复片段 %d/%d：\n%s", header, i+1, len(chunks), chunk)},
		}, fmt.Sprintf("（片段 %d/%d）", i+1, len(chunks)))
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	blocks := make([]string, len(notes))
	for i, n := range notes {
		blocks[i] = fmt.Sprintf("【片段 %d 要点】\n%s", i+1, n)
	}
	suffix := ""
	if truncated {
		suffix = "（回复过多，仅覆盖前一部分）"
	}
	reduced, err := l.forumSummaryChat(ctx, chat, cfg, []aiChatMessage{
		{Role: "system", Content: buildForumSystemPrompt(withTitle)},
		{Role: "user", Content: header + "\n\n以下是各回复片段的要点笔记" + suffix + "，请跨片段归并同类方案后，按要求输出完整的讨论总结：\n\n" + strings.Join(blocks, "\n\n")},
	}, "（综合）")
	if err != nil {
		return nil, err
	}
	return finalizeForumSummary(reduced, withTitle), nil
}
