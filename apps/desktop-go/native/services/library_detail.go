package services

import (
	"encoding/json"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// 条目详情（渲染层 knowledge.store 的 selectItem 与草稿保存管线、SourceCaptureRevisions）的数据形状。
// 对应的 IPC 频道：knowledge:get、knowledge:saveDraft、knowledge:selection 的 source-versions。

// 复核状态（packages/shared 的 KnowledgeReviewStatus）。列表里的 LibraryEntry.ReviewStatus 同值域。
const (
	LibraryReviewClear       = "clear"
	LibraryReviewNeedsReview = "needs_review"
)

// 草稿冲突涉及的字段（SaveKnowledgeDraftResult.conflicts）。
const (
	LibraryDraftFieldTitle   = "title"
	LibraryDraftFieldContent = "content"
)

// LibraryItem 是条目的完整形态（packages/shared 的 KnowledgeItem）：比列表里的 LibraryEntry 多了正文、摘要、文字稿与来源。
// 数据库里的 NULL 解码成零值。
type LibraryItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	// Summary 是 AI 摘要；没有为空串。
	Summary string `json:"summary"`
	// Transcript 是音视频的文字稿；没有为空串。
	Transcript string `json:"transcript"`
	ItemType   string `json:"itemType"`
	Status     string `json:"status"`
	// CollectionID 为空表示未归入任何合集。
	CollectionID string `json:"collectionId"`
	IsFavorite   bool   `json:"isFavorite"`
	IsPinned     bool   `json:"isPinned"`
	// ReviewStatus 是导入质量检查的结果（LibraryReview*）；手工创建及未发现异常的条目为 clear（旧数据可能为空）。
	ReviewStatus string `json:"reviewStatus"`
	// ReviewReasons 是需要复核的具体原因，保留采集阶段的可读诊断。
	ReviewReasons []string `json:"reviewReasons"`
	// SourceURI 是最近一次采集的来源地址；手工条目为空。
	SourceURI string `json:"sourceUri"`
	// DeletedAt 是移入回收站的时间（毫秒）；0 表示没有删除。
	DeletedAt int64 `json:"deletedAt"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
	// Tags 按名称排序。
	Tags []LibraryTag `json:"tags"`
}

// TagNames 返回标签名（按 Tags 的顺序）。
func (it LibraryItem) TagNames() []string {
	names := make([]string, 0, len(it.Tags))
	for _, t := range it.Tags {
		names = append(names, t.Name)
	}
	return names
}

// LibraryDraftFields 是草稿里可本地编辑并防抖保存的字段（KnowledgeDraftFields）：nil 的字段表示「没有这个字段」。
// 分类不在其中：改分类是一次命令，立即落盘（LibraryEntryPatch）。
type LibraryDraftFields struct {
	Title   *string
	Content *string
	// TagNames 为 nil 表示没有改标签；非 nil 的空切片表示清空。
	TagNames []string
}

// IsEmpty 报告是否一个字段都没有。
func (f LibraryDraftFields) IsEmpty() bool {
	return f.Title == nil && f.Content == nil && f.TagNames == nil
}

// Clone 返回深拷贝（切片与指针都不与原值共用）。
func (f LibraryDraftFields) Clone() LibraryDraftFields {
	out := LibraryDraftFields{}
	if f.Title != nil {
		out.Title = StringPtr(*f.Title)
	}
	if f.Content != nil {
		out.Content = StringPtr(*f.Content)
	}
	if f.TagNames != nil {
		out.TagNames = append([]string{}, f.TagNames...)
	}
	return out
}

// MarshalJSON 只写出设置了的字段。
func (f LibraryDraftFields) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if f.Title != nil {
		m["title"] = *f.Title
	}
	if f.Content != nil {
		m["content"] = *f.Content
	}
	if f.TagNames != nil {
		m["tagNames"] = f.TagNames
	}
	return json.Marshal(m)
}

// LibraryDraftInput 是一次草稿保存（SaveKnowledgeDraftInput）。Base 是编辑开始时看到的字段，服务端据此判断冲突。
type LibraryDraftInput struct {
	ID        string             `json:"id"`
	RequestID string             `json:"requestId"`
	Base      LibraryDraftFields `json:"base"`
	Patch     LibraryDraftFields `json:"patch"`
}

// LibraryDraftResult 是草稿保存的结果（SaveKnowledgeDraftResult）。
// OK 为假且 Conflicts 非空表示冲突：Item 是服务端此刻的版本，什么也没写；OK 为假且 Conflicts 为空是别的失败（Error 是原因）。
type LibraryDraftResult struct {
	OK        bool         `json:"ok"`
	Item      *LibraryItem `json:"item"`
	Conflicts []string     `json:"conflicts"`
	Error     string       `json:"error"`
}

// LibrarySourceVersion 是一次采集留下的来源版本（knowledge:selection source-versions 的一项）。
type LibrarySourceVersion struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Transcript string `json:"transcript"`
	// ReviewJSON 是该版本采集时的复核原因（JSON 数组的文本）。
	ReviewJSON string `json:"reviewJson"`
	CapturedAt int64  `json:"capturedAt"`
}

// JSLower 是 JS 的 String.prototype.toLowerCase：完整的 Unicode 映射、词尾 Σ。
// 标签名的比较与键（合并、乐观回显）都以它为准，与渲染层行为一致。
func JSLower(s string) string { return cases.Lower(language.Und).String(s) }

// MergeDraftTags 按相对基线的增删合并标签，保留另一入口新增的标签（packages/shared 的 mergeDraftTags）：
// 以去掉首尾空白并转小写（JSLower）的名字作键，首次出现的位置、最后写入的值。
func MergeDraftTags(base, edited, current []string) []string {
	key := func(s string) string { return JSLower(strings.TrimFunc(s, isJSSpace)) }
	wanted := map[string]bool{}
	for _, n := range edited {
		wanted[key(n)] = true
	}
	removed := map[string]bool{}
	for _, n := range base {
		if !wanted[key(n)] {
			removed[key(n)] = true
		}
	}
	var order []string
	result := map[string]string{}
	set := func(k, v string) {
		if _, ok := result[k]; !ok {
			order = append(order, k)
		}
		result[k] = v
	}
	for _, n := range current {
		if !removed[key(n)] {
			set(key(n), n)
		}
	}
	original := map[string]bool{}
	for _, n := range base {
		original[key(n)] = true
	}
	for _, n := range edited {
		if !original[key(n)] {
			set(key(n), n)
		}
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, result[k])
	}
	return out
}
