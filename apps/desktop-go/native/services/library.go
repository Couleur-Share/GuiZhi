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
}
