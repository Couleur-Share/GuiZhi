package app

import (
	"encoding/json"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
	askscreen "guizhi-mygo-shell/viva/screens/ask"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// Wiki 接进 Root 的几处接线（root_wiki.go）：模块挂的是真的 Screen、问答里的 Wiki 来源、来源条目跳知识库、
// 「前往配置」与自动编译指示去设置页、处理中心的「查看范围和任务」打开编译对话框、图谱滚轮的窗口级钩子。
// Screen 自己的行为在 viva/screens/wiki 里测，这里只测接线。

// openWikiRoot 打开 Root：Wiki 有三页（Alpha 引用 Beta，Alpha 有来源条目 i1），知识库里有 i1，起始模块由 module 给出。
func openWikiRoot(t *testing.T, module shell.Module, mutate func(*Options, *servicestest.WikiFake)) (*rootFixture, *servicestest.WikiFake, *servicestest.Library) {
	t.Helper()
	wiki := &servicestest.WikiFake{}
	wiki.PutPage(services.WikiPage{ID: "alpha", Title: "Alpha", Body: "body of alpha"})
	wiki.PutPage(services.WikiPage{ID: "beta", Title: "Beta", Body: "body of beta"})
	wiki.PutPage(services.WikiPage{ID: "gamma", Title: "Gamma", Kind: services.WikiKindEntity, Body: "body of gamma"})
	wiki.PutLink("alpha", "beta")
	wiki.PutSources("alpha", services.WikiPageSource{ItemID: "i1", Title: "Source One"})
	lib := &servicestest.Library{}
	lib.Seed(services.LibraryCounts{All: 1, Uncategorized: 1}, nil, nil)
	lib.SeedEntries([]services.LibraryEntry{{
		ID: "i1", Title: "来源条目", ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
	}})
	lib.SeedDetail("i1", servicestest.DetailBody{Content: "正文"})
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, module
		o.Services.Wiki = wiki
		o.Services.Library = lib
		o.Services.Prefs = &servicestest.Prefs{}
		if mutate != nil {
			mutate(o, wiki)
		}
	})
	return f, wiki, lib
}

// hasButtonNamed 报告画面上是否有名为 name 的按钮。
func (f *rootFixture) hasButtonNamed(name string) bool {
	_, ok := f.session.AccessByRole(gfx.RoleButton, name)
	return ok
}

// 模块挂的是真的界面（不是「尚未迁移」占位）：进入后读到目录。
func TestRootWikiModuleMountsTheRealScreen(t *testing.T) {
	f, _, _ := openWikiRoot(t, shell.ModuleWiki, nil)
	f.waitFrames(t, "目录出现", func() bool { return f.hasButtonNamed("Alpha") && f.hasButtonNamed("Beta") })
	if f.session.HasText(locales.NativeShellNotMigrated(locales.NavWiki())) {
		t.Error("Wiki 已接入，不应再显示「尚未迁移」占位")
	}
	if f.root.wiki == nil {
		t.Fatal("装配了 Wiki 服务应挂 Wiki 模块")
	}
}

// rail 上点 Wiki：切到 Wiki 模块并读出目录。
func TestRootRailSwitchesToWiki(t *testing.T) {
	f, _, _ := openWikiRoot(t, shell.ModuleLibrary, nil)
	f.frames(t, 3)
	if err := f.session.ClickLabel("Wiki"); err != nil {
		t.Fatalf("点 rail 的 Wiki 失败：%v", err)
	}
	f.waitFrames(t, "进入 Wiki 并读出目录", func() bool {
		return f.root.Window().Module == shell.ModuleWiki && f.hasButtonNamed("Alpha")
	})
}

// 没有装配 Wiki 的 Root（自动化实例）不挂 Wiki 模块：落在「尚未迁移」占位上，Close 不崩。
func TestRootWithoutWikiService(t *testing.T) {
	f := openRoot(t, func(o *Options) { o.Page, o.Module = shell.PageHome, shell.ModuleWiki })
	if f.root.wiki != nil {
		t.Fatal("没有 Wiki 服务时不应装配 Wiki 模块")
	}
	f.waitFrames(t, "占位页", func() bool { return f.session.HasText(locales.NativeShellNotMigrated(locales.NavWiki())) })
	f.root.Close()
}

