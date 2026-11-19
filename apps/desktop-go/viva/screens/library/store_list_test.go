package library

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
)

// 本文件对应渲染层 stores/knowledge.store.ts 的列表部分：分页与游标、排序、搜索、选中与多选、请求的乱序保护。
// 数据用 servicestest.GenerateEntries(100)：「全部」范围下有 80 条（20 条归档不算），每页 20 条是四页。

const listTotal = 80

func listHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.svc.SeedEntries(servicestest.GenerateEntries(100))
	return h
}

func entryIDs(v ListView) []string {
	out := make([]string, len(v.Entries))
	for i, e := range v.Entries {
		out[i] = e.ID
	}
	return out
}

func lastListQuery(h *harness) services.LibraryListQuery {
	h.t.Helper()
	qs := h.svc.ListQueries()
	if len(qs) == 0 {
		h.t.Fatal("没有收到列表查询")
	}
	return qs[len(qs)-1]
}

// 挂载时读第一页：默认每页 20 条、按更新时间新的在前、没有游标；读完加载态收起，总数是符合筛选的总数。
func TestListLoadsTheFirstPageWithDefaults(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	v := h.store.View().List
	if len(v.Entries) != 20 || v.Total != listTotal || v.Page != 1 || v.Loading || v.LoadError != "" {
		t.Fatalf("第一页：条数 %d，总数 %d，页 %d，加载 %v，错误 %q", len(v.Entries), v.Total, v.Page, v.Loading, v.LoadError)
	}
	q := h.svc.ListQueries()[0]
	if q.Scope != services.LibraryScopeAll || q.Limit != 20 || q.SortBy != services.LibrarySortUpdatedAt ||
		q.SortOrder != services.LibraryOrderDesc || q.Cursor != "" || q.Offset != 0 || q.Search != "" {
		t.Fatalf("第一页的查询：%+v", q)
	}
	if !v.Entries[0].IsPinned {
		t.Fatalf("置顶条目应排在最前：%v", entryIDs(v)[:3])
	}
}

// 读取开始之前就是加载态：挂载的第一帧不能是「暂无条目」。
func TestListIsLoadingFromTheMomentOfMounting(t *testing.T) {
	h := listHarness(t)
	h.store.Spawn = func(fn func()) {} // 读取还没开始
	h.store.Load(h.ctx)
	if v := h.store.View().List; !v.Loading || len(v.Entries) != 0 {
		t.Fatalf("挂载之后读取开始之前应是加载态：%+v", v)
	}
}

// 翻页：第二页用第一页给的游标；直接跳到没有游标的页用偏移；回翻时游标仍有效。
func TestListPagingUsesCursorsThenOffsets(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	first := entryIDs(h.store.View().List)

	h.store.SetPage(h.ctx, 2)
	q := lastListQuery(h)
	if q.Cursor == "" || q.Offset != 0 {
		t.Fatalf("第二页应带第一页给的游标：%+v", q)
	}
	second := entryIDs(h.store.View().List)
	if slices.Equal(first, second) || h.store.View().List.Page != 2 {
		t.Fatalf("第二页应是另一批条目：%v / %v", first, second)
	}

	h.store.SetPage(h.ctx, 4) // 第四页的游标还不知道：退回偏移
	if q := lastListQuery(h); q.Cursor != "" || q.Offset != 60 {
		t.Fatalf("没有游标的页应退回偏移：%+v", q)
	}
	if v := h.store.View().List; len(v.Entries) != 20 || v.Page != 4 {
		t.Fatalf("第四页：%+v", v.Page)
	}

	h.store.SetPage(h.ctx, 0) // 页码最小是 1
	if v := h.store.View().List; v.Page != 1 || !slices.Equal(entryIDs(v), first) {
		t.Fatalf("回到第一页应读回同一批条目：%d", v.Page)
	}
}

// 台账 B-033 (2)(3)：筛选一变，列表回到第一页、丢掉全部游标、清掉选中与多选；旧筛选在途的读取作废，
// 晚到的结果不能盖住新筛选的列表；下一次读取不能带旧游标（后端会拒绝）。
func TestListFilterChangeResetsPagingAndDropsStaleResponses(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	h.store.SetPage(h.ctx, 2)
	h.store.SelectItem("item-000001")
	h.store.ToggleSelection("item-000002")

	release := h.svc.HoldList() // 下一次读取（旧筛选的第三页）卡住
	done := make(chan struct{})
	go func() { h.store.SetPage(h.ctx, 3); close(done) }()
	waitFor(t, "旧筛选的第三页发出", func() bool { return len(h.svc.ListQueries()) == 3 })

	h.store.SetScope(h.ctx, services.LibraryScopeFavorites) // 同步 Spawn：新筛选的读取立刻完成
	v := h.store.View().List
	if v.Page != 1 || v.SelectedID != "" || len(v.Selection) != 0 || v.Anchor != "" {
		t.Fatalf("筛选变化应回到第一页并清掉选中：%+v", v)
	}
	if q := lastListQuery(h); q.Scope != services.LibraryScopeFavorites || q.Cursor != "" || q.Offset != 0 {
		t.Fatalf("新筛选的第一页不能带旧游标或偏移：%+v", q)
	}
	favorites := entryIDs(v)

	release() // 旧筛选的第三页此刻才返回
	<-done
	if after := h.store.View().List; !slices.Equal(entryIDs(after), favorites) || after.Page != 1 {
		t.Fatalf("旧筛选的晚到结果不应覆盖新筛选：%v", entryIDs(after))
	}
}

