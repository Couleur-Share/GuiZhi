package main

// 对应 services/media/transcript-format.ts：文字稿 AI 排版。
//
// 语音转写的原始输出通常无标点、无分段，用 chat 模型分块整理（补标点 / 分自然段 / 去语气词），不改写内容。
// 每块输出做验收（标点密度 / 长度比 / 截断），不合格自动重试一次——思考类模型偶发「复读式敷衍」，
// 不能把未排版结果当成功写库。重试后仍不合格：第一块就失败直接抛错（多半是模型配错），
// 中途失败则收下已排好的部分、其余接回原文，并回一个 partialReason。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

const (
	transcriptFormatChunkChars = 1600   // TRANSCRIPT_FORMAT_CHUNK_CHARS：单块上限
	transcriptFormatLongChars  = 50_000 // TRANSCRIPT_FORMAT_LONG_CHARS：自动排版的长度上限

	formatChunkTimeout     = 240 * time.Second
	formatBudgetPerChunk   = 90 * time.Second
	formatMinTimeBudget    = 300 * time.Second
	formatChunkMaxTokens   = 6144
	formatMaxAttemptsChunk = 2
	formatConcurrency      = 3
	glossaryMaxTerms       = 40
	glossarySourceMaxChars = 500
	glossaryMinTermChars   = 2
	formatSystemPrompt     = "你是文字稿排版助手，负责为语音转写文本补标点、分段。"
)

// 连续的拉丁词串；相邻两词以单个空格相连时并成一条（「GitHub Actions」是一个专名）。
var reLatinRun = regexp.MustCompile(`[A-Za-z][A-Za-z0-9.+#_-]*(?: [A-Za-z][A-Za-z0-9.+#_-]*)*`)
var reLatinTail = regexp.MustCompile(`[.+#_-]+$`)

// extractGlossaryTerms 从标题与简介里提取拉丁字母专名，作为排版时的术语表。
// 本地引擎（SenseVoiceSmall）中英混杂时错误全部落在拉丁词上，而被毁掉的恰好是专有名词。
func extractGlossaryTerms(sources ...string) []string {
	seen := map[string]bool{}
	terms := []string{}
	for _, source := range sources {
		text := utf16Head(jsTrim(source), glossarySourceMaxChars)
		if text == "" {
			continue
		}
		for _, match := range reLatinRun.FindAllString(text, -1) {
			term := reLatinTail.ReplaceAllString(match, "") // 句末标点会被贴进词尾，统一剥掉
			key := strings.ToLower(term)
			if len(term) < glossaryMinTermChars || seen[key] {
				continue
			}
			seen[key] = true
			terms = append(terms, term)
			if len(terms) >= glossaryMaxTerms {
				return terms
			}
		}
	}
	return terms
}

// 说话人对话体的排版约束：不明确要求保留，模型会把前缀当赘语删掉或合并相邻段落，分离就白做了。
var speakerInstruction = strings.Join([]string{
	"",
	"本段文字稿已区分说话人，每个自然段以「说话人 N：」开头。整理时必须原样保留每个",
	"「说话人 N：」前缀（包括编号），不得删除、改写或增补；不同说话人的段落不得合并，",
	"段落顺序也不得调整。",
}, "\n")

// 常驻的通用技术专名：哪条技术视频都会出现、又偏偏不会写进标题的词，音译后完全不可读。
var commonTechTerms = []string{
	"Python", "JavaScript", "TypeScript", "Java", "Rust", "Golang", "SQL", "CSS", "HTML",
	"API", "JSON", "YAML", "Markdown", "HTTP", "schema", "function call", "SDK", "CLI",
	"LLM", "token", "prompt", "embedding", "RAG", "Agent", "MCP", "GPT", "Claude",
	"Gemini", "Transformer",
	"Git", "GitHub", "Docker", "npm", "Node.js", "React", "Vue", "VS Code",
	"Claude Code", "Cursor", "harness",
	"MySQL", "PostgreSQL", "Redis", "MongoDB", "SQLite",
}

