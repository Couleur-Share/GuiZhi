package library

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// 条目列表的状态（渲染层 knowledge.store 的列表部分）：条目、总数、分页、排序、搜索、选中与多选。
// 与侧栏共用一个 Store：筛选一变，列表、选中与分页游标要在同一刻一起重置（台账 B-033 的 (2)(3)）。

// 每页条数（渲染层 DEFAULT_PAGE_SIZE 与 PAGE_SIZE_OPTIONS）。
const DefaultPageSize = 20

// PageSizeOptions 是每页条数的可选值。
var PageSizeOptions = []int{10, 20, 50, 100}

// ListView 是条目列表此刻的快照；切片在 Store 里整体替换，快照拿到之后不会被改。
type ListView struct {
	Entries []services.LibraryEntry
	// Total 是符合筛选的总数（不是本页条数）。
	Total          int
	Page, PageSize int
	SortBy         string
	SortOrder      string
	// Search 是搜索框的原文（不去空白）；发给后端的是去掉首尾空白之后的。
	Search string
	// Loading 在读取进行中为真；LoadError 是最近一次读取的失败原因（读取成功后清掉）。
	// 界面只在「还没有任何条目」时才把它们画出来（读到过的列表保留着，渲染层同）。
	Loading   bool
	LoadError string
	// SelectedID 是右侧详情在看的条目（渲染层 selectedId）；Selection 是多选的 ID 集合（按选入顺序），Anchor 是 Shift 范围选择的起点。
	SelectedID string
	Selection  []string
	Anchor     string
}

// initialList 是列表的初值：第一页、每页 20 条、按更新时间新的在前（渲染层 knowledge.store 的初始状态）。
func initialList() ListView {
	return ListView{Page: 1, PageSize: DefaultPageSize, SortBy: services.LibrarySortUpdatedAt, SortOrder: services.LibraryOrderDesc}
}

// facetQuery 是侧栏计数的查询：筛选加搜索词（渲染层 buildFacetCountsQuery）。mu 由调用方持有。
func (s *Store) facetQuery() services.LibraryFacetQuery {
	q := s.view.Filters.FacetQuery()
	q.Search = s.view.List.searchTerm()
	return q
}

// searchTerm 是发给后端的搜索词：去掉首尾空白。
func (l ListView) searchTerm() string { return strings.TrimSpace(l.Search) }

// IsSelected 报告条目是否在多选集合里。
func (l ListView) IsSelected(id string) bool { return slices.Contains(l.Selection, id) }

// listState 是 Store 里不进快照的列表内部状态。
type listState struct {
	// seq 是读取请求的序号：请求可能乱序返回，旧筛选或旧页的结果不能覆盖新的（渲染层 listRequestSeq）。
	seq int
	// cursors 是各页的游标（页码 → 游标），第一页恒为空串。页码不在表里表示没有游标，退回偏移。
	cursors map[int]string
}

// resetPaging 回到第一页并丢掉全部游标。游标绑定了查询条件，条件变了就必须丢（否则后端会拒绝）。mu 由调用方持有。
func (s *Store) resetPaging() {
	s.view.List.Page = 1
	s.list.cursors = map[int]string{1: ""}
}

// resetSelection 清空选中与多选（渲染层 updateFilters 的重置）。mu 由调用方持有。
func (s *Store) resetSelection() {
	s.view.List.SelectedID, s.view.List.Selection, s.view.List.Anchor = "", nil, ""
}

// listQuery 按当前状态拼出这一页的查询。mu 由调用方持有。
func (s *Store) listQuery() services.LibraryListQuery {
	l, f := s.view.List, s.view.Filters
	q := services.LibraryListQuery{
		Scope: f.Scope, CollectionID: f.CollectionID, TagID: f.TagID, Platform: f.Platform,
		Search: l.searchTerm(), SortBy: l.SortBy, SortOrder: l.SortOrder, Limit: l.PageSize,
	}
	if l.Page == 1 {
		return q
	}
	if cursor, ok := s.list.cursors[l.Page]; ok && cursor != "" {
		q.Cursor = cursor
		return q
	}
	q.Offset = (l.Page - 1) * l.PageSize
	return q
}

