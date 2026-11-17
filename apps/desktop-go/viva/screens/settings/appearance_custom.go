package settings

import (
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// customPanel 是选中「自定义」后展开的面板（app-settings-subtle，p-4，rounded-xl）：
// 标题与说明、取色器触发钮、十六进制文本框，下面是主色 / 强调色 / 中性色三格预览。
//
// 圆角：rounded-xl 在这里被 .app-settings-subtle 的 border-radius: 1rem 盖掉，所以是 16px（不是 Tailwind 的 12px）。
func (a *Appearance) customPanel(c ui.C) {
	l := a.Look
	t := l.T
	cur := a.Store.Values().UI
	radius := l.Rem(1)
	surface := kit.Surface{Bg: t.SettingsSubtleBg, Border: t.SettingsSubtleBorder, BorderW: 1, Radius: radius}
	panel := ui.Column(c.Key("custom")).FillWidth().MarginTop(l.Space(4)).Pad(l.Space(4) + 1)
	panel = panel.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) })
	panel.Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).Children(func() {
			ui.Column(c).Shrink(1).Children(func() {
				l.Text(c, locales.SettingsCustomColor(), kit.TextSM, kit.WeightMedium, t.Foreground)
				l.Text(c, locales.SettingsCustomColorDesc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(0.5))
			})
			ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(2)).Children(func() {
				a.colorPicker(c, cur.CustomThemeHex)
				a.hexInput(c, cur.CustomThemeHex)
			})
		})
		// 紧凑预览：三格等宽，h-9 rounded-lg，文字 text-sm font-medium 居中。
		ui.Row(c.Key("chips")).FillWidth().MarginTop(l.Space(4)).Items(ui.Center).Gap(l.Space(2)).Children(func() {
			a.chip(c, "primary", locales.SettingsPrimary(), t.PrimaryForeground, kit.Surface{Bg: t.Primary})
			a.chip(c, "accent", locales.SettingsAccent(), t.AccentForeground, kit.Surface{Bg: t.Accent})
			a.chip(c, "neutral", locales.SettingsNeutral(), t.Foreground,
				kit.Surface{Bg: t.SettingsInputBg, Border: t.SettingsInputBorder, BorderW: 1})
		})
	})
}

// chip 是预览条里的一格：flex-1 h-9 rounded-lg，文字居中。
//
// 有边框的格子（中性色）在 CSS 里 flex-basis 0 指内容盒，边框宽度另算：三格平分剩余空间之前，它先占 2px，所以最终比另两格宽 2px。
// 这里的边框是 PaintSurface 画的、不参与布局，用等宽的内边距把这 2px 补回布局里。
func (a *Appearance) chip(c ui.C, key, label string, fg tokens.Color, surface kit.Surface) {
	l := a.Look
	surface.Radius = l.Rem(0.75) // rounded-lg = var(--radius)
	ui.Row(c.Key(key)).Grow(1).Basis(0).MinWidth(0).Height(l.Rem(2.25)).Pad(surface.BorderW).Items(ui.Center).Justify(ui.Center).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).
		Children(func() {
			l.Text(c, label, kit.TextSM, kit.WeightMedium, fg)
		})
}

// colorPicker 是取色器（渲染层 <ColorPicker value={customThemeHex} onChange={setCustomThemeHex} ariaLabel="自定义">）。
func (a *Appearance) colorPicker(c ui.C, stored string) {
	hex, changed := a.Look.ColorPicker(c.Key("picker"), &a.picker, kit.ColorPickerSpec{Name: locales.SettingsCustomColor(), Value: stored})
	if changed {
		a.setCustomHex(hex)
	}
}

// hexPlaceholder 是十六进制文本框的占位文字（placeholder="#3b82f6"），也是它的可访问名。
const hexPlaceholder = "#3b82f6"

// hexInput 是自定义颜色的十六进制文本框（h-9 w-28 px-3 rounded-lg app-settings-input text-sm font-mono）。
//
// 它是受控输入：每次编辑都交给 setCustomThemeHex，渲染层里非法写法会让 hexToHs 抛错、设置不变，输入框随之回到原值；
// 这里同样——合法（恰好六位十六进制，可带 #）才写入，否则文字退回设置里的值。
func (a *Appearance) hexInput(c ui.C, stored string) {
	l := a.Look
	t := l.T
	surface := kit.Surface{Bg: t.SettingsInputBg, Border: t.SettingsInputBorder, BorderW: 1, Radius: l.Rem(0.75)} // rounded-lg
	pad := l.Space(3) + 1                                                                                         // px-3 加 1px 边框
	// 底板与输入元素是两个元素：Box.Draw 画在输入内容之上，会把文字盖住（与搜索框同一做法）。
	// 可访问名取占位文字（渲染层的 <input> 没有 aria-label，名称来自 placeholder）。
	var box ui.Box
	ui.Stack(c.Key("hex")).Width(l.Rem(7)).Height(l.Rem(2.25)).Shrink(0).Children(func() {
		ui.Row(c.Key("surface")).Absolute().Inset(0).Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) })
		box = l.Input(c.Key("input"), &a.hexField, hexPlaceholder, hexPlaceholder, kit.InputSpec{
			Size: l.Rem(0.875), Weight: kit.WeightNormal, Color: t.Foreground, Height: l.Rem(2.25), Left: pad, Right: pad, Mono: true,
		}).Absolute().Inset(0)
	})
	if box.Changed() {
		next := a.hexField.Text()
		if !a.setCustomHex(next) {
			a.hexField.SetText(stored) // 受控：非法写法被拒绝，文字退回原值
		}
	} else if a.hexField.Text() != stored {
		a.hexField.SetText(stored) // 设置从别处改了（取色器）：文字跟着变
	}
}

// setCustomHex 复刻渲染层 setCustomThemeHex：合法才写入，写入时同时把主题色切到 custom 并更新色相与饱和度。
// 存的值是 `#` 加去掉前导 # 后的原文（保留大小写）。返回是否接受了这次编辑。
func (a *Appearance) setCustomHex(hex string) bool {
	hue, sat, ok := HexToHS(hex)
	if !ok {
		return false
	}
	return a.save(func(v *settings.Values) {
		v.UI.CustomThemeHex = "#" + strings.TrimPrefix(hex, "#") // 与渲染层一致：存原文（含空白与大小写），只去掉一个前导 #
		v.UI.ThemeColor, v.UI.ThemeHue, v.UI.ThemeSaturation = customColorID, hue, sat
	})
}
