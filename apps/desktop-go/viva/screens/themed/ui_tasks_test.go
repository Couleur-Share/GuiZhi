package themed

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

func tasksRig(t *testing.T, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	r := newUIRig(t, append([]func(*uiRig){withHost(hostTasks)}, mutate...)...)
	r.settle()
	return r
}

func putTask(r *uiRig, req services.ThemedRequest, title, state, stage string) services.ThemedTask {
	return r.svc.PutTask(services.ThemedTask{ItemID: req.ItemID, SourceKind: req.SourceKind, Title: title, State: state, Stage: stage})
}

func openName(title string) string { return locales.ThemedReadingOpen() + " " + title }

func TestTasksListShowsEveryTask(t *testing.T) {
	r := newUIRig(t, withHost(hostTasks), func(r *uiRig) {
		putTask(r, reqA, "Alpha article", services.ThemedTaskRunning, services.ThemedStageWrite)
		putTask(r, reqB, "Beta article", services.ThemedTaskCompleted, services.ThemedStageDone)
	})
	r.settle()
	for _, want := range []string{locales.ThemedReadingTasks(), "Alpha article", "Beta article"} {
		if !r.textHas(want) {
			t.Fatalf("任务页应显示 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if !r.hasButton(openName("Alpha article")) || !r.hasButton(openName("Beta article")) {
		t.Fatalf("每条任务都应有「查看主题页」；画面上有 %q", r.session.Texts())
	}
	if r.hasText(locales.ThemedReadingEmptyTasks()) {
		t.Fatal("有任务时不该显示空态")
	}
}

func TestTasksEmptyShowsGuidance(t *testing.T) {
	r := tasksRig(t)
	if !r.textHas(locales.ThemedReadingEmptyTasks()) {
		t.Fatalf("没有任务应给出引导；画面上有 %q", r.session.Texts())
	}
}

func TestTasksLoadErrorOffersRetry(t *testing.T) {
	r := newUIRig(t, withHost(hostTasks), func(r *uiRig) {
		putTask(r, reqA, "Alpha article", services.ThemedTaskCompleted, services.ThemedStageDone)
		r.svc.Fail("listTasks", errTest("task store unavailable"))
	})
	r.settle()
	if !r.textHas("task store unavailable") {
		t.Fatalf("读取失败应显示原因；画面上有 %q", r.session.Texts())
	}
	if r.textHas(locales.ThemedReadingEmptyTasks()) {
		t.Fatal("读取失败不能渲染成空态")
	}
	r.svc.Fail("listTasks", nil)
	r.clickButton(locales.CommonRetry())
	r.settle()
	if !r.textHas("Alpha article") {
		t.Fatalf("重试成功后应列出任务；画面上有 %q", r.session.Texts())
	}
}

func TestTasksBackReturnsToInbox(t *testing.T) {
	r := tasksRig(t)
	r.clickButton(locales.ThemedReadingBackToInbox())
	if r.back != 1 {
		t.Fatalf("「返回」应通知宿主一次，实际 %d", r.back)
	}
}

func TestTasksRefreshReloads(t *testing.T) {
	r := tasksRig(t)
	before := r.svc.Calls().ListTasks
	putTask(r, reqA, "Late article", services.ThemedTaskCompleted, services.ThemedStageDone)
	r.clickButton(locales.CommonRefresh())
	r.settle()
	if got := r.svc.Calls().ListTasks; got != before+1 {
		t.Fatalf("刷新应再读一次：%d -> %d", before, got)
	}
	if !r.textHas("Late article") {
		t.Fatalf("刷新后应出现新任务；画面上有 %q", r.session.Texts())
	}
}

func TestTasksStopAndResume(t *testing.T) {
	r := newUIRig(t, withHost(hostTasks), func(r *uiRig) {
		putTask(r, reqA, "Alpha article", services.ThemedTaskRunning, services.ThemedStageWrite)
	})
	r.settle()
	r.clickButton(locales.ThemedReadingStop())
	r.settle()
	if got := r.svc.Calls().Cancels; len(got) != 1 {
		t.Fatalf("应取消一次：%v", got)
	}
	if r.hasButton(locales.ThemedReadingStop()) || !r.hasButton(locales.ThemedReadingReaderResume()) {
		t.Fatalf("取消后应只剩「继续」；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingReaderResume())
	r.settle()
	if got := r.svc.Calls().Resumes; len(got) != 1 {
		t.Fatalf("应继续一次：%v", got)
	}
	if !r.hasButton(locales.ThemedReadingStop()) {
		t.Fatalf("继续后应回到执行中；画面上有 %q", r.session.Texts())
	}
	if r.hasButton(locales.ThemedReadingReaderOffline()) {
		t.Fatal("任务列表里没有「不联网继续」")
	}
}

func TestTasksShowLiveProgress(t *testing.T) {
	var task services.ThemedTask
	r := newUIRig(t, withHost(hostTasks), func(r *uiRig) {
		task = putTask(r, reqA, "Alpha article", services.ThemedTaskRunning, services.ThemedStageWrite)
	})
	r.settle()
	before := task
	if !r.has(gfx.RoleStatus, statusTitle(before, StatusOptions{HasPage: true})) {
		t.Fatalf("应显示写作阶段；画面上有 %q", r.session.Texts())
	}
	r.svc.UpdateTask(task.ID, func(tk *services.ThemedTask) { tk.Stage = services.ThemedStageDesign })
	r.settle()
	after := before
	after.Stage = services.ThemedStageDesign
	if !r.has(gfx.RoleStatus, statusTitle(after, StatusOptions{HasPage: true})) {
		t.Fatalf("推送进度后卡片应更新为设计阶段；画面上有 %q", r.session.Texts())
	}
}

func TestTasksOpenJumpsToItemInAIReading(t *testing.T) {
	r := newUIRig(t, withHost(hostTasks), func(r *uiRig) {
		r.svc.PutPage(reqA, services.ThemedPage{ID: "page-a"})
		putTask(r, reqA, "Alpha article", services.ThemedTaskCompleted, services.ThemedStageDone)
	})
	r.settle()
	r.clickButton(openName("Alpha article"))
	got := r.openedItems()
	if len(got) != 1 || got[0].ItemID != reqA.ItemID || got[0].SourceKind != reqA.SourceKind {
		t.Fatalf("应请外壳切到这份内容：%+v", got)
	}
	// 外壳切到知识库后，详情绑定这份内容，直接进入 AI 阅读（不是原文）。
	r.host = hostLibrary
	r.settle()
	if !r.aiOn() {
		t.Fatal("从任务列表进入应直接显示 AI 阅读")
	}
	// 只生效一次：切回原文后重新绑定不再强行进入 AI 阅读。
	r.screen.Mode.Select(false)
	r.settle()
	if r.aiOn() {
		t.Fatal("用户选了原文就该尊重")
	}
}

func TestTasksJumpForOtherItemDoesNotHijackReading(t *testing.T) {
	// 两份内容都没有页面：默认都是原文，只有「待跳转」能把阅读切成 AI 阅读。
	r := newUIRig(t, withHost(hostTasks), func(r *uiRig) {
		putTask(r, reqB, "Beta article", services.ThemedTaskCompleted, services.ThemedStageDone)
	})
	r.settle()
	r.clickButton(openName("Beta article"))
	// 外壳还没切过去时，详情仍绑在 A 上：不能把 A 切成 AI 阅读。
	r.host = hostLibrary
	r.settle()
	if r.aiOn() {
		t.Fatal("待跳转属于 B，不该改变 A 的阅读模式")
	}
	r.subject.ItemID, r.subject.Title, r.subject.ContentKey = reqB.ItemID, "Beta article", "k2"
	r.settle()
	if !r.aiOn() {
		t.Fatal("绑定到任务所属的条目后应进入 AI 阅读")
	}
}
func TestTasksUnmountDetachesAndRemountReloads(t *testing.T) {
	r := tasksRig(t)
	if r.svc.Subscribers() != 1 {
		t.Fatalf("显示时应订阅进度，实际 %d", r.svc.Subscribers())
	}
	reads := r.svc.Calls().ListTasks
	r.host = hostNone
	r.settle()
	if n := r.svc.Subscribers(); n != 0 {
		t.Fatalf("离开任务页应退订，仍有 %d 个", n)
	}
	r.host = hostTasks
	r.settle()
	if r.svc.Subscribers() != 1 {
		t.Fatalf("回来应重新订阅，实际 %d", r.svc.Subscribers())
	}
	if got := r.svc.Calls().ListTasks; got <= reads {
		t.Fatalf("回来应重新读取：%d -> %d", reads, got)
	}
}
