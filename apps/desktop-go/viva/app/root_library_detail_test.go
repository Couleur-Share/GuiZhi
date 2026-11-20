package app

import (
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 知识库详情接进 Root 的几处接线（root.go 的 mountLibrary 与 bindInput）：编辑器默认预览取自设置、来源链接经 Services.OpenExternal、
// 带修饰键的快捷键走窗口的 OnShortcut、专注阅读时二级侧栏让位。Screen 自己的行为在 viva/screens/library 里测，这里只测接线。

const detailTestTitle = "一条笔记"

// openLibraryDetailRoot 在知识库模块里打开 Root：一条笔记，来源链接是 https://example.com/a；mutate 可改选项。
func openLibraryDetailRoot(t *testing.T, mutate func(*Options)) (*rootFixture, *servicestest.Library) {
	t.Helper()
	fake := &servicestest.Library{}
	fake.Seed(services.LibraryCounts{All: 1, Uncategorized: 1}, nil, nil)
	fake.SeedEntries([]services.LibraryEntry{{
		ID: "n1", Title: detailTestTitle, ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
	}})
	fake.SeedDetail("n1", servicestest.DetailBody{Content: "正文", SourceURI: "https://example.com/a"})
	f := openRoot(t, func(o *Options) {
		o.Page = shell.PageHome
		o.Services.Library = fake
		o.Services.Prefs = &servicestest.Prefs{}
		if mutate != nil {
			mutate(o)
		}
	})
	store := f.root.library.Store
	waitUntil(t, "知识库首屏条目", func() bool { return len(store.View().List.Entries) == 1 })
	f.frames(t, 4)
	return f, fake
}

// openDetail 点卡片选中那条笔记，等详情读到。
func (f *rootFixture) openDetail(t *testing.T) {
	t.Helper()
	node, ok := f.session.AccessByRole(gfx.RoleButton, detailTestTitle)
	if !ok {
		t.Fatalf("找不到卡片「%s」", detailTestTitle)
	}
	if err := f.session.Click(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	store := f.root.library.Store
	waitUntil(t, "详情读到", func() bool { d := store.View().Detail; return d.Item != nil && d.Item.ID == "n1" })
	f.frames(t, 4)
}

// 编辑器默认预览取自设置（每次现读）。这个设置没有界面入口，规范化一律把它翻成开（渲染层 normalizeMergedPresentationSettings 同），
// 所以只能测接上了线、取到的是开；以后加设置界面、规范化放开之后，要补「改了设置下一次打开条目就跟着变」。
func TestLibraryEditorPreviewIsWiredToTheSetting(t *testing.T) {
	f, _ := openLibraryDetailRoot(t, nil)
	if preview := f.root.library.EditorPreview; preview == nil || !preview() {
		t.Fatal("「编辑器默认预览」应已接线且为开")
	}
}

// 来源链接经 Services.OpenExternal 交给系统浏览器。
func TestLibrarySourceLinkGoesThroughServicesOpenExternal(t *testing.T) {
	var mu sync.Mutex
	var opened []string
	f, _ := openLibraryDetailRoot(t, func(o *Options) {
		o.Services.OpenExternal = func(u string) error {
			mu.Lock()
			defer mu.Unlock()
			opened = append(opened, u)
			return nil
		}
	})
	f.openDetail(t)
	link, ok := f.session.AccessByRole(gfx.RoleLink, "example.com")
	if !ok {
		t.Fatal("详情头部应有来源链接")
	}
	if err := f.session.Click(link.Bounds.X+link.Bounds.Width/2, link.Bounds.Y+link.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 || opened[0] != "https://example.com/a" {
		t.Fatalf("应经 Services.OpenExternal 打开来源链接，实际 %v", opened)
	}
}

// Ctrl+S 与 Alt+Z 是带修饰键的可打印字符，Viva 不把它们交给 OnKey，只走 OnShortcut：窗口接线（bindInput）漏了这一路它们就永远不会触发。
func TestLibraryDetailShortcutsArriveThroughOnShortcut(t *testing.T) {
	f, fake := openLibraryDetailRoot(t, nil)
	f.openDetail(t)
	// 自动保存关着：防抖不会替我们落盘，只有 Ctrl+S 能触发保存。
	if err := f.root.store.Update(func(v *settings.Values) { v.UI.AutoSave = false }); err != nil {
		t.Fatal(err)
	}
	title, ok := f.session.AccessByRole(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	if !ok {
		t.Fatal("详情头部应有标题框")
	}
	if err := f.session.Click(title.Bounds.X+title.Bounds.Width/2, title.Bounds.Y+title.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := f.session.Keys("end"); err != nil {
		t.Fatal(err)
	}
	if err := f.session.Type("!"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if n := len(fake.Drafts()); n != 0 {
		t.Fatalf("自动保存关着、还没按 Ctrl+S，不该保存，实际 %d 次", n)
	}
	if err := f.session.Keys("ctrl+s"); err != nil {
		t.Fatalf("Ctrl+S 应被窗口的 OnShortcut 接住：%v", err)
	}
	waitUntil(t, "Ctrl+S 落盘", func() bool { return len(fake.Drafts()) == 1 })

	if err := f.session.Keys("alt+z"); err != nil {
		t.Fatalf("Alt+Z 应被窗口的 OnShortcut 接住：%v", err)
	}
	if !f.root.library.FocusReading() {
		t.Fatal("Alt+Z 应进入专注阅读")
	}
}

// 专注阅读时整块二级侧栏不挂载（渲染层 App.tsx 的 !(isFocusReadingMode && appModule === "library")），顶栏照常；退出之后回来。
func TestLibraryFocusReadingHidesTheSecondaryPanel(t *testing.T) {
	f, _ := openLibraryDetailRoot(t, nil)
	f.openDetail(t)
	scope := locales.LibraryScopeAllShort()
	if !f.session.HasText(scope) {
		t.Fatalf("夹具失效：二级侧栏里应有「%s」", scope)
	}
	if err := f.session.Keys("alt+z"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if f.session.HasText(scope) {
		t.Fatal("专注阅读时二级侧栏不该挂载")
	}
	if _, ok := f.session.AccessByRole(gfx.RoleButton, locales.CommonCollapse()); !ok {
		t.Fatal("专注阅读时顶栏（侧栏收起钮）照常显示")
	}
	if err := f.session.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if !f.session.HasText(scope) {
		t.Fatal("退出专注阅读之后二级侧栏应回来")
	}
}
