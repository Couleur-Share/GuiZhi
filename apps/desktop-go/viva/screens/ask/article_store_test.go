package ask

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应 article-ask.store.ts：打开与代次、提问流式补丁与存档节流、停止与过时补丁、重答、保存失败与重试、证据清除。

func patchAnswer(text string) aiflow.ArticlePatch { return aiflow.ArticlePatch{Answer: &text} }

func patchStatus(status string) aiflow.ArticlePatch {
	return aiflow.ArticlePatch{Status: &status, ClearStep: true}
}

func (r *rig) openArticle(item string) {
	r.t.Helper()
	r.svc.PutItemTitle(item, "Title of "+item)
	r.article().Open(target(item), "")
	if v := r.artView(); v.LoadError != "" || v.Loading || v.ItemID != item {
		r.t.Fatalf("打开应成功：%+v", v)
	}
}

func TestArticleOpenFreshSession(t *testing.T) {
	r := newRig(t)
	r.svc.PutItemTitle("item-1", "Title")
	r.article().Open(target("item-1"), "")
	v := r.artView()
	if v.SessionID != "uuid-1" || v.ItemID != "item-1" || v.Title != "Title" || !v.WebEnabled || len(v.Messages) != 0 || v.Loading || v.LoadError != "" {
		t.Fatalf("没有历史会话时开一个空会话，联网默认开启：%+v", v)
	}
	if got := r.svc.Lists(); len(got) != 1 || got[0].Scope != services.AskScopeArticle || got[0].ItemID != "item-1" {
		t.Fatalf("应按文章列出本文会话：%+v", got)
	}
	r.article().Open(target("item-1"), "") // 同一篇：什么也不做
	if len(r.svc.Lists()) != 1 {
		t.Fatal("已打开同一篇不该重复加载")
	}
	r.article().Open(target("item-1"), "uuid-1")
	if len(r.svc.Lists()) != 1 {
		t.Fatal("指定的会话就是当前会话，也不重复加载")
	}
}

func TestArticleOpenLoadsLatestSessionAndInterruptsRunning(t *testing.T) {
	r := newRig(t)
	r.svc.PutItemTitle("item-1", "Saved title")
	web := false
	running := articleDone("m2", "q2", "partial")
	running.Status = services.AskStatusRunning
	running.Step = str("searching")
	r.putArticle("a1", "item-1", "Saved title", &web, articleDone("m1", "q1", "a1"), running)
	r.article().Open(target("item-1"), "")
	v := r.artView()
	if v.SessionID != "a1" || v.Title != "Saved title" || v.WebEnabled || len(v.Messages) != 2 || len(v.Sessions) != 1 {
		t.Fatalf("应打开最近一次会话，标题取文章当前标题：%+v", v)
	}
	if m := v.Messages[1]; m.Status != services.AskStatusError || m.Error == nil || *m.Error != locales.AskSessionInterrupted() || m.Step != nil {
		t.Fatalf("没完成的轮次标成已中断并清掉步骤：%+v", m)
	}
	if !r.article().Persist() || len(r.svc.Saves()) != 0 {
		t.Fatalf("读入的内容算已落盘，不重复保存：%+v", r.svc.Saves())
	}
}

func TestArticleOpenSpecificSessionAndDeletedArticleTitle(t *testing.T) {
	r := newRig(t)
	r.putArticle("a1", "item-1", "Saved title", nil, articleDone("m1", "q1", "a1"))
	r.putArticle("a2", "item-1", "Saved title", nil, articleDone("m2", "q2", "a2"))
	r.article().Open(target("item-1"), "a1")
	v := r.artView()
	if v.SessionID != "a1" || v.Title != "Saved title" || !v.WebEnabled {
		t.Fatalf("文章已不存在时用会话里存的标题，没记过联网开关按开启：%+v", v)
	}
	r.article().Open(target("item-1"), "a2")
	if v := r.artView(); v.SessionID != "a2" || v.Messages[0].Question != "q2" {
		t.Fatalf("%+v", v)
	}
	r2 := newRig(t)
	r2.article().Open(target("gone"), "")
	if got := r2.artView().Title; got != locales.ArticleAskArticleDeleted() {
		t.Fatalf("文章没了又没有存过标题：%q", got)
	}
}

