package services

import "context"

// 知识库的范围取值（packages/shared 的 KnowledgeScope）。
const (
	LibraryScopeUncategorized = "uncategorized"
	LibraryScopeAll           = "all"
	LibraryScopeFavorites     = "favorites"
	LibraryScopeArchived      = "archived"
	LibraryScopeTrash         = "trash"
)

// LibraryCollection 是侧栏「合集」分区的一行（packages/shared 的 Collection）。
// 字段与渲染层同名同义；数据库里的 NULL（没有图标）解码成空串。
type LibraryCollection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Icon 是 emoji 图标；空串表示用默认的文件夹图标。
	Icon      string `json:"icon"`
	SortOrder int    `json:"sortOrder"`
	// ItemCount 是合集内未删除的条目数（侧栏的数字不取它，取 LibraryCounts.ByCollection）。
	ItemCount int   `json:"itemCount"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// LibraryTag 是侧栏「标签」分区的一行（packages/shared 的 Tag）。
type LibraryTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// ColorKey 是颜色名（red、orange、amber、green、teal、blue、indigo、purple、pink、gray）。
	ColorKey string `json:"colorKey"`
	// ItemCount 含归档的条目；侧栏判断「有没有条目」与行上的数字取 LibraryCounts.ByTag，两者口径不同。
	ItemCount int   `json:"itemCount"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// LibraryCounts 是侧栏计数（packages/shared 的 KnowledgeCounts）：
// 范围数字是全局概览，三个 By… 映射是当前筛选上下文下的分面计数（统计各自的选项时忽略同组条件）。
type LibraryCounts struct {
	Uncategorized int            `json:"uncategorized"`
	All           int            `json:"all"`
	Favorites     int            `json:"favorites"`
	Archived      int            `json:"archived"`
	Trash         int            `json:"trash"`
	ByCollection  map[string]int `json:"byCollection"`
	ByTag         map[string]int `json:"byTag"`
	ByPlatform    map[string]int `json:"byPlatform"`
}

// LibraryFacetQuery 是 knowledge:counts 的查询（KnowledgeFacetCountsQuery）：当前范围与三个分面；空串表示没有。
type LibraryFacetQuery struct {
	Scope        string `json:"scope"`
	CollectionID string `json:"collectionId,omitempty"`
	TagID        string `json:"tagId,omitempty"`
	Platform     string `json:"platform,omitempty"`
	Search       string `json:"search,omitempty"`
}

// LibraryTagPatch 是标签的部分修改：nil 的字段不改。
type LibraryTagPatch struct {
	Name     *string
	ColorKey *string
}

