package ask

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// withActive 让首帧恢复到指定的会话（上次活跃会话记在偏好里）。
func withActive(id string) func(*uiRig) {
	return func(r *uiRig) { _ = r.prefs.Set(PrefActiveSession, id) }
}

// failedTurn 造一轮失败的问答。
func failedTurn(id, kind, message string, stopped bool) services.AskMessage {
	return services.AskMessage{
		ID: id, Question: "question " + id, Status: services.AskStatusError, ErrorKind: kind, Error: message, Stopped: stopped,
		Sources: []services.AskSource{}, Steps: []string{},
	}
}

// ask 在输入框里填好问题并点「发送」。
func (r *uiRig) ask(question string) {
	r.t.Helper()
	r.screen.setDraft(question)
	r.frames(2)
	r.clickButton(locales.AskSend())
}

func (r *uiRig) contains(text string) bool { return strings.Contains(r.allText(), text) }

func TestUIEmptyStateOffersExamplesThatOnlyFillTheInput(t *testing.T) {
	r := newUIRig(t)
	for _, ex := range examples() {
		if !r.hasButton(ex) {
			t.Fatalf("空态应有示例问题 %q；画面上有 %q", ex, r.session.Texts())
		}
	}
	if r.hasButton(locales.AskGoConfigure()) {
		t.Fatal("文本模型已配置时不该出现「去配置模型」")
	}
	r.clickButton(examples()[1])
	if got := r.screen.draft.Text(); got != examples()[1] {
		t.Fatalf("点示例应把问题填进输入框，得到 %q", got)
	}
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatalf("示例只预填、不该直接发送，实际发出 %d 次", n)
	}
	if !r.screen.draftFocus {
		t.Fatal("填入示例后输入框应取得焦点")
	}
}

func TestUIEmptyStateGuidesToConfigureWhenNoModel(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) { r.svc.SetQAConfigured(false) })
	if !r.hasButton(locales.AskGoConfigure()) || !r.contains(locales.AskNeedModel()) {
		t.Fatalf("没配模型应给出引导：%q", r.session.Texts())
	}
	for _, ex := range examples() {
		if r.hasButton(ex) {
			t.Fatalf("没配模型时示例问题点了也只会失败，不该画：%q", ex)
		}
	}
	r.clickButton(locales.AskGoConfigure())
	if r.settingsOpened() != 1 {
		t.Fatalf("「去配置模型」应打开设置的 AI 分区，调用 %d 次", r.settingsOpened())
	}
}

func TestUIEmptyStateHintsAtKeywordOnlySearchWithoutEmbedding(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		r.inbox.SetSemanticStatus(services.InboxSemanticStatus{Configured: false})
	})
	hint := locales.AskSemanticOffHint()
	if !r.hasButton(hint) {
		t.Fatalf("没配 embedding 应提示只做关键词检索：%q", r.session.Texts())
	}
	if r.hasButton(locales.AskSemanticRun(1)) || r.contains(locales.AskSemanticStatus(0, 0)) {
		t.Fatal("没配 embedding 时侧栏不该出现语义索引卡")
	}
	r.clickButton(hint)
	if r.settingsOpened() != 1 {
		t.Fatal("提示按钮应打开设置的 AI 分区")
	}

	configured := newUIRig(t)
	if configured.hasButton(hint) {
		t.Fatal("配好 embedding 之后不该再有提示")
	}
}

func TestUIEmptyStateWaitsForSetupBeforeDrawingHints(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		r.svc.Fail(servicestest.AskOpQAConfigured, errBoom)
	})
	// 查询失败时：既不能画引导（会误导已配好的用户），也不能画示例（点了未必能发）。
	if r.hasButton(locales.AskGoConfigure()) || r.hasButton(examples()[0]) {
		t.Fatalf("状态没查到之前不画引导与示例：%q", r.session.Texts())
	}
	if !r.contains(locales.AskEmpty()) {
		t.Fatal("标题与说明仍应在")
	}
}

