package library

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测知识库列表工作区（卡片与表格两种视图共用的工具条、分页条、选择与读取失败）的界面级行为（台账：M2-2 余项账第 5 项）：
// 点了哪里、Store 与画面发生什么——按钮、菜单、下拉、复选框、点击修饰键怎样落到 Store，读取失败怎样重试，
// 还没迁入的操作怎样交代。Store 自己的规则在 store_list_test.go，窗口级键盘在 keyboard_test.go，表格窗口化在
// table_window_test.go，搜索命中词在 search_marks_test.go。
//
// 数据同 keyboard_test.go：服务里 GenerateEntries(100)，「全部」范围 80 条、每页 20 条，回收站 5 条。

// toTable 切到列表（表格）视图。
func (r *rig) toTable() {
	r.t.Helper()
	r.click(gfx.RoleButton, locales.LibraryViewList())
	r.frames(4)
}

// clickAtWith 按住修饰键点一下：无头会话的按键与指针共用同一份修饰键状态。
func (r *rig) clickAtWith(mod string, x, y float32) {
	r.t.Helper()
	if err := r.session.KeyDown(mod); err != nil {
		r.t.Fatalf("按下 %q 失败：%v", mod, err)
	}
	r.clickAt(x, y)
	if err := r.session.KeyUp(mod); err != nil {
		r.t.Fatalf("松开 %q 失败：%v", mod, err)
	}
	r.frames(2)
}

// ids 是本页第 from 到 to 条（含两端）的编号。
func (r *rig) ids(from, to int) []string {
	r.t.Helper()
	var out []string
	for _, e := range r.view().List.Entries[from : to+1] {
		out = append(out, e.ID)
	}
	return out
}

// rowBlank 是表格里某一行的空白处（标题钮右边的标题格空白）：点在这里落在整行的命中区上，不落在任何按钮上。
func (r *rig) rowBlank(e services.LibraryEntry) (x, y float32) {
	r.t.Helper()
	n, ok := r.cardOf(e)
	if !ok {
		r.t.Fatalf("找不到条目 %q 的标题钮", e.Title)
	}
	return n.Bounds.X + n.Bounds.Width + 40, n.Bounds.Y + n.Bounds.Height/2
}

// ---- 分页条 ----

// 「上一页」「下一页」翻页，在两端禁用；禁用的按钮点了不翻页；翻页读到的是另一批条目。
func TestPagerButtonsTurnThePageAndDisableAtTheEnds(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	page := func() int { return r.view().List.Page }
	prev := func() bool { return r.mustNode(gfx.RoleButton, locales.LibraryPreviousPage()).Disabled }
	next := func() bool { return r.mustNode(gfx.RoleButton, locales.LibraryNextPage()).Disabled }
	if !prev() || next() {
		t.Fatalf("第一页：「上一页」应禁用、「下一页」应可用，实际 %v / %v", prev(), next())
	}
	x, y := centerOf(r.mustNode(gfx.RoleButton, locales.LibraryPreviousPage()))
	r.clickAt(x, y)
	if page() != 1 {
		t.Fatalf("点禁用的「上一页」不该翻页，实际第 %d 页", page())
	}
	first := entryIDs(r.view().List)
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if page() != 2 || prev() {
		t.Fatalf("点「下一页」应到第 2 页并启用「上一页」，实际第 %d 页，上一页禁用 %v", page(), prev())
	}
	if slices.Equal(first, entryIDs(r.view().List)) {
		t.Fatal("第 2 页应是另一批条目")
	}
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if page() != 4 || !next() {
		t.Fatalf("到第 4 页（最后一页）：「下一页」应禁用，实际第 %d 页，下一页禁用 %v", page(), next())
	}
	x, y = centerOf(r.mustNode(gfx.RoleButton, locales.LibraryNextPage()))
	r.clickAt(x, y)
	if page() != 4 {
		t.Fatalf("点禁用的「下一页」不该翻页，实际第 %d 页", page())
	}
	r.click(gfx.RoleButton, locales.LibraryPreviousPage())
	if page() != 3 {
		t.Fatalf("点「上一页」应回到第 3 页，实际第 %d 页", page())
	}
}

