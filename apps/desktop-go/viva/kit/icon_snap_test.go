package kit

import (
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

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

// 图标居中时 snapShiftLocal 就是 snapShiftAxis（后者只是前者在 local = (size−side)/2 上的特例）。
func TestSnapShiftLocalMatchesCenteredAxis(t *testing.T) {
	for _, size := range []float32{19.25, 20.5, 22.5} {
		for _, pos := range []float32{10.25, 10.875, 60.5, 711.766} {
			for _, side := range []float32{12.25, 13.5, 14} {
				want := snapShiftAxis(pos, -size/2, size, side)
				if got := snapShiftLocal(pos, -size/2, (size-side)/2); got != want {
					t.Fatalf("size=%v pos=%v side=%v：snapShiftLocal = %v，snapShiftAxis = %v", size, pos, side, got, want)
				}
			}
		}
	}
}

// 搜索框右端的一组（B-042，小字号档 1280×800 的 list-search-empty）。渲染层的组是 absolute top-1/2 -translate-y-1/2：
// 组外框 y=10.25、高 20.5（快捷键提示 20.5 高），所以 L = 20.5、平移 −10.25；清除钮 19.25 高在组里居中（偏 0.625），
// 内边距 3.5，图标在组内偏 4.125。Chromium 的落笔原点：round(20.5)=21、余数 −0.5，4.125−0.5 = 3.625 取整成 4，
// 原点 = 21 − 10.25 + 4 = 14.75；版面位置是 14.375，差 +0.375。x 方向：组外框 x=711.766，图标偏 3.5：
// round(711.766)=712、余数 −0.234，3.5−0.234 = 3.266 取整成 3，原点 715（版面位置 715.266，差 −0.266）。
func TestSnapShiftLocalSearchClearGroup(t *testing.T) {
	dy := snapShiftLocal(10.25, -10.25, 4.125)
	if math.Abs(float64(dy)-0.375) > 1e-5 {
		t.Errorf("dy = %v，要 +0.375", dy)
	}
	if got := 10.25 + 4.125 + dy; math.Abs(float64(got)-14.75) > 1e-5 {
		t.Errorf("y 落笔位置 = %v，要 14.75", got)
	}
	dx := snapShiftLocal(711.766, 0, 3.5)
	if math.Abs(float64(dx)+0.266) > 1e-4 {
		t.Errorf("dx = %v，要 −0.266", dx)
	}
	if got := 711.766 + 3.5 + dx; math.Abs(float64(got)-715) > 1e-4 {
		t.Errorf("x 落笔位置 = %v，要 715", got)
	}
}

// groupXInks 按搜索框右端那一组的版面并排画两份清除钮的 X（组 y=10.25、高 20.5；钮 19.25 方块；12.25px 的 X；组左缘的小数部分都是 .766）：
// 第 0 格用普通 Icon，第 1 格用 IconInTranslated。返回两格里 X 的墨迹包围盒（设备像素，缩放 1，相对格子左上角）。
// 两份放在同一页的同一个会话里：每个会话要 0.3 s 左右，整个包的测试时间有每包 10 s 的线。
func groupXInks(t *testing.T) (plain, moved image.Rectangle) {
	t.Helper()
	const width = 400
	groups := make([]gfx.Rect, 2)
	h := openPage(t, still(), 2*width, cellH, func(l *Look, c ui.C) {
		ui.Row(c).Children(func() {
			for i := 0; i < 2; i++ {
				cc := c.Key(fmt.Sprintf("cell%d", i))
				ui.Column(cc).Width(width).PadLeft(311.766).PadTop(10.25).Children(func() {
					ui.Row(cc.Key("group")).Items(ui.Center).Gap(3.5).Draw(func(d *ui.DrawContext) { groups[i] = d.Rect }).Children(func() {
						ui.Row(cc.Key("btn")).Pad(3.5).Children(func() {
							if i == 1 {
								l.IconInTranslated(cc.Key("icon"), icons.X, 12.25, l.T.Foreground, &groups[i])
							} else {
								l.Icon(cc.Key("icon"), icons.X, 12.25, l.T.Foreground)
							}
						})
						ui.Row(cc.Key("chip")).Size(70, 20.5)
					})
				})
			}
		})
	})
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	return cellInk(img, 0, width), cellInk(img, 1, width)
}

// IconInTranslated 把 X 画在渲染层的位置：普通 Icon 吸附原点 (315, 14)，墨迹第 16..23 行（B-042 里 Viva 的读数）；
// 变换容器的规则给出 y=14.75，墨迹第 17..24 行（B-042 里 React 的读数），x 仍是 315（格内坐标）。
func TestIconInTranslatedFollowsChromiumPaintOffset(t *testing.T) {
	plain, moved := groupXInks(t)
	if plain.Empty() || moved.Empty() {
		t.Fatalf("没有画出 X：普通 %v，变换 %v", plain, moved)
	}
	if plain.Min.Y != 16 || plain.Max.Y != 24 {
		t.Errorf("普通 Icon 的墨迹行应为 16..23，得 %d..%d", plain.Min.Y, plain.Max.Y-1)
	}
	if moved.Min.Y != 17 || moved.Max.Y != 25 {
		t.Errorf("变换容器里的墨迹行应为 17..24，得 %d..%d", moved.Min.Y, moved.Max.Y-1)
	}
	if moved.Min.X != plain.Min.X || moved.Dx() != plain.Dx() {
		t.Errorf("x 方向两者应一致（原点都在 315）：普通 %v，变换 %v", plain, moved)
	}
}