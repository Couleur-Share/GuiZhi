package services

// M3-5b：主题阅读（AI 阅读页）的数据形状与窄接口。
//
// 行为来源只有一个：渲染层 components/themed-reading 与 packages/shared/types/themed-reading.ts，后端是 themed_ipc.go /
// themed_runtime_ipc.go 的 themedReading:* 处理器与 themed_runtime.go 的任务运行时；这里的 DTO 与那几处的 JSON 形状一一对应
// （只取界面用得到的字段：页面里很大的 reconstruction / design / generation / source.blocks 留在后端，不跨过这条边界）。
//
// 任务的排队、执行与检查点都在主包的运行时里：Generate / Resume 只是「建任务并排队」，立刻返回；进度经 OnProgress 推送，
// 界面据此刷新；关闭面板只退订，绝不隐式取消已付费的生成。状态机（切换条目的防串页、固定当前页等新版完成再切、
// 任务轮询与列表筛选）在 viva/screens/themed，不在这里。
//
// 渲染 AI 阅读页本身（v3 的外壳文档、旧格式的整页 HTML）不经这个接口：阅读岛承载它们（见 M3-5b Step 2）。

import "context"

// 内容来源：正文或论坛的讨论总结（页面、任务与生成都按来源隔离）。
const (
	ThemedKindBody    = "body"
	ThemedKindSummary = "summary"
)

// 任务状态（ThemedTask.State）。
const (
	ThemedTaskQueued      = "queued"
	ThemedTaskRunning     = "running"
	ThemedTaskCompleted   = "completed"
	ThemedTaskPartial     = "partial"
	ThemedTaskFailed      = "failed"
	ThemedTaskCancelled   = "cancelled"
	ThemedTaskInterrupted = "interrupted"
)

// 任务阶段（ThemedTask.Stage）；Research 阶段暂停时可以「不联网继续」。
const (
	ThemedStagePrepare    = "prepare"
	ThemedStageUnderstand = "understand"
	ThemedStageResearch   = "research"
	ThemedStageWrite      = "write"
	ThemedStageDesign     = "design"
	ThemedStageImages     = "images"
	ThemedStageAssemble   = "assemble"
	ThemedStageValidate   = "validate"
	ThemedStageDone       = "done"
)

// 生成选项的「调整方式」（ThemedOptions.Action）与查证深度。
const (
	ThemedActionCreate   = "create"
	ThemedActionRevise   = "revise"
	ThemedActionRedesign = "redesign"
	ThemedActionRefresh  = "refresh"

	ThemedDepthStandard = "standard"
	ThemedDepthDeep     = "deep"
)

// 页面版本的角色（ThemedPage.Role）与素材状态。
const (
	ThemedRoleCurrent  = "current"
	ThemedRolePrevious = "previous"
	ThemedRoleWorking  = "working"

	ThemedAssetReady   = "ready"
	ThemedAssetPending = "pending"
	ThemedAssetFailed  = "failed"

	ThemedAssetOriginal  = "original"
	ThemedAssetGenerated = "generated"
)

// ThemedMaxImages 是一次生成最多新增的主题图片数（处理器校验 0–5）。
const ThemedMaxImages = 5

// ThemedRequest 指一份页面：条目加内容来源。
type ThemedRequest struct {
	ItemID     string `json:"itemId"`
	SourceKind string `json:"sourceKind"`
}

// ThemedOptions 是生成选项（ThemedReadingOptions）。入参时：Style、GenerateImages、MaxImages 必填且按处理器校验（图片上限 0–5）；
// EnhancedInteraction 为 nil 时处理器取默认 true，所以界面总是显式给值；ResearchDepth 与 Action 为空表示省略（不能发空串）。
// 读出时是页面生成时保存的选项（旧页面可能缺 EnhancedInteraction，按默认值 true 看，见 Enhanced）。
type ThemedOptions struct {
	EnhancedInteraction *bool  `json:"enhancedInteraction,omitempty"`
	ResearchDepth       string `json:"researchDepth,omitempty"`
	Style               string `json:"style"`
	Research            bool   `json:"research"`
	Action              string `json:"action,omitempty"`
	GenerateImages      bool   `json:"generateImages"`
	MaxImages           int    `json:"maxImages"`
	// FromCurrent 使用当前页面的内容快照和素材重新设计。
	FromCurrent bool `json:"fromCurrent,omitempty"`
}

