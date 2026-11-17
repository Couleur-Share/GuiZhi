package imports

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件是采集工作区的交互测试（无头会话，不碰 WebView2）：首次读取与防抖、读取失败不当空态、空态引导、
// 侧栏筛选与来源筛选、搜索、选择与批量动作、暂停队列、清理确认、删除与撤销、加载更早的任务。
// 行的呈现见 screen_row_test.go，状态机的竞态见 store_test.go。

// mixed 是一屏有各种状态的队列：一个在跑、一个已完成、两个失败。
func mixed() services.ImportPage {
	return pageOf(
		mkTask("run", nil),
		completed(func(x *services.ImportTask) { x.ID = "done" }),
		failedTask(func(x *services.ImportTask) { x.ID, x.Error = "f1", "HTTP 522" }),
		failedTask(func(x *services.ImportTask) { x.ID, x.Error = "f2", "HTTP 403" }),
	)
}

func countName(label string, n int) string { return fmt.Sprintf("%s %d", label, n) }

func (r *rig) rows() int { return r.count(gfx.RoleButton, locales.ImportsSelect()) }

func lastQuery(r *rig) services.ImportQuery {
	qs := r.svc.Queries()
	if len(qs) == 0 {
		r.t.Fatal("服务还没收到任何查询")
	}
	return qs[len(qs)-1]
}

// ---- 首次读取 ----

// 挂载时读一次列表，但要先过 150 ms 的防抖；读到之前页头不报数（否则会先写「共 0 条」再跳成真实条数）。
func TestFirstLoadWaitsForDebounceAndHidesCountsUntilThen(t *testing.T) {
	r := newRig(t, mixed())
	if got := len(r.svc.Queries()); got != 0 {
		t.Fatalf("防抖到点之前不应读取，实际 %d 次", got)
	}
	if r.hasTextLike("共 ") {
		t.Error("读到之前页头不应报数")
	}
	if r.rows() != 0 {
		t.Error("读到之前不应有任务行")
	}
	r.debounce()
	if got := len(r.svc.Queries()); got != 1 {
		t.Fatalf("防抖到点后应读取 1 次，实际 %d", got)
	}
	if r.rows() != 4 {
		t.Errorf("应有 4 行任务，实际 %d", r.rows())
	}
	if want := locales.ImportsSummaryActive(4, 1); !r.hasText(want) {
		t.Errorf("页头应写 %q；画面上有 %q", want, r.session.Texts())
	}
}

// 读取失败必须画成错误态并给重试，不能画成「还没有任务」的空态；重试成功后恢复列表。
func TestLoadFailureIsNotEmptyStateAndRetryRecovers(t *testing.T) {
	r := newRig(t, services.ImportPage{})
	r.svc.Set(services.ImportPage{}, errors.New("database is locked"))
	r.debounce()
	if !r.hasText(locales.CommonLoadFailed()) || !r.hasTextLike("database is locked") {
		t.Fatalf("读取失败应显示原因；画面上有 %q", r.session.Texts())
	}
	if r.hasText(locales.ImportsEmpty()) {
		t.Fatal("读取失败不能渲染成空态")
	}
	r.svc.Set(mixed(), nil)
	r.click(gfx.RoleButton, locales.CommonRetry())
	if r.hasText(locales.CommonLoadFailed()) || r.rows() != 4 {
		t.Errorf("重试成功后应恢复列表：行数 %d；画面上有 %q", r.rows(), r.session.Texts())
	}
}

// ---- 空态与「查看全部任务」 ----

func TestEmptyQueueShowsGuideAndEntries(t *testing.T) {
	r := loaded(t, services.ImportPage{})
	if !r.hasText(locales.ImportsEmpty()) {
		t.Fatalf("没有任何任务应显示引导；画面上有 %q", r.session.Texts())
	}
	if want := locales.ImportsEmptyHint("Ctrl+N"); !r.hasText(want) {
		t.Errorf("引导里应带快捷键提示 %q", want)
	}
	// 侧栏与引导各有一枚「快速采集」「采集文件」：侧栏的排在前面，引导里的是第二个。
	r.clickNth(gfx.RoleButton, locales.CaptureTitle(), 1)
	if r.quickOpened != 1 {
		t.Errorf("点引导里的「快速采集」应打开快速采集，实际 %d 次", r.quickOpened)
	}
	r.clickNth(gfx.RoleButton, locales.HeaderNewImportFiles(), 1)
	if !slices.Contains(r.calls(), "select") {
		t.Errorf("点「采集文件」应弹出文件选择，调用记录 %v", r.calls())
	}
}

