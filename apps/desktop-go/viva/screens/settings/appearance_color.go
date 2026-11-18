package settings

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// customColorID 是「自定义」色板在 themeColor 里的取值（预设用各自的 ID）。
const customColorID = "custom"

// themeColor 是「主题颜色」分区：右上角当前颜色名，一行色带（六个预设加自定义入口），选了自定义才展开的自定义面板。
func (a *Appearance) themeColor(c ui.C) {
	l := a.Look
	t := l.T
	cur := a.Store.Values().UI
	custom := cur.ThemeColor == customColorID
	l.SettingSectionBody(c, locales.SettingsThemeColor(), func() {
		ui.Column(c).FillWidth().Pad(l.Space(4)).Children(func() {
			// 选中颜色的名称（不挤占色带空间）：text-xs muted，靠右，下方 mb-3。名称为空时那个 div 没有内容、高度为 0。
			ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.End).MarginBottom(l.Space(3)).Children(func() {
				if name := a.colorName(cur); name != "" {
					l.Text(c, name, kit.TextXS, kit.WeightNormal, t.MutedForeground)
				}
			})
			// 单行色带：均匀分布，px-2 py-2 给选中环留出安全边距。格子按 Chromium 的 LayoutUnit 规则分宽并居中（kit.EqualCells，台账 B-025），
			// 否则恰好落在半个像素上的色板（大字号档的第四枚）会比渲染层差 1 像素。
			a.swatchRow.Center = true
			kit.EqualCellsBox(c, &a.swatchRow).Pad(l.Space(2)).Children(func() {
				for _, p := range tokens.HuePresets {
					a.swatch(c, p.ID, kit.SwatchSpec{
						Name: presetName(p.ID), Fill: tokens.HSL(p.Hue, p.Saturation, 55),
						On: cur.ThemeColor == p.ID, HoverOpacity: 0.9,
					}, func() { a.selectPreset(p) })
				}
				fill, _ := hexColor(cur.CustomThemeHex) // 无效写法 CSS 当作没有底色
				a.swatch(c, customColorID, kit.SwatchSpec{
					Name: locales.SettingsCustomColor(), Fill: fill, On: custom, HoverOpacity: 0.95,
				}, a.selectCustom)
			})
			if custom {
				a.customPanel(c)
			}
		})
	})
}

// colorName 是右上角显示的颜色名：自定义时是「自定义 #十六进制」，预设时是预设名。
func (a *Appearance) colorName(cur settings.UI) string {
	if cur.ThemeColor == customColorID {
		return locales.SettingsCustomColor() + " " + cur.CustomThemeHex
	}
	return presetName(cur.ThemeColor)
}

// swatch 是色带里的一格（渲染层 flex-1 flex justify-center min-w-0，里面一枚 flex-shrink-0 的色板）：
// 格子由 swatchRow 这个 EqualCells 版面摆放，所以色板直接作它的孩子，居中由版面做。
func (a *Appearance) swatch(c ui.C, key string, spec kit.SwatchSpec, onClick func()) {
	if a.Look.Swatch(c.Key(key), spec).Clicked() {
		onClick()
	}
}

// selectPreset 选一个预设主题色（渲染层 setThemeColor(id)）：色相与饱和度取预设的。
func (a *Appearance) selectPreset(p tokens.HuePreset) {
	a.save(func(v *settings.Values) {
		v.UI.ThemeColor, v.UI.ThemeHue, v.UI.ThemeSaturation = p.ID, p.Hue, p.Saturation
	})
}

// selectCustom 选自定义（渲染层 setThemeColor("custom")）：色相与饱和度由当前自定义十六进制算出。
// 渲染层在十六进制不合法时 hexToHs 抛错、设置不变；这里同样不改。
func (a *Appearance) selectCustom() {
	hue, sat, ok := HexToHS(a.Store.Values().UI.CustomThemeHex)
	if !ok {
		return
	}
	a.save(func(v *settings.Values) {
		v.UI.ThemeColor, v.UI.ThemeHue, v.UI.ThemeSaturation = customColorID, hue, sat
	})
}
