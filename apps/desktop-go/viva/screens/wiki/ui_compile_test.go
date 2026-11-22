package wiki

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 全文编译对话框（L4）：入口、预览与勾选、筛选与展开、开始、进度与暂停取消继续、没有模型、失败与被拒。

// withReady 摆 n 条可执行资料的预览（每条两块、无可复用）。
func withReady(n int) func(*uiRig) {
	return func(r *uiRig) { r.svc.SetPreview(previewOf(manyReady(n)...)) }
}

// entryName 是资料复选框的无障碍名：标题、分块数。
func entryName(id string, blocks int) string {
	return "item " + id + " · " + locales.WikiCompileEntryBlocks(blocks)
}

func (r *uiRig) openCompile() {
	r.t.Helper()
	r.clickButton(locales.WikiCompileDialog())
	if !r.store.Compile().View().Open {
		r.t.Fatalf("对话框应打开：%q", r.session.Texts())
	}
}

func TestUICompileToolbarLabelFollowsPendingCount(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(3))
	if !r.hasButton(locales.WikiCompilePending(3)) {
		t.Fatalf("有 3 条可执行资料，主按钮应写「编译 3 条新内容」：%q", r.session.Texts())
	}
	r.clickButton(locales.WikiCompilePending(3))
	if !r.store.Compile().View().Open || !r.has(gfx.RoleGroup, locales.WikiCompileDialog()) {
		t.Fatalf("点主按钮应打开编译对话框：%q", r.session.Texts())
	}
}

func TestUICompileToolbarDisabledWhenUpToDate(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.svc.SetPreview(previewOf(candidate("c1", services.WikiCandidateCurrent, 2, 0)))
	r.screen.OpenCompileDialog()
	r.store.Compile().Close()
	r.store.Refresh()
	r.frames(3)
	if !r.hasButton(locales.WikiCompileNow()) {
		t.Fatalf("没有待编译资料主按钮应写「立即编译」：%q", r.session.Texts())
	}
}

func TestUICompileDialogPreviewDefaultsToSelectingEntries(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(3))
	r.openCompile()
	for _, id := range []string{"i000", "i001", "i002"} {
		if !r.has(gfx.RoleCheckbox, entryName(id, 2)) {
			t.Errorf("应列出资料 %s：%q", id, r.session.Texts())
		}
	}
	if got := len(r.store.Compile().View().Selected); got != 3 {
		t.Fatalf("三条可选资料默认全选：%d", got)
	}
	if !r.hasText(locales.WikiCompileCounts(3, 0, 0, 0)) {
		t.Errorf("应显示范围计数：%q", r.session.Texts())
	}
	if !r.hasText(locales.WikiCompileEstimate(3, 6, 6, 12)) {
		t.Errorf("应显示本批估算：%q", r.session.Texts())
	}
	if !strings.Contains(noSpace(r.allText()), noSpace(locales.WikiCompileIntro())) {
		t.Errorf("应有说明：%q", r.session.Texts())
	}
}

func TestUICompileUncheckingChangesSelectionAndEstimate(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(3))
	r.openCompile()
	r.click(gfx.RoleCheckbox, entryName("i001", 2))
	if got := r.store.Compile().View().Selected; len(got) != 2 {
		t.Fatalf("取消勾选后应剩两条：%v", got)
	}
	if !r.hasText(locales.WikiCompileEstimate(2, 4, 4, 8)) {
		t.Errorf("估算应随勾选更新：%q", r.session.Texts())
	}
	r.clickButton(locales.WikiCompileStart())
	starts := r.svc.Starts()
	if len(starts) != 1 || len(starts[0]) != 2 {
		t.Fatalf("只应开始被勾选的两条：%+v", starts)
	}
	for _, e := range starts[0] {
		if e.ID == "i001" {
			t.Fatal("被取消勾选的 i001 不该进入编译")
		}
	}
}

func TestUICompileStartRunsToCompletion(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(2))
	r.openCompile()
	r.clickButton(locales.WikiCompileStart())
	if got := r.svc.Runs(); len(got) != 1 {
		t.Fatalf("应驱动一次编译：%v", got)
	}
	if !strings.Contains(r.allText(), locales.WikiCompileStatusCompleted()) {
		t.Fatalf("任务列表应显示已完成：%q", r.session.Texts())
	}
	if !r.hasText(locales.WikiCompileJobs()) {
		t.Error("应有任务列表标题")
	}
	if r.store.View().IsCompiling {
		t.Error("编译结束后主状态不该还在编译")
	}
}

