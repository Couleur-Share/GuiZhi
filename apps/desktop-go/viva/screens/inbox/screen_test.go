package inbox

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件是处理中心界面的 L4 测试：侧栏筛选、主体的四种状态、批量选择与整理、AI 智能归类、单行与聚合行的动作、
// 局部错误不挡主列表。状态机本身（刷新代次、整理、归类的细节）见 store_test.go 与 store_actions_test.go，这里只验证界面把它接对了。

// filterName 是侧栏某一行的可访问名：名称加计数。
func filterName(label string, count int) string { return label + " " + strconv.Itoa(count) }

// boardItems 是三种分组各有内容的列表：2 条复核、1 条失败的采集问题、2 条未归合集。
func boardItems() []services.InboxItem {
	return []services.InboxItem{
		review("r1", "复核一", "标题缺失", "正文过短"),
		review("r2", "复核二"),
		importIssue("t1", services.ImportStatusFailed, "网络超时"),
		unclassified("u1", "未归一"),
		unclassified("u2", "未归二"),
	}
}

// ---- 侧栏 ----

func TestSidebarShowsSixFiltersWithCountsAndGroupHeadings(t *testing.T) {
	r := newUIRig(t, boardItems())
	v := r.store.View()
	want := []struct {
		label string
		count int
	}{
		{locales.InboxFilterAll(), v.Total},
		{locales.InboxFilterReviewRequired(), 2},
		{locales.InboxFilterImportIssue(), 1},
		{locales.InboxFilterUnclassified(), 2},
		{locales.InboxFilterSemanticPending(), 0},
		{locales.InboxFilterWikiPending(), 0},
	}
	for _, w := range want {
		if !r.has(gfx.RoleButton, filterName(w.label, w.count)) {
			t.Errorf("侧栏缺少筛选行「%s」", filterName(w.label, w.count))
		}
	}
	for _, heading := range []string{strings.ToUpper(locales.NavInbox()), locales.InboxOptionalSection(), locales.InboxBackgroundSection()} {
		if !r.hasText(heading) {
			t.Errorf("侧栏缺少分组标题 %q；画面上有 %q", heading, r.session.Texts())
		}
	}
	if v.Total != 3 {
		t.Fatalf("夹具的待处理总数应是 3（2 复核 + 1 采集问题），实际 %d", v.Total)
	}
}

func TestSidebarMarksTheSelectedFilterAsPressed(t *testing.T) {
	r := newUIRig(t, boardItems())
	all := filterName(locales.InboxFilterAll(), 3)
	review := filterName(locales.InboxFilterReviewRequired(), 2)
	if n, _ := r.node(gfx.RoleButton, all); !n.Selected {
		t.Fatal("初始筛选是「全部」，该行应处于按下状态")
	}
	r.clickButton(review)
	if n, _ := r.node(gfx.RoleButton, review); !n.Selected {
		t.Fatal("点击后「需要复核」应处于按下状态")
	}
	if n, _ := r.node(gfx.RoleButton, all); n.Selected {
		t.Fatal("切走之后「全部」不应再是按下状态")
	}
}

func TestSidebarFilterNarrowsTheListAndSummary(t *testing.T) {
	r := newUIRig(t, boardItems())
	// 「全部」不含可选整理（未归合集）：2 条复核 + 1 条采集问题。
	if !r.hasText("复核一") || !r.hasText("任务 t1") || r.hasText("未归一") {
		t.Fatalf("「全部」应列出复核与采集问题、不含未归合集；画面上有 %q", r.session.Texts())
	}
	if !r.hasText(locales.InboxCountPending(3)) {
		t.Fatalf("页头摘要应是 %q；画面上有 %q", locales.InboxCountPending(3), r.session.Texts())
	}

	r.clickButton(filterName(locales.InboxFilterUnclassified(), 2))
	if r.store.View().Filter != FilterUnclassified {
		t.Fatalf("筛选应切到未归合集，实际 %q", r.store.View().Filter)
	}
	if !r.hasText("未归一") || !r.hasText("未归二") || r.hasText("复核一") || r.hasText("任务 t1") {
		t.Fatalf("未归合集分组只应列出未归合集；画面上有 %q", r.session.Texts())
	}
	if !r.hasText(locales.InboxCountPending(2)) {
		t.Fatalf("页头摘要应是 %q；画面上有 %q", locales.InboxCountPending(2), r.session.Texts())
	}

	r.clickButton(filterName(locales.InboxFilterImportIssue(), 1))
	if !r.hasText("任务 t1") || r.hasText("未归一") {
		t.Fatalf("采集问题分组只应列出采集问题；画面上有 %q", r.session.Texts())
	}
}

