package app

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 有采集服务、没有知识库服务（装配不全的自动化实例）：对话框可用，但空白笔记要知识库来建，
// 所以对话框里没有「空白笔记」按钮，顶栏创建菜单的这一项也没有动作。
func TestRootCaptureWithoutLibraryHasNoBlankNoteEntry(t *testing.T) {
	imports := &servicestest.Imports{}
	imports.Set(services.ImportPage{Counts: map[string]int64{}}, nil)
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, shell.ModuleImports
		o.Services.Imports, o.Services.Prefs = imports, &servicestest.Prefs{}
	})
	f.frames(t, 6)
	if f.root.capture == nil {
		t.Fatal("有采集服务就应装配对话框")
	}
	if f.root.capture.Hooks.BlankNote != nil || f.root.win.OnNewBlankNote != nil {
		t.Fatal("没有知识库时不该接空白笔记入口")
	}
	if !f.root.openCapture() {
		t.Fatal("对话框应能打开")
	}
	f.frames(t, 4)
	if !f.captureOpen() {
		t.Fatalf("应显示快速采集；画面上有 %q", f.session.Texts())
	}
	if _, ok := f.session.AccessByRole(gfx.RoleButton, locales.CaptureBlankNote()); ok {
		t.Fatal("没有知识库时对话框不该显示「空白笔记」")
	}
}