func TestArticleOpenErrorsAreLoadErrorsAndRetry(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig)
	}{
		{"列出失败", func(r *rig) { r.svc.Fail(servicestest.AskOpList, errBoom) }},
		{"读取失败", func(r *rig) { r.putArticle("a1", "item-1", "T", nil); r.svc.Fail(servicestest.AskOpGet, errBoom) }},
		{"读文章失败", func(r *rig) { r.svc.Fail(servicestest.AskOpItemInfo, errBoom) }},
		{"会话与文章不一致", func(r *rig) {
			rec := r.putArticle("a1", "item-OTHER", "T", nil)
			_ = rec
		}},
		{"记录损坏", func(r *rig) {
			rec := services.AskSessionRecord{MessagesJSON: `[{"id":"m"}]`}
			rec.ID, rec.Scope, rec.ItemID = "a1", services.AskScopeArticle, "item-1"
			rec.Target = &aiflow.ArticleTarget{ItemID: "item-1", View: "body"}
			r.svc.PutSession(rec)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.svc.PutItemTitle("item-1", "T")
			tc.setup(r)
			if tc.name == "会话与文章不一致" {
				r.article().Open(target("item-1"), "a1")
			} else {
				r.article().Open(target("item-1"), "")
			}
			v := r.artView()
			if v.LoadError == "" || v.Loading {
				t.Fatalf("加载失败应是错误态：%+v", v)
			}
			if r.article().Ask("q", target("item-1"), nil) {
				t.Fatal("加载出错时不接受提问")
			}
			if len(r.svc.AskCalls()) != 0 {
				t.Fatal("加载出错时不发起问答")
			}
			// 修复后再 Open 即重试
			r.svc.Fail(servicestest.AskOpList, nil)
			r.svc.Fail(servicestest.AskOpGet, nil)
			r.svc.Fail(servicestest.AskOpItemInfo, nil)
			if tc.name == "记录损坏" || tc.name == "会话与文章不一致" {
				return
			}
			r.article().Open(target("item-1"), "")
			if v := r.artView(); v.LoadError != "" || v.ItemID != "item-1" {
				t.Fatalf("重试应恢复：%+v", v)
			}
		})
	}
}

// 快速切换文章：第一次在「保存当前对话」处卡住，第二次发起后，第一次醒来必须丢弃。
func TestArticleOpenStaleGenerationIsDropped(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.svc.PutItemTitle("item-1", "One")
	r.svc.PutItemTitle("item-2", "Two")
	r.svc.PutItemTitle("item-3", "Three")
	r.article().Open(target("item-1"), "")
	r.article().Ask("q", target("item-1"), nil)
	r.dirtyArticle()
	release := r.svc.HoldSave()
	first := goAsync(func() { r.article().Open(target("item-2"), "") })
	waitFor(t, "第一次打开卡在保存", func() bool { return len(r.svc.Saves()) >= 3 })
	second := goAsync(func() { r.article().Open(target("item-3"), "") })
	waitFor(t, "代次前进", func() bool { a := r.article(); a.mu.Lock(); defer a.mu.Unlock(); return a.generation >= 3 })
	release()
	first()
	second()
	if v := r.artView(); v.ItemID != "item-3" || v.Title != "Three" {
		t.Fatalf("最终应停在第二次打开的文章：%+v", v)
	}
}

func (r *rig) dirtyArticle() {
	a := r.article()
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.signatures, a.view.SessionID)
}

