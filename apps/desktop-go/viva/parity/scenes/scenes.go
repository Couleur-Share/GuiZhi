// Package scenes 登记可对照的 Viva 屏幕：每个条目的 ID 与 React 采集器（scripts/parity/scenes/react-scenes.mjs）里的场景 ID 一致。
//
// Package scenes registers the Viva screens that can be compared with the renderer.
package scenes

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
	"guizhi-mygo-shell/viva/tokens"
)

// pageSettingsWaivers 是整窗设置页（顶栏带快捷键提示）共有的已登记偏差。
var pageSettingsWaivers = []parity.Waiver{
	{
		Ledger: "A-006", Layer: parity.LayerL2, Kind: "text-width", Subject: "Alt+Shift+F",
		Note: "快捷键提示是固定间距文字，Viva 把字格宽度取整（5.498→5px），一行 11 个字符窄 5.48px；行盒与外框宽度已按渲染层留出",
	},
	{
		Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: "Alt+Shift+F", MaxDY: 1,
		Note: "同上：字格取整使整行墨迹整体偏短，左缘与每个字符的位置累计最多差一个字格（5.5px）；小、大字号档纵向另差 1 像素（Viva 偏下，等宽字体的基线，台账 B-022）",
	},
}

// overlayInkWaiver 登记 A-009：渲染层的浮层（fixed + 圆角裁切）在独立合成层里，文字用灰度抗锯齿；
// Viva 的 LCD / 灰度是整窗开关（渲染器按窗口清屏色一次决定），同一帧里无法只让浮层用灰度，
// 所以浮层文字的墨迹总量系统性偏大（2026-11-16 在 select-close-action 的 36 个变体里实测 1.15–1.31 倍）。
// 只放过浮层里文字的墨迹总量比；位置、字重、颜色、字号照常判定，页面层的同一段文字也照常判定。
//
// 范围（台账 G-024）：只放过「Viva 偏多」（Dir +1）、比值不超过 1.5（MaxDelta 0.5）。A-009 的依据是 LCD 比灰度多出覆盖，
// 只支持「偏多」；此前这条登记没有方向与上限，Viva 的墨迹少到 0.36 倍也被放过，C-008（被禁用的实心主按钮文字被淡出两次）
// 因此自 M2-1 起藏了四个场景。上限取对终版输出的全量扫描（15 个输出目录、5466 份报告、12890 次放过）实测的最大比值 1.478，
// 向外留 0.02 余量：有新场景超过 1.5 时先查原因，不要先放宽。没有写 MinDelta：墨迹总量问题本来只在偏差超过 0.15 时才产生。
var overlayInkWaiver = parity.Waiver{
	Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Overlay: true, Dir: 1, MaxDelta: 0.5,
	Note: "浮层文字：渲染层灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（位置、字重、颜色、字号均一致）",
}

