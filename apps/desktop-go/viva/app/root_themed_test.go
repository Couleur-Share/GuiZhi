package app

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	libraryscreen "guizhi-mygo-shell/viva/screens/library"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 主题阅读（AI 阅读）接进 Root 的几处接线（root_themed.go，台账 B-064 / B-074）：知识库详情的「AI 阅读」、处理中心的「主题页任务」、
// 设置 AI 分区的「联网搜索」、去配置模型与打开参考资料链接两个跨模块回调、切走时的卸载。主题阅读自己的界面行为在 viva/screens/themed 里测，这里只测接线。

var themedReq = services.ThemedRequest{ItemID: "n1", SourceKind: "body"}

type themedEnv struct {
	f        *rootFixture
	svc      *servicestest.ThemedFake
	inbox    *servicestest.Inbox
	prefs    *servicestest.Prefs
	external []string
}

// openThemedRoot 打开 Root：知识库里有 n1（带正文），处理中心里有 n1 的待复核，主题阅读用假服务；wired 为假时不装配主题阅读。
func openThemedRoot(t *testing.T, module shell.Module, wired bool, mutate func(*Options, *servicestest.ThemedFake)) *themedEnv {
	t.Helper()
	env := &themedEnv{svc: &servicestest.ThemedFake{}, inbox: &servicestest.Inbox{}, prefs: &servicestest.Prefs{}}
	env.inbox.SetList(inboxTestList())
	lib := &servicestest.Library{}
	lib.Seed(services.LibraryCounts{All: 2, Uncategorized: 2}, nil, nil)
	lib.SeedEntries([]services.LibraryEntry{{
		ID: "n1", Title: "待复核一", ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
	}})
	lib.SeedDetail("n1", servicestest.DetailBody{Content: "# 正文\n\n一些内容。"})
	env.f = openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, module
		o.Services.Inbox = env.inbox
		o.Services.Library = lib
		o.Services.AI = &servicestest.AISettingsFake{}
		o.Services.Prefs = env.prefs
		o.Services.OpenExternal = func(url string) error { env.external = append(env.external, url); return nil }
		if wired {
			o.Services.Themed = env.svc
		}
		if mutate != nil {
			mutate(o, env.svc)
		}
	})
	// 无头测试没有 WebView2（Options.Web 为空）就没有阅读岛宿主，读者工具条（原文 / AI 阅读 / 阅读页设置）默认不画；
	// 打开对照开关，让测试走到「AI 阅读」。页面本身由 Area 的无头占位代替，真实岛里的渲染不在这里验证。
	if env.f.root.library != nil {
		env.f.root.library.ReaderToolbar = true
	}
	return env
}

func (e *themedEnv) hasButton(name string) bool { return e.f.hasButtonNamed(name) }

// openDetail 等知识库首屏读到条目，选中 n1 并等详情读到。
func (e *themedEnv) openDetail(t *testing.T) {
	t.Helper()
	store := e.f.root.library.Store
	waitUntil(t, "知识库首屏条目", func() bool { return len(store.View().List.Entries) == 1 })
	e.f.frames(t, 3)
	store.SelectItem("n1")
	waitUntil(t, "详情读到", func() bool { d := store.View().Detail; return d.Item != nil && d.Item.ID == "n1" })
	e.f.frames(t, 4)
}

// 装配了主题阅读服务：同一个界面挂到知识库、处理中心与设置三处。
func TestRootThemedMountsOneScreenOnThreeEntries(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleLibrary, true, nil)
	r := e.f.root
	if r.themed == nil {
		t.Fatal("装配了主题阅读服务应挂主题阅读界面")
	}
	if r.library.Themed != r.themed {
		t.Error("知识库详情应接同一份主题阅读界面")
	}
	if r.inbox.Hooks.ThemedTasks == nil {
		t.Error("处理中心应接「主题页任务」页")
	}
	if r.settings.ReadingSearch == nil {
		t.Error("设置应接「联网搜索」块")
	}
}

