package app

import (
	"encoding/json"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	askscreen "guizhi-mygo-shell/viva/screens/ask"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// AI 问答接进 Root 的几处接线（root_ask.go）：模块挂的是真的 Screen、跨模块回调（配置模型、打开来源）、
// 采集结果的「就此提问」、以及 B-054（知识库彻底删除后清除问答里已展开的证据）。Screen 自己的行为在 viva/screens/ask 里测，这里只测接线。

// askTestSession 是一条带一个来源的会话：来源指向知识库条目 i1，证据正文是「证据正文-i1」。
func askTestSession(t *testing.T) services.AskSessionRecord {
	t.Helper()
	msg := services.AskMessage{
		ID: "m1", Question: "问题一", Answer: "结论见 [1]", Status: services.AskStatusDone,
		Sources: []services.AskSource{{
			Ordinal: 1, Kind: "item", RefID: "i1", Title: "来源条目",
			Evidence: &aiflow.EvidenceSnapshot{
				Version: 1, Kind: "item", Title: "来源条目", Text: "证据正文-i1", Fingerprint: "fp1",
				ReviewStatus: "clear", ReviewReasons: []string{},
			},
		}},
	}
	body, err := json.Marshal([]services.AskMessage{msg})
	if err != nil {
		t.Fatal(err)
	}
	return services.AskSessionRecord{ID: "s1", Title: "问题一", MessagesJSON: string(body)}
}

// askChipName 是来源芯片按钮的无障碍名。
const askChipName = "[1] 来源条目"

// hasChip 报告会话里的来源芯片是否已出现。
func (f *rootFixture) hasChip() bool {
	_, ok := f.session.AccessByRole(gfx.RoleButton, askChipName)
	return ok
}

// openAskRoot 打开 Root：问答与知识库都有数据（知识库里 i1 已在回收站），起始模块是问答。
func openAskRoot(t *testing.T, mutate func(*Options, *servicestest.AskFake)) (*rootFixture, *servicestest.AskFake, *servicestest.Library) {
	t.Helper()
	ask := &servicestest.AskFake{}
	ask.PutSession(askTestSession(t))
	lib := &servicestest.Library{}
	lib.Seed(services.LibraryCounts{All: 1, Trash: 1}, nil, nil)
	lib.SeedEntries([]services.LibraryEntry{{
		ID: "i1", Title: "来源条目", ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		DeletedAt: 1_800_000_000_000, ReviewStatus: "clear", ReviewReasons: []string{},
		CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
	}})
	lib.SeedDetail("i1", servicestest.DetailBody{Content: "正文"})
	// 上次活跃会话记在偏好里：首帧恢复到 s1（没有它会落在空会话上）。
	prefs := &servicestest.Prefs{}
	_ = prefs.Set(askscreen.PrefActiveSession, "s1")
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, shell.ModuleAsk
		o.Services.Ask = ask
		o.Services.Library = lib
		o.Services.Prefs = prefs
		if mutate != nil {
			mutate(o, ask)
		}
	})
	return f, ask, lib
}

// 问答模块挂的是真的界面（不是「尚未迁移」占位）：进入后读到历史会话并打开它，消息与来源芯片出现。
func TestRootAskModuleMountsTheRealScreen(t *testing.T) {
	f, _, _ := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", func() bool { return f.session.HasText("问题一") && f.hasChip() })
	if f.session.HasText(locales.NativeShellNotMigrated(locales.NavAsk())) {
		t.Error("问答已接入，不应再显示「尚未迁移」占位")
	}
}

// 没有装配 Ask 的 Root（自动化实例）不挂问答模块：落在「尚未迁移」占位上，Close 不崩。
func TestRootWithoutAskService(t *testing.T) {
	f := openRoot(t, func(o *Options) { o.Page, o.Module = shell.PageHome, shell.ModuleAsk })
	if f.root.ask != nil || f.root.askStore != nil {
		t.Fatal("没有问答服务时不应装配问答模块")
	}
	f.root.Close()
}

