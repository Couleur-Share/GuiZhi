// Package scenes 登记可对照的 Viva 屏幕：每个条目的 ID 与 React 采集器（scripts/parity/scenes/react-scenes.mjs）里的场景 ID 一致。
//
// Package scenes registers the Viva screens that can be compared with the renderer.
package scenes

import (
	"fmt"
	"sort"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// pageSettingsWaivers 是整窗设置页（顶栏带快捷键提示）共有的已登记偏差。
var pageSettingsWaivers = []parity.Waiver{
	{
		Ledger: "A-006", Layer: parity.LayerL2, Kind: "text-width", Subject: "Alt+Shift+F",
		Note: "快捷键提示是固定间距文字，Viva 把字格宽度取整（5.498→5px），一行 11 个字符窄 5.48px；行盒与外框宽度已按渲染层留出",
	},
	{
		Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: "Alt+Shift+F",
		Note: "同上：字格取整使整行墨迹整体偏短，左缘与每个字符的位置累计最多差一个字格（5.5px）",
	},
}

// overlayInkWaiver 登记 A-009：渲染层的浮层（fixed + 圆角裁切）在独立合成层里，文字用灰度抗锯齿；
// Viva 的 LCD / 灰度是整窗开关（渲染器按窗口清屏色一次决定），同一帧里无法只让浮层用灰度，
// 所以浮层文字的墨迹总量系统性偏大（2026-11-16 在 select-close-action 的 36 个变体里实测 1.15–1.31 倍）。
// 只放过浮层里文字的墨迹总量比；位置、字重、颜色、字号照常判定，页面层的同一段文字也照常判定。
var overlayInkWaiver = parity.Waiver{
	Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Overlay: true,
	Note: "浮层文字：渲染层灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（位置、字重、颜色、字号均一致）",
}

// withOverlayInk 在 waivers 之外再加上 overlayInkWaiver，返回新切片（不改动 waivers 的底层数组：它被多个场景共用）。
func withOverlayInk(waivers []parity.Waiver) []parity.Waiver {
	return append(append([]parity.Waiver{}, waivers...), overlayInkWaiver)
}

// confirmBackdrop 是 ConfirmDialog 的遮罩（bg-background/60 加 backdrop-blur-sm，4px）：遮罩之上是名为 name 的对话框面板（台账 A-007）。
// 对话框由 Scene.Steps 点开，打开之前的页面不需要任何操作（BaseSteps 为空）。
func confirmBackdrop(name string) *parity.BackdropSpec {
	return &parity.BackdropSpec{Sigma: 4, ScrimAlpha: 0.6, PanelRole: "group", PanelName: name}
}

// withBackdrop 给场景登记遮罩背后的模糊。
func withBackdrop(s parity.Scene, b *parity.BackdropSpec) parity.Scene {
	s.Backdrop = b
	return s
}

// pageSettings 是整窗（顶栏 + rail + 设置页）的场景；steps 非空时在首次稳定后按它操作界面。
func pageSettings(id string, steps func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			w := &shellscreen.Window{
				Look: look, Page: shellscreen.PageSettings, OwnControls: true, SearchShortcut: "Alt+Shift+F",
				Settings: &settingsscreen.Shell{Look: look, Store: settingsscreen.NewMemStore(settings.Default())},
			}
			return w.View
		},
		Steps:   steps,
		Waivers: append(append([]parity.Waiver{}, pageSettingsWaivers...), waivers...),
	}
}

// openSelect 是「点开设置页里名为 name 的下拉并把指针移开」，与渲染层的 openSettingsSelect 对应。
// scrollEnd 为真时先把设置内容滚到底（语言下拉在窗口底部，不滚就被视口边缘截住）。
func openSelect(name string, scrollEnd bool) func(s *headless.Session) error {
	return func(s *headless.Session) error {
		if scrollEnd {
			if err := parity.ScrollDown(s, 700, 400, 4000); err != nil {
				return err
			}
			if err := s.Rest(); err != nil {
				return err
			}
		}
		if err := parity.ClickRole(s, gfx.RoleCombobox, name); err != nil {
			return err
		}
		return parity.ParkPointer(s)
	}
}

// openBackgroundTasksDialog 是「把设置页的『后台任务』开关从关打开，弹出确认对话框并把指针移开」，
// 与渲染层的 openBackgroundTasksDialog 对应。
func openBackgroundTasksDialog(s *headless.Session) error {
	if err := parity.ClickRole(s, gfx.RoleSwitch, "后台任务"); err != nil {
		return err
	}
	return parity.ParkPointer(s)
}

