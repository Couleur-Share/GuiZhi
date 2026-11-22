package services

import (
	"context"
	"encoding/json"
)

// M3-1 处理中心（渲染层 inbox.store.ts、InboxWorkspace.tsx、SidebarInboxPanel.tsx）的数据形状与能力。
// DTO 与 packages/shared/types/inbox.ts 同名同义；主包适配器经现有 IPC 处理器取数（native_inbox.go），
// 所以字段的 JSON 名就是处理器输出里的名字。

// 处理中心的分组取值（InboxItemKind）。
const (
	InboxKindReviewRequired     = "review-required"
	InboxKindUnclassified       = "unclassified"
	InboxKindImportIssue        = "import-issue"
	InboxKindDiscoveryCandidate = "discovery-candidate"
	InboxKindSemanticPending    = "semantic-pending"
	InboxKindWikiPending        = "wiki-pending"
)

// InboxItem 是处理中心的一行（渲染层 InboxItem 的并集类型摊平成一个结构）：不同 Kind 只用到其中一部分字段。
//
//   - review-required：ItemID、Title、Reasons；
//   - unclassified：ItemID、Title；
//   - import-issue：TaskID、Title、Status（failed / duplicate / completed）、Message、ResultItemID、DuplicateItemID；
//   - discovery-candidate：ViewID、ExternalID、Title（处理器目前不产出）；
//   - semantic-pending / wiki-pending：Count（聚合行，不对应某个条目）。
//
// 可空字段（渲染层的 string | null）用空串表示缺省。
type InboxItem struct {
	Kind            string   `json:"kind"`
	ID              string   `json:"id"`
	ItemID          string   `json:"itemId"`
	Title           string   `json:"title"`
	Reasons         []string `json:"reasons"`
	TaskID          string   `json:"taskId"`
	Status          string   `json:"status"`
	Message         string   `json:"message"`
	ResultItemID    string   `json:"resultItemId"`
	DuplicateItemID string   `json:"duplicateItemId"`
	ViewID          string   `json:"viewId"`
	ExternalID      string   `json:"externalId"`
	Count           int      `json:"count"`
	CreatedAt       int64    `json:"createdAt"`
}

// IsAggregate 报告这一行是不是聚合行（语义索引、Wiki 编译）：聚合行没有条目 ID，不能勾选。
func (i InboxItem) IsAggregate() bool {
	return i.Kind == InboxKindSemanticPending || i.Kind == InboxKindWikiPending
}

// KnowledgeItemID 返回行对应的知识条目 ID（渲染层 knowledgeItemId）：只有复核与未归合集两类行带它。
func (i InboxItem) KnowledgeItemID() string {
	if i.Kind == InboxKindReviewRequired || i.Kind == InboxKindUnclassified {
		return i.ItemID
	}
	return ""
}

// InboxCounts 是各分组涉及的数量（渲染层 Record<InboxItemKind, number>）；后台任务按内容条数统计。
type InboxCounts struct {
	ReviewRequired     int `json:"review-required"`
	Unclassified       int `json:"unclassified"`
	ImportIssue        int `json:"import-issue"`
	DiscoveryCandidate int `json:"discovery-candidate"`
	SemanticPending    int `json:"semantic-pending"`
	WikiPending        int `json:"wiki-pending"`
}

// Of 返回某个分组的数量；未知分组返回 0。
func (c InboxCounts) Of(kind string) int {
	switch kind {
	case InboxKindReviewRequired:
		return c.ReviewRequired
	case InboxKindUnclassified:
		return c.Unclassified
	case InboxKindImportIssue:
		return c.ImportIssue
	case InboxKindDiscoveryCandidate:
		return c.DiscoveryCandidate
	case InboxKindSemanticPending:
		return c.SemanticPending
	case InboxKindWikiPending:
		return c.WikiPending
	}
	return 0
}

// With 返回把某个分组的数量换成 n 的副本；未知分组原样返回。
func (c InboxCounts) With(kind string, n int) InboxCounts {
	switch kind {
	case InboxKindReviewRequired:
		c.ReviewRequired = n
	case InboxKindUnclassified:
		c.Unclassified = n
	case InboxKindImportIssue:
		c.ImportIssue = n
	case InboxKindDiscoveryCandidate:
		c.DiscoveryCandidate = n
	case InboxKindSemanticPending:
		c.SemanticPending = n
	case InboxKindWikiPending:
		c.WikiPending = n
	}
	return c
}

// InboxList 是 inbox:list 的结果（InboxListResult）。
type InboxList struct {
	Items  []InboxItem `json:"items"`
	Counts InboxCounts `json:"counts"`
	// Total 是需人工处理的内容去重总数，不含 Wiki 编译与语义索引。
	Total int `json:"total"`
}

// InboxWikiCandidates 是 Wiki 编译预览里各状态的候选条数（wikiPreview().counts 里处理中心用到的四项）。
type InboxWikiCandidates struct {
	Ready   int `json:"ready"`
	Review  int `json:"review"`
	Waiting int `json:"waiting"`
	Upgrade int `json:"upgrade"`
}

// InboxSemanticStatus 是语义索引的进度（semantic:status）。Configured 为假表示没有配置 embedding 模型：
// 渲染层此时不查状态、待索引数按 0 算（语义检索是可选增强，不是故障）。
type InboxSemanticStatus struct {
	Configured    bool `json:"configured"`
	EligibleItems int  `json:"eligibleItems"`
	IndexedItems  int  `json:"indexedItems"`
}