func TestSidebarSwitchingFilterClearsSelection(t *testing.T) {
	r := newUIRig(t, boardItems())
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核一"))
	if len(r.store.View().Selection) != 1 {
		t.Fatal("勾选后应有一条选中")
	}
	r.clickButton(filterName(locales.InboxFilterUnclassified(), 2))
	if got := r.store.View().Selection; len(got) != 0 {
		t.Fatalf("切换分组应清空选择（选中的行可能已不在新分组里），实际 %v", got)
	}
	if r.hasText(locales.InboxSelectedCount(1)) {
		t.Fatal("选择清空后批量条应消失")
	}
}

func TestSidebarSummaryUsesWikiAndSemanticWording(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) {
		r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 4})
		r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 10, IndexedItems: 7})
	})
	r.clickButton(filterName(locales.InboxFilterWikiPending(), 4))
	if !r.hasText(locales.InboxCountWikiPending(4)) {
		t.Fatalf("Wiki 分组的摘要应是 %q；画面上有 %q", locales.InboxCountWikiPending(4), r.session.Texts())
	}
	r.clickButton(filterName(locales.InboxFilterSemanticPending(), 3))
	if !r.hasText(locales.InboxCountSemanticPending(3)) {
		t.Fatalf("语义索引分组的摘要应是 %q；画面上有 %q", locales.InboxCountSemanticPending(3), r.session.Texts())
	}
}

// ---- 主体的四种状态 ----

func TestBodyShowsSpinnerWhileFirstLoadIsPending(t *testing.T) {
	var release func()
	r := newUIRig(t, boardItems(), goroutines, func(r *uiRig) { release = r.svc.Hold("List") })
	r.frames(3)
	if !r.store.View().IsLoading {
		t.Fatal("列表读取被卡住期间应处于读取中")
	}
	if r.hasText(locales.InboxGroupDone()) || r.hasText(locales.CommonLoadFailed()) || r.hasText("复核一") {
		t.Fatalf("读取中既不能画成「已处理完」也不能画成读取失败；画面上有 %q", r.session.Texts())
	}
	if !r.has(gfx.RoleButton, locales.InboxRefresh()) {
		t.Fatal("读取中页头的刷新钮仍应在")
	}
	release()
	r.settle("列表读取完成", func() bool { return r.hasText("复核一") })
}

func TestBodyShowsLoadErrorNotEmptyState(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) { r.svc.FailOn("List", errors.New("数据库打不开")) })
	if !r.hasText(locales.CommonLoadFailed()) || !r.hasText("数据库打不开") {
		t.Fatalf("读取失败应画错误态并给出原因；画面上有 %q", r.session.Texts())
	}
	if r.hasText(locales.InboxGroupDone()) {
		t.Fatal("读取失败不能渲染成「当前分组已处理完」")
	}
	if !r.has(gfx.RoleStatus, "") && !r.has(gfx.RoleButton, locales.CommonRetry()) {
		t.Fatal("错误态应有重试入口")
	}

	// 故障恢复后点「重试」：错误态消失，列表出现。
	r.svc.FailOn("List", nil)
	r.clickButton(locales.CommonRetry())
	if r.hasText(locales.CommonLoadFailed()) || !r.hasText("复核一") {
		t.Fatalf("重试成功后应显示列表；画面上有 %q", r.session.Texts())
	}
}

func TestBodyShowsGreenEmptyStateWhenGroupIsDone(t *testing.T) {
	r := newUIRig(t, nil)
	if !r.hasText(locales.InboxGroupDone()) {
		t.Fatalf("没有待处理内容时应显示「当前分组已处理完」；画面上有 %q", r.session.Texts())
	}
	if r.hasText(locales.CommonLoadFailed()) {
		t.Fatal("空列表不是读取失败")
	}
	// 页头没有可选行，所以也没有全选框。
	if r.has(gfx.RoleCheckbox, locales.InboxSelectAllVisible()) {
		t.Fatal("没有可选行时不应有全选框")
	}

	// 某个分组空了也是同一个空态：采集问题分组里没有内容。
	r2 := newUIRig(t, []services.InboxItem{review("r1", "复核一")})
	r2.clickButton(filterName(locales.InboxFilterImportIssue(), 0))
	if !r2.hasText(locales.InboxGroupDone()) || r2.hasText("复核一") {
		t.Fatalf("空分组应显示已处理完；画面上有 %q", r2.session.Texts())
	}
}

