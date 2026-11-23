package themed

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// wantSnapshot 断言阅读岛最近一帧收到的是页面快照，并返回它。
func (r *uiRig) wantSnapshot() reader.View {
	r.t.Helper()
	v, ok := r.lastIsland()
	if !ok || v.Kind != reader.ViewSnapshot {
		r.t.Fatalf("阅读岛应收到页面快照，实际 %+v ok=%v；画面上有 %q", v, ok, r.session.Texts())
	}
	return v
}

// wantNoSnapshot 断言阅读岛这一帧是收起的（ViewNone）。
func (r *uiRig) wantNoSnapshot() {
	r.t.Helper()
	if v, ok := r.lastIsland(); !ok || v.Kind != reader.ViewNone {
		r.t.Fatalf("阅读岛应收起，实际 %+v ok=%v", v, ok)
	}
}

func TestPaneCurrentPageRendersInIslandAsSnapshot(t *testing.T) {
	r := newUIRig(t, withIsland)
	page := r.putPage()
	r.activate()
	v := r.wantSnapshot()
	if want := "<html>page " + page.ID + " instance inst</html>"; v.SnapshotDoc != want {
		t.Fatalf("快照文档 = %q，想要 %q", v.SnapshotDoc, want)
	}
	if v.SnapshotInstanceID != "inst" || v.SnapshotVersionID != page.ID {
		t.Fatalf("快照应带实例与版本标识：%+v", v)
	}
	if v.SnapshotWidth != reader.SnapshotWidthFluid {
		t.Fatalf("主题页应按流式宽度显示：%v", v.SnapshotWidth)
	}
	if v.SnapshotAssets == nil || len(v.SnapshotAssets) != 0 {
		t.Fatalf("没有可放行的素材，应是空切片（不是 nil）：%#v", v.SnapshotAssets)
	}
	if r.hasButton(locales.ThemedReadingReaderCreate()) || r.hasText(locales.ThemedReadingLoading()) {
		t.Fatalf("有页面时不该出现空态或加载中；画面上有 %q", r.session.Texts())
	}
	if n := len(r.svc.Calls().Renders); n != 1 {
		t.Fatalf("同一版本应只渲染一次，实际 %d 次", n)
	}
}

func TestPaneSnapshotDocumentIsStableAcrossFrames(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.activate()
	r.frames(10)
	if n := len(r.svc.Calls().Renders); n != 1 {
		t.Fatalf("多帧不该重复渲染，实际 %d 次", n)
	}
}

