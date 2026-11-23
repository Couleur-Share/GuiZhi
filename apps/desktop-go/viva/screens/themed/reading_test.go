package themed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

func TestOpenLoadsEmptyAndAutoSetupOnce(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	v := r.view()
	if v.Result == nil || v.Page() != nil || v.LoadError != "" || v.Task != nil {
		t.Fatalf("空内容应读到「没有页面」的结果: %+v", v)
	}
	if v.Result.Models.Text == nil || v.InstanceID != "inst" {
		t.Fatalf("应带模型信息和实例标识: %+v", v)
	}
	if !r.reading.ConsumeAutoSetup(false, false) || r.view().Setup != SetupNew {
		t.Fatal("没有原文可退回、没有页面和任务时，首次读到后应自动打开生成设置")
	}
	r.reading.CloseSetup()
	if r.reading.ConsumeAutoSetup(false, false) {
		t.Fatal("同一次选中只自动打开一次")
	}
	// 换内容后重新计一次；有原文可退回或只读时不自动打开
	r.open(reqB)
	if r.reading.ConsumeAutoSetup(true, false) {
		t.Fatal("有原文可退回时不自动打开")
	}
	r.reading.Open("item-c", services.ThemedKindBody, "k", true)
	if r.reading.ConsumeAutoSetup(false, true) {
		t.Fatal("条目只读时不自动打开")
	}
}

func TestOpenWithPageAndLegacyDocument(t *testing.T) {
	r := newRig(t)
	p := r.svc.PutPage(reqA, services.ThemedPage{FormatVersion: 1})
	r.svc.SetDocument(reqA, "<html>old</html>")
	r.open(reqA)
	v := r.view()
	if v.Page() == nil || v.Page().ID != p.ID || v.Result.Document != "<html>old</html>" {
		t.Fatalf("旧格式页面应带整页文档: %+v", v.Result)
	}
	m := v.Pane(false, false)
	if m.Body != BodyLegacyFrame || !m.LegacyNote || m.StatusStrip {
		t.Fatalf("旧格式页面走 iframe 并提示只读: %+v", m)
	}
	// 页面不显示时只读元数据
	r.reading.Open("item-a", services.ThemedKindBody, "key-item-a", false)
	calls := r.svc.Calls().Gets
	if last := calls[len(calls)-1]; !last.MetadataOnly {
		t.Fatalf("页面不显示时应只读元数据: %+v", last)
	}
}

func TestOpenSameContentDoesNotReload(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	n := len(r.svc.Calls().Gets)
	r.open(reqA)
	if got := len(r.svc.Calls().Gets); got != n {
		t.Fatalf("内容没变不应重读: %d -> %d", n, got)
	}
	r.reading.Open(reqA.ItemID, reqA.SourceKind, "changed", true)
	if got := len(r.svc.Calls().Gets); got != n+1 {
		t.Fatalf("正文摘要变了要重读（过期标记随之变化）: %d", got)
	}
}

func TestLoadFailureIsRetryableNotEmpty(t *testing.T) {
	r := newRig(t)
	r.svc.Fail(servicestest.ThemedOpGet, errors.New("disk offline"))
	r.open(reqA)
	v := r.view()
	if v.LoadError != "disk offline" || v.Result != nil {
		t.Fatalf("读取失败应进入错误态: %+v", v)
	}
	m := v.Pane(false, false)
	if !m.ShowLoadError || m.ShowLoading || m.Body != BodyNone {
		t.Fatalf("错误态不能渲染成空态或加载中: %+v", m)
	}
	if got := r.log.entries(); len(got) != 1 || !strings.Contains(got[0], "disk offline") {
		t.Fatalf("后台失败应留痕: %v", got)
	}
	r.svc.Fail(servicestest.ThemedOpGet, nil)
	r.reading.Reload()
	if v := r.view(); v.LoadError != "" || v.Result == nil {
		t.Fatalf("重试成功后应恢复: %+v", v)
	}
}

