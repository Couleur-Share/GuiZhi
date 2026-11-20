package kit

import (
	"math"

	"guizhi-mygo-shell/viva/tokens"
)

// MixToken 在两个令牌颜色之间按进度 p（0 = from，1 = to）插值，用于悬停等过渡的中间帧。
//
// CSS 的颜色过渡在预乘 alpha 的 sRGB 里线性插值（Chromium 的默认），不是直接插值未预乘的通道：
// transparent → accent 的中间帧不会带黑边。这里按同一规则算，两端颜色相同时结果不变；
// 结果按 8 位通道四舍五入（显示本来就是 8 位）。
func MixToken(from, to tokens.Color, p float32) tokens.Color {
	switch {
	case p <= 0:
		return from
	case p >= 1:
		return to
	}
	alpha := from.A + (to.A-from.A)*p
	if alpha <= 0 {
		return tokens.Transparent
	}
	mix := func(a, b uint8) uint8 {
		pre := float64(a)*float64(from.A) + (float64(b)*float64(to.A)-float64(a)*float64(from.A))*float64(p)
		return uint8(math.Round(pre / float64(alpha)))
	}
	return tokens.Color{R: mix(from.R, to.R), G: mix(from.G, to.G), B: mix(from.B, to.B), A: alpha}
}

// disabledInk 是禁用按钮的前景色（文字、图标、转圈）。渲染层的 disabled:opacity-N 把整枚按钮合成一层，再以 N 的不透明度叠到背后的
// 面板上：按钮层里文字盖在底色上，所以文字像素 = N × 按钮层 + (1 − N) × 面板。Viva 的透明度逐色淡出，半透明的文字会再叠在
// 已经淡出的底色上，文字与底色的反差只剩渲染层的 N 倍——禁用的实心按钮上的字比渲染层淡得多（台账 C-008 / B-058）。
//
// 这里直接算出渲染层的那个像素：预乘的按钮层（fg 叠在 bg 上）× N + (1 − 按钮层不透明度 × N) × surface，返回不透明色。
// 它画在逐色淡出的底色上，字形实心处正好是这个像素；字形边缘的抗锯齿覆盖率在两边都是「先在层内合成、再淡出」，同样一致。
//
// 只在算得对的时候用：按钮有底色（bg.A > 0）且 surface 是不透明的面板色。否则退回逐色淡出——没有底色时它与整层淡出逐像素相同，
// 而且不依赖背后是什么；不知道背后是什么（surface 零值），或背后是半透明的面板（壁纸模式），整层淡出的结果算不出来。
func disabledInk(fg, bg tokens.Color, opacity float32, surface tokens.Color) tokens.Color {
	if bg.A <= 0 || surface.A < 1 {
		return fade(fg, opacity)
	}
	fa, ba, n := float64(fg.A), float64(bg.A), float64(opacity)
	layerA := fa + ba*(1-fa)
	channel := func(f, b, s uint8) uint8 {
		layer := float64(f)*fa + float64(b)*ba*(1-fa) // 预乘
		return uint8(math.Round(layer*n + (1-layerA*n)*float64(s)))
	}
	return tokens.Color{R: channel(fg.R, bg.R, surface.R), G: channel(fg.G, bg.G, surface.G), B: channel(fg.B, bg.B, surface.B), A: 1}
}
