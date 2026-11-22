package ask

import (
	"errors"
	"slices"
	"testing"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应 ArticleConversation.tsx 的提问框：草稿与选段、联网搜索缺失提示、发送流程的各个分支。

func quoteOf(view, selection string) *aiflow.ArticleTarget {
	return &aiflow.ArticleTarget{ItemID: "item-1", View: view, Selection: &selection, Floor: func() *int64 { f := int64(3); return &f }()}
}

func TestDraftKey(t *testing.T) {
	if DraftKey("i", "") != "i:reader" || DraftKey("i", "s1") != "i:s1" {
		t.Fatal("阅读器侧栏与每个会话各有一份草稿")
	}
}

func TestDraftAndQuoteAreIndependent(t *testing.T) {
	r := newRig(t)
	a := r.article()
	key := DraftKey("item-1", "")
	a.SetDraft(key, "text")
	a.SetQuote(key, quoteOf("body", "sel"))
	a.SetDraft(key, "text 2")
	if d := a.Draft(key); d.Text != "text 2" || d.Quote == nil || *d.Quote.Selection != "sel" {
		t.Fatalf("改文本不丢选段：%+v", d)
	}
	a.SetQuote(key, nil)
	if d := a.Draft(key); d.Text != "text 2" || d.Quote != nil {
		t.Fatalf("清选段不丢文本：%+v", d)
	}
	if d := a.Draft(DraftKey("item-2", "")); d.Text != "" || d.Quote != nil {
		t.Fatal("别的键互不影响")
	}
}

func TestAcceptSelectionNeedsSelection(t *testing.T) {
	r := newRig(t)
	a := r.article()
	key := DraftKey("item-1", "")
	a.AcceptSelection(key, target("item-1"), "explain")
	if d := a.Draft(key); d.Text != "" {
		t.Fatalf("没有选段不处理：%+v", d)
	}
	tg := target("item-1")
	tg.Selection = str("chosen")
	a.AcceptSelection(key, tg, "explain this")
	d := a.Draft(key)
	if d.Text != "explain this" || d.Quote == nil || *d.Quote.Selection != "chosen" {
		t.Fatalf("%+v", d)
	}
	*tg.Selection = "mutated"
	if *a.Draft(key).Quote.Selection != "chosen" {
		t.Fatal("引用不与调用方共享指针")
	}
}

func TestSyncSourceClearsQuoteOnlyWhenSourceChanges(t *testing.T) {
	r := newRig(t)
	a := r.article()
	key := DraftKey("item-1", "")
	tg := target("item-1")
	a.SyncSource(key, tg) // 首次只记录
	a.SetQuote(key, quoteOf("body", "sel"))
	a.SyncSource(key, tg)
	if a.Draft(key).Quote == nil {
		t.Fatal("来源没变不清选段")
	}
	tg.VersionID = str("v2")
	a.SyncSource(key, tg)
	if a.Draft(key).Quote != nil {
		t.Fatal("版本变了应清掉选段")
	}
	a.SetQuote(key, quoteOf("body", "sel"))
	tg.View = "summary"
	a.SyncSource(key, tg)
	if a.Draft(key).Quote != nil {
		t.Fatal("视图变了应清掉选段")
	}
}

func TestSendIgnoresEmptyDraft(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "   ")
	if r.article().Send(key, target("item-1"), SendOptions{}) || len(r.svc.ArticleCalls()) != 0 {
		t.Fatal("空草稿不发送")
	}
}

func TestSendSuccessClearsDraftAndQuote(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "my question")
	r.article().SetQuote(key, quoteOf("body", "quoted"))
	submitted := 0
	ok := r.article().Send(key, target("item-1"), SendOptions{OnSubmitted: func() { submitted++ }})
	if !ok || submitted != 1 {
		t.Fatalf("ok=%v submitted=%d", ok, submitted)
	}
	if d := r.article().Draft(key); d.Text != "" || d.Quote != nil {
		t.Fatalf("发出后清空草稿与选段：%+v", d)
	}
	calls := r.svc.ArticleCalls()
	if len(calls) != 1 || calls[0].Question != "my question" || calls[0].Target.Selection == nil || *calls[0].Target.Selection != "quoted" || calls[0].Target.Floor == nil || *calls[0].Target.Floor != 3 {
		t.Fatalf("选段与楼层随问题发出：%+v", calls)
	}
	if r.article().Submitting() {
		t.Fatal("结束后不再是提交中")
	}
}

