package services

// M3-5a：Wiki 模块（目录、搜索、详情、手动编辑、关系图谱、全文编译、更新建议）的数据形状与窄接口。
//
// 行为来源只有一个：渲染层 stores/wiki.store.ts、services/knowledge-ai/wiki-v2.ts 与 components/wiki 下的组件，
// 后端是 wiki.go（wiki:* 处理器）与 wiki_compiler.go（wiki:compiler 的检查点与更新建议）；这里的 DTO 与那几处的 JSON 形状一一对应。
// 全文编译的驱动循环（取分块 → 调模型 → 清洗 → 落盘）在 aiflow.WikiDriver，主包适配器（native_wiki.go）把它们与处理器接起来；
// 状态机（目录筛选与搜索、选页与防串页、编辑草稿、编译任务轮询、更新建议状态）在 viva/screens/wiki，不在这里。
//
// Wiki 的 [[链接]] 在页面里是按标题与别名解析的：界面层用 aiflow.NormalizeWikiTitle / BuildLinkResolver / CleanWikiLinks
// 解析（与保存时的出链重建同一份实现），不经过本接口。

import (
	"context"

	"guizhi-mygo-shell/aiflow"
)

// WikiPageKind 是页面类型：主题 / 实体 / 概念。
type WikiPageKind = string

const (
	WikiKindTopic   WikiPageKind = "topic"
	WikiKindEntity  WikiPageKind = "entity"
	WikiKindConcept WikiPageKind = "concept"
)

// WikiCompilerVersion 是当前的编译器版本（页面的 promptVersion 与它不同，说明是旧版编译的产物，可选择资料升级）。
const WikiCompilerVersion = aiflow.WikiCompilerVersion

// 编译任务的状态（WikiCompileJob.Status）。
const (
	WikiJobRunning     = "running"
	WikiJobPaused      = "paused"
	WikiJobCancelled   = "cancelled"
	WikiJobInterrupted = "interrupted"
	WikiJobFailed      = "failed"
	WikiJobCompleted   = "completed"
)

// 编译预览里一条候选资料的状态（WikiCompileCandidate.State）。
const (
	WikiCandidateReady    = "ready"
	WikiCandidateReview   = "review"
	WikiCandidateWaiting  = "waiting"
	WikiCandidateUpgrade  = "upgrade"
	WikiCandidateCurrent  = "current"
	WikiCandidateExcluded = "excluded"
)

// ---- 目录与读取 ----

// WikiCatalogEntry 是目录里的一页（WikiCatalogEntry）。AliasesJSON 是别名数组的 JSON 文本，可空；
// ManualEditedAt 非空表示这一页被手动改过（后续编译不再覆盖它的正文）。
type WikiCatalogEntry struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	NormalizedTitle string       `json:"normalizedTitle"`
	Kind            WikiPageKind `json:"kind"`
	Summary         string       `json:"summary"`
	AliasesJSON     *string      `json:"aliasesJson"`
	ManualEditedAt  *int64       `json:"manualEditedAt"`
	UpdatedAt       int64        `json:"updatedAt"`
}

// WikiBacklinkCounts 是页面 ID → 被多少页引用（没有引用的页面不在表里，读出来是 0）。
type WikiBacklinkCounts map[string]int

// WikiStatus 是顶栏状态行的三个数字（WikiCompilationStatus）。
type WikiStatus struct {
	PageCount         int `json:"pageCount"`
	CompiledItemCount int `json:"compiledItemCount"`
	EligibleItemCount int `json:"eligibleItemCount"`
}

// WikiSearchHit 是全文检索（覆盖标题、摘要与正文，按相关度排序）的一条命中。
type WikiSearchHit struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Kind    WikiPageKind `json:"kind"`
	Summary string       `json:"summary"`
}

// WikiPage 是页面本体（WikiPage）。Provider / Model / PromptVersion 是编译来源信息，手写页面也保留编译时的值。
type WikiPage struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	NormalizedTitle string       `json:"normalizedTitle"`
	Kind            WikiPageKind `json:"kind"`
	Summary         string       `json:"summary"`
	Body            string       `json:"body"`
	AliasesJSON     *string      `json:"aliasesJson"`
	Provider        string       `json:"provider"`
	Model           string       `json:"model"`
	PromptVersion   string       `json:"promptVersion"`
	GeneratedAt     int64        `json:"generatedAt"`
	ManualEditedAt  *int64       `json:"manualEditedAt"`
	CreatedAt       int64        `json:"createdAt"`
	UpdatedAt       int64        `json:"updatedAt"`
}

// WikiPageSource 是页面的一个来源条目（WikiSourceRef）；条目已在回收站的不会出现。
type WikiPageSource struct {
	ItemID        string   `json:"itemId"`
	Title         string   `json:"title"`
	ReviewStatus  string   `json:"reviewStatus"`
	ReviewReasons []string `json:"reviewReasons"`
}