func TestPaneLegacyPageUsesStoredDocument(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage(func(p *services.ThemedPage) { p.FormatVersion = 2 })
	r.svc.SetDocument(reqA, "<html>legacy</html>")
	r.activate()
	v := r.wantSnapshot()
	if v.SnapshotDoc != "<html>legacy</html>" {
		t.Fatalf("旧格式页面应直接显示读到的文档：%q", v.SnapshotDoc)
	}
	if n := len(r.svc.Calls().Renders); n != 0 {
		t.Fatalf("旧格式页面不走外壳渲染，实际 %d 次", n)
	}
	if !r.textHas(locales.ThemedReadingReaderLegacyReadOnly()) {
		t.Fatalf("旧格式页面应说明只读；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingReaderSettings())
	if r.has(gfx.RoleMenuItem, locales.ThemedReadingReaderAdjust()) {
		t.Fatal("旧格式页面不能调整")
	}
}

func TestPaneWithoutIslandHostDrawsPageShell(t *testing.T) {
	r := newUIRig(t)
	r.putPage()
	r.activate()
	if !r.has(gfx.RoleGroup, r.screen.subjectLine()) {
		t.Fatalf("没有岛宿主时应画页面占位容器；画面上有 %q", r.session.Texts())
	}
}

func TestPaneOriginalModeCollapsesIsland(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.activate()
	r.wantSnapshot()
	r.screen.Mode.Select(false)
	r.settle()
	r.wantNoSnapshot()
	if r.hasButton(locales.ThemedReadingReaderSettings()) {
		t.Fatal("原文模式不画 AI 阅读的工具栏")
	}
}

func TestPaneNoPageNoIslandContent(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.activate()
	r.wantNoSnapshot()
}

func TestPaneSwitchingItemLoadsThatItemsPage(t *testing.T) {
	r := newUIRig(t, withIsland)
	a := r.putPage()
	b := r.svc.PutPage(reqB, services.ThemedPage{ID: "page-b"})
	r.activate()
	if got := r.wantSnapshot().SnapshotVersionID; got != a.ID {
		t.Fatalf("应先显示 A 的页面：%s", got)
	}
	r.subject.ItemID, r.subject.Title, r.subject.ContentKey = reqB.ItemID, "Article B", "k2"
	r.settle()
	v := r.wantSnapshot()
	if v.SnapshotVersionID != b.ID || !strings.Contains(v.SnapshotDoc, b.ID) {
		t.Fatalf("切换条目后应显示 B 的页面，实际 %+v", v)
	}
}

func TestPaneUnbindDetachesAndCollapses(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.activate()
	if r.svc.Subscribers() == 0 {
		t.Fatal("显示时应订阅任务进度")
	}
	r.bind = false
	r.frames(1)
	r.locked(func() { r.islands = nil })
	r.settle()
	if n := r.svc.Subscribers(); n != 0 {
		t.Fatalf("详情关闭后应取消订阅，仍有 %d 个", n)
	}
	if r.aiOn() {
		t.Fatal("没有绑定时不该是活动状态")
	}
	if _, ok := r.lastIsland(); ok {
		t.Fatal("没有绑定时 Area 不该再挂阅读岛（交还给知识库的原文视图）")
	}
}

func TestPaneEditingHidesAIReading(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.activate()
	r.wantSnapshot()
	r.subject.Editing = true
	r.settle()
	if r.aiOn() {
		t.Fatal("编辑原文时不该显示 AI 阅读")
	}
	r.wantNoSnapshot()
	if r.hasButton(locales.ThemedReadingReaderSettings()) {
		t.Fatal("编辑时不该画 AI 阅读的工具栏")
	}
}

func TestPaneTrashedItemIsReadOnly(t *testing.T) {
	r := newUIRig(t, withIsland, func(r *uiRig) { r.subject.ReadOnly = true })
	r.putPage()
	r.activate()
	r.wantSnapshot()
	if !r.textHas(locales.ThemedReadingReaderReadOnly()) {
		t.Fatalf("回收站里的条目应提示只读；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingReaderSettings())
	for _, label := range []string{locales.ThemedReadingReaderRegenerate(), locales.ThemedReadingReaderDelete()} {
		n, ok := r.node(gfx.RoleMenuItem, label)
		if !ok || !n.Disabled {
			t.Fatalf("只读时「%s」应禁用：%+v ok=%v", label, n, ok)
		}
	}
	if n, ok := r.node(gfx.RoleMenuItem, locales.ThemedReadingExport()); !ok || n.Disabled {
		t.Fatalf("只读时仍可导出：%+v ok=%v", n, ok)
	}
}

func TestPaneStaleNoteWhenSourceChanged(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.svc.SetStale(reqA, true)
	r.activate()
	if !r.textHas(locales.ThemedReadingReaderStale()) {
		t.Fatalf("原文改过应提示页面已过期；画面上有 %q", r.session.Texts())
	}
	r.wantSnapshot()
}

func TestPaneImagesOmittedNote(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.svc.SetImagesOmitted(true)
	r.activate()
	if !r.textHas(locales.NativeThemedImagesOmitted()) {
		t.Fatalf("文档不带图片时应说明；画面上有 %q", r.session.Texts())
	}
	r.wantSnapshot()
}

func TestPaneLoadErrorOffersRetry(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.svc.Fail("get", errTest("database is locked"))
	r.activate()
	if !r.textHas("database is locked") {
		t.Fatalf("读取失败应显示原因；画面上有 %q", r.session.Texts())
	}
	if r.hasButton(locales.ThemedReadingReaderCreate()) {
		t.Fatal("读取失败不能渲染成空态")
	}
	r.wantNoSnapshot()
	r.svc.Fail("get", nil)
	r.clickButton(locales.CommonRetry())
	r.settle()
	r.wantSnapshot()
}

func TestPaneRenderErrorOffersRetry(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	r.svc.Fail("render", errTest("renderer crashed"))
	r.activate()
	if !r.textHas("renderer crashed") {
		t.Fatalf("渲染失败应显示原因；画面上有 %q", r.session.Texts())
	}
	r.wantNoSnapshot()
	r.svc.Fail("render", nil)
	r.clickButton(locales.CommonRetry())
	r.settle()
	r.wantSnapshot()
}

func TestPanePreviewShownWhileNoPage(t *testing.T) {
	r := newUIRig(t, withIsland)
	task := r.taskIn(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign, PreviewRevision: 1})
	r.svc.SetPreview(reqA, &services.ThemedPreview{Page: services.ThemedPage{ID: "draft-1", FormatVersion: 3}, Revision: 1})
	r.activate()
	if r.timers.count() == 0 {
		t.Fatal("有可预览的章节应排一次预览拉取")
	}
	r.timers.fire(0)
	r.settle()
	v := r.wantSnapshot()
	if !strings.Contains(v.SnapshotDoc, "preview draft-1 rev 1") {
		t.Fatalf("应显示预览文档：%q", v.SnapshotDoc)
	}
	calls := r.svc.Calls().Renders
	if len(calls) == 0 || calls[len(calls)-1].TaskID != task.ID {
		t.Fatalf("预览应按任务渲染：%+v", calls)
	}
	if !r.has(gfx.RoleStatus, statusTitle(task, StatusOptions{HasPage: true, OfflineAvailable: true})) {
		t.Fatalf("预览上方应有进度条；画面上有 %q", r.session.Texts())
	}
}

func TestPanePreviewToggleOnExistingPage(t *testing.T) {
	r := newUIRig(t, withIsland)
	page := r.putPage()
	r.taskIn(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign, PreviewRevision: 1, VersionID: "draft-1"})
	r.svc.SetPreview(reqA, &services.ThemedPreview{Page: services.ThemedPage{ID: "draft-1", FormatVersion: 3}, Revision: 1})
	r.activate()
	if got := r.wantSnapshot().SnapshotVersionID; got != page.ID {
		t.Fatalf("默认仍显示当前版：%s", got)
	}
	r.clickButton(locales.ThemedReadingReaderPreviewDraft())
	r.timers.fire(r.timers.count() - 1)
	r.settle()
	if got := r.wantSnapshot().SnapshotDoc; !strings.Contains(got, "preview draft-1") {
		t.Fatalf("点「预览新稿」后应显示预览：%q", got)
	}
	r.clickButton(locales.ThemedReadingReaderBackToCurrent())
	r.settle()
	if got := r.wantSnapshot().SnapshotVersionID; got != page.ID {
		t.Fatalf("「返回当前版」应回到当前版：%s", got)
	}
}

func TestPaneGenerateThenSwitchLatest(t *testing.T) {
	r := newUIRig(t, withIsland)
	old := r.putPage(func(p *services.ThemedPage) { p.ID = "page-old" })
	r.activate()
	r.openTool(locales.ThemedReadingReaderRegenerate())
	r.clickButton(locales.ThemedReadingGenerate())
	task := r.svc.Calls().Generates
	if len(task) != 1 {
		t.Fatalf("应发出生成，实际 %d", len(task))
	}
	r.settle()
	var id string
	for _, tk := range mustTasks(t, r) {
		id = tk.ID
	}
	r.svc.CompleteTask(id)
	r.settle()
	if got := r.wantSnapshot().SnapshotVersionID; got != old.ID {
		t.Fatalf("新版完成后仍固定显示旧版：%s", got)
	}
	r.clickButton(locales.ThemedReadingReaderSwitchLatest())
	r.settle()
	if got := r.wantSnapshot().SnapshotVersionID; got == old.ID || got == "" {
		t.Fatalf("切换后应显示新版：%q", got)
	}
	if r.hasButton(locales.ThemedReadingReaderSwitchLatest()) {
		t.Fatal("切换后按钮应消失")
	}
}

func mustTasks(t *testing.T, r *uiRig) []services.ThemedTask {
	t.Helper()
	tasks, err := r.svc.ListTasks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}