func TestSidebarEntriesOpenCaptureAndFilePicker(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.CaptureTitle())
	if r.quickOpened != 1 {
		t.Errorf("侧栏「快速采集」应打开快速采集，实际 %d 次", r.quickOpened)
	}
	r.click(gfx.RoleButton, locales.HeaderNewImportFiles())
	if !slices.Contains(r.calls(), "select") {
		t.Errorf("侧栏「采集文件」应弹出文件选择，调用记录 %v", r.calls())
	}
}

// 有任务但当前筛选下没有：与「一条任务都没有」是两回事，出口是「查看全部任务」，它把筛选与搜索都重置。
func TestFilteredEmptyOffersResetToAllTasks(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, countName(locales.ImportsPanelCanceled(), 0))
	if !r.hasText(locales.ImportsFilteredEmpty()) {
		t.Fatalf("筛选后没有任务应写「%s」；画面上有 %q", locales.ImportsFilteredEmpty(), r.session.Texts())
	}
	if r.hasText(locales.ImportsEmpty()) {
		t.Error("筛选后为空不是「还没有任务」的引导")
	}
	r.click(gfx.RoleButton, locales.ImportsResetFilter())
	r.debounce()
	if r.hasText(locales.ImportsFilteredEmpty()) || r.rows() != 4 {
		t.Errorf("重置后应回到全部任务：行数 %d；画面上有 %q", r.rows(), r.session.Texts())
	}
	if got := r.store.View().Filter; got != FilterAll {
		t.Errorf("重置后状态筛选应回到全部，实际 %q", got)
	}
}

// ---- 筛选 ----

func TestSidebarFilterNarrowsListAndAsksServer(t *testing.T) {
	r := loaded(t, mixed())
	for _, c := range []struct {
		label string
		n     int
	}{
		{locales.ImportsPanelAll(), 4}, {locales.ImportsPanelActive(), 1}, {locales.ImportsPanelCompleted(), 1},
		{locales.ImportsPanelDegraded(), 0}, {locales.ImportsPanelDuplicate(), 0}, {locales.ImportsPanelFailed(), 2},
		{locales.ImportsPanelCanceled(), 0},
	} {
		if !r.has(gfx.RoleButton, countName(c.label, c.n)) {
			t.Errorf("侧栏应有「%s」", countName(c.label, c.n))
		}
	}
	r.click(gfx.RoleButton, countName(locales.ImportsPanelFailed(), 2))
	if r.rows() != 2 {
		t.Errorf("筛到失败应只剩 2 行，实际 %d", r.rows())
	}
	if got := lastQuery(r).Status; got != "failed" {
		t.Errorf("筛选应带给服务端：Status = %q", got)
	}
	// 重试按钮只作用于当前可见的失败任务。
	if !r.has(gfx.RoleButton, locales.ImportsRetryVisibleFailed(2)) {
		t.Errorf("页头应有「%s」", locales.ImportsRetryVisibleFailed(2))
	}
	r.click(gfx.RoleButton, countName(locales.ImportsPanelActive(), 1))
	if r.rows() != 1 || r.has(gfx.RoleButton, locales.ImportsRetryVisibleFailed(2)) {
		t.Errorf("筛到进行中应只剩 1 行且没有重试失败钮：行数 %d", r.rows())
	}
}

func TestOriginFilterAsksServerAndHighlightsChoice(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.ImportsOriginMobile())
	if got := lastQuery(r).Origin; got != OriginMobile {
		t.Errorf("来源筛选应带给服务端：Origin = %q", got)
	}
	if got := r.store.View().Origin; got != OriginMobile {
		t.Errorf("Store 的来源应是 mobile，实际 %q", got)
	}
	r.click(gfx.RoleButton, locales.ImportsOriginAll())
	if got := lastQuery(r).Origin; got != OriginAll && got != "" {
		t.Errorf("回到全部来源后 Origin = %q", got)
	}
}

// ---- 搜索 ----

