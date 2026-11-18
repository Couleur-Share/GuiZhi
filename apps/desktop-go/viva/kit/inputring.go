package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// InputRing 是文本框聚焦环的显示进度（0..1）。
//
// 渲染层的全局规则 `input:focus-visible { ring-2 ring-primary/50 }` 对文本框任何方式的聚焦都生效
// （点击与 Tab 都匹配 :focus-visible），所以 focused 取文本框的 State.Focused，不是 FocusVisible。
// 套着 `.app-settings-input` 的文本框自己写了 box-shadow 的 160ms ease 过渡（globals.css），
// 环宽 0→2px 与不透明度 0→0.5 一起补间（与 Switch 的环同一做法）。
// anim 是调用方在文本框旁声明的专用动画 ID（`ui.ID(c.Key("ring"))`），文本框自己的命中 ID 取不到。
func (l *Look) InputRing(c ui.C, anim gfx.ID, focused bool) float32 {
	target := float32(0)
	if focused {
		target = 1
	}
	return c.Canvas().Animate(anim, 0, target, l.Motion(DurationSurface, EaseDefault))
}

// PaintInputRing 在文本框的表面之外画聚焦环（进度 p 由 InputRing 给出）；radius 是文本框自己的圆角。
// 环是 box-shadow，只出现在边框盒外侧，所以用空心环——半透明的底色才不会透出环色。
func (l *Look) PaintInputRing(d *ui.DrawContext, p, radius float32) {
	if p <= 0 {
		return
	}
	l.PaintRingBand(d, 2*p, Fade(l.T.Primary.WithAlpha(0.5), p), radius)
}
