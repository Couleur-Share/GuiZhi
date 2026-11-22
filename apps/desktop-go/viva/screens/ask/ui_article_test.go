package ask

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件是围绕本文对话面板的 L4 测试：ArticleConversation、ArticleHistoryList、ArticleMessageCard 与全局问答里的
// GlobalArticleConversation。面板在无头窗口里单独画（模块隐藏），读写真实的 ArticleStore 与服务替身。

// newArticleRig 打开只有围绕本文面板的窗口：item-1 的阅读器侧栏，带收起钮。
func newArticleRig(t *testing.T, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	first := func(r *uiRig) {
		r.hidden = true
		r.svc.PutItemTitle("item-1", "Title of item-1")
		r.articleSpec = &ArticleSpec{Target: target("item-1"), OnClose: func() {}}
	}
	return newUIRig(t, append([]func(*uiRig){first}, mutate...)...)
}

// putArticleSession 摆一条本文会话到库里。
func putArticleSession(t *testing.T, svc *servicestest.AskFake, id, item, articleTitle string, msgs ...aiflow.ArticleMessage) {
	t.Helper()
	body, err := marshalArticleMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	tg := target(item)
	rec := services.AskSessionRecord{MessagesJSON: body}
	rec.ID, rec.Title, rec.Scope, rec.ItemID, rec.ArticleTitle, rec.Target = id, buildArticleTitle(msgs), services.AskScopeArticle, item, articleTitle, &tg
	svc.PutSession(rec)
}

func withArticleSession(id string, msgs ...aiflow.ArticleMessage) func(*uiRig) {
	return func(r *uiRig) { putArticleSession(r.t, r.svc, id, "item-1", "Title of item-1", msgs...) }
}

func (r *uiRig) draftKey() string { return DraftKey("item-1", r.articleSpec.SessionID) }

func (r *uiRig) focusArticleInput() {
	r.t.Helper()
	r.click(gfx.RoleTextField, locales.ArticleAskQuestion())
	if !r.screen.art.in.focus {
		r.t.Fatal("点击后输入框应取得焦点")
	}
}

func (r *uiRig) articleView() ArticleView { return r.store.Article().View() }

func richTurn() aiflow.ArticleMessage {
	m := articleDone("m1", "what is it?", "The answer is here.")
	m.Sources = []aiflow.ArticleSource{{Ordinal: 1, Kind: "article", Title: "Src A", Text: "excerpt text"}}
	m.Model, m.WebStatus, m.Warnings = str("gpt-x"), "partial", []string{"be careful"}
	truncated := true
	m.Truncated = &truncated
	return m
}

func TestUIArticlePanelShowsHeaderEmptyStateAndComposer(t *testing.T) {
	r := newArticleRig(t)
	if !r.has(gfx.RoleGroup, locales.ArticleAskTitle()) {
		t.Fatalf("面板应是带名字的区域：%q", r.session.Texts())
	}
	for _, text := range []string{locales.ArticleAskTitle(), "Title of item-1"} {
		if !r.hasText(text) {
			t.Fatalf("头部缺「%s」：%q", text, r.session.Texts())
		}
	}
	for _, name := range []string{locales.ArticleAskNew(), locales.ArticleAskHistory(), locales.ArticleAskClose(), locales.AskSend()} {
		if !r.hasButton(name) {
			t.Fatalf("缺按钮「%s」：%q", name, r.session.Texts())
		}
	}
	if !r.contains(locales.ArticleAskEmpty()) {
		t.Fatalf("没有消息时应给出空态说明：%q", r.session.Texts())
	}
	if n, ok := r.node(gfx.RoleSwitch, "\u2611 "+locales.ArticleAskWeb()); !ok || !n.Checked {
		t.Fatalf("联网查证默认开启：%q", r.session.Texts())
	}
	if n, ok := r.node(gfx.RoleButton, locales.AskSend()); !ok || !n.Disabled {
		t.Fatal("输入为空时发送钮应禁用")
	}
	if !r.has(gfx.RoleTextField, locales.ArticleAskQuestion()) {
		t.Fatal("应有输入框")
	}
	if r.hasButton(locales.ArticleAskClose()) != true {
		t.Fatal("有 OnClose 才有收起钮")
	}
}

func TestUIArticlePanelWithoutOnCloseHasNoCloseButton(t *testing.T) {
	r := newArticleRig(t, func(r *uiRig) { r.articleSpec.OnClose = nil })
	if r.hasButton(locales.ArticleAskClose()) {
		t.Fatal("没有 OnClose 不该有收起钮")
	}
}