func TestBodyListsCardsForEachKind(t *testing.T) {
	items := []services.InboxItem{
		review("r1", "复核一", "标题缺失", "正文过短"),
		review("r2", ""),
		importIssue("t1", services.ImportStatusFailed, "网络超时"),
		{Kind: services.InboxKindImportIssue, ID: "import:t2", TaskID: "t2", Title: "任务 t2", Status: services.ImportStatusCompleted,
			Message: "文字稿缺失", ResultItemID: "item-t2"},
		{Kind: services.InboxKindImportIssue, ID: "import:t3", TaskID: "t3", Title: "任务 t3", Status: services.ImportStatusDuplicate,
			Message: "已有同一网页", DuplicateItemID: "item-dup"},
	}
	r := newUIRig(t, items)
	for _, text := range []string{
		"复核一", locales.LibraryUntitled(), "任务 t1", "任务 t2", "任务 t3",
		"标题缺失" + reasonSeparator + "正文过短", locales.InboxReviewRequiredReason(),
		"网络超时", "文字稿缺失", "已有同一网页",
	} {
		if !r.hasText(text) {
			t.Errorf("卡片列表缺少文字 %q；画面上有 %q", text, r.session.Texts())
		}
	}
	// 两条复核行各有「打开」「标记已复核」；采集问题行没有勾选框。
	if got := r.count(gfx.RoleCheckbox, locales.InboxSelectItem("复核一")); got != 1 {
		t.Errorf("复核行应有一个勾选框，实际 %d", got)
	}
	if r.has(gfx.RoleCheckbox, locales.InboxSelectItem("任务 t1")) {
		t.Error("采集问题行不能勾选")
	}
	// 复核行的勾选框用原始标题命名（空标题时名字里没有「无标题」的回退）。
	if !r.has(gfx.RoleCheckbox, locales.InboxSelectItem("")) {
		t.Error("空标题的复核行勾选框应以空标题命名")
	}
	if got := r.count(gfx.RoleButton, locales.CommonOpen()); got != 2 {
		t.Errorf("两条复核行各有一个「打开」，实际 %d", got)
	}
	if got := r.count(gfx.RoleButton, locales.LibraryMarkReviewed()); got != 2 {
		t.Errorf("两条复核行各有一个「标记已复核」，实际 %d", got)
	}
	// 采集问题：失败的有「重试」，有结果或重复条目的有「查看条目」，已完成带警告的有「不再提醒」。
	if got := r.count(gfx.RoleButton, locales.CommonRetry()); got != 1 {
		t.Errorf("只有失败的采集问题有「重试」，实际 %d", got)
	}
	if got := r.count(gfx.RoleButton, locales.InboxViewItem()); got != 2 {
		t.Errorf("有结果条目与有重复条目的采集问题各有「查看条目」，实际 %d", got)
	}
	if got := r.count(gfx.RoleButton, locales.InboxDismissWarning()); got != 1 {
		t.Errorf("已完成带警告的采集问题有「不再提醒」，实际 %d", got)
	}
}

func TestBodyUnclassifiedCardsExplainWhy(t *testing.T) {
	r := newUIRig(t, boardItems())
	r.clickButton(filterName(locales.InboxFilterUnclassified(), 2))
	if got := countTexts(r, locales.InboxUnclassifiedReason()); got != 2 {
		t.Fatalf("每条未归合集的卡片都应说明「尚未归入任何合集」，实际 %d 处", got)
	}
	if r.count(gfx.RoleButton, locales.LibraryMarkReviewed()) != 0 {
		t.Fatal("未归合集的卡片没有「标记已复核」")
	}
	if got := r.count(gfx.RoleButton, locales.CommonOpen()); got != 2 {
		t.Fatalf("未归合集的卡片各有「打开」，实际 %d", got)
	}
}

func countTexts(r *uiRig, text string) int {
	n := 0
	for _, got := range r.session.Texts() {
		if got == text {
			n++
		}
	}
	return n
}

// ---- 单行动作 ----

func TestRowOpenButtonOpensTheKnowledgeItem(t *testing.T) {
	r := newUIRig(t, boardItems())
	r.clickNthButton(locales.CommonOpen(), 1)
	if !slices.Equal(r.opened, []string{"r2"}) {
		t.Fatalf("点第二行的「打开」应打开 r2，实际 %v", r.opened)
	}
}

func TestRowMarkReviewedCallsServiceAndRefreshes(t *testing.T) {
	r := newUIRig(t, boardItems())
	// 服务端处理之后，复核一不再待复核：先把下一次读到的列表换掉，再点按钮。
	r.svc.SetList(listOf(review("r2", "复核二"), importIssue("t1", services.ImportStatusFailed, "网络超时")))
	r.clickNthButton(locales.LibraryMarkReviewed(), 0)
	if got := r.svc.ReviewedCalls(); len(got) != 1 || !slices.Equal(got[0], []string{"r1"}) {
		t.Fatalf("应只对 r1 调一次标记已复核，实际 %v", got)
	}
	if r.hasText("复核一") || !r.hasText("复核二") {
		t.Fatalf("标记之后列表应刷新、不再有复核一；画面上有 %q", r.session.Texts())
	}
}

func TestRowMarkReviewedFailureToastsAndKeepsRow(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) { r.svc.FailOn("MarkReviewed", errors.New("写库失败")) })
	r.clickNthButton(locales.LibraryMarkReviewed(), 0)
	if len(r.notifier.Queued()) == 0 {
		t.Fatal("失败应弹出带原因的提示")
	}
	if last := r.notifier.Queued()[len(r.notifier.Queued())-1]; !strings.Contains(last.Detail, "写库失败") {
		t.Fatalf("提示的详情应带失败原因，实际 %+v", last)
	}
	if !r.hasText("复核一") {
		t.Fatal("失败后这一行应还在")
	}
}

