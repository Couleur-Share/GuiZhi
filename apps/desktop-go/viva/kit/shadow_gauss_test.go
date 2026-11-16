package kit

import (
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// shadowPage 打开一页：只在 caster 位置画一层 shadow（不画盒子本身），返回截图。
func shadowPage(t *testing.T, caster gfx.Rect, radius float32) *harness {
	t.Helper()
	return openPage(t, still(), 480, 360, func(l *Look, c ui.C) {
		ui.Slot(c).Size(480, 360).Draw(func(d *ui.DrawContext) {
			paintGaussianShadow(d, caster, radius, Shadow2XL[0])
		})
	})
}

// expectedShadowAlpha 是 CSS box-shadow（0 25px 50px -12px）在像素中心 (x, y) 的透明度，按定义用误差函数直接算：
// 阴影矩形（盒子收缩 12、下移 25）与 σ=25 的高斯核卷积，再乘 0.25。
func expectedShadowAlpha(caster gfx.Rect, x, y float64) float64 {
	sh := Shadow2XL[0]
	sigma := float64(sh.Blur) / 2
	ax, bx := float64(caster.X-sh.Spread+sh.X), float64(caster.X+caster.Width+sh.Spread+sh.X)
	ay, by := float64(caster.Y-sh.Spread+sh.Y), float64(caster.Y+caster.Height+sh.Spread+sh.Y)
	gx := normalCDF((x-ax)/sigma) - normalCDF((x-bx)/sigma)
	gy := normalCDF((y-ay)/sigma) - normalCDF((y-by)/sigma)
	return float64(sh.Color.A) * gx * gy
}

// 窄小盒子的阴影与 CSS 的高斯定义逐像素一致（±1 个灰阶）：这正是 Viva 自带的距离场阴影做不到的区间。
func TestGaussianShadowMatchesTheCSSDefinition(t *testing.T) {
	for _, caster := range []gfx.Rect{
		{X: 150, Y: 100, Width: 172, Height: 50},  // 单行提示
		{X: 120, Y: 90, Width: 240, Height: 94.5}, // 带详情的提示
	} {
		h := shadowPage(t, caster, 16)
		img, err := h.session.Image()
		if err != nil {
			t.Fatal(err)
		}
		bg := float64(img.RGBAAt(2, 2).R)
		worst, at := 0.0, ""
		for y := int(caster.Y); y < 360; y += 3 {
			for x := int(caster.X) - 60; x < int(caster.X+caster.Width)+60; x += 3 {
				// 投影盒子「十字」之内的像素不画阴影（由盒子自己盖住），这里只比盒子之外
				inside := float32(x) >= caster.X && float32(x) < caster.X+caster.Width &&
					float32(y) >= caster.Y && float32(y) < caster.Y+caster.Height
				if inside {
					continue
				}
				want := bg * (1 - expectedShadowAlpha(caster, float64(x)+0.5, float64(y)+0.5))
				got := float64(img.RGBAAt(x, y).R)
				if d := math.Abs(got - want); d > worst {
					worst, at = d, fmt.Sprintf("(%d,%d) 期望 %.2f 实际 %.0f", x, y, want, got)
				}
			}
		}
		if worst > 1.5 {
			t.Errorf("盒子 %v 的高斯阴影最大偏差 %.2f 灰阶，超出 1.5：%s", caster, worst, at)
		}
	}
}

// 盒子「十字」之内不画阴影：半透明的表面不会被自己的阴影压暗。
func TestGaussianShadowIsCutOutUnderTheCaster(t *testing.T) {
	caster := gfx.Rect{X: 150, Y: 100, Width: 172, Height: 50}
	h := shadowPage(t, caster, 16)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	bg := img.RGBAAt(2, 2)
	for _, p := range [][2]int{{236, 125}, {160, 125}, {310, 125}, {236, 102}, {236, 147}} {
		if got := img.RGBAAt(p[0], p[1]); got != bg {
			t.Errorf("盒子里 (%d,%d) 不该有阴影：背景 %v，实际 %v", p[0], p[1], bg, got)
		}
	}
}

// 阴影里没有拼缝：相邻格子的接缝处与格子中间的差别在插值误差之内。
func TestGaussianShadowHasNoSeams(t *testing.T) {
	caster := gfx.Rect{X: 150, Y: 100, Width: 172, Height: 50}
	h := shadowPage(t, caster, 16)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	// 沿盒子正下方一列逐像素走：相邻像素的差不超过 2 个灰阶（阴影在这里每像素变化不到 1 个灰阶）
	x := 236
	for y := 152; y < 230; y++ {
		a, b := int(img.RGBAAt(x, y).R), int(img.RGBAAt(x, y+1).R)
		if d := a - b; d > 2 || d < -2 {
			t.Errorf("x=%d 列 y=%d→%d 相邻像素差 %d 灰阶，像是格子拼缝", x, y, y+1, d)
		}
	}
}

// 什么时候走高斯网格：阴影矩形的短边不足 6σ（= 3×Blur）。
func TestGaussianShadowIsChosenOnlyForSmallCasters(t *testing.T) {
	sh := Shadow2XL[0]
	for _, c := range []struct {
		name string
		r    gfx.Rect
		want bool
	}{
		{"单行提示 172×50", gfx.Rect{Width: 172, Height: 50}, true},
		{"带详情的提示 416×252", gfx.Rect{Width: 416, Height: 252}, false},
		{"确认对话框 384×200", gfx.Rect{Width: 384, Height: 200}, false},
		{"矮对话框 384×150", gfx.Rect{Width: 384, Height: 150}, true},
	} {
		if got := gaussianShadowNeeded(c.r, sh); got != c.want {
			t.Errorf("%s：期望 %v，实际 %v", c.name, c.want, got)
		}
	}
	if gaussianShadowNeeded(gfx.Rect{Width: 10, Height: 10}, ShadowLG[0]) != true {
		t.Error("小盒子配 shadow-lg 应走高斯网格")
	}
	if gaussianShadowNeeded(gfx.Rect{Width: 10, Height: 10}, tokens.Shadow{Y: 4, Color: tokens.RGBA(0, 0, 0, 0.1)}) {
		t.Error("没有模糊的阴影不走高斯网格")
	}
}

// 网格线落在整设备像素上并升序，而且包含必须对齐的位置。
func TestGaussLinesAlignToDevicePixels(t *testing.T) {
	for _, scale := range []float32{1, 1.25, 1.5, 2} {
		lines := gaussLines(163, 310, 75, scale, 150, 166, 306, 322)
		for i, p := range lines {
			if px := float64(p) * float64(scale); math.Abs(px-math.Round(px)) > 1e-3 {
				t.Errorf("缩放 %g：网格线 %g 不在整设备像素上", scale, p)
			}
			if i > 0 && p <= lines[i-1] {
				t.Errorf("缩放 %g：网格线没有升序：%g 在 %g 之后", scale, p, lines[i-1])
			}
		}
		for _, want := range []float32{150, 166, 306, 322} {
			found := false
			for _, p := range lines {
				found = found || math.Abs(float64(p-snapEdge(want, scale))) < 1e-3
			}
			if !found {
				t.Errorf("缩放 %g：缺少必须对齐的网格线 %g", scale, want)
			}
		}
	}
}