// overlayEmojiInkWaiver 登记 A-023 在浮层里的延伸：新建合集对话框的图标选择器里，选中的表情「📖」在 Viva 里的墨迹总量
// 只有渲染层的 0.837–0.848 倍（T1 与 T2 各变体里恰好低于 0.85 阈值的那几个；其余变体在阈值之内）。方向与 A-023 在侧栏行上的
// 「Viva 偏多」相反，也不是 A-009 的 LCD 效应（彩色字形不走 ClearType）：两边画出的表情位置、大小一致，肉眼可见的差别是
// 渲染层的纵向蓝边更饱和、Viva 的偏淡（细边的栅格化不同，原因没有逐项验证）。
// 只放过浮层里单个彩色表情、Viva 偏少、偏差在 [0.15, 0.17] 的墨迹总量比；偏差范围取实测向外取整到 0.01。
// 只登记在出现它的场景上，别的对话框里的表情不因此被放过。
var overlayEmojiInkWaiver = parity.Waiver{
	Ledger: "A-023", Layer: parity.LayerL2, Kind: "ink-mass", Overlay: true, Emoji: true, Dir: -1, MinDelta: 0.15, MaxDelta: 0.17,
	Note: "浮层里的彩色表情：Viva 的墨迹总量比渲染层少 15%–17%（纵向细蓝边偏淡；位置与大小一致，原因未逐项验证）",
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

// modalBackdrop 是 Modal 的遮罩（bg-background/60 加 backdrop-blur-md，12px）：遮罩之上是名为 name 的对话框面板（台账 A-007）。
func modalBackdrop(name string) *parity.BackdropSpec {
	return &parity.BackdropSpec{Sigma: 12, ScrimAlpha: 0.6, PanelRole: "group", PanelName: name}
}

// withBackdrop 给场景登记遮罩背后的模糊。
func withBackdrop(s parity.Scene, b *parity.BackdropSpec) parity.Scene {
	s.Backdrop = b
	return s
}

// settingsWindow 是整窗（顶栏 + rail + 设置页）的初始状态：每次渲染都新建，状态不跨渲染共享。
func settingsWindow(look *kit.Look) *shellscreen.Window {
	return &shellscreen.Window{
		Look: look, Page: shellscreen.PageSettings, OwnControls: true,
		SearchShortcut: "Alt+Shift+F", CaptureShortcut: "Alt+Shift+N",
		Settings: &settingsscreen.Shell{Look: look, Store: settingsscreen.NewMemStore(settings.Default())},
	}
}

// pageSettings 是整窗（顶栏 + rail + 设置页）的场景；steps 非空时在首次稳定后按它操作界面。
func pageSettings(id string, steps func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID:      id,
		Build:   func(look *kit.Look) func(c ui.C) { return settingsWindow(look).View },
		Steps:   steps,
		Waivers: append(append([]parity.Waiver{}, pageSettingsWaivers...), waivers...),
	}
}

// updateVersion 是「有新版本」场景里主进程推送的版本号（与 react-scenes.mjs 的 UPDATE_VERSION 一致）。
const updateVersion = "1.2.3"

// pageSettingsUpdate 是顶栏出现「有新版本」按钮的整窗场景（B-006）：渲染层由主进程推 updater:status = available 触发，
// Viva 一侧由外壳置 Window.Update 与 UpdateVersion。
func pageSettingsUpdate(id string) parity.Scene {
	s := pageSettings(id, nil)
	s.Build = func(look *kit.Look) func(c ui.C) {
		w := settingsWindow(look)
		w.Update, w.UpdateVersion = true, updateVersion
		return w.View
	}
	return s
}

// pageAppearance 是整窗（顶栏 + rail + 设置页）停在「显示设置」分区的场景，外观由 spec 决定（选「自定义」后整个界面重新取色）。
// 整窗里才放得下取色器的弹层（弹层压在 rail 与页面之上）。
func pageAppearance(id string, spec func(kit.Spec) kit.Spec, steps func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID:   id,
		Spec: spec,
		Build: func(look *kit.Look) func(c ui.C) {
			w := &shellscreen.Window{
				Look: look, Page: shellscreen.PageSettings, OwnControls: true, SearchShortcut: "Alt+Shift+F",
				Settings: &settingsscreen.Shell{Look: look, Store: appearanceStore(look), Active: settingsscreen.SectionAppearance},
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
			if err := scrollToEnd(s); err != nil {
				return err
			}
		}
		if err := parity.ClickRole(s, gfx.RoleCombobox, name); err != nil {
			return err
		}
		return parity.ParkPointer(s)
	}
}

// openNewMenu 是「点顶栏『创建方式』箭头展开菜单并把指针移开」，与渲染层的 openNewMenu 对应
// （渲染层采集器随后保留焦点：菜单打开时第一项自动聚焦，那是用户看到的自然状态）。
func openNewMenu(s *headless.Session) error {
	if err := parity.ClickRole(s, gfx.RoleCombobox, "创建方式"); err != nil {
		return err
	}
	return parity.ParkPointer(s)
}

// openNewMenuKeyboard 是「展开菜单后按一次 ↓」，与渲染层的 openNewMenuKeyboard 对应。
func openNewMenuKeyboard(s *headless.Session) error {
	if err := openNewMenu(s); err != nil {
		return err
	}
	return s.Keys("down")
}