func TestUIArticlePanelLoadingAndLoadErrorAreNotTheEmptyState(t *testing.T) {
	r := newArticleRig(t, func(r *uiRig) { r.svc.Fail(servicestest.AskOpList, errBoom) })
	if !r.contains(errBoom.Error()) || r.contains(locales.ArticleAskEmpty()) {
		t.Fatalf("读取失败应渲染错误态，不是空态：%q", r.session.Texts())
	}
	if n, ok := r.node(gfx.RoleButton, locales.AskSend()); !ok || !n.Disabled {
		t.Fatal("读取失败时不接受提问")
	}
	r.svc.Fail(servicestest.AskOpList, nil)
	r.clickButton(locales.CommonRetry())
	if !r.contains(locales.ArticleAskEmpty()) || r.contains(errBoom.Error()) {
		t.Fatalf("重试成功后应进入空态：%q", r.session.Texts())
	}
}

func TestUIArticleMessageCardShowsEveryPart(t *testing.T) {
	failed := articleDone("m2", "second?", "")
	failed.Status, failed.Error = services.AskStatusError, str("boom reason")
	r := newArticleRig(t, withArticleSession("s1", richTurn(), failed))
	for _, text := range []string{"what is it?", "be careful", locales.ArticleAskUnverified(), locales.AskAnswerTruncated(), "gpt-x", "second?", "boom reason"} {
		if !r.hasText(text) {
			t.Fatalf("缺「%s」：%q", text, r.session.Texts())
		}
	}
	if !r.contains("The answer is here.") {
		t.Fatalf("应显示回答正文：%q", r.session.Texts())
	}
	if !r.hasButton("[1] Src A") {
		t.Fatalf("应有来源 chip：%q", r.session.Texts())
	}
	if r.count(gfx.RoleButton, locales.AskRetryTurn()) != 2 || r.count(gfx.RoleButton, locales.AskCopyAnswer()) != 1 {
		t.Fatalf("每轮都有「重新回答」，只有有回答的一轮有「复制」：%q", r.session.Texts())
	}
}

func TestUIArticleMessageCardRunningShowsStepAndDisablesRetry(t *testing.T) {
	gate := make(chan struct{})
	step := "searching the article"
	r := newArticleRig(t, goroutines, func(r *uiRig) {
		r.svc.QueueArticle(servicestest.ArticleScript{Patches: []aiflow.ArticlePatch{{Step: &step}}, Gate: gate})
	})
	r.store.Article().SetDraft(r.draftKey(), "q")
	r.frames(2)
	r.clickButton(locales.AskSend())
	r.settle("回答进行中", func() bool { return r.articleView().Running && r.hasText(step) })
	if n, ok := r.node(gfx.RoleButton, locales.AskRetryTurn()); !ok || !n.Disabled {
		t.Fatal("回答中「重新回答」应禁用")
	}
	r.clickButton(locales.AskStop())
	close(gate)
	r.settle("回答已停止", func() bool { return !r.articleView().Running })
}

func TestUIArticleMessageCardQuotesTheAskedSelection(t *testing.T) {
	m := articleDone("m1", "q", "a")
	tg := target("item-1")
	tg.Selection = str("the chosen paragraph")
	m.Context = &aiflow.ArticleContext{Target: tg}
	r := newArticleRig(t, withArticleSession("s1", m))
	if !r.hasText("the chosen paragraph") {
		t.Fatalf("应显示这轮问题引用的选段：%q", r.session.Texts())
	}
}

func TestUIArticleSourceChipExpandsExcerptAndLocateReportsFailure(t *testing.T) {
	var found atomic.Bool
	var located []aiflow.ArticleSource
	r := newArticleRig(t, withArticleSession("s1", richTurn()), func(r *uiRig) {
		r.articleSpec.OnLocate = func(src aiflow.ArticleSource, done func(bool)) {
			located = append(located, src)
			done(found.Load())
		}
	})
	if r.has(gfx.RoleGroup, locales.ArticleAskExcerpt()) {
		t.Fatal("没点来源时不该展开摘录")
	}
	r.clickButton("[1] Src A")
	if !r.has(gfx.RoleGroup, locales.ArticleAskExcerpt()) || !r.hasText("excerpt text") {
		t.Fatalf("点来源应展开摘录：%q", r.session.Texts())
	}
	r.clickButton(locales.ArticleAskLocate())
	if len(located) != 1 || located[0].Ordinal != 1 || !r.hasText(locales.ArticleAskLocateFailed()) {
		t.Fatalf("没找到时应亮出失败说明：%d %q", len(located), r.session.Texts())
	}
	found.Store(true)
	r.clickButton(locales.ArticleAskLocate())
	if r.hasText(locales.ArticleAskLocateFailed()) {
		t.Fatal("找到之后应清掉失败说明")
	}
	r.clickButton(locales.CommonClose())
	if r.has(gfx.RoleGroup, locales.ArticleAskExcerpt()) {
		t.Fatal("关闭应收起摘录")
	}
}