func TestSwitchingItemIgnoresLateReadOfPreviousItem(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{ID: "page-a"})
	r.svc.PutPage(reqB, services.ThemedPage{ID: "page-b"})
	r.reading.Spawn = func(fn func()) { go fn() }
	release, arrived := r.svc.Hold(servicestest.ThemedOpGet)
	r.open(reqA)
	<-arrived // A 的读取卡在服务里
	r.open(reqB)
	waitFor(t, "B 读完", func() bool { v := r.view(); return v.Result != nil && v.Page() != nil })
	release()
	time.Sleep(50 * time.Millisecond)
	if v := r.view(); v.ItemID != reqB.ItemID || v.Page() == nil || v.Page().ID != "page-b" {
		t.Fatalf("A 的晚到响应不能盖住 B: %+v", v)
	}
}

func TestOperationStaysOnItsOwnItemAcrossSwitch(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{ID: "page-a"})
	r.svc.PutPage(reqB, services.ThemedPage{ID: "page-b"})
	r.svc.SetExportPath("C:/out.html")
	r.open(reqA)
	release, arrived := r.svc.Hold(servicestest.ThemedOpExport)
	done := make(chan bool, 1)
	go func() { done <- r.reading.ExportHTML(false, false) }()
	<-arrived
	r.open(reqB)
	readsBefore := len(r.svc.Calls().Gets)
	release()
	if !<-done {
		t.Fatal("导出应成功")
	}
	exports := r.svc.Calls().Exports
	if len(exports) != 1 || exports[0].ItemID != reqA.ItemID || exports[0].VersionID != "page-a" {
		t.Fatalf("导出必须发给开始时的那份内容: %+v", exports)
	}
	if v := r.view(); v.ItemID != reqB.ItemID || v.Busy {
		t.Fatalf("旧操作的收尾不能碰新内容的忙标记: %+v", v)
	}
	if got := len(r.svc.Calls().Gets); got != readsBefore {
		t.Fatalf("旧操作收尾不应重读新内容: %d -> %d", readsBefore, got)
	}
}

func TestGenerateFlowPinsPageAndOffersLatest(t *testing.T) {
	r := newRig(t)
	old := r.svc.PutPage(reqA, services.ThemedPage{ID: "page-old"})
	r.open(reqA)
	r.reading.Attach()
	if !r.reading.Generate(services.ThemedOptions{GenerateImages: false, MaxImages: 3}) {
		t.Fatal("生成应启动")
	}
	v := r.view()
	if v.Task == nil || v.Task.State != services.ThemedTaskQueued || !v.Active() || !v.Disabled(false) {
		t.Fatalf("生成后应有排队任务并禁用页面操作: %+v", v.Task)
	}
	if v.Page().ID != old.ID {
		t.Fatal("生成期间仍显示旧版")
	}
	r.svc.UpdateTask(v.Task.ID, func(t *services.ThemedTask) {
		t.State, t.Stage = services.ThemedTaskRunning, services.ThemedStageDesign
	})
	if v := r.view(); v.Task.Stage != services.ThemedStageDesign {
		t.Fatalf("推送应更新任务: %+v", v.Task)
	}
	r.svc.CompleteTask(v.Task.ID)
	v = r.view()
	if v.Active() || v.Latest == nil || v.Page().ID != old.ID || v.Latest.Page.ID == old.ID {
		t.Fatalf("完成后仍固定旧版，新版放在 Latest 里等切换: page=%v latest=%v", v.Page().ID, v.Latest)
	}
	if m := v.Pane(false, false); !m.Toolbar.SwitchLatest {
		t.Fatalf("应出现「新版已完成 · 切换」: %+v", m.Toolbar)
	}
	r.reading.AdoptLatest()
	if v := r.view(); v.Latest != nil || v.Page().ID == old.ID {
		t.Fatalf("切换后应显示新版: %+v", v)
	}
}

