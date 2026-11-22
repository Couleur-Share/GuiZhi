package library

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
	askscreen "guizhi-mygo-shell/viva/screens/ask"
)

// 本文件测详情右侧「围绕本文提问」栏的界面接线（detailask.go，台账 B-061）：开合、并排版面、与工具侧栏互斥、换条目与关浮层时的重置、
// 提问目标、选段提问入口与来源定位。栏里的界面（头部、历史、消息、输入）在 viva/screens/ask 里测过，这里只测栏接对了没有。

// withArticleAsk 给夹具接上问答界面：真实的 ArticleStore 与服务替身，条目标题与库里一致。
func withArticleAsk(r *rig) {
	svc := &servicestest.AskFake{}
	for _, e := range detailEntries() {
		svc.PutItemTitle(e.ID, e.Title)
	}
	guard := r.screen.Guard
	store := askscreen.NewStore(svc, guard, nil)
	store.Spawn = func(fn func()) { fn() }
	store.Ctx = context.Background()
	history := askscreen.NewHistoryStore(svc, guard, nil)
	r.askSvc = svc
	r.ask = &askscreen.Screen{
		Look: r.look, Store: store, History: history, Setup: askscreen.NewSetup(svc, nil, guard, nil), Ctx: context.Background(), Guard: guard,
		Now: func() time.Time { return rigNow }, Loc: time.UTC,
	}
	r.screen.ArticleAsk = r.ask
}

// askRig 是接了问答界面的详情夹具（卡片视图）。
func askRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	return detailRig(t, append([]func(*rig){withArticleAsk}, mutate...)...)
}

func (r *rig) article() askscreen.ArticleView { return r.ask.Store.Article().View() }

// askPane 是提问栏此刻的外框（带名字的区域）；没画出来时 ok 为假。
func (r *rig) askPane() (gfx.Rect, bool) {
	n, ok := r.node(gfx.RoleGroup, locales.ArticleAskTitle())
	return n.Bounds, ok
}

// askButton 是头部的「提问」钮（与栏同名，但角色是按钮）。
func (r *rig) askButton() (expanded bool) {
	r.t.Helper()
	n, ok := r.detailNode(gfx.RoleButton, locales.ArticleAskTitle())
	if !ok {
		r.t.Fatal("头部缺「提问」钮")
	}
	return n.Expanded
}

func (r *rig) toggleAskButton() {
	r.t.Helper()
	r.clickInDetail(gfx.RoleButton, locales.ArticleAskTitle())
}

// 点「提问」展开右侧的栏、再点收起；钮的可访问状态是展开（aria-expanded），栏读的是这一条。
func TestAskButtonOpensAndClosesTheSidePane(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	if _, ok := r.askPane(); ok || r.askButton() {
		t.Fatal("没点之前不该有提问栏，钮也不该是展开的")
	}
	r.toggleAskButton()
	if _, ok := r.askPane(); !ok || !r.askButton() {
		t.Fatalf("点「提问」应展开提问栏：%q", r.session.Texts())
	}
	if got := r.article().ItemID; got != detailNoteID {
		t.Fatalf("栏应读写这一条的问答，实际 %q", got)
	}
	if !r.hasText(detailEntryOf(detailNoteID).Title) {
		t.Fatalf("栏头部应写着这一条的标题：%q", r.session.Texts())
	}
	r.toggleAskButton()
	if _, ok := r.askPane(); ok || r.askButton() {
		t.Fatal("再点一下应收起")
	}
	if n := r.screen.detail.ask.openID; n != "" {
		t.Fatalf("收起后不该记着开着的条目，实际 %q", n)
	}
}

