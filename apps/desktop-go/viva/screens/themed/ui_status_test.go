package themed

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// taskIn 摆一个属于被测条目的任务。
func (r *uiRig) taskIn(t services.ThemedTask) services.ThemedTask {
	t.ItemID, t.SourceKind = reqA.ItemID, reqA.SourceKind
	if t.Title == "" {
		t.Title = "Article A"
	}
	return r.svc.PutTask(t)
}

// statusTitle 是状态卡此刻该显示的标题（取自状态层，不在测试里硬写文案）。
func statusTitle(task services.ThemedTask, o StatusOptions) string { return BuildStatus(task, o).Title }

func TestStatusRunningTaskShowsCardWithProgressAndStop(t *testing.T) {
	r := newUIRig(t)
	task := r.taskIn(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageWrite, Completed: 2, Total: 5})
	r.activate()
	want := statusTitle(task, StatusOptions{Expanded: true, OfflineAvailable: true})
	if !r.has(gfx.RoleStatus, want) {
		t.Fatalf("应有一张可访问名为 %q 的状态卡；画面上有 %q", want, r.session.Texts())
	}
	if !r.hasButton(locales.ThemedReadingStop()) {
		t.Fatal("执行中的任务应有「停止」")
	}
	if r.hasButton(locales.ThemedReadingReaderResume()) {
		t.Fatal("执行中的任务不该有「继续」")
	}
	if r.hasButton(locales.ThemedReadingReaderCreate()) {
		t.Fatal("有任务时不该再引导生成")
	}
}

func TestStatusStopCancelsTaskAndOffersResume(t *testing.T) {
	r := newUIRig(t)
	task := r.taskIn(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageWrite})
	r.activate()
	r.clickButton(locales.ThemedReadingStop())
	r.settle()
	if got := r.svc.Calls().Cancels; len(got) != 1 || got[0] != task.ID {
		t.Fatalf("应取消 %s，实际 %v", task.ID, got)
	}
	if r.hasButton(locales.ThemedReadingStop()) {
		t.Fatal("取消后不该还有「停止」")
	}
	if !r.hasButton(locales.ThemedReadingReaderResume()) {
		t.Fatalf("取消后应能「继续」；画面上有 %q", r.session.Texts())
	}
}

func TestStatusFailedTaskShowsReasonAndCopiesIt(t *testing.T) {
	r := newUIRig(t)
	r.taskIn(services.ThemedTask{State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign, Error: "model returned 429 rate limit"})
	r.activate()
	if !r.textHas("model returned 429 rate limit") {
		t.Fatalf("失败原因应原样显示；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingReaderCopyError())
	if got := r.copied(); len(got) != 1 || got[0] != "model returned 429 rate limit" {
		t.Fatalf("应把失败原因写进剪贴板，实际 %q", got)
	}
	if got := r.lastToast(); got.Message != locales.ThemedReadingReaderCopied() {
		t.Fatalf("复制成功应有提示：%+v", got)
	}
}

func TestStatusFailedTaskResumeContinuesIt(t *testing.T) {
	r := newUIRig(t)
	task := r.taskIn(services.ThemedTask{State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign, Error: "boom"})
	r.activate()
	r.clickButton(locales.ThemedReadingReaderResume())
	if got := r.svc.Calls().Resumes; len(got) != 1 || got[0] != task.ID {
		t.Fatalf("应继续 %s，实际 %v", task.ID, got)
	}
	r.settle()
	if !r.hasButton(locales.ThemedReadingStop()) {
		t.Fatalf("继续后任务回到执行中；画面上有 %q", r.session.Texts())
	}
}

func TestStatusResumeFailureToastsAndKeepsCard(t *testing.T) {
	r := newUIRig(t)
	r.taskIn(services.ThemedTask{State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign, Error: "boom"})
	r.svc.Fail("resume", errTest("cannot resume now"))
	r.activate()
	r.clickButton(locales.ThemedReadingReaderResume())
	if got := r.lastToast(); got.Detail == "" {
		t.Fatalf("继续失败应带可复制的原因：%+v", got)
	}
	if !r.hasButton(locales.ThemedReadingReaderResume()) {
		t.Fatal("失败后应仍能重试")
	}
}

func TestStatusPausedResearchOffersOfflineContinue(t *testing.T) {
	r := newUIRig(t)
	task := r.taskIn(services.ThemedTask{State: services.ThemedTaskPartial, Stage: services.ThemedStageResearch, Error: "search quota exhausted"})
	r.activate()
	if !r.hasButton(locales.ThemedReadingReaderRetryResearch()) {
		t.Fatalf("联网查证暂停应有「重试查证」；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingReaderOffline())
	if got := r.svc.Calls().Offlines; len(got) != 1 || got[0] != task.ID {
		t.Fatalf("应不联网继续 %s，实际 %v", task.ID, got)
	}
}

func TestStatusReadOnlyHidesControls(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) { r.subject.ReadOnly = true })
	r.taskIn(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageWrite})
	r.activate()
	for _, name := range []string{locales.ThemedReadingStop(), locales.ThemedReadingReaderResume(), locales.ThemedReadingReaderOffline()} {
		if r.hasButton(name) {
			t.Fatalf("只读（回收站里）不该有「%s」", name)
		}
	}
}

func TestStatusDetailsDisclosureTogglesLines(t *testing.T) {
	r := newUIRig(t)
	r.taskIn(services.ThemedTask{State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign, Error: "boom"})
	r.activate()
	if r.textHas(locales.ThemedReadingUsageMissing()) {
		t.Fatal("「生成详情」默认折叠")
	}
	r.clickButton(locales.ThemedReadingReaderTaskDetails())
	if !r.textHas(locales.ThemedReadingUsageMissing()) {
		t.Fatalf("展开后应显示详情行；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingReaderTaskDetails())
	if r.textHas(locales.ThemedReadingUsageMissing()) {
		t.Fatal("再点一次应折叠")
	}
}

func TestStatusStripShownAboveExistingPage(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.putPage()
	task := r.taskIn(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign})
	r.activate()
	want := statusTitle(task, StatusOptions{HasPage: true, OfflineAvailable: true})
	if !r.has(gfx.RoleStatus, want) {
		t.Fatalf("已有页面时任务进度应是页面上方的紧凑条 %q；画面上有 %q", want, r.session.Texts())
	}
	if v, ok := r.lastIsland(); !ok || v.Kind != reader.ViewSnapshot {
		t.Fatalf("紧凑条之下仍应显示当前页面：%+v ok=%v", v, ok)
	}
}

func TestStatusStartOverAfterEndedTask(t *testing.T) {
	r := newUIRig(t)
	r.taskIn(services.ThemedTask{State: services.ThemedTaskCancelled, Stage: services.ThemedStageDesign})
	r.activate()
	r.clickButton(locales.ThemedReadingReaderStartOver())
	if !r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatalf("「重新生成」应打开生成设置；画面上有 %q", r.session.Texts())
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