// 模型没配好：空态上的「去配置模型」打开设置页的 AI 分区。
func TestRootAskConfigureModelOpensSettingsAI(t *testing.T) {
	f, _, _ := openAskRoot(t, func(_ *Options, ask *servicestest.AskFake) { ask.SetQAConfigured(false) })
	f.waitFrames(t, "会话内容出现", func() bool { return f.session.HasText("问题一") })
	f.clickButton(t, locales.AskNewSession())
	f.waitFrames(t, "配置模型入口出现", func() bool {
		_, ok := f.session.AccessByRole(gfx.RoleButton, locales.AskGoConfigure())
		return ok
	})
	f.clickButton(t, locales.AskGoConfigure())
	f.waitFrames(t, "打开设置页的 AI 分区", func() bool {
		return f.root.Window().Page == shell.PageSettings && f.root.settings.Active == settingsscreen.SectionAI
	})
}

// 点来源芯片上的「打开」类入口：条目来源在知识库里打开并选中（渲染层 onOpenSource）。
func TestRootAskOpenSourceGoesToLibraryAndSelectsIt(t *testing.T) {
	f, _, _ := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.root.ask.Hooks.OpenSource(services.AskSource{Kind: "item", RefID: "i1"})
	store := f.root.library.Store
	f.waitFrames(t, "知识库里选中条目", func() bool {
		d := store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == "i1"
	})
	if scope := store.View().Filters.Scope; scope != services.LibraryScopeAll {
		t.Errorf("范围应设为全部，实际 %q", scope)
	}
}

// Wiki 页来源指向 Wiki 模块。没有装配 Wiki 服务的实例只切模块（落在占位页上）；装配了 Wiki 的实例还会打开那一页，
// 见 root_wiki_test.go 的 TestRootAskWikiSourceOpensWikiPage。
func TestRootAskOpenWikiSourceSwitchesToWikiModule(t *testing.T) {
	f, _, _ := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.root.ask.Hooks.OpenSource(services.AskSource{Kind: "wiki", RefID: "p1"})
	f.waitFrames(t, "切到 Wiki 模块", func() bool { return f.root.Window().Module == shell.ModuleWiki })
}

// 采集结果的「就此提问」：切到问答、新建会话（不污染刚浏览的历史会话）、预填草稿但不发送。
func TestRootAskAboutItemJumpsWithDraft(t *testing.T) {
	imp := &servicestest.Imports{}
	imp.Set(services.ImportPage{}, nil)
	f, ask, _ := openAskRoot(t, func(o *Options, _ *servicestest.AskFake) {
		o.Services.Imports = imp
		o.Module = shell.ModuleImports
	})
	f.waitFrames(t, "进入采集模块", func() bool { return f.root.Window().Module == shell.ModuleImports })
	draft := locales.ImportsAskAboutItemDraft("某篇采集")
	f.root.imports.Hooks.AskAboutItem(draft)
	f.waitFrames(t, "切到问答并预填草稿", func() bool {
		return f.root.Window().Module == shell.ModuleAsk && f.session.HasText(draft)
	})
	view := f.root.askStore.View()
	if view.ActiveSessionID == "s1" || len(view.Messages) != 0 {
		t.Errorf("应新建空会话，不污染历史会话：active=%q 消息=%d", view.ActiveSessionID, len(view.Messages))
	}
	if n := len(ask.AskCalls()); n != 0 {
		t.Errorf("只预填不发送，实际发起了 %d 次提问", n)
	}
}

// B-054 端到端：已展开的证据在知识库彻底删除（选了「同时清除问答证据」）之后重新脱敏，正文不再显示、改为说明原因；
// 没选清除证据时证据原样保留。
func TestRootAskEvidenceClearedByLibraryHardDelete(t *testing.T) {
	f, _, lib := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.clickButton(t, askChipName)
	f.waitFrames(t, "证据展开", func() bool { return f.session.HasText("证据正文-i1") })

	if !f.root.library.Store.DeleteForever(f.root.ctx, []string{"i1"}, true) {
		t.Fatal("彻底删除应成功")
	}
	if got := lib.ClearedEvidence(); len(got) != 1 || got[0] != "i1" {
		t.Fatalf("知识库侧应记录清除证据：%v", got)
	}
	f.waitFrames(t, "证据被清除并说明原因", func() bool { return f.session.HasText(locales.AskEvidenceClearedHint()) })
	if f.session.HasText("证据正文-i1") {
		t.Error("已清除的证据正文不能再显示")
	}
}