// 表格的页码按钮：点哪页去哪页，当前页高亮（Selected），与上一页 / 下一页共用同一份页码。
func TestTablePageNumberButtonsJumpToThePage(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	numberSelected := func(n int) bool { return r.mustNode(gfx.RoleButton, locales.LibraryPageNumber(n)).Selected }
	if !numberSelected(1) || numberSelected(3) {
		t.Fatal("第 1 页应是当前页")
	}
	r.click(gfx.RoleButton, locales.LibraryPageNumber(3))
	if got := r.view().List.Page; got != 3 || !numberSelected(3) || numberSelected(1) {
		t.Fatalf("点「第 3 页」应去第 3 页并高亮它，实际第 %d 页", got)
	}
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if got := r.view().List.Page; got != 4 || !numberSelected(4) {
		t.Fatalf("页码与「下一页」共用页码：应到第 4 页，实际第 %d 页", got)
	}
	r.click(gfx.RoleButton, locales.LibraryPageNumber(1))
	if got := r.view().List.Page; got != 1 {
		t.Fatalf("点「第 1 页」应回到第 1 页，实际第 %d 页", got)
	}
}

// 页码窗口：表格里最多五颗页码按钮，以当前页为中心（前两页后两页），贴着两端时顶住，不留空位。
func TestTablePageNumbersFollowTheCurrentPageWindow(t *testing.T) {
	t.Parallel()
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(servicestest.GenerateEntries(300)) }) // 「全部」240 条，每页 20 条是 12 页
	r.toTable()
	shown := func() []int {
		var out []int
		for n := 1; n <= 12; n++ {
			if r.has(gfx.RoleButton, locales.LibraryPageNumber(n)) {
				out = append(out, n)
			}
		}
		return out
	}
	for _, c := range []struct {
		page int
		want []int
	}{
		{1, []int{1, 2, 3, 4, 5}}, {3, []int{1, 2, 3, 4, 5}}, {4, []int{2, 3, 4, 5, 6}},
		{6, []int{4, 5, 6, 7, 8}}, {11, []int{8, 9, 10, 11, 12}}, {12, []int{8, 9, 10, 11, 12}},
	} {
		r.store.SetPage(r.screen.Ctx, c.page)
		r.frames(3)
		if got := shown(); !slices.Equal(got, c.want) {
			t.Fatalf("第 %d 页：页码按钮应是 %v，实际 %v", c.page, c.want, got)
		}
	}
}

// 每页条数的下拉：选一个新的值改每页条数并回到第一页；选已经选着的值也一样回到第一页并重新读取
// （渲染层的 Select 对当前值照常触发 onChange，setPageSize 无条件回第一页并 fetchList，Viva 与它一致）。
func TestPageSizeSelectChangesThePageSizeAndReturnsToPageOne(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if got := r.view().List.Page; got != 3 {
		t.Fatalf("夹具：应在第 3 页，实际第 %d 页", got)
	}
	r.click(gfx.RoleCombobox, locales.LibraryPageSize())
	r.click(gfx.RoleOption, "50")
	l := r.view().List
	if l.PageSize != 50 || l.Page != 1 || len(l.Entries) != 50 {
		t.Fatalf("选 50：每页 %d，第 %d 页，本页 %d 条（应为 50 / 1 / 50）", l.PageSize, l.Page, len(l.Entries))
	}
	if got := r.mustNode(gfx.RoleCombobox, locales.LibraryPageSize()).Value; got != "50" {
		t.Fatalf("下拉上显示的值应是 50，实际 %q", got)
	}
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if got := r.view().List.Page; got != 2 {
		t.Fatalf("夹具：应在第 2 页，实际第 %d 页", got)
	}
	queries := len(r.svc.ListQueries())
	r.click(gfx.RoleCombobox, locales.LibraryPageSize())
	r.click(gfx.RoleOption, "50")
	if l, got := r.view().List, len(r.svc.ListQueries()); l.Page != 1 || l.PageSize != 50 || got != queries+1 {
		t.Fatalf("再选 50：应回到第 1 页并重新读取一次；实际第 %d 页，多发了 %d 次读取", l.Page, got-queries)
	}
}

// ---- 工具条 ----

func sortButtonName(label string) string { return locales.LibrarySortBy() + ": " + label }