func TestGenerateFailureShowsReasonAndKeepsRetry(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.svc.Fail(servicestest.ThemedOpGenerate, errors.New("text model is not configured"))
	if r.reading.Generate(services.ThemedOptions{}) {
		t.Fatal("失败不能报成功")
	}
	toast := r.lastToast()
	if toast.Kind != kit.ToastError || toast.Detail != "text model is not configured" {
		t.Fatalf("失败要弹带原因的错误提示: %+v", toast)
	}
	if v := r.view(); v.Busy || v.Task != nil {
		t.Fatalf("失败后可以重试: %+v", v)
	}
	r.svc.Fail(servicestest.ThemedOpGenerate, nil)
	if !r.reading.Generate(services.ThemedOptions{}) {
		t.Fatal("重试应成功")
	}
}

func TestPreGenerateErrorBlocksGenerate(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.PreGenerate = func(context.Context) error { return errors.New("unsaved edits") }
	if r.reading.Generate(services.ThemedOptions{}) {
		t.Fatal("有未保存改动时不应生成")
	}
	if len(r.svc.Calls().Generates) != 0 {
		t.Fatal("不应发出生成请求")
	}
	if toast := r.lastToast(); toast.Detail != "unsaved edits" {
		t.Fatalf("应提示原因: %+v", toast)
	}
}

func TestOnlyOneMutationAtATime(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{})
	r.svc.SetExportPath("C:/o.html")
	r.open(reqA)
	release, arrived := r.svc.Hold(servicestest.ThemedOpExport)
	done := make(chan bool, 1)
	go func() { done <- r.reading.ExportHTML(false, false) }()
	<-arrived
	if r.reading.Remove() {
		t.Fatal("操作进行中不能再发起别的变更")
	}
	if !r.view().Busy {
		t.Fatal("应显示忙")
	}
	release()
	<-done
	if r.view().Busy {
		t.Fatal("结束后应释放忙")
	}
}

func TestCancelResumeContinueOffline(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.Attach()
	r.reading.Generate(services.ThemedOptions{})
	id := r.view().Task.ID
	if !r.reading.Cancel() {
		t.Fatal("停止应成功")
	}
	if v := r.view(); v.Task.State != services.ThemedTaskCancelled || v.Active() {
		t.Fatalf("停止后任务应为已取消: %+v", v.Task)
	}
	if !r.reading.Resume() {
		t.Fatal("继续应成功")
	}
	if r.view().Task.ID != id || !r.view().Active() {
		t.Fatalf("继续后任务重新排队: %+v", r.view().Task)
	}
	r.svc.UpdateTask(id, func(t *services.ThemedTask) {
		t.State, t.Stage, t.Error = services.ThemedTaskPartial, services.ThemedStageResearch, "search is unavailable"
	})
	if !r.reading.ContinueOffline() {
		t.Fatal("不联网继续应成功")
	}
	c := r.svc.Calls()
	if len(c.Cancels) != 1 || len(c.Resumes) != 1 || len(c.Offlines) != 1 {
		t.Fatalf("各发一次请求: %+v", c)
	}
}

func TestTaskActionWithoutTaskIsNoop(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	if r.reading.Cancel() || r.reading.Resume() || r.reading.ContinueOffline() {
		t.Fatal("没有任务时什么也不做")
	}
	if len(r.toasts()) != 0 {
		t.Fatal("不应弹提示")
	}
}

