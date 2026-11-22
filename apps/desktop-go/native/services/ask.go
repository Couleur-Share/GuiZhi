package services

// M3-4：AI 问答（全局问答工作区、围绕本文提问）的数据形状与窄接口。
//
// 行为来源只有一个：渲染层 preload/api/ask.ts（会话持久化）、stores/ask.store.ts、stores/article-ask.store.ts
// 与 services/knowledge-ai 的问答流程；这里的 DTO 与那几处的 JSON 形状一一对应——
// 会话的 messagesJson 沿用同一格式，渲染层写下的旧会话在 Viva 里原样可读，反过来也一样。
//
// 问答引擎（检索循环、混合检索、流式回答、证据快照）在 aiflow，主包适配器（native_ask.go）把它们与现有 IPC 处理器接起来；
// 状态机（会话切换、保存重试、取消、过时请求丢弃）在 viva/screens/ask，不在这里。

import (
	"context"
	"errors"

	"guizhi-mygo-shell/aiflow"
)

// 会话范围（AskSessionOptions.scope）：空串按「knowledge」（全局问答）处理。
const (
	AskScopeKnowledge = "knowledge"
	AskScopeArticle   = "article"
)

// 一轮问答的状态（AskMessage.status）。
const (
	AskStatusRunning = "running"
	AskStatusDone    = "done"
	AskStatusError   = "error"
)

// 全局问答一轮失败的归类（AskMessage.errorKind；「已停止」按 generic 存，带 error 文案）。
const (
	AskErrorNotConfigured = "not-configured"
	AskErrorNoSource      = "no-source"
	AskErrorGeneric       = "generic"
)

// ErrAskNoSource 表示知识库里没有相关资料（对应 QaNoSourceError；区别于调用失败，界面显示引导文案）。
var ErrAskNoSource = errors.New("QA_NO_RELEVANT_SOURCE")

// AskSource 是回答引用的一份资料（QaSourceRef）。
// SourceID 与 Cleared 是证据清除之后留下的标记（sanitizeEvidenceJson 写入）：往返时不能丢，否则「来源已清除」的提示会消失。
type AskSource struct {
	Ordinal  int                      `json:"ordinal"`
	Kind     string                   `json:"kind"` // item | wiki
	RefID    string                   `json:"refId"`
	Title    string                   `json:"title"`
	SourceID string                   `json:"sourceId,omitempty"`
	Cleared  bool                     `json:"cleared,omitempty"`
	Evidence *aiflow.EvidenceSnapshot `json:"evidence,omitempty"`
}