func TestRowRetryImportTaskRetriesThenRefreshes(t *testing.T) {
	r := newUIRig(t, boardItems())
	before := r.svc.CallCount("List")
	r.clickButton(locales.CommonRetry())
	if got := r.svc.RetriedCalls(); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("应重试任务 t1，实际 %v", got)
	}
	if r.svc.CallCount("List") <= before {
		t.Fatal("重试之后应刷新列表")
	}
}

func TestRowViewItemOpensDuplicateBeforeResult(t *testing.T) {
	items := []services.InboxItem{
		{Kind: services.InboxKindImportIssue, ID: "import:t3", TaskID: "t3", Title: "任务 t3", Status: services.ImportStatusDuplicate,
			DuplicateItemID: "item-dup", ResultItemID: "item-res"},
		{Kind: services.InboxKindImportIssue, ID: "import:t2", TaskID: "t2", Title: "任务 t2", Status: services.ImportStatusCompleted,
			Message: "文字稿缺失", ResultItemID: "item-t2"},
	}
	r := newUIRig(t, items)
	r.clickNthButton(locales.InboxViewItem(), 0)
	r.clickNthButton(locales.InboxViewItem(), 1)
	if !slices.Equal(r.opened, []string{"item-dup", "item-t2"}) {
		t.Fatalf("「查看条目」应优先打开重复的那条、其次是结果条目，实际 %v", r.opened)
	}
}

func TestRowDismissWarningAcknowledgesTheTask(t *testing.T) {
	warn := services.InboxItem{Kind: services.InboxKindImportIssue, ID: "import:t2", TaskID: "t2", Title: "任务 t2",
		Status: services.ImportStatusCompleted, Message: "文字稿缺失", ResultItemID: "item-t2"}
	r := newUIRig(t, []services.InboxItem{warn})
	r.svc.SetList(listOf())
	r.clickButton(locales.InboxDismissWarning())
	if got := r.svc.AcknowledgedCalls(); !slices.Equal(got, []string{"t2"}) {
		t.Fatalf("应对 t2 确认警告，实际 %v", got)
	}
	if r.hasText("任务 t2") || !r.hasText(locales.InboxGroupDone()) {
		t.Fatalf("确认之后这一行应消失；画面上有 %q", r.session.Texts())
	}
}

func TestRowDismissWarningShowsBusyAndDisablesButton(t *testing.T) {
	warn := services.InboxItem{Kind: services.InboxKindImportIssue, ID: "import:t2", TaskID: "t2", Title: "任务 t2",
		Status: services.ImportStatusCompleted, Message: "文字稿缺失", ResultItemID: "item-t2"}
	var release func()
	r := newUIRig(t, []services.InboxItem{warn}, goroutines)
	release = r.svc.Hold("AcknowledgeImportWarning")
	r.clickButton(locales.InboxDismissWarning())
	r.settle("按钮进入处理中", func() bool { return r.has(gfx.RoleButton, locales.InboxProcessing()) })
	if n, _ := r.node(gfx.RoleButton, locales.InboxProcessing()); !n.Disabled {
		t.Fatal("处理期间按钮应禁用，免得连点")
	}
	release()
	r.settle("处理结束", func() bool { return !r.has(gfx.RoleButton, locales.InboxProcessing()) })
}

// ---- 聚合行 ----

func TestAggregateCardsRunSemanticIndexAndOpenWikiPreview(t *testing.T) {
	r := newUIRig(t, nil, func(r *uiRig) {
		r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3})
		r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 10, IndexedItems: 6})
		r.svc.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticDone, Indexed: 4})
	})
	if !r.hasText(locales.InboxSemanticAggregateTitle()) || !r.hasText(locales.InboxWikiAggregateTitle()) {
		t.Fatalf("两个聚合行都应显示；画面上有 %q", r.session.Texts())
	}
	if !r.hasText(locales.InboxSemanticAggregateDesc(4)) || !r.hasText(locales.InboxWikiAggregateDesc(3)) {
		t.Fatalf("聚合行应带各自的待处理数；画面上有 %q", r.session.Texts())
	}
	// 语义索引在 Wiki 之前。
	r.clickNthButton(locales.InboxRunNow(), 0)
	if r.svc.IndexRuns() != 1 {
		t.Fatalf("第一个聚合行是语义索引，应执行一轮索引，实际 %d", r.svc.IndexRuns())
	}
	r.clickNthButton(locales.InboxRunNow(), 1)
	if r.wikiOpened != 1 {
		t.Fatalf("Wiki 聚合行的「立即执行」应打开预览，实际 %d 次", r.wikiOpened)
	}
}