func TestRestoreRemoveAndConfirmDelete(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{ID: "v1"})
	r.svc.PutPage(reqA, services.ThemedPage{ID: "v2"})
	r.svc.PutPrevious(reqA, services.ThemedPage{ID: "v1"})
	removed := 0
	r.reading.OnPageRemoved = func() { removed++ }
	r.open(reqA)
	if !r.view().Result.Previous {
		t.Fatal("应有上一版")
	}
	if !r.reading.Restore() || r.view().Page().ID != "v1" {
		t.Fatalf("恢复后应显示上一版: %+v", r.view().Page())
	}
	if toast := r.lastToast(); toast.Kind != kit.ToastSuccess {
		t.Fatalf("恢复成功要提示: %+v", toast)
	}
	// 删除失败：确认框保留，不通知退回原文
	r.svc.Fail(servicestest.ThemedOpRemove, errors.New("locked"))
	r.reading.RequestDelete()
	if r.reading.ConfirmDelete() || !r.view().DeleteConfirm || removed != 0 {
		t.Fatal("删除失败应保留确认框且不报成功")
	}
	r.svc.Fail(servicestest.ThemedOpRemove, nil)
	if !r.reading.ConfirmDelete() || r.view().DeleteConfirm || removed != 1 || r.view().Page() != nil {
		t.Fatalf("删除成功应关闭确认并通知退回原文: %+v", r.view())
	}
}

func TestExportCancelledIsNotFailureNorSuccess(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{})
	r.open(reqA) // 导出路径为空：用户在保存对话框取消
	if !r.reading.ExportHTML(true, false) {
		t.Fatal("取消不算失败")
	}
	if len(r.toasts()) != 0 {
		t.Fatalf("取消不弹任何提示: %+v", r.toasts())
	}
	r.svc.SetExportPath("C:/o.html")
	r.reading.ExportHTML(false, true)
	if toast := r.lastToast(); toast.Kind != kit.ToastSuccess {
		t.Fatalf("导出成功要提示: %+v", toast)
	}
	if e := r.svc.Calls().Exports; !e[0].WithoutImages || !e[1].StaticOnly {
		t.Fatalf("导出选项要传下去: %+v", e)
	}
}

func TestDetachStopsProgressButNeverCancelsTask(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.Attach()
	r.reading.Attach() // 重复订阅无副作用
	if r.svc.Subscribers() != 1 {
		t.Fatalf("应只订阅一次: %d", r.svc.Subscribers())
	}
	r.reading.Generate(services.ThemedOptions{})
	id := r.view().Task.ID
	r.reading.Detach()
	if r.svc.Subscribers() != 0 {
		t.Fatal("应已退订")
	}
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.Stage = services.ThemedStageDesign })
	if r.view().Task.Stage == services.ThemedStageDesign {
		t.Fatal("退订后不再更新")
	}
	if len(r.svc.Calls().Cancels) != 0 {
		t.Fatal("关闭面板绝不隐式取消已付费的任务")
	}
}

func TestProgressForOtherItemIsIgnored(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.Attach()
	r.svc.PutPage(reqB, services.ThemedPage{})
	other := r.svc.PutTask(services.ThemedTask{ItemID: reqB.ItemID, SourceKind: reqB.SourceKind, State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign})
	r.svc.UpdateTask(other.ID, func(t *services.ThemedTask) { t.Completed = 1 })
	if r.view().Task != nil {
		t.Fatalf("别的内容的进度不能串进来: %+v", r.view().Task)
	}
}

func TestRegenerateAssetUsesRequestedAsset(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{Assets: []services.ThemedAsset{{ID: "img-1", Role: services.ThemedAssetGenerated, Status: services.ThemedAssetFailed}}})
	r.open(reqA)
	if !r.reading.RegenerateAsset("img-1") {
		t.Fatal("重做应启动")
	}
	if c := r.svc.Calls().Regenerates; len(c) != 1 || c[0].AssetID != "img-1" || c[0].Req != reqA {
		t.Fatalf("%+v", c)
	}
	if v := r.view(); v.Task == nil || v.Task.AssetID != "img-1" {
		t.Fatalf("应出现重做任务: %+v", v.Task)
	}
}

// ---- 静态预览 ----

func previewPage(rev int) *services.ThemedPreview {
	return &services.ThemedPreview{Revision: rev, Page: services.ThemedPage{ID: "draft", FormatVersion: 3}}
}