// NeedsReview 报告来源条目是否在待复核状态（页面据此提示「相关结论可能受内容缺失影响」）。
func (s WikiPageSource) NeedsReview() bool { return s.ReviewStatus == "needs_review" }

// WikiPageDetail 是详情页要的全部内容（WikiPageDetail）：页面、反向链接（引用了它的页面）与来源条目。
type WikiPageDetail struct {
	Page      WikiPage           `json:"page"`
	Backlinks []WikiCatalogEntry `json:"backlinks"`
	Sources   []WikiPageSource   `json:"sources"`
}

// WikiRevision 是页面的一份历史版本（每次被编译覆盖或手动编辑前的快照，每页最多保留 10 份，新的在前）。
type WikiRevision struct {
	ID            string       `json:"id"`
	PageID        string       `json:"pageId"`
	Title         string       `json:"title"`
	Kind          WikiPageKind `json:"kind"`
	Summary       string       `json:"summary"`
	Body          string       `json:"body"`
	AliasesJSON   *string      `json:"aliasesJson"`
	Model         string       `json:"model"`
	PromptVersion string       `json:"promptVersion"`
	CreatedAt     int64        `json:"createdAt"`
}

// WikiGraphNode 与 WikiGraphLink 是关系图谱的节点与有向边（源页面引用目标页面）。
type WikiGraphNode struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Kind  WikiPageKind `json:"kind"`
}

type WikiGraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// WikiGraph 是图谱数据（WikiGraph）：节点按连接度取前 300 个，TotalNodes 是页面总数（比 len(Nodes) 大说明被截断）。
type WikiGraph struct {
	Nodes      []WikiGraphNode `json:"nodes"`
	Links      []WikiGraphLink `json:"links"`
	TotalNodes int             `json:"totalNodes"`
}

// ---- 写入 ----

// WikiUpdatePageInput 是手动改写页面正文的入参（wiki:updatePage）。LinkTargets 是正文里 [[链接]] 解析出的目标页规范化标题，
// 后端据此重建这一页的出链；ReleaseToAuto 为 true 时清除「手动编辑」标记，让后续编译重新接管。
type WikiUpdatePageInput struct {
	PageID        string   `json:"pageId"`
	Body          string   `json:"body"`
	LinkTargets   []string `json:"linkTargets"`
	ReleaseToAuto bool     `json:"releaseToAuto,omitempty"`
}

// ---- 全文编译 ----

// WikiCompileCounts 是预览里各状态的候选条数；Ready 是工具栏按钮上显示的「待编译」数。
type WikiCompileCounts struct {
	Ready    int `json:"ready"`
	Review   int `json:"review"`
	Waiting  int `json:"waiting"`
	Upgrade  int `json:"upgrade"`
	Current  int `json:"current"`
	Excluded int `json:"excluded"`
}

// WikiCompileCandidate 是预览里的一条候选资料。Fingerprint 是内容指纹：开始编译时后端会核对，资料变了就拒绝。
// Blocks 是分块数，Reusable 是其中可复用缓存、不必再调模型的块数。
type WikiCompileCandidate struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Blocks      int    `json:"blocks"`
	Reusable    int    `json:"reusable"`
}

// Selectable 报告候选是否能被选进本批编译（只有「可执行」与「待选择升级」的资料可选）。
func (c WikiCompileCandidate) Selectable() bool {
	return c.State == WikiCandidateReady || c.State == WikiCandidateUpgrade
}

// WikiRequestRange 是预览估算的模型调用次数区间（含至多一次格式纠错）。
type WikiRequestRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// WikiCompilePreview 是编译预览：全部候选、各状态计数与调用次数估算。
type WikiCompilePreview struct {
	Entries  []WikiCompileCandidate `json:"entries"`
	Counts   WikiCompileCounts      `json:"counts"`
	Requests WikiRequestRange       `json:"requests"`
}