// 问答里的 Wiki 来源：切到 Wiki 模块并打开那一页（渲染层 onOpenSource 的 wiki 分支）。
func TestRootAskWikiSourceOpensWikiPage(t *testing.T) {
	f, _, _ := openAskWikiRoot(t)
	f.waitFrames(t, "会话与来源芯片出现", func() bool { return f.hasButtonNamed("[1] Beta") })
	f.clickButton(t, "[1] Beta") // 来源芯片先展开「回答时的证据」，再点「查看当前版本」才打开来源
	f.waitFrames(t, "证据摘录出现", func() bool { return f.hasButtonNamed(locales.AskEvidenceViewCurrent()) })
	f.clickButton(t, locales.AskEvidenceViewCurrent())
	f.waitFrames(t, "切到 Wiki 并打开 Beta", func() bool {
		v := f.root.wiki.Store.View()
		return f.root.Window().Module == shell.ModuleWiki && v.SelectedPageID == "beta" && v.Detail != nil
	})
	f.waitFrames(t, "详情画出来", func() bool { return f.session.HasText("body of beta") })
	if f.root.Window().Page != shell.PageHome {
		t.Errorf("应在主页：%v", f.root.Window().Page)
	}
}

// 来源指向图谱里被截掉的 Wiki 页也一样：直接调回调，选中的是 RefID。
func TestRootOpenAskSourceWikiSelectsRefID(t *testing.T) {
	f, _, _ := openWikiRoot(t, shell.ModuleAsk, nil)
	f.frames(t, 3)
	f.root.openAskSource(services.AskSource{Kind: "wiki", RefID: "gamma"})
	f.waitFrames(t, "打开 Gamma", func() bool {
		return f.root.Window().Module == shell.ModuleWiki && f.root.wiki.Store.View().SelectedPageID == "gamma"
	})
}

// openAskWikiRoot 打开 Root：问答里有一条会话，唯一的来源是 Wiki 页 Beta；Wiki 与问答同时装配。
func openAskWikiRoot(t *testing.T) (*rootFixture, *servicestest.AskFake, *servicestest.WikiFake) {
	t.Helper()
	ask := &servicestest.AskFake{}
	msg := services.AskMessage{
		ID: "m1", Question: "问题一", Answer: "结论见 [1]", Status: services.AskStatusDone,
		Sources: []services.AskSource{{Ordinal: 1, Kind: "wiki", RefID: "beta", Title: "Beta"}},
	}
	body, err := json.Marshal([]services.AskMessage{msg})
	if err != nil {
		t.Fatal(err)
	}
	ask.PutSession(services.AskSessionRecord{ID: "s1", Title: "问题一", MessagesJSON: string(body)})
	prefs := &servicestest.Prefs{}
	_ = prefs.Set(askscreen.PrefActiveSession, "s1")
	f, wiki, _ := openWikiRoot(t, shell.ModuleAsk, func(o *Options, _ *servicestest.WikiFake) {
		o.Services.Ask = ask
		o.Services.Prefs = prefs
	})
	return f, ask, wiki
}

// 详情里的来源条目：点它跳到知识库并选中该条目（Store.OnOpenSource → openLibraryItem）。
func TestRootWikiSourceChipOpensLibraryItem(t *testing.T) {
	f, _, _ := openWikiRoot(t, shell.ModuleWiki, nil)
	f.waitFrames(t, "目录出现", func() bool { return f.hasButtonNamed("Alpha") })
	f.clickButton(t, "Alpha")
	f.waitFrames(t, "来源芯片出现", func() bool { return f.hasButtonNamed("Source One") })
	f.clickButton(t, "Source One")
	store := f.root.library.Store
	f.waitFrames(t, "知识库里选中来源条目", func() bool {
		d := store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == "i1"
	})
	if scope := store.View().Filters.Scope; scope != services.LibraryScopeAll {
		t.Errorf("范围应设为全部，实际 %q", scope)
	}
}

// 没有 Wiki 模型：编译对话框里的「前往配置」收起对话框并打开设置页的 AI 分区。
func TestRootWikiGoConfigureOpensSettingsAI(t *testing.T) {
	f, _, _ := openWikiRoot(t, shell.ModuleWiki, func(_ *Options, wiki *servicestest.WikiFake) {
		wiki.SetCompileConfigured(false)
		wiki.SetPreview(wikiPreview(2))
	})
	f.waitFrames(t, "目录出现", func() bool { return f.hasButtonNamed("Alpha") })
	f.clickButton(t, locales.WikiCompileDialog())
	f.waitFrames(t, "配置入口出现", func() bool { return f.hasButtonNamed(locales.ArticleAskConfigure()) })
	f.clickButton(t, locales.ArticleAskConfigure())
	f.waitFrames(t, "打开设置页的 AI 分区", func() bool {
		return f.root.Window().Page == shell.PageSettings && f.root.settings.Active == settingsscreen.SectionAI
	})
	if f.root.wiki.Store.Compile().View().Open {
		t.Error("去配置时应收起编译对话框")
	}
}

