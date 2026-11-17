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

// ImportStageStat 是单个阶段的实际开销（渲染层 ImportStageStat）：任务跑完后留在行上，回答「这次为什么这么久」。
type ImportStageStat struct {
	Stage            string   `json:"stage"`
	Ms               int64    `json:"ms"` // 该阶段累计耗时（毫秒）
	Calls            int      `json:"calls"`
	FailedCalls      int      `json:"failedCalls"`
	PromptTokens     int64    `json:"promptTokens"`
	CompletionTokens int64    `json:"completionTokens"`
	Models           []string `json:"models"`
}

// ImportTask 是导入任务的一行（import:list 返回的 entries 元素）。
// 字段与渲染层 packages/shared 的 ImportTask 同名同义；可空字段用空串/零值表示缺省。
type ImportTask struct {
	ID              string            `json:"id"`
	Origin          string            `json:"origin"`
	SourceKind      string            `json:"sourceKind"` // url | file | text
	SourceInput     string            `json:"sourceInput"`
	DisplayName     string            `json:"displayName"`
	Status          string            `json:"status"`
	Stage           string            `json:"stage"`
	Error           string            `json:"error"`
	Warning         string            `json:"warning"`
	ItemType        string            `json:"itemType"`
	ResultItemID    string            `json:"resultItemId"`
	DuplicateItemID string            `json:"duplicateItemId"`
	CollectionID    string            `json:"collectionId"`
	RefreshOfItemID string            `json:"refreshOfItemId"`
	TagNames        []string          `json:"tagNames"`
	CaptureStrategy string            `json:"captureStrategy"`
	CommentLimit    int               `json:"commentLimit"`
	StageStats      []ImportStageStat `json:"stageStats"`
	ReceivedAt      int64             `json:"receivedAt"`
	CreatedAt       int64             `json:"createdAt"`
	UpdatedAt       int64             `json:"updatedAt"`
}

// ImportQueueState 是调度器状态（渲染层 ImportQueueState）：暂停不改变 pending 任务的持久化状态。
type ImportQueueState struct {
	Paused       bool `json:"paused"`
	RunningCount int  `json:"runningCount"`
	PendingCount int  `json:"pendingCount"`
	Concurrency  int  `json:"concurrency"`
}

// ImportClearQuery 对应 import:clearTerminal / clearTerminalPreview 的参数：清理哪个范围内的终态任务。
type ImportClearQuery struct {
	Scope  string `json:"scope"` // filtered | all
	Origin string `json:"origin"`
	Status string `json:"status"`
	Query  string `json:"query"`
}

// ImportRetryOptions 对应 import:retry 的可选参数；零值表示不传。
type ImportRetryOptions struct {
	VerifyWeb       bool   `json:"verifyWeb,omitempty"`
	ForceDuplicate  bool   `json:"forceDuplicate,omitempty"`
	CaptureStrategy string `json:"captureStrategy,omitempty"`
	CommentLimit    int    `json:"commentLimit,omitempty"`
}

// EnqueueImportInput 是入队的一项（渲染层 EnqueueImportInput）。
type EnqueueImportInput struct {
	Kind            string   `json:"kind"` // text | file | url
	Input           string   `json:"input"`
	CollectionID    *string  `json:"collectionId"`
	RefreshOfItemID string   `json:"refreshOfItemId,omitempty"`
	TagNames        []string `json:"tagNames,omitempty"`
	ForceDuplicate  bool     `json:"forceDuplicate,omitempty"`
	CaptureStrategy string   `json:"captureStrategy,omitempty"`
	CommentLimit    int      `json:"commentLimit,omitempty"`
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
	// OnTaskChange 登记「某个任务变了」的回调（import:changed 的推送），带变化后的任务快照；可能在任意 goroutine 触发。
	OnTaskChange(fn func(ImportTask)) (cancel func())

	// QueueState 读调度器状态（import:queueState）。
	QueueState(ctx context.Context) (ImportQueueState, error)
	// SetPaused 暂停或继续队列，返回新的状态（import:pause / import:resume）。
	SetPaused(ctx context.Context, paused bool) (ImportQueueState, error)
	// Cancel 取消一个排队或处理中的任务（import:cancel）。
	Cancel(ctx context.Context, id string) error
	// Retry 重试一个失败、取消或重复的任务（import:retry）；found 为假表示任务已不存在。
	Retry(ctx context.Context, id string, opts ImportRetryOptions) (found bool, err error)
	// Remove 从列表删除一个已结束的任务记录（import:remove）；不删除已入库的知识条目。
	Remove(ctx context.Context, id string) error
	// PreviewClearTerminal 数出清理范围内会被删除的终态任务数（import:clearTerminalPreview）。
	PreviewClearTerminal(ctx context.Context, q ImportClearQuery) (int, error)
	// ClearTerminal 删除清理范围内的终态任务，返回删除的条数（import:clearTerminal）。
	ClearTerminal(ctx context.Context, q ImportClearQuery) (int, error)
	// Enqueue 把一批输入加入采集队列（import:enqueue）。
	Enqueue(ctx context.Context, inputs []EnqueueImportInput) ([]ImportTask, error)
	// SelectFiles 弹出系统文件选择框（dialog:selectImportFiles）；取消返回空列表。
	SelectFiles(ctx context.Context) ([]string, error)
	// ItemFacts 读一个知识条目的事实（knowledge:get 之后只取标题与几个数字）：任务详情的成果卡与来源刷新对照用。
	// 条目不存在返回 found=false；读取失败返回携带原因的 error（详情弹窗不因此出错，只记日志）。
	ItemFacts(ctx context.Context, id string) (facts ImportItemFacts, found bool, err error)
}
