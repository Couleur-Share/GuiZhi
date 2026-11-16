package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// Icon 画一枚 size×size 的图标（渲染层的 lucide 图标：h-4 w-4、h-5 w-5 之类），颜色是令牌（可带透明度）。
//
// 所有图标都经这里画：对照工具给 Look.IconSink 接上记录函数后，每帧每枚图标的外框会报给它，
// 用来和渲染层里的 SVG 外框逐个配对。正常运行时 IconSink 为空，没有任何额外开销。
func (l *Look) Icon(c ui.C, ic gfx.Icon, size float32, color tokens.Color) ui.Box {
	col := l.Col(color)
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		if sink := l.IconSink; sink != nil {
			sink(d.Rect)
		}
		PaintIcon(d.Canvas, ic, d.Rect, d.Fade(col))
	})
}

// iconSnapEpsilon 是吸附到整设备像素时给「刚好半个像素」的位置留的容差。渲染层的位置是 1/64 像素的定点数（精确的 .5），
// Viva 的布局是 float32 运算，同一个位置可能差一个舍入误差而落到 .5 的另一侧；1/64 的一半远大于该误差，又不会吞掉真实的 1/64 差。
const iconSnapEpsilon = 1.0 / 128

// PaintIcon 把一枚图标画进 bounds（取其中居中的正方形）。
//
// 渲染层（Chromium）对内联 SVG：盒子的原点吸附到最近的整设备像素，内容按精确的 边长/视框 缩放，边长不取整——
// 例如 12.25px 的图标在 14.375 处，原点落在 14，笔画宽 2×12.25/24 = 1.02px（2026-11-16 在 app.window.page-settings 的
// 小字号档由 React 参照的逐行墨迹量得：最大化图标的上笔画在第 15 行占 98%）。这里照此栅格化，
// 不再把边长取整后再居中（那样 12.25 画成 12，位置还会偏半个像素）。
//
// lucide 的描边图标走自己的精确栅格化（见 iconraster.go），与渲染层逐像素对齐；
// 多色图标、填充图标等其它图标交还 Viva 原生的 Canvas.DrawIcon。
func PaintIcon(canvas *gfx.Canvas, ic gfx.Icon, bounds gfx.Rect, tint gfx.Color) {
	if tint.Alpha <= 0 {
		return
	}
	if !rasterizable(ic) {
		canvas.DrawIcon(ic, bounds, tint)
		return
	}
	scale := float64(canvas.PixelScale())
	side := math.Min(float64(bounds.Width), float64(bounds.Height))
	sideDev := side * scale
	if sideDev <= 0 {
		return
	}
	// 盒子 = bounds 里居中的 side×side 正方形（图标盒子通常恰好是 bounds）。
	x0 := (float64(bounds.X) + (float64(bounds.Width)-side)/2) * scale
	y0 := (float64(bounds.Y) + (float64(bounds.Height)-side)/2) * scale
	origin := gfx.Point{
		X: float32(math.Floor(x0+0.5+iconSnapEpsilon) / scale),
		Y: float32(math.Floor(y0+0.5+iconSnapEpsilon) / scale),
	}
	n := int(math.Ceil(sideDev - 1e-6))
	paintCoverage(canvas, iconCoverageSide(ic, sideDev, n, 0, 0), n, origin, float32(scale), tint)
}
