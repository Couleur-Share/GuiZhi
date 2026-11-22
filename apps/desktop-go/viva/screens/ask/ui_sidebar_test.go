package ask

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// rowButton 找名称以 prefix 开头的会话行主按钮（名称是「标题 时间」）。
func (r *uiRig) rowButton(prefix string) (string, bool) {
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && strings.HasPrefix(n.Label, prefix+" ") {
			return n.Label, true
		}
	}
	return "", false
}

func (r *uiRig) clickRow(prefix string) {
	r.t.Helper()
	name, ok := r.rowButton(prefix)
	if !ok {
		r.t.Fatalf("找不到会话行 %q；画面上有 %q", prefix, r.session.Texts())
	}
	r.clickButton(name)
}

func (r *uiRig) hoverRow(prefix string) {
	r.t.Helper()
	name, ok := r.rowButton(prefix)
	if !ok {
		r.t.Fatalf("找不到会话行 %q", prefix)
	}
	r.hover(gfx.RoleButton, name)
}

func TestUISidebarListsSessionsAndSwitchesOnClick(t *testing.T) {
	r := newUIRig(t, withSessions("s1", "s2"))
	for _, id := range []string{"s1", "s2"} {
		if _, ok := r.rowButton("question " + id); !ok {
			t.Fatalf("应列出会话 %s：%q", id, r.session.Texts())
		}
	}
	r.clickRow("question s1")
	v := r.store.View()
	if v.ActiveSessionID != "s1" || len(v.Messages) != 1 {
		t.Fatalf("点会话行应切换到它：%+v", v)
	}
	if !r.contains("answer s1") {
		t.Fatalf("工作区应显示该会话的回答：%q", r.session.Texts())
	}
	if got := r.prefs.active(); got != "s1" {
		t.Fatalf("应记住上次活跃会话，得到 %q", got)
	}
}

func TestUISidebarEmptyHistoryAndNewSession(t *testing.T) {
	empty := newUIRig(t)
	if !empty.contains(locales.AskSessionListEmpty()) {
		t.Fatalf("没有历史会话时应给出说明：%q", empty.session.Texts())
	}

	r := newUIRig(t, withSessions("s1"), withActive("s1"))
	if !r.contains("answer s1") {
		t.Fatal("前提：恢复了上次的会话")
	}
	r.clickButton(locales.AskNewSession())
	v := r.store.View()
	if len(v.Messages) != 0 || v.ActiveSessionID == "s1" {
		t.Fatalf("新对话应开一个空会话：%+v", v)
	}
	if !r.contains(locales.AskEmpty()) {
		t.Fatal("新对话应回到空态引导")
	}
	if _, ok := r.rowButton("question s1"); !ok {
		t.Fatal("旧会话仍在历史里")
	}
}

func TestUISidebarDeleteAsksForConfirmation(t *testing.T) {
	r := newUIRig(t, withSessions("s1", "s2"))
	r.hoverRow("question s1")
	r.clickButton(locales.AskDeleteSession())
	if len(r.svc.Deletes()) != 0 {
		t.Fatal("点删除钮只是打开确认，不能直接删")
	}
	if !r.hasButton(locales.CommonConfirm()) {
		t.Fatalf("应出现确认对话框：%q", r.session.Texts())
	}
	// 取消：什么也不删。
	r.clickButton(locales.CommonCancel())
	if len(r.svc.Deletes()) != 0 {
		t.Fatal("取消后不该删除")
	}

	r.hoverRow("question s1")
	r.clickButton(locales.AskDeleteSession())
	r.clickButton(locales.CommonConfirm())
	if got := r.svc.Deletes(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("确认后应删除 s1：%q", got)
	}
	if _, ok := r.rowButton("question s1"); ok {
		t.Fatal("被删的会话应从列表消失")
	}
	if _, ok := r.rowButton("question s2"); !ok {
		t.Fatal("其余会话不受影响")
	}
}

func TestUISidebarDeletingTheActiveSessionStartsAFreshOne(t *testing.T) {
	r := newUIRig(t, withSessions("s1"), withActive("s1"))
	r.hoverRow("question s1")
	r.clickButton(locales.AskDeleteSession())
	r.clickButton(locales.CommonConfirm())
	v := r.store.View()
	if v.ActiveSessionID == "s1" || len(v.Messages) != 0 {
		t.Fatalf("删掉当前会话后应落到新会话：%+v", v)
	}
	if !r.contains(locales.AskEmpty()) {
		t.Fatal("应回到空态")
	}
}

