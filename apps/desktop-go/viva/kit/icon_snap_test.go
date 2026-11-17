package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/icons"
)

// 大字号档（根字号 18px）的清除钮：外框 [666.75 69.75 22.5 22.5]（absolute right-1.5 top-1/2 -translate-y-1/2），
// 里面 13.5px 的 X。版面位置在 L = (666.75, 81)：x 取整成 667、余数 −0.25，容器内偏移 4.5 − 0.25 = 4.25 取整成 4；
// y 的余数是 0，容器内偏移 4.5 取整成 5。实测 React 的 X 比版面位置低半个像素、x 落在整数 671。
func TestTranslatedSnapShiftClearButtonLarge(t *testing.T) {
	rect := gfx.Rect{X: 666.75, Y: 69.75, Width: 22.5, Height: 22.5}
	dx, dy := translatedSnapShift(rect, 0, -rect.Height/2, 13.5)
	if math.Abs(float64(dx)+0.25) > 1e-6 || math.Abs(float64(dy)-0.5) > 1e-6 {
		t.Fatalf("偏移 = (%v, %v)，要 (-0.25, +0.5)", dx, dy)
	}
	// 落笔原点 = 版面位置 + 偏移 = (671.25−0.25, 74.25+0.5) = (671, 74.75)
	if got := rect.X + (rect.Width-13.5)/2 + dx; math.Abs(float64(got)-671) > 1e-6 {
		t.Errorf("x 落笔位置 = %v，要 671", got)
	}
	if got := rect.Y + (rect.Height-13.5)/2 + dy; math.Abs(float64(got)-74.75) > 1e-6 {
		t.Errorf("y 落笔位置 = %v，要 74.75", got)
	}
}

// 整数尺寸（中字号档 20px 钮里 12px 图标）：版面位置都在整数上，偏移为 0——不能凭空引入偏移。
func TestTranslatedSnapShiftIntegralIsZero(t *testing.T) {
	rect := gfx.Rect{X: 500, Y: 70, Width: 20, Height: 20}
	dx, dy := translatedSnapShift(rect, 0, -10, 12)
	if dx != 0 || dy != 0 {
		t.Fatalf("整数布局的偏移应为 0，得 (%v, %v)", dx, dy)
	}
}

// 偏移永远落在 (−0.5, 0.5] 内：取整只会把落笔位置挪不到半个像素。
func TestTranslatedSnapShiftBounded(t *testing.T) {
	for _, h := range []float32{17.5, 20, 22.5, 24.5, 31.5} {
		for _, y := range []float32{60, 60.25, 60.5, 60.75, 63.125} {
			rect := gfx.Rect{X: 100.125, Y: y, Width: h, Height: h}
			dx, dy := translatedSnapShift(rect, 0, -h/2, h*0.6)
			for _, v := range []float32{dx, dy} {
				if v <= -0.5-1e-6 || v > 0.5+1e-6 {
					t.Fatalf("h=%v y=%v：偏移 %v 超出 (−0.5, 0.5]", h, y, v)
				}
			}
		}
	}
}

func rectsOf(t *testing.T, dx, dy float32, exact bool) []gfx.Rectangle {
	t.Helper()
	var canvas gfx.Canvas
	canvas.SetPixelScale(1)
	bounds := gfx.Rect{X: 20, Y: 30, Width: 14, Height: 14}
	tint := gfx.Color{Red: 0.35, Green: 0.38, Blue: 0.45, Alpha: 1}
	if exact {
		PaintIconExact(&canvas, icons.X, bounds, tint, dx, dy)
	} else {
		PaintIcon(&canvas, icons.X, bounds, tint)
	}
	return append([]gfx.Rectangle(nil), canvas.Rectangles()...)
}

// 原点已在整设备像素上、偏移为 0 时，PaintIconExact 与 PaintIcon 画出同样的图元。
func TestPaintIconExactMatchesSnappedAtWholePixels(t *testing.T) {
	ra, rb := rectsOf(t, 0, 0, false), rectsOf(t, 0, 0, true)
	if len(ra) == 0 || len(ra) != len(rb) {
		t.Fatalf("图元数不同：%d / %d", len(ra), len(rb))
	}
	for i := range ra {
		if ra[i].Bounds != rb[i].Bounds || ra[i].Color != rb[i].Color {
			t.Fatalf("第 %d 个图元不同：%+v / %+v", i, ra[i], rb[i])
		}
	}
}

// 半个像素的偏移会改变覆盖度（图标确实画在小数位置，不是被吸附回整像素）。
func TestPaintIconExactShiftChangesCoverage(t *testing.T) {
	a, b := rectsOf(t, 0, 0, true), rectsOf(t, 0, 0.5, true)
	if len(a) == len(b) {
		same := true
		for i := range a {
			if a[i].Bounds != b[i].Bounds || a[i].Color != b[i].Color {
				same = false
				break
			}
		}
		if same {
			t.Fatal("偏移 0.5 后图元与不偏移的完全相同：偏移没有生效")
		}
	}
}
