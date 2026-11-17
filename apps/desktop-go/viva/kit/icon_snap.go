package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 变换容器里的图标落笔位置
//
// 渲染层里 absolute top-1/2 -translate-y-1/2 这类「用平移居中」的容器（搜索框的清除钮等），Chromium 的绘制规则是：
// 容器的版面位置 L（变换之前）取整后放进「绘制偏移平移」，余数 u = L − round(L) 留给子孙；
// 子孙的 SVG 盒子再用「容器内偏移 + u」取整落笔，平移本身按精确的小数加在最外层。
// 所以图标最终的原点 = round(L) + 平移 + round(local + u)，而版面（getBoundingClientRect）给的是 L + 平移 + local：
// 两者相差 round(w) − w，w = local + u，落在 (−0.5, 0.5]。典型例子：大字号档 22.5px 的清除钮里 13.5px 的 X，
// 容器内偏移 4.5 取整成 5，图标比版面位置低半个像素，墨迹整整下移一行（2026-11-16 在 app.window.imports.filtered-empty 的 large 档实测）。
//
// Viva 的版面直接按变换之后的位置排，图标又吸附到整设备像素，两者都复现不了这个半像素。这里按上面的规则算出偏移，
// 用不吸附原点的 PaintIconExact 落笔。

// translatedSnapShift 返回「被平移居中的容器」里居中图标的落笔偏移（逻辑像素，加在版面位置上）。
// rect 是容器变换之后的外框，tx、ty 是它被平移的量（-translate-y-1/2 就是 0、−高/2），side 是图标边长；图标在容器里居中。
func translatedSnapShift(rect gfx.Rect, tx, ty, side float32) (dx, dy float32) {
	return snapShiftAxis(rect.X, tx, rect.Width, side), snapShiftAxis(rect.Y, ty, rect.Height, side)
}

// snapShiftAxis 是一根轴上的偏移：pos 是变换之后的位置，t 是平移，size 是容器尺寸，side 是图标边长。
// 取整用 roundHalfUp（baseline.go），即 Chromium 的 LayoutUnit::Round：四舍五入，恰好一半向 +∞。
func snapShiftAxis(pos, t, size, side float32) float32 {
	layout := float64(pos) - float64(t) // 变换之前的版面位置 L
	u := layout - roundHalfUp(layout)
	w := float64(size-side)/2 + u
	return float32(roundHalfUp(w) - w)
}

// PaintIconExact 与 PaintIcon 相同，只是原点不吸附到整设备像素：原点取 bounds 里居中的方块的左上角再加 (dx, dy)（逻辑像素），
// 小数部分交给栅格化器（覆盖度按带偏移的原点算）。只用在 Chromium 自己把图标画在小数位置的场合（见 translatedSnapShift）。
func PaintIconExact(canvas *gfx.Canvas, ic gfx.Icon, bounds gfx.Rect, tint gfx.Color, dx, dy float32) {
	if tint.Alpha <= 0 {
		return
	}
	if !rasterizable(ic) {
		shifted := bounds
		shifted.X, shifted.Y = shifted.X+dx, shifted.Y+dy
		canvas.DrawIcon(ic, shifted, tint)
		return
	}
	scale := float64(canvas.PixelScale())
	side := math.Min(float64(bounds.Width), float64(bounds.Height))
	sideDev := side * scale
	if sideDev <= 0 {
		return
	}
	x0 := (float64(bounds.X) + (float64(bounds.Width)-side)/2 + float64(dx)) * scale
	y0 := (float64(bounds.Y) + (float64(bounds.Height)-side)/2 + float64(dy)) * scale
	ix, iy := math.Floor(x0), math.Floor(y0)
	fx, fy := x0-ix, y0-iy
	n := int(math.Ceil(sideDev + math.Max(fx, fy) - 1e-6)) // 掩码要装得下带偏移的图标
	origin := gfx.Point{X: float32(ix / scale), Y: float32(iy / scale)}
	paintCoverage(canvas, iconCoverageSide(ic, sideDev, n, fx, fy), n, origin, float32(scale), tint)
}

// IconShifted 是 Icon 的一种：落笔位置在版面位置上再加 *shift（逻辑像素，由外层在绘制时填好，见 Button 的 TranslateCentered），
// 且不吸附到整设备像素。外框（IconSink 报的、对照里与渲染层的 SVG 外框配对的）仍是版面位置。
func (l *Look) IconShifted(c ui.C, ic gfx.Icon, size float32, color tokens.Color, shift *gfx.Point) ui.Box {
	col := l.Col(color)
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		mark := l.markIcon(d)
		PaintIconExact(d.Canvas, ic, d.Rect, d.Fade(col), shift.X, shift.Y)
		l.reportIcon(d, mark)
	})
}