// 排序菜单：六项，点一项改排序并回到第一页、菜单收起、按钮上换成新的名字；再点按钮是收起而不是重选。
func TestSortMenuChoosesAnOrderAndShowsItOnTheButton(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	trigger := sortButtonName(locales.LibrarySortUpdatedDesc())
	r.click(gfx.RoleCombobox, trigger)
	if !r.mustNode(gfx.RoleCombobox, trigger).Expanded {
		t.Fatal("点开之后排序按钮应是展开状态")
	}
	for _, label := range []func() string{
		locales.LibrarySortUpdatedDesc, locales.LibrarySortUpdatedAsc, locales.LibrarySortCreatedDesc,
		locales.LibrarySortCreatedAsc, locales.LibrarySortTitleAsc, locales.LibrarySortTitleDesc,
	} {
		if !r.has(gfx.RoleMenuItem, label()) {
			t.Errorf("排序菜单里缺「%s」", label())
		}
	}
	r.click(gfx.RoleMenuItem, locales.LibrarySortTitleAsc())
	l := r.view().List
	if l.SortBy != services.LibrarySortTitle || l.SortOrder != services.LibraryOrderAsc || l.Page != 1 {
		t.Fatalf("选「标题 A→Z」：按 %q %q，第 %d 页", l.SortBy, l.SortOrder, l.Page)
	}
	queries := r.svc.ListQueries()
	if q := queries[len(queries)-1]; q.SortBy != services.LibrarySortTitle || q.SortOrder != services.LibraryOrderAsc || q.Cursor != "" || q.Offset != 0 {
		t.Fatalf("新排序的第一页读取：%+v", q)
	}
	if r.has(gfx.RoleMenuItem, locales.LibrarySortTitleAsc()) {
		t.Fatal("选完之后菜单应收起")
	}
	named := sortButtonName(locales.LibrarySortTitleAsc())
	if !r.has(gfx.RoleCombobox, named) {
		t.Fatalf("按钮上应换成「%s」", named)
	}
	n := len(queries)
	r.click(gfx.RoleCombobox, named)
	r.click(gfx.RoleCombobox, named)
	if r.has(gfx.RoleMenuItem, locales.LibrarySortTitleAsc()) || len(r.svc.ListQueries()) != n {
		t.Fatal("再点一次按钮应收起菜单，不重新读取")
	}
	r.click(gfx.RoleCombobox, named)
	r.keys("Escape")
	if r.has(gfx.RoleMenuItem, locales.LibrarySortTitleAsc()) {
		t.Fatal("Esc 应收起菜单")
	}
}

// 搜索中排序由全文检索的相关度接管：按钮写「相关度」且禁用，点了不出菜单；清空搜索后恢复。
func TestSortButtonIsDisabledWhileSearching(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.store.SetSearch(r.screen.Ctx, "条目")
	r.frames(3)
	relevance := sortButtonName(locales.LibrarySortRelevance())
	n := r.mustNode(gfx.RoleCombobox, relevance)
	if !n.Disabled {
		t.Fatal("搜索中排序按钮应禁用")
	}
	x, y := centerOf(n)
	r.clickAt(x, y)
	if r.has(gfx.RoleMenuItem, locales.LibrarySortTitleAsc()) {
		t.Fatal("搜索中点排序按钮不该出菜单")
	}
	r.store.SetSearch(r.screen.Ctx, "")
	r.frames(3)
	if n := r.mustNode(gfx.RoleCombobox, sortButtonName(locales.LibrarySortUpdatedDesc())); n.Disabled {
		t.Fatal("清空搜索后排序按钮应恢复可用")
	}
}

