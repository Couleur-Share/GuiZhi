package aiflow

// 本文问答的数据类型（对应 packages/shared/types/article-ask.ts）。
//
// JSON 标签与 TS 字段名一致：会话记录（messagesJson）沿用同一格式，切换前后的旧会话不用迁移。
// 可缺省的字段用指针 / nil 切片表示「未设置」（TS 的 undefined），与「空串」「空数组」区分开。

// ArticleTarget 对应 ArticleTarget：来源描述只含标识和摘录，不携带网络凭证。
type ArticleTarget struct {
	ItemID     string  `json:"itemId"`
	View       string  `json:"view"` // body | summary | transcript | recognized | replies | snapshot | themed
	SourceKind *string `json:"sourceKind,omitempty"`
	VersionID  *string `json:"versionId,omitempty"`
	Selection  *string `json:"selection,omitempty"`
	Floor      *int64  `json:"floor,omitempty"`
}

// ArticleSource 对应 ArticleSource：回答时提供给模型的一份资料。
// 字段顺序即序列化顺序，也是提示词里资料对象的键序（与主包 articleSource 的 JSON 顺序一致）。
type ArticleSource struct {
	Ordinal     int               `json:"ordinal"`
	Kind        string            `json:"kind"` // article | web
	Title       string            `json:"title"`
	Text        string            `json:"text"`
	URL         *string           `json:"url,omitempty"`
	Target      *ArticleTarget    `json:"target,omitempty"`
	Fingerprint *string           `json:"fingerprint,omitempty"`
	CapturedAt  *int64            `json:"capturedAt,omitempty"`
	Evidence    *EvidenceSnapshot `json:"evidence,omitempty"`
	Cleared     *bool             `json:"cleared,omitempty"`
}

// ArticleContext 对应 ArticleContext：本轮问答读到的本文上下文。
type ArticleContext struct {
	Target      ArticleTarget   `json:"target"`
	Title       string          `json:"title"`
	Fingerprint string          `json:"fingerprint"`
	Sources     []ArticleSource `json:"sources"`
	Clipped     bool            `json:"clipped"`
}

// ArticleMessage 对应 ArticleMessage：一轮问答的记录（会话 messagesJson 的元素）。
type ArticleMessage struct {
	ID         string          `json:"id"`
	Question   string          `json:"question"`
	Answer     string          `json:"answer"`
	Status     string          `json:"status"` // running | done | error
	Context    *ArticleContext `json:"context,omitempty"`
	Sources    []ArticleSource `json:"sources"`
	WebEnabled bool            `json:"webEnabled"`
	WebStatus  string          `json:"webStatus"` // off | searching | ready | partial | failed
	Warnings   []string        `json:"warnings"`
	Step       *string         `json:"step,omitempty"`
	Error      *string         `json:"error,omitempty"`
	Model      *string         `json:"model,omitempty"`
	Truncated  *bool           `json:"truncated,omitempty"`
}

// ArticlePatch 对应 Partial<ArticleMessage>：只有被设置的字段才会写进消息。
// 指针与 nil 切片表示「未设置」；ClearStep 对应 `step: undefined`（把步骤提示清掉）。
type ArticlePatch struct {
	Step      *string
	ClearStep bool
	Context   *ArticleContext
	Sources   []ArticleSource
	Warnings  []string
	WebStatus *string
	Answer    *string
	Status    *string
	Model     *string
	Truncated *bool
}

// Clone 深拷贝，对应 structuredClone：问答过程会就地改写来源（文本脱敏、补证据），不能动到调用方传入的上下文。
func (c ArticleContext) Clone() ArticleContext {
	out := c
	out.Target = c.Target.clone()
	if c.Sources != nil {
		out.Sources = make([]ArticleSource, len(c.Sources))
		for i, source := range c.Sources {
			out.Sources[i] = source.clone()
		}
	}
	return out
}

func (t ArticleTarget) clone() ArticleTarget {
	out := t
	out.SourceKind = copyPtr(t.SourceKind)
	out.VersionID = copyPtr(t.VersionID)
	out.Selection = copyPtr(t.Selection)
	out.Floor = copyPtr(t.Floor)
	return out
}

func (s ArticleSource) clone() ArticleSource {
	out := s
	out.URL = copyPtr(s.URL)
	out.Fingerprint = copyPtr(s.Fingerprint)
	out.CapturedAt = copyPtr(s.CapturedAt)
	out.Cleared = copyPtr(s.Cleared)
	if s.Target != nil {
		target := s.Target.clone()
		out.Target = &target
	}
	if s.Evidence != nil {
		evidence := s.Evidence.clone()
		out.Evidence = &evidence
	}
	return out
}

func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func ptr[T any](v T) *T { return &v }