func TestRootAskEvidenceKeptWhenHardDeleteDoesNotClearIt(t *testing.T) {
	f, _, lib := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.clickButton(t, askChipName)
	f.waitFrames(t, "证据展开", func() bool { return f.session.HasText("证据正文-i1") })

	if !f.root.library.Store.DeleteForever(f.root.ctx, []string{"i1"}, false) {
		t.Fatal("彻底删除应成功")
	}
	if got := lib.ClearedEvidence(); len(got) != 0 {
		t.Fatalf("没选清除证据就不应登记：%v", got)
	}
	f.frames(t, 4)
	if !f.session.HasText("证据正文-i1") || f.session.HasText(locales.AskEvidenceClearedHint()) {
		t.Error("没选清除证据时证据应原样保留")
	}
}

// ---- B-061：围绕本文提问 ----

// 文章一（a1，在库）、文章二（a2，在回收站，作本文问答里的来源）、文章三（a3，在库）。
const (
	articleOneID   = "a1"
	articleTwoID   = "a2"
	articleThreeID = "a3"
)

// articleSession 是 a1 的本文会话：一轮问答，来源指向 a2，带证据快照「证据正文-a2」。
func articleSession(t *testing.T) services.AskSessionRecord {
	t.Helper()
	tg := aiflow.ArticleTarget{ItemID: articleOneID, View: "body"}
	src := aiflow.ArticleSource{
		Ordinal: 1, Kind: "article", Title: "来源文章", Text: "摘录",
		Target: &aiflow.ArticleTarget{ItemID: articleTwoID, View: "body"},
		Evidence: &aiflow.EvidenceSnapshot{
			Version: 1, Kind: "item", Title: "来源文章", Text: "证据正文-a2", Fingerprint: "fp2",
			ReviewStatus: "clear", ReviewReasons: []string{},
		},
	}
	msg := aiflow.ArticleMessage{
		ID: "am1", Question: "本文问题", Answer: "本文回答", Status: services.AskStatusDone,
		Sources: []aiflow.ArticleSource{src}, Warnings: []string{}, WebStatus: "off",
	}
	body, err := json.Marshal([]aiflow.ArticleMessage{msg})
	if err != nil {
		t.Fatal(err)
	}
	rec := services.AskSessionRecord{ID: "as1", Title: "本文问题", MessagesJSON: string(body)}
	rec.Scope, rec.ItemID, rec.ArticleTitle, rec.Target = services.AskScopeArticle, articleOneID, "文章一", &tg
	return rec
}

// openArticleAskRoot 打开 Root：知识库里有 a1、a3（在库）与 a2（回收站），问答里有 a1 的本文会话 as1。
// module 是起始模块；活跃会话记成 as1，所以从问答模块起步时落在这条本文会话上。
func openArticleAskRoot(t *testing.T, module shell.Module, mutate func(*Options, *servicestest.AskFake)) (*rootFixture, *servicestest.AskFake, *servicestest.Library) {
	t.Helper()
	ask := &servicestest.AskFake{}
	ask.PutItemTitle(articleOneID, "文章一")
	ask.PutItemTitle(articleTwoID, "文章二")
	ask.PutItemTitle(articleThreeID, "文章三")
	ask.PutSession(articleSession(t))
	lib := &servicestest.Library{}
	lib.Seed(services.LibraryCounts{All: 2, Uncategorized: 2, Trash: 1}, nil, nil)
	entry := func(id, title string, deleted int64) services.LibraryEntry {
		return services.LibraryEntry{
			ID: id, Title: title, ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive, DeletedAt: deleted,
			ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
		}
	}
	lib.SeedEntries([]services.LibraryEntry{entry(articleOneID, "文章一", 0), entry(articleTwoID, "文章二", 1_800_000_000_000), entry(articleThreeID, "文章三", 0)})
	lib.SeedDetail(articleOneID, servicestest.DetailBody{Content: "文章一的正文内容"})
	lib.SeedDetail(articleTwoID, servicestest.DetailBody{Content: "文章二的正文"})
	lib.SeedDetail(articleThreeID, servicestest.DetailBody{Content: "文章三的正文"})
	prefs := &servicestest.Prefs{}
	_ = prefs.Set(askscreen.PrefActiveSession, "as1")
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, module
		o.Services.Ask = ask
		o.Services.Library = lib
		o.Services.Prefs = prefs
		if mutate != nil {
			mutate(o, ask)
		}
	})
	return f, ask, lib
}