// 栏与正文并排、让出宽度：阅读区在栏的左边、不被盖住；宽屏（行宽 ≥ 960）固定 400，窄屏取 min(400, 48%) 且不小于 280。
func TestAskPaneSitsBesideTheReaderAndFollowsTheWidthRule(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	tab, _ := r.detailNode(gfx.RoleButton, locales.LibraryBodySection())
	before := tab.Bounds
	r.toggleAskButton()
	pane, ok := r.askPane()
	if !ok {
		t.Fatal("夹具失效：栏没有打开")
	}
	row := r.screen.detail.ask.rootW
	if row <= 0 || row >= askWideMin {
		t.Fatalf("卡片视图的详情栏应在窄档（<%v），实际 %v", askWideMin, row)
	}
	if want := min(askPaneWidth, row*askNarrowPct/100); abs32(pane.Width-max(want, askNarrowMin)) > 1.5 {
		t.Fatalf("窄档栏宽应是 max(min(400, 48%%), 280)=%v，实际 %v（行宽 %v）", max(want, askNarrowMin), pane.Width, row)
	}
	if now, _ := r.detailNode(gfx.RoleButton, locales.LibraryBodySection()); now.Bounds.X+now.Bounds.Width > pane.X+1 {
		t.Fatalf("阅读区的工具栏不该被栏盖住：标签 %+v 栏 %+v", now.Bounds, pane)
	} else if now.Bounds.X != before.X {
		t.Fatalf("阅读区靠左的位置不该动：%v → %v", before.X, now.Bounds.X)
	}

	// 专注阅读：详情占满工作区（≥ 960），栏固定 400；并且不再限宽居中，栏贴到工作区右缘。
	r.clickInDetail(gfx.RoleButton, locales.LibraryEnterFocusReading())
	if _, ok := r.askPane(); ok {
		t.Fatal("切到专注阅读是重新挂载，栏应回到收起")
	}
	r.toggleAskButton()
	pane, _ = r.askPane()
	if r.screen.detail.ask.rootW < askWideMin {
		t.Fatalf("专注阅读的行宽应在宽档，实际 %v", r.screen.detail.ask.rootW)
	}
	if abs32(pane.Width-askPaneWidth) > 1.5 {
		t.Fatalf("宽档栏宽应固定 400，实际 %v", pane.Width)
	}
	if edge := pane.X + pane.Width; abs32(edge-1280) > 1.5 {
		t.Fatalf("栏应贴到窗口右缘，实际右缘 %v", edge)
	}
}

// 很窄的窗口：48% 比 280 还小时取 280（min-w-[280px]）。
func TestAskPaneNeverGetsNarrowerThanItsMinimum(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	if err := r.session.Resize(760, 800); err != nil {
		t.Fatal(err)
	}
	r.frames(4)
	r.toggleAskButton()
	pane, ok := r.askPane()
	if !ok {
		t.Fatal("夹具失效：栏没有打开")
	}
	row := r.screen.detail.ask.rootW
	if row*askNarrowPct/100 >= askNarrowMin {
		t.Skipf("行宽 %v 的 48%% 已不小于 280，这个窗口量不到下限", row)
	}
	if abs32(pane.Width-askNarrowMin) > 1.5 {
		t.Fatalf("行宽 %v 的 48%% 小于 280，栏宽应取 280，实际 %v", row, pane.Width)
	}
}

// 提问栏与文章工具侧栏互斥：开一个就收起另一个（渲染层 onToggleAsk 先 setToolsItemId(null)，工具栏开合先 setAskItemId(null)）。
func TestAskPaneAndToolsPanelExcludeEachOther(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openTools(detailNoteID)
	// 工具侧栏盖在头部上，点不到下面的「提问」钮：这个方向直接走钮的处理函数。
	r.screen.toggleAsk(detailNoteID)
	r.frames(3)
	if _, ok := r.askPane(); !ok || r.toolsShown() {
		t.Fatalf("开提问栏应收起工具侧栏：提问栏 %v 工具栏 %v", ok, r.toolsShown())
	}
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	if _, ok := r.askPane(); ok || !r.toolsShown() {
		t.Fatalf("开工具侧栏应收起提问栏：提问栏 %v 工具栏 %v", ok, r.toolsShown())
	}
}

