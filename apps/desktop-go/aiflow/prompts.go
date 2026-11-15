package aiflow

// 知识域的提示词与用户消息构造（对应 services/knowledge-ai/prompts.ts）。
//
// 文案必须与 TS 逐字一致：提示词一变，模型输出就变，golden（testdata/golden/prompts.json）逐字节核对。
// 改提示词要两边一起改并递增对应的 *PromptVersion；长度与截取按 UTF-16 码元，与 TS 的 .length / .slice 一致。

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	SummaryPromptVersion = "summary-v1"

	SummarySystemPrompt = "你是一个知识管理助手。请用简体中文将用户提供的内容总结为 2~4 条要点：" +
		"每条独立一行并以「- 」开头，总字数不超过 200 字，只输出要点本身，" +
		"不要输出任何前言、标题或结尾说明。"

	// SummaryMapSystemPrompt 是分块摘要 map 阶段的提示词：从单个片段提取要点。
	SummaryMapSystemPrompt = "你是知识管理助手。下面是长文的一个片段，请提取其中的关键信息，" +
		"用简体中文列成 3~6 条要点，每条独立一行以「- 」开头，只输出要点本身，不要输出前言或结尾。"

	TagSuggestionPromptVersion = "autotag-v2"

	TagSuggestionSystemPrompt = "你是一个知识管理助手。请为用户提供的内容生成 2~3 个简短的中文标签，" +
		"每个标签 2~6 个字、概括最核心的主题或领域。只输出标签本身，标签之间用中文逗号分隔，" +
		"不要输出编号、井号或任何其他文字。"

	QAPromptVersion = "qa-v3"

	// QASystemPrompt 用于单发 RAG 管线：宽召回可能混入无关条目，要求模型甄别并只引用支撑回答的资料。
	QASystemPrompt = "你是一个个人知识库问答助手。用户会提供编号资料与问题。资料由自动检索获得，" +
		"可能包含与问题无关的条目：请先甄别，只依据真正相关的资料用简体中文回答，无关资料直接忽略。" +
		"引用资料时在句末标注编号（如 [1]），只标注支撑该句结论的资料，编号必须来自资料列表，不要编造编号；" +
		"资料不足以回答时明确说明缺少哪方面的信息，不要编造资料中不存在的内容；" +
		"回答保持简洁，不要复述资料原文。" +
		"回答可使用 Markdown 排版（如要点列表、**加粗**、`代码`），层级不超过两层，不要输出一级标题。"

	QAAgentPromptVersion = "qa-agent-v1"

	// QAAgentSystemPrompt 是问答 Agent 的动作协议（检索即推理）：模型每轮输出一个 JSON 动作，
	// 代码解析执行并把结果追加进轨迹，直到模型给出最终回答。
	QAAgentSystemPrompt = "你是个人知识库的问答助手，通过工具在知识库中检索资料来回答用户问题。" +
		"知识库包含两类资料：知识条目（用户采集的原文）与 Wiki 页面（AI 编译的结构化知识）。\n" +
		"你每轮只输出一个 JSON 对象（不要输出任何其他文字或代码块标记），从以下三种动作中选一种：\n" +
		`{"action":"search","query":"检索关键词"} —— 搜索知识库，返回带编号的资料列表` + "\n" +
		`{"action":"read","target":3} —— 阅读指定编号的资料内容` + "\n" +
		`{"action":"answer","text":"最终回答"} —— 给出最终回答并结束` + "\n" +
		"纪律：\n" +
		"1. 给出回答前必须至少阅读过一份资料；回答中引用资料在句末标注编号（如 [1]），" +
		"只标注真正支撑该句结论、且你阅读过的资料编号，不要编造编号；\n" +
		"2. search 使用简短关键词（不要整句照抄），没有命中就换一组关键词；" +
		"资料不足以回答时在 answer 中明确说明缺少哪方面的信息，不得编造内容；\n" +
		"3. Wiki 页面适合获取总览与关联线索，知识条目适合细节与原文出处；阅读 Wiki 页面后可以继续阅读它关联的资料；\n" +
		"4. answer 的 text 用简体中文，可使用 Markdown 排版（要点列表、**加粗**），层级不超过两层，" +
		"不要输出一级标题，回答保持简洁、不要复述资料原文。"
)

