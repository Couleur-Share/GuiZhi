package library

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 WebSnapshotPane 背后的状态（store_snapshot.go）：读取（含乱序与失败）、补采入队后的轮询与收尾、取消补采、导出 HTML，
// 以及换条目、清详情、Store.Close 时的作废与停止轮询。条目沿用 detailHarness 的 a / b / c（假服务按条目与版本预设快照）。

func wsMeta() *services.LibraryWebSnapshotMeta {
	return &services.LibraryWebSnapshotMeta{
		Assets:   []services.LibraryWebSnapshotAsset{{FileName: "a.png", SHA256: "h", Bytes: 1}},
		Failures: []services.LibraryWebSnapshotFailure{},
		Warnings: []string{},
	}
}

func wsView(version string) services.LibraryWebSnapshotView {
	return services.LibraryWebSnapshotView{
		InstanceID: "inst-" + version,
		Document:   "<p>" + version + "</p>",
		Version:    &services.LibraryWebSnapshotVersion{ID: version, CapturedAt: 1000, Snapshot: wsMeta()},
	}
}

// snapshotHarness 选中条目 a，并预设它的当前版本 v2 与历史版本 v1。
func snapshotHarness(t *testing.T) *harness {
	t.Helper()
	h := detailHarness(t)
	h.svc.SeedSnapshot("a", "", wsView("v2"))
	h.svc.SeedSnapshot("a", "v1", wsView("v1"))
	h.open("a")
	return h
}

func (h *harness) snapshot() SnapshotView { return h.store.View().Detail.Snapshot }

// snapshotEmpty 报告快照状态是零值（SnapshotView 含切片，不能直接比较）。
func snapshotEmpty(s SnapshotView) bool {
	return s.ItemID == "" && s.VersionID == "" && !s.Loading && s.View == nil && s.Error == "" && !s.SupplementBusy && s.SupplementTasks == nil
}

func (h *harness) pollTick() { h.clock.Advance(snapshotPollInterval) }

// ---- 读取 ----

func TestSnapshotLoadReadsTheRequestedVersion(t *testing.T) {
	h := snapshotHarness(t)
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	s := h.snapshot()
	if s.Loading || s.Error != "" || s.View == nil || s.View.Version.ID != "v2" || s.ItemID != "a" || s.VersionID != "" {
		t.Fatalf("读取当前版本：%+v", s)
	}
	h.store.LoadWebSnapshot(h.ctx, "a", "v1")
	s = h.snapshot()
	if s.View == nil || s.View.Version.ID != "v1" || s.VersionID != "v1" {
		t.Fatalf("读取历史版本：%+v", s)
	}
	if want := []servicestest.SnapshotCall{{ItemID: "a"}, {ItemID: "a", VersionID: "v1"}}; !slices.Equal(h.svc.SnapshotCalls(), want) {
		t.Fatalf("请求 %+v", h.svc.SnapshotCalls())
	}
}

func TestSnapshotLoadIsAsyncAndShowsLoadingMeanwhile(t *testing.T) {
	h := snapshotHarness(t)
	var q spawnQueue
	h.store.Spawn = q.Spawn
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	if s := h.snapshot(); !s.Loading || s.View != nil || s.Error != "" {
		t.Fatalf("读取在后台：界面线程上应是读取中且没有旧快照：%+v", s)
	}
	if len(h.svc.SnapshotCalls()) != 0 {
		t.Fatal("读取不该在界面线程里同步发生")
	}
	q.Run()
	if s := h.snapshot(); s.Loading || s.View == nil {
		t.Fatalf("读完：%+v", s)
	}
}

func TestSnapshotReloadDropsTheOldViewAtOnce(t *testing.T) {
	h := snapshotHarness(t)
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	var q spawnQueue
	h.store.Spawn = q.Spawn
	h.store.LoadWebSnapshot(h.ctx, "a", "v1")
	if s := h.snapshot(); !s.Loading || s.View != nil {
		t.Fatalf("重新读取一开始就清掉旧快照（渲染层 setView(null)）：%+v", s)
	}
}

