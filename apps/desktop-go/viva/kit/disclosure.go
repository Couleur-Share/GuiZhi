package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// DisclosureMarker 是渲染层给 <summary> 的 disclosure-closed 标记排出来的几何（Chromium LayoutNG 的 ListMarker：
// list-style-position: inside 的符号标记）。量都是逻辑像素；内部按设备像素做整数运算，与渲染层是同一套取整。
//
// 标记占一段行内宽度（Advance）：文字从 <summary> 左缘起让出这么多。三角形（Tri）是边长 bullet 的方块里的实心三角，
// 顶点在左上、右中、左下；位置相对 <summary> 行盒左上角。
type DisclosureMarker struct {
	Advance float32
	Tri     gfx.Rect
}

// ChromeDisclosureMarker 按行内文字的字号、行高与设备像素比算标记的几何（字重取常规档：标记画在 <summary> 自己的字体上，
// 渲染层的 summary 是 cursor-pointer 的普通文字，没有加粗）。
//
// 整数运算（ascent 是该字号取整后的上升高度，设备像素）：
//
//	bullet  = (ascent*2/3 + 1) / 2
//	Advance = ascent + 7                          （margin-start −1 + 符号宽 bullet+2 + margin-end ascent−(bullet+2)+8）
//	Tri     = (x 偏移, 内容区顶 + 3*(ascent − ascent*2/3)/2, bullet, bullet)
func ChromeDisclosureMarker(fontPx, linePx, scale float32) DisclosureMarker {
	if scale <= 0 {
		scale = 1
	}
	sizeDev, lineDev := float64(fontPx*scale), float64(linePx*scale)
	ascent, _ := ChromeContentRows(WeightNormal, sizeDev)
	// 内容区顶离行盒顶：基线离行顶减去上升高度（文字比行高矮时上下各留半个行距）。
	top := ChromeBaselineRow(WeightNormal, sizeDev, lineDev) - ascent
	bullet := (ascent*2/3 + 1) / 2
	return DisclosureMarker{
		Advance: float32(ascent+disclosureTail) / scale,
		Tri: gfx.Rect{
			X:      float32(disclosureInset) / scale,
			Y:      float32(top+3*(ascent-ascent*2/3)/2) / scale,
			Width:  float32(bullet) / scale,
			Height: float32(bullet) / scale,
		},
	}
}

const (
	// disclosureTail 是标记比 ascent 多占的行内宽度（kCMarkerPaddingPx 7 加上与 margin-start 抵消后的 0）。
	disclosureTail = 7
	// disclosureInset 是三角形左缘离行盒左缘的设备像素（符号的 margin-start −1 加上符号矩形自己的 x 偏移 1）。
	disclosureInset = 0
)

// PaintDisclosure 在 row（<summary> 行盒的外框）里画标记的实心三角，填 color：收起时（disclosure-closed）顶点在左上、右中、左下，
// 展开时（disclosure-open）朝下，顶点在左上、右上、下中。两种形状占同样大的方块。
func PaintDisclosure(d *ui.DrawContext, row gfx.Rect, m DisclosureMarker, open bool, color tokens.Color) {
	r := gfx.Rect{X: row.X + m.Tri.X, Y: row.Y + m.Tri.Y, Width: m.Tri.Width, Height: m.Tri.Height}
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	b := d.Canvas.PathBuilder()
	b.MoveTo(0, 0)
	if open {
		b.LineTo(r.Width, 0)
		b.LineTo(r.Width/2, r.Height)
	} else {
		b.LineTo(r.Width, r.Height/2)
		b.LineTo(0, r.Height)
	}
	b.Close()
	d.Canvas.DrawPath(b.Path(r.Width, r.Height), r, gfx.PathStyle{Fill: d.Fade(gfxColor(color))})
}