func TestUIArticleSourceWithoutSavedTextExplainsAndWithoutLocatorFails(t *testing.T) {
	m := richTurn()
	m.Sources[0].Text = ""
	r := newArticleRig(t, withArticleSession("s1", m))
	r.clickButton("[1] Src A")
	if !r.hasText(locales.ArticleAskExcerptNotSaved()) {
		t.Fatalf("没有保存摘录时应说明：%q", r.session.Texts())
	}
	r.clickButton(locales.ArticleAskLocate())
	if !r.hasText(locales.ArticleAskLocateFailed()) {
		t.Fatal("没有定位入口时按失败处理")
	}
}

func TestUIArticleEvidenceSourceUsesTheSharedExcerptAndHistoricalHint(t *testing.T) {
	m := richTurn()
	m.Sources = []aiflow.ArticleSource{
		{Ordinal: 1, Kind: "article", Title: "Src A", Evidence: &aiflow.EvidenceSnapshot{
			Version: 1, Kind: "article", Title: "Evidence title", Text: "evidence body", CapturedAt: 1700000000000, ReviewStatus: "clear", ReviewReasons: []string{},
		}},
		{Ordinal: 2, Kind: "article", Title: "Src B", Cleared: func() *bool { b := true; return &b }()},
	}
	r := newArticleRig(t, withArticleSession("s1", m))
	r.clickButton("[1] Src A")
	if !r.has(gfx.RoleGroup, locales.AskEvidenceTitle()) || !r.hasText("evidence body") {
		t.Fatalf("有证据快照应用共用的证据摘录：%q", r.session.Texts())
	}
	r.clickButton(locales.AskEvidenceViewCurrent())
	if !r.hasText(locales.ArticleAskLocateFailedEvidence()) {
		t.Fatalf("定位失败时说明历史摘录仍可查看：%q", r.session.Texts())
	}
	r.clickButton(locales.CommonClose())
	r.clickButton("[2] Src B")
	if !r.hasText(locales.AskEvidenceCleared()) || r.hasButton(locales.AskEvidenceViewCurrent()) {
		t.Fatalf("已清除的来源只说明已清除、没有「查看当前版本」：%q", r.session.Texts())
	}
}

func TestUIArticleWebSourceOpensOnlySafeLinks(t *testing.T) {
	var opened []string
	m := richTurn()
	m.Sources = []aiflow.ArticleSource{{Ordinal: 1, Kind: "web", Title: "Web A", Text: "web text", URL: str("https://example.com/a")}}
	r := newArticleRig(t, withArticleSession("s1", m), func(r *uiRig) {
		r.screen.Hooks.OpenURL = func(u string) error { opened = append(opened, u); return nil }
	})
	r.clickButton("[1] Web A")
	r.clickButton(locales.ArticleAskOpenSource())
	if len(opened) != 1 || opened[0] != "https://example.com/a" || r.hasText(locales.ArticleAskLocateFailed()) {
		t.Fatalf("安全的网页来源应交给浏览器：%v", opened)
	}
	for _, bad := range []string{"javascript:alert(1)", "https://user:pw@example.com/", "ftp://example.com/", "https://", "::"} {
		if r.screen.openWebSource(bad) || len(opened) != 1 {
			t.Fatalf("不安全的链接 %q 不该被打开", bad)
		}
	}
	if r.screen.Hooks.OpenURL = nil; r.screen.openWebSource("https://example.com/") {
		t.Fatal("没有外壳钩子时按打不开处理")
	}
}

func TestUIArticleCopyAnswerWritesRenumberedTextAndToastsSuccess(t *testing.T) {
	r := newArticleRig(t, withArticleSession("s1", richTurn()))
	r.clickButton(locales.AskCopyAnswer())
	got := r.copiedTexts()
	if len(got) != 1 || !strings.Contains(got[0], "The answer is here.") {
		t.Fatalf("应复制回答：%q", got)
	}
	last := r.toasts()
	if len(last) == 0 || last[len(last)-1].Kind != kit.ToastSuccess || last[len(last)-1].Message != locales.AskCopied() {
		t.Fatalf("复制成功应弹出成功提示：%+v", last)
	}
}