func TestSnapshotLoadOnlyForTheItemShownInTheDetail(t *testing.T) {
	h := snapshotHarness(t)
	h.store.LoadWebSnapshot(h.ctx, "b", "")
	h.store.LoadWebSnapshot(h.ctx, "", "")
	if len(h.svc.SnapshotCalls()) != 0 || h.snapshot().ItemID != "" {
		t.Fatalf("详情里不是这一条时不读取：%+v %+v", h.svc.SnapshotCalls(), h.snapshot())
	}
}

func TestSnapshotLoadFailureKeepsTheReasonAndRetryClearsIt(t *testing.T) {
	h := snapshotHarness(t)
	h.svc.FailOn("WebSnapshot:a", errors.New("读库失败"))
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	s := h.snapshot()
	if s.Loading || s.View != nil || s.Error != "读库失败" {
		t.Fatalf("失败：%+v", s)
	}
	if got := h.loggedUnder("library.webSnapshot"); len(got) != 1 {
		t.Errorf("失败要留日志：%v", got)
	}
	if len(h.queuedToasts()) != 0 {
		t.Errorf("读取失败显示在面板里（带重试），不另弹提示：%+v", h.queuedToasts())
	}
	h.svc.FailOn("WebSnapshot:a", nil)
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	if s = h.snapshot(); s.Error != "" || s.View == nil {
		t.Fatalf("重试成功后清掉错误：%+v", s)
	}
}

func TestSnapshotLoadFailureWithoutReasonUsesTheDefaultText(t *testing.T) {
	h := snapshotHarness(t)
	h.svc.FailOn("WebSnapshot:a", errors.New("  "))
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	if got := h.snapshot().Error; got != locales.LibrarySnapshotLoadFailed() {
		t.Fatalf("空原因：%q", got)
	}
}

func TestSnapshotCorruptViewIsAnErrorNotAnEmptyState(t *testing.T) {
	h := snapshotHarness(t)
	h.svc.SeedSnapshot("a", "", services.LibraryWebSnapshotView{Error: "图片校验失败"})
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	if s := h.snapshot(); s.View != nil || s.Error != "图片校验失败" {
		t.Fatalf("快照损坏要进错误态（带可复制原因），不能当成「还没有快照」：%+v", s)
	}
}

func TestSnapshotNeverSavedIsAViewWithoutVersion(t *testing.T) {
	h := snapshotHarness(t)
	h.svc.SeedSnapshot("a", "", services.LibraryWebSnapshotView{})
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	if s := h.snapshot(); s.Error != "" || s.View == nil || s.View.Version != nil {
		t.Fatalf("没有保存过快照：%+v", s)
	}
}

func TestSnapshotOlderReadNeverOverwritesANewerOne(t *testing.T) {
	h := snapshotHarness(t)
	var q spawnQueue
	h.store.Spawn = q.Spawn
	h.store.LoadWebSnapshot(h.ctx, "a", "")   // 先发起的读取排在队首
	h.store.LoadWebSnapshot(h.ctx, "a", "v1") // 后发起的才是用户此刻要看的
	q.Run()
	if s := h.snapshot(); s.View == nil || s.View.Version.ID != "v1" || s.VersionID != "v1" {
		t.Fatalf("乱序返回的旧结果不能盖掉新的：%+v", s)
	}
}

func TestSnapshotReadInFlightIsDroppedWhenTheItemChanges(t *testing.T) {
	h := snapshotHarness(t)
	var q spawnQueue
	h.store.Spawn = q.Spawn
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	if !h.store.SelectItemWait(h.ctx, "b") {
		t.Fatal("切到 b 失败")
	}
	q.Run()
	if s := h.snapshot(); !snapshotEmpty(s) {
		t.Fatalf("换条目后旧条目的读取结果要丢弃：%+v", s)
	}
}

func TestSnapshotStateResetsOnSelectAndClear(t *testing.T) {
	h := snapshotHarness(t)
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	h.open("b")
	if s := h.snapshot(); s.ItemID != "" || s.View != nil || s.Loading || s.Error != "" {
		t.Fatalf("换条目要清空：%+v", s)
	}
	h.store.LoadWebSnapshot(h.ctx, "b", "") // b 没有预设快照：读取失败也要在换条目时一并清掉
	h.store.SelectItem("a")
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	h.store.SelectItem("")
	if s := h.snapshot(); s.ItemID != "" || s.View != nil {
		t.Fatalf("取消选中要清空：%+v", s)
	}
}

