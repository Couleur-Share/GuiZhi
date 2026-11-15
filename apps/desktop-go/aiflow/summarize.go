package aiflow

// 条目摘要（对应 services/knowledge-ai/summarize.ts）：短文单发，长文 map-reduce 分块。

import (
	"context"
	"errors"
)

const summaryTemperature = 0.3

// summaryMaxTokens：摘要正文不超过 200 字（约 150 token），余量全部留给思考类模型的推理消耗——
// 推理 token 同样计入 max_tokens，额度不够时模型会在思考阶段就被截断、只回一个空正文
// （v0.7.1 实际踩到过：800 token 被 799 个推理 token 吃光）。
const summaryMaxTokens = 4096

// SummaryResult 是摘要结果。
type SummaryResult struct {
	Text string
	// Truncated：任一阶段撞上 max_tokens，结果能用，但话没说完，得让用户知道。
	Truncated bool
}

func (r *Runner) summaryChat(ctx context.Context, system, user string) (ScenarioChatResult, error) {
	temperature := summaryTemperature
	return r.RunScenarioChat(ctx, ScenarioSummary, []ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, ScenarioChatOptions{Temperature: &temperature, MaxTokens: summaryMaxTokens})
}

// GenerateSummary 生成条目摘要。
func (r *Runner) GenerateSummary(ctx context.Context, title, content string) (SummaryResult, error) {
	text := jsTrim(content)
	if text == "" {
		return SummaryResult{}, errors.New("内容为空，无法生成摘要")
	}
	displayTitle := jsTrim(title)
	if displayTitle == "" {
		displayTitle = "无标题"
	}

	if u16Len(text) <= SummaryChunkSize {
		result, err := r.summaryChat(ctx, SummarySystemPrompt, BuildUserPrompt(displayTitle, text))
		if err != nil {
			return SummaryResult{}, err
		}
		return SummaryResult{Text: jsTrim(result.Content), Truncated: result.FinishReason == "length"}, nil
	}

	// map：逐片段提取要点
	chunks := ChunkContent(text)
	partials := make([]string, 0, len(chunks))
	// 被截断的片段要点会残缺，reduce 出来的摘要同样不完整，所以逐段累计
	truncated := false
	for i, chunk := range chunks {
		result, err := r.summaryChat(ctx, SummaryMapSystemPrompt, BuildSummaryMapPrompt(displayTitle, chunk, i+1, len(chunks)))
		if err != nil {
			return SummaryResult{}, err
		}
		partials = append(partials, jsTrim(result.Content))
		truncated = truncated || result.FinishReason == "length"
	}

	// reduce：综合去重输出最终要点
	reduced, err := r.summaryChat(ctx, SummarySystemPrompt, BuildSummaryReducePrompt(displayTitle, partials))
	if err != nil {
		return SummaryResult{}, err
	}
	return SummaryResult{
		Text:      jsTrim(reduced.Content),
		Truncated: truncated || reduced.FinishReason == "length",
	}, nil
}
