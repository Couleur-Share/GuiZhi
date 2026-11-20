package services

import (
	"encoding/json"
	"errors"
)

// 条目操作（渲染层 knowledge.store 的收藏、置顶、标签、状态、回收站与批量，library-workflow.store 的批量执行）的数据形状。
// 对应的 IPC 频道：knowledge:update、knowledge:selection（freeze / execute）、web:snapshot-enqueue、knowledge:get、export:aiHandoff。

// 批量命令的种类（packages/shared 的 KnowledgeBatchAction.kind）。
const (
	LibraryBatchUpdate  = "update"  // 改字段：收藏、合集、追加标签
	LibraryBatchStatus  = "status"  // 改状态：归档与取消归档
	LibraryBatchTrash   = "trash"   // 移到回收站
	LibraryBatchRestore = "restore" // 从回收站恢复
	LibraryBatchDelete  = "delete"  // 彻底删除（只对回收站里的条目）
)

// LibraryEntryPatch 是对条目字段的部分修改（packages/shared 的 KnowledgeItemUpdate 与 KnowledgeBulkUpdate 里界面用到的部分）：
// nil 的字段不改。序列化成处理器认的 JSON：只写出设置了的字段。
type LibraryEntryPatch struct {
	IsFavorite *bool
	IsPinned   *bool
	// CollectionID 非 nil 表示改合集；指向空串表示移出合集（JSON 里是 null，渲染层的 `collectionId: null`）。
	CollectionID *string
	// TagNames 整体替换条目的标签；nil 表示不改，非 nil 的空切片表示清空。
	TagNames []string
	// AddTagNames 在原有标签之外追加（批量打标签）；不存在的标签会被新建。
	AddTagNames []string
	// ReviewStatus 改复核状态（LibraryReview*）；ReviewReasons 同时改原因：nil 表示不改，非 nil 的空切片表示清空。
	// 只有详情里的「标记已复核」「采用来源版本」用到，批量命令不带。
	ReviewStatus  *string
	ReviewReasons []string
}

// MarshalJSON 只写出设置了的字段。
func (p LibraryEntryPatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if p.IsFavorite != nil {
		m["isFavorite"] = *p.IsFavorite
	}
	if p.IsPinned != nil {
		m["isPinned"] = *p.IsPinned
	}
	if p.CollectionID != nil {
		if *p.CollectionID == "" {
			m["collectionId"] = nil
		} else {
			m["collectionId"] = *p.CollectionID
		}
	}
	if p.TagNames != nil {
		m["tagNames"] = p.TagNames
	}
	if p.AddTagNames != nil {
		m["addTagNames"] = p.AddTagNames
	}
	if p.ReviewStatus != nil {
		m["reviewStatus"] = *p.ReviewStatus
	}
	if p.ReviewReasons != nil {
		m["reviewReasons"] = p.ReviewReasons
	}
	return json.Marshal(m)
}

// BoolPtr 与 StringPtr 是写补丁时取字面量地址的小帮手。
func BoolPtr(v bool) *bool       { return &v }
func StringPtr(v string) *string { return &v }

// LibraryBatchCommand 是一条批量命令（KnowledgeBatchAction）：同一批次里每条条目执行同一个命令。
type LibraryBatchCommand struct {
	Kind string `json:"kind"`
	// Patch 只在 Kind 为 update 时用。
	Patch *LibraryEntryPatch `json:"patch,omitempty"`
	// Status 只在 Kind 为 status 时用（active、archived）。
	Status string `json:"status,omitempty"`
	// ClearEvidence 只在 Kind 为 delete 时用：同时清掉历史问答里对这些条目的证据。
	ClearEvidence bool `json:"clearEvidence,omitempty"`
}

// LibraryBatchResult 是批量里一条的结果（KnowledgeBatchResult）：逐条记账，一条失败不影响其余。
type LibraryBatchResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// LibraryHandoff 是一份「AI 交接稿」（packages/shared 的 AiHandoffResult 加上条目标题）：条目整理成可以直接粘贴给 AI 的文本。
type LibraryHandoff struct {
	Title, Text string
	// CharCount 是交接稿的字数；OmittedChars 是精简版略去的长文本字数（完整版为 0）。
	CharCount, OmittedChars int
}

// LibraryExportResult 是另存交接稿的结果（export:aiHandoff）：用户取消不算失败。
type LibraryExportResult struct {
	Success  bool
	Canceled bool
	FilePath string
	Error    string
}

// 取交接稿时的两种业务失败：界面按它们换成本地化的提示，其余错误原样展示。
var (
	// ErrLibraryItemMissing 表示条目已不存在。
	ErrLibraryItemMissing = errors.New("条目不存在")
	// ErrHandoffEmpty 表示条目既没有正文也没有文字稿，没有可交给 AI 的素材。
	ErrHandoffEmpty = errors.New("条目内容为空，没有可交给 AI 的素材")
)