// closeDialogScene 是设置页上弹着「关闭应用」对话框的整窗场景：渲染层的触发是主进程推 window:showCloseDialog（采集器直接走那条推送路径），
// Viva 一侧由外壳置 Window.CloseDialogOpen。对话框是步骤里才打开的（Prepare：状态每次渲染新建），所以「打开之前」的页面
// （模糊背景的预测，台账 A-007）仍走 Build——一个没有对话框的设置页。after 可选：对话框出现之后再做的操作（勾选、悬停、Tab）。
func closeDialogScene(id string, after func(s *headless.Session) error) parity.Scene {
	return withBackdrop(parity.Scene{
		ID:    id,
		Build: func(look *kit.Look) func(c ui.C) { return settingsWindow(look).View },
		Prepare: func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
			w := settingsWindow(look)
			return w.View, func(s *headless.Session) error {
				w.CloseDialogOpen = true
				// 出两帧（对话框要先被声明、再有版面与命中区），再让入场动画走完：后面的操作要用到它们。
				for range 2 {
					if err := s.Frame(); err != nil {
						return err
					}
				}
				if err := s.Rest(); err != nil {
					return err
				}
				if after == nil {
					return nil
				}
				return after(s)
			}
		},
		Waivers: append(append([]parity.Waiver{}, pageSettingsWaivers...), overlayInkWaiver),
	}, modalBackdrop("关闭应用"))
}

// clickCheckboxLabel 是「点名为 name 的复选框右边的文字」：对应渲染层采集器点 label 的文字（label 的 onClick 翻转，不取键盘焦点）。
// 复选框节点是 16px 的方框，文字在它右边 8px 之后。
func clickCheckboxLabel(name string) func(s *headless.Session) error {
	return func(s *headless.Session) error {
		node, ok := s.AccessByRole(gfx.RoleCheckbox, name)
		if !ok {
			return fmt.Errorf("找不到复选框「%s」", name)
		}
		if err := s.Click(node.Bounds.X+node.Bounds.Width+16, node.Bounds.Y+node.Bounds.Height/2); err != nil {
			return err
		}
		return parity.ParkPointer(s)
	}
}

