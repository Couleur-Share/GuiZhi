package ask

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 stores/ask.store.ts 的加载与会话管理：首次加载与恢复、损坏记录不伪装成空会话、
// 切换的代次保护、保存失败与重试、删除与新建、证据清除。提问、停止与重答见 store_ask_test.go。

func TestInitializeFreshStartsEmptySession(t *testing.T) {
	r := newRig(t)
	r.putAsk("old", done("m", "earlier", "a"))
	if v := r.view(); v.HasLoaded {
		t.Fatalf("加载之前不该放行空态：%+v", v)
	}
	r.store.Initialize()
	v := r.view()
	if !v.HasLoaded || v.LoadError != "" || v.ActiveSessionID != "session-1" || len(v.Messages) != 0 {
		t.Fatalf("没有记住的会话时应开一个空会话：%+v", v)
	}
	if len(v.Sessions) != 1 || v.Sessions[0].ID != "old" {
		t.Fatalf("历史列表应读到已有会话：%+v", v.Sessions)
	}
	if got := r.prefs.active(); got != "session-1" {
		t.Fatalf("新会话应被记住：%q", got)
	}
	if got := r.svc.Queries(); len(got) != 1 || got[0].Limit != 50 {
		t.Fatalf("首次加载只读一页 50 条：%+v", got)
	}
	r.store.Initialize() // 重复调用无副作用
	if len(r.svc.Queries()) != 1 {
		t.Fatal("重复初始化不该再读一次")
	}
}

func TestInitializeRestoresRememberedSession(t *testing.T) {
	r := newRig(t)
	running := services.AskMessage{ID: "m2", Question: "q2", Answer: "partial", Status: services.AskStatusRunning}
	r.putAsk("s1", done("m1", "q1", "a1"), running)
	if err := r.prefs.Set(PrefActiveSession, "s1"); err != nil {
		t.Fatal(err)
	}
	r.ready()
	v := r.view()
	if v.ActiveSessionID != "s1" || len(v.Messages) != 2 {
		t.Fatalf("应恢复上次的会话：%+v", v)
	}
	if v.Messages[0].Status != services.AskStatusDone || v.Messages[0].Answer != "a1" {
		t.Fatalf("完成的轮次原样：%+v", v.Messages[0])
	}
	if got := v.Messages[1]; got.Status != services.AskStatusError || got.Error != locales.AskSessionInterrupted() || got.ErrorKind != services.AskErrorGeneric || ClassOf(got) != ClassGeneric {
		t.Fatalf("上次没完成的轮次应标成已中断：%+v", got)
	}
	// 读入的内容算作已落盘：直接 Persist 不重复保存
	if !r.store.Persist() || len(r.svc.Saves()) != 0 {
		t.Fatalf("没改动不该保存：%+v", r.svc.Saves())
	}
}

func TestInitializeRememberedSessionGoneStartsFresh(t *testing.T) {
	r := newRig(t)
	if err := r.prefs.Set(PrefActiveSession, "deleted-long-ago"); err != nil {
		t.Fatal(err)
	}
	r.ready()
	if v := r.view(); v.ActiveSessionID != "session-1" || len(v.Messages) != 0 {
		t.Fatalf("记住的会话已不在时开新会话：%+v", v)
	}
}

func TestInitializeRestoresArticleSession(t *testing.T) {
	r := newRig(t)
	web := false
	r.putArticle("a1", "item-1", "Title", &web, articleDone("m1", "q", "a"))
	_ = r.prefs.Set(PrefActiveSession, "a1")
	r.ready()
	v := r.view()
	if v.ActiveSessionID != "a1" || v.ArticleSession == nil || v.ArticleSession.ItemID != "item-1" || len(v.Messages) != 0 {
		t.Fatalf("本文会话应以 ArticleSession 恢复，全局消息为空：%+v", v)
	}
	if v.ArticleSession.WebEnabledOrDefault() {
		t.Fatal("联网开关应带出")
	}
}