// ---- 补采 ----

func supplementHarness(t *testing.T) *harness {
	t.Helper()
	h := snapshotHarness(t)
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	h.svc.SeedSupplementTasks("t1")
	h.svc.SeedImportStatuses(services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusProcessing, RefreshOfItemID: "a"})
	return h
}

func TestSnapshotSupplementPollsEverySecondUntilTheTaskEnds(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	if s := h.snapshot(); !s.SupplementBusy || !slices.Equal(s.SupplementTasks, []string{"t1"}) {
		t.Fatalf("入队后：%+v", s)
	}
	if got := h.svc.Supplemented(); len(got) != 1 || !slices.Equal(got[0], []string{"a"}) {
		t.Fatalf("入队请求 %v", got)
	}
	if h.clock.Pending() != 1 {
		t.Fatalf("应排好一次轮询：%d", h.clock.Pending())
	}
	reads := len(h.svc.SnapshotCalls())
	h.pollTick()
	if s := h.snapshot(); !s.SupplementBusy || h.clock.Pending() != 1 {
		t.Fatalf("任务还在处理：仍忙且排好下一次轮询：%+v pending=%d", s, h.clock.Pending())
	}
	if len(h.queuedToasts()) != 0 || len(h.svc.SnapshotCalls()) != reads {
		t.Fatal("任务没结束前不提示、不重新读取")
	}
	h.svc.SeedSupplementTasks("t1")
	h.svc.SeedImportStatuses(services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCompleted, RefreshOfItemID: "a"})
	h.pollTick()
	s := h.snapshot()
	if s.SupplementBusy || s.SupplementTasks != nil || h.clock.Pending() != 0 {
		t.Fatalf("结束后放下忙标记并停止轮询：%+v pending=%d", s, h.clock.Pending())
	}
	if len(h.svc.SnapshotCalls()) != reads+1 || s.View == nil || s.Loading {
		t.Fatalf("结束后重新读取快照：%+v 读取 %d→%d", s, reads, len(h.svc.SnapshotCalls()))
	}
	toast := onlyToast(t, h)
	if toast.Kind != kit.ToastSuccess || toast.Message != locales.LibrarySnapshotVersionSaved() || toast.Detail != "" {
		t.Fatalf("提示 %+v", toast)
	}
}

func TestSnapshotSupplementReloadKeepsTheViewedVersion(t *testing.T) {
	h := supplementHarness(t)
	h.store.LoadWebSnapshot(h.ctx, "a", "v1")
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.svc.SeedImportStatuses(services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCompleted})
	h.pollTick()
	calls := h.svc.SnapshotCalls()
	if last := calls[len(calls)-1]; last != (servicestest.SnapshotCall{ItemID: "a", VersionID: "v1"}) {
		t.Fatalf("补采完成后重新读取的是正在看的版本：%+v", last)
	}
}

func TestSnapshotSupplementFailureReasonsGoIntoTheCopyableDetail(t *testing.T) {
	cases := []struct {
		name   string
		status services.LibraryImportTaskStatus
		kind   kit.ToastKind
		msg    string
		detail string
	}{
		{"失败", services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusFailed, Error: "抓取超时"}, kit.ToastError, locales.LibrarySnapshotSupplementIncomplete(), "抓取超时"},
		{"被取消", services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCanceled}, kit.ToastError, locales.LibrarySnapshotSupplementIncomplete(), ""},
		{"完成但有警告", services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCompleted, Warning: "部分图片缺失"}, kit.ToastSuccess, locales.LibrarySnapshotVersionSaved(), "部分图片缺失"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := supplementHarness(t)
			h.store.SupplementWechatSnapshot(h.ctx, "a")
			c.status.RefreshOfItemID = "a"
			h.svc.SeedImportStatuses(c.status)
			h.pollTick()
			toast := onlyToast(t, h)
			if toast.Kind != c.kind || toast.Message != c.msg || toast.Detail != c.detail {
				t.Fatalf("提示 %+v", toast)
			}
			if h.snapshot().SupplementBusy {
				t.Fatal("结束后忙标记要放下")
			}
		})
	}
}

