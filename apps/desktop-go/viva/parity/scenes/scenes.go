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

// registry 是全部已登记场景。新增屏幕时在这里加一行，并在 React 采集器里登记同名场景。
var registry = map[string]parity.Scene{
	"app.window.page-settings": pageSettings("app.window.page-settings", nil),
	"app.window.page-settings.select-close-action": pageSettings("app.window.page-settings.select-close-action",
		openSelect("关闭窗口行为", false), overlayInkWaiver),
	"app.window.page-settings.select-language": pageSettings("app.window.page-settings.select-language",
		openSelect("语言", true), overlayInkWaiver),
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
