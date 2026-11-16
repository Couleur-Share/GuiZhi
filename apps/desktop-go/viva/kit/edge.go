package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// Edge 是盒子的一条边：单边边框（Tailwind 的 border-r、border-b）画在哪一边。
type Edge uint8

const (
	EdgeTop Edge = iota
	EdgeRight
	EdgeBottom
	EdgeLeft
)

// PaintEdge 在 d.Rect 的一条边上画一道 width 宽的单色线（渲染层的 border-r / border-b 等单边边框）。
// 线在边框盒之内，位置与 Chromium 一样先把盒子吸附到设备像素再取边；颜色是令牌（可带透明度）。
// 在 Box.Draw 里调用；调用方要给对应的边留出 width 的内边距，子元素才不会压在线上。
func (l *Look) PaintEdge(d *ui.DrawContext, e Edge, width float32, color tokens.Color) {
	if width <= 0 || color.A <= 0 {
		return
	}
	outer := SnapRect(d.Rect, d.Canvas.PixelScale())
	var line gfx.Rect
	switch e {
	case EdgeTop:
		line = gfx.Rect{X: outer.X, Y: outer.Y, Width: outer.Width, Height: width}
	case EdgeRight:
		line = gfx.Rect{X: outer.X + outer.Width - width, Y: outer.Y, Width: width, Height: outer.Height}
	case EdgeBottom:
		line = gfx.Rect{X: outer.X, Y: outer.Y + outer.Height - width, Width: outer.Width, Height: width}
	default:
		line = gfx.Rect{X: outer.X, Y: outer.Y, Width: width, Height: outer.Height}
	}
	d.Canvas.Fill(line, d.Fade(l.Col(color)))
}
