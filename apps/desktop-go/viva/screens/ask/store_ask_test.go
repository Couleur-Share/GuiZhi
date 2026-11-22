package ask

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应 ask.store.ts 的提问流程：流式更新、多轮上下文、四类失败、停止与并发取消、过时请求丢弃、重答与删除单轮。

func TestAskSuccessStreamsStepsTextAndSources(t *testing.T) {
	r := newRig(t)
	r.ready()
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{
		Steps: []string{"search", "read"}, Texts: []string{"par", "partial"}, Gate: gate,
		Answer: services.AskAnswer{Text: "final", Sources: []services.AskSource{source(1, "i1", "fp")}, Model: "m-1", UsedFallback: true, Truncated: true},
	})
	wait := goAsync(func() { r.store.Ask("  question  ") })
	waitFor(t, "流式文本到达", func() bool {
		v := r.view()
		return len(v.Messages) == 1 && v.Messages[0].Answer == "partial"
	})
	v := r.view()
	m := v.Messages[0]
	if !v.IsRunning || m.Status != services.AskStatusRunning || !slices.Equal(m.Steps, []string{"search", "read"}) || m.Question != "question" {
		t.Fatalf("回答过程中应显示进度与到目前为止的文本：%+v running=%v", m, v.IsRunning)
	}
	close(gate)
	wait()
	v = r.view()
	m = v.Messages[0]
	if v.IsRunning || m.Status != services.AskStatusDone || m.Answer != "final" || m.Model != "m-1" || !m.UsedFallback || !m.Truncated || len(m.Sources) != 1 {
		t.Fatalf("完成后的消息不对：%+v", m)
	}
	saves := r.svc.Saves()
	if len(saves) < 2 || !strings.Contains(saves[0].MessagesJSON, `"status":"running"`) || !strings.Contains(saves[len(saves)-1].MessagesJSON, `"status":"done"`) {
		t.Fatalf("应先存下这一轮的提问，结束后再存结果：%d 次", len(saves))
	}
}

func TestAskPassesOnlyDoneTurnsAsHistory(t *testing.T) {
	r := newRig(t)
	failed := services.AskMessage{ID: "m2", Question: "q2", Status: services.AskStatusError, Error: "x"}
	r.putAsk("s1", done("m1", "q1", "a1"), failed, done("m3", "q3", "a3"))
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.ready()
	r.store.Ask("q4")
	calls := r.svc.AskCalls()
	want := []services.AskTurn{{Question: "q1", Answer: "a1"}, {Question: "q3", Answer: "a3"}}
	if len(calls) != 1 || !slices.Equal(calls[0].History, want) || calls[0].Question != "q4" {
		t.Fatalf("多轮上下文只带已完成的轮次：%+v", calls)
	}
}

func TestAskIgnoresEmptyAndWhileRunning(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.store.Ask("   ")
	r.store.Ask("")
	if len(r.svc.AskCalls()) != 0 || len(r.view().Messages) != 0 {
		t.Fatal("空问题不发起")
	}
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Gate: gate})
	wait := goAsync(func() { r.store.Ask("first") })
	waitFor(t, "第一轮开始", func() bool { return r.view().IsRunning && len(r.svc.AskCalls()) == 1 })
	r.store.Ask("second")
	if len(r.svc.AskCalls()) != 1 || len(r.view().Messages) != 1 {
		t.Fatal("回答进行中再次提问应被忽略")
	}
	close(gate)
	wait()
}

func TestAskNotConfiguredNeverWritesHistory(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.svc.SetQAConfigured(false)
	r.store.Ask("q")
	if len(r.view().Messages) != 0 || len(r.svc.Saves()) != 0 || len(r.svc.AskCalls()) != 0 {
		t.Fatal("没有文本模型不能把注定失败的提问写进历史")
	}
	r.wantToast(locales.CommonOperationFailed(locales.AskSend()), locales.AskNotConfigured())
	r.svc.SetQAConfigured(true)
	r.svc.Fail(servicestest.AskOpQAConfigured, errBoom)
	r.store.Ask("q")
	r.wantToast(locales.CommonOperationFailed(locales.AskSend()), "boom")
	if len(r.svc.AskCalls()) != 0 {
		t.Fatal("读不到配置也不发起")
	}
}