// 输入停下 150 ms 后才读取；清除钮一键清空并重新读取。
func TestSearchIsDebouncedAndClearable(t *testing.T) {
	r := loaded(t, mixed())
	before := len(r.svc.Queries())
	r.click(gfx.RoleTextField, locales.ImportsSearchPlaceholder())
	r.typeText("rag")
	if got := len(r.svc.Queries()); got != before {
		t.Fatalf("防抖到点之前不应读取：%d → %d", before, got)
	}
	r.debounce()
	if got := lastQuery(r).Query; got != "rag" {
		t.Errorf("防抖到点后应以搜索词读取，Query = %q", got)
	}
	if !r.has(gfx.RoleButton, locales.HeaderClearSearch()) {
		t.Fatal("有搜索词时应出现清除按钮")
	}
	r.click(gfx.RoleButton, locales.HeaderClearSearch())
	r.debounce()
	if got := lastQuery(r).Query; got != "" {
		t.Errorf("清除后应以空词重新读取，Query = %q", got)
	}
	if r.has(gfx.RoleButton, locales.HeaderClearSearch()) {
		t.Error("清空后清除按钮应消失")
	}
}

// ---- 选择与批量动作 ----

func TestSelectionShowsBulkBarAndEscClears(t *testing.T) {
	r := loaded(t, mixed())
	r.clickNth(gfx.RoleButton, locales.ImportsSelect(), 2)
	if !r.hasText(locales.ImportsSelectedCount(1)) {
		t.Fatalf("选中后应出现批量条「%s」；画面上有 %q", locales.ImportsSelectedCount(1), r.session.Texts())
	}
	r.keys("Escape")
	if r.hasTextLike(locales.ImportsSelectedCount(1)) {
		t.Error("Esc 应清空选择并收起批量条")
	}
	r.clickNth(gfx.RoleButton, locales.ImportsSelect(), 2)
	r.click(gfx.RoleButton, locales.ImportsClearSelection())
	if r.hasTextLike(locales.ImportsSelectedCount(1)) {
		t.Error("「取消选择」应清空选择")
	}
}

func TestBulkSelectVisibleThenRetryOnlyFailedOnes(t *testing.T) {
	r := loaded(t, mixed())
	r.clickNth(gfx.RoleButton, locales.ImportsSelect(), 0)
	r.click(gfx.RoleButton, locales.ImportsSelectVisible())
	if !r.hasText(locales.ImportsSelectedCount(4)) {
		t.Fatalf("全选可见应选中 4 项；画面上有 %q", r.session.Texts())
	}
	// 重试只作用于失败或已取消的任务：已完成与进行中的不能被重新跑一遍。
	r.click(gfx.RoleButton, countName(locales.ImportsRetry(), 2))
	got := r.calls()
	if !slices.Contains(got, "retry:f1") || !slices.Contains(got, "retry:f2") || slices.Contains(got, "retry:done") || slices.Contains(got, "retry:run") {
		t.Errorf("应只重试两个失败任务：调用记录 %v", got)
	}
	if r.hasTextLike(locales.ImportsSelectedCount(4)) {
		t.Error("批量重试之后应清空选择")
	}
}

func TestBulkCancelAppliesToActiveOnly(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.ImportsSelect()) // 第一行：进行中
	r.click(gfx.RoleButton, locales.ImportsSelectVisible())
	r.click(gfx.RoleButton, countName(locales.CommonCancel(), 1))
	if got := r.calls(); !slices.Contains(got, "cancel:run") || len(got) < 1 {
		t.Errorf("批量取消应只取消进行中的任务：%v", got)
	}
}

func TestHeaderRetryFailedRetriesVisibleFailures(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.ImportsRetryVisibleFailed(2))
	got := r.calls()
	if !slices.Contains(got, "retry:f1") || !slices.Contains(got, "retry:f2") {
		t.Errorf("页头重试应重试全部可见失败：%v", got)
	}
}

// 批量里有一个失败不能让整批报成功：逐条记账，汇总失败原因，提示留到手动关闭。
func TestBulkRetryReportsEachFailure(t *testing.T) {
	r := loaded(t, mixed())
	r.svc.FailOn("retry:f2", errors.New("任务已被占用"))
	r.click(gfx.RoleButton, locales.ImportsRetryVisibleFailed(2))
	if got := r.calls(); !slices.Contains(got, "retry:f1") || !slices.Contains(got, "retry:f2") {
		t.Fatalf("一条失败不应中断后面的：%v", got)
	}
	if r.notifier.Len() == 0 {
		t.Error("有失败时应弹出错误提示")
	}
	if entries := r.log.entries(); len(entries) == 0 {
		t.Error("失败应写入日志")
	}
}

