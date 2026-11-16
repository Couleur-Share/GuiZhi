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