func TestAskFailureClasses(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		answer string
		kind   string
		class  Class
		text   string
	}{
		{"未配置", services.ErrAINotConfigured, "partial", services.AskErrorNotConfigured, ClassNotConfigured, ""},
		{"无来源", services.ErrAskNoSource, "partial", services.AskErrorNoSource, ClassNoSource, ""},
		{"通用", errors.New("rate limited"), "partial", services.AskErrorGeneric, ClassGeneric, "rate limited"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.ready()
			r.svc.QueueAsk(servicestest.AskScript{Texts: []string{tc.answer}, Err: tc.err})
			r.store.Ask("q")
			m := lastMessage(t, r.view())
			if m.Status != services.AskStatusError || m.ErrorKind != tc.kind || m.Error != tc.text || ClassOf(m) != tc.class {
				t.Fatalf("%+v class=%q", m, ClassOf(m))
			}
			if tc.kind == services.AskErrorNoSource && m.Answer != "" {
				t.Fatalf("无来源应清掉已有文本：%q", m.Answer)
			}
			if r.view().IsRunning {
				t.Fatal("失败后应结束运行态")
			}
			if rec, ok := r.svc.Session(r.view().ActiveSessionID); !ok || !strings.Contains(rec.MessagesJSON, `"status":"error"`) {
				t.Fatalf("失败的轮次也要保存：%+v", rec)
			}
			if len(r.toasts()) != 0 {
				t.Fatalf("失败显示在消息里，不弹提示：%+v", r.toasts())
			}
		})
	}
}

func TestClassOf(t *testing.T) {
	if ClassOf(services.AskMessage{Status: services.AskStatusDone, ErrorKind: services.AskErrorGeneric}) != ClassNone {
		t.Fatal("没失败不归类")
	}
	if got := ClassOf(services.AskMessage{Status: services.AskStatusError, ErrorKind: services.AskErrorGeneric, Stopped: true}); got != ClassStopped {
		t.Fatalf("停止是独立一类：%q", got)
	}
	if got := ClassOf(services.AskMessage{Status: services.AskStatusError, ErrorKind: services.AskErrorNotConfigured, Stopped: true}); got != ClassNotConfigured {
		t.Fatalf("%q", got)
	}
	if got := ClassOf(services.AskMessage{Status: services.AskStatusError}); got != ClassGeneric {
		t.Fatalf("没有类别按通用失败：%q", got)
	}
}

func TestAskStopMarksStoppedAndDropsLateCallbacks(t *testing.T) {
	r := newRig(t)
	r.ready()
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Texts: []string{"x"}, Gate: gate, After: []string{"late"}, Answer: services.AskAnswer{Text: "never"}})
	wait := goAsync(func() { r.store.Ask("q") })
	waitFor(t, "已有文本", func() bool { v := r.view(); return len(v.Messages) == 1 && v.Messages[0].Answer == "x" })
	r.store.Stop()
	wait()
	v := r.view()
	m := v.Messages[0]
	if v.IsRunning || m.Status != services.AskStatusError || m.Error != locales.AskStopped() || !m.Stopped || ClassOf(m) != ClassStopped || m.Answer != "x" {
		t.Fatalf("停止后应标成已停止，保留已到的文本，迟到的回调被丢弃：%+v", m)
	}
	rec, _ := r.svc.Session(v.ActiveSessionID)
	if !strings.Contains(rec.MessagesJSON, `"stopped":true`) {
		t.Fatalf("停止的状态要保存：%s", rec.MessagesJSON)
	}
	r.store.Stop() // 再停一次无副作用
}

func TestStoppedErrorRoundTripsAsStopped(t *testing.T) {
	r := newRig(t)
	r.putAsk("other", done("m", "q", "a")) // 保证有地方可去
	r.ready()
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Gate: gate})
	wait := goAsync(func() { r.store.Ask("q") })
	waitFor(t, "开始", func() bool { return len(r.svc.AskCalls()) == 1 })
	r.store.Stop()
	wait()
	id := r.view().ActiveSessionID
	r.store.SwitchSession("other")
	r.store.SwitchSession(id)
	if m := lastMessage(t, r.view()); ClassOf(m) != ClassStopped {
		t.Fatalf("重新读入后仍是「已停止」：%+v", m)
	}
}