func TestSnapshotSupplementTreatsAVanishedTaskAsEnded(t *testing.T) {
	h := snapshotHarness(t)
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	h.svc.SeedSupplementTasks("gone") // 没登记过状态：模拟任务已被清理
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.pollTick()
	if h.snapshot().SupplementBusy || h.clock.Pending() != 0 {
		t.Fatalf("读不到的任务按已结束处理，否则忙标记永远不放下：%+v", h.snapshot())
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastSuccess {
		t.Fatalf("提示 %+v", toast)
	}
}

func TestSnapshotSupplementEnqueueFailureReleasesTheBusyFlag(t *testing.T) {
	h := supplementHarness(t)
	h.svc.FailOn("SupplementWechatTasks", errors.New("队列已满"))
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	s := h.snapshot()
	if s.SupplementBusy || s.SupplementTasks != nil || h.clock.Pending() != 0 {
		t.Fatalf("入队失败：%+v", s)
	}
	toast := onlyToast(t, h)
	if toast.Kind != kit.ToastError || toast.Message != locales.CommonOperationFailed(locales.LibrarySnapshotSupplementWechat()) || toast.Detail != "队列已满" {
		t.Fatalf("提示 %+v", toast)
	}
	if got := h.loggedUnder(locales.LibrarySnapshotSupplementWechat()); len(got) != 1 {
		t.Errorf("失败要留日志：%v", h.log.entries())
	}
	// 失败后可以重试
	h.svc.FailOn("SupplementWechatTasks", nil)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	if !h.snapshot().SupplementBusy {
		t.Fatal("放下忙标记后应能再次补采")
	}
}

func TestSnapshotSupplementIsNotReentrant(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	if got := h.svc.Supplemented(); len(got) != 1 {
		t.Fatalf("补采在途时不再入队：%v", got)
	}
	if h.clock.Pending() != 1 {
		t.Fatalf("只该有一个轮询：%d", h.clock.Pending())
	}
}

func TestSnapshotSupplementOnlyForTheItemShownInTheDetail(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "b")
	if len(h.svc.Supplemented()) != 0 || h.snapshot().SupplementBusy {
		t.Fatal("详情里不是这一条时不补采")
	}
}

func TestSnapshotSupplementStatusReadFailureStopsWaiting(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.svc.FailOn("ImportTaskStatuses", errors.New("队列服务不可用"))
	h.pollTick()
	s := h.snapshot()
	if s.SupplementBusy || s.SupplementTasks != nil || s.Error != "队列服务不可用" || h.clock.Pending() != 0 {
		t.Fatalf("读不到状态就别一直等：%+v pending=%d", s, h.clock.Pending())
	}
	if len(h.loggedUnder("library.snapshotSupplementPoll")) != 1 {
		t.Errorf("后台失败留日志：%v", h.log.entries())
	}
	if len(h.queuedToasts()) != 0 {
		t.Errorf("错误显示在面板里，不弹提示：%+v", h.queuedToasts())
	}
}

func TestSnapshotSwitchingItemStopsThePollingAndSilencesTheEnd(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.open("b")
	if h.clock.Pending() != 0 {
		t.Fatalf("换条目要停掉轮询：%d", h.clock.Pending())
	}
	h.svc.SeedImportStatuses(services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCompleted})
	reads := len(h.svc.SnapshotCalls())
	h.pollTick()
	if len(h.queuedToasts()) != 0 || len(h.svc.SnapshotCalls()) != reads || h.snapshot().SupplementBusy {
		t.Fatalf("离开之后不提示、不读取、不带着忙标记：%+v %+v", h.queuedToasts(), h.snapshot())
	}
}

func TestSnapshotStatusReadInFlightIsDroppedWhenTheItemChanges(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	release := h.svc.HoldImportStatuses()
	done := make(chan struct{})
	go func() { h.pollTick(); close(done) }()
	waitFor(t, "状态读取卡在闸门上", func() bool { return slices.Contains(h.svc.Calls(), "ImportTaskStatuses") })
	h.open("b")
	h.svc.SeedImportStatuses(services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCompleted})
	release()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("轮询没有结束")
	}
	if len(h.queuedToasts()) != 0 || h.clock.Pending() != 0 || !snapshotEmpty(h.snapshot()) {
		t.Fatalf("在途的状态读取回来发现换了条目：丢弃，不提示、不再排轮询：%+v pending=%d", h.snapshot(), h.clock.Pending())
	}
}

