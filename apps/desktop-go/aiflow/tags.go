package aiflow

// AI 标签建议（对应 services/knowledge-ai/suggest-tags.ts）：为条目生成 2~3 个中文标签。

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
)

const (
	tagMaxLength = 12 // UTF-16 码元数
	tagMaxCount  = 5
	// tagMaxTokens：标签本身只要几十 token，余量留给思考类模型的推理消耗（推理计入 max_tokens）。
	tagMaxTokens   = 2048
	tagTemperature = 0.3
)

// reTagPrefix 对应 /^\s*(?:\d+[.、)]\s*)?#?/：去掉行首的空白、编号与井号。
var reTagPrefix = regexp.MustCompile(`^[` + jsSpaceClass + `]*(?:[0-9]+[.、)][` + jsSpaceClass + `]*)?#?`)

// tagQuotes 是要去掉的引号类字符：["'「」『』`]。
const tagQuotes = "\"'「」『』`"

// ParseTagResponse 解析模型输出的标签列表：容忍编号、井号、多种分隔符与引号污染。
func ParseTagResponse(response string) []string {
	tags := []string{}
	pieces := strings.FieldsFunc(response, func(r rune) bool { return strings.ContainsRune(",，、;；\n", r) })
	for _, raw := range pieces {
		cleaned := reTagPrefix.ReplaceAllString(raw, "")
		cleaned = strings.Map(func(r rune) rune {
			if strings.ContainsRune(tagQuotes, r) {
				return -1
			}
			return r
		}, cleaned)
		cleaned = jsTrim(cleaned)
		if cleaned != "" && u16Len(cleaned) <= tagMaxLength && !slices.Contains(tags, cleaned) {
			tags = append(tags, cleaned)
		}
		if len(tags) >= tagMaxCount {
			break
		}
	}
	return tags
}

// SuggestTags 为条目建议标签。
func (r *Runner) SuggestTags(ctx context.Context, title, content string) ([]string, error) {
	text := jsTrim(content)
	if text == "" && jsTrim(title) == "" {
		return nil, errors.New("内容为空，无法建议标签")
	}
	displayTitle := jsTrim(title)
	if displayTitle == "" {
		displayTitle = "无标题"
	}

	temperature := tagTemperature
	result, err := r.RunScenarioChat(ctx, ScenarioTagging, []ChatMessage{
		{Role: "system", Content: TagSuggestionSystemPrompt},
		{Role: "user", Content: BuildUserPrompt(displayTitle, text)},
	}, ScenarioChatOptions{Temperature: &temperature, MaxTokens: tagMaxTokens})
	if err != nil {
		return nil, err
	}

	tags := ParseTagResponse(result.Content)
	if len(tags) == 0 {
		return nil, errors.New("未能解析出有效标签")
	}
	return tags, nil
}