// buildGlossaryInstruction：措辞刻意收紧到「把文中已有的错误形式改成表中写法」，放开成自由改写就是拿幻觉换错字。
func buildGlossaryInstruction(glossary []string) string {
	lines := []string{""}
	if len(glossary) > 0 {
		lines = append(lines, "本条内容的专有名词表（取自标题与简介）："+strings.Join(glossary, "、"))
	}
	lines = append(lines, "常见技术名词："+strings.Join(commonTechTerms, "、"))
	lines = append(lines,
		"语音转写常把这类名词听错（如 dacker → Docker、us state → useState、"+
			"circle → SQL）。文中出现表内名词的错误形式时，改成表中写法——"+
			"这是第 4 条的唯一放宽之处。表以外的词不要改动；"+
			"表中没有在文里出现的词，也不要添加进去。")
	return strings.Join(lines, "\n")
}

// 排版指令并入 user 消息（部分中转站会丢弃或弱化 system 消息），原文用显式分隔符包裹。
var formatInstructions = strings.Join([]string{
	"下面是一段语音转写的原始文字，请整理排版，要求：",
	"1. 为全文添加或修正标点符号；",
	"2. 按语义划分自然段，段落之间用空行分隔；",
	"3. 删除明显的口头填充词（嗯、啊、呃等）、开场与结尾**不含任何信息**的寒暄套话" +
		"（「话不多说」「感谢观看，我们下次见」这类），以及转写残留的多余空格；" +
		"凡是提到资源、链接、版本、后续计划的句子一律保留；",
	"4. 仅在十分确定时修正明显的同音错别字；",
	"5. 严格保持原文的表述与信息：不概括、不扩写、不调整语序、不遗漏内容、不添加原文没有的信息，整理后字数应与原文相当；",
	"6. 输入可能是长文字稿的中间片段，开头结尾不完整属正常，按原样整理；",
	"7. 直接输出整理后的正文，不要输出任何说明、标题或 Markdown 标记。",
}, "\n")

func buildChunkMessages(chunk string, glossary []string) []aiChatMessage {
	// 通用词表不依赖具体内容，所以这一段总是给
	parts := []string{formatInstructions, buildGlossaryInstruction(glossary)}
	if countSpeakerPrefixes(chunk) > 0 {
		parts = append(parts, speakerInstruction)
	}
	instructions := strings.Join(parts, "\n")
	return []aiChatMessage{
		{Role: "system", Content: formatSystemPrompt},
		{Role: "user", Content: instructions + "\n\n【待整理文字稿开始】\n" + chunk + "\n【待整理文字稿结束】"},
	}
}

// ── 说话人标记（shared/utils/speaker-note.ts）──

// 段首的「说话人 N：」，N 从 1 起。JS 的 ^（m 标志）还认 \r、U+2028、U+2029 作行界，这里只认 \n；
// 转写文本由本地引擎生成，不含这几种行界。
var reSpeakerPrefix = regexp.MustCompile(`(?m)^说话人 [0-9]+：`)

func countSpeakerPrefixes(text string) int { return len(reSpeakerPrefix.FindAllStringIndex(text, -1)) }

// listSpeakers 出现过的说话人标签，按首次出现排序、去重。
func listSpeakers(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reSpeakerPrefix.FindAllString(text, -1) {
		label := strings.TrimSuffix(m, "：")
		if !seen[label] {
			seen[label] = true
			out = append(out, label)
		}
	}
	return out
}

// ── 模型解析 ──