func TestArticleAskSuccessPersistsWithMetadata(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	tg := target("item-1")
	tg.Selection = str("selected text")
	ok := r.article().Ask("  why?  ", tg, nil)
	v := r.artView()
	if !ok || v.Running || len(v.Messages) != 1 {
		t.Fatalf("%+v", v)
	}
	m := v.Messages[0]
	if m.Question != "why?" || m.Answer != "echo:why?" || m.Status != services.AskStatusDone || m.Step != nil || !m.WebEnabled || m.WebStatus != "searching" || m.ID != "uuid-2" {
		t.Fatalf("%+v", m)
	}
	calls := r.svc.ArticleCalls()
	if len(calls) != 1 || calls[0].Question != "why?" || !calls[0].WebEnabled || calls[0].RequestID != m.ID || calls[0].Target.Selection == nil || len(calls[0].History) != 0 {
		t.Fatalf("%+v", calls)
	}
	saves := r.svc.Saves()
	last := saves[len(saves)-1]
	if last.Scope != services.AskScopeArticle || last.ItemID != "item-1" || last.ArticleTitle != "Title of item-1" || last.Title != "why?" || last.Target == nil || last.WebEnabled == nil || !*last.WebEnabled {
		t.Fatalf("保存应带文章关联、标题、目标与联网开关：%+v", last)
	}
	if !strings.Contains(saves[0].MessagesJSON, `"status":"running"`) {
		t.Fatal("开始前先存下提问")
	}
	if len(v.Sessions) != 1 || v.Sessions[0].ID != "uuid-1" {
		t.Fatalf("保存后会话列表带上本会话：%+v", v.Sessions)
	}
	if r.savedEvents() == 0 {
		t.Fatal("保存后应通知历史刷新")
	}
	// 第二轮带上第一轮作为历史
	if !r.article().Ask("again", target("item-1"), nil) {
		t.Fatal("第二轮应成功")
	}
	calls = r.svc.ArticleCalls()
	if len(calls[1].History) != 1 || calls[1].History[0].Question != "why?" {
		t.Fatalf("历史应带上前一轮：%+v", calls[1].History)
	}
}

func TestArticleAskRefusals(t *testing.T) {
	r := newRig(t)
	if r.article().Ask("q", target("item-1"), nil) {
		t.Fatal("还没打开不接受提问")
	}
	r.openArticle("item-1")
	for name, args := range map[string]struct {
		q string
		t aiflow.ArticleTarget
	}{"空问题": {"   ", target("item-1")}, "别的文章": {"q", target("item-2")}} {
		if r.article().Ask(args.q, args.t, nil) {
			t.Fatalf("%s不接受提问", name)
		}
	}
	if len(r.svc.ArticleCalls()) != 0 {
		t.Fatal("被拒绝的提问不该发起问答")
	}
	gate := make(chan struct{})
	r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate})
	wait := goAsync(func() { r.article().Ask("first", target("item-1"), nil) })
	waitFor(t, "运行中", func() bool { return r.artView().Running && len(r.svc.ArticleCalls()) == 1 })
	if r.article().Ask("second", target("item-1"), nil) {
		t.Fatal("回答进行中不接受提问")
	}
	close(gate)
	wait()
}

func TestArticleAskStreamsPatchesAndAppliesEveryField(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	gate := make(chan struct{})
	step, web := "reading", "ready"
	ctxVal := aiflow.ArticleContext{Target: target("item-1"), Title: "ctx", Fingerprint: "fp"}
	r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate, Patches: []aiflow.ArticlePatch{
		{Step: &step}, {Context: &ctxVal}, {Sources: []aiflow.ArticleSource{{Ordinal: 1, Kind: "article", Title: "s"}}},
		{Warnings: []string{"w"}}, {WebStatus: &web}, patchAnswer("par"), patchAnswer("partial"),
	}, After: []aiflow.ArticlePatch{patchStatus(services.AskStatusDone)}})
	wait := goAsync(func() { r.article().Ask("q", target("item-1"), nil) })
	waitFor(t, "补丁到达", func() bool { v := r.artView(); return len(v.Messages) == 1 && v.Messages[0].Answer == "partial" })
	m := r.artView().Messages[0]
	if m.Step == nil || *m.Step != "reading" || m.Context == nil || m.Context.Title != "ctx" || len(m.Sources) != 1 || !slices.Equal(m.Warnings, []string{"w"}) || m.WebStatus != "ready" {
		t.Fatalf("补丁里被设置的字段应写进消息：%+v", m)
	}
	close(gate)
	wait()
	if m := r.artView().Messages[0]; m.Status != services.AskStatusDone || m.Step != nil {
		t.Fatalf("放行后收到终态补丁：状态完成、步骤清掉：%+v", m)
	}
}