// 视图切换：卡片 / 列表两枚按钮，当前的是 Selected；切换换了列表的画法，并把选择写进设置。
func TestViewToggleSwitchesBetweenCardsAndTableAndRemembersIt(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	inTable := func() bool { return r.has(gfx.RoleCheckbox, locales.LibrarySelectPageRows()) }
	if !r.mustNode(gfx.RoleButton, locales.LibraryViewCard()).Selected || inTable() {
		t.Fatal("一开始应是卡片视图")
	}
	r.toTable()
	if !inTable() || !r.mustNode(gfx.RoleButton, locales.LibraryViewList()).Selected || r.mustNode(gfx.RoleButton, locales.LibraryViewCard()).Selected {
		t.Fatal("点「列表视图」之后应是表格，列表按钮高亮")
	}
	if raw, ok := r.prefs.Get(settings.PrefLibraryViewMode); !ok || string(raw) != `"`+ViewList+`"` {
		t.Fatalf("视图模式应写进设置，实际 %s（有 %v）", raw, ok)
	}
	r.click(gfx.RoleButton, locales.LibraryViewCard())
	if inTable() || !r.mustNode(gfx.RoleButton, locales.LibraryViewCard()).Selected {
		t.Fatal("点「卡片视图」应回到卡片")
	}
	if raw, _ := r.prefs.Get(settings.PrefLibraryViewMode); string(raw) != `"`+ViewCard+`"` {
		t.Fatalf("回到卡片之后设置里应是卡片，实际 %s", raw)
	}
	writes := len(r.prefs.SetKeys())
	r.click(gfx.RoleButton, locales.LibraryViewCard())
	if got := len(r.prefs.SetKeys()); got != writes {
		t.Fatalf("点已经选着的视图不该再写设置，多写了 %d 次", got-writes)
	}
}

// 视图模式存不进设置：当前会话照常切换，原因只进日志（偏好丢了下次启动回到默认，不影响使用，所以不弹提示）。
func TestViewTogglePreferenceFailureOnlyLogs(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.prefs.FailSetWith(errors.New("磁盘已满"))
	r.toTable()
	if !r.has(gfx.RoleCheckbox, locales.LibrarySelectPageRows()) {
		t.Fatal("设置写不进去也应切到表格")
	}
	if logs := strings.Join(r.log.entries(), "\n"); !strings.Contains(logs, "磁盘已满") {
		t.Fatalf("写入失败的原因应进日志，实际 %q", logs)
	}
	if r.notifier.Len() != 0 {
		t.Fatalf("后台失败不该弹提示，实际 %d 条", r.notifier.Len())
	}
}