func TestAggregateRunningDisablesBothButtonsAndShowsProgress(t *testing.T) {
	var release func()
	r := newUIRig(t, nil, goroutines, func(r *uiRig) {
		r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3})
		r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 10, IndexedItems: 6})
		release = r.svc.Hold("RunSemanticIndexing")
	})
	r.clickNthButton(locales.InboxRunNow(), 0)
	r.settle("语义索引进入执行", func() bool { return r.has(gfx.RoleButton, locales.InboxIndexing()) })
	if n, _ := r.node(gfx.RoleButton, locales.InboxIndexing()); !n.Disabled {
		t.Fatal("执行中的按钮应禁用")
	}
	if n, ok := r.node(gfx.RoleButton, locales.InboxRunNow()); !ok || !n.Disabled {
		t.Fatalf("任何一个聚合行在执行时，另一个按钮也应禁用（存在=%v）", ok)
	}
	release()
	r.settle("索引结束", func() bool { return !r.has(gfx.RoleButton, locales.InboxIndexing()) })
}

// ---- 批量选择 ----

func TestHeaderSelectAllHasMixedStateAndTogglesVisibleRows(t *testing.T) {
	r := newUIRig(t, boardItems())
	selectAll := locales.InboxSelectAllVisible()
	if n, ok := r.node(gfx.RoleCheckbox, selectAll); !ok || n.Checked || n.Mixed {
		t.Fatalf("没有选中时全选框应是未选中（存在=%v，%+v）", ok, n)
	}

	// 只选一行：半选。
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核一"))
	if n, _ := r.node(gfx.RoleCheckbox, selectAll); !n.Mixed || n.Checked {
		t.Fatalf("只选了部分时全选框应是半选，实际 %+v", n)
	}
	if !r.hasText(locales.InboxSelectedCount(1)) {
		t.Fatalf("批量条应显示已选 1 条；画面上有 %q", r.session.Texts())
	}

	// 半选时点全选框：选中全部可选行（采集问题行没有勾选框，不计入）。
	r.click(gfx.RoleCheckbox, selectAll)
	if got := r.store.View().SelectedVisibleCount; got != 2 {
		t.Fatalf("半选时点全选应选中全部 2 条可选行，实际 %d", got)
	}
	if n, _ := r.node(gfx.RoleCheckbox, selectAll); !n.Checked || n.Mixed {
		t.Fatalf("全部选中时全选框应是选中态，实际 %+v", n)
	}

	// 全选时再点：清空选择，批量条消失。
	r.click(gfx.RoleCheckbox, selectAll)
	if got := r.store.View().Selection; len(got) != 0 {
		t.Fatalf("再点一次应清空选择，实际 %v", got)
	}
	if r.hasText(locales.InboxSelectedCount(2)) || r.has(gfx.RoleButton, locales.InboxOrganizeSelected()) {
		t.Fatal("清空选择后批量条应消失")
	}
}

func TestBulkBarAppearsWithSelectionAndCancelClearsIt(t *testing.T) {
	r := newUIRig(t, boardItems())
	if r.has(gfx.RoleButton, locales.InboxOrganizeSelected()) {
		t.Fatal("没有选中时不应有批量条")
	}
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核一"))
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核二"))
	for _, name := range []string{locales.InboxOrganizeSelected(), locales.CommonCancel()} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("批量条缺少按钮「%s」", name)
		}
	}
	if !r.has(gfx.RoleCombobox, locales.InboxMoveToCollection()) || !r.has(gfx.RoleTextField, locales.InboxAddTagsPlaceholder()) {
		t.Error("批量条缺少合集下拉或标签输入框")
	}
	if !r.hasText(locales.InboxSelectedCount(2)) {
		t.Errorf("批量条应显示已选 2 条；画面上有 %q", r.session.Texts())
	}
	// 选中里没有未归合集的条目：没有 AI 智能归类。
	if r.has(gfx.RoleButton, locales.InboxSmartClassify(0)) {
		t.Error("选中里没有未归合集的条目时不应有智能归类")
	}
	r.clickButton(locales.CommonCancel())
	if len(r.store.View().Selection) != 0 || r.has(gfx.RoleButton, locales.InboxOrganizeSelected()) {
		t.Fatal("取消应清空选择并收起批量条")
	}
}

// ---- 批量整理 ----