// hoverButton 是「把指针停在名为 name 的按钮中心」，对应渲染层采集器的 hover（场景用 keepPointer 保留悬停）。
func hoverButton(name string) func(s *headless.Session) error {
	return func(s *headless.Session) error { return parity.MoveOverRolePrefix(s, gfx.RoleButton, name) }
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
		Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: "SQLITE_BUSY", MaxDY: 1,
		Note: "同上：字格取整使每行墨迹整体偏短，字形位置累计最多差一个字格；大字号档纵向另差 1 像素（Viva 偏下，等宽字体的基线，台账 B-022）",
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
	// 顶栏「新建」旁的创建方式菜单（B-007）：点箭头，菜单出现、第一项程序聚焦（不画环）；-key 再按一次 ↓，第二项带键盘聚焦环。
	"app.window.page-settings.new-menu": pageSettings("app.window.page-settings.new-menu",
		openNewMenu, overlayInkWaiver),
	"app.window.page-settings.new-menu-key": pageSettings("app.window.page-settings.new-menu-key",
		openNewMenuKeyboard, overlayInkWaiver),
	// 顶栏「有新版本」按钮（B-006）：虚线边框（border-dashed border-primary/50）、bg-primary/10，右侧一条竖分隔线。
	"app.window.page-settings.update-available": pageSettingsUpdate("app.window.page-settings.update-available"),
	"app.window.page-settings.dlg-background-tasks": withBackdrop(
		pageSettings("app.window.page-settings.dlg-background-tasks", openBackgroundTasksDialog, overlayInkWaiver),
		confirmBackdrop("启用系统后台运行")),
	// 关闭应用对话框（S7，渲染层 CloseDialog）：整窗 + 模态，遮罩背后的页面模糊（A-007，backdrop-blur-md 是 12），浮层文字的墨迹总量偏大（A-009）。
	// 默认态；勾上「记住我的选择」（点文字）；悬停两枚选项；键盘 Tab 一次（焦点落到页眉关闭钮，带聚焦环）。
	"app.window.page-settings.dlg-close":         closeDialogScene("app.window.page-settings.dlg-close", nil),
	"app.window.page-settings.dlg-close-checked": closeDialogScene("app.window.page-settings.dlg-close-checked", clickCheckboxLabel("记住我的选择")),
	"app.window.page-settings.dlg-close-hover-minimize": closeDialogScene("app.window.page-settings.dlg-close-hover-minimize",
		hoverButton("最小化到托盘")),
	"app.window.page-settings.dlg-close-hover-exit": closeDialogScene("app.window.page-settings.dlg-close-hover-exit",
		hoverButton("退出应用")),
	"app.window.page-settings.dlg-close-key": closeDialogScene("app.window.page-settings.dlg-close-key",
		func(s *headless.Session) error { return s.Keys("tab") }), "settings.shell": {
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
	"settings.appearance":        appearanceScene("settings.appearance", nil, nil, appearanceInkWaivers...),
	"settings.appearance.bottom": appearanceScene("settings.appearance.bottom", nil, scrollToEnd, scrolledBottomWaivers...),
	"settings.appearance.custom": appearanceScene("settings.appearance.custom", customThemeSpec, openCustomColor,
		slices.Concat(appearanceInkWaivers, monoFieldWaivers("「#3b82f6」", 2.5))...),
	"settings.appearance.custom-bottom": appearanceScene("settings.appearance.custom-bottom", customThemeSpec,
		func(s *headless.Session) error {
			if err := openCustomColor(s); err != nil {
				return err
			}
			return scrollToEnd(s)
		}, scrolledBottomWaivers...),
	// 控件状态（B-004）：行悬停、开关键盘聚焦、分段按钮与色板的悬停与键盘聚焦；选中项用变体里实际选中的那一枚。
	"settings.general.row-hover": generalScene("settings.general.row-hover",
		func(s *headless.Session) error { return parity.MoveInRow(s, gfx.RoleSwitch, "后台任务", 8, 6) }),
	"settings.general.switch-focus": generalScene("settings.general.switch-focus", focusByTab(gfx.RoleSwitch, "自动保存")),
	"settings.general.select-hover": generalScene("settings.general.select-hover",
		func(s *headless.Session) error {
			return parity.MoveOverRolePrefix(s, gfx.RoleCombobox, "关闭窗口行为")
		}),
	"settings.general.select-focus": generalScene("settings.general.select-focus", focusByTab(gfx.RoleCombobox, "关闭窗口行为")),
	"settings.appearance.segment-hover": appearanceScene("settings.appearance.segment-hover", nil,
		func(s *headless.Session) error { return parity.MoveOverRolePrefix(s, gfx.RoleButton, "跟随系统") }, appearanceInkWaivers...),
	"settings.appearance.segment-hover-selected": appearanceScene("settings.appearance.segment-hover-selected", nil,
		moveOverSelected(0), appearanceInkWaivers...),
	"settings.appearance.segment-focus": appearanceScene("settings.appearance.segment-focus", nil,
		focusByTab(gfx.RoleButton, "跟随系统"), appearanceInkWaivers...),
	"settings.appearance.segment-focus-selected": appearanceScene("settings.appearance.segment-focus-selected", nil,
		focusSelected(0), appearanceInkWaivers...),
	// 色板带 title：悬停 320 ms 后出气泡（渲染层的 TooltipLayer 是全局的，页面级场景默认没有提示层，这里加上）。
	"settings.appearance.swatch-hover": withTooltips(appearanceScene("settings.appearance.swatch-hover", nil,
		func(s *headless.Session) error {
			if err := parity.MoveOverRolePrefix(s, gfx.RoleButton, "自定义"); err != nil {
				return err
			}
			return s.Advance(kit.TooltipDelay + 80*time.Millisecond)
		}, withOverlayInk(appearanceInkWaivers)...)),
	"settings.appearance.swatch-focus": appearanceScene("settings.appearance.swatch-focus", nil,
		focusByTab(gfx.RoleButton, "自定义"), appearanceInkWaivers...),
	"settings.appearance.swatch-focus-selected": appearanceScene("settings.appearance.swatch-focus-selected", nil,
		focusSelected(40), appearanceInkWaivers...),
	// 控件状态（B-004，续）：背景图主按钮、取色器触发钮的悬停与键盘聚焦，分段按钮的按下。
	"settings.appearance.pick-hover": appearanceScene("settings.appearance.pick-hover", nil,
		func(s *headless.Session) error {
			if err := scrollToControl(gfx.RoleButton, "选择图片", 120)(s); err != nil {
				return err
			}
			return parity.MoveOverRolePrefix(s, gfx.RoleButton, "选择图片")
		}, scrolledBottomWaivers...),
	// 先 Tab 再滚：渲染层的场景脚本是先把控件滚到离内容区上沿 120px，再按一次 Tab 进入键盘模态、程序聚焦（浏览器不会再动滚动位置）；
	// Viva 没有程序聚焦，只能从头按 Tab 走过去，途中每一次落点都会按浏览器的规则跟随滚动（kit/reach.go，台账 B-030），
	// 所以走到之后再把控件滚回 120px：比的是「按钮聚焦（键盘环）、滚动位置固定」这个终态。
	// 逐键对照 React 的 Tab 环在 viva/app/root_tabreach_test.go。
	"settings.appearance.pick-focus": appearanceScene("settings.appearance.pick-focus", nil,
		func(s *headless.Session) error {
			if err := focusByTab(gfx.RoleButton, "选择图片")(s); err != nil {
				return err
			}
			if err := scrollToControl(gfx.RoleButton, "选择图片", 120)(s); err != nil {
				return err
			}
			return parity.ParkPointer(s)
		}, scrolledBottomWaivers...),
	"settings.appearance.segment-pressed": appearanceScene("settings.appearance.segment-pressed", nil,
		func(s *headless.Session) error { return parity.PressRole(s, gfx.RoleButton, "跟随系统") }, appearanceInkWaivers...),
	"settings.appearance.custom-trigger-hover": appearanceScene("settings.appearance.custom-trigger-hover", customThemeSpec,
		func(s *headless.Session) error {
			if err := openCustomColor(s); err != nil {
				return err
			}
			return parity.MoveOverRoleNth(s, gfx.RoleButton, "自定义", 1)
		}, slices.Concat(appearanceInkWaivers, monoFieldWaivers("「#3b82f6」", 2.5))...),
	"settings.appearance.hex-focus": appearanceScene("settings.appearance.hex-focus", customThemeSpec,
		func(s *headless.Session) error {
			if err := openCustomColor(s); err != nil {
				return err
			}
			// 点进文本框聚焦（文本框任何方式聚焦都匹配 :focus-visible）；点完指针挪开，焦点留着。
			if err := parity.ClickRole(s, gfx.RoleTextField, "#3b82f6"); err != nil {
				return err
			}
			return parity.ParkPointer(s)
		}, slices.Concat(appearanceInkWaivers, monoFieldWaivers("「#3b82f6」", 2.5))...),
	"settings.appearance.custom-trigger-focus": appearanceScene("settings.appearance.custom-trigger-focus", customThemeSpec,
		func(s *headless.Session) error {
			if err := openCustomColor(s); err != nil {
				return err
			}
			return focusByTabNth(gfx.RoleButton, "自定义", 1)(s)
		}, slices.Concat(appearanceInkWaivers, monoFieldWaivers("「#3b82f6」", 2.5))...),
	// 整窗 + 取色器弹层：弹层是压在页面之上的 fixed 面板（透明度与缩放入场），只能在整窗里对照。
	"app.window.page-settings.custom-picker": pageAppearance("app.window.page-settings.custom-picker", customThemeSpec, openCustomPicker,
		slices.Concat(appearanceInkWaivers, monoFieldWaivers("「#3b82f6」", 2.5), monoFieldWaivers("「#3B82F6」", 3))...),
}

// focusByTab 是「按 Tab 直到名为 name 的控件取得键盘焦点」，对应渲染层的 focusByKeyboard（先按一次 Tab 进入键盘模态、再程序聚焦）：
// 终态相同——键盘模态下的焦点，带 :focus-visible 的聚焦环。同名控件取可访问树里的第一个。
func focusByTab(role gfx.Role, name string) func(s *headless.Session) error {
	return focusByTabNth(role, name, 0)
}

// focusByTabNth 是 focusByTab 取第 n 个（从 0 起，按可访问树的顺序）同名同角色的控件，对应渲染层的 getByRole(...).nth(n).focus()。
func focusByTabNth(role gfx.Role, name string, nth int) func(s *headless.Session) error {
	return func(s *headless.Session) error {
		for range 80 {
			seen := 0
			for _, n := range s.Access() {
				if n.Role != role || n.Label != name {
					continue
				}
				if seen == nth && n.Focused {
					return parity.ParkPointer(s)
				}
				seen++
			}
			if err := s.Keys("tab"); err != nil {
				return err
			}
		}
		return fmt.Errorf("按了 80 次 Tab 仍没有走到第 %d 个 %s「%s」", nth, role, name)
	}
}

// firstSelectedButton 是可访问树里第一个选中（aria-pressed）的按钮；square > 0 时只取近似正方形、边长在 [0.75, 1.25] × square 的
// （色带里的圆形色板：渲染层的 h-10 w-10 是 2.5rem，随字号档为 35 / 40 / 45px，所以按标称 40 取宽松范围，不能写死 ±1px）。
func firstSelectedButton(s *headless.Session, square float32) (app.AccessNode, bool) {
	for _, n := range s.Access() {
		if n.Role != gfx.RoleButton || !n.Selected {
			continue
		}
		if square > 0 && (abs32(n.Bounds.Width-n.Bounds.Height) > 1 || n.Bounds.Width < 0.75*square || n.Bounds.Width > 1.25*square) {
			continue
		}
		return n, true
	}
	return app.AccessNode{}, false
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// moveOverSelected 是「把指针停在第一个选中的按钮中心」，对应渲染层的 hoverInAppearance(selectedSegment)。
func moveOverSelected(square float32) func(s *headless.Session) error {
	return func(s *headless.Session) error {
		n, ok := firstSelectedButton(s, square)
		if !ok {
			return fmt.Errorf("找不到选中的按钮")
		}
		return s.Move(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
	}
}

// focusSelected 是「用键盘聚焦第一个选中的按钮」，对应渲染层的 focusByKeyboard(selectedSegment / selectedSwatch)。
func focusSelected(square float32) func(s *headless.Session) error {
	return func(s *headless.Session) error {
		n, ok := firstSelectedButton(s, square)
		if !ok {
			return fmt.Errorf("找不到选中的按钮")
		}
		return focusByTab(gfx.RoleButton, n.Label)(s)
	}
}

// customThemeSpec 是选「自定义」之后的外观：整个界面按自定义的十六进制（出厂 #3b82f6）重新取色相与饱和度。
func customThemeSpec(base kit.Spec) kit.Spec {
	hue, sat, ok := settingsscreen.HexToHS(settings.Default().UI.CustomThemeHex)
	if ok {
		base.Hue, base.Saturation = hue, sat
	}
	return base
}

// openCustomColor 是「点色带里的『自定义』并把指针移开」，与渲染层的 openCustomColor 对应。
func openCustomColor(s *headless.Session) error {
	if err := parity.ClickRole(s, gfx.RoleButton, "自定义"); err != nil {
		return err
	}
	return parity.ParkPointer(s)
}

// openCustomPicker 是「选『自定义』展开面板后点面板里的取色器触发钮并把指针移开」，与渲染层的 openCustomPicker 对应。
// 触发钮与色带里的预设同名，是第二个名为「自定义」的按钮。
func openCustomPicker(s *headless.Session) error {
	if err := openCustomColor(s); err != nil {
		return err
	}
	if err := parity.ClickRoleNth(s, gfx.RoleButton, "自定义", 1); err != nil {
		return err
	}
	return parity.ParkPointer(s)
}

// scrollToControl 是「把内容滚到名为 name 的控件上沿离内容区上沿 offset 像素」（到末端就停），与渲染层的 scrollSettingsTo 对应。
// 控件不在可见区时不在可访问树里（Viva 只出看得见的节点），所以先按 200px 一步往下滚到它出现，再按它的实际位置补滚；
// 补滚之后位置若还没到（部分可见的节点量到的外框不是整个控件的）就再来一轮，到末端被夹住时位置不再变化即停。
func scrollToControl(role gfx.Role, name string, offset float32) func(s *headless.Session) error {
	return func(s *headless.Session) error {
		x, y := scrollPoint(s)
		var last float32 = -1
		for range 60 {
			n, ok := s.AccessByRole(role, name)
			delta := float32(200)
			if ok {
				delta = n.Bounds.Y - offset
				if abs32(delta) < 0.5 || (last >= 0 && abs32(n.Bounds.Y-last) < 0.5) {
					return s.Rest()
				}
				last = n.Bounds.Y
			}
			if err := parity.ScrollDown(s, x, y, delta); err != nil {
				return err
			}
			if err := s.Rest(); err != nil {
				return err
			}
		}
		return fmt.Errorf("滚了 60 轮仍没有把 %s「%s」滚到位", role, name)
	}
}

// scrollToEnd 是「把设置内容滚到底」，与渲染层的 scrollSettingsToEnd 对应。
func scrollToEnd(s *headless.Session) error {
	x, y := scrollPoint(s)
	if err := parity.ScrollDown(s, x, y, 4000); err != nil {
		return err
	}
	return s.Rest()
}

// scrollPoint 是滚轮的落点：画布正中。固定坐标（曾经是 700,400）在 800x600 的内容区（496 宽）之外，滚轮落空、页面根本没动。
func scrollPoint(s *headless.Session) (x, y float32) {
	snap := s.Snapshot()
	return float32(snap.Width) / 2, float32(snap.Height) / 2
}

// opacityInkWaiver 登记 A-009 的延伸：整组 opacity < 1 的文字（`opacity-70` 的字号标注、`disabled:opacity-40` 的按钮）
// 在 Chromium 里画在带透明度的合成层里，文字用灰度抗锯齿（实测 React 一侧该文字的 RGB 色差只有底色自带的 18–19，
// 页面上别的文字有 100 以上的 LCD 彩边）；Viva 的 LCD / 灰度是整窗开关，同一帧里没法只让这一组用灰度，
// 所以墨迹总量系统性偏大（2026-11-17 实测 1.15–1.38 倍）。
// 只放过这一类（墨迹总量比）、这一段文字，且按「应有的大小」框住：位置、字重、颜色、字号照常判定。
func opacityInkWaiver(subject string) parity.Waiver {
	return parity.Waiver{
		Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: subject, MinDelta: 0.1, MaxDelta: 0.4, Dir: 1,
		Note: "整组带透明度的文字（opacity-70 / disabled:opacity-40）在合成层里用灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（位置、字重、颜色、字号均一致）",
	}
}

// appearanceInkWaivers 是显示设置页里整组带透明度的文字：「字体大小」磁贴下面 opacity-70 的像素值（`14`、`16`、`18` 与 `px`），
// 以及没有背景图时 disabled:opacity-40 的「开启」按钮。
var appearanceInkWaivers = []parity.Waiver{
	opacityInkWaiver("「14」"), opacityInkWaiver("「16」"), opacityInkWaiver("「18」"), opacityInkWaiver("「px」"),
	opacityInkWaiver("「开启」"),
}

// smallLatinInkWaiver 登记 A-016：小字号档（根字号 14px，text-xs = 10.5px）的拉丁字符，Viva 的笔画比渲染层淡，
// 墨迹总量比只在这一档、这一类文字上落到 0.835–0.847（阈值下限 0.85）；位置、宽度、颜色、字号都一致。
// 滑块旁的数值单位（`%`、`px`）是这类文字：笔画细、字形小，两个引擎的栅格化差异占比大。按「应有的大小」框住。
func smallLatinInkWaiver(subject string) parity.Waiver {
	return parity.Waiver{
		Ledger: "A-016", Layer: parity.LayerL2, Kind: "ink-mass", Subject: subject, MinDelta: 0.1, MaxDelta: 0.25, Dir: -1,
		Note: "10.5px 拉丁字符的笔画在两个引擎的栅格化里粗细不同，Viva 的墨迹总量偏低（位置、宽度、颜色、字号均一致）",
	}
}

// scrolledBottomWaivers 是滚到内容底之后显示设置页的已登记偏差：顶部仍可见的「开启」按钮，以及两个滑块的数值单位。
var scrolledBottomWaivers = slices.Concat(appearanceInkWaivers, []parity.Waiver{smallLatinInkWaiver("「%」"), smallLatinInkWaiver("「px」")})

// monoFieldWaivers 登记文本框里固定间距内容（font-mono，如十六进制色值）的 A-006 偏差：Viva 的字格取整，
// 整行与渲染层相差最多半个字格×字数，字形位置累计最多差两个像素（大字号档的 7 个字符累计 2.4px）。
// maxWidth 是三个字号档里整行宽度差的上限：面板里的 14px 色值框 7 个字符差 1.85 / 2.12 / 2.38px（取 2.5），
// 取色器弹层里的 12px 色值框差到 2.9px（取 3）。
func monoFieldWaivers(subject string, maxWidth float64) []parity.Waiver {
	return []parity.Waiver{
		{
			Ledger: "A-006", Layer: parity.LayerL2, Kind: "text-width", Subject: subject, MaxDelta: maxWidth,
			Note: "输入框里的固定间距文字，Viva 把字格取整（Consolas 的 7.70→8px 等），整行与渲染层的宽度差在半个字格×字数以内",
		},
		{
			Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: subject, MaxDelta: 2, MaxDY: 1,
			Note: "同上：字格取整使每个字符的位置累计最多差两个像素（大字号档的纵向基线另差一个像素，台账 B-022）",
		},
	}
}

// appearanceStore 给显示设置页一份与外观一致的设置：渲染层的变体把主题模式、主题色预设与字号设成变体的取值，
// 页面上选中的分段与色板因此跟着变体走（出厂默认的主题模式是「跟随系统」，不能直接用）。
func appearanceStore(look *kit.Look) settingsscreen.Store {
	v := settings.Default()
	v.UI.ThemeMode = settings.ThemeMode(look.Spec.Mode)
	v.UI.FontSize = settings.FontSize(look.Spec.FontSize)
	for _, p := range tokens.HuePresets {
		if p.Hue == look.Spec.Hue && p.Saturation == look.Spec.Saturation {
			v.UI.ThemeColor, v.UI.ThemeHue, v.UI.ThemeSaturation = p.ID, p.Hue, p.Saturation
		}
	}
	return settingsscreen.NewMemStore(v)
}

// generalScene 是「应用设置」页的场景：内容区（设置页的滚动区）对照；steps 把页面带到要对照的状态（可为空）。
func generalScene(id string, steps func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			g := &settingsscreen.General{Look: look, Store: settingsscreen.NewMemStore(settings.Default())}
			return g.Page
		},
		Steps:   steps,
		Waivers: waivers,
	}
}

// appearanceScene 是「显示设置」页的场景：内容区（设置页的滚动区）对照。
// spec 非空时场景有自己的外观（选「自定义」之后整个界面按自定义颜色重新取色），steps 把页面带到要对照的状态。
func appearanceScene(id string, spec func(kit.Spec) kit.Spec, steps func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID:   id,
		Spec: spec,
		Build: func(look *kit.Look) func(c ui.C) {
			a := &settingsscreen.Appearance{Look: look, Store: appearanceStore(look)}
			return a.Page
		},
		Steps:   steps,
		Waivers: waivers,
	}
}

// withTooltips 给页面级场景加上窗口根上的悬停提示层（渲染层的 TooltipLayer 挂在整个应用上；整窗场景自带它，页面级场景没有）。
// 提示状态每次渲染新建，不跨渲染共享。
func withTooltips(s parity.Scene) parity.Scene {
	build := s.Build
	s.Build = func(look *kit.Look) func(c ui.C) {
		page := build(look)
		tips := &kit.TooltipState{}
		return func(c ui.C) {
			page(c)
			look.Tooltips(c, tips)
		}
	}
	return s
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