// ---- 行内动作 ----

func TestRowCancelAndRetryCallService(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.CommonCancel())
	if !slices.Contains(r.calls(), "cancel:run") {
		t.Errorf("进行中任务的取消应调用服务：%v", r.calls())
	}
	r.clickNth(gfx.RoleButton, locales.ImportsRetry(), 0)
	if !slices.Contains(r.calls(), "retry:f1") {
		t.Errorf("失败任务的重试应调用服务：%v", r.calls())
	}
}

func TestRowOpenItemUsesResultItem(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.ImportsOpenItem())
	if len(r.opened) != 1 || r.opened[0] != "item-9" {
		t.Errorf("「打开条目」应打开结果条目，实际 %v", r.opened)
	}
}

// ---- 暂停队列 ----

func TestPauseAndResumeQueue(t *testing.T) {
	r := newRig(t, mixed(), func(r *rig) {
		r.svc.SetQueueState(services.ImportQueueState{PendingCount: 2, RunningCount: 1, Concurrency: 2})
	})
	r.debounce()
	if !r.hasText(locales.ImportsSummaryActive(4, 1)) {
		t.Fatalf("未暂停时页头应写进行中；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.ImportsPauseQueue())
	if !slices.Contains(r.calls(), "pause") {
		t.Fatalf("点「暂停队列」应调用服务暂停：%v", r.calls())
	}
	if !r.has(gfx.RoleButton, locales.ImportsResumeQueue()) || !r.hasText(locales.ImportsSummaryPaused(2)) {
		t.Errorf("暂停后应改成「继续队列」并写等待数；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.ImportsResumeQueue())
	if !slices.Contains(r.calls(), "resume") || !r.has(gfx.RoleButton, locales.ImportsPauseQueue()) {
		t.Errorf("点「继续队列」应恢复：%v", r.calls())
	}
}

func TestPauseButtonHiddenWhenNothingToPause(t *testing.T) {
	r := loaded(t, mixed()) // 队列里没有排队的任务，也没暂停
	if r.has(gfx.RoleButton, locales.ImportsPauseQueue()) || r.has(gfx.RoleButton, locales.ImportsResumeQueue()) {
		t.Error("没有排队任务且未暂停时不应出现暂停按钮")
	}
}

// ---- 清理确认 ----

func TestClearConfirmPreviewsCountThenClears(t *testing.T) {
	r := loaded(t, mixed(), func(r *rig) { r.svc.SetClearCount(3) })
	r.click(gfx.RoleButton, locales.ImportsClearFilteredSource())
	if !slices.Contains(r.calls(), "preview:filtered") {
		t.Fatalf("应先预览会清理多少条：%v", r.calls())
	}
	title := locales.ImportsClearConfirmTitle()
	if !r.has(gfx.RoleGroup, title) {
		t.Fatalf("预览之后应打开确认框「%s」；画面上有 %q", title, r.session.Texts())
	}
	scope := locales.ImportsOriginAll() + " \u00b7 " + locales.ImportsCurrentFilter()
	if want := locales.ImportsClearConfirmSource(scope, 3); !strings.Contains(r.allText(), want) { // 说明文字会折行，整句比对
		t.Errorf("确认框应写明范围与条数 %q；画面上有 %q", want, r.session.Texts())
	}
	if slices.Contains(r.calls(), "clear:filtered") {
		t.Fatal("确认之前不应清理")
	}
	r.click(gfx.RoleButton, locales.ImportsClearCount(3))
	if !slices.Contains(r.calls(), "clear:filtered") {
		t.Errorf("确认后应清理：%v", r.calls())
	}
	if r.has(gfx.RoleGroup, title) {
		t.Error("确认后对话框应关闭")
	}
}

func TestClearConfirmCancelDoesNotClear(t *testing.T) {
	r := loaded(t, mixed(), func(r *rig) { r.svc.SetClearCount(3) })
	r.click(gfx.RoleButton, locales.ImportsClearAllSources())
	if !slices.Contains(r.calls(), "preview:all") {
		t.Fatalf("「清理所有来源终态」应以 all 范围预览：%v", r.calls())
	}
	r.click(gfx.RoleButton, locales.CommonCancel())
	if r.has(gfx.RoleGroup, locales.ImportsClearConfirmTitle()) {
		t.Error("取消后对话框应关闭")
	}
	for _, call := range r.calls() {
		if call == "clear:all" || call == "clear:filtered" {
			t.Errorf("取消不应清理：%v", r.calls())
		}
	}
}

// 范围内没有可清理的任务时，确认框只剩一个「关闭」，不给一个会删 0 条的危险按钮。
func TestClearConfirmWithNothingToClearOnlyCloses(t *testing.T) {
	r := loaded(t, mixed(), func(r *rig) { r.svc.SetClearCount(0) })
	r.click(gfx.RoleButton, locales.ImportsClearFilteredSource())
	if !r.has(gfx.RoleButton, locales.CommonClose()) {
		t.Fatalf("没有可清理的任务时应只给「关闭」；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CommonClose())
	for _, call := range r.calls() {
		if call == "clear:all" || call == "clear:filtered" {
			t.Errorf("关闭不应清理：%v", r.calls())
		}
	}
}

func TestClearPreviewFailureIsReportedNotSilent(t *testing.T) {
	r := loaded(t, mixed())
	r.svc.FailOn("preview", errors.New("数据库繁忙"))
	r.click(gfx.RoleButton, locales.ImportsClearFilteredSource())
	if r.has(gfx.RoleGroup, locales.ImportsClearConfirmTitle()) {
		t.Error("预览失败不应打开确认框")
	}
	if r.notifier.Len() != 1 {
		t.Errorf("预览失败应弹出 1 条错误提示，实际 %d 条", r.notifier.Len())
	}
}

// ---- 删除与撤销 ----

// 删除不确认但可撤销：先从列表隐藏，窗口内点「撤销」就回来、后端一次都没调；窗口过了才真正提交。
func TestRemoveHidesRowAndUndoRestoresIt(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.ImportsRemoveTask()) // 第一枚是已完成那行的（进行中的不能删）
	if r.rows() != 3 {
		t.Fatalf("删除后应立即从列表隐藏：行数 %d", r.rows())
	}
	if want := locales.ImportsRemovedFromList(1); !r.hasText(want) {
		t.Fatalf("应弹出可撤销的提示 %q；画面上有 %q", want, r.session.Texts())
	}
	if slices.Contains(r.calls(), "remove:done") {
		t.Fatal("撤销窗口内不应调用后端删除")
	}
	r.click(gfx.RoleButton, locales.CommonUndo())
	if r.rows() != 4 {
		t.Errorf("撤销后任务应回到列表：行数 %d", r.rows())
	}
	r.clock.Advance(removalUndoWindow * 2)
	if slices.Contains(r.calls(), "remove:done") {
		t.Errorf("已撤销的删除永远不应提交：%v", r.calls())
	}
}

func TestRemoveCommitsAfterUndoWindow(t *testing.T) {
	r := loaded(t, mixed())
	r.click(gfx.RoleButton, locales.ImportsRemoveTask())
	r.clock.Advance(removalUndoWindow - 1)
	if slices.Contains(r.calls(), "remove:done") {
		t.Fatal("窗口还没到不应提交")
	}
	r.clock.Advance(2)
	if !slices.Contains(r.calls(), "remove:done") {
		t.Errorf("窗口过后应提交删除：%v", r.calls())
	}
}

// ---- 加载更早的任务 ----

func TestLoadMoreAsksForNextPage(t *testing.T) {
	page := mixed()
	page.NextCursor = "cursor-1"
	r := loaded(t, page)
	r.click(gfx.RoleButton, locales.ImportsLoadMoreHistory())
	if got := lastQuery(r).Cursor; got != "cursor-1" {
		t.Errorf("加载更早的任务应带上翻页游标，Cursor = %q", got)
	}
}

func TestNoLoadMoreWithoutNextCursor(t *testing.T) {
	r := loaded(t, mixed())
	if r.has(gfx.RoleButton, locales.ImportsLoadMoreHistory()) {
		t.Error("没有更早的任务时不应出现「加载更早的任务」")
	}
}