func TestBulkOrganizeSendsCollectionTagsAndClearsAfterSuccess(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) {
		r.collections = []services.LibraryCollection{{ID: "c1", Name: "工作", Icon: "\U0001F4BC"}, {ID: "c2", Name: "读书"}}
	})
	if r.collLoads != 1 {
		t.Fatalf("进入模块时应读一次合集，实际 %d 次", r.collLoads)
	}
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核一"))
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核二"))

	// 合集下拉：选项是「图标 名称」，没有图标的用 📚。
	r.click(gfx.RoleCombobox, locales.InboxMoveToCollection())
	for _, label := range []string{"\U0001F4BC 工作", defaultCollectionIcon + " 读书"} {
		if !r.has(gfx.RoleOption, label) {
			t.Fatalf("下拉缺少选项 %q；画面上有 %q", label, r.session.Texts())
		}
	}
	r.click(gfx.RoleOption, "\U0001F4BC 工作")
	if got := r.store.View().OrganizeCollectionID; got != "c1" {
		t.Fatalf("选中「工作」后合集应是 c1，实际 %q", got)
	}

	r.click(gfx.RoleTextField, locales.InboxAddTagsPlaceholder())
	r.typeText("论文, 待读")
	if got := r.store.View().OrganizeTagText; got != "论文, 待读" {
		t.Fatalf("输入的标签应同步到 Store，实际 %q", got)
	}

	r.clickButton(locales.InboxOrganizeSelected())
	calls := r.svc.OrganizeCalls()
	if len(calls) != 1 {
		t.Fatalf("应调一次整理，实际 %d 次", len(calls))
	}
	got := calls[0]
	if !slices.Equal(got.ItemIDs, []string{"r1", "r2"}) || got.CollectionID != "c1" || !slices.Equal(got.AddTagNames, []string{"论文", "待读"}) {
		t.Fatalf("整理入参不对：%+v", got)
	}
	// 成功后选择与标签清空，批量条收起；合集保留（渲染层只清标签输入）。
	v := r.store.View()
	if len(v.Selection) != 0 || v.OrganizeTagText != "" || v.OrganizeCollectionID != "c1" {
		t.Fatalf("整理成功后应清空选择与标签、保留合集，实际 选择=%v 标签=%q 合集=%q", v.Selection, v.OrganizeTagText, v.OrganizeCollectionID)
	}
	if r.has(gfx.RoleButton, locales.InboxOrganizeSelected()) {
		t.Fatal("整理成功后批量条应收起")
	}
	// 再次选中时，标签输入框是空的（输入框跟着 Store 清空，不留旧文字）。
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核一"))
	if n, _ := r.node(gfx.RoleTextField, locales.InboxAddTagsPlaceholder()); n.Value != "" {
		t.Fatalf("清空之后标签输入框应是空的，实际 %q", n.Value)
	}
}

func TestBulkOrganizeFailureKeepsSelectionAndShowsReason(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) { r.svc.FailOn("Organize", errors.New("合集已被删除")) })
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核一"))
	r.clickButton(locales.InboxOrganizeSelected())
	queued := r.notifier.Queued()
	if len(queued) == 0 || !strings.Contains(queued[len(queued)-1].Detail+queued[len(queued)-1].Message, "合集已被删除") {
		t.Fatalf("失败应弹出带原因的提示，实际 %+v", queued)
	}
	if got := r.store.View().Selection; !slices.Equal(got, []string{"r1"}) {
		t.Fatalf("失败后选择应保留以便重试，实际 %v", got)
	}
	if !r.has(gfx.RoleButton, locales.InboxOrganizeSelected()) {
		t.Fatal("失败后批量条应还在")
	}
}

// ---- AI 智能归类 ----

func TestSmartClassifyButtonOnlyForUnclassifiedSelection(t *testing.T) {
	r := newUIRig(t, boardItems())
	// 「全部」不含未归合集；切到未归合集再选。
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("复核一"))
	if r.has(gfx.RoleButton, locales.InboxSmartClassify(1)) {
		t.Fatal("只选了复核行时不应有 AI 智能归类")
	}
	r.clickButton(filterName(locales.InboxFilterUnclassified(), 2))
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("未归一"))
	if !r.has(gfx.RoleButton, locales.InboxSmartClassify(1)) {
		t.Fatalf("选中未归合集条目后应出现 AI 智能归类 1；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("未归二"))
	if !r.has(gfx.RoleButton, locales.InboxSmartClassify(2)) {
		t.Fatal("按钮上的数字应跟选中数一致")
	}
}

func TestSmartClassifyRunsAppliesAndRefreshes(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) {
		r.svc.SetSources([]services.InboxAISource{{ItemID: "u1", Title: "未归一"}, {ItemID: "u2", Title: "未归二"}})
		r.svc.SetAssignments(map[string]string{"u1": "论文", "u2": "论文"})
		r.svc.SetApplyResult(services.InboxAIApplyResult{Classified: 2, CreatedCollectionNames: []string{"论文"}})
	})
	r.clickButton(filterName(locales.InboxFilterUnclassified(), 2))
	r.click(gfx.RoleCheckbox, locales.InboxSelectAllVisible())
	r.clickButton(locales.InboxSmartClassify(2))
	if got := r.svc.AppliedCalls(); len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("应把 2 条归类结果写入一次，实际 %v", got)
	}
	if got := r.svc.ClassifyCalls(); len(got) != 1 {
		t.Fatalf("应调一次 AI 归类，实际 %d", len(got))
	}
	if r.store.View().Classifying {
		t.Fatal("归类结束后不应还处于归类中")
	}
	if len(r.notifier.Queued()) == 0 {
		t.Fatal("归类结束应弹出结果提示")
	}
}

