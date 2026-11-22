package app

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 处理中心接进 Root 的几处接线（root_inbox.go）：模块挂的是真的 Screen、rail 角标在任何模块里都准、
// 跨模块回调（打开条目、配置模型、Wiki 预览）、知识库计数变化后合并刷新。Screen 自己的行为在 viva/screens/inbox 里测，这里只测接线。

// inboxTestList 是两条待复核（n1 在知识库里有真条目）加一条采集问题；待处理总数 3。
func inboxTestList() services.InboxList {
	return services.InboxList{
		Items: []services.InboxItem{
			{Kind: services.InboxKindReviewRequired, ID: "review:n1", ItemID: "n1", Title: "待复核一", Reasons: []string{"正文过短"}},
			{Kind: services.InboxKindReviewRequired, ID: "review:n2", ItemID: "n2", Title: "待复核二", Reasons: []string{"标题缺失"}},
			{Kind: services.InboxKindImportIssue, ID: "task:t1", TaskID: "t1", Title: "采集一", Status: "failed", Message: "网络超时"},
		},
		Counts: services.InboxCounts{ReviewRequired: 2, ImportIssue: 1},
		Total:  3,
	}
}

// openInboxRoot 打开 Root：处理中心与知识库都有数据，起始模块由 module 给出。
func openInboxRoot(t *testing.T, module shell.Module) (*rootFixture, *servicestest.Inbox, *servicestest.Library) {
	t.Helper()
	inbox := &servicestest.Inbox{}
	inbox.SetList(inboxTestList())
	lib := &servicestest.Library{}
	lib.Seed(services.LibraryCounts{All: 2, Uncategorized: 2}, nil, nil)
	lib.SeedEntries([]services.LibraryEntry{{
		ID: "n1", Title: "待复核一", ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		ReviewStatus: "needs_review", ReviewReasons: []string{}, CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
	}})
	lib.SeedDetail("n1", servicestest.DetailBody{Content: "正文"})
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, module
		o.Services.Inbox = inbox
		o.Services.Library = lib
		o.Services.Prefs = &servicestest.Prefs{}
	})
	return f, inbox, lib
}

// clickButton 点名字为 name 的按钮，随后出几帧。
func (f *rootFixture) clickButton(t *testing.T, name string) {
	t.Helper()
	n, ok := f.session.AccessByRole(gfx.RoleButton, name)
	if !ok {
		t.Fatalf("找不到按钮「%s」", name)
	}
	if err := f.session.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
}

// 处理中心模块挂的是真的界面（不是「尚未迁移」占位）：进入后读到列表、卡片出现。
func TestRootInboxModuleMountsTheRealScreen(t *testing.T) {
	f, svc, _ := openInboxRoot(t, shell.ModuleInbox)
	f.waitFrames(t, "处理中心列表出现", func() bool { return f.session.HasText("待复核一") })
	if svc.CallCount("List") == 0 {
		t.Error("进入处理中心应读过列表")
	}
	if f.session.HasText(locales.NativeShellNotMigrated(locales.NavInbox())) {
		t.Error("处理中心已接入，不应再显示「尚未迁移」占位")
	}
	if !f.session.HasText("待复核二") || !f.session.HasText("采集一") {
		t.Errorf("三张卡片都应出现，现有文字：%v", f.session.Texts())
	}
}

// rail 角标取自处理中心的待处理总数，而且不用先进处理中心：在知识库模块里也已读好。
func TestRootInboxBadgeIsFedWithoutOpeningTheModule(t *testing.T) {
	f, _, _ := openInboxRoot(t, shell.ModuleLibrary)
	f.waitFrames(t, "rail 角标取到待处理总数", func() bool { return f.root.Window().InboxTotal == 3 })
}