func TestApplyPatchFields(t *testing.T) {
	step, status, answer, model, web, trunc := "s", services.AskStatusDone, "a", "m", "ready", true
	base := aiflow.ArticleMessage{ID: "x", Question: "q", Step: str("old"), Sources: []aiflow.ArticleSource{{Ordinal: 9}}, Warnings: []string{"keep"}, Answer: "old-answer"}
	got := applyPatch(base, aiflow.ArticlePatch{Step: &step, Answer: &answer, Status: &status, Model: &model, WebStatus: &web, Truncated: &trunc})
	if *got.Step != "s" || got.Answer != "a" || got.Status != status || *got.Model != "m" || got.WebStatus != "ready" || !*got.Truncated || len(got.Sources) != 1 || got.Warnings[0] != "keep" || got.ID != "x" {
		t.Fatalf("%+v", got)
	}
	cleared := applyPatch(base, aiflow.ArticlePatch{ClearStep: true})
	if cleared.Step != nil || cleared.Answer != "old-answer" {
		t.Fatalf("ClearStep 只清步骤：%+v", cleared)
	}
	if base.Step == nil || *base.Step != "old" {
		t.Fatal("不能改动入参")
	}
}

// 流式期间的存档节流：拿到上下文必存；回答片段按间隔存，第一次（上次存档时间为零）一定到点。
// 终态补丁最后才来，所以「内容里仍是 running」的保存就是中途存档（含开始前那一次）。
func TestArticleCheckpointThrottle(t *testing.T) {
	cases := []struct {
		name  string
		every time.Duration
		want  int
	}{
		{"间隔很长：上下文一次、第一个回答片段一次", time.Hour, 2},
		{"间隔为零：上下文一次、每个回答片段各一次", 0, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.openArticle("item-1")
			r.article().CheckpointEvery = tc.every
			ctxVal := aiflow.ArticleContext{Target: target("item-1")}
			r.svc.QueueArticle(servicestest.ArticleScript{Patches: []aiflow.ArticlePatch{
				{Context: &ctxVal}, patchAnswer("a"), patchAnswer("ab"), patchAnswer("abc"), patchStatus(services.AskStatusDone),
			}})
			if !r.article().Ask("q", target("item-1"), nil) {
				t.Fatal("应成功")
			}
			running := 0
			for _, s := range r.svc.Saves() {
				if strings.Contains(s.MessagesJSON, `"status":"running"`) {
					running++
				}
			}
			if got := running - 1; got != tc.want { // 减去开始前存提问的那一次
				t.Fatalf("中途存档 %d 次，期望 %d", got, tc.want)
			}
		})
	}
}
func TestArticleAskErrorWritesReasonAndLogs(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.svc.QueueArticle(servicestest.ArticleScript{Err: errors.New("model exploded")})
	if r.article().Ask("q", target("item-1"), nil) {
		t.Fatal("失败应返回 false")
	}
	v := r.artView()
	m := v.Messages[0]
	if v.Running || m.Status != services.AskStatusError || m.Error == nil || *m.Error != "model exploded" || m.Step != nil {
		t.Fatalf("%+v", m)
	}
	found := false
	for _, e := range r.log.entries() {
		found = found || strings.HasPrefix(e, "articleAsk.answer|model exploded")
	}
	if !found {
		t.Fatalf("失败应留痕：%v", r.log.entries())
	}
	if rec, _ := r.svc.Session(v.SessionID); !strings.Contains(rec.MessagesJSON, "model exploded") {
		t.Fatal("失败的轮次也要保存")
	}
}

