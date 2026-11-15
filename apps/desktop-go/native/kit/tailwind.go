package kit

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// tailwindPalette 是渲染层写死的 Tailwind 3.4 色阶：Toast、确认框、未保存对话框直接用了
// green / red / yellow / blue / amber-*，它们不属于主题令牌（globals.css 没有对应的 CSS 变量）。
// 集中放在这里，控件里不散落十六进制字面量；
// scripts/tests/gen-native.test.mjs 用 tailwindcss/colors 逐项对拍，抄错或版本变了会失败。
var tailwindPalette = map[string]string{
	"green-50":   "#f0fdf4",
	"green-100":  "#dcfce7",
	"green-200":  "#bbf7d0",
	"green-400":  "#4ade80",
	"green-500":  "#22c55e",
	"green-600":  "#16a34a",
	"green-800":  "#166534",
	"green-900":  "#14532d",
	"red-50":     "#fef2f2",
	"red-100":    "#fee2e2",
	"red-200":    "#fecaca",
	"red-400":    "#f87171",
	"red-500":    "#ef4444",
	"red-600":    "#dc2626",
	"red-700":    "#b91c1c",
	"red-800":    "#991b1b",
	"red-900":    "#7f1d1d",
	"yellow-50":  "#fefce8",
	"yellow-200": "#fef08a",
	"yellow-500": "#eab308",
	"yellow-800": "#854d0e",
	"yellow-900": "#713f12",
	"blue-50":    "#eff6ff",
	"blue-200":   "#bfdbfe",
	"blue-500":   "#3b82f6",
	"blue-800":   "#1e40af",
	"blue-900":   "#1e3a8a",
	"amber-100":  "#fef3c7",
	"amber-400":  "#fbbf24",
	"amber-600":  "#d97706",
	"amber-900":  "#78350f",
}

// tw 取调色板里的一个色值（如 "green-500"）。名字写错是程序错误，直接 panic，测试会第一时间发现。
func tw(name string) ui.Color {
	hex, ok := tailwindPalette[name]
	if !ok {
		panic("kit: 调色板里没有 " + name)
	}
	return ui.Hex(hex)
}

// over 把可能半透明的 top 叠在不透明底色 base 上，得到不透明的结果。
//
// 渲染层的提示与对话框用「半透明底色 + backdrop-blur」，原生界面没有背景模糊（见对等差异表）：
// 半透明面板直接盖在内容上会透出下面的文字，所以这里按「模糊后接近底色」预先合成。
func over(top, base ui.Color) ui.Color {
	a := float64(top.A) / 255
	mix := func(t, b uint8) uint8 { return uint8(math.Round(float64(t)*a + float64(b)*(1-a))) }
	return ui.Color{R: mix(top.R, base.R), G: mix(top.G, base.G), B: mix(top.B, base.B), A: 255}
}

// solid 返回面板用的不透明底色：浅色/深色下令牌本来不透明，原样返回；
// 壁纸（玻璃）模式下令牌是半透明的，叠在页面底色上合成。
func (l *Look) solid(c ui.Color) ui.Color {
	if c.A == 255 {
		return c
	}
	return over(c, l.T.Background)
}