// 点「打开」：切到知识库、范围设为全部、选中那条笔记（渲染层 openItem）。
func TestRootInboxOpenItemGoesToLibraryAndSelectsIt(t *testing.T) {
	f, _, _ := openInboxRoot(t, shell.ModuleInbox)
	f.waitFrames(t, "处理中心列表出现", func() bool { return f.session.HasText("待复核一") })
	// 第一张卡片的「打开」；按钮重名，取画面上最靠前的一个。
	open, ok := f.session.AccessByRole(gfx.RoleButton, locales.CommonOpen())
	if !ok {
		t.Fatal("找不到「打开」按钮")
	}
	if err := f.session.Click(open.Bounds.X+open.Bounds.Width/2, open.Bounds.Y+open.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	store := f.root.library.Store
	f.waitFrames(t, "知识库里选中条目", func() bool {
		d := store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == "n1"
	})
	if scope := store.View().Filters.Scope; scope != services.LibraryScopeAll {
		t.Errorf("范围应设为全部，实际 %q", scope)
	}
}

// Wiki 模型没配好：横幅上的「去配置模型」打开设置页的 AI 分区。
func TestRootInboxConfigureModelOpensSettingsAI(t *testing.T) {
	f, svc, _ := openInboxRoot(t, shell.ModuleInbox)
	svc.SetWikiCandidates(services.InboxWikiCandidates{Waiting: 2})
	svc.SetWikiModelReady(false)
	f.waitFrames(t, "处理中心列表出现", func() bool { return f.session.HasText("待复核一") })
	// 进入时只读一次；候选数据是 Mount 之后才改的，刷新一遍让横幅出现。
	f.root.inboxStore.Refresh(f.root.ctx)
	f.waitFrames(t, "配置模型入口出现", func() bool {
		_, ok := f.session.AccessByRole(gfx.RoleButton, locales.InboxWikiConfigureModel())
		return ok
	})
	f.clickButton(t, locales.InboxWikiConfigureModel())
	f.waitFrames(t, "打开设置页的 AI 分区", func() bool {
		return f.root.Window().Page == shell.PageSettings && f.root.settings.Active == settingsscreen.SectionAI
	})
}

// 没有装配 Wiki 服务的实例：点「查看范围和任务」只说明去向（提示条标题是「操作失败」，详情说明尚未迁移，与其他未迁移入口一致），不跳转、不崩。
// 装配了 Wiki 的实例改为打开编译对话框，见 root_wiki_test.go 的 TestRootInboxWikiPreviewOpensCompileDialog。
func TestRootInboxWikiPreviewSaysItIsNotMigrated(t *testing.T) {
	f, svc, _ := openInboxRoot(t, shell.ModuleInbox)
	svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3})
	svc.SetWikiModelReady(true)
	f.waitFrames(t, "处理中心列表出现", func() bool { return f.session.HasText("待复核一") })
	f.root.inboxStore.Refresh(f.root.ctx)
	f.waitFrames(t, "Wiki 横幅出现", func() bool {
		_, ok := f.session.AccessByRole(gfx.RoleButton, locales.InboxWikiCandidatesView())
		return ok
	})
	f.clickButton(t, locales.InboxWikiCandidatesView())
	f.waitFrames(t, "提示条说明未迁移", func() bool {
		return f.session.HasText(locales.CommonOperationFailed(locales.InboxWikiCandidatesView()))
	})
	if f.root.Window().Module != shell.ModuleInbox {
		t.Errorf("预览未迁移时应留在处理中心，实际在 %q", f.root.Window().Module)
	}
}

// 知识库计数变了（新建、删除、归类）：合并刷新处理中心。等待时长是包变量，测试里缩短。
func TestRootInboxRefreshesWhenLibraryCountsChange(t *testing.T) {
	old := inboxCountsDelay
	inboxCountsDelay = 20 * time.Millisecond
	t.Cleanup(func() { inboxCountsDelay = old })

	f, svc, lib := openInboxRoot(t, shell.ModuleLibrary)
	store := f.root.library.Store
	f.waitFrames(t, "知识库计数与处理中心都读到", func() bool { return store.View().Counts != nil && svc.CallCount("List") > 0 })
	f.frames(t, 3) // 让监听记下首次计数
	before := svc.CallCount("List")

	lib.SetCounts(services.LibraryCounts{All: 3, Uncategorized: 3})
	store.RefreshCounts(f.root.ctx)
	f.waitFrames(t, "计数变化后处理中心再读一次", func() bool { return svc.CallCount("List") > before })

	// 计数没变就不再刷新：再刷新一次知识库计数，等一段时间，处理中心的读取次数不增加。
	after := svc.CallCount("List")
	store.RefreshCounts(f.root.ctx)
	f.frames(t, 3)
	time.Sleep(80 * time.Millisecond)
	f.frames(t, 2)
	if got := svc.CallCount("List"); got != after {
		t.Errorf("计数没变不应刷新处理中心：读取 %d 次 → %d 次", after, got)
	}
}
