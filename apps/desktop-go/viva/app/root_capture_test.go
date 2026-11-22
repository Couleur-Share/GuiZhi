package app

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 快速采集对话框接进 Root 的接线（root_capture.go、root.go、root_shell.go、app.go 的 bindInput）：
// 顶栏新建与菜单、托盘与快捷键命令、采集页的入口都开对话框；空白笔记切到知识库新建；系统文件拖放交给草稿框。
// 对话框自己的行为在 viva/screens/capture 里测，这里只测接线。

// openCaptureRoot 在指定模块的主页打开 Root：采集与知识库服务都在（两个合集，一条笔记）。
func openCaptureRoot(t *testing.T, module shell.Module, mutate ...func(*Options)) (*rootFixture, *servicestest.Imports, *servicestest.Library) {
	t.Helper()
	imports := &servicestest.Imports{}
	imports.Set(services.ImportPage{Counts: map[string]int64{}}, nil)
	lib := &servicestest.Library{}
	lib.Seed(services.LibraryCounts{All: 1, Uncategorized: 1},
		[]services.LibraryCollection{{ID: "c1", Name: "工作"}, {ID: "c2", Name: "灵感"}}, nil)
	lib.SeedEntries([]services.LibraryEntry{{
		ID: "n1", Title: "一条笔记", ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
	}})
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, module
		o.Services.Imports, o.Services.Library, o.Services.Prefs = imports, lib, &servicestest.Prefs{}
		for _, m := range mutate {
			m(o)
		}
	})
	f.frames(t, 6)
	return f, imports, lib
}

func (f *rootFixture) captureOpen() bool {
	_, ok := f.session.AccessByRole(gfx.RoleHeading, locales.CaptureTitle())
	return ok
}

func (f *rootFixture) clickRole(t *testing.T, role gfx.Role, name string) {
	t.Helper()
	n, ok := f.session.AccessByRole(role, name)
	if !ok {
		t.Fatalf("找不到 %v「%s」；画面上有 %q", role, name, f.session.Texts())
	}
	if err := f.session.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
}

// 顶栏「新建」主按钮打开对话框；Esc 关闭。
func TestRootTopBarNewButtonOpensTheCaptureDialog(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary)
	if f.captureOpen() {
		t.Fatal("没点之前不该有对话框")
	}
	f.clickRole(t, gfx.RoleButton, locales.HeaderNew())
	if !f.captureOpen() {
		t.Fatalf("点「新建」应打开快速采集；画面上有 %q", f.session.Texts())
	}
	if err := f.session.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if f.captureOpen() {
		t.Fatal("Esc 应关闭对话框")
	}
}

// 创建方式菜单：「快速采集」开对话框。
func TestRootCreateMenuQuickCaptureOpensTheDialog(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary)
	f.clickRole(t, gfx.RoleCombobox, locales.HeaderCreateMenu())
	if err := f.session.ClickLabel(locales.CaptureTitle()); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if !f.captureOpen() {
		t.Fatalf("菜单里的「快速采集」应打开对话框；画面上有 %q", f.session.Texts())
	}
}

// 创建方式菜单：「空白笔记」从别的模块切到知识库，新建一条不归合集的条目并打开它。
func TestRootCreateMenuBlankNoteSwitchesToTheLibraryAndOpensTheNewItem(t *testing.T) {
	f, _, lib := openCaptureRoot(t, shell.ModuleImports)
	f.clickRole(t, gfx.RoleCombobox, locales.HeaderCreateMenu())
	if err := f.session.ClickLabel(locales.CaptureBlankNote()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "新建了空白条目", func() bool { return len(lib.Creates()) == 1 })
	store := f.root.library.Store
	waitUntil(t, "新条目被选中并打开", func() bool { d := store.View().Detail; return d.Item != nil && d.Item.ID == "item-new-1" })
	f.frames(t, 4)
	if f.root.Window().Module != shell.ModuleLibrary || f.root.Window().Page != shell.PageHome {
		t.Fatalf("应切到知识库主页：%v %v", f.root.Window().Module, f.root.Window().Page)
	}
	if got := lib.Creates()[0].CollectionID; got != "" {
		t.Fatalf("顶栏的空白笔记不归合集：%q", got)
	}
}

// 创建方式菜单：「采集文件」选完文件直接入队。
func TestRootCreateMenuImportFilesEnqueuesTheChosenFiles(t *testing.T) {
	f, imports, _ := openCaptureRoot(t, shell.ModuleLibrary)
	imports.SetFiles([]string{`C:\notes\a.md`, `C:\notes\b.png`})
	f.clickRole(t, gfx.RoleCombobox, locales.HeaderCreateMenu())
	if err := f.session.ClickLabel(locales.HeaderNewImportFiles()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "文件入队", func() bool { return len(imports.Enqueued()) == 2 })
	waitUntil(t, "成功提示", func() bool {
		f.frames(t, 1)
		return f.session.HasText(locales.CaptureEnqueued(2))
	})
	if f.captureOpen() {
		t.Fatal("采集文件不该打开对话框")
	}
}

// 托盘「新建」：先回到主页再开对话框；快捷键只开对话框、不切页面。
func TestRootNewItemCommandsOpenTheDialog(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) { o.Page = shell.PageSettings })
	f.root.dispatch(CommandNewItem)
	f.frames(t, 4)
	if f.root.Window().Page != shell.PageHome || !f.captureOpen() {
		t.Fatalf("item:new 应回到主页并开对话框：page=%v open=%v", f.root.Window().Page, f.captureOpen())
	}
	if err := f.session.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)

	f.root.dispatch(CommandOpenSettings)
	f.frames(t, 3)
	f.root.dispatch(CommandShortcutNewItem)
	f.frames(t, 4)
	if f.root.Window().Page != shell.PageSettings || !f.captureOpen() {
		t.Fatalf("快捷键应在设置页上直接开对话框：page=%v open=%v", f.root.Window().Page, f.captureOpen())
	}
}