// 组合筛选：叠加两个以上分面才出现入口；点开列出各条件，逐条取消或一键清空，取消后弹层收起。
func TestCombinedFiltersListTheFacetsAndCanBeRemovedOneByOne(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	entry := locales.LibraryCombinedFilters(2)
	if r.has(gfx.RoleButton, entry) {
		t.Fatal("没有叠加分面时不该有组合筛选入口")
	}
	r.store.SelectCollection(r.screen.Ctx, "c1")
	r.frames(3)
	if r.has(gfx.RoleButton, locales.LibraryCombinedFilters(1)) || r.has(gfx.RoleButton, entry) {
		t.Fatal("只有一个分面时不该有组合筛选入口")
	}
	r.store.SelectTag(r.screen.Ctx, "t1")
	r.frames(3)
	r.click(gfx.RoleButton, entry)
	if !r.mustNode(gfx.RoleButton, entry).Expanded {
		t.Fatal("点开之后入口应是展开状态")
	}
	removeCollection := locales.LibraryRemoveFilter(locales.LibraryCollection(), "工作")
	removeTag := locales.LibraryRemoveFilter(locales.LibraryTags(), "go")
	if !r.has(gfx.RoleButton, removeCollection) || !r.has(gfx.RoleButton, removeTag) {
		t.Fatalf("弹层里应列出合集与标签两个条件；画面文字 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, removeTag)
	if f := r.view().Filters; f.TagID != "" || f.CollectionID != "c1" {
		t.Fatalf("取消标签条件：应只剩合集，实际 %+v", f)
	}
	if r.has(gfx.RoleButton, removeCollection) || r.has(gfx.RoleButton, entry) {
		t.Fatal("取消之后弹层应收起，只剩一个分面时入口也应消失")
	}
	r.store.SelectTag(r.screen.Ctx, "t1")
	r.frames(3)
	r.click(gfx.RoleButton, entry)
	r.click(gfx.RoleButton, locales.LibraryClearFilters())
	if f := r.view().Filters; f.CollectionID != "" || f.TagID != "" {
		t.Fatalf("清空筛选应取消全部分面，实际 %+v", f)
	}
	if r.has(gfx.RoleButton, entry) {
		t.Fatal("清空之后入口应消失")
	}
}

// ---- 选择 ----

// 卡片：普通点击选中并清掉多选；Ctrl / Meta 点击切换勾选；Shift 点击选起点到它之间的全部。
func TestCardClicksSelectAndModifiersExtendTheSelection(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	entries := r.view().List.Entries
	pos := func(i int) (float32, float32) {
		n, ok := r.cardOf(entries[i])
		if !ok {
			t.Fatalf("第 %d 张卡片不在画面里", i)
		}
		return centerOf(n)
	}
	x, y := pos(2)
	r.clickAt(x, y)
	if l := r.view().List; l.SelectedID != entries[2].ID || len(l.Selection) != 0 {
		t.Fatalf("普通点击应选中第 2 条且没有多选，实际 %q 多选 %v", l.SelectedID, l.Selection)
	}
	x, y = pos(4)
	r.clickAtWith("ctrl", x, y)
	if l := r.view().List; !slices.Equal(l.Selection, []string{entries[4].ID}) || l.SelectedID != entries[2].ID {
		t.Fatalf("Ctrl 点击第 4 条：应进多选、选中项不动，实际多选 %v 选中 %q", l.Selection, l.SelectedID)
	}
	r.clickAtWith("ctrl", x, y)
	if l := r.view().List; len(l.Selection) != 0 {
		t.Fatalf("再 Ctrl 点击一次应取消勾选，实际 %v", l.Selection)
	}
	r.clickAtWith("cmd", x, y)
	if l := r.view().List; !slices.Equal(l.Selection, []string{entries[4].ID}) {
		t.Fatalf("Meta 点击与 Ctrl 点击一样切换勾选，实际 %v", l.Selection)
	}
	x, y = pos(7)
	r.clickAtWith("shift", x, y)
	if l := r.view().List; !slices.Equal(l.Selection, r.ids(4, 7)) || l.Anchor != entries[4].ID {
		t.Fatalf("Shift 点击第 7 条：应选第 4 到 7 条、起点不动，实际 %v 起点 %q", l.Selection, l.Anchor)
	}
	x, y = pos(1)
	r.clickAt(x, y)
	if l := r.view().List; len(l.Selection) != 0 || l.SelectedID != entries[1].ID {
		t.Fatalf("有多选时普通点击：应清掉多选并选中被点的，实际多选 %v 选中 %q", l.Selection, l.SelectedID)
	}
}

// 表格：点行里的空白处与卡片一样选中；标题钮选中这一行；Ctrl / Shift 点击行扩展多选。
func TestTableRowClicksSelectAndModifiersExtendTheSelection(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	entries := r.view().List.Entries
	x, y := r.rowBlank(entries[1])
	r.clickAt(x, y)
	if l := r.view().List; l.SelectedID != entries[1].ID || len(l.Selection) != 0 {
		t.Fatalf("点行里的空白处应选中这一行，实际 %q 多选 %v", l.SelectedID, l.Selection)
	}
	title, _ := r.cardOf(entries[2])
	tx, ty := centerOf(title)
	r.clickAt(tx, ty)
	if got := r.view().List.SelectedID; got != entries[2].ID {
		t.Fatalf("点标题钮应选中这一行，实际 %q", got)
	}
	x, y = r.rowBlank(entries[4])
	r.clickAtWith("ctrl", x, y)
	if l := r.view().List; !slices.Equal(l.Selection, []string{entries[4].ID}) || l.SelectedID != entries[2].ID {
		t.Fatalf("Ctrl 点击第 4 行：应进多选、选中项不动，实际多选 %v 选中 %q", l.Selection, l.SelectedID)
	}
	x, y = r.rowBlank(entries[7])
	r.clickAtWith("shift", x, y)
	if l := r.view().List; !slices.Equal(l.Selection, r.ids(4, 7)) {
		t.Fatalf("Shift 点击第 7 行：应选第 4 到 7 行，实际 %v", l.Selection)
	}
	x, y = r.rowBlank(entries[9])
	r.clickAtWith("cmd", x, y)
	if l := r.view().List; !slices.Equal(l.Selection, append(r.ids(4, 7), entries[9].ID)) {
		t.Fatalf("Meta 点击第 9 行与 Ctrl 点击一样切换勾选，实际 %v", l.Selection)
	}
}

// 表格的复选框：行首的复选框切换这一行的勾选，表头的复选框与「本页」按钮切换整页；
// 卡片视图里的「本页」只并入、不切换（渲染层两个视图本来就不同）。
func TestTableCheckboxesAndPageSelection(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	entries := r.view().List.Entries
	rowCheck := locales.LibrarySelectRow(entries[1].Title)
	r.click(gfx.RoleCheckbox, rowCheck)
	if got := r.view().List.Selection; !slices.Equal(got, []string{entries[1].ID}) {
		t.Fatalf("点行首复选框应勾选这一行，实际 %v", got)
	}
	if !r.mustNode(gfx.RoleCheckbox, rowCheck).Checked {
		t.Fatal("勾选之后复选框应是选中状态")
	}
	r.click(gfx.RoleCheckbox, rowCheck)
	if got := r.view().List.Selection; len(got) != 0 {
		t.Fatalf("再点一次应取消勾选，实际 %v", got)
	}
	pageAll := locales.LibrarySelectPageRows()
	r.click(gfx.RoleCheckbox, pageAll)
	if got := r.view().List.Selection; len(got) != 20 {
		t.Fatalf("点表头复选框应勾选本页 20 行，实际 %d", len(got))
	}
	if !r.mustNode(gfx.RoleCheckbox, pageAll).Checked {
		t.Fatal("本页全部勾选后表头复选框应是选中状态")
	}
	r.click(gfx.RoleCheckbox, pageAll)
	if got := r.view().List.Selection; len(got) != 0 {
		t.Fatalf("再点表头复选框应取消本页，实际 %d", len(got))
	}
	r.click(gfx.RoleButton, locales.LibrarySelectPage())
	r.click(gfx.RoleButton, locales.LibrarySelectPage())
	if got := r.view().List.Selection; len(got) != 0 {
		t.Fatalf("表格里的「本页」是切换：点两次应回到没有勾选，实际 %d", len(got))
	}

	cards := wsRig(t)
	cards.click(gfx.RoleButton, locales.LibrarySelectPage())
	cards.click(gfx.RoleButton, locales.LibrarySelectPage())
	if got := cards.view().List.Selection; len(got) != 20 {
		t.Fatalf("卡片视图里的「本页」只并入：点两次仍是 20 条，实际 %d", len(got))
	}
}

// 整页勾选的边界：本页只勾了一部分时，表头复选框把本页补齐（不是取消）；翻到别的页再整页勾选、再取消，只动本页，别页的勾选保留。
func TestTablePageSelectionCompletesPartialPagesAndKeepsOtherPages(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	first := r.view().List.Entries
	r.click(gfx.RoleCheckbox, locales.LibrarySelectRow(first[1].Title))
	pageAll := locales.LibrarySelectPageRows()
	r.click(gfx.RoleCheckbox, pageAll)
	if got := r.view().List.Selection; len(got) != 20 {
		t.Fatalf("本页只勾了一行时点表头复选框应补齐本页 20 行，实际 %d", len(got))
	}
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if got := r.view().List.Selection; len(got) != 20 {
		t.Fatalf("翻页不该改勾选，实际 %d", len(got))
	}
	r.click(gfx.RoleCheckbox, pageAll)
	if got := r.view().List.Selection; len(got) != 40 {
		t.Fatalf("第 2 页再整页勾选应并入成 40 行，实际 %d", len(got))
	}
	r.click(gfx.RoleCheckbox, pageAll)
	if got := r.view().List.Selection; len(got) != 20 || !slices.Contains(got, first[1].ID) {
		t.Fatalf("取消第 2 页应只去掉本页的 20 行、第 1 页的勾选保留，实际 %d 行", len(got))
	}
}

// ---- 读取失败与空态 ----

// 读取失败：写出原因（不是空态）、可以重试；重试仍失败时原因保持、还能再试；恢复之后重试把列表读回来。
func TestLoadFailureShowsTheReasonAndRetryReloadsTheList(t *testing.T) {
	t.Parallel()
	for _, view := range []string{ViewCard, ViewList} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			r := wsRig(t, func(r *rig) { r.svc.FailOn("List", errors.New("数据库繁忙")) })
			if view == ViewList {
				r.toTable()
			}
			if !r.hasText("数据库繁忙") || !r.has(gfx.RoleButton, locales.CommonRetry()) {
				t.Fatalf("读取失败应写出原因并给出重试；画面文字 %q", r.session.Texts())
			}
			if r.hasText(locales.LibraryListEmpty()) {
				t.Fatal("读取失败不能画成「暂无条目」的空态")
			}
			r.click(gfx.RoleButton, locales.CommonRetry())
			if !r.hasText("数据库繁忙") || !r.has(gfx.RoleButton, locales.CommonRetry()) {
				t.Fatalf("重试仍失败时原因应保持、还能再试；画面文字 %q", r.session.Texts())
			}
			r.svc.FailOn("List", nil)
			r.click(gfx.RoleButton, locales.CommonRetry())
			l := r.view().List
			if l.LoadError != "" || len(l.Entries) != 20 || r.has(gfx.RoleButton, locales.CommonRetry()) || r.hasText("数据库繁忙") {
				t.Fatalf("恢复之后重试应读回列表：错误 %q，%d 条", l.LoadError, len(l.Entries))
			}
		})
	}
}