// 换条目：栏收起，进行中的回答停掉；在新条目上再打开读到的是新条目（渲染层 key=ask:<id> 的卸载）。
func TestSwitchingItemsClosesThePaneAndStopsTheAnswer(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	r := askRig(t, func(r *rig) {
		r.ask.Store.Spawn = func(fn func()) { go fn() }
		r.askSvc.QueueArticle(servicestest.ArticleScript{Gate: gate})
	})
	r.openCard(detailNoteID)
	r.toggleAskButton()
	r.ask.Store.Article().SetDraft(askscreen.DraftKey(detailNoteID, ""), "第一个问题")
	r.frames(2)
	r.click(gfx.RoleButton, locales.AskSend())
	waitFor(t, "回答进行中", func() bool { r.frames(1); return r.article().Running })

	r.pick(detailForumID)
	if _, ok := r.askPane(); ok {
		t.Fatal("换条目应收起提问栏")
	}
	waitFor(t, "换条目后回答停掉", func() bool { r.frames(1); return !r.article().Running })
	close(gate)

	r.toggleAskButton()
	waitFor(t, "栏读到新条目", func() bool { r.frames(1); return r.article().ItemID == detailForumID })
	if len(r.article().Messages) != 0 {
		t.Fatalf("新条目的栏不该带着上一条的消息：%+v", r.article().Messages)
	}
}

// 关详情浮层（表格视图）：栏收起、回答停掉；再打开详情时栏是收起的。
func TestClosingTheDetailModalResetsThePane(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openModal()
	r.toggleAskButton()
	if _, ok := r.askPane(); !ok {
		t.Fatal("浮层里的详情也该能开提问栏")
	}
	r.ask.Store.Article().SetDraft(askscreen.DraftKey(r.view().List.SelectedID, ""), "草稿")
	r.click(gfx.RoleButton, locales.LibraryCloseDetail())
	if r.screen.DetailModalOpen() {
		t.Fatal("夹具失效：浮层应已关")
	}
	if got := r.screen.detail.ask; got.openID != "" || got.itemID != "" {
		t.Fatalf("关浮层应复位提问栏的状态，实际 %+v", got)
	}
	r.openModalAgain()
	if _, ok := r.askPane(); ok {
		t.Fatal("再打开详情浮层时栏应是收起的")
	}
}

// 详情浮层的焦点陷阱只放行浮层自己的作用域：浮层里的提问栏照样能输入、能发送，Esc 先收起栏而不是关浮层。
func TestAskPaneIsInteractiveInsideTheDetailModal(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openModal()
	r.toggleAskButton()
	r.click(gfx.RoleTextField, locales.ArticleAskQuestion())
	r.typeText("浮层里的问题")
	r.click(gfx.RoleButton, locales.AskSend())
	calls := r.askSvc.ArticleCalls()
	if len(calls) != 1 || calls[0].Question != "浮层里的问题" {
		t.Fatalf("在浮层里应能输入并发送，服务收到 %+v", calls)
	}
	r.click(gfx.RoleTextField, locales.ArticleAskQuestion())
	r.keys("escape")
	if _, ok := r.askPane(); ok {
		t.Fatal("焦点在栏里按 Esc 应收起栏")
	}
	if !r.screen.DetailModalOpen() {
		t.Fatal("这一下 Esc 被栏拦下了，不该同时关掉详情浮层")
	}
}

// openModalAgain 在已经是表格视图的夹具里再点第一行打开详情浮层。
func (r *rig) openModalAgain() {
	r.t.Helper()
	entries := r.view().List.Entries
	x, y := r.rowBlank(entries[0])
	r.clickAt(x, y)
	if !r.screen.DetailModalOpen() {
		r.t.Fatal("夹具失效：点行应打开详情浮层")
	}
}