// Enhanced 报告增强交互是否开启：缺省按开启（与处理器的默认一致）。
func (o ThemedOptions) Enhanced() bool { return o.EnhancedInteraction == nil || *o.EnhancedInteraction }

// ThemedSource 是页面生成时的内容快照里界面用得到的部分（正文与分块留在后端）。
type ThemedSource struct {
	Title       string  `json:"title"`
	SourceURI   *string `json:"sourceUri"`
	Fingerprint string  `json:"fingerprint"`
}

// ThemedAsset 是页面里的一张图片（ThemedReadingAsset）：原文里的原图，或 AI 生成的主题图片。
// FileName 非空且 Status 为 ready 时是本地图片文件，界面用 local-image 地址显示。
type ThemedAsset struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Purpose     string `json:"purpose"`
	Alt         string `json:"alt"`
	AspectRatio string `json:"aspectRatio"`
	FileName    string `json:"fileName,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// Ready 报告这张图是否已经可显示。
func (a ThemedAsset) Ready() bool { return a.Status == ThemedAssetReady && a.FileName != "" }

// Generated 报告是否是 AI 生成的插画（只有它「重做这张图」；原图失败时是「重试原图」）。
func (a ThemedAsset) Generated() bool { return a.Role == ThemedAssetGenerated }

// ThemedScript 是页面里的一个交互脚本模块（只取标识与状态）。
type ThemedScript struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// ThemedDesign 是页面设计里界面用得到的部分：有没有交互脚本（决定「暂停交互」按钮）。
type ThemedDesign struct {
	Scripts []ThemedScript `json:"scripts"`
}

// ThemedPage 是一个页面版本（ThemedReadingVersion 的界面子集）。
// FormatVersion 为 3 是当前格式（可调整、可继续）；1 / 2 是旧格式，只读保留，仍可阅读和导出。
type ThemedPage struct {
	ID            string        `json:"id"`
	ItemID        string        `json:"itemId"`
	SourceKind    string        `json:"sourceKind"`
	Role          string        `json:"role"`
	FormatVersion int           `json:"formatVersion"`
	Source        ThemedSource  `json:"source"`
	Options       ThemedOptions `json:"options"`
	Design        *ThemedDesign `json:"design"`
	Assets        []ThemedAsset `json:"assets"`
	Warnings      []string      `json:"warnings"`
	TextModel     string        `json:"textModel,omitempty"`
	ImageModel    string        `json:"imageModel,omitempty"`
	CreatedAt     int64         `json:"createdAt"`
	UpdatedAt     int64         `json:"updatedAt"`
}

// Legacy 报告是否是旧格式页面（只读：不能调整、不能继续生成或补全素材）。
func (p ThemedPage) Legacy() bool { return p.FormatVersion < 3 }

// ScriptCount 是页面带的交互脚本数（大于 0 时界面提供「暂停交互」）。
func (p ThemedPage) ScriptCount() int {
	if p.Design == nil {
		return 0
	}
	return len(p.Design.Scripts)
}

// ThemedReused 是任务复用了多少已有成果（不必再花钱重做）。
type ThemedReused struct {
	Chapters   int `json:"chapters"`
	Notes      int `json:"notes"`
	References int `json:"references"`
	Assets     int `json:"assets"`
}

// ThemedUsage 是任务的用量记账。旧任务可能没有；SearchCalls / PagesRead 只有联网查证的任务才有。
type ThemedUsage struct {
	TextCalls   int  `json:"textCalls"`
	ImageCalls  int  `json:"imageCalls"`
	ImagesSaved int  `json:"imagesSaved"`
	SearchCalls *int `json:"searchCalls,omitempty"`
	PagesRead   *int `json:"pagesRead,omitempty"`
}

// ThemedDesignProgress 是设计请求的真实输出进度（第几次尝试、已接收字符数）。
type ThemedDesignProgress struct {
	Attempt       int `json:"attempt"`
	ReceivedChars int `json:"receivedChars"`
}

// ThemedTask 是一个生成任务（ThemedReadingTask）。Completed / Total 是当前阶段的进度计数。
// Error 是失败原因（已抹掉 API Key，界面原样显示并允许复制）。
type ThemedTask struct {
	Reused          *ThemedReused         `json:"reused,omitempty"`
	Timings         map[string]int64      `json:"timings,omitempty"`
	FirstContentAt  int64                 `json:"firstContentAt,omitempty"`
	PreviewRevision int                   `json:"previewRevision,omitempty"`
	ID              string                `json:"id"`
	ItemID          string                `json:"itemId"`
	SourceKind      string                `json:"sourceKind"`
	VersionID       string                `json:"versionId"`
	Title           string                `json:"title"`
	State           string                `json:"state"`
	Stage           string                `json:"stage"`
	Completed       int                   `json:"completed"`
	Total           int                   `json:"total"`
	PlannedImages   *int                  `json:"plannedImages,omitempty"`
	DesignProgress  *ThemedDesignProgress `json:"designProgress,omitempty"`
	Usage           *ThemedUsage          `json:"usage,omitempty"`
	Error           string                `json:"error,omitempty"`
	// AssetID 非空表示这是「重做一张图」的任务：只执行这个槽位，不重新设计页面。
	AssetID string `json:"assetId,omitempty"`
	// LegacyFormat 表示旧格式（v1 / v2）页面的任务：只读保留，不能继续。
	LegacyFormat bool  `json:"legacyFormat,omitempty"`
	CreatedAt    int64 `json:"createdAt"`
	UpdatedAt    int64 `json:"updatedAt"`
}

// Active 报告任务是否还在排队或执行（此时页面不可调整，也不可再发起新的生成）。
func (t ThemedTask) Active() bool {
	return t.State == ThemedTaskQueued || t.State == ThemedTaskRunning
}

// Resumable 报告任务能否「继续未完成部分」：旧格式任务不能继续，其余只有未完成的终态可以。
func (t ThemedTask) Resumable() bool {
	if t.LegacyFormat {
		return false
	}
	switch t.State {
	case ThemedTaskPartial, ThemedTaskFailed, ThemedTaskCancelled, ThemedTaskInterrupted:
		return true
	}
	return false
}

// ThemedState 是页面的轻量状态（themedReading:state）：有没有页面、是否过期、最近一个任务。
// 切换条目时用它判断默认进入「AI 阅读」还是「原文」，比读整页便宜。
type ThemedState struct {
	HasPage       bool        `json:"hasPage"`
	VersionID     string      `json:"versionId,omitempty"`
	FormatVersion int         `json:"formatVersion,omitempty"`
	Stale         bool        `json:"stale"`
	Task          *ThemedTask `json:"task,omitempty"`
}

// ThemedModels 是生成会用到的模型名：nil 表示没有配置（文本模型缺失不能生成；生图模型缺失只能仅排版）。
type ThemedModels struct {
	Text  *string `json:"text"`
	Image *string `json:"image"`
}

// ThemedSearchStatus 是联网搜索的配置状态（ReadingSearchStatus）。Provider 取 tavily / anysearch / native；
// NativeModel 非空表示主文本模型可用于发起原生搜索测试（不代表服务端已验证支持）。
type ThemedSearchStatus struct {
	Configured          bool     `json:"configured"`
	Persistent          bool     `json:"persistent"`
	Provider            string   `json:"provider"`
	ConfiguredProviders []string `json:"configuredProviders"`
	DefaultEnabled      bool     `json:"defaultEnabled"`
	NativeModel         string   `json:"nativeModel,omitempty"`
	NativeUnavailable   string   `json:"nativeUnavailableReason,omitempty"`
}

// ThemedReadResult 是读一份页面的结果（themedReading:get）。Page 为 nil 表示还没有页面；Task 是这份内容最近的任务。
// Document 只在旧格式页面（FormatVersion 小于 3）且不是 MetadataOnly 时有值：整页 HTML，放进阅读岛的沙箱 iframe 显示；
// 当前格式的页面经外壳视图渲染，不带文档。Search 读不出来时为 nil（后端只记日志，不让它拖垮读页面）。
type ThemedReadResult struct {
	Page     *ThemedPage         `json:"page"`
	Previous bool                `json:"previous"`
	Stale    bool                `json:"stale"`
	Task     *ThemedTask         `json:"task,omitempty"`
	Document string              `json:"document,omitempty"`
	Models   ThemedModels        `json:"models"`
	Search   *ThemedSearchStatus `json:"search,omitempty"`
}

// ThemedGetInput 是读页面的入参。InstanceID 是这次阅读实例的标识（旧格式文档里的 iframe 消息按它过滤，要符合 [\w-]{1,100}）；
// MetadataOnly 为 true 时不校验图片、不生成文档（页面不显示时只要任务与模型信息）。
type ThemedGetInput struct {
	ThemedRequest
	InstanceID   string `json:"instanceId,omitempty"`
	MetadataOnly bool   `json:"metadataOnly,omitempty"`
}

// ThemedReference 是页面用到的一篇参考资料（ReadingReference 去掉正文 text）。只有被页面引用且读取成功的才会出现。
type ThemedReference struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	CapturedAt  int64  `json:"capturedAt"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// ThemedPreview 是生成途中已有章节的静态预览（themedReading:preview）。Revision 递增，旧的回包不能盖掉新的。
type ThemedPreview struct {
	Page     ThemedPage `json:"page"`
	Revision int        `json:"revision"`
}