func TestConcurrentStopIsSafe(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.ready()
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Texts: []string{"x"}, Gate: gate, After: []string{"late"}})
	wait := goAsync(func() { r.store.Ask("q") })
	waitFor(t, "开始", func() bool { v := r.view(); return len(v.Messages) == 1 && v.Messages[0].Answer == "x" })
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.store.Stop()
			_ = r.store.View()
		}()
	}
	wg.Wait()
	wait()
	waitFor(t, "后台保存结束", func() bool {
		rec, ok := r.svc.Session(r.view().ActiveSessionID)
		return ok && strings.Contains(rec.MessagesJSON, `"stopped":true`)
	})
	v := r.view()
	if v.IsRunning || len(v.Messages) != 1 || !v.Messages[0].Stopped {
		t.Fatalf("%+v", v)
	}
}

func TestConcurrentStopAndSwitchWhileStreaming(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.putAsk("other", done("m", "qo", "ao"))
	r.ready()
	gate := make(chan struct{})
	texts := make([]string, 200)
	for i := range texts {
		texts[i] = strings.Repeat("x", i+1)
	}
	r.svc.QueueAsk(servicestest.AskScript{Texts: texts, Gate: gate, After: texts[:20]})
	wait := goAsync(func() { r.store.Ask("q") })
	waitFor(t, "开始", func() bool { return len(r.svc.AskCalls()) == 1 })
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); r.store.SwitchSession("other") }()
	go func() { defer wg.Done(); r.store.Stop() }()
	go func() { defer wg.Done(); r.store.NewSession() }()
	wg.Wait()
	wait()
	_ = r.view()
	// 无论怎么交错，进行中的状态都不会残留
	if r.view().IsRunning {
		t.Fatal("取消之后不该还在运行")
	}
}

// 回答进行中切走会话：迟到的回调不能落进新会话。
func TestAskStaleRunDoesNotLeakIntoOtherSession(t *testing.T) {
	r := newRig(t)
	r.putAsk("other", done("m9", "q-other", "a-other"))
	r.ready()
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Texts: []string{"x"}, Gate: gate, After: []string{"late"}, Answer: services.AskAnswer{Text: "never"}})
	wait := goAsync(func() { r.store.Ask("q") })
	waitFor(t, "已有文本", func() bool { v := r.view(); return len(v.Messages) == 1 && v.Messages[0].Answer == "x" })
	r.store.SwitchSession("other")
	wait()
	v := r.view()
	if v.ActiveSessionID != "other" || len(v.Messages) != 1 || v.Messages[0].Answer != "a-other" || v.IsRunning {
		t.Fatalf("新会话不能被旧一轮改动：%+v", v)
	}
	if rec, _ := r.svc.Session("other"); !strings.Contains(rec.MessagesJSON, "a-other") || strings.Contains(rec.MessagesJSON, "late") {
		t.Fatalf("旧一轮不能写进新会话：%s", rec.MessagesJSON)
	}
}

// 重新提问后，上一轮被取消的迟到回调不能写进新一轮。
func TestAskLateCallbacksOfStoppedRunDoNotTouchNextRun(t *testing.T) {
	r := newRig(t)
	r.ready()
	gate1 := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Texts: []string{"one"}, Gate: gate1, After: []string{"LATE"}})
	wait1 := goAsync(func() { r.store.Ask("q1") })
	waitFor(t, "第一轮有文本", func() bool { v := r.view(); return len(v.Messages) == 1 && v.Messages[0].Answer == "one" })
	r.store.Stop()
	gate2 := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Texts: []string{"two"}, Gate: gate2, Answer: services.AskAnswer{Text: "two-final"}})
	wait2 := goAsync(func() { r.store.Ask("q2") })
	waitFor(t, "第二轮有文本", func() bool { v := r.view(); return len(v.Messages) == 2 && v.Messages[1].Answer == "two" })
	wait1() // 第一轮的取消与迟到文本此时才到达
	close(gate2)
	wait2()
	v := r.view()
	if len(v.Messages) != 2 || v.Messages[0].Answer != "one" || v.Messages[1].Answer != "two-final" || v.Messages[1].Status != services.AskStatusDone {
		t.Fatalf("两轮互不干扰：%+v", v.Messages)
	}
}