// 没装配（自动化实例）：三处入口保持原样，「AI 阅读」与「主题页任务」只说明去向，Close 不崩。
func TestRootWithoutThemedKeepsEntriesAsNotMigrated(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleInbox, false, nil)
	r := e.f.root
	if r.themed != nil || r.library.Themed != nil || r.inbox.Hooks.ThemedTasks != nil || r.settings.ReadingSearch != nil {
		t.Fatal("没有主题阅读服务时不该装配任何入口")
	}
	e.f.waitFrames(t, "处理中心列表出现", func() bool { return e.f.session.HasText("待复核一") })
	e.f.clickButton(t, locales.ThemedReadingTasks())
	if !e.f.session.HasText("B-064") && !strings.Contains(joinTexts(e.f), "B-064") {
		// 提示的详情折叠在「查看详情」里：展开后应写明台账编号。
		e.f.clickButton(t, locales.CommonViewDetail())
	}
	if !strings.Contains(joinTexts(e.f), "B-064") {
		t.Fatalf("未接线的入口应写明归属（B-064）；画面上有 %q", e.f.session.Texts())
	}
	r.Close()
}

func joinTexts(f *rootFixture) string { return strings.Join(f.session.Texts(), "\n") }

// 知识库详情：点「AI 阅读」进入空态引导，生成请求带着这条内容的标识发出。
func TestRootThemedLibraryDetailGeneratesForTheOpenItem(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleLibrary, true, nil)
	f := e.f
	e.openDetail(t)
	f.waitFrames(t, "详情读到", func() bool { return e.hasButton(locales.ThemedReadingReaderAi()) })
	f.clickButton(t, locales.ThemedReadingReaderAi())
	f.waitFrames(t, "空态引导", func() bool { return e.hasButton(locales.ThemedReadingReaderCreate()) })
	f.clickButton(t, locales.ThemedReadingReaderCreate())
	f.waitFrames(t, "设置对话框", func() bool { return e.hasButton(locales.ThemedReadingGenerate()) })
	f.clickButton(t, locales.ThemedReadingGenerate())
	f.waitFrames(t, "生成发出", func() bool { return len(e.svc.Calls().Generates) == 1 })
	if got := e.svc.Calls().Generates[0].Req; got != themedReq {
		t.Fatalf("生成请求 = %+v，想要 %+v", got, themedReq)
	}
	f.waitFrames(t, "任务状态卡", func() bool { return e.hasButton(locales.ThemedReadingStop()) })
}

// 处理中心「主题页任务」：任务页代替列表；点「查看主题页」切到知识库、选中那条内容并直接进入 AI 阅读。
func TestRootThemedInboxTaskOpensTheItemInAIReading(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleInbox, true, func(_ *Options, svc *servicestest.ThemedFake) {
		svc.PutPage(themedReq, services.ThemedPage{ID: "page-1"})
		svc.PutTask(services.ThemedTask{ItemID: "n1", SourceKind: "body", Title: "任务标题一", State: services.ThemedTaskCompleted, Stage: services.ThemedStageDone})
	})
	f := e.f
	f.waitFrames(t, "处理中心列表出现", func() bool { return f.session.HasText("待复核一") })
	f.clickButton(t, locales.ThemedReadingTasks())
	f.waitFrames(t, "任务页出现", func() bool { return f.session.HasText("任务标题一") })
	if f.session.HasText("待复核二") {
		t.Fatal("任务页应代替处理中心的列表")
	}
	f.clickButton(t, locales.ThemedReadingOpen())
	f.waitFrames(t, "切到知识库并选中条目", func() bool {
		d := f.root.library.Store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == "n1"
	})
	f.waitFrames(t, "直接进入 AI 阅读", func() bool {
		return f.root.themed.ModeView().Active && e.f.hasRole(gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	})
	if scope := f.root.library.Store.View().Filters.Scope; scope != services.LibraryScopeAll {
		t.Errorf("范围应设为全部，实际 %q", scope)
	}
	// 条目的阅读记忆也要记成这份内容的 AI 阅读（渲染层 patchContentReadingMemory）：下次打开仍停在这份内容的 AI 阅读。
	if mem, ok := libraryscreen.LoadReadingMemory(e.prefs, "n1", time.Now()); !ok || mem.Tab != libraryscreen.TabBody || !mem.ThemedBySource["body"] {
		t.Errorf("任务对应的内容应记入阅读记忆（正文标签、AI 主题视图）：%+v ok=%v", mem, ok)
	}
	// 离开处理中心后再回来是列表，不是上次的任务页。
	f.root.runOnUI(func() { f.root.win.Module = shell.ModuleInbox })
	f.frames(t, 3)
	f.waitFrames(t, "回到处理中心列表", func() bool { return f.session.HasText("待复核二") })
}

