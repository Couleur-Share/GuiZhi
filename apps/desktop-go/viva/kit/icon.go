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

// PaintIcon 把一枚图标画进 bounds（居中，尺寸与原点落在整设备像素上——Chromium 对内联 SVG 的盒子同样做像素对齐）。
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
	scale := canvas.PixelScale()
	side := math.Min(float64(bounds.Width), float64(bounds.Height))
	size := int(math.Floor(side*float64(scale) + 0.5))
	if size <= 0 {
		return
	}
	x := (float64(bounds.X)+float64(bounds.Width)*0.5)*float64(scale) - float64(size)*0.5
	y := (float64(bounds.Y)+float64(bounds.Height)*0.5)*float64(scale) - float64(size)*0.5
	origin := gfx.Point{X: float32(math.Floor(x+0.5)) / scale, Y: float32(math.Floor(y+0.5)) / scale}
	paintCoverage(canvas, iconCoverage(ic, size, 0, 0), size, origin, scale, tint)
}