func TestSnapshotCloseStopsThePolling(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.store.Close()
	if h.clock.Pending() != 0 {
		t.Fatalf("Close 要停掉轮询：%d", h.clock.Pending())
	}
	h.store.Close() // 可重复调用
}

// ---- 取消补采 ----

func TestSnapshotCancelSupplementCancelsTheTasksAndThePollingFinishesIt(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.store.CancelWechatSupplement(h.ctx)
	if got := h.svc.CanceledImportTasks(); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("取消的任务 %v", got)
	}
	if len(h.queuedToasts()) != 0 || !h.snapshot().SupplementBusy {
		t.Fatal("取消本身不提示；收尾由轮询发现任务结束时统一做")
	}
	h.pollTick()
	toast := onlyToast(t, h)
	if toast.Kind != kit.ToastError || toast.Message != locales.LibrarySnapshotSupplementIncomplete() || h.snapshot().SupplementBusy {
		t.Fatalf("取消后的收尾：%+v %+v", toast, h.snapshot())
	}
}

func TestSnapshotCancelSupplementReportsATaskThatAlreadyEnded(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.svc.SeedImportStatuses(services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCompleted})
	h.store.CancelWechatSupplement(h.ctx)
	toast := onlyToast(t, h)
	if toast.Kind != kit.ToastError || toast.Message != locales.CommonOperationFailed(locales.LibrarySnapshotCancelSupplement()) || toast.Detail != locales.NativeLibrarySnapshotTaskEnded() {
		t.Fatalf("提示 %+v", toast)
	}
}

func TestSnapshotCancelSupplementFailureIsReported(t *testing.T) {
	h := supplementHarness(t)
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.svc.FailOn("CancelImportTask:t1", errors.New("取消失败"))
	h.store.CancelWechatSupplement(h.ctx)
	if toast := onlyToast(t, h); toast.Detail != "取消失败" {
		t.Fatalf("提示 %+v", toast)
	}
}

func TestSnapshotCancelSupplementIsANoopWhenNothingIsRunning(t *testing.T) {
	h := supplementHarness(t)
	h.store.CancelWechatSupplement(h.ctx)
	if len(h.svc.CanceledImportTasks()) != 0 || len(h.queuedToasts()) != 0 {
		t.Fatal("没有补采在途时取消什么也不做")
	}
}

// ---- 导出 ----

func TestSnapshotExportToastsByOutcome(t *testing.T) {
	cases := []struct {
		name    string
		res     *services.LibraryWebSnapshotExportResult
		fail    error
		toasts  int
		kind    kit.ToastKind
		message string
	}{
		{"成功", nil, nil, 1, kit.ToastSuccess, locales.LibrarySnapshotExported()},
		{"有缺失", &services.LibraryWebSnapshotExportResult{Path: "/x", Incomplete: true}, nil, 1, kit.ToastWarning, locales.LibrarySnapshotExportedIncomplete()},
		{"用户没选目录", &services.LibraryWebSnapshotExportResult{Canceled: true}, nil, 0, kit.ToastInfo, ""},
		{"失败", nil, errors.New("磁盘已满"), 1, kit.ToastError, locales.CommonOperationFailed(locales.LibrarySnapshotExportHtml())},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := snapshotHarness(t)
			if c.res != nil {
				h.svc.SeedSnapshotExport("a", *c.res)
			}
			if c.fail != nil {
				h.svc.FailOn("ExportWebSnapshotHTML", c.fail)
			}
			h.store.ExportWebSnapshotHTML(h.ctx, "a", "v2")
			if want := []servicestest.SnapshotCall{{ItemID: "a", VersionID: "v2"}}; !slices.Equal(h.svc.SnapshotExports(), want) {
				t.Fatalf("导出请求 %+v", h.svc.SnapshotExports())
			}
			toasts := h.queuedToasts()
			if len(toasts) != c.toasts {
				t.Fatalf("提示 %+v", toasts)
			}
			if c.toasts == 1 {
				if toasts[0].Kind != c.kind || toasts[0].Message != c.message {
					t.Fatalf("提示 %+v", toasts[0])
				}
				if c.fail != nil && !strings.Contains(toasts[0].Detail, "磁盘已满") {
					t.Fatalf("失败原因要放进详情：%+v", toasts[0])
				}
			}
		})
	}
}