// hasRole 报告画面上有没有 role、name 的节点。
func (f *rootFixture) hasRole(role gfx.Role, name string) bool {
	_, ok := f.session.AccessByRole(role, name)
	return ok
}

// openArticleInLibrary 经 openLibraryItem（问答来源与「返回文章」走的同一条路）打开条目，等详情读到、头部画出提问钮。
func (f *rootFixture) openArticleInLibrary(t *testing.T, id string) {
	t.Helper()
	f.root.openLibraryItem(f.root.ctx, id)
	f.waitFrames(t, "知识库里选中并画出详情", func() bool {
		d := f.root.library.Store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == id && f.hasRole(gfx.RoleButton, locales.ArticleAskTitle())
	})
}

// 详情头部的「提问」钮开合右侧的提问栏，栏读写问答模块里同一个 ArticleStore；栏的左边仍是阅读区。
func TestRootLibraryDetailAskOpensAndClosesTheArticlePane(t *testing.T) {
	f, _, _ := openArticleAskRoot(t, shell.ModuleLibrary, nil)
	if f.root.library.ArticleAsk != f.root.ask {
		t.Fatal("知识库应接上问答界面")
	}
	f.openArticleInLibrary(t, articleOneID)
	if f.hasRole(gfx.RoleGroup, locales.ArticleAskTitle()) {
		t.Fatal("没点「提问」之前不该有提问栏")
	}
	f.clickButton(t, locales.ArticleAskTitle())
	f.waitFrames(t, "提问栏出现并读到本文会话", func() bool {
		return f.hasRole(gfx.RoleGroup, locales.ArticleAskTitle()) && f.session.HasText("本文回答")
	})
	if got := f.root.askStore.Article().View().ItemID; got != articleOneID {
		t.Fatalf("栏应读写 a1 的本文问答，实际 %q", got)
	}
	pane, _ := f.session.AccessByRole(gfx.RoleGroup, locales.ArticleAskTitle())
	body, ok := f.session.AccessByRole(gfx.RoleButton, locales.LibraryBodySection())
	if !ok || body.Bounds.X+body.Bounds.Width > pane.Bounds.X+1 {
		t.Fatalf("阅读区应在栏的左边、不被盖住：标签 %+v 栏 %+v", body.Bounds, pane.Bounds)
	}
	f.clickButton(t, locales.ArticleAskTitle())
	f.waitFrames(t, "提问栏收起", func() bool { return !f.hasRole(gfx.RoleGroup, locales.ArticleAskTitle()) })
}

// 换条目：栏收起、进行中的回答停掉（渲染层阅读器随条目换而卸载并 stop）；新条目上再打开读到的是新条目。
func TestRootLibraryDetailAskResetsWhenSwitchingItems(t *testing.T) {
	gate := make(chan struct{})
	f, ask, _ := openArticleAskRoot(t, shell.ModuleLibrary, func(_ *Options, ask *servicestest.AskFake) {
		ask.QueueArticle(servicestest.ArticleScript{Gate: gate})
	})
	defer close(gate)
	f.openArticleInLibrary(t, articleOneID)
	f.clickButton(t, locales.ArticleAskTitle())
	f.waitFrames(t, "栏读到本文会话", func() bool { return f.session.HasText("本文回答") })
	f.root.askStore.Article().SetDraft(askscreen.DraftKey(articleOneID, ""), "接着问")
	f.frames(t, 2)
	f.clickButton(t, locales.AskSend())
	f.waitFrames(t, "回答进行中", func() bool { return f.root.askStore.Article().View().Running })
	if n := len(ask.ArticleCalls()); n != 1 {
		t.Fatalf("应发起一次本文提问，实际 %d", n)
	}

	f.openArticleInLibrary(t, articleThreeID)
	f.waitFrames(t, "换条目后栏收起、回答停掉", func() bool {
		return !f.hasRole(gfx.RoleGroup, locales.ArticleAskTitle()) && !f.root.askStore.Article().View().Running
	})
	f.clickButton(t, locales.ArticleAskTitle())
	f.waitFrames(t, "新条目上的栏读到新条目", func() bool { return f.root.askStore.Article().View().ItemID == articleThreeID })
	if f.session.HasText("本文回答") {
		t.Error("新条目的栏不该带着上一条的对话")
	}
}