// 当前页越界（删光了最后一页）：回退到最后一页重取，而不是显示空列表。
func TestListOutOfRangePageFallsBackToTheLastPage(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	h.store.SetPage(h.ctx, 9)
	v := h.store.View().List
	if v.Page != 4 || len(v.Entries) != 20 || v.LoadError != "" {
		t.Fatalf("应回退到第四页：页 %d，条数 %d，错误 %q", v.Page, len(v.Entries), v.LoadError)
	}
}

// 读取失败：记下原因并写日志；已经读到的条目保留；重试成功后原因清掉。
func TestListLoadErrorKeepsEntriesAndClearsOnRetry(t *testing.T) {
	h := listHarness(t)
	h.svc.FailOn("List", errors.New("数据库繁忙"))
	h.store.Load(h.ctx)
	v := h.store.View().List
	if v.LoadError != "数据库繁忙" || v.Loading || len(v.Entries) != 0 {
		t.Fatalf("从没读到过：应有错误原因、没有条目：%+v", v)
	}
	if logs := h.log.entries(); !slices.ContainsFunc(logs, func(l string) bool { return strings.Contains(l, "加载知识条目列表失败") }) {
		t.Fatalf("失败要写日志：%v", logs)
	}

	h.svc.FailOn("List", nil)
	h.store.FetchList(h.ctx)
	if v := h.store.View().List; v.LoadError != "" || len(v.Entries) != 20 {
		t.Fatalf("重试成功后应清掉错误：%+v", v.LoadError)
	}

	h.svc.FailOn("List", errors.New("又失败了"))
	h.store.FetchList(h.ctx)
	if v := h.store.View().List; v.LoadError != "又失败了" || len(v.Entries) != 20 {
		t.Fatalf("读到过之后失败：条目保留、记下原因：%d %q", len(v.Entries), v.LoadError)
	}
}

// 排序与每页条数都回到第一页（停留在原页码会越过总页数）。
func TestListSortAndPageSizeGoBackToThePageOne(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	h.store.SetPage(h.ctx, 3)

	h.store.SetSort(h.ctx, services.LibrarySortTitle, services.LibraryOrderAsc)
	q := lastListQuery(h)
	if v := h.store.View().List; v.Page != 1 || q.SortBy != services.LibrarySortTitle || q.SortOrder != services.LibraryOrderAsc || q.Cursor != "" {
		t.Fatalf("改排序：页 %d，查询 %+v", v.Page, q)
	}

	h.store.SetPage(h.ctx, 2)
	h.store.SetPageSize(h.ctx, 50)
	q = lastListQuery(h)
	if v := h.store.View().List; v.Page != 1 || v.PageSize != 50 || q.Limit != 50 || q.Cursor != "" || len(v.Entries) != 50 {
		t.Fatalf("改每页条数：页 %d，每页 %d，条数 %d，查询 %+v", v.Page, v.PageSize, len(v.Entries), q)
	}
}

// 搜索：回到第一页、清掉多选，发给后端的是去掉首尾空白的词，侧栏计数也带上它；
// 结果出来后详情若还停在不在结果里的条目上就清掉；结果里有它就保留；清空搜索不动选中项。
func TestListSearchResetsPagingAndClearsDetailOutsideResults(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	h.store.SetPage(h.ctx, 2)
	h.store.SelectItem("item-000001")
	h.store.ToggleSelection("item-000002")

	h.store.SetSearch(h.ctx, "  条目 7  ")
	q := lastListQuery(h)
	v := h.store.View().List
	if v.Page != 1 || q.Search != "条目 7" || q.Cursor != "" || len(v.Selection) != 0 || v.Search != "  条目 7  " {
		t.Fatalf("搜索：页 %d，查询 %+v，多选 %v，原文 %q", v.Page, q, v.Selection, v.Search)
	}
	counts := h.svc.CountQueries()
	if counts[len(counts)-1].Search != "条目 7" {
		t.Fatalf("侧栏计数也要带搜索词：%+v", counts[len(counts)-1])
	}
	if v.SelectedID != "" {
		t.Fatalf("详情停在不在结果里的条目上，应清掉：%q", v.SelectedID)
	}

	h.store.SelectItem("item-000007") // 「条目 7」命中的条目（标题 "条目 7"）
	h.store.SetSearch(h.ctx, "条目 7")
	if got := h.store.View().List.SelectedID; got != "item-000007" {
		t.Fatalf("选中项在结果里，应保留：%q", got)
	}

	h.store.SetSearch(h.ctx, "")
	if got := h.store.View().List.SelectedID; got != "item-000007" {
		t.Fatalf("清空搜索不动选中项：%q", got)
	}
	if q := lastListQuery(h); q.Search != "" {
		t.Fatalf("清空搜索后应读回完整列表：%+v", q)
	}
}