// resolveChatModel 从共享 AI 配置按路由优先级挑 chat 模型：routes 依次尝试，
// 然后默认模型、然后第一个；未配置齐全返回 nil。logLabel 用于读取失败时的日志。
func (l *Library) resolveChatModel(logLabel string, routes ...string) *aiClientConfig {
	cfg, err := l.readAIConfig()
	if err != nil {
		fmt.Printf("[media] 读取%s模型配置失败: %v\n", logLabel, err)
		return nil
	}
	var chat []*omap
	for _, m := range cfg.Models {
		caps, _ := m.vals["capabilities"].(*omap)
		if caps != nil {
			if c, ok := caps.get("chat"); ok && c == false {
				continue
			}
		}
		chat = append(chat, m)
	}
	byID := func(id any) *omap {
		s, _ := id.(string)
		if s == "" {
			return nil
		}
		for _, m := range chat {
			if m.vals["id"] == s {
				return m
			}
		}
		return nil
	}
	var model *omap
	for _, route := range routes {
		id, _ := cfg.Routes.get(route)
		if model = byID(id); model != nil {
			break
		}
	}
	if model == nil {
		for _, m := range chat {
			if m.vals["isDefault"] == true {
				model = m
				break
			}
		}
	}
	if model == nil && len(chat) > 0 {
		model = chat[0]
	}
	if model == nil {
		return nil
	}
	str := func(k string) string { s, _ := model.vals[k].(string); return jsTrim(s) }
	out := &aiClientConfig{
		Provider:    func() string { s, _ := model.vals["provider"].(string); return s }(),
		APIProtocol: func() string { s, _ := model.vals["apiProtocol"].(string); return s }(),
		APIKey:      str("apiKey"), APIURL: str("apiUrl"), Model: str("model"),
	}
	if out.APIURL == "" || out.APIKey == "" || out.Model == "" {
		return nil
	}
	return out
}

// resolveTranscriptFormatterConfig 排版用模型：fastText 优先，回退 mainText。
func (l *Library) resolveTranscriptFormatterConfig() *aiClientConfig {
	return l.resolveChatModel("文字稿排版", "fastText", "mainText")
}

// ── 切块 ──

func isChunkBoundary(u uint16) bool {
	switch u {
	case '。', '！', '？', '!', '?', '；', ';', '.', '\n':
		return true
	}
	return jsSpace(rune(u))
}

func fromUnits(u []uint16) string { return string(utf16.Decode(u)) }

// splitTranscriptChunks 按句读边界把长文字稿切成排版块；无标点时退化到空格/硬切。
// 下标按 UTF-16 码元计，与 JS 的 String 一致。
func splitTranscriptChunks(raw string, maxChars int) []string {
	text := jsTrim(raw)
	if text == "" {
		return nil
	}
	units := utf16.Encode([]rune(text))
	if len(units) <= maxChars {
		return []string{text}
	}
	var chunks []string
	start := 0
	for start < len(units) {
		end := min(start+maxChars, len(units))
		if end < len(units) {
			// 从窗口末尾向前找边界，避免把句子拦腰截断；最多回退半个窗口
			for i := end - 1; i > start+maxChars/2; i-- {
				if isChunkBoundary(units[i]) {
					end = i + 1
					break
				}
			}
		}
		if chunk := jsTrim(fromUnits(units[start:end])); chunk != "" {
			chunks = append(chunks, chunk)
		}
		start = end
	}
	return chunks
}

var reTranscriptPunct = regexp.MustCompile(`[。，、！？：；…,.!?:;\n]`)

// rejectFormattedChunk 排版输出验收：空输出、被截断、长度跑偏、仍无标点分段、说话人前缀数量不符都判不合格。
// 返回不合格原因，合格返回空串。
func rejectFormattedChunk(rawChunk string, res aiChatResult) string {
	cleaned := jsTrim(res.Content)
	if cleaned == "" {
		return "输出为空"
	}
	if res.FinishReason == "length" {
		return "输出被 max_tokens 截断"
	}
	ratio := float64(utf16Len(cleaned)) / float64(utf16Len(rawChunk))
	if ratio < 0.65 || ratio > 2 {
		return fmt.Sprintf("输出长度异常（原文的 %d%%）", int(math.Floor(ratio*100+0.5)))
	}
	punct := len(reTranscriptPunct.FindAllStringIndex(cleaned, -1))
	required := max(1, utf16Len(cleaned)/200)
	if punct < required {
		return fmt.Sprintf("输出仍缺少标点分段（仅 %d 处）", punct)
	}
	// 说话人前缀必须一条不少一条不多
	if expected := countSpeakerPrefixes(rawChunk); expected > 0 {
		if actual := countSpeakerPrefixes(cleaned); actual != expected {
			return fmt.Sprintf("说话人前缀数量不符（原文 %d 处，输出 %d 处）", expected, actual)
		}
	}
	return ""
}

