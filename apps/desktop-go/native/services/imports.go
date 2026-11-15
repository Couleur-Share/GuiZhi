package services

import "context"

// 导入任务状态与过滤取值（与 packages/db 的 import-task-query 一致）。
const (
	ImportStatusPending    = "pending"
	ImportStatusProcessing = "processing"
	ImportStatusCompleted  = "completed"
	ImportStatusFailed     = "failed"
	ImportStatusCanceled   = "canceled"
	ImportStatusDuplicate  = "duplicate"

	// ImportFilterAll / ImportFilterActive 等是列表过滤，不是任务状态：active = pending + processing。
	ImportFilterAll      = "all"
	ImportFilterActive   = "active"
	ImportFilterDegraded = "degraded"
)

// ImportQuery 对应 import:list 的查询参数。空串取默认（Origin=all、Status=all）。
type ImportQuery struct {
	Origin   string // all | mobile | desktop
	Status   string // 过滤或状态，见上面的常量
	Query    string
	PageSize int
	Cursor   string
}

// ImportTask 是导入任务的一行（import:list 返回的 entries 元素）。
// 字段与渲染层 packages/shared 的 ImportTask 同名同义；可空字段用空串/零值表示缺省。
type ImportTask struct {
	ID              string `json:"id"`
	Origin          string `json:"origin"`
	SourceKind      string `json:"sourceKind"` // url | file | text
	SourceInput     string `json:"sourceInput"`
	DisplayName     string `json:"displayName"`
	Status          string `json:"status"`
	Stage           string `json:"stage"`
	Error           string `json:"error"`
	Warning         string `json:"warning"`
	ItemType        string `json:"itemType"`
	ResultItemID    string `json:"resultItemId"`
	DuplicateItemID string `json:"duplicateItemId"`
	CollectionID    string `json:"collectionId"`
	CaptureStrategy string `json:"captureStrategy"`
	ReceivedAt      int64  `json:"receivedAt"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// ImportPage 是一页导入任务及其计数。
type ImportPage struct {
	Entries       []ImportTask     `json:"entries"`
	Active        []ImportTask     `json:"active"`
	NextCursor    string           `json:"nextCursor"`
	Total         int64            `json:"total"`
	Counts        map[string]int64 `json:"counts"`
	DegradedCount int64            `json:"degradedCount"`
}

// Imports 是导入队列页依赖的能力。
type Imports interface {
	// List 查询一页导入任务；失败返回携带原因的 error（界面用 LoadErrorState 呈现，不渲染成空态）。
	List(ctx context.Context, q ImportQuery) (ImportPage, error)
	// OnChange 登记「有任务状态变化」的回调（可能在任意 goroutine 触发，须自行去抖），返回取消函数。
	OnChange(fn func()) (cancel func())
}
