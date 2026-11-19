package services

// 条目列表（渲染层 knowledge.store 的 fetchList 与 knowledge:list 频道）的数据形状。

// 列表排序字段（packages/shared 的 KnowledgeSortField）。置顶条目始终在最前。
const (
	LibrarySortUpdatedAt = "updatedAt"
	LibrarySortCreatedAt = "createdAt"
	LibrarySortTitle     = "title"
)

// 列表排序方向。
const (
	LibraryOrderAsc  = "asc"
	LibraryOrderDesc = "desc"
)

// 条目类型（packages/shared 的 KnowledgeItemType）。数据库里可能出现比本版本更新的类型，界面要有兜底。
const (
	LibraryTypeNote     = "note"
	LibraryTypeWebpage  = "webpage"
	LibraryTypeVideo    = "video"
	LibraryTypeImage    = "image"
	LibraryTypeAudio    = "audio"
	LibraryTypeDocument = "document"
	LibraryTypeSnippet  = "snippet"
	LibraryTypeForum    = "forum"
)

// 条目状态。
const (
	LibraryStatusActive   = "active"
	LibraryStatusArchived = "archived"
)

// LibraryListQuery 是 knowledge:list 的查询（KnowledgeItemQuery 里列表用到的部分）。空串表示「没有这个条件」。
type LibraryListQuery struct {
	Scope        string `json:"scope"`
	CollectionID string `json:"collectionId,omitempty"`
	TagID        string `json:"tagId,omitempty"`
	Platform     string `json:"platform,omitempty"`
	// Search 是全文搜索关键词（已去掉首尾空白）；非空时结果按相关度排序，排序字段不起作用。
	Search    string `json:"search,omitempty"`
	SortBy    string `json:"sortBy,omitempty"`
	SortOrder string `json:"sortOrder,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	// Cursor 是不透明的 keyset 游标，提供时优先于 Offset；游标绑定了查询的指纹，换了条件就失效。
	Cursor string `json:"cursor,omitempty"`
	Offset int    `json:"offset"`
}

// LibraryEntry 是列表里的一条（packages/shared 的 KnowledgeItemListEntry）。数据库里的 NULL 解码成零值。
type LibraryEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Snippet 是正文前段的纯文本摘要；搜索时是以命中处为中心的一段。
	Snippet  string `json:"snippet"`
	ItemType string `json:"itemType"`
	Status   string `json:"status"`
	// CollectionID 为空表示未归入任何合集。
	CollectionID string `json:"collectionId"`
	IsFavorite   bool   `json:"isFavorite"`
	IsPinned     bool   `json:"isPinned"`
	// Platform 是采集来源平台；手工条目为空。
	Platform string `json:"platform"`
	// DeletedAt 是移入回收站的时间（毫秒）；0 表示没有删除。
	DeletedAt int64 `json:"deletedAt"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
	// Tags 按名称排序；ItemCount 在这里没有意义（恒为 0）。
	Tags []LibraryTag `json:"tags"`
	// ReviewStatus 是待复核状态（clear、pending 等）；ReviewReasons 是原因。
	ReviewStatus  string   `json:"reviewStatus"`
	ReviewReasons []string `json:"reviewReasons"`
}

// LibraryPage 是一页结果（KnowledgeItemListResult）。
type LibraryPage struct {
	Entries []LibraryEntry `json:"entries"`
	// Total 是符合条件的总数，不是本页条数。
	Total int `json:"total"`
	// NextCursor 非空表示本页之后还有数据；下一页应优先用它（比偏移稳定，也更快）。
	NextCursor string `json:"nextCursor"`
}