func TestUISendStreamsStepsAnswerAndOfferingStop(t *testing.T) {
	gate := make(chan struct{})
	r := newUIRig(t, goroutines, func(r *uiRig) {
		r.svc.QueueAsk(servicestest.AskScript{
			Steps: []string{"step-a", "step-b"}, Texts: []string{"part one"}, Gate: gate,
			Answer: services.AskAnswer{Text: "final answer"},
		})
	})
	r.settle("首次加载结束", func() bool { return r.store.View().HasLoaded })

	r.ask("what is new")
	r.settle("进入回答中", func() bool { return r.store.View().IsRunning && r.contains("step-b") })
	if got := r.screen.draft.Text(); got != "" {
		t.Fatalf("发送后输入框应清空，得到 %q", got)
	}
	if !r.hasButton(locales.AskStop()) || r.hasButton(locales.AskSend()) {
		t.Fatal("回答中应是「停止」钮，不是「发送」钮")
	}
	if !r.contains("what is new") || !r.contains("part one") {
		t.Fatalf("应显示问题与流式文本：%q", r.session.Texts())
	}

	close(gate)
	r.settle("回答结束", func() bool { return !r.store.View().IsRunning })
	if !r.contains("final answer") {
		t.Fatalf("应显示最终回答：%q", r.session.Texts())
	}
	if !r.hasButton(locales.AskStepsLabel(2)) {
		t.Fatalf("回答结束后检索过程折叠成「%s」", locales.AskStepsLabel(2))
	}
	if !r.hasButton(locales.AskSend()) {
		t.Fatal("结束后应回到「发送」钮")
	}
	r.clickButton(locales.AskStepsLabel(2))
	if !r.contains("step-a") {
		t.Fatal("展开后应列出每一步")
	}
}

func TestUIStopMarksTheTurnStopped(t *testing.T) {
	gate := make(chan struct{})
	r := newUIRig(t, goroutines, func(r *uiRig) {
		r.svc.QueueAsk(servicestest.AskScript{Steps: []string{"working"}, Gate: gate})
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})
	r.settle("首次加载结束", func() bool { return r.store.View().HasLoaded })
	r.ask("slow question")
	r.settle("进入回答中", func() bool { return r.store.View().IsRunning })

	r.clickButton(locales.AskStop())
	r.settle("已停止", func() bool { return !r.store.View().IsRunning })
	last := lastMessage(t, r.store.View())
	if !last.Stopped || last.Status != services.AskStatusError {
		t.Fatalf("点停止应把这一轮标成已停止：%+v", last)
	}
	if !r.contains(locales.AskStopped()) {
		t.Fatalf("应显示「已停止」：%q", r.session.Texts())
	}
	if r.hasButton(locales.AskStop()) {
		t.Fatal("停止后不该还有「停止」钮")
	}
}

func TestUIErrorClassesAreToldApart(t *testing.T) {
	r := newUIRig(t, withActive("s1"), func(r *uiRig) {
		putSession(t, r.svc, "s1",
			failedTurn("e1", services.AskErrorNotConfigured, "x", false),
			failedTurn("e2", services.AskErrorNoSource, "x", false),
			failedTurn("e3", services.AskErrorGeneric, "gateway exploded", false),
			failedTurn("e4", services.AskErrorGeneric, locales.AskStopped(), true),
		)
	})
	for name, want := range map[string]string{
		"未配置":  locales.AskNotConfigured(),
		"没有资料": locales.AskNoSource(),
		"普通失败": "gateway exploded",
		"已停止":  locales.AskStopped(),
	} {
		if !r.contains(want) {
			t.Fatalf("%s：画面上应有 %q；实际 %q", name, want, r.session.Texts())
		}
	}
	// 未配置带「前往设置」，且只有这一类带。
	if n := r.count(gfx.RoleButton, locales.AskGoToSettings()); n != 1 {
		t.Fatalf("只有未配置那一轮带「前往设置」，实际 %d 个", n)
	}
	r.clickButton(locales.AskGoToSettings())
	if r.settingsOpened() != 1 {
		t.Fatal("「前往设置」应打开设置的 AI 分区")
	}
}