func TestUIArticleRetryAnswersTheTurnAgain(t *testing.T) {
	r := newArticleRig(t, withArticleSession("s1", articleDone("m1", "why?", "old")))
	r.clickButton(locales.AskRetryTurn())
	if calls := r.svc.ArticleCalls(); len(calls) != 1 || calls[0].Question != "why?" {
		t.Fatalf("应按原问题重答：%+v", calls)
	}
}

func TestUITypingAndEnterSendsTheArticleQuestion(t *testing.T) {
	r := newArticleRig(t)
	r.focusArticleInput()
	r.typeText("hello world")
	if got := r.store.Article().Draft(r.draftKey()).Text; got != "hello world" {
		t.Fatalf("输入应写回 store 里的草稿，得到 %q", got)
	}
	r.keys("enter")
	calls := r.svc.ArticleCalls()
	if len(calls) != 1 || calls[0].Question != "hello world" {
		t.Fatalf("Enter 应发送问题：%+v", calls)
	}
	if got := r.screen.art.in.field.Text(); got != "" {
		t.Fatalf("发送后输入框应清空，得到 %q", got)
	}
	if !r.contains("echo:hello world") {
		t.Fatalf("应显示回答：%q", r.session.Texts())
	}
}

func TestUIArticleShiftEnterAndEmptyEnterNeverSend(t *testing.T) {
	r := newArticleRig(t)
	r.focusArticleInput()
	r.keys("enter")
	if len(r.svc.ArticleCalls()) != 0 || r.screen.art.in.field.Text() != "" {
		t.Fatal("空输入按 Enter 不发送、也不插入换行")
	}
	r.typeText("first")
	r.keysWithShift("enter")
	r.typeText("second")
	if got := r.screen.art.in.field.Text(); got != "first\nsecond" {
		t.Fatalf("Shift+Enter 应换行，得到 %q", got)
	}
	if len(r.svc.ArticleCalls()) != 0 {
		t.Fatal("Shift+Enter 不该发送")
	}
}

func TestUIArticleSendButtonSendsAndDraftSurvivesSwitchingBack(t *testing.T) {
	r := newArticleRig(t)
	key := r.draftKey()
	r.store.Article().SetDraft(key, "half written")
	r.frames(2)
	if got := r.screen.art.in.field.Text(); got != "half written" {
		t.Fatalf("store 里的草稿应进输入框：%q", got)
	}
	r.hidden = true
	r.articleSpec = nil
	r.frames(2) // 面板卸载
	r.articleSpec = &ArticleSpec{Target: target("item-1")}
	r.frames(2)
	if got := r.screen.art.in.field.Text(); got != "half written" {
		t.Fatalf("写了一半的草稿切走再回来还在：%q", got)
	}
	r.clickButton(locales.AskSend())
	if calls := r.svc.ArticleCalls(); len(calls) != 1 || calls[0].Question != "half written" {
		t.Fatalf("点发送应发出草稿：%+v", calls)
	}
}

func TestUIArticleRunningShowsStopLocksWebSwitchAndEnterDoesNotSend(t *testing.T) {
	gate := make(chan struct{})
	r := newArticleRig(t, goroutines, func(r *uiRig) {
		r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate})
	})
	r.store.Article().SetDraft(r.draftKey(), "slow question")
	r.frames(2)
	r.clickButton(locales.AskSend())
	r.settle("回答进行中", func() bool { return r.articleView().Running })
	if !r.hasButton(locales.AskStop()) || r.hasButton(locales.AskSend()) {
		t.Fatalf("回答中按钮应换成停止：%q", r.session.Texts())
	}
	if n, ok := r.node(gfx.RoleSwitch, "\u2611 "+locales.ArticleAskWeb()); !ok || !n.Disabled {
		t.Fatal("回答中联网开关应禁用")
	}
	r.focusArticleInput()
	r.typeText("next")
	r.keys("enter")
	if len(r.svc.ArticleCalls()) != 1 {
		t.Fatal("回答中 Enter 不发送")
	}
	r.clickButton(locales.AskStop())
	close(gate)
	r.settle("回答已停止", func() bool { return !r.articleView().Running })
	if !r.contains(locales.AskStopped()) {
		t.Fatalf("停止后这一轮应标成已停止：%q", r.session.Texts())
	}
}