func TestSendWithoutQuoteDropsSelectionButKeepsFloor(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "q")
	tg := target("item-1")
	tg.Selection = str("stale selection")
	r.article().Send(key, tg, SendOptions{})
	if got := r.svc.ArticleCalls()[0].Target; got.Selection != nil {
		t.Fatalf("没有引用时不带选段：%+v", got)
	}
}

func TestSendNotConfiguredToastsAndOpensSettings(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.svc.SetQAConfigured(false)
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "q")
	if r.article().Send(key, target("item-1"), SendOptions{}) {
		t.Fatal("没有模型不能发送")
	}
	r.wantToast(locales.AskNotConfigured(), "")
	if r.settingsOpened() != 1 || r.article().Draft(key).Text != "q" || len(r.svc.ArticleCalls()) != 0 {
		t.Fatal("应跳到设置，草稿保留，不发起问答")
	}
}

func TestSendMissingSearchKeepsDraftAndOfflineResends(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.svc.SetWebSearchConfigured(false)
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "q")
	if r.article().Send(key, target("item-1"), SendOptions{}) {
		t.Fatal("联网开启但搜索没配置，先提示")
	}
	if !r.article().MissingSearch() || r.article().Draft(key).Text != "q" || len(r.svc.ArticleCalls()) != 0 {
		t.Fatal("应显示提示并保留草稿")
	}
	r.article().DismissMissingSearch()
	if r.article().MissingSearch() {
		t.Fatal("收起提示")
	}
	r.article().Send(key, target("item-1"), SendOptions{})
	if !r.article().Send(key, target("item-1"), SendOptions{Offline: true}) {
		t.Fatal("关闭联网后发送应成功")
	}
	v := r.artView()
	calls := r.svc.ArticleCalls()
	if v.WebEnabled || len(calls) != 1 || calls[0].WebEnabled || r.article().MissingSearch() {
		t.Fatalf("关联网、发出、提示收起：web=%v calls=%+v", v.WebEnabled, calls)
	}
}

func TestSendWebOffSkipsSearchCheck(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	r.article().SetWeb(false)
	r.svc.SetWebSearchConfigured(false)
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "q")
	if !r.article().Send(key, target("item-1"), SendOptions{}) {
		t.Fatal("联网关闭时不检查搜索配置")
	}
}

func TestSendSetupFailuresToastWithReason(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig) SendOptions
		want  string
	}{
		{"读模型配置失败", func(r *rig) SendOptions { r.svc.Fail(servicestest.AskOpQAConfigured, errBoom); return SendOptions{} }, "boom"},
		{"读搜索配置失败", func(r *rig) SendOptions { r.svc.Fail(servicestest.AskOpWebConfigured, errBoom); return SendOptions{} }, "boom"},
		{"取目标失败", func(r *rig) SendOptions {
			return SendOptions{ResolveTarget: func() (aiflow.ArticleTarget, error) { return aiflow.ArticleTarget{}, errors.New("reader gone") }}
		}, "reader gone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.openArticle("item-1")
			key := DraftKey("item-1", "")
			r.article().SetDraft(key, "q")
			opts := tc.setup(r)
			if r.article().Send(key, target("item-1"), opts) {
				t.Fatal("应失败")
			}
			r.wantToast(locales.ArticleAskSendFailed(), tc.want)
			if r.article().Draft(key).Text != "q" || len(r.svc.ArticleCalls()) != 0 {
				t.Fatal("失败时草稿保留，不发起问答")
			}
			if r.article().Submitting() {
				t.Fatal("失败后要结束提交中状态")
			}
		})
	}
}

func TestSendQuoteFromOtherSourceIsRejected(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "q")
	r.article().SetQuote(key, quoteOf("summary", "sel")) // 引用来自「讨论总结」，阅读器此刻在「正文」
	if r.article().Send(key, target("item-1"), SendOptions{}) {
		t.Fatal("引用与此刻来源不一致应拒绝")
	}
	r.wantToast(locales.ArticleAskSendFailed(), "阅读来源已切换，请重新选择段落")
	if d := r.article().Draft(key); d.Text != "q" || d.Quote == nil {
		t.Fatalf("拒绝时草稿与引用都保留：%+v", d)
	}
}

func TestSendUsesResolvedTarget(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "q")
	resolved := aiflow.ArticleTarget{ItemID: "item-1", View: "summary", VersionID: str("v9")}
	r.article().Send(key, target("item-1"), SendOptions{ResolveTarget: func() (aiflow.ArticleTarget, error) { return resolved, nil }})
	if got := r.svc.ArticleCalls()[0].Target; got.View != "summary" || got.VersionID == nil || *got.VersionID != "v9" {
		t.Fatalf("应以阅读器此刻的目标发出：%+v", got)
	}
}