// Library 是知识库界面用到的数据能力（渲染层 collection.store、tag.store 与 knowledge.store 的计数、条目列表）。
// 对应的 IPC 频道：knowledge:counts、knowledge:list、collection:*、tag:*。
type Library interface {
	// Counts 返回侧栏计数（knowledge:counts）。
	Counts(ctx context.Context, q LibraryFacetQuery) (LibraryCounts, error)

	// List 返回一页条目（knowledge:list）。游标绑定查询条件：条件变了之后旧游标会被拒绝。
	List(ctx context.Context, q LibraryListQuery) (LibraryPage, error)

	// Collections 按 sort_order 列出全部合集（collection:list）。
	Collections(ctx context.Context) ([]LibraryCollection, error)
	// CreateCollection 新建合集（collection:create）；icon 为空表示没有图标。返回新建的合集。
	CreateCollection(ctx context.Context, name, icon string) (LibraryCollection, error)
	// UpdateCollection 改名与图标（collection:update）；icon 为空表示清掉图标。
	UpdateCollection(ctx context.Context, id, name, icon string) error
	// DeleteCollection 删合集，其中的条目保留为未归合集（collection:delete）；removed 为假表示合集已不存在。
	DeleteCollection(ctx context.Context, id string) (removed bool, err error)

	// Tags 按名称列出全部标签（tag:list）。
	Tags(ctx context.Context) ([]LibraryTag, error)
	// UpdateTag 改名或换色（tag:update）。
	UpdateTag(ctx context.Context, id string, patch LibraryTagPatch) error
	// DeleteTag 摘掉标签，条目保留（tag:delete）；removed 为假表示标签已不存在。
	DeleteTag(ctx context.Context, id string) (removed bool, err error)

	// Freeze 按筛选条件冻结整个结果的 ID（knowledge:selection freeze）：不分页、不限条数，含回收站范围，顺序与列表一致。
	Freeze(ctx context.Context, q LibraryListQuery) ([]string, error)
	// ExecuteBatch 对一批条目逐条执行同一个命令（knowledge:selection execute），每条独立记账。
	// runID 相同的重试复用已提交的结果、不会重做；返回的结果与 ids 一一对应。处理器整体报错时返回 error。
	ExecuteBatch(ctx context.Context, runID string, ids []string, cmd LibraryBatchCommand) ([]LibraryBatchResult, error)
	// UpdateEntry 改单条的收藏、置顶、标签（knowledge:update）；found 为假表示条目已不存在。
	// 返回的条目带这些字段的新值与更新时间，没有摘要。
	UpdateEntry(ctx context.Context, id string, patch LibraryEntryPatch) (entry LibraryEntry, found bool, err error)
	// SupplementWechat 把公众号文章加入补采队列（web:snapshot-enqueue）；一次最多 50 篇，调用方分批。
	SupplementWechat(ctx context.Context, ids []string) error
	// AiHandoff 取一条的 AI 交接稿（knowledge:get 加整理）；full 为假时是只要总结的精简版。
	// 条目不存在返回 ErrLibraryItemMissing，既没有正文也没有文字稿返回 ErrHandoffEmpty。
	AiHandoff(ctx context.Context, id string, full bool) (LibraryHandoff, error)
	// ExportAiHandoff 让用户另存交接稿（export:aiHandoff，弹出保存对话框）；用户取消时 Canceled 为真。
	ExportAiHandoff(ctx context.Context, title, text string) (LibraryExportResult, error)

	// GetItem 取一条的完整形态（knowledge:get）；found 为假表示条目不存在（已被删除）。
	GetItem(ctx context.Context, id string) (item LibraryItem, found bool, err error)
	// SaveDraft 保存编辑草稿（knowledge:saveDraft）：带基线比较，冲突时什么也不写、返回服务端的版本；
	// 标签按相对基线的增删合并。错误（error）只表示调用本身失败；业务上的失败在 LibraryDraftResult 里。
	SaveDraft(ctx context.Context, in LibraryDraftInput) (LibraryDraftResult, error)
	// SourceVersions 列出一条的历次采集留下的来源版本，新的在前，最多 20 个（knowledge:selection source-versions）。
	SourceVersions(ctx context.Context, id string) ([]LibrarySourceVersion, error)
	// MediaCapabilities 读语音转写路由的可选能力（media:capabilities）；详情据此决定摆不摆「区分说话人」。
	MediaCapabilities(ctx context.Context) (LibraryMediaCapabilities, error)

	// SourceComments 读一条已落库的来源评论（platformCapture:listComments），按点赞数降序；没有为空。
	SourceComments(ctx context.Context, itemID string) ([]LibrarySourceComment, error)
	// RefreshSourceComments 重新采集一条的来源评论并返回采集后的全部评论（platformCapture:refreshComments）；
	// limit 只能是 LibraryCommentLimits 里的档位，条目没有平台来源时失败。
	RefreshSourceComments(ctx context.Context, itemID string, limit int) ([]LibrarySourceComment, error)
	// WebVersions 读网页条目的原文版本与比较基线（web:versions）；业务上的失败（条目不存在……）作为 error 返回。
	WebVersions(ctx context.Context, itemID string) (LibraryWebVersions, error)
	// AdoptWebVersion 把某个完整的原文版本采用为正文（web:adopt）；基线与服务端不同时失败，什么也不写。
	AdoptWebVersion(ctx context.Context, in LibraryAdoptWebVersionInput) error

	// GenerateSummary 为条目生成 AI 摘要（aiflow.GenerateSummary）；未配置返回 ErrAINotConfigured。
	GenerateSummary(ctx context.Context, title, content string) (LibrarySummaryResult, error)
	// SuggestTags 为条目生成 AI 建议标签（aiflow.SuggestTags）；未配置返回 ErrAINotConfigured。
	SuggestTags(ctx context.Context, title, content string) ([]string, error)
	// RecognizeImageText 识别单张本地配图中的文字（ocr 路由 + image:readBase64 + ai:httpRequest）；未配置返回 ErrAINotConfigured。
	RecognizeImageText(ctx context.Context, assetFileName string) (string, error)
	// UpdateItemFields 局部更新条目的摘要或正文并返回更新后的完整条目（knowledge:update）；found 为假表示条目不存在。
	UpdateItemFields(ctx context.Context, id string, patch LibraryItemFieldsPatch) (item LibraryItem, found bool, err error)

	// TranscribeMedia 转写音视频条目（media:transcribe）：含排版、总结与 AI 标题，结果写回条目。
	// diarize 为真时区分说话人（仅内置本地引擎）。业务失败与未配置在 LibraryMediaActionResult 里，error 只表示调用本身失败。
	TranscribeMedia(ctx context.Context, itemID string, diarize bool) (LibraryMediaActionResult, error)
	// FormatTranscript 对已有文字稿做 AI 排版（media:formatTranscript）；allowLong 为真表示用户已确认超长稿的代价。
	FormatTranscript(ctx context.Context, itemID string, allowLong bool) (LibraryMediaActionResult, error)
	// SummarizeMedia 生成音视频总结或论坛讨论总结并写入正文（media:summarize）。
	SummarizeMedia(ctx context.Context, itemID string) (LibraryMediaActionResult, error)
	// RefreshForumDiscussion 重新抓取支持站点的论坛讨论并替换正文里的讨论小节（media:refreshForumDiscussion）。
	RefreshForumDiscussion(ctx context.Context, itemID string) (LibraryMediaActionResult, error)
	// SubscribeMediaProgress 订阅转写与排版的进度推送（media:transcribeProgress、media:formatProgress）。
	// 回调可能在任意 goroutine 触发；某一路传 nil 表示不关心。返回取消函数，可重复调用，取消后不再回调。
	SubscribeMediaProgress(onTranscribe func(LibraryTranscribeProgress), onFormat func(LibraryTranscriptFormatProgress)) (unsubscribe func())
}
