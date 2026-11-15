package aiflow

// 全局问答（qa.ts）的类型与依赖接口。

import (
	"context"
	"errors"
)

// ErrQANoSource 对应 QaNoSourceError：知识库里没有相关资料（区别于调用失败，界面显示引导文案）。
var ErrQANoSource = errors.New("QA_NO_RELEVANT_SOURCE")

// QASourceKind 对应 QaSourceKind：引用资料的类别。
type QASourceKind string

const (
	QAKindItem QASourceKind = "item" // 知识条目原文
	QAKindWiki QASourceKind = "wiki" // AI 编译的 Wiki 页面
)

// QASourceRef 对应 QaSourceRef：回答引用的一份资料（Evidence 是读取时留下的证据快照）。
type QASourceRef struct {
	Evidence *EvidenceSnapshot `json:"evidence,omitempty"`
	Ordinal  int               `json:"ordinal"`
	Kind     QASourceKind      `json:"kind"`
	// RefID 是条目 id 或 Wiki 页面 id（按 Kind 区分）。
	RefID string `json:"refId"`
	Title string `json:"title"`
}

// QAAnswer 对应 QaAnswer。
type QAAnswer struct {
	Text    string
	Sources []QASourceRef
	Model   string
	// UsedFallback 为 true 表示走了兜底管线（Agent 协议失败）。
	UsedFallback bool
	// Truncated 为 true 表示回答撞上 max_tokens 被截断，话没说完。
	Truncated bool
}

// QAItemContent 对应 QaItemContent：问答读到的条目全文。
type QAItemContent struct {
	ReviewStatus  string   // 空串表示未设置
	ReviewReasons []string // nil 表示未设置
	UpdatedAt     *int64
	Title         string
	Content       string
	Transcript    *string // nil 对应 null / undefined
}

// QADeps 对应 QaDeps：问答引擎的全部依赖。可缺省的成员与 TS 的可选成员一一对应——
// WikiCatalog 缺省时循环退化为纯条目检索，SearchWikiPages 缺省时退回目录的内存子串计分。
type QADeps struct {
	OnEvidence func(sources []QASourceRef)
	// Chat 是已绑定 qa 场景配置的对话调用；ctx 必须透传到底层请求，否则「停止」只能等这一轮自然返回。
	Chat func(ctx context.Context, messages []ChatMessage, opts ScenarioChatOptions) (ScenarioChatResult, error)
	// SearchItems 是知识库全文检索（含归档，排除回收站）。
	SearchItems func(ctx context.Context, query string, limit int) ([]SearchHit, error)
	// ReadItem 读取条目全文；条目不存在返回 nil。
	ReadItem func(ctx context.Context, id string) (*QAItemContent, error)
	// WikiCatalog 是 Wiki 目录（出链解析用）。
	WikiCatalog func(ctx context.Context) ([]WikiCatalogEntry, error)
	// SearchWikiPages 是 Wiki 页面全文检索。
	//
	// 没有它就只能退回内存子串计分，而那套对中文基本失效：
	// 中文查询切不出 token，只有「问句里逐字包含页面标题」才加得上分。
	SearchWikiPages func(ctx context.Context, query string, limit int) ([]WikiSearchHit, error)
	// ReadWikiPage 读取页面详情（含来源条目）；页面不存在返回 nil。
	ReadWikiPage func(ctx context.Context, id string) (*WikiPageDetail, error)
}