// 设置里开了自动编译：工具栏有指示，点它回设置页的通用分区；没开就没有指示。
func TestRootWikiAutoCompileIndicatorOpensSettingsGeneral(t *testing.T) {
	f, _, _ := openWikiRoot(t, shell.ModuleWiki, nil)
	f.waitFrames(t, "目录出现", func() bool { return f.hasButtonNamed("Alpha") })
	if f.hasButtonNamed(locales.WikiAutoCompile()) {
		t.Fatal("自动编译关着不该有指示")
	}

	on := &servicestest.Settings{}
	values := settings.Default()
	values.UI.WikiCompileEnabled = true
	on.Set(values)
	f2, _, _ := openWikiRoot(t, shell.ModuleWiki, func(o *Options, _ *servicestest.WikiFake) { o.Services.Settings = on })
	f2.waitFrames(t, "指示出现", func() bool { return f2.hasButtonNamed(locales.WikiAutoCompile()) })
	f2.clickButton(t, locales.WikiAutoCompile())
	f2.waitFrames(t, "打开设置页的通用分区", func() bool {
		return f2.root.Window().Page == shell.PageSettings && f2.root.settings.Active == settingsscreen.SectionGeneral
	})
}

// 处理中心横幅的「查看范围和任务」：切到 Wiki 模块并打开编译对话框（有 Wiki 服务时替代原来的「尚未迁移」提示）。
func TestRootInboxWikiPreviewOpensCompileDialog(t *testing.T) {
	inbox := &servicestest.Inbox{}
	inbox.SetList(inboxTestList())
	inbox.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3})
	inbox.SetWikiModelReady(true)
	f, _, _ := openWikiRoot(t, shell.ModuleInbox, func(o *Options, wiki *servicestest.WikiFake) {
		o.Services.Inbox = inbox
		wiki.SetPreview(wikiPreview(3))
	})
	f.waitFrames(t, "处理中心列表出现", func() bool { return f.session.HasText("待复核一") })
	f.root.inboxStore.Refresh(f.root.ctx)
	f.waitFrames(t, "Wiki 横幅出现", func() bool { return f.hasButtonNamed(locales.InboxWikiCandidatesView()) })
	f.clickButton(t, locales.InboxWikiCandidatesView())
	f.waitFrames(t, "切到 Wiki 并打开编译对话框", func() bool {
		return f.root.Window().Module == shell.ModuleWiki && f.root.wiki.Store.Compile().View().Open
	})
	f.waitFrames(t, "对话框读出范围", func() bool { return f.hasButtonNamed(locales.WikiCompileStart()) })
	if f.session.HasText(locales.CommonOperationFailed(locales.InboxWikiCandidatesView())) {
		t.Error("Wiki 接入后不该再提示「尚未迁移」")
	}
}

// 图谱滚轮缩放走窗口级钩子：Root 把它装进 Look，滚轮落在画布上就缩放，不滚动背后的内容。
func TestRootWikiGraphWheelZoomsThroughWindowHook(t *testing.T) {
	f, _, _ := openWikiRoot(t, shell.ModuleWiki, nil)
	f.waitFrames(t, "目录出现", func() bool { return f.hasButtonNamed("Alpha") })
	if f.root.look.WheelHook == nil {
		t.Fatal("装配了 Wiki 应把图谱滚轮钩子装进 Look")
	}
	f.clickButton(t, locales.WikiViewGraph())
	f.waitFrames(t, "图谱读出来", func() bool { return f.root.wiki.Store.View().Graph != nil })
	f.frames(t, 3)

	if err := f.session.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: -400, Precise: true}); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if f.root.wiki.GraphZoom() <= 1 {
		t.Fatalf("滚轮落在图谱画布上应放大：%v", f.root.wiki.GraphZoom())
	}
	before := f.root.wiki.GraphZoom()
	// 离开图谱视图后滚轮不再被接走。
	f.clickButton(t, locales.WikiViewCatalog())
	f.frames(t, 3)
	if err := f.session.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: -400, Precise: true}); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if after := f.root.wiki.GraphZoom(); after != before {
		t.Fatalf("目录视图下滚轮不该改图谱缩放：%v → %v", before, after)
	}
}

// 编译预览：n 条可执行资料。
func wikiPreview(n int) services.WikiCompilePreview {
	entries := make([]services.WikiCompileCandidate, n)
	for i := range entries {
		id := string(rune('a' + i))
		entries[i] = services.WikiCompileCandidate{ID: id, Title: "item " + id, Fingerprint: "fp-" + id, State: services.WikiCandidateReady, Blocks: 2}
	}
	return services.WikiCompilePreview{Entries: entries, Counts: services.WikiCompileCounts{Ready: n}}
}