// FetchList 重新读取当前页（渲染层 fetchList）。只认最后发出的那次请求；失败记下原因，已有的条目保留。
func (s *Store) FetchList(ctx context.Context) {
	s.mu.Lock()
	s.list.seq++
	seq := s.list.seq
	s.view.List.Loading, s.view.List.LoadError = true, ""
	query := s.listQuery()
	s.mu.Unlock()
	s.wake()

	page, err := s.svc.List(ctx, query)

	s.mu.Lock()
	if seq != s.list.seq {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.view.List.Loading, s.view.List.LoadError = false, err.Error()
		s.mu.Unlock()
		s.guard.Background("library.list", fmt.Errorf("加载知识条目列表失败：%w", err))
		s.wake()
		return
	}
	l := &s.view.List
	// 当前页越界（例如删光了最后一页）：回退到最后一页重取。
	if len(page.Entries) == 0 && l.Page > 1 {
		if last := max(1, (page.Total+l.PageSize-1)/l.PageSize); l.Page > last {
			l.Page = last
			s.mu.Unlock()
			s.FetchList(ctx)
			return
		}
	}
	l.Entries, l.Total, l.Loading, l.LoadError = page.Entries, page.Total, false, ""
	// 有下一页就记下它的游标；没有就丢掉本页之后的全部游标。
	if page.NextCursor != "" {
		s.list.cursors[l.Page+1] = page.NextCursor
	} else {
		for p := range s.list.cursors {
			if p > l.Page {
				delete(s.list.cursors, p)
			}
		}
	}
	s.mu.Unlock()
	s.wake()
}

// RefreshAll 重新读取筛选相关的全部数据（渲染层 refreshAll）：列表与侧栏计数并行，都读完才返回。
func (s *Store) RefreshAll(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	s.Spawn(func() { defer wg.Done(); s.FetchList(ctx) })
	s.RefreshCounts(ctx)
	wg.Wait()
}

// ---- 搜索、排序、分页 ----

// SetSearch 设置搜索词并重新读取（渲染层 setSearchQuery）：回到第一页、清掉多选；
// 结果出来之后，右侧详情若还停在一条不在结果里的条目上，就清掉它（否则会让人以为那是命中项）。
func (s *Store) SetSearch(ctx context.Context, query string) {
	s.mu.Lock()
	s.view.List.Search = query
	s.resetPaging()
	s.view.List.Selection, s.view.List.Anchor = nil, ""
	s.list.seq++ // 旧搜索词的在途读取作废
	s.view.List.Loading = true
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() {
		s.RefreshAll(ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		l := &s.view.List
		if l.Search != query || strings.TrimSpace(query) == "" || l.SelectedID == "" {
			return // 搜索词又变了，或搜索已清空：不动选中项
		}
		if !slices.ContainsFunc(l.Entries, func(e services.LibraryEntry) bool { return e.ID == l.SelectedID }) {
			l.SelectedID = ""
			s.wake()
		}
	})
}

// SetSort 改排序并回到第一页（渲染层 setSort）。
func (s *Store) SetSort(ctx context.Context, sortBy, order string) {
	s.mu.Lock()
	s.view.List.SortBy, s.view.List.SortOrder = sortBy, order
	s.resetPaging()
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.FetchList(ctx) })
}

// SetPage 翻到某一页（渲染层 setPage）。
func (s *Store) SetPage(ctx context.Context, page int) {
	s.mu.Lock()
	s.view.List.Page = max(1, page)
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.FetchList(ctx) })
}

// SetPageSize 改每页条数并回到第一页（渲染层 setPageSize：停留在原页码会越过总页数）。
func (s *Store) SetPageSize(ctx context.Context, size int) {
	s.mu.Lock()
	s.view.List.PageSize = max(1, size)
	s.resetPaging()
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.FetchList(ctx) })
}

// ---- 选中与多选 ----