// 全局问答里的本文会话：头部「返回文章」切到知识库并打开那篇文章（渲染层 GlobalArticleConversation 的返回）。
func TestRootGlobalArticleBackReturnsToTheLibraryItem(t *testing.T) {
	f, _, _ := openArticleAskRoot(t, shell.ModuleAsk, nil)
	f.waitFrames(t, "全局问答里是本文对话", func() bool {
		return f.session.HasText("本文回答") && f.hasRole(gfx.RoleButton, locales.ArticleAskBack())
	})
	f.clickButton(t, locales.ArticleAskBack())
	f.waitFrames(t, "回到知识库并打开文章一", func() bool {
		d := f.root.library.Store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == articleOneID
	})
	if scope := f.root.library.Store.View().Filters.Scope; scope != services.LibraryScopeAll {
		t.Errorf("范围应设为全部，实际 %q", scope)
	}
}

// B-054：来源条目被彻底删除并清除问答证据之后，详情里已展开的本文问答证据同步脱敏（不只全局问答会话）。
func TestRootArticleAskEvidenceClearedByLibraryHardDelete(t *testing.T) {
	f, _, lib := openArticleAskRoot(t, shell.ModuleLibrary, nil)
	f.openArticleInLibrary(t, articleOneID)
	f.clickButton(t, locales.ArticleAskTitle())
	const chip = "[1] 来源文章"
	f.waitFrames(t, "来源芯片出现", func() bool { return f.hasRole(gfx.RoleButton, chip) })
	f.clickButton(t, chip)
	f.waitFrames(t, "证据展开", func() bool { return f.session.HasText("证据正文-a2") })

	if !f.root.library.Store.DeleteForever(f.root.ctx, []string{articleTwoID}, true) {
		t.Fatal("彻底删除应成功")
	}
	if got := lib.ClearedEvidence(); len(got) != 1 || got[0] != articleTwoID {
		t.Fatalf("知识库侧应记录清除证据：%v", got)
	}
	f.waitFrames(t, "本文问答里的证据被清除并说明原因", func() bool { return f.session.HasText(locales.AskEvidenceCleared()) })
	if f.session.HasText("证据正文-a2") {
		t.Error("已清除的证据正文不能再显示")
	}
}

// 来源定位与网页来源的打开经根接线：定位把任务排到界面线程、交给知识库详情（原文在条目里就回报找到），网页来源经 Services.OpenExternal。
func TestRootArticleAskHooksLocateAndOpenURL(t *testing.T) {
	var opened []string
	f, _, _ := openArticleAskRoot(t, shell.ModuleLibrary, func(o *Options, _ *servicestest.AskFake) {
		o.Services.OpenExternal = func(u string) error { opened = append(opened, u); return nil }
	})
	f.openArticleInLibrary(t, articleOneID)
	hooks := f.root.ask.Hooks
	if hooks.OpenURL == nil || hooks.LocateArticle == nil {
		t.Fatal("问答界面应接上网页来源的打开与来源定位")
	}
	if err := hooks.OpenURL("https://example.com/x"); err != nil || len(opened) != 1 || opened[0] != "https://example.com/x" {
		t.Fatalf("网页来源应经 Services.OpenExternal 打开：%v %v", opened, err)
	}
	body := "body"
	found := make(chan bool, 1)
	hooks.LocateArticle(aiflow.ArticleSource{
		Text: "文章一的正文", Target: &aiflow.ArticleTarget{ItemID: articleOneID, View: "themed", SourceKind: &body},
	}, func(ok bool) { found <- ok })
	f.waitFrames(t, "定位有结果", func() bool { return len(found) == 1 })
	if !<-found {
		t.Error("摘录在文章正文里，应回报找到")
	}
}