func TestUISidebarPinAndRename(t *testing.T) {
	r := newUIRig(t, withSessions("s1", "s2"))
	// 列表是 s2 在上、s1 在下：第二个「置顶」属于 s1。
	r.clickNthButton(locales.LibraryPin(), 1)
	metas := r.svc.Metas()
	if len(metas) != 1 || metas[0].ID != "s1" || metas[0].Patch.Pinned == nil || !*metas[0].Patch.Pinned {
		t.Fatalf("应把 s1 置顶：%+v", metas)
	}
	if !r.hasButton(locales.LibraryUnpin()) {
		t.Fatalf("置顶后按钮应变成「取消置顶」：%q", r.session.Texts())
	}

	r.clickNthButton(locales.LibraryRename(), 0)
	if !r.hasButton(locales.CommonSave()) {
		t.Fatalf("应打开改名对话框：%q", r.session.Texts())
	}
	// 空白标题不能保存。
	r.screen.rename.field.SetText("   ")
	r.frames(2)
	r.clickButton(locales.CommonSave())
	if len(r.svc.Metas()) != 1 {
		t.Fatal("空白标题不该发起保存")
	}
	r.screen.rename.field.SetText("A better title")
	r.frames(2)
	r.clickButton(locales.CommonSave())
	metas = r.svc.Metas()
	if len(metas) != 2 || metas[1].Patch.Title == nil || *metas[1].Patch.Title != "A better title" {
		t.Fatalf("应保存新标题：%+v", metas)
	}
	r.frames(3)
	if r.hasButton(locales.CommonSave()) {
		t.Fatal("保存成功后对话框应关闭")
	}
	if !r.contains("A better title") {
		t.Fatalf("列表应显示新标题：%q", r.session.Texts())
	}
}

func TestUISidebarRenameFailureKeepsDialogOpenWithReason(t *testing.T) {
	r := newUIRig(t, withSessions("s1"))
	r.svc.Fail("meta", errBoom)
	r.clickNthButton(locales.LibraryRename(), 0)
	r.screen.rename.field.SetText("Whatever")
	r.frames(2)
	r.clickButton(locales.CommonSave())
	if !r.hasButton(locales.CommonSave()) {
		t.Fatal("保存失败时对话框应留着，用户才能重试")
	}
	last := r.toasts()
	if len(last) == 0 || last[len(last)-1].Detail != errBoom.Error() {
		t.Fatalf("应有带详情的失败提示：%+v", last)
	}
}

func TestUISidebarSearchIsDebounced(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		putSession(t, r.svc, "s1", done("m1", "alpha question", "alpha answer"))
		putSession(t, r.svc, "s2", done("m2", "beta question", "beta answer"))
	})
	base := len(r.svc.Queries())
	r.click(gfx.RoleTextField, locales.AskHistorySearch())
	r.typeText("beta")
	if got := len(r.svc.Queries()); got != base {
		t.Fatalf("停下 250 ms 之前不该查询：%d → %d", base, got)
	}
	r.clock.Advance(100 * time.Millisecond)
	r.typeText("!")
	r.clock.Advance(200 * time.Millisecond)
	if got := len(r.svc.Queries()); got != base {
		t.Fatalf("继续输入应重新计时：%d → %d", base, got)
	}
	r.screen.search.SetText("beta")
	r.frames(2)
	r.clock.Advance(300 * time.Millisecond)
	r.frames(3)
	qs := r.svc.Queries()
	if len(qs) != base+1 || qs[len(qs)-1].Search != "beta" {
		t.Fatalf("停下后应只查一次、带搜索词：%+v", qs)
	}
	if _, ok := r.rowButton("alpha question"); ok {
		t.Fatal("搜索后不匹配的会话应消失")
	}
	if _, ok := r.rowButton("beta question"); !ok {
		t.Fatalf("匹配的会话应留下：%q", r.session.Texts())
	}
}

func TestUISidebarFiltersByScopeRangeAndArticle(t *testing.T) {
	r := newUIRig(t, withSessions("s1"), func(r *uiRig) {
		tg := target("item-9")
		rec := services.AskSessionRecord{MessagesJSON: "[]"}
		rec.ID, rec.Title, rec.Scope, rec.ItemID, rec.ArticleTitle, rec.Target = "a1", "article chat", services.AskScopeArticle, "item-9", "Nine", &tg
		r.svc.PutSession(rec)
	})
	last := func() services.AskSessionQuery {
		qs := r.svc.Queries()
		return qs[len(qs)-1]
	}

	r.click(gfx.RoleCombobox, locales.AskHistoryScope())
	r.click(gfx.RoleOption, locales.AskHistoryScopeKnowledge())
	if last().Scope != services.AskScopeKnowledge {
		t.Fatalf("应按全局问答筛选：%+v", last())
	}
	if _, ok := r.rowButton("article chat"); ok {
		t.Fatal("筛选全局问答后不该列出本文会话")
	}

	r.click(gfx.RoleCombobox, locales.AskHistoryRange())
	r.click(gfx.RoleOption, locales.AskHistoryRange7d())
	q := last()
	if q.From == nil {
		t.Fatalf("选「近 7 天」应带下限：%+v", q)
	}
	if want := uiNow.UnixMilli() - 7*dayMs; *q.From != want {
		t.Fatalf("下限应是此刻往前 7 天：%d ≠ %d", *q.From, want)
	}
	r.click(gfx.RoleCombobox, locales.AskHistoryRange())
	r.click(gfx.RoleOption, locales.AskHistoryRangeAll())
	if last().From != nil {
		t.Fatal("回到「全部时间」应去掉下限")
	}

	r.click(gfx.RoleCombobox, locales.AskHistoryScope())
	r.click(gfx.RoleOption, locales.AskHistoryScopeAll())
	if !r.has(gfx.RoleCombobox, locales.AskHistoryArticle()) {
		t.Fatalf("有本文会话时应出现关联文章下拉：%q", r.session.Texts())
	}
	r.click(gfx.RoleCombobox, locales.AskHistoryArticle())
	r.click(gfx.RoleOption, "Nine")
	if last().ItemID != "item-9" {
		t.Fatalf("应按文章筛选：%+v", last())
	}
}