// AskMessage 是全局问答的一轮（AskMessage，会话 messagesJson 的元素）。
type AskMessage struct {
	ID              string      `json:"id"`
	Question        string      `json:"question"`
	Answer          string      `json:"answer"`
	Sources         []AskSource `json:"sources"`
	EvidenceSources []AskSource `json:"evidenceSources,omitempty"`
	Warnings        []string    `json:"warnings,omitempty"`
	Steps           []string    `json:"steps"`
	Status          string      `json:"status"`
	Error           string      `json:"error,omitempty"`
	ErrorKind       string      `json:"errorKind,omitempty"`
	// Stopped 标记这一轮是被用户点「停止」中断的（errorKind 仍按 generic 存、Error 带「已停止」文案，与渲染层读写互通；
	// 渲染层读到这个多出来的字段会忽略，Viva 读回时据此把它归为「已停止」而不是普通失败）。
	Stopped      bool   `json:"stopped,omitempty"`
	Model        string `json:"model,omitempty"`
	UsedFallback bool   `json:"usedFallback,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
}

// ---- 会话持久化（preload/api/ask.ts） ----

// AskSessionMeta 是会话列表里的一条（AskSessionMeta）。围绕本文的会话带 Scope、ItemID、ArticleTitle、WebEnabled 与 Target。
type AskSessionMeta struct {
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	Pinned       bool                  `json:"pinned,omitempty"`
	CreatedAt    int64                 `json:"createdAt"`
	UpdatedAt    int64                 `json:"updatedAt"`
	Scope        string                `json:"scope,omitempty"`
	ItemID       string                `json:"itemId,omitempty"`
	ArticleTitle string                `json:"articleTitle,omitempty"`
	WebEnabled   *bool                 `json:"webEnabled,omitempty"`
	Target       *aiflow.ArticleTarget `json:"target,omitempty"`
}

// IsArticle 报告会话是否围绕某篇文章。
func (m AskSessionMeta) IsArticle() bool { return m.Scope == AskScopeArticle }

// WebEnabledOrDefault 对应 `record.webEnabled !== false`：没有记录过联网开关的会话按已开启处理。
func (m AskSessionMeta) WebEnabledOrDefault() bool { return m.WebEnabled == nil || *m.WebEnabled }

// AskSessionRecord 是读出的完整会话（带消息体 JSON）。
type AskSessionRecord struct {
	AskSessionMeta
	MessagesJSON string `json:"messagesJson"`
}

// SaveAskSessionInput 是保存会话的入参（SaveAskSessionInput）。
type SaveAskSessionInput struct {
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	MessagesJSON string                `json:"messagesJson"`
	Scope        string                `json:"scope,omitempty"`
	ItemID       string                `json:"itemId,omitempty"`
	ArticleTitle string                `json:"articleTitle,omitempty"`
	WebEnabled   *bool                 `json:"webEnabled,omitempty"`
	Target       *aiflow.ArticleTarget `json:"target,omitempty"`
}

// AskSessionFilter 是 ask:listSessions 的筛选（范围与文章）。
type AskSessionFilter struct {
	Scope  string `json:"scope,omitempty"`
	ItemID string `json:"itemId,omitempty"`
}

// AskSessionQuery 是分页查询的入参（AskSessionQuery）：Search 走全文索引，From / To 是更新时间的毫秒区间，Cursor 来自上一页。
// 可缺省的字段用指针 / 空值表示「未传」，处理器按 undefined 与 null 区分处理。
type AskSessionQuery struct {
	Scope  string  `json:"scope,omitempty"`
	ItemID string  `json:"itemId,omitempty"`
	Search string  `json:"search,omitempty"`
	From   *int64  `json:"from,omitempty"`
	To     *int64  `json:"to,omitempty"`
	Cursor *string `json:"cursor,omitempty"`
	Limit  int     `json:"limit,omitempty"`
}

// AskArticleRef 是「围绕本文」筛选下拉的一项（每篇文章取最近一次会话的标题）。
type AskArticleRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// AskSessionPage 是一页会话；Articles 只在第一页（没有游标）里给出。
type AskSessionPage struct {
	Entries    []AskSessionMeta `json:"entries"`
	NextCursor *string          `json:"nextCursor"`
	Articles   []AskArticleRef  `json:"articles,omitempty"`
}

// AskSessionMetaPatch 是改标题与置顶的局部补丁；nil 的字段不改。
type AskSessionMetaPatch struct {
	Title  *string `json:"title,omitempty"`
	Pinned *bool   `json:"pinned,omitempty"`
}

// AskItem 是问答界面关心的条目事实：本文问答的标题、「返回文章」前要核对条目没进回收站。
type AskItem struct {
	Title   string
	Deleted bool
}

// ---- 全局问答 ----

// AskTurn 是已完成的一轮问答，作为多轮上下文传入（QaTurn）。
type AskTurn struct {
	Question string
	Answer   string
}

// AskRequest 是一次全局问答的入参。
type AskRequest struct {
	Question string
	History  []AskTurn
}

// AskCallbacks 是问答过程中的回调，全部可空。回调可能在后台 goroutine 里被调用；
// 取消之后仍可能收到这一轮剩余的少量回调（与渲染层一致），调用方要自己丢弃过时的更新。
type AskCallbacks struct {
	// OnStep 报告进度（检索词、阅读哪份资料）。
	OnStep func(step string)
	// OnText 在回答逐字到达时回调「到目前为止的全文」，直接覆盖显示即可。
	OnText func(text string)
	// OnEvidence 在每次阅读之后回调这一刻的全部来源（含证据快照）。
	OnEvidence func(sources []AskSource)
	// OnWarning 报告一句给用户看的降级提示（语义检索不可用等）；同一句话只回调一次。
	OnWarning func(message string)
}

// AskAnswer 是一次全局问答的最终结果。
type AskAnswer struct {
	Text    string
	Sources []AskSource
	Model   string
	// UsedFallback 为 true 表示走了兜底管线（Agent 协议失败）。
	UsedFallback bool
	// Truncated 为 true 表示回答撞上 max_tokens 被截断，话没说完。
	Truncated bool
}

// ArticleAskRequest 是一次围绕本文提问的入参。
type ArticleAskRequest struct {
	Target     aiflow.ArticleTarget
	Question   string
	History    []aiflow.ArticleMessage
	WebEnabled bool
	// RequestID 标识这一轮的联网搜索，取消时据此通知后端。
	RequestID string
	// Context 非 nil 表示重试：沿用保存的原始上下文，只核验文章仍存在。
	Context *aiflow.ArticleContext
}

// Ask 是 AI 问答的数据层。
//
// 错误约定：模型未配置返回 ErrAINotConfigured；知识库没有相关资料返回 ErrAskNoSource；
// ctx 被取消（用户点了「停止」）返回 context.Canceled（errors.Is 成立）；其余是带原因的普通错误。
// 返回的错误文本不含 API Key。
type Ask interface {
	// QuerySessions 对应 askSession:query（分页、筛选、全文搜索）。
	QuerySessions(ctx context.Context, q AskSessionQuery) (AskSessionPage, error)
	// ListSessions 对应 ask:listSessions（最近 100 条，可按范围与文章筛选）。
	ListSessions(ctx context.Context, f AskSessionFilter) ([]AskSessionMeta, error)
	// GetSession 对应 ask:getSession；会话不存在返回 nil。
	GetSession(ctx context.Context, id string) (*AskSessionRecord, error)
	// SaveSession 对应 ask:saveSession；后端会按证据清除记录对证据脱敏，返回的是落库后的记录。
	SaveSession(ctx context.Context, in SaveAskSessionInput) (AskSessionRecord, error)
	// DeleteSession 对应 ask:deleteSession；返回是否真的删掉了一条。
	DeleteSession(ctx context.Context, id string) (bool, error)
	// UpdateSessionMeta 对应 askSession:meta。
	UpdateSessionMeta(ctx context.Context, id string, patch AskSessionMetaPatch) (AskSessionRecord, error)

	// QAConfigured 对应 isScenarioConfigured("qa")：问答场景是否已有可用模型。
	QAConfigured(ctx context.Context) (bool, error)
	// WebSearchConfigured 对应 themedReading.searchConfig() 的 search.configured：联网搜索是否已配置。
	WebSearchConfigured(ctx context.Context) (bool, error)
	// ItemInfo 对应 knowledge.get(id) 里问答用到的字段：条目不存在返回 found=false；已在回收站的条目仍能读到，Deleted 为 true。
	ItemInfo(ctx context.Context, itemID string) (item AskItem, found bool, err error)

	// AskKnowledge 执行一次全局问答（askKnowledgeBase）：阻塞到回答结束或取消。
	AskKnowledge(ctx context.Context, req AskRequest, cb AskCallbacks) (AskAnswer, error)
	// AskArticle 执行一次围绕本文的问答（askArticle）：进度经 patch 增量交出，成功返回 nil。
	AskArticle(ctx context.Context, req ArticleAskRequest, patch func(aiflow.ArticlePatch)) error

	// SanitizeConversation 对应 sanitizeEvidenceJson：按已清除的条目 ID 给会话消息 JSON 里的证据脱敏（正文与链接清空、标记已清除，回答文本原样）。
	// 与保存时后端用的是同一份实现，所以内存里的缓存与落库内容一致。
	SanitizeConversation(messagesJSON string, clearedItemIDs []string) (string, error)
}