const (
	// MaxContentLength 是正文送入模型前的截断上限（字符），控制 token 消耗。
	MaxContentLength = 6000
	// SummaryChunkSize：超过此长度的正文走 map-reduce 分块摘要（否则单次即可）。
	SummaryChunkSize = 6000
	// SummaryMaxChunks 约束长文的 LLM 调用次数与成本；超出部分截断。
	SummaryMaxChunks = 8
)

// QaTurn 是一轮历史问答。
type QaTurn struct {
	Question string
	Answer   string
}

// cutLength 返回截到 limit 个码元时实际应保留的码元数：切口落在代理对中间就退一位，不切开 emoji 与扩展汉字。
func cutLength(text string, limit int) int {
	if unit, ok := u16At(text, limit-1); ok && unit >= 0xd800 && unit <= 0xdbff {
		return limit - 1
	}
	return limit
}

// TruncatePlain 截断到上限（无标注后缀；不切开代理对）。
func TruncatePlain(text string, limit int) string {
	if u16Len(text) <= limit {
		return text
	}
	return u16Slice(text, 0, cutLength(text, limit))
}

// TruncateText 截断到上限并附标注；不切断代理对。
func TruncateText(text string, limit int) string {
	if u16Len(text) <= limit {
		return text
	}
	return u16Slice(text, 0, cutLength(text, limit)) + "…（已截断）"
}

// BuildUserPrompt 构造用户消息：标题 + 截断后的正文。
func BuildUserPrompt(title, content string) string {
	return "标题：" + title + "\n\n内容：\n" + TruncateText(content, MaxContentLength)
}

// ChunkContent 把长文按块切分（用于 map-reduce 摘要），超出最大块数的部分截断。
// 与 TS 一样按 UTF-16 码元定边界；切口落在代理对中间时该处落成 U+FFFD（见 jsx.go 的说明）。
func ChunkContent(content string) []string {
	units := utf16.Encode([]rune(content))
	chunks := []string{}
	for offset := 0; offset < len(units) && len(chunks) < SummaryMaxChunks; offset += SummaryChunkSize {
		end := min(offset+SummaryChunkSize, len(units))
		chunks = append(chunks, string(utf16.Decode(units[offset:end])))
	}
	return chunks
}

// BuildSummaryMapPrompt 构造 map 阶段用户消息：标题 + 片段序号 + 片段正文。
func BuildSummaryMapPrompt(title, chunk string, index, total int) string {
	return "《" + title + "》片段 " + strconv.Itoa(index) + "/" + strconv.Itoa(total) + "：\n" + chunk
}

// BuildSummaryReducePrompt 构造 reduce 阶段用户消息：把各片段要点综合去重。
func BuildSummaryReducePrompt(title string, partialSummaries []string) string {
	lines := []string{"标题：" + title, "", "以下是全文各片段的要点，请综合、去重后按要求的格式输出完整结果："}
	lines = append(lines, partialSummaries...)
	return strings.Join(lines, "\n")
}

func appendHistory(parts []string, history []QaTurn) []string {
	if len(history) == 0 {
		return parts
	}
	parts = append(parts, "此前对话：")
	for _, turn := range history {
		parts = append(parts, "问："+turn.Question, "答："+turn.Answer)
	}
	return append(parts, "")
}

// BuildQaUserPrompt 构造问答用户消息：资料块 + 近几轮对话 + 新问题。
func BuildQaUserPrompt(question string, contextBlocks []string, history []QaTurn) string {
	parts := []string{"资料："}
	for _, block := range contextBlocks {
		parts = append(parts, block, "")
	}
	parts = appendHistory(parts, history)
	parts = append(parts, "问题："+question)
	return strings.Join(parts, "\n")
}

// BuildQaAgentPrompt 构造问答 Agent 用户消息：历史对话 + 问题 + 动作轨迹 + 下一步指令。
func BuildQaAgentPrompt(question string, history []QaTurn, transcript []string) string {
	parts := appendHistory(nil, history)
	parts = append(parts, "用户问题："+question, "", "已执行的动作与结果：")
	if len(transcript) == 0 {
		parts = append(parts, "（尚未执行任何动作）")
	} else {
		parts = append(parts, transcript...)
	}
	parts = append(parts, "", "请输出下一步动作（只输出一个 JSON 对象）：")
	return strings.Join(parts, "\n")
}