func TestSnapshotExportIsNotReentrantWhileTheDialogIsOpen(t *testing.T) {
	h := snapshotHarness(t)
	goSpawn(h)
	release := h.svc.HoldSnapshotExport()
	h.store.ExportWebSnapshotHTML(h.ctx, "a", "v2")
	h.store.ExportWebSnapshotHTML(h.ctx, "a", "v2")
	release()
	waitFor(t, "导出提示", func() bool { return len(h.queuedToasts()) == 1 })
	if got := h.svc.SnapshotExports(); len(got) != 1 {
		t.Fatalf("选择框开着时不再触发第二次导出：%+v", got)
	}
	waitFor(t, "放下导出标记", func() bool {
		h.store.mu.Lock()
		defer h.store.mu.Unlock()
		return !h.store.detail.snapshot.exporting
	})
	h.store.ExportWebSnapshotHTML(h.ctx, "a", "v2")
	waitFor(t, "第二次导出", func() bool { return len(h.svc.SnapshotExports()) == 2 })
}

// 同一代里先发起的读取更晚返回（乱序）：旧结果不能盖掉新结果（按序号作废，不止按代号）。
func TestSnapshotOlderReadReturningLastIsDropped(t *testing.T) {
	h := snapshotHarness(t)
	var q spawnQueue
	h.store.Spawn = q.Spawn
	h.store.LoadWebSnapshot(h.ctx, "a", "")
	h.store.LoadWebSnapshot(h.ctx, "a", "v1")
	if len(q.queued) != 2 {
		t.Fatalf("应有两个待执行的读取：%d", len(q.queued))
	}
	q.queued[1]() // 新的先到
	q.queued[0]() // 旧的后到
	if s := h.snapshot(); s.View == nil || s.View.Version.ID != "v1" || s.VersionID != "v1" || s.Loading {
		t.Fatalf("后到的旧结果不能盖掉新的：%+v", s)
	}
}

// 补采服务的替身：入队不返回任务；状态读取多返回一条与本次无关的未结束任务（服务可能返回比请求更多的状态）。
type snapshotOddLibrary struct {
	services.Library
	noTasks bool
}

func (l snapshotOddLibrary) SupplementWechatTasks(ctx context.Context, itemIDs []string) ([]string, error) {
	if l.noTasks {
		return nil, nil
	}
	return l.Library.SupplementWechatTasks(ctx, itemIDs)
}

func (l snapshotOddLibrary) ImportTaskStatuses(ctx context.Context, ids []string) ([]services.LibraryImportTaskStatus, error) {
	all, err := l.Library.ImportTaskStatuses(ctx, ids)
	return append(all, services.LibraryImportTaskStatus{ID: "unrelated", Status: services.ImportStatusProcessing}), err
}

// 入队成功却没有任务可等：当作失败放下忙标记并提示，不排轮询（否则忙标记永远不放下）。
func TestSnapshotSupplementWithoutAnyTaskIsAFailure(t *testing.T) {
	h := supplementHarness(t)
	h.store.svc = snapshotOddLibrary{Library: h.store.svc, noTasks: true}
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	if s := h.snapshot(); s.SupplementBusy || s.SupplementTasks != nil || h.clock.Pending() != 0 {
		t.Fatalf("没有任务可等：%+v pending=%d", s, h.clock.Pending())
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError {
		t.Fatalf("提示 %+v", toast)
	}
}

// 轮询只看本次入队的任务：状态里混着别的未结束任务，不能让这次补采一直等下去。
func TestSnapshotSupplementPollIgnoresUnrelatedTasks(t *testing.T) {
	h := supplementHarness(t)
	h.store.svc = snapshotOddLibrary{Library: h.store.svc}
	h.store.SupplementWechatSnapshot(h.ctx, "a")
	h.svc.SeedImportStatuses(services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusCompleted, RefreshOfItemID: "a"})
	h.pollTick()
	if h.snapshot().SupplementBusy || h.clock.Pending() != 0 {
		t.Fatalf("无关任务不该拖住这次补采：%+v pending=%d", h.snapshot(), h.clock.Pending())
	}
}
