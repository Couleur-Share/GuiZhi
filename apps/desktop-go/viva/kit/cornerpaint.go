package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// arcKappa 是用三次贝塞尔近似四分之一圆弧的控制点系数（4/3·(√2−1)）。
const arcKappa = 0.5522847498

// PaintRoundedCorners 填一个四个角半径各不相同的圆角矩形（左上、右上、右下、左下）。
//
// PaintSurface 只会画四角同半径的圆角；渲染层里 overflow-hidden 的圆角容器把里面的子元素裁成内圆角，
// 子元素自己的圆角与容器的内圆角取交集时就会出现「靠外的两个角一个半径、靠里的两个角另一个半径」的形状
// （例如卡片/列表切换里的滑块）。这里按设备像素吸附后的矩形画路径，半径超过半边长时按半边长截断。
// 在 Box.Draw 里调用，颜色是令牌（可带透明度）。
func PaintRoundedCorners(d *ui.DrawContext, rect gfx.Rect, color tokens.Color, tl, tr, br, bl float32) {
	canvas := d.Canvas
	r := SnapRect(rect, canvas.PixelScale())
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	limit := min(r.Width, r.Height) / 2
	tl, tr, br, bl = clampRadius(tl, limit), clampRadius(tr, limit), clampRadius(br, limit), clampRadius(bl, limit)
	w, h := r.Width, r.Height
	b := canvas.PathBuilder()
	b.MoveTo(tl, 0)
	b.LineTo(w-tr, 0)
	b.CubicTo(w-tr+arcKappa*tr, 0, w, tr-arcKappa*tr, w, tr)
	b.LineTo(w, h-br)
	b.CubicTo(w, h-br+arcKappa*br, w-br+arcKappa*br, h, w-br, h)
	b.LineTo(bl, h)
	b.CubicTo(bl-arcKappa*bl, h, 0, h-bl+arcKappa*bl, 0, h-bl)
	b.LineTo(0, tl)
	b.CubicTo(0, tl-arcKappa*tl, tl-arcKappa*tl, 0, tl, 0)
	b.Close()
	canvas.DrawPath(b.Path(w, h), r, gfx.PathStyle{Fill: d.Fade(gfxColor(color))})
}

// clampRadius 把半径夹在 [0, limit]。
func clampRadius(v, limit float32) float32 { return max(0, min(v, limit)) }