func TestUICompileFilterNarrowsEntries(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(3))
	r.openCompile()
	r.click(gfx.RoleTextField, locales.WikiCompileFilter())
	r.typeText("i001")
	if !r.has(gfx.RoleCheckbox, entryName("i001", 2)) || r.has(gfx.RoleCheckbox, entryName("i000", 2)) {
		t.Fatalf("筛选后只剩 i001：%q", r.session.Texts())
	}
	if got := r.store.Compile().View().Filter; got != "i001" {
		t.Fatalf("筛选词应写进 Store：%q", got)
	}
}

func TestUICompileShowMoreRevealsNextPage(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(60))
	r.openCompile()
	if !r.hasButton(locales.WikiCompileShowMore()) {
		// 列表在自己的滚动区里，「显示更多」在末尾；先滚到底。
		r.scrollBy(640, 400, 4000)
	}
	if !r.store.Compile().View().HasMore() {
		t.Fatal("60 条应有折叠项")
	}
	r.store.Compile().ShowMore()
	r.frames(3)
	if r.store.Compile().View().HasMore() {
		t.Fatal("展开一页之后 60 条应全部显示")
	}
}

func TestUICompileNoModelGuidesToSettings(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(2), func(r *uiRig) { r.svc.SetCompileConfigured(false) })
	r.openCompile()
	if !strings.Contains(noSpace(r.allText()), noSpace(locales.WikiCompileNoModel())) {
		t.Fatalf("没有模型应有引导：%q", r.session.Texts())
	}
	n, ok := r.node(gfx.RoleButton, locales.WikiCompileStart())
	if !ok || r.store.Compile().View().CanStart() {
		t.Fatalf("没有模型开始钮应禁用：%v %v", n, ok)
	}
	r.clickButton(locales.ArticleAskConfigure())
	if r.settingsOpened() != 1 {
		t.Fatal("「前往配置」应去设置页的 AI 分区")
	}
	if r.store.Compile().View().Open {
		t.Fatal("去配置时应收起对话框")
	}
}

func TestUICompilePreviewFailureShowsErrorAndRetry(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(2), func(r *uiRig) { r.svc.Fail(servicestest.WikiOpPreview, errors.New("preview boom")) })
	r.openCompile()
	if !r.hasText("preview boom") {
		t.Fatalf("预览失败应显示原因：%q", r.session.Texts())
	}
	r.svc.Fail(servicestest.WikiOpPreview, nil)
	r.clickButton(locales.CommonRetry())
	if !r.has(gfx.RoleCheckbox, entryName("i000", 2)) {
		t.Fatalf("重试成功后应列出资料：%q", r.session.Texts())
	}
}

func TestUICompileRejectedStartReplacesPreviewAndSelection(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(2))
	fresh := previewOf(candidate("n1", services.WikiCandidateReady, 2, 0))
	r.svc.RejectStart(&services.WikiStartError{Message: "scope changed", Preview: &fresh})
	r.openCompile()
	r.clickButton(locales.WikiCompileStart())
	if !r.hasText("scope changed") {
		t.Fatalf("被拒应显示原因：%q", r.session.Texts())
	}
	if !r.has(gfx.RoleCheckbox, entryName("n1", 2)) || r.has(gfx.RoleCheckbox, entryName("i000", 2)) {
		t.Fatalf("预览应换成后端带回的新范围：%q", r.session.Texts())
	}
	if got := r.store.Compile().View().Selected; len(got) != 1 || got[0] != "n1" {
		t.Fatalf("勾选应改成新预览里的可选项：%v", got)
	}
}

func TestUICompileCloseButtonClosesDialog(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(1))
	r.openCompile()
	r.clickButton(locales.CommonClose())
	if r.store.Compile().View().Open || r.has(gfx.RoleGroup, locales.WikiCompileDialog()) {
		t.Fatalf("应关闭对话框：%q", r.session.Texts())
	}
}

// ---- 进行中：暂停、取消、继续（后台真正跑，用 Gate 卡住编译） ----

