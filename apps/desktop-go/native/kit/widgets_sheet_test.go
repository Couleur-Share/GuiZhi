package kit

import (
	"fmt"
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
)

// sheetCase 是联系表里的一格：每帧画的内容，加上第一帧之后要做的交互（展开详情、开始录制等）。
type sheetCase struct {
	name  string
	w, h  int
	build func(l *Look) (view func(c *ui.Context), after func(tr *ui.Tester))
}

// page 是各格共用的底：一块有内容的页面，盖在它上面的对话框、面板才看得出遮罩与层次。
func sheetPage(c *ui.Context, l *Look) {
	ui.Column(c).Fill().Background(l.T.Background).Padding(24).Gap(10).Children(func() {
		ui.Text(c, "知识库").FontSize(20).FontWeight(600).TextColor(l.T.Foreground)
		for _, s := range []string{"Go 并发模式：从 goroutine 到 pipeline", "MyGo 原生窗口的内存剖面", "归知迁移原生 GUI 计划"} {
			ui.Row(c).Padding(12).Radius(8).Background(l.T.UiSurfaceBg).BorderWidth(1, 1, 1, 1).BorderColor(l.T.UiSurfaceBorder).Children(func() {
				ui.Text(c, s).FontSize(14).TextColor(l.T.Foreground)
			})
		}
	})
}

func sheetCases() []sheetCase {
	return []sheetCase{
		{name: "toast", w: 640, h: 420, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			n := NewNotifier(nil)
			n.Success("已保存到知识库")
			n.Warning("2 条导入没有正文，已跳过")
			n.Error("无法连接模型服务", "HTTP 401 Unauthorized\n{\"error\":{\"message\":\"Incorrect API key provided: sk-****\"}}")
			return func(c *ui.Context) { sheetPage(c, l); ToastViewport(c, l, n) },
				func(tr *ui.Tester) { _ = tr.Click(locales.CommonViewDetail()); tr.Frame() }
		}},
		{name: "toast-actions", w: 640, h: 300, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			n := NewNotifier(nil)
			n.Info("已复制到剪贴板")
			n.Undo("已删除 3 条笔记", locales.CommonUndo(), func() {})
			return func(c *ui.Context) { sheetPage(c, l); ToastViewport(c, l, n) }, nil
		}},
		{name: "confirm", w: 640, h: 360, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			d := NewConfirm(nil)
			d.Open(ConfirmOptions{Title: "立即完整备份？", Message: "会备份知识库与设置，耗时取决于库的大小。", OnConfirm: func() {}})
			return func(c *ui.Context) { sheetPage(c, l); d.View(c, l) }, nil
		}},
		{name: "confirm-destructive", w: 640, h: 360, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			d := NewConfirm(nil)
			d.Open(ConfirmOptions{Title: "删除 3 条笔记？", Message: "删除后可在回收站找回 30 天。", Destructive: true, ConfirmText: "删除", OnConfirm: func() {}})
			return func(c *ui.Context) { sheetPage(c, l); d.View(c, l) }, nil
		}},
		{name: "unsaved", w: 640, h: 360, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			d := NewUnsaved(nil)
			d.Open(UnsavedOptions{Save: func() error { return nil }})
			return func(c *ui.Context) { sheetPage(c, l); d.View(c, l) }, nil
		}},
		{name: "palette", w: 760, h: 520, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			p := NewPalette(paletteFixture(map[string]int{}))
			p.Open()
			return func(c *ui.Context) { sheetPage(c, l); p.View(c, l) }, nil
		}},
		{name: "palette-query", w: 760, h: 360, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			p := NewPalette(paletteFixture(map[string]int{}))
			p.Open()
			return func(c *ui.Context) { sheetPage(c, l); p.View(c, l) },
				func(tr *ui.Tester) { tr.Type("备份"); tr.Frame() }
		}},
		{name: "shortcut-field", w: 520, h: 230, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			set, recording, invalid := NewShortcutField(), NewShortcutField(), NewShortcutField()
			return func(c *ui.Context) {
					ui.Column(c).Fill().Background(l.T.Background).Padding(24).Gap(14).Children(func() {
						row := func(label string, f *ShortcutField, o ShortcutFieldOptions) {
							ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
								ui.Text(c, label).FontSize(14).TextColor(l.T.Foreground).Width(120)
								o.Label = label
								f.View(c, l, o)
							})
						}
						row("全局搜索", set, ShortcutFieldOptions{Value: "CommandOrControl+Shift+K", OnChange: func(string) {}, OnClear: func() {}})
						row("快速采集", recording, ShortcutFieldOptions{Value: "Alt+Shift+P", OnChange: func(string) {}, OnClear: func() {}})
						row("显示窗口", invalid, ShortcutFieldOptions{Value: "", Invalid: true, OnChange: func(string) {}})
					})
				},
				func(tr *ui.Tester) {
					_ = tr.Click(locales.SettingsShortcutInputLabel("快速采集"))
					tr.CodeDown(ui.Ctrl, ui.CodeControlLeft)
					tr.CodeDown(ui.Ctrl|ui.Shift, ui.CodeShiftLeft)
					tr.Frame()
				}
		}},
		{name: "palette-icons", w: 360, h: 120, build: func(l *Look) (func(*ui.Context), func(*ui.Tester)) {
			// 命令面板用到的图标：确认矢量描边在明暗两种底色上都清楚
			return func(c *ui.Context) {
				ui.Row(c).Fill().Background(l.T.Background).Padding(24).Gap(18).AlignItems(ui.Center).Children(func() {
					for _, n := range []icons.Name{icons.Search, icons.Sparkles, icons.DatabaseBackup, icons.Settings, icons.ChevronDown} {
						Icon(c, n, 24).TextColor(l.T.Foreground)
					}
				})
			}, nil
		}},
	}
}

// TestWidgetsSheet 在设置了 GUIZHI_WIDGET_SHEET=前缀 时，把共享控件的各种状态在明、暗两种外观下各画一张图
// （前缀-名称-light.png / 前缀-名称-dark.png），供人眼核对；平时跳过。
func TestWidgetsSheet(t *testing.T) {
	prefix := os.Getenv("GUIZHI_WIDGET_SHEET")
	if prefix == "" {
		t.Skip("未设置 GUIZHI_WIDGET_SHEET")
	}
	for _, dark := range []bool{false, true} {
		l, mode := lightLook(), "light"
		if dark {
			l, mode = darkLook(), "dark"
		}
		for _, sc := range sheetCases() {
			resetCapture(t)
			view, after := sc.build(l)
			tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(l.Theme); view(c) }, sc.w, sc.h)
			tr.Frame()
			if after != nil {
				after(tr)
			}
			tr.Frame()
			f, err := os.Create(fmt.Sprintf("%s-%s-%s.png", prefix, sc.name, mode))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, tr.Image()); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