func TestPreviewScheduledThrottledAndMonotonic(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.Attach()
	r.reading.Generate(services.ThemedOptions{})
	id := r.view().Task.ID

	// 任务还没有可预览的章节：不排程
	if r.timers.count() != 0 {
		t.Fatalf("没有预览版本号时不应排程: %d", r.timers.count())
	}
	r.svc.SetPreview(reqA, previewPage(1))
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.State, t.PreviewRevision = services.ThemedTaskRunning, 1 })
	if r.timers.count() != 1 || r.timers.delay(0) != 0 {
		t.Fatalf("首次预览应立即排程: %d", r.timers.count())
	}
	// 同一组条件不重复排程
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.Completed = 1 })
	if r.timers.count() != 1 {
		t.Fatalf("同一版本号不应重复排程: %d", r.timers.count())
	}
	r.clock.Store(100)
	r.timers.fire(0)
	if v := r.view(); v.Preview == nil || v.Preview.Revision != 1 || !v.ShowingPreview() || v.ReadingPage().ID != "draft" {
		t.Fatalf("没有页面时应直接显示预览: %+v", v.Preview)
	}
	// 新版本号：距上次拉取（100ms 时）不足 500ms，要等差额
	r.clock.Store(200)
	r.svc.SetPreview(reqA, previewPage(2))
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.PreviewRevision = 2 })
	if r.timers.count() != 2 || r.timers.delay(1) != 400*time.Millisecond {
		t.Fatalf("两次拉取至少间隔 500ms: delay=%v", r.timers.delay(1))
	}
	// 旧排程作废：再来一个新版本号，先前未触发的排程不应拉取
	r.svc.SetPreview(reqA, previewPage(3))
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.PreviewRevision = 3 })
	before := len(r.svc.Calls().Previews)
	r.timers.fire(1)
	if len(r.svc.Calls().Previews) != before {
		t.Fatal("被取代的排程不应拉取")
	}
	r.timers.fire(2)
	if r.view().Preview.Revision != 3 {
		t.Fatalf("应显示最新预览: %+v", r.view().Preview)
	}
}

func TestPreviewLateOlderRevisionDoesNotOverwrite(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.Attach()
	r.reading.Generate(services.ThemedOptions{})
	id := r.view().Task.ID
	r.svc.SetPreview(reqA, previewPage(5))
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.State, t.PreviewRevision = services.ThemedTaskRunning, 5 })
	r.timers.fire(0)
	// 直接用较旧的版本号走一遍拉取路径
	r.svc.SetPreview(reqA, previewPage(2))
	r.reading.mu.Lock()
	gen, sel := r.reading.previewGen, r.reading.selectGen
	r.reading.mu.Unlock()
	r.reading.fetchPreview(reqA, id, gen, sel)
	if got := r.view().Preview.Revision; got != 5 {
		t.Fatalf("晚到的旧版本不能盖掉新版本: %d", got)
	}
}

func TestPreviewDroppedWhenSwitchingItem(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.Attach()
	r.reading.Generate(services.ThemedOptions{})
	id := r.view().Task.ID
	r.svc.SetPreview(reqA, previewPage(1))
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.State, t.PreviewRevision = services.ThemedTaskRunning, 1 })
	r.open(reqB)
	r.timers.fire(0)
	if v := r.view(); v.Preview != nil || len(r.svc.Calls().Previews) != 0 {
		t.Fatalf("切走后旧排程不应拉取或显示: %+v", v.Preview)
	}
}

func TestPreviewWithExistingPageNeedsToggle(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{ID: "page-old"})
	r.open(reqA)
	r.reading.Attach()
	r.reading.Generate(services.ThemedOptions{})
	id := r.view().Task.ID
	r.svc.SetPreview(reqA, previewPage(1))
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.State, t.PreviewRevision = services.ThemedTaskRunning, 1 })
	if r.timers.count() != 0 {
		t.Fatal("已有页面且没打开「预览新稿」时不拉取")
	}
	if m := r.view().Pane(false, false); !m.Toolbar.PreviewToggle {
		t.Fatalf("应出现「预览新稿」: %+v", m.Toolbar)
	}
	r.reading.TogglePreview()
	if r.timers.count() != 1 {
		t.Fatal("打开预览后才排程")
	}
	r.timers.fire(0)
	v := r.view()
	if !v.ShowingPreview() || v.ReadingPage().ID != "draft" || v.Page().ID != "page-old" {
		t.Fatalf("预览新稿时渲染草稿，固定的旧版保持不变: %+v", v)
	}
	r.reading.TogglePreview()
	if r.view().ShowingPreview() {
		t.Fatal("返回当前版")
	}
}

