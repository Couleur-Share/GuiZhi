package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

// askFixtureJSON 是 AI 问答对照场景的数据夹具：与 React 采集器（scripts/parity/lib/react-ask.mjs）读的是同一个文件，
// 两侧因此处在同一组合成数据里。会话的消息体（messages）沿用渲染层 messagesJson 的格式，原样当作消息体 JSON 使用。
//
//go:embed testdata/ask-fixture.json
var askFixtureJSON []byte

// AskFixtureSession 是夹具里的一条会话：元数据与消息体（JSON 数组原文）。
type AskFixtureSession struct {
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	Pinned       bool                  `json:"pinned"`
	CreatedAt    int64                 `json:"createdAt"`
	UpdatedAt    int64                 `json:"updatedAt"`
	Scope        string                `json:"scope"`
	ItemID       string                `json:"itemId"`
	ArticleTitle string                `json:"articleTitle"`
	WebEnabled   *bool                 `json:"webEnabled"`
	Target       *aiflow.ArticleTarget `json:"target"`
	Messages     json.RawMessage       `json:"messages"`
}

// Record 把夹具会话转成服务层的会话记录（消息体 JSON 原样）。
func (s AskFixtureSession) Record() services.AskSessionRecord {
	rec := services.AskSessionRecord{MessagesJSON: string(s.Messages)}
	rec.ID, rec.Title, rec.Pinned = s.ID, s.Title, s.Pinned
	rec.CreatedAt, rec.UpdatedAt = s.CreatedAt, s.UpdatedAt
	rec.Scope, rec.ItemID, rec.ArticleTitle = s.Scope, s.ItemID, s.ArticleTitle
	rec.WebEnabled, rec.Target = s.WebEnabled, s.Target
	return rec
}

// AskFixture 是夹具的内容。
type AskFixture struct {
	// Now、Timezone、Locale 是对照用的固定时钟（两侧同值：会话时间与证据的采集时间写法取决于它们）。
	Now      int64  `json:"now"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	// Semantic 是语义索引进度（已配置 embedding 模型的场景）。
	Semantic struct {
		EligibleItems int `json:"eligibleItems"`
		IndexedItems  int `json:"indexedItems"`
	} `json:"semantic"`
	// ConversationId 是「有一轮问答」的全局会话，ArticleSessionId 是围绕本文的会话（对应文章是 Article）。
	ConversationID   string `json:"conversationId"`
	ArticleSessionID string `json:"articleSessionId"`
	Article          struct {
		ItemID string `json:"itemId"`
		Title  string `json:"title"`
	} `json:"article"`
	Sessions []AskFixtureSession `json:"sessions"`
}

// LoadAskFixture 解码夹具。
func LoadAskFixture() (AskFixture, error) {
	var f AskFixture
	if err := json.Unmarshal(askFixtureJSON, &f); err != nil {
		return AskFixture{}, fmt.Errorf("解码 AI 问答夹具失败：%w", err)
	}
	return f, nil
}