// WikiCompileJob 是一个编译任务（WikiCompileJob）。Total / Completed / Failed 按分块计。
type WikiCompileJob struct {
	ID        string `json:"id"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// Resumable 报告任务是否能「继续未完成分块」：除了正在跑与已完成的，其余状态都可以。
func (j WikiCompileJob) Resumable() bool {
	return j.Status != WikiJobRunning && j.Status != WikiJobCompleted
}

// WikiStartError 是开始编译被后端拒绝：资料或范围在预览之后变了（带刷新过的预览，界面据此更新选择），
// 或已有任务在跑、没选资料。Preview 可空。
type WikiStartError struct {
	Message string
	Preview *WikiCompilePreview
}

func (e *WikiStartError) Error() string { return e.Message }

// ---- 更新建议 ----

// WikiSuggestionDraft 是建议采用的页面内容（WikiContributions 的元素）。
type WikiSuggestionDraft struct {
	Title           string       `json:"title"`
	NormalizedTitle string       `json:"normalizedTitle"`
	Kind            WikiPageKind `json:"kind"`
	Summary         string       `json:"summary"`
	Body            string       `json:"body"`
	AliasesJSON     *string      `json:"aliasesJson"`
	LinkTargets     []string     `json:"linkTargets"`
}

// WikiSuggestion 是一页的更新建议：手动改过的页面（或仍有旧版来源的页面）在来源更新后不会被直接覆盖，
// 新内容存成建议等用户审阅。Token 是「建议内容 + 页面当前快照」的哈希，页面或建议变化后失效。
type WikiSuggestion struct {
	Draft     WikiSuggestionDraft `json:"draft"`
	Model     string              `json:"model"`
	SourceIDs []string            `json:"sourceIds"`
	Reason    string              `json:"reason"`
	Token     string              `json:"token"`
}

// Wiki 是 Wiki 模块的数据层。
//
// 错误约定：返回的错误文本不含 API Key；模型未配置返回 ErrAINotConfigured；其余是带原因的普通错误。
// 「按 ID 读」的方法在目标不存在时返回 nil / false，不算错误。
type Wiki interface {
	// Catalog 对应 wiki:catalog：全部页面，最近更新在前。
	Catalog(ctx context.Context) ([]WikiCatalogEntry, error)
	// BacklinkCounts 对应 wiki:backlinkCounts。
	BacklinkCounts(ctx context.Context) (WikiBacklinkCounts, error)
	// Status 对应 wiki:status。
	Status(ctx context.Context) (WikiStatus, error)
	// Search 对应 wiki:search：全文检索，按相关度排序，最多 limit 条；查询里没有可检索的词时返回空切片。
	Search(ctx context.Context, query string, limit int) ([]WikiSearchHit, error)
	// GetPage 对应 wiki:getPage；页面不存在返回 nil。
	GetPage(ctx context.Context, id string) (*WikiPageDetail, error)
	// ListRevisions 对应 wiki:listRevisions：这一页的历史版本，最新在前。
	ListRevisions(ctx context.Context, pageID string) ([]WikiRevision, error)
	// Graph 对应 wiki:graph。
	Graph(ctx context.Context) (WikiGraph, error)

	// UpdatePage 对应 wiki:updatePage：改写正文并重建出链；页面不存在返回 false。会先存一份历史版本。
	UpdatePage(ctx context.Context, in WikiUpdatePageInput) (bool, error)
	// DeletePage 对应 wiki:deletePage；页面不存在返回 false。指向它的链接会变成断链，下一轮编译可能重新生成它。
	DeletePage(ctx context.Context, id string) (bool, error)
	// RestoreRevision 对应 wiki:restoreRevision：把页面回滚到某份历史版本（回滚本身也会存一份快照）；版本不存在返回 false。
	RestoreRevision(ctx context.Context, revisionID string) (bool, error)

	// CompileConfigured 对应 isScenarioConfigured("wiki")：Wiki 场景是否已有可用模型。
	CompileConfigured(ctx context.Context) (bool, error)
	// CompilePreview 对应 wikiPreview()：按当前 Wiki 模型的身份串预览全库的编译范围（大库较慢，逐条算指纹）。
	CompilePreview(ctx context.Context) (WikiCompilePreview, error)
	// CompileJobs 对应 wiki:compiler 的 jobs：最近 100 个任务，新的在前。
	CompileJobs(ctx context.Context) ([]WikiCompileJob, error)
	// StartCompile 对应 wiki:compiler 的 start（允许选择历史升级）：创建任务但不执行；被拒绝时返回 *WikiStartError。
	StartCompile(ctx context.Context, selected []WikiCompileCandidate) (WikiCompileJob, error)
	// RunCompile 对应 runWikiJob：串行驱动任务直到没有可做的分块、被暂停或被取消，阻塞到结束；progress 可为 nil，
	// 每次从检查点读到任务状态时回调。返回最后的任务状态（被取消时为 cancelled）。模型未配置返回 ErrAINotConfigured，
	// 模型身份与任务创建时不同、或已有任务在跑时返回带原因的错误。ctx 取消等同于取消任务。
	RunCompile(ctx context.Context, job WikiCompileJob, progress func(WikiCompileJob)) (WikiCompileJob, error)
	// ControlCompile 对应 controlWikiJob：status 取 WikiJobPaused / WikiJobCancelled / WikiJobRunning。
	// 取消恰好是正在驱动的任务时同时中止进行中的请求；改成 running 只改检查点，由调用方再 RunCompile。
	ControlCompile(ctx context.Context, jobID, status string) (WikiCompileJob, error)

	// Suggestion 对应 wiki:compiler 的 suggestion（读取）：没有建议返回 nil。
	Suggestion(ctx context.Context, pageID string) (*WikiSuggestion, error)
	// AcceptSuggestion 对应 suggestion 的 accept：采用建议并保存当前版本用于恢复；建议或页面已变化（令牌失效）返回 false。
	AcceptSuggestion(ctx context.Context, pageID, token string) (bool, error)
}
