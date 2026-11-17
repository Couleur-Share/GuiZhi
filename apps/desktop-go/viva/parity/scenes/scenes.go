// Package scenes 登记可对照的 Viva 屏幕：每个条目的 ID 与 React 采集器（scripts/parity/scenes/react-scenes.mjs）里的场景 ID 一致。
//
// Package scenes registers the Viva screens that can be compared with the renderer.
package scenes

import (
	"fmt"
	"slices"
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
	"guizhi-mygo-shell/viva/tokens"
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

// modalBackdrop 是 Modal 的遮罩（bg-background/60 加 backdrop-blur-md，12px）：遮罩之上是名为 name 的对话框面板（台账 A-007）。
func modalBackdrop(name string) *parity.BackdropSpec {
	return &parity.BackdropSpec{Sigma: 12, ScrimAlpha: 0.6, PanelRole: "group", PanelName: name}
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
	// 整窗 + 取色器弹层：弹层是压在页面之上的 fixed 面板（透明度与缩放入场），只能在整窗里对照。
	"app.window.page-settings.custom-picker": pageAppearance("app.window.page-settings.custom-picker", customThemeSpec, openCustomPicker,
		slices.Concat(appearanceInkWaivers, monoFieldWaivers("「#3b82f6」", 2.5), monoFieldWaivers("「#3B82F6」", 3))...),
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

// scrollToEnd 是「把设置内容滚到底」，与渲染层的 scrollSettingsToEnd 对应。
func scrollToEnd(s *headless.Session) error {
	if err := parity.ScrollDown(s, 700, 400, 4000); err != nil {
		return err
	}
	return s.Rest()
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
			Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: subject, MaxDelta: 2,
			Note: "同上：字格取整使每个字符的位置累计最多差两个像素（大字号档的纵向基线还差一个像素）",
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
