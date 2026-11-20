package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// DisclosureMarker 是渲染层给 <summary> 画的 disclosure 标记的几何（Chromium 的 list-style-type: disclosure-open / disclosure-closed，
// list-style-position: inside）。量都是逻辑像素。
//
// 标记占一段行内宽度（Advance）：文字从 <summary> 左缘起让出这么多。三角形是等边的，外接框的长边 Side；
// 朝下（展开）时长边是宽，朝右（收起）时长边是高，两种形状的外接框中心在同一条线上：基线之上 Rise 处。
//
// 几何取自渲染层实测（台账 B-043 的对照，DPR 1，小、中、大三个字号档 10.5、12、13.5px 各一份 React 参照）：
//
//	Advance  = 1.0625*字号 − 0.046875      （711、813、915 个 1/64 像素：三档的文字起点离 <summary> 左缘 11.109、12.703、14.297px，逐档差恰好 102/64）
//	Side     = 字号 × 2/3                  （7、8、8.9px：三档的三角形面积、质心反推的宽）
//	高       = Side × √3/2                 （等边；面积反推 6.05、6.76、7.75px，与等边的 6.06、6.93、7.72 相符，中档偏小 2.5% 是边缘抗锯齿的覆盖率）
//	Rise     = 字号 × 0.3275               （中心离基线：中档 3.93px，小、大档在基线取整之后同为 0.3275 倍字号）
//	左缘     = <summary> 左缘               （三角形外接框紧贴行盒左缘）
//
// 基线取整：基线的绝对位置 = 行盒顶 + 基线行，Chromium 把它四舍五入到整像素（行盒顶在 .5 处时：小档 119.5→120、大档 153.5→154），
// 三角形跟着这个整数基线走，所以 PaintDisclosure 里按绝对位置取整。
// 只在 DPR 1 实测过；DPR > 1 时按线性换算，未对照（台账 R 项里的缩放档说明）。
type DisclosureMarker struct {
	Advance  float32
	Side     float32
	Rise     float32
	Baseline float32 // 基线离行盒顶，逻辑像素（与行内文字同一套取整，ChromeBaselineRow）
}

// disclosureHeightRatio 是等边三角形的高与边长之比（√3/2）。
var disclosureHeightRatio = float32(math.Sqrt(3) / 2)

// ChromeDisclosureMarker 按行内文字的字号、行高（逻辑像素）与设备像素比算标记的几何（字重取常规档：
// 标记画在 <summary> 自己的字体上，渲染层的 summary 是 cursor-pointer 的普通文字，没有加粗）。
func ChromeDisclosureMarker(fontPx, linePx, scale float32) DisclosureMarker {
	if scale <= 0 {
		scale = 1
	}
	sizeDev, lineDev := float64(fontPx*scale), float64(linePx*scale)
	return DisclosureMarker{
		Advance:  float32(1.0625*sizeDev-0.046875) / scale,
		Side:     fontPx * 2 / 3,
		Rise:     fontPx * 0.3275,
		Baseline: float32(ChromeBaselineRow(WeightNormal, sizeDev, lineDev)) / scale,
	}
}

// PaintDisclosure 在 row（<summary> 行盒的外框）里画标记的实心三角，填 color：收起时（disclosure-closed）朝右，
// 顶点在左上、右中、左下；展开时（disclosure-open）朝下，顶点在左上、右上、下中。
func PaintDisclosure(d *ui.DrawContext, row gfx.Rect, m DisclosureMarker, open bool, color tokens.Color) {
	if m.Side <= 0 {
		return
	}
	tall := m.Side * disclosureHeightRatio
	// 基线的绝对位置四舍五入到整像素，中心在它之上 Rise。
	base := float32(math.Floor(float64(row.Y+m.Baseline) + 0.5))
	cy := base - m.Rise
	var r gfx.Rect
	b := d.Canvas.PathBuilder()
	b.MoveTo(0, 0)
	if open {
		r = gfx.Rect{X: row.X, Y: cy - tall/2, Width: m.Side, Height: tall}
		b.LineTo(r.Width, 0)
		b.LineTo(r.Width/2, r.Height)
	} else {
		r = gfx.Rect{X: row.X, Y: cy - m.Side/2, Width: tall, Height: m.Side}
		b.LineTo(r.Width, r.Height/2)
		b.LineTo(0, r.Height)
	}
	b.Close()
	d.Canvas.DrawPath(b.Path(r.Width, r.Height), r, gfx.PathStyle{Fill: d.Fade(gfxColor(color))})
}