// 离开知识库（工作区卸载）也停掉在途的回答。
func TestUnmountingTheWorkspaceStopsTheArticleAnswer(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	r := askRig(t, func(r *rig) {
		r.ask.Store.Spawn = func(fn func()) { go fn() }
		r.askSvc.QueueArticle(servicestest.ArticleScript{Gate: gate})
	})
	r.openCard(detailNoteID)
	r.toggleAskButton()
	r.ask.Store.Article().SetDraft(askscreen.DraftKey(detailNoteID, ""), "问题")
	r.frames(2)
	r.click(gfx.RoleButton, locales.AskSend())
	waitFor(t, "回答进行中", func() bool { r.frames(1); return r.article().Running })
	r.wsHidden = true
	r.frames(3)
	waitFor(t, "工作区卸载后回答停掉", func() bool { r.frames(1); return !r.article().Running })
	close(gate)
}

// 发送时的提问目标取阅读区此刻的视图（渲染层 readerTarget）：正文是主题阅读的原文（themed，来源 body）；
// 切到论坛的「讨论」标签就是 replies。
func TestAskTargetFollowsTheReadingView(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	r.toggleAskButton()
	send := func(question string) aiflow.ArticleTarget {
		r.t.Helper()
		r.click(gfx.RoleTextField, locales.ArticleAskQuestion())
		r.typeText(question)
		r.click(gfx.RoleButton, locales.AskSend())
		calls := r.askSvc.ArticleCalls()
		if len(calls) == 0 {
			r.t.Fatalf("发送后服务应收到提问：%q", r.session.Texts())
		}
		return calls[len(calls)-1].Target
	}
	got := send("这篇讲什么")
	if got.ItemID != detailNoteID || got.View != "themed" || got.SourceKind == nil || *got.SourceKind != "body" {
		t.Fatalf("笔记正文的目标应是 themed/body，实际 %+v", got)
	}

	r.pick(detailForumID)
	r.toggleAskButton()
	r.clickInDetail(gfx.RoleButton, locales.LibraryForumRepliesSection())
	got = send("大家怎么说")
	if got.ItemID != detailForumID || got.View != "replies" || got.SourceKind != nil {
		t.Fatalf("讨论标签的目标应是 replies，实际 %+v", got)
	}
}

// 编辑态里不能提问（草稿不在库里）：发送前取目标的闭包报错，错误原因交给栏弹带详情的提示；正文落盘后才放行。
func TestAskResolverRefusesWhileEditingAndPassesOtherwise(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	target := aiflow.ArticleTarget{ItemID: detailNoteID, View: "body"}
	if _, err := r.screen.askResolver(target, true)(); err == nil || !strings.Contains(err.Error(), "编辑") {
		t.Fatalf("编辑态应拒绝并说明原因，实际 %v", err)
	}
	got, err := r.screen.askResolver(target, false)()
	if err != nil || got.ItemID != detailNoteID {
		t.Fatalf("没在编辑时应放行，实际 %+v %v", got, err)
	}
}

// 焦点在栏里按 Esc 收起它，并拦下这个键（专注阅读的退出不再运行）。
func TestEscapeInsideThePaneClosesItFirst(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	r.clickInDetail(gfx.RoleButton, locales.LibraryEnterFocusReading())
	r.toggleAskButton()
	r.click(gfx.RoleTextField, locales.ArticleAskQuestion())
	r.keys("escape")
	if _, ok := r.askPane(); ok {
		t.Fatal("焦点在栏里按 Esc 应收起提问栏")
	}
	if !r.screen.FocusReading() {
		t.Fatal("这一下 Esc 被栏拦下了，不该同时退出专注阅读")
	}
}