// 读到过列表之后翻页失败：列表留在画面上（不换成失败态），原因进日志。
func TestLoadFailureAfterEntriesWereReadKeepsTheListOnScreen(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	entries := r.view().List.Entries
	r.svc.FailOn("List", errors.New("磁盘错误"))
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if _, ok := r.cardOf(entries[0]); !ok {
		t.Fatal("翻页失败之后读到过的卡片应还在画面上")
	}
	if r.has(gfx.RoleButton, locales.CommonRetry()) {
		t.Fatal("读到过列表之后失败不该换成整块的失败态")
	}
	if logs := strings.Join(r.log.entries(), "\n"); !strings.Contains(logs, "磁盘错误") {
		t.Fatalf("失败原因应进日志，实际 %q", logs)
	}
}

// 读取进行中、已有条目（翻页、改排序……）：列表留在画面上，转圈只在没有条目时才画，卡片与表格都一样。
func TestReloadingKeepsTheEntriesOnScreenInBothViews(t *testing.T) {
	t.Parallel()
	for _, view := range []string{ViewCard, ViewList} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			r := wsRig(t)
			if view == ViewList {
				r.toTable()
			}
			first := r.view().List.Entries[0]
			n := len(r.svc.ListQueries())
			release := r.svc.HoldList() // 下一次读取卡住：翻页的读取在途
			done := make(chan struct{})
			go func() { r.store.SetPage(r.screen.Ctx, 2); close(done) }()
			waitFor(t, "第二页的读取发出", func() bool { return len(r.svc.ListQueries()) == n+1 })
			r.frames(3)
			if !r.view().List.Loading {
				t.Fatal("夹具：读取应在进行中")
			}
			if _, ok := r.cardOf(first); !ok {
				t.Fatalf("读取进行中、已有条目：列表应留在画面上，不换成转圈（渲染层只在没有条目时才画转圈）；画面文字 %q", r.session.Texts())
			}
			release()
			<-done
			r.frames(3)
			if l := r.view().List; l.Loading || l.Page != 2 || len(l.Entries) == 0 {
				t.Fatalf("读完之后应是第 2 页的条目：%+v", l.Page)
			}
		})
	}
}