func TestUIArticleSelectionPromptBecomesAQuoteThatCanBeRemovedAndIsSent(t *testing.T) {
	r := newArticleRig(t, func(r *uiRig) {
		tg := target("item-1")
		tg.Selection, tg.Floor = str("picked text"), new(int64)
		*tg.Floor = 3
		r.articleSpec.Target, r.articleSpec.Prompt, r.articleSpec.PromptKey = tg, "explain this", 1
	})
	if !r.hasText("picked text") || !r.hasButton(locales.ArticleAskRemoveQuote()) {
		t.Fatalf("选段应显示成引用：%q", r.session.Texts())
	}
	if !r.contains(locales.ArticleAskViewBody() + " \u00b7 3") {
		t.Fatalf("引用头应写阅读视图与楼层：%q", r.session.Texts())
	}
	if got := r.screen.art.in.field.Text(); got != "explain this" {
		t.Fatalf("提示语应进草稿：%q", got)
	}
	r.clickButton(locales.AskSend())
	calls := r.svc.ArticleCalls()
	if len(calls) != 1 || calls[0].Target.Selection == nil || *calls[0].Target.Selection != "picked text" {
		t.Fatalf("选段随问题发出：%+v", calls)
	}
	if r.hasButton(locales.ArticleAskRemoveQuote()) {
		t.Fatal("发出后引用应清掉")
	}
}

func TestUIArticleRemoveQuoteAndSourceChangeClearIt(t *testing.T) {
	r := newArticleRig(t, func(r *uiRig) {
		tg := target("item-1")
		tg.Selection = str("picked text")
		r.articleSpec.Target, r.articleSpec.Prompt, r.articleSpec.PromptKey = tg, "p", 1
	})
	r.clickButton(locales.ArticleAskRemoveQuote())
	if r.hasButton(locales.ArticleAskRemoveQuote()) || r.store.Article().Draft(r.draftKey()).Quote != nil {
		t.Fatal("移除选段应清掉引用")
	}
	tg := target("item-1")
	tg.Selection = str("again")
	r.articleSpec.Target, r.articleSpec.PromptKey = tg, 2
	r.frames(2)
	if !r.hasButton(locales.ArticleAskRemoveQuote()) {
		t.Fatalf("新的选段提问应再挂上引用：%q", r.session.Texts())
	}
	r.articleSpec.Target = aiflow.ArticleTarget{ItemID: "item-1", View: "summary"}
	r.frames(2)
	if r.hasButton(locales.ArticleAskRemoveQuote()) {
		t.Fatal("阅读视图变了，旧视图的选段必须清掉")
	}
}

func TestUIArticleWebSwitchAndMissingSearchInterception(t *testing.T) {
	r := newArticleRig(t, func(r *uiRig) { r.svc.SetWebSearchConfigured(false) })
	on, off := "\u2611 "+locales.ArticleAskWeb(), "\u2610 "+locales.ArticleAskWeb()
	r.click(gfx.RoleSwitch, on)
	if !r.has(gfx.RoleSwitch, off) || r.articleView().WebEnabled {
		t.Fatal("点开关应关闭联网")
	}
	r.click(gfx.RoleSwitch, off)
	if !r.articleView().WebEnabled {
		t.Fatal("再点应重新开启")
	}
	r.store.Article().SetDraft(r.draftKey(), "q")
	r.frames(2)
	r.clickButton(locales.AskSend())
	if !r.contains(locales.ArticleAskNoSearch()) || len(r.svc.ArticleCalls()) != 0 {
		t.Fatalf("联网搜索没配置时应拦下并保留问题：%q", r.session.Texts())
	}
	if r.store.Article().Draft(r.draftKey()).Text != "q" {
		t.Fatal("问题应保留")
	}
	r.clickButton(locales.ArticleAskConfigure())
	if r.settingsOpened() != 1 {
		t.Fatal("「前往配置」应打开设置的 AI 分区")
	}
	r.clickButton(locales.ArticleAskSendOffline())
	if calls := r.svc.ArticleCalls(); len(calls) != 1 || r.articleView().WebEnabled {
		t.Fatalf("「关闭联网后发送」应关掉联网再发：%d web=%v", len(calls), r.articleView().WebEnabled)
	}
	if r.contains(locales.ArticleAskNoSearch()) {
		t.Fatal("发出后提示应收起")
	}
}

func TestUIArticleNotConfiguredToastsAndOpensSettings(t *testing.T) {
	r := newArticleRig(t, func(r *uiRig) { r.svc.SetQAConfigured(false) })
	r.store.Article().SetDraft(r.draftKey(), "q")
	r.frames(2)
	r.clickButton(locales.AskSend())
	if r.settingsOpened() != 1 || len(r.svc.ArticleCalls()) != 0 {
		t.Fatal("没有模型应跳到设置，不发起问答")
	}
	if got := r.toasts(); len(got) == 0 || got[len(got)-1].Message != locales.AskNotConfigured() {
		t.Fatalf("应提示尚未配置 AI 服务：%+v", got)
	}
}