func TestAskInitialSaveFailureAbortsRun(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	r.store.Ask("q")
	v := r.view()
	if len(r.svc.AskCalls()) != 0 || v.IsRunning || v.SaveError != "boom" {
		t.Fatalf("存不下提问就不发起回答：calls=%d %+v", len(r.svc.AskCalls()), v)
	}
	if m := lastMessage(t, v); !m.Stopped || m.Status != services.AskStatusError {
		t.Fatalf("这一轮标成已停止，对话留在内存里：%+v", m)
	}
	r.svc.Fail(servicestest.AskOpSave, nil)
	if !r.store.Persist() {
		t.Fatal("重试保存应成功")
	}
	if v := r.view(); v.SaveError != "" {
		t.Fatalf("%+v", v)
	}
}

func TestAskFinalSaveFailureKeepsAnswerAndRetries(t *testing.T) {
	r := newRig(t)
	r.ready()
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Texts: []string{"x"}, Gate: gate, Answer: services.AskAnswer{Text: "final"}})
	wait := goAsync(func() { r.store.Ask("q") })
	waitFor(t, "第一次保存已完成", func() bool {
		return len(r.svc.Saves()) == 1 && len(r.view().Messages) == 1 && r.view().Messages[0].Answer == "x"
	})
	r.svc.FailSaves(1, errBoom)
	close(gate)
	wait()
	v := r.view()
	if m := lastMessage(t, v); m.Status != services.AskStatusDone || m.Answer != "final" || v.SaveError != "boom" {
		t.Fatalf("答案保留，提示未保存：%+v", v)
	}
	if !r.store.Persist() || r.view().SaveError != "" {
		t.Fatal("重试保存应成功并清掉提示")
	}
	if rec, _ := r.svc.Session(v.ActiveSessionID); !strings.Contains(rec.MessagesJSON, "final") {
		t.Fatalf("重试后答案已落库：%s", rec.MessagesJSON)
	}
}

func TestAskEvidenceMergedDedupedAndWarningsUnique(t *testing.T) {
	r := newRig(t)
	r.ready()
	gate := make(chan struct{})
	a, b := source(1, "i1", "fp1"), source(2, "i2", "fp2")
	b2 := source(2, "i2", "fp2-new") // 同一份资料内容变了：指纹不同，算另一条证据
	r.svc.QueueAsk(servicestest.AskScript{
		Evidence: [][]services.AskSource{{a}, {a, b}, {b2}},
		Warnings: []string{"w", "w", "w2"}, Gate: gate,
		Answer: services.AskAnswer{Text: "ok", Sources: []services.AskSource{a, b2}},
	})
	wait := goAsync(func() { r.store.Ask("q") })
	waitFor(t, "证据到达", func() bool { v := r.view(); return len(v.Messages) == 1 && len(v.Messages[0].EvidenceSources) == 3 })
	m := r.view().Messages[0]
	if len(m.EvidenceSources) != 3 || len(m.Sources) != 1 || m.Sources[0].Evidence.Fingerprint != "fp2-new" {
		t.Fatalf("证据按 kind:ref:指纹去重累积，来源取最新一批：evidence=%d sources=%+v", len(m.EvidenceSources), m.Sources)
	}
	if !slices.Equal(m.Warnings, []string{"w", "w2"}) {
		t.Fatalf("同一句提示只留一条：%v", m.Warnings)
	}
	close(gate)
	wait()
	if m := r.view().Messages[0]; m.Status != services.AskStatusDone || len(m.Sources) != 2 {
		t.Fatalf("%+v", m)
	}
}

func TestMergeEvidenceKeepsFirstPositionLastContent(t *testing.T) {
	a1 := source(1, "i1", "fp")
	a2 := a1
	a2.Title = "updated"
	b := source(2, "i2", "fp")
	got := mergeEvidence([]services.AskSource{a1, b}, []services.AskSource{a2})
	if len(got) != 2 || got[0].Title != "updated" || got[1].RefID != "i2" {
		t.Fatalf("%+v", got)
	}
}