func TestInitializeFailureIsErrorStateNotEmptyAndRetries(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m1", "q1", "a1"))
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.svc.Fail(servicestest.AskOpGet, errBoom)
	r.store.Initialize()
	v := r.view()
	if v.LoadError != "boom" || !v.HasLoaded || v.ActiveSessionID != "" {
		t.Fatalf("失败要有错误态，且放行界面（HasLoaded），不能退成空会话：%+v", v)
	}
	if len(r.log.entries()) != 1 || !strings.HasPrefix(r.log.entries()[0], "ask.initialize|") {
		t.Fatalf("后台失败应留痕：%v", r.log.entries())
	}
	r.svc.Fail(servicestest.AskOpGet, nil)
	r.store.RetryLoad()
	v = r.view()
	if v.LoadError != "" || v.ActiveSessionID != "s1" || len(v.Messages) != 1 {
		t.Fatalf("重试应恢复：%+v", v)
	}
}

func TestCorruptRecordIsNotOverwritable(t *testing.T) {
	cases := map[string]string{
		"非 JSON":      "{not json",
		"不是数组":        `{"a":1}`,
		"元素不是对象":      `[1]`,
		"缺少 question": `[{"id":"m"}]`,
		"来源类型不对":      `[{"question":"q","sources":[{"ordinal":"x"}]}]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			rec := services.AskSessionRecord{MessagesJSON: body}
			rec.ID, rec.Title = "bad", "bad"
			r.svc.PutSession(rec)
			_ = r.prefs.Set(PrefActiveSession, "bad")
			r.store.Initialize()
			v := r.view()
			if v.LoadError == "" || v.ActiveSessionID == "bad" || len(v.Messages) != 0 {
				t.Fatalf("损坏记录应报加载错误：%+v", v)
			}
			if len(r.svc.Saves()) != 0 {
				t.Fatal("损坏记录不能被空会话覆盖")
			}
		})
	}
}

func TestParseStoredMessagesFillsDefaults(t *testing.T) {
	msgs, err := parseStoredMessages(`[{"question":"q","status":"weird","sources":null}]`, func(p string) string { return p + "-gen" })
	if err != nil {
		t.Fatal(err)
	}
	m := msgs[0]
	if m.ID != "ask-gen" || m.Status != services.AskStatusError || m.Sources == nil || m.Steps == nil || m.Answer != "" {
		t.Fatalf("缺省字段应补齐，未知状态按失败：%+v", m)
	}
}

func TestSwitchSessionLoadsAndRemembers(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m1", "q1", "a1"))
	r.putAsk("s2", done("m2", "q2", "a2"))
	r.ready()
	r.store.SwitchSession("s1")
	if v := r.view(); v.ActiveSessionID != "s1" || len(v.Messages) != 1 || r.prefs.active() != "s1" {
		t.Fatalf("%+v", v)
	}
	r.store.SwitchSession("s2")
	if v := r.view(); v.ActiveSessionID != "s2" || v.Messages[0].Question != "q2" {
		t.Fatalf("%+v", v)
	}
	if len(r.svc.Saves()) != 0 {
		t.Fatalf("只是浏览，切换不该刷新旧会话的更新时间：%+v", r.svc.Saves())
	}
	gets := len(r.svc.Gets())
	r.store.SwitchSession("s2") // 同一会话：什么也不做
	if len(r.svc.Gets()) != gets {
		t.Fatal("切到当前会话不该再读库")
	}
}

func TestSwitchSessionSavesCurrentFirstAndSaveFailureBlocksSwitch(t *testing.T) {
	r := newRig(t)
	r.putAsk("other", done("m9", "q9", "a9"))
	r.ready()
	r.store.Ask("hello")
	id := r.view().ActiveSessionID
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	r.dirty()
	r.store.SwitchSession("other")
	v := r.view()
	if v.ActiveSessionID != id || v.SaveError != "boom" {
		t.Fatalf("保存失败时不能切走，免得丢掉内存里的对话：%+v", v)
	}
	r.svc.Fail(servicestest.AskOpSave, nil)
	r.store.SwitchSession("other")
	v = r.view()
	if v.ActiveSessionID != "other" || v.SaveError != "" {
		t.Fatalf("保存恢复后应能切走：%+v", v)
	}
	if rec, ok := r.svc.Session(id); !ok || !strings.Contains(rec.MessagesJSON, "echo:hello") {
		t.Fatalf("切走前应已保存当前会话：%+v", rec)
	}
}

func TestSwitchSessionFailureKeepsCurrentAndRetries(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m1", "q1", "a1"))
	r.ready()
	cur := r.view().ActiveSessionID
	r.svc.Fail(servicestest.AskOpGet, errBoom)
	r.store.SwitchSession("s1")
	v := r.view()
	if v.ActiveSessionID != cur || v.LoadError != "boom" {
		t.Fatalf("失败应保持原会话并给出错误：%+v", v)
	}
	r.svc.Fail(servicestest.AskOpGet, nil)
	r.store.RetryLoad()
	if v := r.view(); v.ActiveSessionID != "s1" || v.LoadError != "" {
		t.Fatalf("重试应再切一次：%+v", v)
	}
}

func TestSwitchSessionMissingAndCorrupt(t *testing.T) {
	r := newRig(t)
	rec := services.AskSessionRecord{MessagesJSON: "[[]]"}
	rec.ID = "bad"
	r.svc.PutSession(rec)
	r.ready()
	cur := r.view().ActiveSessionID
	r.store.SwitchSession("ghost")
	if v := r.view(); v.ActiveSessionID != cur || v.LoadError == "" {
		t.Fatalf("会话不存在应报错：%+v", v)
	}
	r.store.SwitchSession("bad")
	if v := r.view(); v.ActiveSessionID != cur || v.LoadError == "" {
		t.Fatalf("损坏的会话不能切进去：%+v", v)
	}
	if r.prefs.active() != cur {
		t.Fatalf("失败不能改记住的会话：%q", r.prefs.active())
	}
}

// 快速连点切换：第一次在「保存当前会话」处卡住时又发起第二次，第一次醒来后必须丢弃，最终停在第二个会话。
func TestSwitchSessionStaleGenerationIsDropped(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.putAsk("first", done("m1", "q1", "a1"))
	r.putAsk("second", done("m2", "q2", "a2"))
	r.ready()
	r.store.Ask("dirty") // 让当前会话有待保存的内容
	cur := r.view().ActiveSessionID
	r.dirty()
	release := r.svc.HoldSave()
	waitFirst := goAsync(func() { r.store.SwitchSession("first") })
	waitFor(t, "第一次切换卡在保存", func() bool { return len(r.svc.Saves()) >= 3 })
	waitSecond := goAsync(func() { r.store.SwitchSession("second") })
	// 第二次开始后代次已前进；放行保存
	waitFor(t, "代次前进", func() bool { r.store.mu.Lock(); defer r.store.mu.Unlock(); return r.store.generation >= 2 })
	release()
	waitFirst()
	waitSecond()
	v := r.view()
	if v.ActiveSessionID != "second" || v.Messages[0].Question != "q2" {
		t.Fatalf("最终应停在第二个会话：%+v (当前 %s)", v, cur)
	}
	if slices.Contains(r.svc.Gets(), "first") {
		t.Fatalf("过时的切换不该读到第一个会话：%v", r.svc.Gets())
	}
}

func TestPersistFailureThenRetry(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.store.Ask("hello")
	r.dirty()
	r.svc.FailSaves(1, errBoom)
	if r.store.Persist() {
		t.Fatal("保存失败应返回 false")
	}
	v := r.view()
	if v.SaveError != "boom" || len(v.Messages) != 1 {
		t.Fatalf("失败后对话仍在内存里，并提示未保存：%+v", v)
	}
	if !r.store.Persist() {
		t.Fatal("重试应成功")
	}
	if v := r.view(); v.SaveError != "" {
		t.Fatalf("成功后清掉保存错误：%+v", v)
	}
	if !r.store.Persist() {
		t.Fatal("内容没变，再保存应直接成功")
	}
	saves := r.svc.Saves()
	if last := saves[len(saves)-1]; last.ID != r.view().ActiveSessionID || last.Title != "hello" {
		t.Fatalf("保存内容不对：%+v", last)
	}
}

func TestPersistSkipsEmptySessionAndMovesSavedToFront(t *testing.T) {
	r := newRig(t)
	r.putAsk("old", done("m", "older", "a"))
	r.ready()
	if !r.store.Persist() || len(r.svc.Saves()) != 0 {
		t.Fatal("空会话不进数据库")
	}
	r.store.Ask("fresh")
	v := r.view()
	if len(v.Sessions) != 2 || v.Sessions[0].ID != v.ActiveSessionID || v.Sessions[1].ID != "old" {
		t.Fatalf("保存后会话列表最近在前：%+v", v.Sessions)
	}
	if r.savedEvents() == 0 {
		t.Fatal("保存成功后应通知历史刷新")
	}
}

func TestSessionTitleIsFirstQuestionTrimmedTo30UTF16Units(t *testing.T) {
	r := newRig(t)
	r.ready()
	long := strings.Repeat("字", 40)
	r.store.Ask("  " + long + "  ")
	saves := r.svc.Saves()
	if got := saves[0].Title; got != strings.Repeat("字", 30) {
		t.Fatalf("标题取前 30 个码元：%q", got)
	}
	if got := buildSessionTitle(nil); got != locales.AskUntitledSession() {
		t.Fatalf("没有问题用「新对话」：%q", got)
	}
	// 代理对：30 个码元里放不下第 15 个 emoji 的一半
	if got := buildSessionTitle([]services.AskMessage{{Question: strings.Repeat("😀", 20)}}); got != strings.Repeat("😀", 15) {
		t.Fatalf("代理对不能被切断：%q", got)
	}
}

func TestNewSessionReusesEmptyAndCreatesAfterContent(t *testing.T) {
	r := newRig(t)
	r.ready()
	first := r.view().ActiveSessionID
	r.store.NewSession()
	if got := r.view().ActiveSessionID; got != first {
		t.Fatalf("当前会话为空时复用：%s → %s", first, got)
	}
	r.store.Ask("q")
	r.store.NewSession()
	v := r.view()
	if v.ActiveSessionID == first || len(v.Messages) != 0 || r.prefs.active() != v.ActiveSessionID {
		t.Fatalf("有内容时开新会话：%+v", v)
	}
	if _, ok := r.svc.Session(first); !ok {
		t.Fatal("旧会话应已保存")
	}
}

func TestNewSessionBlockedBySaveFailure(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.store.Ask("q")
	cur := r.view().ActiveSessionID
	r.dirty()
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	r.store.NewSession()
	if v := r.view(); v.ActiveSessionID != cur || len(v.Messages) != 1 || v.SaveError == "" {
		t.Fatalf("保存失败不能开新会话把对话丢掉：%+v", v)
	}
}

func TestNewSessionFromArticleSession(t *testing.T) {
	r := newRig(t)
	r.putArticle("a1", "item-1", "T", nil, articleDone("m", "q", "a"))
	_ = r.prefs.Set(PrefActiveSession, "a1")
	r.ready()
	r.store.NewSession()
	v := r.view()
	if v.ArticleSession != nil || v.ActiveSessionID == "a1" || v.ActiveSessionID == "" {
		t.Fatalf("从本文会话新建应回到全局空会话：%+v", v)
	}
}

func TestDeleteSessionActiveStartsFreshAndRemoves(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.store.Ask("q")
	id := r.view().ActiveSessionID
	r.store.DeleteSession(id)
	v := r.view()
	if v.ActiveSessionID == id || v.ActiveSessionID == "" || len(v.Messages) != 0 || len(v.Sessions) != 0 {
		t.Fatalf("删除当前会话后换成新会话：%+v", v)
	}
	if _, ok := r.svc.Session(id); ok {
		t.Fatal("库里应已删除")
	}
	if r.prefs.active() != v.ActiveSessionID {
		t.Fatal("新会话应被记住")
	}
}

func TestDeleteSessionFailureKeepsEverythingAndToasts(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m", "q", "a"))
	r.ready()
	r.svc.Fail(servicestest.AskOpDelete, errBoom)
	r.store.DeleteSession("s1")
	if v := r.view(); len(v.Sessions) != 1 {
		t.Fatalf("删除失败列表不变：%+v", v.Sessions)
	}
	r.wantToast(locales.CommonOperationFailed(locales.AskDeleteSession()), "boom")
}

func TestDeleteSessionSavesRunningContentFirstAndAbortsOnSaveFailure(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.store.Ask("q")
	id := r.view().ActiveSessionID
	r.dirty()
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	r.store.DeleteSession(id)
	if _, ok := r.svc.Session(id); !ok || len(r.svc.Deletes()) != 0 {
		t.Fatal("保存失败时不删：免得内容丢了又没删掉")
	}
}

func TestDeleteOtherSessionKeepsActive(t *testing.T) {
	r := newRig(t)
	r.putAsk("s1", done("m", "q", "a"))
	r.ready()
	cur := r.view().ActiveSessionID
	r.store.DeleteSession("s1")
	v := r.view()
	if v.ActiveSessionID != cur || len(v.Sessions) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestDeleteArticleSessionStopsAndForgets(t *testing.T) {
	r := newRig(t)
	r.svc.PutItemTitle("item-1", "Title")
	r.article().Open(target("item-1"), "")
	r.article().Ask("q", target("item-1"), nil)
	id := r.article().SessionID()
	r.store.DeleteSession(id)
	if _, ok := r.svc.Session(id); ok {
		t.Fatal("本文会话应被删除")
	}
	if v := r.artView(); v.SessionID != "" || v.ItemID != "" || len(v.Messages) != 0 {
		t.Fatalf("本文问答里对应的状态应清掉：%+v", v)
	}
}

func TestRememberFailureOnlyLogs(t *testing.T) {
	r := newRig(t)
	r.prefs.failSet = true
	r.ready()
	if v := r.view(); v.ActiveSessionID == "" || v.LoadError != "" {
		t.Fatalf("偏好不可用只降级为不记忆：%+v", v)
	}
	found := false
	for _, e := range r.log.entries() {
		found = found || strings.HasPrefix(e, "ask.rememberSession|")
	}
	if !found {
		t.Fatalf("应留痕：%v", r.log.entries())
	}
	if len(r.toasts()) != 0 {
		t.Fatal("后台失败不打扰用户")
	}
}

func TestRefreshSessions(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.putAsk("late", done("m", "q", "a"))
	r.store.RefreshSessions()
	if v := r.view(); len(v.Sessions) != 1 || v.Sessions[0].ID != "late" {
		t.Fatalf("%+v", v.Sessions)
	}
	r.svc.Fail(servicestest.AskOpList, errBoom)
	r.store.RefreshSessions()
	if v := r.view(); len(v.Sessions) != 1 {
		t.Fatal("刷新失败保留旧列表")
	}
	if len(r.toasts()) != 0 {
		t.Fatal("后台刷新失败不弹提示")
	}
}

func TestAdoptAndSyncArticleSession(t *testing.T) {
	r := newRig(t)
	r.ready()
	web := true
	meta := services.AskSessionMeta{ID: "a1", Scope: services.AskScopeArticle, ItemID: "item-1", WebEnabled: &web}
	r.store.AdoptArticleSession(meta)
	v := r.view()
	if v.ActiveSessionID != "a1" || v.ArticleSession == nil || len(v.Messages) != 0 || r.prefs.active() != "a1" {
		t.Fatalf("%+v", v)
	}
	r.svc.PutItemTitle("item-1", "T")
	r.article().Open(target("item-1"), "")
	r.article().SetWeb(false)
	r.store.SyncArticleSession(meta, "a2")
	v = r.view()
	if v.ActiveSessionID != "a2" || v.ArticleSession.WebEnabledOrDefault() || v.ArticleSession.Target == nil {
		t.Fatalf("应带上本文问答此刻的目标与联网开关：%+v", v.ArticleSession)
	}
}

func TestOpenArticle(t *testing.T) {
	r := newRig(t)
	r.svc.PutItemTitle("live", "Live")
	r.svc.PutItem("trashed", services.AskItem{Title: "Gone", Deleted: true})
	if !r.store.OpenArticle("live") || !slices.Equal(r.openedArticles(), []string{"live"}) {
		t.Fatalf("存在的文章应交给 OnOpenArticle：%v", r.openedArticles())
	}
	for _, id := range []string{"trashed", "never"} {
		if r.store.OpenArticle(id) {
			t.Fatalf("%s 应打开失败", id)
		}
		r.wantToast(locales.ArticleAskOpenFailed(), "文章不存在或已删除，请先恢复文章")
	}
	r.svc.Fail(servicestest.AskOpItemInfo, errBoom)
	if r.store.OpenArticle("live") {
		t.Fatal("读文章失败应打开失败")
	}
	r.wantToast(locales.ArticleAskOpenFailed(), "boom")
	if len(r.openedArticles()) != 1 {
		t.Fatal("失败不该触发打开")
	}
}

func TestClearEvidenceSanitizesLoadedAndLaterSessions(t *testing.T) {
	r := newRig(t)
	withEvidence := done("m1", "q1", "answer text")
	withEvidence.Sources = []services.AskSource{source(1, "item-1", "fp1"), source(2, "item-2", "fp2")}
	withEvidence.EvidenceSources = slices.Clone(withEvidence.Sources)
	r.putAsk("s1", withEvidence)
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.ready()
	r.store.ClearEvidenceForItems([]string{"item-1"})
	m := r.view().Messages[0]
	if !m.Sources[0].Cleared || m.Sources[0].Title != "cleared" || m.Sources[1].Cleared || m.Answer != "answer text" {
		t.Fatalf("已清除的来源脱敏，回答原样：%+v", m)
	}
	if !m.EvidenceSources[0].Cleared {
		t.Fatalf("证据快照列表也要脱敏：%+v", m.EvidenceSources)
	}
	// 之后再读入的会话同样按清除记录脱敏
	r.putAsk("s2", withEvidence)
	r.store.SwitchSession("s2")
	if m := r.view().Messages[0]; !m.Sources[0].Cleared {
		t.Fatalf("读入后的会话也要脱敏：%+v", m)
	}
	r.store.ClearEvidenceForItems(nil) // 空列表不触发
}

func TestSanitizeFailureKeepsMessagesAndLogs(t *testing.T) {
	r := newRig(t)
	m := done("m1", "q1", "a")
	m.Sources = []services.AskSource{source(1, "item-1", "fp")}
	r.putAsk("s1", m)
	_ = r.prefs.Set(PrefActiveSession, "s1")
	r.ready()
	r.svc.Fail(servicestest.AskOpSanitize, errors.New("sanitize down"))
	r.store.ClearEvidenceForItems([]string{"item-1"})
	if got := r.view().Messages[0]; got.Sources[0].Cleared {
		t.Fatalf("脱敏失败保持原样：%+v", got)
	}
	found := false
	for _, e := range r.log.entries() {
		found = found || strings.HasPrefix(e, "ask.sanitize|")
	}
	if !found {
		t.Fatalf("应留痕：%v", r.log.entries())
	}
}

func TestSaveQueueRunsSameIDInOrderAndOthersIndependently(t *testing.T) {
	q := &saveQueue{tails: map[string]chan struct{}{}}
	var order []int
	var mu = make(chan struct{}, 1)
	record := func(n int) { mu <- struct{}{}; order = append(order, n); <-mu }
	gate := make(chan struct{})
	first := goAsync(func() { q.do("a", func() { <-gate; record(1) }) })
	waitFor(t, "第一个任务入队", func() bool { q.mu.Lock(); defer q.mu.Unlock(); return q.tails["a"] != nil })
	q.mu.Lock()
	firstTail := q.tails["a"]
	q.mu.Unlock()
	second := goAsync(func() { q.do("a", func() { record(2) }) })
	waitFor(t, "第二个任务入队", func() bool { q.mu.Lock(); defer q.mu.Unlock(); return q.tails["a"] != firstTail })
	time.Sleep(20 * time.Millisecond) // 给「没排队就直接执行」的错误实现留出时间暴露
	other := goAsync(func() { q.do("b", func() { record(9) }) })
	other() // 别的 ID 不受影响
	if len(order) != 1 || order[0] != 9 {
		t.Fatalf("别的 ID 应独立执行：%v", order)
	}
	close(gate)
	first()
	second()
	if !slices.Equal(order, []int{9, 1, 2}) {
		t.Fatalf("同一 ID 按提交顺序执行：%v", order)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tails) != 0 {
		t.Fatalf("队列应清理干净：%v", q.tails)
	}
}

func TestSaveQueuePanicDoesNotBlockLater(t *testing.T) {
	q := &saveQueue{tails: map[string]chan struct{}{}}
	func() {
		defer func() { _ = recover() }()
		q.do("a", func() { panic("x") })
	}()
	ran := false
	q.do("a", func() { ran = true })
	if !ran {
		t.Fatal("前一个任务出错不影响后一个")
	}
}