// SelectItem 选中一条（渲染层 selectItem 的选中部分；详情的读取随详情栏迁入）。空串取消选中。
func (s *Store) SelectItem(id string) {
	s.mu.Lock()
	s.view.List.SelectedID = id
	s.mu.Unlock()
	s.wake()
}

// ToggleSelection 切换一条在多选集合里的有无，并把它设为 Shift 范围选择的起点（渲染层 toggleSelection）。
func (s *Store) ToggleSelection(id string) {
	s.mu.Lock()
	l := &s.view.List
	if i := slices.Index(l.Selection, id); i >= 0 {
		l.Selection = slices.Delete(slices.Clone(l.Selection), i, i+1)
	} else {
		l.Selection = append(slices.Clone(l.Selection), id)
	}
	l.Anchor = id
	s.mu.Unlock()
	s.wake()
}

// RangeSelectTo 从起点到这一条之间（含两端）全部选中（渲染层 rangeSelectTo）。起点缺省取多选起点、选中项、本页第一条；
// 起点或目标不在本页时，只选这一条。
func (s *Store) RangeSelectTo(id string) {
	s.mu.Lock()
	l := &s.view.List
	anchor := l.Anchor
	if anchor == "" {
		anchor = l.SelectedID
	}
	if anchor == "" && len(l.Entries) > 0 {
		anchor = l.Entries[0].ID
	}
	index := func(id string) int {
		return slices.IndexFunc(l.Entries, func(e services.LibraryEntry) bool { return e.ID == id })
	}
	from, to := index(anchor), index(id)
	if anchor == "" || from < 0 || to < 0 {
		l.Selection, l.Anchor = []string{id}, id
	} else {
		from, to = min(from, to), max(from, to)
		ids := make([]string, 0, to-from+1)
		for _, e := range l.Entries[from : to+1] {
			ids = append(ids, e.ID)
		}
		l.Selection, l.Anchor = ids, anchor
	}
	s.mu.Unlock()
	s.wake()
}

// SetSelection 整体设定多选集合，起点取最后一个（渲染层 setSelection）。
func (s *Store) SetSelection(ids []string) {
	s.mu.Lock()
	s.view.List.Selection = slices.Clone(ids)
	s.view.List.Anchor = ""
	if len(ids) > 0 {
		s.view.List.Anchor = ids[len(ids)-1]
	}
	s.mu.Unlock()
	s.wake()
}

// SelectPage 把本页全部条目并入多选集合（渲染层「本页」按钮）。
func (s *Store) SelectPage() {
	s.mu.Lock()
	merged := slices.Clone(s.view.List.Selection)
	for _, e := range s.view.List.Entries {
		if !slices.Contains(merged, e.ID) {
			merged = append(merged, e.ID)
		}
	}
	s.mu.Unlock()
	s.SetSelection(merged)
}

// TogglePageSelection 切换本页的全选（渲染层 ItemTableView 的 togglePageSelection，表头复选框与「本页」按钮共用）：
// 本页条目已全部在多选集合里就把本页取消，否则把本页并入；两条路径都经 SetSelection，起点取集合里最后一个。
func (s *Store) TogglePageSelection() {
	s.mu.Lock()
	page := s.view.List.Entries
	current := slices.Clone(s.view.List.Selection)
	allSelected := len(page) > 0
	for _, e := range page {
		if !slices.Contains(current, e.ID) {
			allSelected = false
			break
		}
	}
	var next []string
	if allSelected {
		for _, id := range current {
			if !slices.ContainsFunc(page, func(e services.LibraryEntry) bool { return e.ID == id }) {
				next = append(next, id)
			}
		}
	} else {
		next = current
		for _, e := range page {
			if !slices.Contains(next, e.ID) {
				next = append(next, e.ID)
			}
		}
	}
	s.mu.Unlock()
	s.SetSelection(next)
}

// ClearSelection 清空多选。
func (s *Store) ClearSelection() {
	s.mu.Lock()
	s.view.List.Selection, s.view.List.Anchor = nil, ""
	s.mu.Unlock()
	s.wake()
}