func TestPreviewFailureIsLoggedNotShown(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.Attach()
	r.reading.Generate(services.ThemedOptions{})
	id := r.view().Task.ID
	r.svc.Fail(servicestest.ThemedOpPreview, errors.New("preview unavailable"))
	r.svc.UpdateTask(id, func(t *services.ThemedTask) { t.State, t.PreviewRevision = services.ThemedTaskRunning, 1 })
	r.timers.fire(0)
	if len(r.toasts()) != 0 {
		t.Fatal("预览失败不弹提示")
	}
	if got := r.log.entries(); len(got) != 1 || !strings.Contains(got[0], "preview unavailable") {
		t.Fatalf("应留痕: %v", got)
	}
}

func TestExternallyStartedTaskPinsPageOnReload(t *testing.T) {
	r := newRig(t)
	old := r.svc.PutPage(reqA, services.ThemedPage{ID: "page-old"})
	r.open(reqA) // 不订阅进度：只靠重读发现别处（任务列表、另一个窗口）启动的生成
	task, err := r.svc.Generate(context.Background(), reqA, services.ThemedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r.reading.Reload()
	if v := r.view(); !v.Active() || v.Page().ID != old.ID {
		t.Fatalf("重读到活动任务时应固定当前页: %+v", v)
	}
	r.svc.CompleteTask(task.ID)
	r.reading.Reload()
	if v := r.view(); v.Page().ID != old.ID || v.Latest == nil || v.Latest.Page.ID == old.ID {
		t.Fatalf("新版完成后仍固定旧版，等用户切换: page=%s latest=%v", v.Page().ID, v.Latest)
	}
}
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// 旧内容的操作收尾只能释放自己那份内容的忙标记：A 的导出还在途时切到 B 并发起 B 的删除，A 收尾后 B 仍在忙（同一时间只允许一个操作）。
func TestStaleOperationEndDoesNotReleaseNewItemsBusy(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{ID: "page-a"})
	r.svc.PutPage(reqB, services.ThemedPage{ID: "page-b"})
	r.svc.SetExportPath("C:/out.html")
	r.open(reqA)
	releaseA, arrivedA := r.svc.Hold(servicestest.ThemedOpExport)
	doneA := make(chan bool, 1)
	go func() { doneA <- r.reading.ExportHTML(false, false) }()
	<-arrivedA
	r.open(reqB)
	releaseB, arrivedB := r.svc.Hold(servicestest.ThemedOpRemove)
	doneB := make(chan bool, 1)
	go func() { doneB <- r.reading.Remove() }()
	<-arrivedB
	if v := r.view(); v.ItemID != reqB.ItemID || !v.Busy {
		t.Fatalf("前置：B 的删除应在进行: %+v", v)
	}
	releaseA()
	if !<-doneA {
		t.Fatal("A 的导出应成功")
	}
	if v := r.view(); v.ItemID != reqB.ItemID || !v.Busy {
		t.Fatalf("A 的旧操作收尾不能释放 B 正在进行的操作的忙标记: %+v", v)
	}
	if r.reading.Remove() {
		t.Fatal("B 的删除还没结束，不能再发起第二个操作")
	}
	releaseB()
	if !<-doneB {
		t.Fatal("B 的删除应成功")
	}
	if r.view().Busy {
		t.Fatal("B 的操作结束后应释放忙标记")
	}
}
