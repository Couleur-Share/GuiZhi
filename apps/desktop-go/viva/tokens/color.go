// Package tokens 是渲染层设计令牌的数据包：颜色、阴影、色相预设、字号档与动效时长。
//
// 它不 import 任何界面工具包——同一份数据被界面（viva/kit 转成 gfx.Theme）、
// 对等工程（viva/parity 与渲染层计算样式逐项对拍）和测试共用。
// tokens_gen.go 由 apps/desktop-go/scripts/gen-native-theme.mjs 按浏览器同样的方式求值 globals.css 后生成，不要手改。
//
// Package tokens holds the renderer's design tokens as plain data, with no UI-toolkit dependency.
package tokens

import "math"

// Color 是 sRGB 颜色，alpha 不预乘：通道 0–255，A 为 0–1。
type Color struct {
	R, G, B uint8
	A       float32
}

// Transparent 是全透明（生成代码里「该模式没有可画的颜色」的取值）。
var Transparent = Color{}

// RGBA 由 8 位通道与 0–1 的透明度构成颜色。
func RGBA(r, g, b uint8, a float32) Color { return Color{R: r, G: g, B: b, A: a} }

// HSL 把 globals.css 里 "H S% L%" 形式的令牌换成不透明颜色（h 为度，s、l 为百分数）。
func HSL(h, s, l float64) Color {
	s, l = s/100, l/100
	a := s * math.Min(l, 1-l)
	f := func(n float64) uint8 {
		k := math.Mod(n+h/30, 12)
		v := l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))
		return uint8(math.Round(v * 255))
	}
	return Color{R: f(0), G: f(8), B: f(4), A: 1}
}

// col 是生成代码用的构造器：hsl(h s% l% / a)。
func col(h, s, l, a float64) Color {
	c := HSL(h, s, l)
	if a < 1 {
		c.A = float32(a)
	}
	return c
}

// WithAlpha 返回换了透明度的颜色（通道不变）。
func (c Color) WithAlpha(a float32) Color {
	c.A = a
	return c
}

// Over 把这个（可能半透明的）颜色叠在不透明底色 base 上，得到不透明的结果；
// 与浏览器合成同一像素时的 source-over 一致（每通道四舍五入）。
func (c Color) Over(base Color) Color {
	a := float64(c.A)
	mix := func(t, b uint8) uint8 { return uint8(math.Round(float64(t)*a + float64(b)*(1-a))) }
	return Color{R: mix(c.R, base.R), G: mix(c.G, base.G), B: mix(c.B, base.B), A: 1}
}

// Shadow 是 CSS box-shadow 的一层（globals.css 的 --shadow* 令牌）；零值表示无阴影。
type Shadow struct {
	X, Y, Blur, Spread float32
	Color              Color
}

// IsZero 报告这一层是否不画任何东西。
func (s Shadow) IsZero() bool { return s.Color.A == 0 }

// HuePreset 是设置里的一个主题色预设（MORANDI_THEMES）。
type HuePreset struct {
	ID, Name   string
	Hue        float64
	Saturation float64
}

// FontSizePreset 是一个字号档（FONT_SIZES）；Px 是该档的根字号。
type FontSizePreset struct {
	ID, Name string
	Px       int
}

// BaseFontPx 是「中」档的根字号：Tailwind 的 rem 以根字号为 1，字号档位相对它缩放。
const BaseFontPx = 16

// FindHuePreset 按 ID 取预设；找不到返回默认预设。
func FindHuePreset(id string) HuePreset {
	for _, p := range HuePresets {
		if p.ID == id {
			return p
		}
	}
	for _, p := range HuePresets {
		if p.ID == DefaultHuePreset {
			return p
		}
	}
	return HuePresets[0]
}

// FindFontSize 按 ID 取字号档；找不到返回默认档。
func FindFontSize(id string) FontSizePreset {
	for _, f := range FontSizes {
		if f.ID == id {
			return f
		}
	}
	for _, f := range FontSizes {
		if f.ID == DefaultFontSize {
			return f
		}
	}
	return FontSizes[0]
}

// Mode 是明暗模式（设置里的 themeMode）。
type Mode string

const (
	ModeSystem Mode = "system"
	ModeLight  Mode = "light"
	ModeDark   Mode = "dark"
)

// For 返回某个外观下的令牌：dark 取深色，glass 取壁纸（玻璃）模式；hue、sat 是主题色相与饱和度。
func For(dark, glass bool, hue, sat float64) Tokens {
	switch {
	case glass && dark:
		return GlassDark(hue, sat)
	case glass:
		return GlassLight(hue, sat)
	case dark:
		return Dark(hue, sat)
	default:
		return Light(hue, sat)
	}
}

// DurationMs 是第 i 档动效时长（毫秒）：reduced 为「减弱」档；顺序 instant、quick、base、smooth、slow。
func DurationMs(i int, reduced bool) int {
	if reduced {
		return MotionReducedMs[i]
	}
	return MotionStandardMs[i]
}
