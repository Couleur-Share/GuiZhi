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
	// Shadows 是多层阴影（CSS box-shadow 列表：第一层在最上，所以从最后一层画起）；与 Shadow 二选一。
	// Tailwind 默认的 shadow-lg、shadow-2xl 等都是多层或带负扩散的阴影，见 shadows.go。
	Shadows []tokens.Shadow
	// Clip 非零时，只画落在它里面的部分（逻辑像素，按设备像素吸附）。
	// 用来画只有一侧有圆角的盒子（rounded-l-lg）：按整个盒子画圆角矩形，再裁到这一侧。
	Clip gfx.Rect
	// Backdrop 非零时，在阴影之后、Bg 之前先垫一块不透明的底色：backdrop-blur 的替身（台账 A-007，同 Toast 的 paintToastSurface）。
	// 渲染层的 `bg-background/95 backdrop-blur-sm` 把背后的内容糊掉，只剩 5% 的糊影；Viva 没有模糊原语，直接叠 95% 的底色会让背后的文字
	// 以 5% 的清晰字形透出来。垫一块页面底色后背后的内容不再透出，与「糊掉」在像素上只差几个灰阶；阴影在垫层之外，不受影响。
	Backdrop tokens.Color
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
	for i := len(s.Shadows) - 1; i >= 0; i-- {
		if sh := s.Shadows[i]; !sh.IsZero() {
			l.paintShadow(d, outer, s.Radius, sh)
		}
	}
	if !s.Shadow.IsZero() {
		l.paintShadow(d, outer, s.Radius, s.Shadow)
	}
	if s.Backdrop.A > 0 {
		d.Canvas.Rounded(outer, s.Radius, d.Fade(l.Col(s.Backdrop)))
	}
	if s.Bg.A > 0 {
		d.Canvas.Rounded(outer, s.Radius, d.Fade(l.Col(s.Bg)))
	}
	if s.BorderW > 0 && s.Border.A > 0 {
		d.Canvas.RoundedOutline(outer, s.Radius, s.BorderW, d.Fade(l.Col(s.Border)))
	}
}

// PaintCastShadow 画 caster 的外阴影（盒子自己的部分挖掉），只留落在 clip 里的部分；阴影按 CSS 的高斯定义算，
// 网格逐设备像素一格、整格取格中心的值，与盒子大小无关。
//
// 用在「别的元素把阴影投到已经画好的内容上」的地方：侧栏（z-20）的阴影投在比它先画的工作区之上，
// Viva 里侧栏先画、工作区后画、侧栏又自带裁剪，只能在整行画完之后补一遍（台账 B-046 (7)(c)）。
// PaintSurface 对大盒子走 Viva 的距离场阴影，模糊半径只有 2px 时右缘外第二列的 1 个灰阶落不到
// （0 1px 2px 0 rgba(0,0,0,.04)：渲染层是 3、1 两个灰阶，距离场只画出 3、0）。
func (l *Look) PaintCastShadow(d *ui.DrawContext, caster, clip gfx.Rect, radius float32, sh tokens.Shadow) {
	if sh.IsZero() {
		return
	}
	scale := d.Canvas.PixelScale()
	outer := SnapRect(caster, scale)
	if outer.Width <= 0 || outer.Height <= 0 {
		return
	}
	d.Canvas.Clip(SnapRect(clip, scale))
	defer d.Canvas.Unclip()
	paintGaussianShadowStep(d, outer, radius, sh, 0)
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

// PaintRingBand 在 d.Rect 外画一圈空心环：与 PaintRing 画出的外形相同，但中间是空的。
// box-shadow 的环只出现在边框盒的外侧，PaintRing 画的是实心圆盘、靠随后画的不透明背景盖住内部；
// 背景透明或半透明的控件（取色器的触发钮、带透明度的聚焦环）改用这个，环里才不会透出环的颜色。
//
// 宽度可以不是整数设备像素（环在过渡中从 0 长到 2px）：Chromium 的 box-shadow 对 spread 做抗锯齿，最外一圈像素只按覆盖率上色。
// Viva 的 RoundedOutline 遇到小数几何会把整圈取整成偏内一像素、留下缝，所以拆成两圈：整数像素的一圈，
// 加外面一圈 1 设备像素宽、不透明度乘以小数部分的细环（覆盖率）。
func (l *Look) PaintRingBand(d *ui.DrawContext, width float32, color tokens.Color, radius float32) {
	scale := d.Canvas.PixelScale()
	outer := SnapRect(d.Rect, scale)
	if outer.Width <= 0 || outer.Height <= 0 || color.A <= 0 || width <= 0 {
		return
	}
	px := 1 / scale
	whole := float32(math.Floor(float64(width/px)+1e-4)) * px
	frac := (width - whole) / px
	band := func(w float32, c tokens.Color) {
		ring := gfx.Rect{X: outer.X - w, Y: outer.Y - w, Width: outer.Width + 2*w, Height: outer.Height + 2*w}
		d.Canvas.RoundedOutline(ring, radius+w, w, d.Fade(l.Col(c)))
	}
	if whole > 0 {
		band(whole, color)
	}
	if frac > 0.01 {
		// 外圈只有 px 宽：以 whole+px 为外沿、px 为厚度。
		ring := gfx.Rect{X: outer.X - whole - px, Y: outer.Y - whole - px, Width: outer.Width + 2*(whole+px), Height: outer.Height + 2*(whole+px)}
		d.Canvas.RoundedOutline(ring, radius+whole+px, px, d.Fade(l.Col(Fade(color, frac))))
	}
}
