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
	// Shadow2XL 是 shadow-2xl：确认对话框、提示。
	Shadow2XL = []tokens.Shadow{
		{X: 0, Y: 25, Blur: 50, Spread: -12, Color: black25},
	}
)