// Pending 是待建立索引的内容条数（渲染层 max(0, eligibleItems - indexedItems)）；没有配置模型时是 0。
func (s InboxSemanticStatus) Pending() int {
	if !s.Configured {
		return 0
	}
	return max(0, s.EligibleItems-s.IndexedItems)
}

// InboxOrganizeInput 是批量整理的入参（InboxOrganizeInput）：CollectionID 空串表示不改合集，AddTagNames 为空表示不加标签。
type InboxOrganizeInput struct {
	ItemIDs      []string
	CollectionID string
	AddTagNames  []string
}

// MarshalJSON 只写出有值的可选字段，与渲染层 `...(collectionId ? { collectionId } : {})` 一致。
func (in InboxOrganizeInput) MarshalJSON() ([]byte, error) {
	ids := in.ItemIDs
	if ids == nil {
		ids = []string{}
	}
	m := map[string]any{"itemIds": ids}
	if in.CollectionID != "" {
		m["collectionId"] = in.CollectionID
	}
	if len(in.AddTagNames) > 0 {
		m["addTagNames"] = in.AddTagNames
	}
	return json.Marshal(m)
}

// InboxAISource 是已选未分类条目的有界摘要（InboxAiClassificationSource）：主进程只向 AI 暴露这些。
type InboxAISource struct {
	ItemID  string `json:"itemId"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}

// InboxAIAssignment 是一条分类结果：条目归入哪个合集（现有的，或将新建的）。
type InboxAIAssignment struct {
	ItemID         string `json:"itemId"`
	CollectionName string `json:"collectionName"`
}

// InboxAIApplyResult 是应用分类计划的结果（InboxAiClassificationApplyResult）。
type InboxAIApplyResult struct {
	Classified             int      `json:"classified"`
	Skipped                int      `json:"skipped"`
	CreatedCollectionNames []string `json:"createdCollectionNames"`
}

// 一轮语义索引的回执类别（渲染层 SemanticRunNotice.kind）。
const (
	InboxSemanticDone          = "done"
	InboxSemanticPartial       = "partial"
	InboxSemanticFailed        = "failed"
	InboxSemanticNothing       = "nothing"
	InboxSemanticNotConfigured = "not-configured"
)

// InboxSemanticNotice 是手动触发的一轮语义索引的回执。Message 是失败原因，空串表示没有。
type InboxSemanticNotice struct {
	Kind    string
	Indexed int
	Failed  int
	Message string
}

// Inbox 是处理中心依赖的能力。读取失败返回携带原因的 error（界面区分「主列表失败」与「Wiki / 语义状态失败」，不渲染成空态）。
type Inbox interface {
	// List 读取需要处理的条目、各分组计数与去重总数（inbox:list）。
	List(ctx context.Context) (InboxList, error)
	// WikiCandidates 读取 Wiki 编译候选的各状态条数（wiki:compiler 的 preview）。
	WikiCandidates(ctx context.Context) (InboxWikiCandidates, error)
	// WikiModelReady 报告 Wiki 场景是否已有可用的文本模型（渲染层 isScenarioConfigured("wiki")）。
	WikiModelReady(ctx context.Context) (bool, error)
	// SemanticStatus 读取语义索引进度；没有配置 embedding 模型时 Configured 为假、不是错误（semantic:status）。
	SemanticStatus(ctx context.Context) (InboxSemanticStatus, error)

	// Organize 把条目归入合集和 / 或加标签，返回实际改动的条数（inbox:organize）。
	Organize(ctx context.Context, in InboxOrganizeInput) (changed int, err error)
	// MarkReviewed 把条目标为已复核，返回实际改动的条数（inbox:markReviewed）。
	MarkReviewed(ctx context.Context, itemIDs []string) (changed int, err error)
	// AcknowledgeImportWarning 不再提醒某个采集任务的警告（保留采集记录），返回 1 表示这次处理了、0 表示早已处理过
	// （inbox:acknowledgeImportWarning）。
	AcknowledgeImportWarning(ctx context.Context, taskID string) (changed int, err error)
	// RetryImportTask 重试一个失败的采集任务（import:retry）；found 为假表示任务已不存在。
	RetryImportTask(ctx context.Context, taskID string) (found bool, err error)

	// AIClassificationSources 取已选条目里仍然符合归类条件（未分类、无需复核）的有界摘要（inbox:aiClassificationSources）。
	AIClassificationSources(ctx context.Context, itemIDs []string) ([]InboxAISource, error)
	// Classify 分批请求 AI，按现有合集给出与 sources 同序的分类计划；每批完成后回调进度（已完成批数、总批数，可为 nil）。
	// 取消走 ctx（返回的错误满足 errors.Is(err, context.Canceled)）；模型未配置返回 ErrAINotConfigured。
	Classify(ctx context.Context, sources []InboxAISource, existingCollectionNames []string, onProgress func(completed, total int)) ([]InboxAIAssignment, error)
	// ApplyAIClassification 在同一事务里新建缺失的合集并分组归入（inbox:applyAiClassification）。
	ApplyAIClassification(ctx context.Context, assignments []InboxAIAssignment) (InboxAIApplyResult, error)

	// SemanticIndexing 报告此刻是否有一轮由本服务发起的语义索引在跑（后台调度的任务将来与它共用同一把守卫时再并入）。
	SemanticIndexing() bool
	// RunSemanticIndexing 手动执行一轮语义索引并返回回执。已有一轮在跑时返回 nil（没有回执）；
	// 执行中的失败归在回执里（Kind 为 failed），error 只用于连配置都读不到之类的前置失败。
	RunSemanticIndexing(ctx context.Context) (*InboxSemanticNotice, error)
}