// 空态：「全部」里没有条目写「暂无条目」，回收站里没有条目写回收站自己的文案；回收站空时不给「清空回收站」。
func TestEmptyListNamesTheTrashSeparately(t *testing.T) {
	t.Parallel()
	for _, view := range []string{ViewCard, ViewList} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			r := wsRig(t, func(r *rig) { r.svc.SeedEntries(nil) })
			if view == ViewList {
				r.toTable()
			}
			if !r.hasText(locales.LibraryListEmpty()) || r.hasText(locales.LibraryTrashEmpty()) {
				t.Fatalf("「全部」里没有条目应写「暂无条目」；画面文字 %q", r.session.Texts())
			}
			if r.has(gfx.RoleCombobox, locales.LibraryPageSize()) || r.has(gfx.RoleButton, locales.LibraryNextPage()) {
				t.Fatal("没有条目时不画分页条")
			}
			r.click(gfx.RoleButton, locales.LibraryScopeTrash())
			if !r.hasText(locales.LibraryTrashEmpty()) || r.hasText(locales.LibraryListEmpty()) {
				t.Fatalf("回收站里没有条目应写回收站的文案；画面文字 %q", r.session.Texts())
			}
			if r.has(gfx.RoleButton, locales.LibraryEmptyTrash()) {
				t.Fatal("回收站是空的，不该有「清空回收站」")
			}
		})
	}
}