// 栏的收起钮：停止回答并保存之后收起（渲染层 onClose）。
func TestPaneCloseButtonClosesIt(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	r.toggleAskButton()
	r.click(gfx.RoleButton, locales.ArticleAskClose())
	r.frames(3)
	if _, ok := r.askPane(); ok || r.askButton() {
		t.Fatal("点栏里的收起钮应收起提问栏")
	}
}

// 选段提问条的落点：带选段打开栏，选段挂成引用；「解释这段」还把提示语写进草稿。条本身要等岛协议有选区消息（见 detailask.go 的注释）。
func TestAskSelectionOpensThePaneWithTheQuote(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	r.screen.AskSelection(detailNoteID, "  并发模式  ", nil, true)
	r.frames(4)
	if _, ok := r.askPane(); !ok {
		t.Fatalf("选段提问应打开提问栏：%q", r.session.Texts())
	}
	if !r.hasText("并发模式") {
		t.Fatalf("选段应挂成引用：%q", r.session.Texts())
	}
	key := askscreen.DraftKey(detailNoteID, "")
	got := r.ask.Store.Article().Draft(key)
	if got.Text != locales.ArticleAskExplainPrompt() || got.Quote == nil || got.Quote.Selection == nil || *got.Quote.Selection != "并发模式" {
		t.Fatalf("「解释这段」应写入提示语并挂上选段引用，实际草稿 %+v", got)
	}

	// 别的条目发来的选段（换条目的间隙）与空选段都忽略。
	r.screen.AskSelection(detailForumID, "x", nil, false)
	r.screen.AskSelection(detailNoteID, "   ", nil, false)
	if got := r.screen.detail.ask.promptKey; got != 1 {
		t.Fatalf("无效选段不该再触发提示语，序号 %d", got)
	}
}

// 来源定位：来源所在的条目已打开且视图一致时，把原文片段交给查找条并回报命中。
func TestLocateArticleFindsTheExcerptInTheOpenItem(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	body := "body"
	var got []bool
	done := func(found bool) { got = append(got, found) }
	r.screen.LocateArticle(aiflow.ArticleSource{
		Text:   "并发模式\n\n正文内容",
		Target: &aiflow.ArticleTarget{ItemID: detailNoteID, View: "themed", SourceKind: &body},
	}, done)
	r.frames(3)
	if len(got) != 1 || !got[0] {
		t.Fatalf("摘录在正文里，应回报找到：%v", got)
	}
	p := r.screen.detail.panel
	if !p.findOpen || p.findQuery != "并发模式 正文内容" {
		t.Fatalf("应打开查找条并填入摘录（空白折叠）：开 %v 词 %q", p.findOpen, p.findQuery)
	}
}

// 来源所在的条目还没打开（全局问答里点来源）：先等它打开，再切到来源所在的标签、查找；找不到的摘录回报失败。
func TestLocateArticleWaitsForTheItemAndSwitchesToTheSourceTab(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	body := "body"
	var got []bool
	r.screen.LocateArticle(aiflow.ArticleSource{
		Text:   "如何备份 WAL 模式的库",
		Target: &aiflow.ArticleTarget{ItemID: detailForumID, View: "themed", SourceKind: &body},
	}, func(found bool) { got = append(got, found) })
	r.frames(3)
	if len(got) != 0 {
		t.Fatalf("条目还没打开，不该有结果：%v", got)
	}
	r.pick(detailForumID)
	if len(got) != 1 || !got[0] {
		t.Fatalf("打开之后应切到正文标签并找到：%v", got)
	}
	if tab := r.screen.detail.panel.tab; tab != TabBody {
		t.Fatalf("论坛默认在「讨论总结」，定位正文应切到正文标签，实际 %q", tab)
	}

	var missed []bool
	r.screen.LocateArticle(aiflow.ArticleSource{
		Text:   "这段话不在文章里",
		Target: &aiflow.ArticleTarget{ItemID: detailForumID, View: "themed", SourceKind: &body},
	}, func(found bool) { missed = append(missed, found) })
	r.frames(3)
	if len(missed) != 1 || missed[0] {
		t.Fatalf("摘录不在文章里应回报没找到：%v", missed)
	}
}