func TestAskLateEvidenceAfterClearIsSanitized(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.store.ClearEvidenceForItems([]string{"i1"})
	r.svc.QueueAsk(servicestest.AskScript{
		Evidence: [][]services.AskSource{{source(1, "i1", "fp"), source(2, "i2", "fp2")}},
		Answer:   services.AskAnswer{Text: "ok", Sources: []services.AskSource{source(1, "i1", "fp")}},
	})
	r.store.Ask("q")
	m := r.view().Messages[0]
	if !m.Sources[0].Cleared || m.Sources[0].Title != "cleared" || len(m.EvidenceSources) != 2 || !m.EvidenceSources[0].Cleared || m.EvidenceSources[1].Cleared {
		t.Fatalf("清除之后到达的证据也要脱敏，其余原样：%+v", m)
	}
	if m.Answer != "ok" {
		t.Fatalf("回答文本原样：%q", m.Answer)
	}
}

func TestRetryDropsLaterTurnsAndReasks(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m1", "q1", "a1"), done("m2", "q2", "a2"), done("m3", "q3", "a3"))
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.ready()
	r.store.Retry("m2")
	calls := r.svc.AskCalls()
	if len(calls) != 1 || calls[0].Question != "q2" || !slices.Equal(calls[0].History, []services.AskTurn{{Question: "q1", Answer: "a1"}}) {
		t.Fatalf("重答应丢弃这一轮及之后的轮次，上下文只剩之前的：%+v", calls)
	}
	v := r.view()
	if len(v.Messages) != 2 || v.Messages[1].Answer != "echo:q2" || v.Messages[1].ID == "m2" {
		t.Fatalf("%+v", v.Messages)
	}
	r.store.Retry("ghost")
	if len(r.svc.AskCalls()) != 1 {
		t.Fatal("找不到的轮次不处理")
	}
}

func TestRetryIgnoredWhileRunning(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m1", "q1", "a1"))
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.ready()
	gate := make(chan struct{})
	r.svc.QueueAsk(servicestest.AskScript{Gate: gate})
	wait := goAsync(func() { r.store.Ask("q2") })
	waitFor(t, "运行中", func() bool { return r.view().IsRunning && len(r.svc.AskCalls()) == 1 })
	r.store.Retry("m1")
	r.store.RemoveMessage("m1")
	if v := r.view(); len(v.Messages) != 2 {
		t.Fatalf("运行中不重答也不删除：%+v", v.Messages)
	}
	close(gate)
	wait()
}

func TestRemoveMessage(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m1", "q1", "a1"), done("m2", "q2", "a2"))
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.ready()
	r.store.RemoveMessage("ghost")
	if len(r.view().Messages) != 2 || len(r.svc.Saves()) != 0 {
		t.Fatal("找不到的轮次什么也不做")
	}
	r.store.RemoveMessage("m1")
	v := r.view()
	if len(v.Messages) != 1 || v.Messages[0].ID != "m2" {
		t.Fatalf("%+v", v.Messages)
	}
	if rec, _ := r.svc.Session("s1"); strings.Contains(rec.MessagesJSON, "q1") {
		t.Fatalf("删除应保存：%s", rec.MessagesJSON)
	}
	r.store.RemoveMessage("m2") // 删光就连会话一起删
	if _, ok := r.svc.Session("s1"); ok || r.view().ActiveSessionID == "s1" {
		t.Fatal("删光最后一轮应删除会话")
	}
}

func TestRemoveMessageSaveFailureRestores(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m1", "q1", "a1"), done("m2", "q2", "a2"))
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.ready()
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	r.store.RemoveMessage("m1")
	v := r.view()
	if len(v.Messages) != 2 || v.SaveError != "boom" {
		t.Fatalf("保存失败应恢复原样，不让界面显示的比库里少：%+v", v)
	}
}

func TestStopWithoutRunIsNoop(t *testing.T) {
	r := newRig(t)
	r.ready()
	before := r.wakes.Load()
	r.store.Stop()
	if r.wakes.Load() != before || len(r.svc.Saves()) != 0 {
		t.Fatal("没有进行中的回答时停止是空操作")
	}
}

func TestAskInArticleSessionIsRefused(t *testing.T) {
	r := newRig(t)
	r.putArticle("a1", "item-1", "T", nil, articleDone("m", "q", "a"))
	_ = r.prefs.Set(PrefActiveSession, "a1")
	r.ready()
	r.store.Ask("q")
	if len(r.svc.AskCalls()) != 0 {
		t.Fatal("本文会话里的全局提问入口不发起问答")
	}
}