func TestArticleStopMarksStoppedAndDropsLatePatches(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	gate := make(chan struct{})
	r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate, Patches: []aiflow.ArticlePatch{patchAnswer("x")}, After: []aiflow.ArticlePatch{patchAnswer("LATE"), patchStatus(services.AskStatusDone)}})
	wait := goAsync(func() { r.article().Ask("q", target("item-1"), nil) })
	waitFor(t, "已有文本", func() bool { v := r.artView(); return len(v.Messages) == 1 && v.Messages[0].Answer == "x" })
	r.article().Stop()
	wait()
	v := r.artView()
	m := v.Messages[0]
	if v.Running || m.Status != services.AskStatusError || m.Error == nil || *m.Error != locales.AskStopped() || m.Answer != "x" {
		t.Fatalf("停止后标成已停止，迟到的补丁被丢弃：%+v", m)
	}
	if rec, _ := r.svc.Session(v.SessionID); strings.Contains(rec.MessagesJSON, "LATE") || !strings.Contains(rec.MessagesJSON, locales.AskStopped()) {
		t.Fatalf("停止的状态要保存：%s", rec.MessagesJSON)
	}
	r.article().Stop()
}

func TestArticleConcurrentStop(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.openArticle("item-1")
	gate := make(chan struct{})
	patches := make([]aiflow.ArticlePatch, 100)
	for i := range patches {
		patches[i] = patchAnswer(strings.Repeat("x", i+1))
	}
	r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate, Patches: patches, After: patches[:10]})
	wait := goAsync(func() { r.article().Ask("q", target("item-1"), nil) })
	waitFor(t, "开始", func() bool { return len(r.svc.ArticleCalls()) == 1 })
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); r.article().Stop(); _ = r.article().View() }()
	}
	wg.Wait()
	wait()
	if r.artView().Running {
		t.Fatal("取消之后不该还在运行")
	}
}

func TestArticleStaleRunAfterNewSessionIsDropped(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	gate := make(chan struct{})
	r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate, Patches: []aiflow.ArticlePatch{patchAnswer("x")}, After: []aiflow.ArticlePatch{patchAnswer("LATE")}})
	wait := goAsync(func() { r.article().Ask("q", target("item-1"), nil) })
	waitFor(t, "已有文本", func() bool { v := r.artView(); return len(v.Messages) == 1 && v.Messages[0].Answer == "x" })
	r.article().NewSession(target("item-1"))
	wait()
	if v := r.artView(); len(v.Messages) != 0 || v.SessionID == "uuid-1" || v.Running {
		t.Fatalf("新会话不能被旧一轮改动：%+v", v)
	}
}

func TestArticleAskInitialSaveFailureAborts(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	if r.article().Ask("q", target("item-1"), nil) {
		t.Fatal("存不下提问就不发起")
	}
	v := r.artView()
	if len(r.svc.ArticleCalls()) != 0 || v.Running || v.SaveError != locales.ArticleAskNotSaved() {
		t.Fatalf("%d %+v", len(r.svc.ArticleCalls()), v)
	}
	r.wantToast(locales.CommonOperationFailed(locales.ArticleAskSave()), "boom")
	r.svc.Fail(servicestest.AskOpSave, nil)
	if !r.article().Persist() {
		t.Fatal("重试保存应成功")
	}
	if v := r.artView(); v.SaveError != "" || len(v.Messages) != 1 {
		t.Fatalf("成功后清掉提示，对话还在：%+v", v)
	}
}