// 来源的视图切不过去（笔记没有「讨论」标签）或条目一直没打开：到期按失败回报，只回报一次；再来一次定位会先让上一次失败。
func TestLocateArticleGivesUpAfterTheWindowAndReplacesTheOldRequest(t *testing.T) {
	t.Parallel()
	now := rigNow
	r := askRig(t, func(r *rig) { r.screen.Now = func() time.Time { return now } })
	r.openCard(detailNoteID)
	var first, second []bool
	r.screen.LocateArticle(aiflow.ArticleSource{
		Text:   "并发",
		Target: &aiflow.ArticleTarget{ItemID: detailNoteID, View: "replies"},
	}, func(found bool) { first = append(first, found) })
	r.frames(3)
	if len(first) != 0 {
		t.Fatalf("还在窗口内不该有结果：%v", first)
	}
	now = now.Add(askLocateWindow + time.Second)
	r.frames(3)
	if len(first) != 1 || first[0] {
		t.Fatalf("到期应回报失败一次：%v", first)
	}

	r.screen.LocateArticle(aiflow.ArticleSource{Text: "x", Target: &aiflow.ArticleTarget{ItemID: "nope", View: "body"}}, func(found bool) { second = append(second, found) })
	r.screen.LocateArticle(aiflow.ArticleSource{}, func(found bool) { second = append(second, found) })
	if len(second) != 2 || second[0] || second[1] {
		t.Fatalf("被顶掉的请求与没有目标的请求都应回报失败：%v", second)
	}
}

// 工作区卸载时在途的定位按失败回报，不悬着。
func TestUnmountingTheWorkspaceFailsAPendingLocate(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	var got []bool
	r.screen.LocateArticle(aiflow.ArticleSource{Text: "x", Target: &aiflow.ArticleTarget{ItemID: "nope", View: "body"}}, func(found bool) { got = append(got, found) })
	r.frames(2)
	r.wsHidden = true
	r.frames(3)
	if len(got) != 1 || got[0] {
		t.Fatalf("卸载应让在途的定位失败一次：%v", got)
	}
}

// 提问目标的推导：快照带版本，主题阅读的原文带来源，配图并入正文，其余取标签。
func TestArticleTargetOfDerivesViewFromTheReaderView(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		view       reader.View
		active     ReadingTab
		wantView   string
		wantSource string
		wantVer    string
	}{
		{"原文", reader.View{Kind: reader.ViewOriginal, SourceKind: "summary"}, TabSummary, "themed", "summary", ""},
		{"快照", reader.View{Kind: reader.ViewSnapshot, SnapshotVersionID: "v9"}, TabBody, "snapshot", "", "v9"},
		{"快照没有版本", reader.View{Kind: reader.ViewSnapshot}, TabBody, "snapshot", "", ""},
		{"配图", reader.View{Kind: reader.ViewGallery}, TabImages, "body", "", ""},
		{"讨论", reader.View{Kind: reader.ViewReplies}, TabReplies, "replies", "", ""},
		{"文字稿", reader.View{Kind: reader.ViewNone}, TabTranscript, "transcript", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := articleTargetOf("i1", c.view, c.active)
			if got.ItemID != "i1" || got.View != c.wantView || derefStr(got.SourceKind) != c.wantSource || derefStr(got.VersionID) != c.wantVer {
				t.Fatalf("得到 %+v", got)
			}
		})
	}
}

// 没接问答界面时不报告展开（钮的状态来自界面状态而不是点击次数）。
func TestAskOpenForNeedsAnAskScreen(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.screen.detail.ask.openID = detailNoteID
	if r.screen.askOpenFor(detailNoteID) {
		t.Fatal("没有问答界面时不该认为栏开着")
	}
}
