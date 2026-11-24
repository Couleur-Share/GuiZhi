package services

import "context"

// 采集文档站（渲染层 DocumentSitePanel）依赖的能力：网页采集批次。
//
// 对应主进程的 crawl:* 频道（渲染层 window.api.webCapture 的 create / list / get / pause / resume / retry / cancel）：
// 用户给出入口网址、目录范围、页数与深度上限，主进程按 robots 与目录边界发现页面并逐页入库；
// 界面只负责创建批次、轮询批次与页面明细、在批次上暂停 / 继续 / 重试失败项 / 取消。
// 没有「先扫描、再勾选、再批量入队」这一步——批次创建后的发现与入库都在主进程里。
//
// 适配器（package main 的 native_imports_ext.go）把它接到现有处理器；SSRF 由处理器与底层 safe_fetch 保障，
// 界面与适配层不另开网络路径。失败一律返回携带原因的 error（处理器的 { ok:false, error } 也转成 error）。

// 采集批次的状态（CrawlJobStatus）。
const (
	CrawlJobPending     = "pending"
	CrawlJobRunning     = "running"
	CrawlJobPaused      = "paused"
	CrawlJobInterrupted = "interrupted" // 上次退出时还在跑，等用户点「继续」
	CrawlJobCompleted   = "completed"
	CrawlJobCanceled    = "canceled"
)

// 批次里单个页面的状态（CrawlPageStatus）。
const (
	CrawlPagePending        = "pending"
	CrawlPageRunning        = "running"
	CrawlPageAdded          = "added"
	CrawlPageDuplicate      = "duplicate"
	CrawlPageUpdated        = "updated"
	CrawlPagePendingVersion = "pending-version"
	CrawlPageUnchanged      = "unchanged"
	CrawlPageFailed         = "failed"
	CrawlPageSkipped        = "skipped"
	CrawlPageCanceled       = "canceled"
)

// 重复处理策略（CreateCrawlJobInput.duplicatePolicy）。
const (
	CrawlDuplicateSkip   = "skip"
	CrawlDuplicateUpdate = "update"
)

// 入口模式（WebSeed.mode）：文档站采集固定用 directory。
const (
	CrawlSeedPage      = "page"
	CrawlSeedDirectory = "directory"
)

// CrawlSeed 是批次的一个入口（WebSeed）。Directory 为空表示取入口所在目录（不传给处理器）。
type CrawlSeed struct {
	URL       string `json:"url"`
	Mode      string `json:"mode"`
	Directory string `json:"directory,omitempty"`
}

// CrawlInput 是批次的输入（CreateCrawlJobInput）。Purpose 文档站固定为 documents；CollectionID 为空表示未归合集。
type CrawlInput struct {
	Purpose         string      `json:"purpose"`
	Seeds           []CrawlSeed `json:"seeds"`
	CollectionID    *string     `json:"collectionId"`
	MaxPages        int         `json:"maxPages"`
	MaxDepth        int         `json:"maxDepth"`
	DuplicatePolicy string      `json:"duplicatePolicy"`
}

// CrawlJob 是一个采集批次（CrawlJob）。Counts 的键是页面状态，值是该状态的页数；没有的状态不出现。
type CrawlJob struct {
	ID        string         `json:"id"`
	Input     CrawlInput     `json:"input"`
	Status    string         `json:"status"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
	Error     string         `json:"error"`
	Counts    map[string]int `json:"counts"`
}

// CrawlPage 是批次里发现的一个页面（CrawlPage）；不含采集结果正文，只带状态、关联条目与原因。
type CrawlPage struct {
	ID           string `json:"id"`
	JobID        string `json:"jobId"`
	URL          string `json:"url"`
	Depth        int    `json:"depth"`
	SeedIndex    int    `json:"seedIndex"`
	Status       string `json:"status"`
	Stage        string `json:"stage"`
	ItemID       string `json:"itemId"`
	ImportTaskID string `json:"importTaskId"`
	Error        string `json:"error"`
}

// CrawlJobDetail 是一个批次与它的页面明细；批次已不存在时 Job 为 nil（页面列表为空）。
type CrawlJobDetail struct {
	Job   *CrawlJob   `json:"job"`
	Pages []CrawlPage `json:"pages"`
}

// DocSite 是「采集文档站」整页依赖的能力。
type DocSite interface {
	// CreateJob 创建并启动一个批次（crawl:create），返回批次 id；入口、范围与数值上限由处理器再校验一次，原因原样带回。
	CreateJob(ctx context.Context, in CrawlInput) (id string, err error)
	// ListJobs 列出全部批次（crawl:list），新的在前由处理器决定。
	ListJobs(ctx context.Context) ([]CrawlJob, error)
	// GetJob 读一个批次与它的页面明细（crawl:get）；批次不存在是 Job 为 nil、不是错误。
	GetJob(ctx context.Context, id string) (CrawlJobDetail, error)
	// PauseJob 暂停运行中的批次（crawl:pause）。
	PauseJob(ctx context.Context, id string) error
	// ResumeJob 继续已暂停或等待继续的批次（crawl:resume）；批次不存在或已取消返回带原因的错误。
	ResumeJob(ctx context.Context, id string) error
	// RetryJob 重新排队批次里失败的页面（crawl:retry）。
	RetryJob(ctx context.Context, id string) error
	// CancelJob 取消批次（crawl:cancel）。
	CancelJob(ctx context.Context, id string) error
}