// toastMonoWaivers 登记提示详情原文（font-mono，11px）的 A-006 偏差：Viva 的字格取整，整行偏短、字形位置累计差一个字格。
// 折行位置与行数由 kit.WrapMono 按渲染层的规则决定，不受影响（外框、行数仍然逐项比较）。
var toastMonoWaivers = []parity.Waiver{
	{
		Ledger: "A-006", Layer: parity.LayerL2, Kind: "text-width", Subject: "SQLITE_BUSY",
		Note: "详情原文是固定间距文字，Viva 把字格取整（Consolas 11px 的 6.048→6px），整行偏短最多 2.6px",
	},
	{
		Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: "SQLITE_BUSY",
		Note: "同上：字格取整使每行墨迹整体偏短，字形位置累计最多差一个字格",
	},
}

// toastScene 是「页面内容隐去，只留右下角提示」的场景，与 React 采集器的 toastScene 对应：
// show 往通知通道里放提示，steps 可选（展开详情等）。页面只有提示宿主，背景是均匀的页面底色（台账 A-007）。
// 提示容器是 fixed 的浮层，文字的灰度抗锯齿偏差（A-009）对它同样成立，所以场景一律带上 overlayInkWaiver。
func toastScene(id string, show func(n *kit.Notifier, t parity.ToastTexts), steps func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			n := kit.NewNotifier(nil)
			n.Clipboard = func(string) error { return nil } // 对照不碰系统剪贴板
			show(n, parity.ToastContent())
			return func(c ui.C) { look.Toasts(c, n) }
		},
		Steps:   steps,
		Waivers: append([]parity.Waiver{overlayInkWaiver}, waivers...),
	}
}

// expandToastDetail 是「点开第一条提示的『查看详情』并把指针移开」，与渲染层的 toastScene(expandDetail) 对应。
func expandToastDetail(s *headless.Session) error {
	if err := parity.ClickRole(s, gfx.RoleButton, "查看详情"); err != nil {
		return err
	}
	return parity.ParkPointer(s)
}

// registry 是全部已登记场景。新增屏幕时在这里加一行，并在 React 采集器里登记同名场景。
var registry = map[string]parity.Scene{
	"ui.toast.success": toastScene("ui.toast.success", func(n *kit.Notifier, t parity.ToastTexts) { n.Success(t.Success) }, nil),
	"ui.toast.info":    toastScene("ui.toast.info", func(n *kit.Notifier, t parity.ToastTexts) { n.Info(t.Info) }, nil),
	"ui.toast.warning": toastScene("ui.toast.warning", func(n *kit.Notifier, t parity.ToastTexts) { n.Warning(t.Warning) }, nil),
	"ui.toast.error":   toastScene("ui.toast.error", func(n *kit.Notifier, t parity.ToastTexts) { n.Error(t.Error, "") }, nil),
	"ui.toast.error-detail": toastScene("ui.toast.error-detail",
		func(n *kit.Notifier, t parity.ToastTexts) { n.Error(t.Error, t.Detail) }, nil),
	"ui.toast.detail-expanded": toastScene("ui.toast.detail-expanded",
		func(n *kit.Notifier, t parity.ToastTexts) { n.Error(t.Error, t.Detail) }, expandToastDetail, toastMonoWaivers...),
	"ui.toast.action": toastScene("ui.toast.action",
		func(n *kit.Notifier, t parity.ToastTexts) { n.Undo(t.Undo, locales.CommonUndo(), func() {}) }, nil),
	"ui.toast.container": toastScene("ui.toast.container", func(n *kit.Notifier, t parity.ToastTexts) {
		n.Error(t.Error, t.Detail)
		n.Success(t.Success)
		n.Undo(t.Undo, locales.CommonUndo(), func() {})
	}, nil),
	"app.window.page-settings": pageSettings("app.window.page-settings", nil),
	"app.window.page-settings.select-close-action": pageSettings("app.window.page-settings.select-close-action",
		openSelect("关闭窗口行为", false), overlayInkWaiver),
	"app.window.page-settings.select-language": pageSettings("app.window.page-settings.select-language",
		openSelect("语言", true), overlayInkWaiver),
	"app.window.page-settings.dlg-background-tasks": withBackdrop(
		pageSettings("app.window.page-settings.dlg-background-tasks", openBackgroundTasksDialog, overlayInkWaiver),
		confirmBackdrop("启用系统后台运行")),
	"settings.shell": {
		ID: "settings.shell",
		Build: func(look *kit.Look) func(c ui.C) {
			s := &settingsscreen.Shell{Look: look, Store: settingsscreen.NewMemStore(settings.Default())}
			return s.Page
		},
	},
	"settings.general": {
		ID: "settings.general",
		Build: func(look *kit.Look) func(c ui.C) {
			g := &settingsscreen.General{Look: look, Store: settingsscreen.NewMemStore(settings.Default())}
			return g.Page
		},
	},
}

// Get 按 ID 取场景。
func Get(id string) (parity.Scene, error) {
	s, ok := registry[id]
	if !ok {
		return parity.Scene{}, fmt.Errorf("没有登记的场景 %q（已登记：%v）", id, IDs())
	}
	return s, nil
}

// IDs 返回全部已登记场景的 ID，排序。
func IDs() []string {
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