func TestSmartClassifyShowsProgressAndSecondClickStopsWithoutWriting(t *testing.T) {
	var release func()
	r := newUIRig(t, boardItems(), goroutines, func(r *uiRig) {
		r.svc.SetSources([]services.InboxAISource{{ItemID: "u1", Title: "未归一"}, {ItemID: "u2", Title: "未归二"}})
		r.svc.SetAssignments(map[string]string{"u1": "论文", "u2": "论文"})
		r.svc.SetClassifyBatches(2)
		release = r.svc.HoldClassifyAfter(1)
	})
	r.clickButton(filterName(locales.InboxFilterUnclassified(), 2))
	r.click(gfx.RoleCheckbox, locales.InboxSelectAllVisible())
	r.clickButton(locales.InboxSmartClassify(2))

	stop := locales.InboxStopClassify(1, 2)
	r.settle("出现归类进度", func() bool { return r.has(gfx.RoleButton, stop) })
	// 归类期间界面线程不能被卡住：列表与批量条都还能出帧、响应。
	if !r.has(gfx.RoleButton, locales.InboxOrganizeSelected()) || !r.hasText("未归一") {
		t.Fatalf("归类期间主列表与批量条应照常显示；画面上有 %q", r.session.Texts())
	}

	r.clickButton(stop)
	release()
	r.settle("归类停止", func() bool { return !r.store.View().Classifying })
	if got := r.svc.AppliedCalls(); len(got) != 0 {
		t.Fatalf("停止归类不应写入任何分类，实际 %v", got)
	}
	if !r.has(gfx.RoleButton, locales.InboxSmartClassify(2)) {
		t.Fatal("停止之后按钮应回到「AI 智能归类」")
	}
}

func TestSmartClassifyButtonNotConfiguredOpensAISettings(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) { r.svc.FailOn("Classify", services.ErrAINotConfigured) })
	r.clickButton(filterName(locales.InboxFilterUnclassified(), 2))
	r.click(gfx.RoleCheckbox, locales.InboxSelectItem("未归一"))
	r.clickButton(locales.InboxSmartClassify(1))
	if r.settingsAI != 1 {
		t.Fatalf("未配置 AI 时应跳到设置里的 AI 分区，实际 %d 次", r.settingsAI)
	}
}

// ---- 局部错误不挡主列表 ----

func TestSectionErrorsAreLocalAlertsWithRetryAndDoNotBlockTheList(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) {
		r.svc.FailOn("WikiCandidates", errors.New("wiki 库繁忙"))
		r.svc.FailOn("SemanticStatus", errors.New("向量库未响应"))
	})
	wiki, semantic := locales.InboxWikiStatusFailed("wiki 库繁忙"), locales.InboxSemanticStatusFailed("向量库未响应")
	if !r.hasText(wiki) || !r.hasText(semantic) {
		t.Fatalf("两块局部错误都应有条；画面上有 %q", r.session.Texts())
	}
	if got := r.count(gfx.RoleStatus, ""); got < 2 {
		t.Fatalf("局部错误条应是直播区域（role=alert），实际 %d 个", got)
	}
	if got := r.count(gfx.RoleButton, locales.CommonRetry()); got < 2 {
		t.Fatalf("每条局部错误都应有重试，实际 %d 个", got)
	}
	// 主列表照常显示，而且没有被画成读取失败。
	if !r.hasText("复核一") || r.hasText(locales.CommonLoadFailed()) {
		t.Fatalf("局部错误不应挡住主列表；画面上有 %q", r.session.Texts())
	}

	// 故障恢复后点任意一条的重试：两条都消失（重试是整体刷新）。
	r.svc.FailOn("WikiCandidates", nil)
	r.svc.FailOn("SemanticStatus", nil)
	r.clickNthButton(locales.CommonRetry(), 0)
	if r.hasText(wiki) || r.hasText(semantic) || !r.hasText("复核一") {
		t.Fatalf("重试成功后局部错误应消失；画面上有 %q", r.session.Texts())
	}
}

func TestRefreshFailureAfterLoadedListReplacesListWithErrorState(t *testing.T) {
	r := newUIRig(t, boardItems())
	r.svc.FailOn("List", errors.New("断开了"))
	r.clickButton(locales.InboxRefresh())
	if !r.hasText(locales.CommonLoadFailed()) || r.hasText(locales.InboxGroupDone()) {
		t.Fatalf("重读失败应画错误态，不能画成已处理完；画面上有 %q", r.session.Texts())
	}
}

