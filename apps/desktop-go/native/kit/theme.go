// Package kit 放原生界面的应用级共享件：主题桥、图标、Toast 等。
// 界面代码不直接散落调用易变的 MyGo API，统一经这里的少量控件，把框架版本变动限制在一处。
//
// Package kit holds the application-level shared pieces of the native UI.
package kit

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// ThemeSpec 描述一次主题装配的输入，对应渲染层设置里的外观项。
type ThemeSpec struct {
	Dark       bool
	Hue        float64 // 主题色相，0–360；零值按默认 220
	Saturation float64 // 主题饱和度，0–100；零值按默认 70
}

const (
	defaultHue        = 220.0
	defaultSaturation = 70.0
)

// HSL 把 globals.css 里 "H S% L%" 形式的令牌换成 ui.Color（h 为度，s、l 为百分数）。
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

// Theme 把渲染层 globals.css 的语义令牌（:root / .dark）映射成 ui.Theme。
// 目前覆盖界面用到的核心令牌；高对比度、字号档位、动效偏好、壁纸等在主题桥后续步骤补齐。
func Theme(s ThemeSpec) *ui.Theme {
	hue, sat := s.Hue, s.Saturation
	if hue == 0 {
		hue = defaultHue
	}
	if sat == 0 {
		sat = defaultSaturation
	}
	if s.Dark {
		return &ui.Theme{
			Dark: true, Background: HSL(220, 25, 7), Surface: HSL(220, 22, 10),
			SurfaceHover: HSL(220, 20, 14), SurfacePressed: HSL(220, 20, 17),
			Border: HSL(220, 20, 14), Text: HSL(220, 15, 92), TextMuted: HSL(220, 12, 60),
			Accent: HSL(hue, sat, 68), AccentHover: HSL(hue, sat, 72), AccentPressed: HSL(hue, sat, 62),
			AccentText: HSL(220, 25, 7), InactiveAccent: HSL(hue, sat, 68),
			Danger: HSL(0, 70, 55), Warning: HSL(38, 90, 55), Success: HSL(142, 55, 50),
			Selection: HSL(hue, 40, 28), Focus: HSL(hue, sat, 68),
		}
	}
	return &ui.Theme{
		Background: HSL(220, 14, 96), Surface: HSL(0, 0, 100),
		SurfaceHover: HSL(220, 14, 94), SurfacePressed: HSL(220, 14, 91),
		Border: HSL(220, 13, 90), Text: HSL(220, 20, 25), TextMuted: HSL(220, 12, 40),
		Accent: HSL(hue, sat, 35), AccentHover: HSL(hue, sat, 30), AccentPressed: HSL(hue, sat, 27),
		AccentText: HSL(0, 0, 100), InactiveAccent: HSL(hue, sat, 35),
		Danger: HSL(0, 60, 40), Warning: HSL(38, 90, 40), Success: HSL(142, 55, 32),
		Selection: HSL(hue, 60, 85), Focus: HSL(hue, sat, 35),
	}
}
