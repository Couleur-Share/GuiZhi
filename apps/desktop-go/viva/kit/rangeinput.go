package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 滑块（渲染层的 <input type="range">，样式见 globals.css「原生控件外观收敛」）
//
// 输入框高 1.25rem、背景透明；轨道高 0.375rem、全圆角、`muted-foreground/0.25`；滑块头 0.875rem 的圆，
// 2px 页面底色描边、主色填充、`0 1px 3px rgba(0,0,0,.25)` 阴影，纵向比轨道顶高 0.25rem（即与轨道垂直居中）；
// 悬停时滑块头放大到 1.12 倍（quick 档，标准缓动）。轨道没有「已选」填充段。
//
// 取值、拖动、点轨道与键盘由 Viva 的 SliderBase 负责。它按 16px 的滑块头计算行程，渲染层是 14px：
// 点击与拖动取到的值与渲染层最多差滑块头行程之差（2px）对应的零点几个百分点（台账 B-019）；绘制按渲染层的 14px 行程。

// 滑块的几何（rem）。
const (
	rangeHeightRem = 1.25
	rangeTrackRem  = 0.375
	rangeThumbRem  = 0.875
)

// RangeSpec 描述一个滑块。
type RangeSpec struct {
	// Name 是可访问名（渲染层里由 <label htmlFor> 提供）。
	Name string
	// Min、Max、Step 是取值范围与步长；Step 为 0 表示连续。
	Min, Max, Step float32
}

// RangeInput 画一个撑满宽度的滑块。state.Value 是此刻的值，由调用方在声明前写入（受控）；
// 返回的 changed 表示这一帧用户改了它（拖动、点轨道、键盘），新值在 state.Value 里。
func (l *Look) RangeInput(c ui.C, state *ui.SliderState, spec RangeSpec) (changed bool) {
	t := l.T
	anim := ui.ID(c) // 悬停放大的动画键：SliderBase 自己的命中 ID 不对外，另取一个稳定的
	box := ui.SliderBase(c, state).FillWidth().Height(l.Rem(rangeHeightRem)).
		Range(spec.Min, spec.Max).Step(spec.Step).Label(spec.Name)
	l.noteStop(box)
	hovered := box.State().Hovered || state.Dragging()
	p := l.hoverTo(c.Canvas(), anim, 0, hovered, DurationQuick)
	changed = box.Changed()
	box.Draw(func(d *ui.DrawContext) {
		trackH, thumbD := l.Rem(rangeTrackRem), l.Rem(rangeThumbRem)
		r := d.Rect
		track := gfx.Rect{X: r.X, Y: r.Y + (r.Height-trackH)/2, Width: r.Width, Height: trackH}
		l.PaintSurfaceAt(d, track, Surface{Bg: t.MutedForeground.WithAlpha(0.25), Radius: trackH / 2})
		// 滑块头：行程是宽度减去它自己的直径；悬停放大以自身中心为原点。
		scale := 1 + 0.12*p
		size := thumbD * scale
		cx := r.X + thumbD/2 + state.Fraction()*(r.Width-thumbD)
		cy := track.Y + trackH/2
		thumb := gfx.Rect{X: cx - size/2, Y: cy - size/2, Width: size, Height: size}
		l.PaintSurfaceAt(d, thumb, Surface{
			Bg: t.Primary, Border: t.Background, BorderW: 2 * scale, Radius: size / 2,
			Shadow: tokens.Shadow{X: 0, Y: scale, Blur: 3 * scale, Spread: 0, Color: tokens.RGBA(0, 0, 0, 0.25)},
		})
	})
	return changed
}

// PaintSurfaceAt 在 r 上画表面（PaintSurface 的指定矩形版：控件内部的一块子区域，如滑块的轨道）。
func (l *Look) PaintSurfaceAt(d *ui.DrawContext, r gfx.Rect, s Surface) {
	sub := *d
	sub.Rect = r
	l.PaintSurface(&sub, s)
}
