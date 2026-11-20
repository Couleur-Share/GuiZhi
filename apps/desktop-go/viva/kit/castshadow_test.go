package kit

import (
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// castShadowPage 打开一页：只画 caster 的外阴影中落在 clip 里的部分（盒子本身不画），返回截图。
func castShadowPage(t *testing.T, caster, clip gfx.Rect, sh tokens.Shadow) *harness {
	t.Helper()
	return openPage(t, still(), 480, 360, func(l *Look, c ui.C) {
		ui.Slot(c).Size(480, 360).Draw(func(d *ui.DrawContext) {
			l.PaintCastShadow(d, caster, clip, 0, sh)
		})
	})
}

// cssShadowAlpha 是 CSS box-shadow 在像素中心 (x, y) 的透明度，按定义用误差函数直接算（同 expectedShadowAlpha，阴影由参数给）。
func cssShadowAlpha(sh tokens.Shadow, caster gfx.Rect, x, y float64) float64 {
	sigma := float64(sh.Blur) / 2
	ax, bx := float64(caster.X-sh.Spread+sh.X), float64(caster.X+caster.Width+sh.Spread+sh.X)
	ay, by := float64(caster.Y-sh.Spread+sh.Y), float64(caster.Y+caster.Height+sh.Spread+sh.Y)
	gx := normalCDF((x-ax)/sigma) - normalCDF((x-bx)/sigma)
	gy := normalCDF((y-ay)/sigma) - normalCDF((y-by)/sigma)
	return float64(sh.Color.A) * gx * gy
}

// 台账 B-046 (7)(c)：二级侧栏的阴影（浅色 0 1px 2px 0 rgba(0,0,0,.04)）在右缘外两列是 3、1 个灰阶——大盒子走 Viva 的距离场阴影
// 只画出 3、0，右缘外第二列的 1 个灰阶落不到；PaintCastShadow 按 CSS 的高斯定义逐设备像素取样，列列都对。
func TestPaintCastShadowKeepsTheSecondColumnOfASmallBlur(t *testing.T) {
	sh := tokens.Shadow{Y: 1, Blur: 2, Color: tokens.RGBA(0, 0, 0, 0.04)}
	caster := gfx.Rect{X: 100, Y: 20, Width: 288, Height: 320}
	clip := gfx.Rect{X: 388, Y: 20, Width: 6, Height: 320}
	h := castShadowPage(t, caster, clip, sh)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	bg := img.RGBAAt(2, 2) // 页面底色（浅灰），盖在它上面的灰阶数与白底上相同：3、1、0
	const y = 180
	// 右缘外三列：3、1、0 个灰阶（渲染层读数），逐列与定义对拍。
	for i, wantDelta := range []int{3, 1, 0, 0} {
		x := 388 + i
		got := int(bg.R) - int(img.RGBAAt(x, y).R)
		if got != wantDelta {
			t.Errorf("x=%d 暗 %d 个灰阶，期望 %d", x, got, wantDelta)
		}
		def := float64(bg.R) * cssShadowAlpha(sh, caster, float64(x)+0.5, y+0.5)
		if math.Abs(float64(got)-def) > 0.75 {
			t.Errorf("x=%d 暗 %d 个灰阶，按 CSS 定义应是 %.2f", x, got, def)
		}
	}
	// 只画在 clip 里：盒子里面、盒子左边与上边、clip 之外的右边都没有。
	for _, p := range [][2]int{{387, y}, {200, y}, {98, y}, {99, y}, {394, y}, {450, y}, {392, 15}, {392, 345}} {
		if got := img.RGBAAt(p[0], p[1]); got != bg {
			t.Errorf("(%d,%d) 不该有阴影：背景 %v，实际 %v", p[0], p[1], bg, got)
		}
	}
}

// 带水平偏移与扩散的阴影同样逐像素与定义一致（缩放 1 的设备像素网格；网格线落在整设备像素上，缩放不为 1 时的对齐由 TestGaussLinesAlignToDevicePixels 守住）。
func TestPaintCastShadowMatchesTheDefinitionWithOffsetAndSpread(t *testing.T) {
	sh := tokens.Shadow{X: 2, Y: 3, Blur: 6, Spread: 1, Color: tokens.RGBA(0, 0, 0, 0.5)}
	caster := gfx.Rect{X: 100, Y: 40, Width: 200, Height: 200}
	clip := gfx.Rect{X: 296, Y: 40, Width: 30, Height: 200}
	h := castShadowPage(t, caster, clip, sh)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	bg := float64(img.RGBAAt(2, 2).R)
	worst, at := 0.0, ""
	for y := 80; y < 200; y += 7 {
		for x := 300; x < 326; x++ {
			want := bg * (1 - cssShadowAlpha(sh, caster, float64(x)+0.5, float64(y)+0.5))
			got := float64(img.RGBAAt(x, y).R)
			if d := math.Abs(got - want); d > worst {
				worst, at = d, fmt.Sprintf("x=%d y=%d", x, y)
			}
		}
	}
	if worst > 1.5 {
		t.Errorf("最大偏差 %.2f 灰阶，超出 1.5（%s）", worst, at)
	}
}

// 整格取格中心的值：σ=2 的深色侧栏阴影（0 2px 4px 0 rgba(0,0,0,.4)）第四列的定义值是 0.016 × 底色，
// 格边上取值再线性插到像素中心会偏大近三成（0.045 对 0.040），取整后多出 1 个灰阶——渲染层的读数是 5、3、1、0。
// 取整之前的值与定义的偏差不超过半个灰阶（逐列比，不放宽到 1.5 个灰阶的通用容差）。
func TestPaintCastShadowSamplesTheCellCenter(t *testing.T) {
	sh := tokens.Shadow{Y: 2, Blur: 4, Color: tokens.RGBA(0, 0, 0, 0.4)}
	caster := gfx.Rect{X: 100, Y: 20, Width: 288, Height: 320}
	clip := gfx.Rect{X: 388, Y: 20, Width: 8, Height: 320}
	h := castShadowPage(t, caster, clip, sh)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	bg := img.RGBAAt(2, 2)
	for _, y := range []int{120, 200, 260} {
		for x := 388; x < 396; x++ {
			got := float64(bg.R) - float64(img.RGBAAt(x, y).R)
			def := float64(bg.R) * cssShadowAlpha(sh, caster, float64(x)+0.5, float64(y)+0.5)
			if math.Abs(got-def) > 0.51 {
				t.Errorf("x=%d y=%d 暗 %.0f 个灰阶，按定义是 %.2f（偏差 %.2f，超出半个灰阶）", x, y, got, def, math.Abs(got-def))
			}
		}
	}
}

// 没有阴影的令牌什么都不画。
func TestPaintCastShadowDoesNothingWithoutAShadow(t *testing.T) {
	caster := gfx.Rect{X: 100, Y: 20, Width: 288, Height: 320}
	h := castShadowPage(t, caster, gfx.Rect{X: 388, Y: 20, Width: 6, Height: 320}, tokens.Shadow{})
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	bg := img.RGBAAt(2, 2)
	for x := 380; x < 400; x++ {
		if got := img.RGBAAt(x, 180); got != bg {
			t.Fatalf("x=%d 不该有阴影：背景 %v，实际 %v", x, bg, got)
		}
	}
}