func TestUIRetryReplacesTheFailedTurn(t *testing.T) {
	r := newUIRig(t, withActive("s1"), func(r *uiRig) {
		putSession(t, r.svc, "s1", failedTurn("e1", services.AskErrorGeneric, "boom detail", false))
		r.svc.QueueAsk(servicestest.AskScript{Answer: services.AskAnswer{Text: "second try"}})
	})
	r.clickButton(locales.AskRetryTurn())
	v := r.store.View()
	if len(v.Messages) != 1 || v.Messages[0].Answer != "second try" || v.Messages[0].Question != "question e1" {
		t.Fatalf("重答应换掉失败的那一轮并沿用原问题：%+v", v.Messages)
	}
	if r.contains("boom detail") {
		t.Fatal("旧的失败原因不该还在")
	}
}

func TestUISaveFailureShowsAlertAndRetryPersists(t *testing.T) {
	gate := make(chan struct{})
	r := newUIRig(t, goroutines, func(r *uiRig) {
		r.svc.QueueAsk(servicestest.AskScript{Gate: gate, Answer: services.AskAnswer{Text: "echo:will not save"}})
	})
	r.settle("首次加载结束", func() bool { return r.store.View().HasLoaded })
	r.ask("will not save")
	// 提问前的那次保存要成功（否则这一轮根本不会发出）；回答完成后的保存才失败。
	r.settle("进入回答中", func() bool { return r.store.View().IsRunning })
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	close(gate)
	r.settle("回答结束", func() bool { return !r.store.View().IsRunning && r.store.View().SaveError != "" })
	r.svc.Fail(servicestest.AskOpSave, nil)
	r.store.Spawn = func(fn func()) { fn() }
	v := r.store.View()
	if v.SaveError == "" {
		t.Fatalf("保存失败应记在 SaveError：%+v", v)
	}
	if !r.contains(locales.AskAnswerNotSaved(v.SaveError)) || !r.hasButton(locales.ArticleAskRetrySave()) {
		t.Fatalf("应有「尚未保存」提示与重试钮：%q", r.session.Texts())
	}
	if !r.contains("echo:will not save") {
		t.Fatal("保存失败不能让回答消失：对话还在内存里")
	}
	before := len(r.svc.Saves())
	r.clickButton(locales.ArticleAskRetrySave())
	if got := len(r.svc.Saves()); got != before+1 {
		t.Fatalf("重试应再保存一次：%d → %d", before, got)
	}
	if r.store.View().SaveError != "" || r.hasButton(locales.ArticleAskRetrySave()) {
		t.Fatal("重试成功后提示应消失")
	}
}

func TestUISourceChipExpandsEvidenceAndOpensCurrentVersion(t *testing.T) {
	msg := done("m1", "q", "see [1]")
	msg.Sources = []services.AskSource{source(1, "i1", "fp1")}
	r := newUIRig(t, withActive("s1"), func(r *uiRig) { putSession(t, r.svc, "s1", msg) })

	chip := "[1] title-i1"
	if !r.hasButton(chip) {
		t.Fatalf("应有来源 chip：%q", r.session.Texts())
	}
	if r.has(gfx.RoleGroup, locales.AskEvidenceTitle()) {
		t.Fatal("证据摘录默认折叠")
	}
	r.clickButton(chip)
	if !r.has(gfx.RoleGroup, locales.AskEvidenceTitle()) || !r.contains("body-i1") {
		t.Fatalf("点 chip 应展开证据摘录：%q", r.session.Texts())
	}
	r.clickButton(locales.AskEvidenceViewCurrent())
	if got := r.openedSources(); len(got) != 1 || got[0].RefID != "i1" {
		t.Fatalf("「查看当前版本」应打开该来源：%+v", got)
	}
	r.clickButton(locales.CommonClose())
	if r.has(gfx.RoleGroup, locales.AskEvidenceTitle()) {
		t.Fatal("关闭后摘录应收起")
	}
}

func TestUIClearedEvidenceIsDescribedNotShown(t *testing.T) {
	msg := done("m1", "q", "see [1]")
	src := source(1, "i1", "fp1")
	src.Cleared = true
	yes := true
	src.Evidence.Cleared = &yes
	src.Evidence.Text = ""
	msg.Sources = []services.AskSource{src}
	r := newUIRig(t, withActive("s1"), func(r *uiRig) { putSession(t, r.svc, "s1", msg) })
	r.clickButton("[1] title-i1")
	if !r.contains(locales.AskEvidenceClearedHint()) {
		t.Fatalf("已清除的证据应说明原因：%q", r.session.Texts())
	}
	if r.contains("body-i1") {
		t.Fatal("已清除的证据正文不能再显示")
	}
}

