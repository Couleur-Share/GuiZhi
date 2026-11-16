package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// 栅格化是图标与渲染层逐像素对齐的基础，这里用解析可算的图形核对：线宽、位置、面积、平移不变性、缓存与回退路径。

func strokeIcon(cmds ...gfx.PathCommand) gfx.Icon {
	return gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: cmds}
}

func sumCoverage(cov []uint8) float64 {
	var s float64
	for _, c := range cov {
		s += float64(c) / 255
	}
	return s
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s：得到 %.4f，期望 %.4f（容差 %.4f）", name, got, want, tol)
	}
}

// 24px、scale 1：竖线 x=12、宽 2 → 第 11、12 列整格覆盖；16px：线宽 1.333、中心落在像素边界 → 第 7、8 列各 2/3。
func TestRasterizeVerticalLineCoverage(t *testing.T) {
	ic := strokeIcon(gfx.MoveTo(12, 4), gfx.LineTo(12, 20))
	cov := rasterizeIcon(ic, 24, 0, 0)
	for y := 8; y < 16; y++ {
		for x := 0; x < 24; x++ {
			want := 0.0
			if x == 11 || x == 12 {
				want = 255
			}
			if got := float64(cov[y*24+x]); math.Abs(got-want) > 1 {
				t.Fatalf("24px (%d,%d)：覆盖度 %v，期望 %v", x, y, got, want)
			}
		}
	}
	cov = rasterizeIcon(ic, 16, 0, 0)
	for y := 6; y < 11; y++ {
		for x := 0; x < 16; x++ {
			want := 0.0
			if x == 7 || x == 8 {
				want = 255 * (4.0 / 3.0 / 2)
			}
			if got := float64(cov[y*16+x]); math.Abs(got-want) > 2 {
				t.Fatalf("16px (%d,%d)：覆盖度 %v，期望 %.1f", x, y, got, want)
			}
		}
	}
}

// 孤点按圆头画成圆盘：面积 π·h²。
func TestRasterizeLonePointIsDisc(t *testing.T) {
	ic := strokeIcon(gfx.MoveTo(12, 12))
	cov := rasterizeIcon(ic, 48, 0, 0) // scale 2，线宽 4px → 半径 2px
	near(t, "圆盘面积", sumCoverage(cov), math.Pi*4, 0.15)
}

// 圆环（四段三次曲线）：面积 2π·r·w。
func TestRasterizeCircleRingArea(t *testing.T) {
	const k = 0.5522847498
	r := 8.0
	ic := strokeIcon(
		gfx.MoveTo(12+float32(r), 12),
		gfx.CubicTo(12+float32(r), 12+float32(r*k), 12+float32(r*k), 12+float32(r), 12, 12+float32(r)),
		gfx.CubicTo(12-float32(r*k), 12+float32(r), 12-float32(r), 12+float32(r*k), 12-float32(r), 12),
		gfx.CubicTo(12-float32(r), 12-float32(r*k), 12-float32(r*k), 12-float32(r), 12, 12-float32(r)),
		gfx.CubicTo(12+float32(r*k), 12-float32(r), 12+float32(r), 12-float32(r*k), 12+float32(r), 12),
		gfx.ClosePath(),
	)
	cov := rasterizeIcon(ic, 24, 0, 0)
	near(t, "圆环面积", sumCoverage(cov), 2*math.Pi*8*2, 100.5*0.01)
}

// 路径拉平后，圆上的点到圆心的距离偏差不超过拉平容差的几倍。
func TestFlattenCircleStaysOnCircle(t *testing.T) {
	const k = 0.5522847498
	ic := strokeIcon(
		gfx.MoveTo(20, 12),
		gfx.CubicTo(20, 12+8*k, 12+8*k, 20, 12, 20),
		gfx.CubicTo(12-8*k, 20, 4, 12+8*k, 4, 12),
		gfx.CubicTo(4, 12-8*k, 12-8*k, 4, 12, 4),
		gfx.CubicTo(12+8*k, 4, 20, 12-8*k, 20, 12),
		gfx.ClosePath(),
	)
	segs := flattenIcon(ic, 1, 0, 0)
	if len(segs) < 16 {
		t.Fatalf("圆只拉平成 %d 段", len(segs))
	}
	worst := 0.0
	for _, s := range segs {
		worst = math.Max(worst, math.Abs(math.Hypot(s.bx-12, s.by-12)-8))
	}
	// 三次曲线逼近圆本身有 0.027% 的半径误差（约 0.002px），拉平再加 0.02px。
	if worst > 0.03 {
		t.Errorf("拉平后离圆最远 %.4fpx", worst)
	}
}

// 图标平移整数像素，覆盖度只是跟着平移。
func TestRasterizeTranslationInvariance(t *testing.T) {
	ic := strokeIcon(gfx.MoveTo(6, 8), gfx.LineTo(14, 10), gfx.QuadTo(18, 14, 12, 18))
	base := rasterizeIcon(ic, 32, 0, 0)
	moved := rasterizeIcon(ic, 32, 2, 1)
	for y := 4; y < 28; y++ {
		for x := 4; x < 28; x++ {
			if a, b := base[y*32+x], moved[(y+1)*32+x+2]; a != b {
				t.Fatalf("(%d,%d) 平移后覆盖度 %d ≠ %d", x, y, a, b)
			}
		}
	}
}