// 没能发出（没有产生任何一轮）就把问题放回草稿，不丢用户写的字；已经产生了一轮（失败显示在那一轮里）则不放回。
func TestSendRestoresDraftOnlyWhenNothingWasSent(t *testing.T) {
	r := newRig(t)
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "lost?")
	// 文章还没打开：Ask 直接拒绝，没有产生轮次
	if r.article().Send(key, target("item-1"), SendOptions{}) {
		t.Fatal("没打开文章不能发")
	}
	if got := r.article().Draft(key).Text; got != "lost?" {
		t.Fatalf("应放回草稿：%q", got)
	}
	r.openArticle("item-1")
	r.svc.QueueArticle(servicestest.ArticleScript{Err: errBoom})
	if r.article().Send(key, target("item-1"), SendOptions{}) {
		t.Fatal("问答失败返回 false")
	}
	if got := r.article().Draft(key).Text; got != "" {
		t.Fatalf("失败已显示在那一轮里，不放回草稿：%q", got)
	}
}

func TestSendIgnoredWhileSubmitting(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "first")
	gate := make(chan struct{})
	r.svc.QueueArticle(servicestest.ArticleScript{Gate: gate})
	wait := goAsync(func() { r.article().Send(key, target("item-1"), SendOptions{}) })
	waitFor(t, "提交中", func() bool { return r.article().Submitting() && len(r.svc.ArticleCalls()) == 1 })
	r.article().SetDraft(key, "second")
	if r.article().Send(key, target("item-1"), SendOptions{}) {
		t.Fatal("提交中不接受再次发送")
	}
	close(gate)
	wait()
	if len(r.svc.ArticleCalls()) != 1 {
		t.Fatal("只发出一次")
	}
}

func TestOpenFromHistoryNewConversationAndClose(t *testing.T) {
	r := newRig(t)
	r.putArticle("a1", "item-1", "T", nil, articleDone("m1", "q1", "a1"))
	r.putArticle("a2", "item-1", "T", nil, articleDone("m2", "q2", "a2"))
	r.svc.PutItemTitle("item-1", "T")
	r.article().Open(target("item-1"), "")
	if !r.article().OpenFromHistory(target("item-1"), "a1") || r.artView().SessionID != "a1" {
		t.Fatal("从历史打开应成功")
	}
	r.svc.Fail(servicestest.AskOpGet, errBoom)
	if r.article().OpenFromHistory(target("item-1"), "a2") {
		t.Fatal("读取失败不算打开成功")
	}
	if r.artView().LoadError == "" {
		t.Fatal("应是错误态")
	}
	r.svc.Fail(servicestest.AskOpGet, nil)

	key := DraftKey("item-1", "")
	r.article().SetDraft(key, "half")
	r.article().SetQuote(key, quoteOf("body", "sel"))
	r.article().Open(target("item-1"), "a1")
	before := r.article().SessionID()
	id := r.article().NewConversation(key, target("item-1"))
	if id == "" || id == before || r.article().Draft(key).Text != "" || r.article().Draft(key).Quote != nil {
		t.Fatalf("新对话清草稿与选段并返回新 ID：%q", id)
	}
	// 保存当前对话失败：开不成，返回空串
	r.article().Ask("q", target("item-1"), nil)
	r.dirtyArticle()
	r.svc.Fail(servicestest.AskOpSave, errBoom)
	if got := r.article().NewConversation(key, target("item-1")); got != "" {
		t.Fatalf("开不成应返回空串：%q", got)
	}
	if r.article().Close() {
		t.Fatal("保存失败时不算收起")
	}
	r.svc.Fail(servicestest.AskOpSave, nil)
	if !r.article().Close() {
		t.Fatal("保存成功才收起")
	}
}

func TestSendRecordsHistoryOrder(t *testing.T) {
	r := newRig(t)
	r.openArticle("item-1")
	key := DraftKey("item-1", "")
	for _, q := range []string{"one", "two"} {
		r.article().SetDraft(key, q)
		r.article().Send(key, target("item-1"), SendOptions{})
	}
	var asked []string
	for _, c := range r.svc.ArticleCalls() {
		asked = append(asked, c.Question)
	}
	if !slices.Equal(asked, []string{"one", "two"}) {
		t.Fatalf("%v", asked)
	}
}