// ThemedExportInput 是导出 HTML 的入参。VersionID 为空取当前版本；WithoutImages 导出无图版本（图片超过 100 MiB 或素材不完整时用）；
// StaticOnly 去掉全部脚本（当前格式页面才有交互可去）。
type ThemedExportInput struct {
	ThemedRequest
	WithoutImages bool   `json:"withoutImages,omitempty"`
	StaticOnly    bool   `json:"staticOnly,omitempty"`
	VersionID     string `json:"versionId,omitempty"`
}

// ThemedExportResult 是导出的结果：用户在保存对话框里取消时 Cancelled 为 true、Path 为空，不算失败。
type ThemedExportResult struct {
	Path      string `json:"path,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// ThemedSearchConfigInput 是联网搜索配置的写入入参：Provider 空表示不改；APIKey 为 nil 表示不改密钥、指向空串表示清除；
// DefaultEnabled 为 nil 表示不改；Test 为 true 时先用候选配置真实搜索一次，通过才保存（失败时保留原先可用的配置）。
type ThemedSearchConfigInput struct {
	Provider       string  `json:"provider,omitempty"`
	APIKey         *string `json:"apiKey,omitempty"`
	DefaultEnabled *bool   `json:"defaultEnabled,omitempty"`
	Test           bool    `json:"test,omitempty"`
}

// String 把密钥隐去：这个值会进日志与调试输出，不能带出明文。
func (in ThemedSearchConfigInput) String() string {
	key := "unchanged"
	if in.APIKey != nil {
		key = "cleared"
		if *in.APIKey != "" {
			key = "***"
		}
	}
	return "ThemedSearchConfigInput{provider=" + in.Provider + " apiKey=" + key + "}"
}

// GoString 同 String，让 %#v 也不泄露密钥。
func (in ThemedSearchConfigInput) GoString() string { return in.String() }

// ThemedReading 是主题阅读的数据层。
//
// 错误约定：返回的错误文本与 ThemedTask.Error 都不含 API Key；处理器回 success=false 时错误就是它的 error 字段原文（中文原因，
// 界面放进可复制的详情）；条目不存在、回收站里的条目只读等业务拒绝也是普通错误，不是哨兵。
// 任务的取消、继续与重新生成没有任何隐式副作用：只改任务与页面，已发出的模型请求可能已计费。
type ThemedReading interface {
	// State 对应 themedReading:state：页面是否存在、是否过期、最近一个任务；条目不存在是错误。
	State(ctx context.Context, req ThemedRequest) (ThemedState, error)
	// Get 对应 themedReading:get：页面、上一版是否存在、是否过期、最近任务、模型名与搜索状态（旧格式另带整页文档）。
	Get(ctx context.Context, in ThemedGetInput) (ThemedReadResult, error)
	// References 对应 themedReading:references：页面实际引用的参考资料（没有时是空切片）。
	References(ctx context.Context, req ThemedRequest) ([]ThemedReference, error)
	// Preview 对应 themedReading:preview：任务生成途中的静态预览；任务没有可预览的章节时返回 nil；任务不属于这份内容是错误。
	Preview(ctx context.Context, req ThemedRequest, taskID string) (*ThemedPreview, error)
	// ListTasks 对应 themedReading:listTasks：全部任务（处理中心「主题页任务」的数据源）。
	ListTasks(ctx context.Context) ([]ThemedTask, error)
	// OnProgress 订阅任务进度推送（themedReading:progress）：任务的每次状态变化都推一份最新快照。
	// 回调在推送方 goroutine 上同步执行，要轻量；返回取消函数（可重复调用）。
	OnProgress(fn func(ThemedTask)) (cancel func())

	// Generate 对应 themedReading:generate：建任务并排队，立即返回 queued 的任务。这份内容已有活动任务、
	// 没有可用的主文本模型、要生成图片却没有生图模型、条目在回收站、正文为空都会被拒绝。
	Generate(ctx context.Context, req ThemedRequest, opts ThemedOptions) (ThemedTask, error)
	// Cancel 对应 themedReading:cancel：把活动任务标为已取消（执行中的任务同时中止进行中的请求）；非活动任务原样返回。
	Cancel(ctx context.Context, taskID string) (ThemedTask, error)
	// Resume 对应 themedReading:resume：继续未完成的部分（失败、取消、中断、部分完成的任务，或已发布页面里失败的素材）。
	// 活动任务原样返回（幂等，不报错）；为已发布页面补失败的素材时返回的是新建的任务，其标识与传入的不同，调用方以返回值为准。
	Resume(ctx context.Context, taskID string) (ThemedTask, error)
	// ContinueOffline 对应 themedReading:continueOffline：联网查证阶段暂停的任务改为不联网继续。
	ContinueOffline(ctx context.Context, taskID string) (ThemedTask, error)
	// RegenerateAsset 对应 themedReading:regenerateAsset：只重做这一张图（不重新设计页面）。
	RegenerateAsset(ctx context.Context, req ThemedRequest, assetID string) (ThemedTask, error)
	// RestorePrevious 对应 themedReading:restorePrevious：把上一版恢复为当前版；没有上一版、上一版图片不完整或已变化是错误。
	RestorePrevious(ctx context.Context, req ThemedRequest) error
	// Remove 对应 themedReading:remove：删除这份内容的主题页（当前页、上一版与专属素材），先取消进行中的任务；正文保留。
	Remove(ctx context.Context, req ThemedRequest) error
	// ExportHTML 对应 themedReading:exportHtml：弹出保存对话框，写出自包含的单文件 HTML。
	ExportHTML(ctx context.Context, in ThemedExportInput) (ThemedExportResult, error)

	// SearchStatus 读联网搜索的配置状态（themedReading:searchConfig 不带参数）。
	SearchStatus(ctx context.Context) (ThemedSearchStatus, error)
	// ConfigureSearch 对应 themedReading:searchConfig：改搜索服务、密钥与默认开关，可选先连接测试；返回保存后的状态。
	ConfigureSearch(ctx context.Context, in ThemedSearchConfigInput) (ThemedSearchStatus, error)
}
