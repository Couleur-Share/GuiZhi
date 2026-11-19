package kit

import "guizhi-mygo-shell/viva/tokens"

// Tailwind 默认阴影（tailwindcss 3.4 的 boxShadow 默认值）。渲染层的 tailwind.config.js 没有覆盖它们：
// 例如 Select 面板的 shadow-lg 在浏览器里的计算值是 `0 10px 15px -3px rgba(0,0,0,.1), 0 4px 6px -4px rgba(0,0,0,.1)`
// （2026-11-16 在 app.window.page-settings.select-close-action 的参照里读得），不是 globals.css 的 --shadow-lg 令牌。
// 阴影颜色不随明暗变化。多层阴影按 CSS 顺序写：第一层在最上。
var (
	black10 = tokens.RGBA(0, 0, 0, 0.1)
	black25 = tokens.RGBA(0, 0, 0, 0.25)

	// ShadowLG 是 shadow-lg：下拉面板。
	ShadowLG = []tokens.Shadow{
		{X: 0, Y: 10, Blur: 15, Spread: -3, Color: black10},
		{X: 0, Y: 4, Blur: 6, Spread: -4, Color: black10},
	}
	// ShadowXL 是 shadow-xl：组合筛选的弹层（`0 20px 25px -5px / 0 8px 10px -6px`，各 10% 黑）。
	ShadowXL = []tokens.Shadow{
		{X: 0, Y: 20, Blur: 25, Spread: -5, Color: black10},
		{X: 0, Y: 8, Blur: 10, Spread: -6, Color: black10},
	}
	// Shadow2XL 是 shadow-2xl：确认对话框、提示。
	Shadow2XL = []tokens.Shadow{
		{X: 0, Y: 25, Blur: 50, Spread: -12, Color: black25},
	}
	// ShadowSM 是 shadow-sm：选中的页签、分段控件的轻投影。
	ShadowSM = []tokens.Shadow{
		{X: 0, Y: 1, Blur: 2, Spread: 0, Color: tokens.RGBA(0, 0, 0, 0.05)},
	}
	// ShadowBase 是 Tailwind 的 shadow（无后缀）：悬停时的轻投影。
	ShadowBase = []tokens.Shadow{
		{X: 0, Y: 1, Blur: 3, Spread: 0, Color: black10},
		{X: 0, Y: 1, Blur: 2, Spread: -1, Color: black10},
	}
)

// ShadowLGOf 是带颜色的 shadow-lg（Tailwind 的 shadow-lg shadow-primary/20）：几何同 ShadowLG，每层颜色换成 color。
func ShadowLGOf(color tokens.Color) []tokens.Shadow {
	out := make([]tokens.Shadow, len(ShadowLG))
	for i, s := range ShadowLG {
		s.Color = color
		out[i] = s
	}
	return out
}

// MixShadows 在两组阴影之间按进度 p 插值（CSS 对 box-shadow 列表的过渡：逐层插值，较短的一边用零几何的透明层补齐，
// 颜色在预乘 alpha 里插值）。两端为空时返回空。
func MixShadows(from, to []tokens.Shadow, p float32) []tokens.Shadow {
	switch {
	case p <= 0:
		return from
	case p >= 1:
		return to
	}
	n := max(len(from), len(to))
	out := make([]tokens.Shadow, 0, n)
	for i := 0; i < n; i++ {
		a, b := paddedShadow(from, to, i), paddedShadow(to, from, i)
		out = append(out, tokens.Shadow{
			X:      a.X + (b.X-a.X)*p,
			Y:      a.Y + (b.Y-a.Y)*p,
			Blur:   a.Blur + (b.Blur-a.Blur)*p,
			Spread: a.Spread + (b.Spread-a.Spread)*p,
			Color:  MixToken(a.Color, b.Color, p),
		})
	}
	return out
}

// paddedShadow 取 list 的第 i 层；没有这一层时，用 other 对应层的颜色（透明）加零几何补齐。
func paddedShadow(list, other []tokens.Shadow, i int) tokens.Shadow {
	if i < len(list) {
		return list[i]
	}
	pad := tokens.Shadow{}
	if i < len(other) {
		pad.Color = other[i].Color.WithAlpha(0)
	}
	return pad
}
