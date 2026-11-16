package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 像素吸附
//
// Chromium 把盒子的背景与边框画在「吸附后的矩形」上（PixelSnappedIntRect）：起点与终点各自四舍五入到设备像素，
// 尺寸是两者之差。Viva 对带圆角的矩形取「起点四舍五入 + 尺寸四舍五入」（为了圆不会被吸成椭圆），
// 小数布局下两者的下边缘、右边缘会差一个设备像素：例如 y=356.75、h=111.5，Chromium 画 357～468，Viva 画 357～469。
//
// 根字号 14px 与 18px 两档里到处是小数布局（行高 17.5、22.5，圆形旋钮直径 17.5……），
// 所以带背景、边框或圆角的表面一律由 kit 按 Chromium 的规则自己吸附并画出，不用 Box.Background / Border / Radius。
// 吸附后的矩形坐标都是整设备像素，Viva 画布内部再做的吸附对它是恒等变换。
// 依据：2026-11-16 在 settings.general 的 dark-royal-blue-small-800x600 上，卡片下边缘比参照低 1px
// （参照 y=356.75、h=111.5，像素差集中在卡片底边一行，482 像素）。

// snapEdge 把一个坐标四舍五入到设备像素（半个像素向上，与 Chromium 的 LayoutUnit::Round 一致）。
func snapEdge(v, scale float32) float32 {
	return float32(math.Floor(float64(v)*float64(scale)+0.5)) / scale
}

// SnapRect 返回 r 在设备像素网格上的吸附结果：起点与终点各自四舍五入；非零的尺寸至少保留一个设备像素。
// scale 是设备像素与逻辑像素之比（画布的 PixelScale）；不大于 0 时按 1。
func SnapRect(r gfx.Rect, scale float32) gfx.Rect {
	if scale <= 0 {
		scale = 1
	}
	x0, y0 := snapEdge(r.X, scale), snapEdge(r.Y, scale)
	x1, y1 := snapEdge(r.X+r.Width, scale), snapEdge(r.Y+r.Height, scale)
	out := gfx.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	if r.Width > 0 && out.Width <= 0 {
		out.Width = 1 / scale
	}
	if r.Height > 0 && out.Height <= 0 {
		out.Height = 1 / scale
	}
	return out
}

// Surface 描述一块带背景、边框、圆角与阴影的矩形表面（渲染层的 bg-* border rounded-* shadow-*）。
// 颜色是令牌（可带透明度），边框画在背景之上的环里，所以半透明的边框叠在背景上而不是透出下面的页面。
type Surface struct {
	Bg      tokens.Color
	Border  tokens.Color
	BorderW float32 // 逻辑像素；0 表示没有边框
	Radius  float32
	Shadow  tokens.Shadow
	// Clip 非零时，只画落在它里面的部分（逻辑像素，按设备像素吸附）。
	// 用来画只有一侧有圆角的盒子（rounded-l-lg）：按整个盒子画圆角矩形，再裁到这一侧。
	Clip gfx.Rect
}

// PaintSurface 在 d.Rect 上画出表面：阴影、背景（铺满边框盒）、边框环，都以吸附后的矩形为准。
// 在 Box.Draw 里调用，画在子元素之下。
func (l *Look) PaintSurface(d *ui.DrawContext, s Surface) {
	outer := SnapRect(d.Rect, d.Canvas.PixelScale())
	if outer.Width <= 0 || outer.Height <= 0 {
		return
	}
	if s.Clip.Width > 0 && s.Clip.Height > 0 {
		d.Canvas.Clip(SnapRect(s.Clip, d.Canvas.PixelScale()))
		defer d.Canvas.Unclip()
	}
	if !s.Shadow.IsZero() {
		style, color := ShadowStyle(s.Shadow)
		d.Canvas.Shadow(outer, s.Radius, style, d.Fade(color))
	}
	if s.Bg.A > 0 {
		d.Canvas.Rounded(outer, s.Radius, d.Fade(l.Col(s.Bg)))
	}
	if s.BorderW > 0 && s.Border.A > 0 {
		d.Canvas.RoundedOutline(outer, s.Radius, s.BorderW, d.Fade(l.Col(s.Border)))
	}
}

// PaintRing 在 d.Rect 外画一圈实心环（Tailwind 的 ring-2：box-shadow 0 0 0 Npx color）。
// 环画在盒子之下的一圈，盒子本身随后用 PaintSurface 画；radius 是盒子的圆角。
func (l *Look) PaintRing(d *ui.DrawContext, width float32, color tokens.Color, radius float32) {
	outer := SnapRect(d.Rect, d.Canvas.PixelScale())
	if outer.Width <= 0 || outer.Height <= 0 || color.A <= 0 {
		return
	}
	ring := gfx.Rect{X: outer.X - width, Y: outer.Y - width, Width: outer.Width + 2*width, Height: outer.Height + 2*width}
	d.Canvas.Rounded(ring, radius+width, d.Fade(l.Col(color)))
}