func TestWikiBannerNeedsModelAndOpensAISettings(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) {
		r.svc.SetWikiCandidates(services.InboxWikiCandidates{Waiting: 2})
		r.svc.SetWikiModelReady(false)
	})
	if !r.hasText(locales.InboxWikiNeedsModel(2)) {
		t.Fatalf("应提示尚未配置文本模型；画面上有 %q", r.session.Texts())
	}
	if r.hasText(locales.InboxWikiCandidatesView()) {
		t.Fatal("需要配置模型时不显示候选摘要")
	}
	r.clickButton(locales.InboxWikiConfigureModel())
	if r.settingsAI != 1 {
		t.Fatalf("「去配置模型」应打开设置的 AI 分区，实际 %d 次", r.settingsAI)
	}
}

func TestWikiBannerShowsCandidateSummaryAndPreviewEntry(t *testing.T) {
	r := newUIRig(t, boardItems(), func(r *uiRig) {
		r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3, Review: 1, Upgrade: 2})
	})
	parts := strings.Join([]string{
		locales.InboxWikiPartReady(3), locales.InboxWikiPartReview(1), locales.InboxWikiPartUpgrade(2),
	}, locales.InboxNameSeparator())
	if !strings.Contains(r.allText(), locales.InboxWikiCandidates(parts)) {
		t.Fatalf("应显示候选摘要 %q；画面上有 %q", locales.InboxWikiCandidates(parts), r.session.Texts())
	}
	r.clickButton(locales.InboxWikiCandidatesView())
	if r.wikiOpened != 1 {
		t.Fatalf("「查看范围和任务」应打开 Wiki 预览，实际 %d 次", r.wikiOpened)
	}
}

func TestNoWikiBannerWhenNothingToCompile(t *testing.T) {
	r := newUIRig(t, boardItems())
	if r.has(gfx.RoleButton, locales.InboxWikiCandidatesView()) || r.has(gfx.RoleButton, locales.InboxWikiConfigureModel()) {
		t.Fatal("没有候选时不应有 Wiki 提示条")
	}
}

// ---- 页头 ----

func TestThemedTasksEntryExplainsNotMigratedWithoutBreakingTheList(t *testing.T) {
	r := newUIRig(t, boardItems())
	r.clickButton(locales.ThemedReadingTasks())
	queued := r.notifier.Queued()
	if len(queued) != 1 {
		t.Fatalf("点击应弹出一条说明，实际 %+v", queued)
	}
	if queued[0].Message != locales.CommonOperationFailed(locales.ThemedReadingTasks()) ||
		queued[0].Detail != locales.NativeLibraryNotMigrated(locales.ThemedReadingTasks(), themedTasksMilestone, themedTasksLedger) {
		t.Fatalf("提示应写明去向（里程碑与台账号），实际 %+v", queued[0])
	}
	if got := r.log.entries(); len(got) != 0 {
		t.Fatalf("未迁移的入口不是故障，不应写错误日志，实际 %v", got)
	}
	if !r.hasText("复核一") {
		t.Fatal("点击之后列表应照常显示")
	}
}

func TestRefreshButtonReloadsAndNamesItself(t *testing.T) {
	r := newUIRig(t, boardItems())
	n, ok := r.node(gfx.RoleButton, locales.InboxRefresh())
	if !ok {
		t.Fatal("页头应有刷新钮")
	}
	if n.Hint != locales.CommonRefresh() {
		t.Fatalf("刷新钮的悬停提示应是「刷新」，实际 %q", n.Hint)
	}
	before := r.svc.CallCount("List")
	r.clickButton(locales.InboxRefresh())
	if got := r.svc.CallCount("List"); got != before+1 {
		t.Fatalf("点刷新应重读一次列表，之前 %d 次，之后 %d 次", before, got)
	}
}

func TestEnteringAgainRefreshesAndReadsCollections(t *testing.T) {
	r := newUIRig(t, boardItems())
	lists, colls := r.svc.CallCount("List"), r.collLoads
	if lists != 1 || colls != 1 {
		t.Fatalf("首次进入应读一次列表与合集，实际 %d、%d", lists, colls)
	}
	r.frames(3) // 一直停留：不重复读取
	if r.svc.CallCount("List") != lists {
		t.Fatal("停留期间不应反复重读")
	}
	r.hidden = true
	r.frames(2) // 切到别的模块：卸载
	r.hidden = false
	r.frames(3) // 切回来：重新进入并重读（模型状态也随之刷新）
	if got := r.svc.CallCount("List"); got != lists+1 {
		t.Fatalf("重新进入应重读列表，之前 %d 次，之后 %d 次", lists, got)
	}
	if r.svc.CallCount("WikiModelReady") < 2 {
		t.Fatal("重新进入应刷新文本模型是否就绪")
	}
	if r.collLoads != colls+1 {
		t.Fatalf("重新进入应重读合集，之前 %d 次，之后 %d 次", colls, r.collLoads)
	}
}