func TestUIArticleSendFailureToastsWithReasonAndKeepsTheQuestion(t *testing.T) {
	r := newArticleRig(t, func(r *uiRig) {
		r.articleSpec.ResolveTarget = func() (aiflow.ArticleTarget, error) { return aiflow.ArticleTarget{}, errBoom }
	})
	r.store.Article().SetDraft(r.draftKey(), "q")
	r.frames(2)
	r.clickButton(locales.AskSend())
	got := r.toasts()
	if len(got) == 0 || got[len(got)-1].Message != locales.ArticleAskSendFailed() || got[len(got)-1].Detail != errBoom.Error() {
		t.Fatalf("应有带原因的失败提示：%+v", got)
	}
	if r.store.Article().Draft(r.draftKey()).Text != "q" {
		t.Fatal("发送失败不能丢问题")
	}
}

func TestUIArticleHistoryOpensSessionsAndCollapses(t *testing.T) {
	var changed []string
	r := newArticleRig(t,
		withArticleSession("s1", articleDone("m1", "first question", "a")),
		withArticleSession("s2", articleDone("m2", "second question", "b")),
		func(r *uiRig) { r.articleSpec.OnSessionChange = func(id string) { changed = append(changed, id) } })
	if r.hasButton("first question") {
		t.Fatal("历史默认收起")
	}
	r.clickButton(locales.ArticleAskHistory())
	if n, ok := r.node(gfx.RoleButton, locales.ArticleAskHistory()); !ok || !n.Expanded {
		t.Fatal("「历史」应是展开收起钮")
	}
	if !r.hasButton("first question") || !r.hasButton("second question") {
		t.Fatalf("应列出这篇文章的本文会话：%q", r.session.Texts())
	}
	if qs := r.svc.Queries(); qs[len(qs)-1].Scope != services.AskScopeArticle || qs[len(qs)-1].ItemID != "item-1" {
		t.Fatalf("本文历史按文章筛选：%+v", qs[len(qs)-1])
	}
	r.clickButton("first question")
	if r.articleView().SessionID != "s1" || len(changed) != 1 || changed[0] != "s1" {
		t.Fatalf("应打开会话并通知换了会话：%q %v", r.articleView().SessionID, changed)
	}
	if r.hasButton("first question") || !r.contains("first question") {
		t.Fatalf("打开后历史收起、对话显示出来：%q", r.session.Texts())
	}
}

func TestUIArticleHistoryRenameAndDelete(t *testing.T) {
	r := newArticleRig(t, withArticleSession("s1", articleDone("m1", "first question", "a")))
	r.clickButton(locales.ArticleAskHistory())
	r.clickNthButton(locales.AskRenameSession(), 0)
	r.screen.art.hist.rename.field.SetText("A better title")
	r.frames(2)
	r.clickButton(locales.CommonSave())
	if rec, _ := r.svc.Session("s1"); rec.Title != "A better title" {
		t.Fatalf("应保存新标题：%q", rec.Title)
	}
	if !r.hasButton("A better title") {
		t.Fatalf("改名后列表应重读：%q", r.session.Texts())
	}
	r.clickNthButton(locales.AskDeleteSession(), 0)
	r.clickButton(locales.CommonConfirm())
	if got := r.svc.Deletes(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("确认后应删除会话：%v", got)
	}
	if !r.hasText(locales.ArticleAskHistoryNoMatch()) {
		t.Fatalf("删光后列表应显示无匹配：%q", r.session.Texts())
	}
}

func TestUIArticleHistoryEmptyErrorAndSearch(t *testing.T) {
	r := newArticleRig(t)
	r.svc.Fail(servicestest.AskOpQuery, errBoom)
	r.clickButton(locales.ArticleAskHistory())
	if !r.contains(errBoom.Error()) || r.hasText(locales.ArticleAskHistoryNoMatch()) {
		t.Fatalf("读取失败应显示原因、不当空态：%q", r.session.Texts())
	}
	r.svc.Fail(servicestest.AskOpQuery, nil)
	r.clickNthButton(locales.CommonRetry(), 0)
	if !r.hasText(locales.ArticleAskHistoryNoMatch()) {
		t.Fatalf("重试成功后无会话应显示无匹配：%q", r.session.Texts())
	}
	before := len(r.svc.Queries())
	r.screen.art.hist.search.SetText("needle")
	r.frames(2)
	r.clock.Advance(100 * time.Millisecond)
	if len(r.svc.Queries()) != before {
		t.Fatal("防抖期内不该查询")
	}
	r.clock.Advance(200 * time.Millisecond)
	r.frames(2)
	qs := r.svc.Queries()
	if len(qs) != before+1 || qs[len(qs)-1].Search != "needle" || qs[len(qs)-1].ItemID != "item-1" {
		t.Fatalf("停下后应只查一次、带搜索词与文章：%+v", qs)
	}
	r.clickButton(locales.ArticleAskHistory())
	r.clickButton(locales.ArticleAskHistory())
	if r.screen.art.hist.search.Text() != "" {
		t.Fatal("历史收起再展开应整体清空")
	}
}