// 翻转对称的图形（加号）覆盖度左右对称。
func TestRasterizePlusIsSymmetric(t *testing.T) {
	ic := strokeIcon(gfx.MoveTo(5, 12), gfx.LineTo(19, 12), gfx.MoveTo(12, 5), gfx.LineTo(12, 19))
	cov := rasterizeIcon(ic, 24, 0, 0)
	for y := 0; y < 24; y++ {
		for x := 0; x < 12; x++ {
			if a, b := cov[y*24+x], cov[y*24+23-x]; absDiff(a, b) > 1 {
				t.Fatalf("(%d,%d) 与镜像格覆盖度 %d ≠ %d", x, y, a, b)
			}
			if a, b := cov[y*24+x], cov[x*24+y]; absDiff(a, b) > 1 {
				t.Fatalf("(%d,%d) 与转置格覆盖度 %d ≠ %d", x, y, a, b)
			}
		}
	}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// 同一枚图标同一尺寸的覆盖度只算一次。
func TestIconCoverageIsCached(t *testing.T) {
	ic := strokeIcon(gfx.MoveTo(4, 12), gfx.LineTo(20, 12))
	a := iconCoverage(ic, 20, 0, 0)
	b := iconCoverage(ic, 20, 0, 0)
	if &a[0] != &b[0] {
		t.Error("相同键应该命中缓存")
	}
	if c := iconCoverage(ic, 21, 0, 0); &c[0] == &a[0] {
		t.Error("不同尺寸不应共用缓存")
	}
}

// PaintIcon 画出的矩形：全部落在图标框内，透明度之和等于覆盖度之和乘色彩透明度；缩放 2 时每个像素是 0.5 逻辑像素。
func TestPaintIconRectangles(t *testing.T) {
	ic := strokeIcon(gfx.MoveTo(4, 12), gfx.LineTo(20, 12), gfx.MoveTo(12, 4), gfx.LineTo(12, 20))
	tint := gfx.Color{Red: 0.2, Green: 0.3, Blue: 0.4, Alpha: 0.8}
	for _, scale := range []float32{1, 2} {
		var canvas gfx.Canvas
		canvas.SetPixelScale(scale)
		bounds := gfx.Rect{X: 10, Y: 20, Width: 16, Height: 16}
		PaintIcon(&canvas, ic, bounds, tint)
		rects := canvas.Rectangles()
		if len(rects) == 0 {
			t.Fatalf("scale %v：没有画出任何矩形", scale)
		}
		var mass float64
		for _, r := range rects {
			b := r.Bounds
			if b.X < bounds.X-1e-3 || b.Y < bounds.Y-1e-3 || b.X+b.Width > bounds.X+bounds.Width+1e-3 || b.Y+b.Height > bounds.Y+bounds.Height+1e-3 {
				t.Fatalf("scale %v：矩形 %+v 超出图标框 %+v", scale, b, bounds)
			}
			if r.Color.Red != tint.Red || r.Color.Alpha > tint.Alpha+1e-6 {
				t.Fatalf("scale %v：矩形颜色 %+v 与色调不符", scale, r.Color)
			}
			mass += float64(r.Color.Alpha) * float64(b.Width) * float64(b.Height) * float64(scale) * float64(scale)
		}
		size := int(16 * scale)
		want := float64(tint.Alpha) * sumCoverage(rasterizeIcon(ic, size, 0, 0))
		near(t, "透明度质量", mass, want, want*0.001)
	}
}

// 透明度为 0 什么也不画；多色图标交还 Viva 原生路径（画三角形，不画矩形）。
func TestPaintIconSkipsAndFallsBack(t *testing.T) {
	ic := strokeIcon(gfx.MoveTo(4, 12), gfx.LineTo(20, 12))
	var canvas gfx.Canvas
	canvas.SetPixelScale(1)
	PaintIcon(&canvas, ic, gfx.Rect{Width: 24, Height: 24}, gfx.Color{Alpha: 0})
	if len(canvas.Rectangles())+len(canvas.Triangles()) != 0 {
		t.Error("透明度为 0 不应画任何东西")
	}
	multi := gfx.Icon{ViewBox: 24, Layers: []gfx.IconLayer{{
		Fill: gfx.Color{Red: 1, Alpha: 1},
		Commands: []gfx.PathCommand{
			gfx.MoveTo(4, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 20), gfx.LineTo(4, 20), gfx.ClosePath(),
		},
	}}}
	if rasterizable(multi) {
		t.Fatal("多色图标不应走栅格化")
	}
	PaintIcon(&canvas, multi, gfx.Rect{Width: 24, Height: 24}, gfx.Color{Alpha: 1})
	if len(canvas.Rectangles()) != 0 || len(canvas.Triangles()) == 0 {
		t.Errorf("多色图标应由 Viva 画成三角形：矩形 %d、三角形 %d", len(canvas.Rectangles()), len(canvas.Triangles()))
	}
}
