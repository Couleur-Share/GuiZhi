// Package wiki 是 Wiki 模块（目录、搜索、详情与编辑、[[双链]]、关系图谱、全文编译、更新建议）的状态与纯逻辑层，
// 对应渲染层的 wiki.store.ts、wiki-body.ts、WikiMarkdown 的链接预处理与 WikiCompileDialog / WikiUpdateSuggestion 的状态。
// 这一层不画任何东西：界面（Step 2 起）只读各 Store 的 View 快照并调用它们的方法。
package wiki

import (
	"slices"
	"strings"

	"guizhi-mygo-shell/native/services"
)

// CatalogFilter 是目录筛选轴。orphan（孤立页）是唯一不看字段、看关系的档：没有任何页面链向它，
// 说明它游离在知识网络之外——对靠互链组织的 Wiki 来说，这是最该被看见的一类页面。
type CatalogFilter string

const (
	FilterAll     CatalogFilter = "all"
	FilterTopic   CatalogFilter = "topic"
	FilterEntity  CatalogFilter = "entity"
	FilterConcept CatalogFilter = "concept"
	FilterManual  CatalogFilter = "manual"
	FilterOrphan  CatalogFilter = "orphan"
)

// Filters 是侧栏筛选轴的展示顺序。
var Filters = []CatalogFilter{FilterAll, FilterTopic, FilterEntity, FilterConcept, FilterManual, FilterOrphan}

// CatalogSort 是目录排序轴。
type CatalogSort string

const (
	SortRecent CatalogSort = "recent"
	SortLinked CatalogSort = "linked"
	SortTitle  CatalogSort = "title"
)

// Sorts 是排序下拉的展示顺序。
var Sorts = []CatalogSort{SortRecent, SortLinked, SortTitle}

// ViewMode 是工作区的视图：目录 + 详情，或关系图谱。
type ViewMode string

const (
	ViewCatalog ViewMode = "catalog"
	ViewGraph   ViewMode = "graph"
)

// SearchLimit 是目录列搜索一次最多取回的页面数（问答检索另有自己的上限）。
const SearchLimit = 100

// CatalogCounts 是侧栏筛选轴各档的计数。
type CatalogCounts struct {
	All, Topic, Entity, Concept, Manual, Orphan int
}

// For 取某一档的计数。
func (c CatalogCounts) For(f CatalogFilter) int {
	switch f {
	case FilterTopic:
		return c.Topic
	case FilterEntity:
		return c.Entity
	case FilterConcept:
		return c.Concept
	case FilterManual:
		return c.Manual
	case FilterOrphan:
		return c.Orphan
	}
	return c.All
}

// CountByFilter 统计目录各筛选档的条数。未知类型的页面只计入「全部」（不会让某一档变成 NaN 之类的怪值）。
func CountByFilter(catalog []services.WikiCatalogEntry, backlinks services.WikiBacklinkCounts) CatalogCounts {
	counts := CatalogCounts{All: len(catalog)}
	for _, e := range catalog {
		switch e.Kind {
		case services.WikiKindTopic:
			counts.Topic++
		case services.WikiKindEntity:
			counts.Entity++
		case services.WikiKindConcept:
			counts.Concept++
		}
		if e.ManualEditedAt != nil {
			counts.Manual++
		}
		if backlinks[e.ID] == 0 {
			counts.Orphan++
		}
	}
	return counts
}

// MatchesFilter 报告一页是否属于某个筛选档。
func MatchesFilter(e services.WikiCatalogEntry, f CatalogFilter, backlinks services.WikiBacklinkCounts) bool {
	switch f {
	case FilterAll:
		return true
	case FilterManual:
		return e.ManualEditedAt != nil
	case FilterOrphan:
		return backlinks[e.ID] == 0
	}
	return e.Kind == string(f)
}

// VisibleParams 是 SelectVisible 的入参。
type VisibleParams struct {
	Catalog        []services.WikiCatalogEntry
	BacklinkCounts services.WikiBacklinkCounts
	Filter         CatalogFilter
	Sort           CatalogSort
	// SearchActive 为 true 表示当前有搜索，SearchHitIDs 是 FTS 命中的页面 ID（按相关度排序，可以是空：搜到零条）。
	SearchActive bool
	SearchHitIDs []string
}

// SelectVisible 计算目录列的可见范围。
//
// 搜索命中（FTS，覆盖正文）与筛选、排序是叠加关系：命中集合先限定范围，再套用当前筛选轴。
// 搜索按 bm25 相关度排序，这时不再套用排序轴——把相关度最高的结果按标题重排等于把搜索结果打散。
// 返回新切片，不改入参。
func SelectVisible(p VisibleParams) []services.WikiCatalogEntry {
	matching := make([]services.WikiCatalogEntry, 0, len(p.Catalog))
	for _, e := range p.Catalog {
		if MatchesFilter(e, p.Filter, p.BacklinkCounts) {
			matching = append(matching, e)
		}
	}
	if p.SearchActive {
		rank := make(map[string]int, len(p.SearchHitIDs))
		for i, id := range p.SearchHitIDs {
			if _, dup := rank[id]; !dup {
				rank[id] = i
			}
		}
		hits := matching[:0]
		for _, e := range matching {
			if _, ok := rank[e.ID]; ok {
				hits = append(hits, e)
			}
		}
		slices.SortStableFunc(hits, func(a, b services.WikiCatalogEntry) int { return rank[a.ID] - rank[b.ID] })
		return hits
	}
	switch p.Sort {
	case SortTitle:
		slices.SortStableFunc(matching, func(a, b services.WikiCatalogEntry) int { return compareTitle(a.Title, b.Title) })
	case SortLinked:
		slices.SortStableFunc(matching, func(a, b services.WikiCatalogEntry) int {
			if d := p.BacklinkCounts[b.ID] - p.BacklinkCounts[a.ID]; d != 0 {
				return d
			}
			return compareInt64(b.UpdatedAt, a.UpdatedAt)
		})
	default:
		slices.SortStableFunc(matching, func(a, b services.WikiCatalogEntry) int { return compareInt64(b.UpdatedAt, a.UpdatedAt) })
	}
	return matching
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareTitle 近似 JS 的 localeCompare：先不区分大小写比较，相同再按原文比较，保证排序稳定、同名大小写变体相邻。
func compareTitle(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// CompileButtonState 是工具栏编译按钮与全文编译对话框共用的预览口径：数字只算「可执行」，
// 只要还有待升级、待复核或等待中的资料就允许打开对话框；预览未知（counts 为 nil）时不拦。
func CompileButtonState(counts *services.WikiCompileCounts) (pending int, actionable bool) {
	if counts == nil {
		return 0, true
	}
	return counts.Ready, counts.Ready+counts.Upgrade+counts.Review+counts.Waiting > 0
}