// 没有采集服务时命令仍按「尚未迁移」留痕（TestRootDispatchCommands 覆盖），也没有拖放目标。
func TestRootWithoutImportsHasNoCaptureDialogOrDropTarget(t *testing.T) {
	f := openRoot(t, func(o *Options) { o.Page, o.Module = shell.PageHome, shell.ModuleLibrary })
	if f.root.capture != nil {
		t.Fatal("没有采集服务就不该装配对话框")
	}
	if taken, err := f.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 600, Y: 400}); err != nil || taken {
		t.Fatalf("没有对话框时窗口不该接收系统文件：taken=%v err=%v", taken, err)
	}
}

// 采集页：空状态与二级侧栏里的「快速采集」都开对话框。
func TestRootImportsPageEntriesOpenTheDialog(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleImports)
	f.clickRole(t, gfx.RoleButton, locales.ImportsPanelCapture())
	if !f.captureOpen() {
		t.Fatalf("侧栏的「%s」应开对话框；画面上有 %q", locales.ImportsPanelCapture(), f.session.Texts())
	}
	if err := f.session.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	waitUntil(t, "空状态里的入口出现", func() bool {
		f.frames(t, 1)
		_, ok := f.session.AccessByRole(gfx.RoleButton, locales.CaptureTitle())
		return ok
	})
	f.clickRole(t, gfx.RoleButton, locales.CaptureTitle())
	if !f.captureOpen() {
		t.Fatalf("空状态的「%s」应开对话框", locales.CaptureTitle())
	}
}

// 对话框里的「空白笔记」：带着所选合集切到知识库新建条目并打开，对话框关闭。
func TestRootDialogBlankNoteCreatesInTheChosenCollection(t *testing.T) {
	f, _, lib := openCaptureRoot(t, shell.ModuleImports)
	f.root.dispatch(CommandShortcutNewItem)
	f.frames(t, 4)
	waitUntil(t, "合集读到", func() bool {
		f.frames(t, 1)
		return slices.Contains(lib.Calls(), "Collections")
	})
	f.clickRole(t, gfx.RoleCombobox, locales.LibraryCollection())
	hits := f.session.Snapshot().Hits
	var option gfx.Hit
	for _, h := range hits {
		if h.Role == gfx.RoleOption && h.Label == "灵感" {
			option = h
		}
	}
	if option.ID.IsZero() {
		t.Fatalf("合集下拉里没有「灵感」：%q", f.session.Texts())
	}
	if err := f.session.Click(option.Bounds.X+option.Bounds.Width/2, option.Bounds.Y+option.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	f.clickRole(t, gfx.RoleButton, locales.CaptureBlankNote())
	waitUntil(t, "在所选合集里新建", func() bool { return len(lib.Creates()) == 1 })
	f.frames(t, 4)
	if got := lib.Creates()[0].CollectionID; got != "c2" {
		t.Fatalf("应在合集 c2 里新建：%q", got)
	}
	if f.captureOpen() || f.root.Window().Module != shell.ModuleLibrary {
		t.Fatalf("应关闭对话框并切到知识库：open=%v module=%v", f.captureOpen(), f.root.Window().Module)
	}
}

// 提交文字：Ctrl+Enter 入队，对话框关闭，队列随后刷新。
func TestRootCaptureSubmitEnqueuesAndRefreshesTheQueue(t *testing.T) {
	f, imports, _ := openCaptureRoot(t, shell.ModuleLibrary)
	f.root.dispatch(CommandShortcutNewItem)
	f.frames(t, 4)
	if err := f.session.Type("随手记一笔"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 2)
	before := len(imports.Queries())
	if err := f.session.Keys("ctrl+enter"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "入队", func() bool { return len(imports.Enqueued()) == 1 })
	got := imports.Enqueued()[0]
	if got.Kind != "text" || got.Input != "随手记一笔" {
		t.Fatalf("入队内容：%+v", got)
	}
	waitUntil(t, "队列刷新", func() bool { return len(imports.Queries()) > before })
	waitUntil(t, "对话框关闭", func() bool {
		f.frames(t, 1)
		return !f.captureOpen()
	})
}

// 系统文件拖放：bindInput 把窗口的 OnFileDrop 接给对话框；对话框没开时不接收，开了之后只在草稿框上接收。
func TestRootFileDropGoesToTheOpenDialog(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary)
	if taken, _ := f.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 640, Y: 300}); taken {
		t.Fatal("对话框没开时不该接收文件")
	}
	f.root.dispatch(CommandShortcutNewItem)
	f.frames(t, 4)
	box, ok := f.session.AccessByRole(gfx.RoleTextField, locales.CapturePlaceholder())
	if !ok {
		t.Fatal("找不到草稿框")
	}
	x, y := box.Bounds.X+box.Bounds.Width/2, box.Bounds.Y+box.Bounds.Height/2
	if taken, err := f.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: x, Y: y}); err != nil || !taken {
		t.Fatalf("悬停在草稿框上应接收：taken=%v err=%v", taken, err)
	}
	if taken, err := f.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: x, Y: y, Paths: []string{`C:\drop\a.txt`}}); err != nil || !taken {
		t.Fatalf("落在草稿框上应接收：taken=%v err=%v", taken, err)
	}
	f.frames(t, 3)
	if !f.session.HasText(`C:\drop\a.txt`) {
		t.Fatalf("落下的文件应列出；画面上有 %q", f.session.Texts())
	}
	if !slices.Contains(f.session.Texts(), locales.CaptureSubmit()) {
		t.Fatal("「采集」按钮应在")
	}
}