// 去配置模型：生成设置里的「配置模型」收起对话框并打开设置页的 AI 分区。
func TestRootThemedGoConfigureOpensSettingsAI(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleLibrary, true, func(_ *Options, svc *servicestest.ThemedFake) { svc.SetModels("", "") })
	f := e.f
	e.openDetail(t)
	f.waitFrames(t, "详情读到", func() bool { return e.hasButton(locales.ThemedReadingReaderAi()) })
	f.clickButton(t, locales.ThemedReadingReaderAi())
	f.waitFrames(t, "空态引导", func() bool { return e.hasButton(locales.ThemedReadingReaderCreate()) })
	f.clickButton(t, locales.ThemedReadingReaderCreate())
	f.waitFrames(t, "配置入口", func() bool { return e.hasButton(locales.ThemedReadingConfigure()) })
	f.clickButton(t, locales.ThemedReadingConfigure())
	f.waitFrames(t, "打开设置页的 AI 分区", func() bool {
		return f.root.Window().Page == shell.PageSettings && f.root.settings.Active == settingsscreen.SectionAI
	})
	if n := len(e.svc.Calls().Generates); n != 0 {
		t.Fatalf("没有模型不该生成，实际 %d 次", n)
	}
}

// 参考资料里的链接交给 Services.OpenExternal（系统浏览器），不安全的协议不放行。
func TestRootThemedReferenceLinkOpensThroughServices(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleLibrary, true, func(_ *Options, svc *servicestest.ThemedFake) {
		svc.PutPage(themedReq, services.ThemedPage{ID: "page-1"})
		svc.SetReferences(themedReq, services.ThemedReference{ID: "r1", Title: "Primary Source", URL: "https://example.org/a", CapturedAt: 1_700_000_000_000})
	})
	f := e.f
	e.openDetail(t)
	f.waitFrames(t, "AI 阅读工具栏", func() bool { return e.f.hasRole(gfx.RoleCombobox, locales.ThemedReadingReaderSettings()) })
	f.clickRole(t, gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	n, ok := f.session.AccessByRole(gfx.RoleMenuItem, locales.ThemedReadingReaderReferences())
	if !ok {
		t.Fatalf("菜单里应有「参考资料」；画面上有 %q", f.session.Texts())
	}
	if err := f.session.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	f.waitFrames(t, "资料列出", func() bool { return f.session.HasText("Primary Source") })
	f.clickRole(t, gfx.RoleLink, "Primary Source")
	if len(e.external) != 1 || e.external[0] != "https://example.org/a" {
		t.Fatalf("应经 Services.OpenExternal 打开：%q", e.external)
	}
}

// 设置 AI 分区：「联网搜索」块出现，保存只动搜索配置。
func TestRootThemedSettingsShowsSearchBlock(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleLibrary, true, func(o *Options, svc *servicestest.ThemedFake) {
		o.Page = shell.PageSettings
		svc.SetSearch(services.ThemedSearchStatus{Provider: "tavily", Persistent: true})
	})
	f := e.f
	f.root.settings.Active = settingsscreen.SectionAI
	f.frames(t, 3)
	if err := f.session.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: 6000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	f.waitFrames(t, "联网搜索块", func() bool { return f.session.HasText(locales.SettingsReadingSearchTitle()) })
	f.waitFrames(t, "读到搜索配置", func() bool { return f.session.HasText(locales.SettingsReadingSearchDefault()) })
}

// 切走：离开知识库后主题阅读退订进度，不取消任务；Close 不崩。
func TestRootThemedLeavingLibraryDetachesWithoutCancelling(t *testing.T) {
	e := openThemedRoot(t, shell.ModuleLibrary, true, func(_ *Options, svc *servicestest.ThemedFake) {
		svc.PutTask(services.ThemedTask{ItemID: "n1", SourceKind: "body", Title: "任务", State: services.ThemedTaskRunning, Stage: services.ThemedStageWrite})
	})
	f := e.f
	e.openDetail(t)
	f.waitFrames(t, "详情读到", func() bool { return e.hasButton(locales.ThemedReadingReaderAi()) })
	f.clickButton(t, locales.ThemedReadingReaderAi())
	f.waitFrames(t, "订阅进度", func() bool { return e.svc.Subscribers() == 1 })
	f.root.runOnUI(func() { f.root.win.Module = shell.ModuleInbox })
	f.waitFrames(t, "退订", func() bool { return e.svc.Subscribers() == 0 })
	if got := e.svc.Calls().Cancels; len(got) != 0 {
		t.Fatalf("切走不能取消任务：%v", got)
	}
	f.root.Close()
	f.root.Close() // 重复 Close 也不崩
}
