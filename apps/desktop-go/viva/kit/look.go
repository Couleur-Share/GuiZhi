// Package kit 是 Viva 原生界面的统一封装层：外观（令牌 → gfx.Theme）、排版与间距单位、
// 以及按渲染层 Tailwind 写法复刻出来的共享控件（开关、选择触发器、设置卡片……）。
//
// 界面代码（viva/screens/*）只通过这里取颜色、尺寸与控件，不直接散落 Viva 的易变细节；
// 文本输入类控件是 V-a 的风险面，只能出现在本包（见 viva/inputguard_test.go 的规则）。
//
// Package kit is the shared layer of the Viva UI: appearance, units and the shared controls.
package kit

import (
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/tokens"
)

// Spec 描述一次外观装配的输入，对应设置里的外观项。
type Spec struct {
	Mode            tokens.Mode // 浅色 / 深色 / 跟随系统
	SystemDark      bool        // 系统当前是否深色（仅 ModeSystem 用到）
	Hue, Saturation float64     // 主题色相与饱和度
	FontSize        string      // tokens.FontSizes 里的档位 ID；未知取默认档
	Glass           bool        // 壁纸（玻璃）模式
}

// DefaultSpec 返回出厂外观：浅色、默认主题色与字号档。
func DefaultSpec() Spec {
	p := tokens.FindHuePreset(tokens.DefaultHuePreset)
	return Spec{Mode: tokens.ModeLight, Hue: p.Hue, Saturation: p.Saturation, FontSize: tokens.DefaultFontSize}
}

// Look 是装配好的外观：渲染层语义令牌、由它们派生的 Viva 主题，以及 rem 的像素值。
// 界面函数接收 *Look，这样令牌与控件主题永远出自同一次装配。
type Look struct {
	T      tokens.Tokens
	Theme  gfx.Theme
	Spec   Spec
	Dark   bool
	Glass  bool
	RootPx float32 // 根字号（字号档对应的像素）：Tailwind 的 1rem
}

// NewLook 把外观输入装配成 Look。
func NewLook(spec Spec) *Look {
	dark := spec.Mode == tokens.ModeDark || (spec.Mode != tokens.ModeLight && spec.SystemDark)
	t := tokens.For(dark, spec.Glass, spec.Hue, spec.Saturation)
	l := &Look{
		T: t, Spec: spec, Dark: dark, Glass: spec.Glass,
		RootPx: float32(tokens.FindFontSize(spec.FontSize).Px),
	}
	l.Theme = themeOf(l)
	return l
}

// Rem 把 rem 数换成像素（Tailwind 的长度都以根字号为 1rem）。
func (l *Look) Rem(n float32) float32 { return n * l.RootPx }

// Space 是 Tailwind 间距刻度：Space(4) = 1rem（p-4、gap-4），Space(0.5) = 2px（在中档）。
func (l *Look) Space(n float32) float32 { return n * 0.25 * l.RootPx }

// Col 把令牌颜色换成 Viva 的颜色。
func (l *Look) Col(c tokens.Color) gfx.Color { return gfxColor(c) }

// gfxColor 把令牌颜色换成 gfx.Color：通道除以 255，alpha 不预乘（与 gfx.RGBA 一致）。
func gfxColor(c tokens.Color) gfx.Color { return gfx.RGBA(c.R, c.G, c.B, c.A) }

// ShadowStyle 把令牌阴影换成 Viva 的阴影样式与颜色。
func ShadowStyle(s tokens.Shadow) (gfx.ShadowStyle, gfx.Color) {
	return gfx.ShadowStyle{OffsetX: s.X, OffsetY: s.Y, Blur: s.Blur, Spread: s.Spread}, gfxColor(s.Color)
}