func TestUIArticleNewConversationClearsDraftAndNotifies(t *testing.T) {
	var changed []string
	r := newArticleRig(t, withArticleSession("s1", articleDone("m1", "old question", "a")), func(r *uiRig) {
		r.articleSpec.OnSessionChange = func(id string) { changed = append(changed, id) }
	})
	old := r.articleView().SessionID
	r.store.Article().SetDraft(r.draftKey(), "unsent")
	r.frames(2)
	r.clickButton(locales.ArticleAskNew())
	v := r.articleView()
	if v.SessionID == old || len(v.Messages) != 0 || len(changed) != 1 || changed[0] != v.SessionID {
		t.Fatalf("应开新会话并通知：%q→%q %v", old, v.SessionID, changed)
	}
	if r.screen.art.in.field.Text() != "" || r.store.Article().Draft(r.draftKey()).Text != "" {
		t.Fatal("新对话应清掉草稿")
	}
}

func TestUIArticleCloseStopsPersistsThenCallsOnCloseAndSaveFailureBlocksIt(t *testing.T) {
	closed := 0
	r := newArticleRig(t, func(r *uiRig) { r.articleSpec.OnClose = func() { closed++ } })
	r.store.Article().SetDraft(r.draftKey(), "q")
	r.frames(2)
	r.clickButton(locales.AskSend())
	if len(r.svc.ArticleCalls()) != 1 {
		t.Fatal("先问一轮，让对话有内容")
	}
	// 改一下联网开关，让对话变脏；保存失败时收起不能成功，且给出可重试的提示。
	r.svc.FailSaves(2, errBoom)
	r.click(gfx.RoleSwitch, "\u2611 "+locales.ArticleAskWeb())
	if !r.contains(locales.ArticleAskNotSaved()) || !r.hasButton(locales.ArticleAskRetrySave()) {
		t.Fatalf("保存失败应给出提示与重试：%q", r.session.Texts())
	}
	r.clickButton(locales.ArticleAskClose())
	if closed != 0 {
		t.Fatal("保存失败时不能收起")
	}
	r.svc.FailSaves(0, nil)
	r.clickButton(locales.ArticleAskRetrySave())
	if r.contains(locales.ArticleAskNotSaved()) {
		t.Fatal("重试保存成功后提示应消失")
	}
	r.clickButton(locales.ArticleAskClose())
	if closed != 1 {
		t.Fatalf("保存成功后应调用 OnClose，实际 %d 次", closed)
	}
}

func TestUIArticleEscapeClosesOnlyFromInsideThePanel(t *testing.T) {
	closed := 0
	r := newArticleRig(t, func(r *uiRig) { r.articleSpec.OnClose = func() { closed++ } })
	if r.screen.ArticleEscape(nil) || r.screen.ArticleEscape(&r.canvas) {
		t.Fatal("焦点不在面板里时不拦 Esc")
	}
	r.focusArticleInput()
	if !r.screen.ArticleEscape(&r.canvas) {
		t.Fatal("焦点在面板里 Esc 应收起")
	}
	r.frames(2)
	if closed != 1 {
		t.Fatalf("Esc 收起应调用 OnClose，实际 %d 次", closed)
	}
}

// ---- 全局问答里的本文对话 ----

// withGlobalArticle 让首帧恢复到一条本文会话（全局问答工作区里画本文对话）。
func withGlobalArticle(msgs ...aiflow.ArticleMessage) func(*uiRig) {
	return func(r *uiRig) {
		r.svc.PutItemTitle("item-1", "Some article")
		putArticleSession(r.t, r.svc, "a1", "item-1", "Some article", msgs...)
		_ = r.prefs.Set(PrefActiveSession, "a1")
	}
}