func TestArticlePersistFailureThenRetry(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.article().Ask("q", target("item-1"), nil)
	r.dirtyArticle()
	r.svc.FailSaves(1, errBoom)
	if r.article().Persist() {
		t.Fatal("保存失败应返回 false")
	}
	if v := r.artView(); v.SaveError != locales.ArticleAskNotSaved() || len(v.Messages) != 1 {
		t.Fatalf("%+v", v)
	}
	if !r.article().Persist() || r.artView().SaveError != "" {
		t.Fatal("重试应成功并清掉提示")
	}
	n := len(r.svc.Saves())
	if !r.article().Persist() || len(r.svc.Saves()) != n {
		t.Fatal("内容没变不重复保存")
	}
}

func TestArticleSetWebPersistsInBackground(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.article().Ask("q", target("item-1"), nil)
	n := len(r.svc.Saves())
	r.article().SetWeb(false)
	if v := r.artView(); v.WebEnabled {
		t.Fatal("开关应关闭")
	}
	saves := r.svc.Saves()
	if len(saves) != n+1 || saves[n].WebEnabled == nil || *saves[n].WebEnabled {
		t.Fatalf("联网开关属于会话内容，改了要保存：%+v", saves[n:])
	}
}

func TestArticleNewSession(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.article().Ask("q", target("item-1"), nil)
	old := r.artView().SessionID
	r.article().NewSession(target("item-1"))
	v := r.artView()
	if v.SessionID == old || len(v.Messages) != 0 || v.WebEnabled || v.ItemID != "item-1" || v.Title == "" {
		t.Fatalf("新对话：空消息、联网默认关、保留标题：%+v", v)
	}
	if _, ok := r.svc.Session(old); !ok {
		t.Fatal("旧对话应已保存")
	}
	// 保存当前对话失败时不能开新的
	r.article().Ask("q2", target("item-1"), nil)
	cur := r.artView().SessionID
	r.dirtyArticle()
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	r.article().NewSession(target("item-1"))
	if v := r.artView(); v.SessionID != cur || len(v.Messages) != 1 {
		t.Fatalf("保存失败不开新会话：%+v", v)
	}
}

func TestArticleRetryReusesContextAndWebFlag(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	ctxVal := aiflow.ArticleContext{Target: aiflow.ArticleTarget{ItemID: "item-1", View: "summary"}, Title: "saved"}
	r.svc.QueueArticle(servicestest.ArticleScript{Patches: []aiflow.ArticlePatch{{Context: &ctxVal}}, Err: errBoom})
	r.article().Ask("q1", target("item-1"), nil)
	r.article().SetWeb(false)
	r.article().Ask("q2", target("item-1"), nil)
	first := r.artView().Messages[0]
	if !r.article().Retry(first.ID) {
		t.Fatal("重答应成功")
	}
	calls := r.svc.ArticleCalls()
	got := calls[len(calls)-1]
	if got.Question != "q1" || got.Context == nil || got.Context.Title != "saved" || got.Target.View != "summary" || !got.WebEnabled || len(got.History) != 0 {
		t.Fatalf("重答沿用保存的上下文、目标与当时的联网开关：%+v", got)
	}
	v := r.artView()
	if len(v.Messages) != 1 || !v.WebEnabled {
		t.Fatalf("之后的轮次被丢弃，联网开关回到当时的值：%+v", v)
	}
	if r.article().Retry("ghost") {
		t.Fatal("找不到的轮次不处理")
	}
}

func TestArticleRetryIgnoredWhileRunning(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.article().Ask("q1", target("item-1"), nil)
	id := r.artView().Messages[0].ID
	gate := make(chan struct{})
	r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate})
	wait := goAsync(func() { r.article().Ask("q2", target("item-1"), nil) })
	waitFor(t, "运行中", func() bool { return r.artView().Running && len(r.svc.ArticleCalls()) == 2 })
	if r.article().Retry(id) {
		t.Fatal("运行中不重答")
	}
	close(gate)
	wait()
}