func TestUISidebarLoadsOlderSessionsOnDemand(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		for i := 0; i < 60; i++ {
			rec := putSession(t, r.svc, "s"+strconv.Itoa(i), done("m"+strconv.Itoa(i), "q"+strconv.Itoa(i), "a"))
			rec.UpdatedAt = int64(1000 + i)
			r.svc.PutSession(rec)
		}
	})
	if got := len(r.history.View().Entries); got != 50 {
		t.Fatalf("首页应是 50 条，得到 %d", got)
	}
	r.scrollBy(120, 400, 100000) // 按钮在列表底部，要先滚到
	r.clickButton(locales.AskLoadOlderSessions())
	if got := len(r.history.View().Entries); got != 60 {
		t.Fatalf("加载更早的应补齐到 60 条，得到 %d", got)
	}
	if r.hasButton(locales.AskLoadOlderSessions()) {
		t.Fatal("没有更多时不该再有「加载更早」")
	}
}

func TestUISidebarHistoryFailureOffersRetry(t *testing.T) {
	r := newUIRig(t, withSessions("s1"))
	r.svc.Fail("query", errBoom)
	r.clickButton(locales.AskNewSession()) // 触发一次保存与刷新
	r.history.Load(false)
	r.frames(3)
	if !r.contains(errBoom.Error()) || !r.hasButton(locales.CommonRetry()) {
		t.Fatalf("历史读取失败应显示原因与重试：%q", r.session.Texts())
	}
	r.svc.Fail("query", nil)
	r.clickButton(locales.CommonRetry())
	if _, ok := r.rowButton("question s1"); !ok {
		t.Fatalf("重试成功后应列出会话：%q", r.session.Texts())
	}
}

func TestUISemanticCardShowsProgressAndRunsIndexing(t *testing.T) {
	r := newUIRig(t)
	if !r.contains(locales.AskSemanticStatus(4, 10)) {
		t.Fatalf("应显示语义索引进度：%q", r.session.Texts())
	}
	run := locales.AskSemanticRun(6)
	if !r.hasButton(run) {
		t.Fatalf("应有「索引 6 条新内容」：%q", r.session.Texts())
	}
	r.inbox.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticDone, Indexed: 6})
	r.clickButton(run)
	if r.inbox.IndexRuns() != 1 {
		t.Fatalf("应发起一轮索引，实际 %d", r.inbox.IndexRuns())
	}
	toasts := r.toasts()
	if len(toasts) == 0 || toasts[len(toasts)-1].Message != locales.AskSemanticDone(6) {
		t.Fatalf("完成后应有成功提示：%+v", toasts)
	}
}

func TestUISemanticCardShowsIndexingStateAndHidesWithoutEmbedding(t *testing.T) {
	busy := newUIRig(t, func(r *uiRig) { r.inbox.SetSemanticIndexing(true) })
	if !busy.contains(locales.AskSemanticIndexing(0)) {
		t.Fatalf("索引进行中应显示进行中状态：%q", busy.session.Texts())
	}
	if busy.hasButton(locales.AskSemanticRun(6)) {
		t.Fatal("进行中不该再有可点的索引钮")
	}

	off := newUIRig(t, func(r *uiRig) { r.inbox.SetSemanticStatus(services.InboxSemanticStatus{Configured: false}) })
	if off.contains(locales.AskSemanticStatus(0, 0)) {
		t.Fatal("没配 embedding 时不出语义索引卡")
	}
}

func TestUISemanticNotConfiguredNoticeOpensSettings(t *testing.T) {
	r := newUIRig(t)
	r.inbox.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticNotConfigured})
	r.clickButton(locales.AskSemanticRun(6))
	if r.settingsOpened() != 1 {
		t.Fatal("回执为未配置时应引导去设置")
	}
}