// 响应乱序：先发出的慢请求后到，不能覆盖后发出的。
func TestListLateResponseDoesNotOverwriteANewerOne(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)

	release := h.svc.HoldList()
	done := make(chan struct{})
	go func() { h.store.SetSort(h.ctx, services.LibrarySortCreatedAt, services.LibraryOrderAsc); close(done) }()
	waitFor(t, "第一次排序的请求发出", func() bool { return len(h.svc.ListQueries()) == 2 })

	h.store.SetSort(h.ctx, services.LibrarySortTitle, services.LibraryOrderAsc)
	byTitle := entryIDs(h.store.View().List)
	release()
	<-done
	if got := entryIDs(h.store.View().List); !slices.Equal(got, byTitle) {
		t.Fatalf("旧请求晚到不应覆盖新结果：%v", got[:3])
	}
}

// 多选：Ctrl 点击切换并把它设为起点；Shift 点击选起点到它之间的全部（含两端，起点不动）；
// 起点缺省取选中项、再缺省取本页第一条；起点或目标不在本页时只选目标；「本页」并入全部；清空。
func TestListSelectionRules(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	ids := entryIDs(h.store.View().List)

	h.store.ToggleSelection(ids[2])
	h.store.ToggleSelection(ids[5])
	if v := h.store.View().List; !slices.Equal(v.Selection, []string{ids[2], ids[5]}) || v.Anchor != ids[5] {
		t.Fatalf("Ctrl 点击：%v 起点 %q", v.Selection, v.Anchor)
	}
	h.store.ToggleSelection(ids[2])
	if v := h.store.View().List; !slices.Equal(v.Selection, []string{ids[5]}) || v.Anchor != ids[2] {
		t.Fatalf("再点一次取消，起点仍设为它：%v 起点 %q", v.Selection, v.Anchor)
	}

	h.store.RangeSelectTo(ids[8])
	if v := h.store.View().List; !slices.Equal(v.Selection, ids[2:9]) || v.Anchor != ids[2] {
		t.Fatalf("Shift 点击：应选 2..8，起点不动：%v 起点 %q", v.Selection, v.Anchor)
	}
	h.store.RangeSelectTo(ids[0]) // 反方向
	if v := h.store.View().List; !slices.Equal(v.Selection, ids[0:3]) {
		t.Fatalf("反方向的范围：%v", v.Selection)
	}

	h.store.ClearSelection()
	h.store.SelectItem(ids[4])
	h.store.RangeSelectTo(ids[6])
	if v := h.store.View().List; !slices.Equal(v.Selection, ids[4:7]) {
		t.Fatalf("没有起点时取选中项：%v", v.Selection)
	}

	h.store.ClearSelection()
	h.store.SelectItem("")
	h.store.RangeSelectTo(ids[3])
	if v := h.store.View().List; !slices.Equal(v.Selection, ids[0:4]) {
		t.Fatalf("没有起点也没有选中项：取本页第一条：%v", v.Selection)
	}

	h.store.RangeSelectTo("不在本页")
	if v := h.store.View().List; !slices.Equal(v.Selection, []string{"不在本页"}) || v.Anchor != "不在本页" {
		t.Fatalf("目标不在本页：只选目标：%v", v.Selection)
	}

	h.store.SetSelection([]string{ids[1]})
	h.store.SelectPage()
	v := h.store.View().List
	if len(v.Selection) != 20 || v.Selection[0] != ids[1] || v.Anchor != v.Selection[19] || !v.IsSelected(ids[19]) {
		t.Fatalf("「本页」：并入全部且不重复，原有的排在前面：%d", len(v.Selection))
	}
	h.store.ClearSelection()
	if v := h.store.View().List; len(v.Selection) != 0 || v.Anchor != "" {
		t.Fatalf("清空：%+v", v.Selection)
	}
}

// 翻页保留多选（渲染层「翻页保留冻结的 ID」）：跨页选择是批量操作的基础。
func TestListPagingKeepsTheSelection(t *testing.T) {
	h := listHarness(t)
	h.store.Load(h.ctx)
	id := entryIDs(h.store.View().List)[1]
	h.store.ToggleSelection(id)
	h.store.SetPage(h.ctx, 2)
	if v := h.store.View().List; !slices.Equal(v.Selection, []string{id}) {
		t.Fatalf("翻页应保留多选：%v", v.Selection)
	}
}