func TestUIAllEvidenceExpander(t *testing.T) {
	msg := done("m1", "q", "answer")
	msg.Sources = []services.AskSource{source(1, "i1", "fp1")}
	msg.EvidenceSources = []services.AskSource{source(1, "i1", "fp1"), source(2, "i2", "fp2")}
	r := newUIRig(t, withActive("s1"), func(r *uiRig) { putSession(t, r.svc, "s1", msg) })
	label := locales.AskEvidenceAll(2)
	if !r.hasButton(label) {
		t.Fatalf("应有「本次读取的全部证据」展开钮：%q", r.session.Texts())
	}
	if r.hasButton("title-i2") {
		t.Fatal("展开前不列出全部证据")
	}
	r.clickButton(label)
	if !r.hasButton("title-i2") {
		t.Fatalf("展开后应列出每条证据：%q", r.session.Texts())
	}
	r.clickButton("title-i2")
	if !r.contains("body-i2") {
		t.Fatal("点证据条目应展开它的摘录")
	}
}

func TestUITruncationModelAndFallbackNotes(t *testing.T) {
	msg := done("m1", "q", "answer")
	msg.Truncated, msg.Model, msg.UsedFallback = true, "model-x", true
	msg.Warnings = []string{"warning-one"}
	r := newUIRig(t, withActive("s1"), func(r *uiRig) { putSession(t, r.svc, "s1", msg) })
	for _, want := range []string{locales.AskAnswerTruncated(), "model-x", locales.AskFallbackBadge(), "warning-one"} {
		if !r.contains(want) {
			t.Fatalf("画面上应有 %q：%q", want, r.session.Texts())
		}
	}
}

func TestUICopyDeleteAndRetryActions(t *testing.T) {
	r := newUIRig(t, withActive("s1"), func(r *uiRig) {
		putSession(t, r.svc, "s1", done("m1", "first", "first answer"), done("m2", "second", "second answer"))
	})
	r.clickNthButton(locales.AskCopyAnswer(), 1)
	if got := r.copiedTexts(); len(got) != 1 || got[0] != "second answer" {
		t.Fatalf("复制应取该轮的回答：%q", got)
	}
	r.clickNthButton(locales.CommonDelete(), 0)
	v := r.store.View()
	if len(v.Messages) != 1 || v.Messages[0].ID != "m2" {
		t.Fatalf("应只删掉第一轮：%+v", v.Messages)
	}
	if r.contains("first answer") {
		t.Fatal("被删的那一轮不该还在画面上")
	}
}

func TestUITurnHeaderCountsTurns(t *testing.T) {
	r := newUIRig(t, withActive("s1"), func(r *uiRig) {
		putSession(t, r.svc, "s1", done("m1", "a", "b"), done("m2", "c", "d"))
	})
	if !r.contains(locales.AskTurnCount(2)) {
		t.Fatalf("页头应显示轮次：%q", r.session.Texts())
	}
	empty := newUIRig(t)
	if empty.contains(locales.AskTurnCount(0)) {
		t.Fatal("空会话不显示轮次")
	}
}

func TestUILoadFailureIsAnErrorStateNotAnEmptyState(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) { r.svc.Fail(servicestest.AskOpQuery, errBoom) })
	if r.contains(locales.AskEmpty()) {
		t.Fatal("读取失败不能渲染成空态")
	}
	if !r.contains(errBoom.Error()) {
		t.Fatalf("应显示失败原因：%q", r.session.Texts())
	}
	r.svc.Fail(servicestest.AskOpQuery, nil)
	// 侧栏的历史列表同样读取失败、也有一个重试钮；工作区的是第二个。
	r.clickNthButton(locales.CommonRetry(), 1)
	if !r.contains(locales.AskEmpty()) {
		t.Fatalf("重试成功后应进入空态：%q", r.session.Texts())
	}
}