package aiflow

// SSE 流式响应解析（对应 services/ai-stream.ts 的 processStreamTextChunk / finalizeStreamState）。
//
// 逐行解析 `data: {...}` 事件并把增量交给回调。沿用 TS 的宽松规则：
//   - 只认 `data: ` 前缀（冒号后必须有空格），`data: [DONE]` 与空行忽略；
//   - 解析失败的事件整条忽略，不中断流；
//   - 非 flush 时最后一行可能是半条事件，留在缓冲里等下一块。

import (
	"strings"
)

type streamState struct {
	fullContent  strings.Builder
	thinking     strings.Builder
	buffer       string
	chunkCount   int
	finishReason string
	usage        *TokenUsage
}

// process 处理一块文本；flush 为 true 时把缓冲里剩下的内容也当作完整行处理。
func (s *streamState) process(chunk string, onStream func(string), flush bool) {
	s.buffer += chunk
	lines := strings.Split(s.buffer, "\n")
	if flush {
		s.buffer = ""
	} else {
		s.buffer = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		trimmed := jsTrim(line)
		if trimmed == "" || trimmed == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(trimmed, "data: ") {
			continue
		}
		parsed, err := parseJSON(trimmed[len("data: "):])
		if err != nil {
			continue
		}
		event, ok := parsed.(map[string]any)
		if !ok {
			continue
		}

		var first map[string]any
		if choices, ok := event["choices"].([]any); ok && len(choices) > 0 {
			first, _ = choices[0].(map[string]any)
		}
		// finish_reason 与 usage 各自独立到达：前者挂在最后一个 choice 上，后者通常是 choices 为空的收尾事件
		if fr, ok := first["finish_reason"].(string); ok && fr != "" {
			s.finishReason = fr
		}
		if usage := event["usage"]; jsTruthy(usage) {
			fields, _ := usage.(map[string]any)
			s.usage = &TokenUsage{
				PromptTokens:     numberOrZero(fields["prompt_tokens"]),
				CompletionTokens: numberOrZero(fields["completion_tokens"]),
			}
		}

		delta := first["delta"]
		if !jsTruthy(delta) {
			continue
		}
		s.chunkCount++
		d, _ := delta.(map[string]any)
		if rc := d["reasoning_content"]; jsTruthy(rc) {
			s.thinking.WriteString(jsToString(rc))
		}
		if c := d["content"]; jsTruthy(c) {
			text := jsToString(c)
			s.fullContent.WriteString(text)
			if onStream != nil {
				onStream(text)
			}
		}
	}
}

// finalize 对应 finalizeStreamState。
func (s *streamState) finalize() ChatResult {
	return ChatResult{
		Content:         s.fullContent.String(),
		ThinkingContent: s.thinking.String(),
		FinishReason:    s.finishReason,
		Usage:           s.usage,
	}
}