// runningRig 开始一轮被 Gate 卡住的编译，返回放行用的 channel。
func runningRig(t *testing.T, progress ...services.WikiCompileJob) (*uiRig, chan struct{}) {
	t.Helper()
	gate := make(chan struct{})
	arrived := make(chan struct{})
	r := newUIRig(t, withSeed, withReady(2), goroutines, func(r *uiRig) {
		r.svc.QueueRun(servicestest.WikiRunScript{Progress: progress, Gate: gate, Arrived: arrived})
	})
	r.openCompile()
	r.settle("预览出现", func() bool { return r.hasButton(locales.WikiCompileStart()) })
	r.clickButton(locales.WikiCompileStart())
	r.settle("编译跑起来", func() bool { return r.hasButton(locales.WikiCompilePause()) })
	return r, gate
}

func TestUICompileRunningShowsProgressAndPauses(t *testing.T) {
	r, gate := runningRig(t, services.WikiCompileJob{Status: services.WikiJobRunning, Total: 4, Completed: 1})
	t.Cleanup(func() { close(gate) })
	if !r.hasText(locales.WikiCompileStatusRunning()+" · "+locales.WikiCompileJobProgress(1, 4)) &&
		!strings.Contains(r.allText(), locales.WikiCompileJobProgress(1, 4)) {
		t.Fatalf("应显示进度：%q", r.session.Texts())
	}
	if !r.store.View().IsCompiling {
		t.Error("编译中主状态应为编译中")
	}
	r.clickButton(locales.WikiCompilePause())
	r.settle("暂停落到服务", func() bool {
		for _, c := range r.svc.Controls() {
			if c.Status == services.WikiJobPaused {
				return true
			}
		}
		return false
	})
}

func TestUICompileCancelStopsRunningJob(t *testing.T) {
	r, gate := runningRig(t)
	t.Cleanup(func() { close(gate) })
	r.clickButton(locales.WikiCompileCancel())
	r.settle("取消落到服务并结束", func() bool {
		for _, c := range r.svc.Controls() {
			if c.Status == services.WikiJobCancelled {
				return !r.store.Compile().View().Busy
			}
		}
		return false
	})
	if !strings.Contains(r.allText(), locales.WikiCompileStatusCancelled()) {
		t.Fatalf("任务应显示已取消：%q", r.session.Texts())
	}
}

func TestUICompileResumePausedJob(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(2), goroutines, func(r *uiRig) {
		r.svc.PutJob(services.WikiCompileJob{ID: "job-old", Status: services.WikiJobPaused, Total: 4, Completed: 1})
	})
	r.openCompile()
	r.settle("任务列表出现", func() bool { return r.hasButton(locales.WikiCompileResume()) })
	if !r.hasText(locales.WikiCompileStatusPaused() + " · " + locales.WikiCompileJobProgress(1, 4)) {
		t.Errorf("暂停的任务应显示状态与进度：%q", r.session.Texts())
	}
	r.clickButton(locales.WikiCompileResume())
	r.settle("继续并跑完", func() bool {
		return len(r.svc.Runs()) == 1 && !r.store.Compile().View().Busy
	})
	if got := r.svc.Controls(); len(got) == 0 || got[0].JobID != "job-old" || got[0].Status != services.WikiJobRunning {
		t.Fatalf("应把 job-old 改回运行：%+v", got)
	}
}

func TestUICompileFailedJobShowsReason(t *testing.T) {
	r := newUIRig(t, withSeed, withReady(1), func(r *uiRig) {
		r.svc.PutJob(services.WikiCompileJob{ID: "job-bad", Status: services.WikiJobFailed, Total: 2, Completed: 1, Failed: 1, Error: "model refused"})
	})
	r.openCompile()
	if !r.hasText("model refused") {
		t.Fatalf("失败任务应显示原因：%q", r.session.Texts())
	}
	if !strings.Contains(r.allText(), locales.WikiCompileJobFailed(1)) {
		t.Errorf("应显示失败数：%q", r.session.Texts())
	}
	if !r.hasButton(locales.WikiCompileResume()) {
		t.Error("失败的任务可以继续未完成分块")
	}
}

// 自动编译指示：开关开着才出现，点它去设置页的通用分区。
func TestUIAutoCompileIndicatorGoesToSettings(t *testing.T) {
	r := newUIRig(t, withSeed)
	if r.hasButton(locales.WikiAutoCompile()) {
		t.Fatal("自动编译关着不该有指示")
	}
	r2 := newUIRig(t, withSeed, withAutoCompile)
	r2.clickButton(locales.WikiAutoCompile())
	if r2.generalOpened() != 1 {
		t.Fatal("点指示应去设置页的通用分区")
	}
}