// clickInList 点工作区里（侧栏右边）第一个角色与名字都对得上的节点：侧栏里也有同名的「更多操作」。
func (r *rig) clickInList(role gfx.Role, name string) {
	r.t.Helper()
	for _, n := range r.session.Access() {
		if n.Role == role && n.Label == name && n.Bounds.X >= 240 {
			x, y := centerOf(n)
			r.clickAt(x, y)
			return
		}
	}
	r.t.Fatalf("工作区里找不到 %v「%s」", role, name)
}

// ---- 列设置 ----

// 列设置：点触发钮开菜单；点一列切换显示（菜单保持打开）；列的显示与宽度写进设置；「重置」恢复默认并收起；Esc 与再点触发钮收起。
func TestColumnMenuTogglesColumnsKeepsOpenAndResets(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	shown := func(name string) bool { return r.has(gfx.RoleSeparator, locales.LibraryResizeColumn(name)) }
	menuOpen := func() bool { return r.has(gfx.RoleMenu, locales.LibraryColumnConfig()) }
	if !shown("摘要") || shown("合集") {
		t.Fatal("默认应显示「摘要」、不显示「合集」")
	}
	if menuOpen() {
		t.Fatal("菜单一开始应是收起的")
	}
	r.click(gfx.RoleCombobox, locales.LibraryColumnConfig())
	if !menuOpen() || !r.mustNode(gfx.RoleCombobox, locales.LibraryColumnConfig()).Expanded {
		t.Fatal("点触发钮应打开菜单、触发钮展开")
	}
	if !r.mustNode(gfx.RoleMenuItem, "摘要").Selected || r.mustNode(gfx.RoleMenuItem, "合集").Selected {
		t.Fatal("菜单里显示中的列应是选中状态")
	}
	r.click(gfx.RoleMenuItem, "摘要")
	r.click(gfx.RoleMenuItem, "合集")
	if shown("摘要") || !shown("合集") || !menuOpen() {
		t.Fatalf("隐藏「摘要」、显示「合集」，菜单保持打开：摘要 %v，合集 %v，菜单 %v", shown("摘要"), shown("合集"), menuOpen())
	}
	if r.mustNode(gfx.RoleMenuItem, "摘要").Selected || !r.mustNode(gfx.RoleMenuItem, "合集").Selected {
		t.Fatal("菜单里的选中状态应跟着列的显示变化")
	}
	raw, ok := r.prefs.Get(settings.PrefLibraryColumns)
	if !ok || !strings.Contains(string(raw), `"id":"collection","visible":true`) || !strings.Contains(string(raw), `"id":"snippet","visible":false`) {
		t.Fatalf("列配置应写进设置，实际 %s（有 %v）", raw, ok)
	}
	r.click(gfx.RoleButton, locales.CommonReset())
	if !shown("摘要") || shown("合集") || menuOpen() {
		t.Fatalf("「重置」应恢复默认列并收起菜单：摘要 %v，合集 %v，菜单 %v", shown("摘要"), shown("合集"), menuOpen())
	}
	if raw, ok := r.prefs.Get(settings.PrefLibraryColumns); !ok || strings.TrimSpace(string(raw)) != "null" {
		t.Fatalf("「重置」应把列偏好写成 null（读回来当作没有偏好，渲染层是删掉 localStorage 的键），实际 %s（有 %v）", raw, ok)
	}
	r.click(gfx.RoleCombobox, locales.LibraryColumnConfig())
	r.keys("Escape")
	if menuOpen() {
		t.Fatal("Esc 应收起菜单")
	}
	r.click(gfx.RoleCombobox, locales.LibraryColumnConfig())
	r.click(gfx.RoleCombobox, locales.LibraryColumnConfig())
	if menuOpen() {
		t.Fatal("再点触发钮应收起菜单")
	}
}