// ── 整篇排版 ──

type formatTranscriptOptions struct {
	// 超过该长度直接跳过排版；0 表示用自动链路的默认上限
	MaxTotalChars int
	// 已完成块数（并发下不保证按顺序完成，报的是计数不是序号）
	OnProgress func(completed, total int)
	Glossary   []string
	// 整篇时间预算，默认按块数缩放；测试注入用
	TimeBudget time.Duration
	// 测试注入：底层 chat 调用
	Chat aiChatFunc
}

type formatTranscriptResult struct {
	Text          string
	SkippedReason string // 非空表示本次没有排版（超长跳过）
	PartialReason string // 非空表示只排完了前面若干块，其余按原文拼回
}

func (l *Library) formatTranscript(ctx context.Context, raw string, cfg aiClientConfig, o formatTranscriptOptions) (formatTranscriptResult, error) {
	text := jsTrim(raw)
	if text == "" {
		return formatTranscriptResult{Text: text}, nil
	}
	maxTotal := o.MaxTotalChars
	if maxTotal == 0 {
		maxTotal = transcriptFormatLongChars
	}
	if n := utf16Len(text); n > maxTotal {
		return formatTranscriptResult{Text: text, SkippedReason: fmt.Sprintf("文字稿 %d 字，超过自动排版上限 %d 字", n, maxTotal)}, nil
	}

	chat := o.Chat
	if chat == nil {
		chat = l.chatCompletion
	}
	chunks := splitTranscriptChunks(text, transcriptFormatChunkChars)
	budget := o.TimeBudget
	if budget == 0 {
		budget = max(formatMinTimeBudget, time.Duration(len(chunks))*formatBudgetPerChunk)
	}
	deadline := time.Now().Add(budget)
	fmt.Printf("[media] 文字稿排版开始（model=%s，共 %d 块，术语 %d 条）\n", cfg.Model, len(chunks), len(o.Glossary))

	offsets := chunkOffsets(text, chunks)
	done := make([]*string, len(chunks))
	reasons := make([]string, len(chunks))
	var (
		mu        sync.Mutex
		completed int
		nextIndex int
		firstErr  error
	)

	type outcome struct {
		text   *string
		reason string
	}
	runChunk := func(chunk string, index int) (outcome, error) {
		lastReason := ""
		for attempt := 1; attempt <= formatMaxAttemptsChunk; attempt++ {
			if err := ctx.Err(); err != nil {
				return outcome{}, err
			}
			res, err := chat(ctx, cfg, buildChunkMessages(chunk, o.Glossary), aiChatOptions{
				Temperature: f64(0.1), MaxTokens: formatChunkMaxTokens, Timeout: formatChunkTimeout,
			})
			if err != nil {
				// 用户点的取消不该留下半成品，直接透传
				if ctx.Err() != nil {
					return outcome{}, err
				}
				// 记在这里而不是重试成功之后：超时与限流同样可能已经产生费用
				l.recordMainAIUsage(ctx, mainAIUsage{Scenario: "formatting", Model: cfg.Model, Failed: true})
				if attempt >= formatMaxAttemptsChunk {
					return outcome{reason: err.Error()}, nil
				}
				fmt.Printf("[media] 第 %d/%d 块排版请求失败（%s），重试一次\n", index+1, len(chunks), err.Error())
				continue
			}
			// 请求本身成功了就要记账，哪怕下面的验收把它判为不合格
			u := mainAIUsage{Scenario: "formatting", Model: cfg.Model}
			if res.Usage != nil {
				u.PromptTokens, u.CompletionTokens = res.Usage.PromptTokens, res.Usage.CompletionTokens
			}
			l.recordMainAIUsage(ctx, u)
			reason := rejectFormattedChunk(chunk, res)
			if reason == "" {
				t := jsTrim(res.Content)
				return outcome{text: &t}, nil
			}
			lastReason = reason
			retry := ""
			if attempt < formatMaxAttemptsChunk {
				retry = "，重试一次"
			}
			fmt.Printf("[media] 第 %d/%d 块排版验收不合格：%s%s\n", index+1, len(chunks), reason, retry)
		}
		return outcome{reason: "模型未按排版要求输出（" + lastReason + "）"}, nil
	}

	worker := func() {
		for {
			mu.Lock()
			if firstErr != nil {
				mu.Unlock()
				return
			}
			if err := ctx.Err(); err != nil {
				firstErr = err
				mu.Unlock()
				return
			}
			index := nextIndex
			nextIndex++
			mu.Unlock()
			if index >= len(chunks) {
				return
			}
			// 预算耗尽就不再发新请求；但第一块无论如何要试一次
			if index > 0 && !time.Now().Before(deadline) {
				return
			}
			out, err := runChunk(chunks[index], index)
			mu.Lock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			done[index], reasons[index] = out.text, out.reason
			completed++
			c := completed
			mu.Unlock()
			if o.OnProgress != nil {
				o.OnProgress(c, len(chunks))
			}
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < min(formatConcurrency, len(chunks)); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); worker() }()
	}
	wg.Wait()
	if firstErr != nil {
		return formatTranscriptResult{}, firstErr
	}

	// 并发下「成功了几块」不再等于「排到第几块」：能留下的是第一个没完成的块之前那一段，
	// 它后面即便有块排好了也不能用——中间缺一块就接不上原文
	firstIncomplete := -1
	for i, d := range done {
		if d == nil {
			firstIncomplete = i
			break
		}
	}
	join := func(parts []*string) string {
		ss := make([]string, len(parts))
		for i, p := range parts {
			ss[i] = *p
		}
		return strings.Join(ss, "\n\n")
	}
	if firstIncomplete == -1 {
		return formatTranscriptResult{Text: join(done)}, nil
	}
	reason := reasons[firstIncomplete]
	if reason == "" {
		reason = "整篇排版超出时间预算"
	}
	if firstIncomplete == 0 {
		// 一块都没成，多半是模型名/鉴权这类硬故障，不能粉饰成「部分成功」
		return formatTranscriptResult{}, errors.New(reason)
	}
	fmt.Printf("[media] 文字稿排版中止于第 %d/%d 块：%s\n", firstIncomplete+1, len(chunks), reason)
	tail := jsTrim(fromUnits(utf16.Encode([]rune(text))[offsets[firstIncomplete]:]))
	var parts []string
	for _, p := range []string{join(done[:firstIncomplete]), tail} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return formatTranscriptResult{
		Text:          strings.Join(parts, "\n\n"),
		PartialReason: fmt.Sprintf("已排版 %d/%d 块，其余保留原始转写：%s", firstIncomplete, len(chunks), reason),
	}, nil
}

// chunkOffsets 每块在原文中的起始下标（UTF-16 码元；chunks 是 text 的连续切片，trim 过）。
func chunkOffsets(text string, chunks []string) []int {
	tu := utf16.Encode([]rune(text))
	offsets := make([]int, 0, len(chunks))
	cursor := 0
	for _, chunk := range chunks {
		cu := utf16.Encode([]rune(chunk))
		at := indexUnits(tu, cu, cursor)
		start := cursor
		if at >= 0 {
			start = at
		}
		offsets = append(offsets, start)
		cursor = start + len(cu)
	}
	return offsets
}

func indexUnits(hay, needle []uint16, from int) int {
	for i := from; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
