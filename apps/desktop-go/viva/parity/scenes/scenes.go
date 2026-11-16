// Package scenes 登记可对照的 Viva 屏幕：每个条目的 ID 与 React 采集器（scripts/parity/scenes/react-scenes.mjs）里的场景 ID 一致。
//
// Package scenes registers the Viva screens that can be compared with the renderer.
package scenes

import (
	"fmt"
	"sort"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// registry 是全部已登记场景。新增屏幕时在这里加一行，并在 React 采集器里登记同名场景。
var registry = map[string]parity.Scene{
	"app.window.page-settings": {
		ID: "app.window.page-settings",
		Build: func(look *kit.Look) func(c ui.C) {
			w := &shellscreen.Window{
				Look: look, Page: shellscreen.PageSettings, OwnControls: true, SearchShortcut: "Alt+Shift+F",
				Settings: &settingsscreen.Shell{Look: look, Store: settingsscreen.NewMemStore(settings.Default())},
			}
			return w.View
		},
		Waivers: []parity.Waiver{
			{
				Ledger: "A-006", Layer: parity.LayerL2, Kind: "text-width", Subject: "Alt+Shift+F",
				Note: "快捷键提示是固定间距文字，Viva 把字格宽度取整（5.498→5px），一行 11 个字符窄 5.48px；行盒与外框宽度已按渲染层留出",
			},
			{
				Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: "Alt+Shift+F",
				Note: "同上：字格取整使整行墨迹整体偏短，左缘与每个字符的位置累计最多差一个字格（5.5px）",
			},
		},
	},
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