func TestUIGlobalArticleConversationReplacesThePlaceholder(t *testing.T) {
	r := newUIRig(t, withGlobalArticle(richTurn()))
	if r.contains(locales.NativeShellNotMigrated(locales.ArticleAskHistoryBadge())) {
		t.Fatal("本文会话不该再是过渡占位")
	}
	if !r.hasButton(locales.ArticleAskBack()) || !r.has(gfx.RoleGroup, locales.ArticleAskTitle()) {
		t.Fatalf("应有「返回文章」与嵌入的本文对话：%q", r.session.Texts())
	}
	if r.has(gfx.RoleTextField, locales.AskPlaceholder()) || !r.has(gfx.RoleTextField, locales.ArticleAskQuestion()) {
		t.Fatal("本文会话用本文输入框，不是全局输入框")
	}
	if r.hasButton(locales.ArticleAskClose()) {
		t.Fatal("全局问答里的本文对话没有收起钮")
	}
	if !r.hasText("what is it?") {
		t.Fatalf("应显示这条会话的问答：%q", r.session.Texts())
	}
}

func TestUIGlobalArticleBackOpensTheArticleOrExplainsWhy(t *testing.T) {
	var opened []string
	r := newUIRig(t, withGlobalArticle(), func(r *uiRig) { r.store.OnOpenArticle = func(id string) { opened = append(opened, id) } })
	r.clickButton(locales.ArticleAskBack())
	if len(opened) != 1 || opened[0] != "item-1" {
		t.Fatalf("应打开会话所属的文章：%v", opened)
	}
	r.svc.PutItem("item-1", services.AskItem{Title: "Some article", Deleted: true})
	r.clickButton(locales.ArticleAskBack())
	got := r.toasts()
	if len(opened) != 1 || len(got) == 0 || got[len(got)-1].Message != locales.ArticleAskOpenFailed() || got[len(got)-1].Detail == "" {
		t.Fatalf("文章在回收站里时应提示并给出原因：%d %+v", len(opened), got)
	}
}

func TestUIGlobalArticleSessionChangesFollowIntoTheGlobalStore(t *testing.T) {
	r := newUIRig(t, withGlobalArticle(articleDone("m1", "first question", "a")), func(r *uiRig) {
		putArticleSession(r.t, r.svc, "a2", "item-1", "Some article", articleDone("m2", "second question", "b"))
	})
	r.clickNthButton(locales.ArticleAskNew(), 1) // 第 0 个是侧栏里全局问答的「新对话」
	id := r.store.View().ActiveSessionID
	if id == "a1" || id == "" || r.store.View().ArticleSession == nil || r.store.View().ArticleSession.ID != id {
		t.Fatalf("新对话应让全局这边换到新会话：%+v", r.store.View())
	}
	r.clickButton(locales.ArticleAskHistory())
	r.clickButton("second question")
	if got := r.store.View().ActiveSessionID; got != "a2" {
		t.Fatalf("从本文历史打开应让全局这边跟过去：%q", got)
	}
	if !r.contains("second question") {
		t.Fatalf("应显示打开的会话：%q", r.session.Texts())
	}
}

func TestUIGlobalArticleLocateOpensTheArticleFirst(t *testing.T) {
	var opened []string
	var located []aiflow.ArticleSource
	r := newUIRig(t, withGlobalArticle(richTurn()), func(r *uiRig) {
		r.store.OnOpenArticle = func(id string) { opened = append(opened, id) }
		r.screen.Hooks.LocateArticle = func(src aiflow.ArticleSource, done func(bool)) {
			located = append(located, src)
			done(true)
		}
	})
	r.clickButton("[1] Src A")
	r.clickButton(locales.ArticleAskLocate())
	if len(opened) != 1 || len(located) != 1 || located[0].Title != "Src A" || r.hasText(locales.ArticleAskLocateFailed()) {
		t.Fatalf("定位应先打开文章再定位：opened=%v located=%d", opened, len(located))
	}
	r.svc.PutItem("item-1", services.AskItem{Title: "Some article", Deleted: true})
	r.clickButton(locales.ArticleAskLocate())
	if len(located) != 1 || !r.hasText(locales.ArticleAskLocateFailed()) {
		t.Fatal("文章打不开时不定位，卡片亮出失败说明")
	}
}

func TestUIGlobalArticleStopsTheAnswerWhenItUnmounts(t *testing.T) {
	gate := make(chan struct{})
	r := newUIRig(t, withGlobalArticle(), goroutines, func(r *uiRig) {
		r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate})
	})
	r.store.Article().SetDraft(DraftKey("item-1", "a1"), "slow")
	r.frames(2)
	r.clickButton(locales.AskSend())
	r.settle("回答进行中", func() bool { return r.articleView().Running })
	r.hidden = true // 切走：本文对话卸载
	r.frames(2)
	close(gate)
	r.settle("卸载后回答被停掉", func() bool { return !r.articleView().Running })
}