func TestArticleClearEvidenceSanitizesMessagesTargetAndLatePatches(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	tg := target("item-1")
	tg.Selection = str("secret selection")
	src := aiflow.ArticleSource{Ordinal: 1, Kind: "article", Title: "real", Target: &aiflow.ArticleTarget{ItemID: "item-1", View: "body"}}
	r.svc.QueueArticle(servicestest.ArticleScript{Patches: []aiflow.ArticlePatch{{Sources: []aiflow.ArticleSource{src}}, patchAnswer("answer")}})
	r.article().Ask("q", tg, nil)
	if v := r.artView(); v.Target.Selection == nil {
		t.Fatal("清除之前目标带选段")
	}
	r.store.ClearEvidenceForItems([]string{"item-1"})
	v := r.artView()
	if v.Target.Selection != nil || v.Target.ItemID != "item-1" {
		t.Fatalf("证据清除后目标只留条目与视图：%+v", v.Target)
	}
	if m := v.Messages[0]; !isCleared(m.Sources[0]) || m.Answer != "answer" {
		t.Fatalf("已展开的来源脱敏，回答原样：%+v", m)
	}
	// 清除之后到达的补丁同样脱敏
	r.svc.QueueArticle(servicestest.ArticleScript{Patches: []aiflow.ArticlePatch{{Sources: []aiflow.ArticleSource{src}}}})
	r.article().Ask("q2", tg, nil)
	if m := r.artView().Messages[1]; len(m.Sources) != 1 || !isCleared(m.Sources[0]) {
		t.Fatalf("迟到的补丁也要脱敏：%+v", m.Sources)
	}
	// 之后读入的会话同样脱敏
	r.article().NewSession(tg)
	r.putArticle("a-old", "item-1", "T", nil, aiflow.ArticleMessage{ID: "m", Question: "q", Answer: "a", Status: services.AskStatusDone, Sources: []aiflow.ArticleSource{src}, Warnings: []string{}})
	r.article().Open(tg, "a-old")
	if m := r.artView().Messages[0]; !isCleared(m.Sources[0]) {
		t.Fatalf("读入的会话也要脱敏：%+v", m.Sources)
	}
}

func TestArticleForgetOnDelete(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.article().Ask("q", target("item-1"), nil)
	id := r.artView().SessionID
	r.article().forget(id)
	if v := r.artView(); v.SessionID != "" || len(v.Messages) != 0 {
		t.Fatalf("%+v", v)
	}
	r.article().forget("someone-else") // 不相干的会话不影响
}

func TestPersistSignatureIncludesTargetWebAndTitle(t *testing.T) {
	a := target("i")
	if signature("[]", &a, true, "T") == signature("[]", &a, false, "T") || signature("[]", &a, true, "T") == signature("[]", &a, true, "U") || signature("[]", &a, true, "T") == signature("[1]", &a, true, "T") {
		t.Fatal("消息、联网开关与标题任一变化都应改变签名")
	}
	b := target("i")
	b.View = "summary"
	if signature("[]", &a, true, "T") == signature("[]", &b, true, "T") {
		t.Fatal("目标变化应改变签名")
	}
}

// 文章标题变了：存档里的标题与此刻不同，下一次保存要把它更新。
func TestArticleOpenRefreshesStoredTitle(t *testing.T) {
	r := newRig(t)
	r.svc.PutItemTitle("item-1", "New title")
	r.putArticle("a1", "item-1", "Old title", nil, articleDone("m1", "q1", "a1"))
	r.article().Open(target("item-1"), "")
	if !r.article().Persist() {
		t.Fatal("保存应成功")
	}
	saves := r.svc.Saves()
	if len(saves) != 1 || saves[0].ArticleTitle != "New title" {
		t.Fatalf("应把新标题存下：%+v", saves)
	}
}

func isCleared(s aiflow.ArticleSource) bool { return s.Cleared != nil && *s.Cleared }
