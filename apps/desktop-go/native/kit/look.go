// Package kit 放原生界面的应用级共享件：主题桥、图标、Toast 等。
// 界面代码不直接散落调用易变的 MyGo API，统一经这里的少量控件，把框架版本变动限制在一处。
//
// Package kit holds the application-level shared pieces of the native UI.
package kit

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// HSL 把 globals.css 里 "H S% L%" 形式的令牌换成不透明的 ui.Color（h 为度，s、l 为百分数）。
func HSL(h, s, l float64) ui.Color {
	s, l = s/100, l/100
	a := s * math.Min(l, 1-l)
	f := func(n float64) uint8 {
		k := math.Mod(n+h/30, 12)
		v := l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))
		return uint8(math.Round(v * 255))
	}
	return ui.Color{R: f(0), G: f(8), B: f(4), A: 255}
}

// col 是生成代码（tokens_gen.go）用的构造器：hsl(h s% l% / a)。ui.Color 是非预乘 alpha，直接缩放 A 即可。
func col(h, s, l, a float64) ui.Color {
	c := HSL(h, s, l)
	if a < 1 {
		c = c.Alpha(float32(a))
	}
	return c
}

// ShadowToken 是 CSS box-shadow 的一层（globals.css 的 --shadow* 令牌）；零值表示无阴影。
type ShadowToken struct {
	X, Y, Blur, Spread float32
	Color              ui.Color
}

// IsZero 报告这一层是否不画任何东西。
func (s ShadowToken) IsZero() bool { return s.Color.A == 0 }

// On 把阴影加到元素上；零值原样返回。
func (s ShadowToken) On(e ui.Element) ui.Element {
	if s.IsZero() {
		return e
	}
	return e.Shadow(s.X, s.Y, s.Blur, s.Spread, s.Color)
}

// HuePreset 是设置里的一个主题色预设（MORANDI_THEMES）；FontSizePreset 是一个字号档（FONT_SIZES）。
type (
	HuePreset struct {
		ID, Name   string
		Hue        float64
		Saturation float64
	}
	FontSizePreset struct {
		ID, Name string
		Px       int
	}
)

// Mode 是明暗模式（设置里的 themeMode）。
type Mode string

const (
	ModeSystem Mode = "system"
	ModeLight  Mode = "light"
	ModeDark   Mode = "dark"
)

// ThemeSpec 描述一次主题装配的输入，对应渲染层设置里的外观项。
// 色相与饱和度必须显式给出（0,0 是合法的灰色主题），默认值用 DefaultThemeSpec。
type ThemeSpec struct {
	Mode       Mode
	Hue        float64    // 0–360
	Saturation float64    // 0–100
	FontSize   string     // FontSizes 里的 ID；未知取默认档
	Motion     MotionPref // 动效偏好
	Wallpaper  *Wallpaper // 非 nil 即壁纸（玻璃）模式
}

// DefaultThemeSpec 返回设置的出厂外观：跟随系统明暗、默认主题色与字号档、动效跟随系统、无壁纸。
func DefaultThemeSpec() ThemeSpec {
	p := FindHuePreset(DefaultHuePreset)
	return ThemeSpec{Mode: ModeSystem, Hue: p.Hue, Saturation: p.Saturation, FontSize: DefaultFontSize, Motion: MotionSystem}
}

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

// baseFontPx 是「中」档的根字号：Tailwind 的 rem 以此为 1，字号档位相对它缩放。
const baseFontPx = 16

// Env 是装配主题时窗口与系统给出的事实。
type Env struct {
	SystemDark bool           // 系统当前是否深色（仅 ModeSystem 用到）
	Prefs      ui.Preferences // 系统偏好：高对比度、文字缩放、减少动态效果…
}

// Look 是装配好的外观：MyGo 主题（嵌入，t.Text、t.Border 等直接可用）加渲染层语义令牌。
// 界面函数接收 *Look，而不是各自读 c.Theme()，这样令牌（卡片底色、玻璃描边…）与控件主题永远出自同一次装配。
type Look struct {
	*ui.Theme
	T     Tokens
	Glass bool    // 壁纸（玻璃）模式
	Zoom  float32 // 字号档位对应的整窗缩放（中 = 1）
	Level MotionLevel
	Spec  ThemeSpec
}

// NewLook 把设置与系统事实装配成 Look。
func NewLook(spec ThemeSpec, env Env) *Look {
	dark := spec.Mode == ModeDark || (spec.Mode != ModeLight && env.SystemDark)
	glass := spec.Wallpaper != nil
	var t Tokens
	switch {
	case glass && dark:
		t = glassDarkTokens(spec.Hue, spec.Saturation)
	case glass:
		t = glassLightTokens(spec.Hue, spec.Saturation)
	case dark:
		t = darkTokens(spec.Hue, spec.Saturation)
	default:
		t = lightTokens(spec.Hue, spec.Saturation)
	}
	th := uiTheme(t, dark)
	// 强调色始终是设置里的主题色，不让系统强调色覆盖；高对比度与文字缩放照系统走
	prefs := env.Prefs
	prefs.Accent = ui.Transparent
	return &Look{
		Theme: th.WithPreferences(prefs),
		T:     t,
		Glass: glass,
		Zoom:  float32(FindFontSize(spec.FontSize).Px) / baseFontPx,
		Level: ResolveMotion(spec.Motion, env.Prefs.ReduceMotion),
		Spec:  spec,
	}
}

// uiTheme 把语义令牌映射成 MyGo 的 ui.Theme（控件的默认外观）。
func uiTheme(t Tokens, dark bool) *ui.Theme {
	black, white := ui.Color{A: 255}, ui.Color{R: 255, G: 255, B: 255, A: 255}
	accent := t.Primary
	th := &ui.Theme{
		Dark:       dark,
		Background: t.Background,
		// 控件的面：渲染层二级按钮是 app-ui-surface-bg，悬停 bg-accent
		Surface:        t.UiSurfaceBg,
		SurfaceHover:   t.Accent,
		SurfacePressed: t.Accent.Mix(t.Foreground, 0.08),
		Border:         t.UiSurfaceBorder,
		Text:           t.Foreground,
		TextMuted:      t.MutedForeground,
		Accent:         accent,
		AccentText:     t.PrimaryForeground,
		InactiveAccent: accent, // 渲染层窗口失焦时不褪色
		Danger:         t.Destructive,
		// 选区与焦点环取渲染层的写法：::selection 是 primary/20，焦点环是 ring
		Selection:       accent.Alpha(0.2),
		Focus:           t.Ring.Alpha(0.5),
		FocusRingWidth:  2,
		FocusRingOffset: 1,
		Scrollbar:       t.MutedForeground.Alpha(0.2),
		ScrollbarStyle:  ui.ScrollbarStyle{ThumbWidth: 8, Hover: t.MutedForeground.Alpha(0.4), Active: t.MutedForeground.Alpha(0.5)},
		// Tailwind 的 md 圆角（--radius 减 2px）；Spacing 与 Tailwind 的 0.25rem 一致
		Radius:   RadiusRem*baseFontPx - 2,
		Spacing:  4,
		FontSize: 14,
	}
	if dark {
		th.AccentHover, th.AccentPressed = accent.Mix(white, 0.12), accent.Mix(black, 0.12)
		th.Warning, th.Success = ui.Hex("#fbbf24"), ui.Hex("#34d399")
	} else {
		th.AccentHover, th.AccentPressed = accent.Mix(black, 0.1), accent.Mix(black, 0.2)
		th.Warning, th.Success = ui.Hex("#d97706"), ui.Hex("#059669")
	}
	return th
}
