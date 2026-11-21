package services

import (
	"encoding/json"
	"errors"
)

// M2-6c：AI 摘要卡片（AiSummaryCard）、图中文字 OCR（AiOcrCard）与标签建议（AiTagSuggest）的数据形状。

// ErrAINotConfigured 表示当前场景或视觉路由尚未配置可用 AI 模型。
var ErrAINotConfigured = errors.New("尚未配置 AI 服务")

// LibrarySummaryResult 是条目摘要的生成结果（aiflow.SummaryResult）。
type LibrarySummaryResult struct {
	Text      string
	Truncated bool
}

// LibraryItemFieldsPatch 是单条更新正文或摘要的局部补丁（knowledge:update）；nil 的字段不传。
type LibraryItemFieldsPatch struct {
	Summary *string
	Content *string
}

// MarshalJSON 只写出非 nil 字段。
func (p LibraryItemFieldsPatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if p.Summary != nil {
		m["summary"] = *p.Summary
	}
	if p.Content != nil {
		m["content"] = *p.Content
	}
	return json.Marshal(m)
}
