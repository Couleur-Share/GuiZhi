package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// layerStats 返回一层阴影的总质量（alpha 之和）与重心。
func layerStats(layer []uint8, m int) (mass, cx, cy float64) {
	for y := 0; y < m; y++ {
		for x := 0; x < m; x++ {
			a := float64(layer[y*m+x]) / 255
			mass += a
			cx += a * float64(x)
			cy += a * float64(y)
		}
	}
	if mass > 0 {
		cx, cy = cx/mass, cy/mass
	}
	return mass, cx, cy
}

// 单个实心像素：阴影的总质量 = 不透明度（高斯核归一，只丢掉 3σ 以外的一点与 8 位量化的零头），重心向下偏移 DY、水平不动。
// 用不透明度 1 来量：不透明度很小时每个像素的 alpha 只有几级，8 位量化会吞掉两成质量（渲染层的滤镜中间图同样是 8 位）。
func TestDropShadowMassAndOffset(t *testing.T) {
	const n = 9
	cov := make([]uint8, n*n)
	cov[4*n+4] = 255
	layers, m, pad := dropShadowLayers(cov, n, 1, []DropShadow{{DY: 1, Blur: 2, Alpha: 1}})
	if len(layers) != 1 {
		t.Fatalf("应有 1 层阴影，得到 %d", len(layers))
	}
	if pad != 1+3 || m != n+2*pad {
		t.Fatalf("外扩应是 偏移 1 + 3σ(=3)，得到 pad=%d m=%d", pad, m)
	}
	mass, cx, cy := layerStats(layers[0], m)
	if math.Abs(mass-1) > 0.03 {
		t.Fatalf("阴影总质量应约为 1，得到 %.4f", mass)
	}
	if math.Abs(cx-float64(4+pad)) > 0.05 || math.Abs(cy-float64(4+pad+1)) > 0.05 {
		t.Fatalf("重心应在 (%d, %d)（图标像素向下 1），得到 (%.2f, %.2f)", 4+pad, 4+pad+1, cx, cy)
	}
}

// 标准差取模糊半径的一半：半径 2 的核比半径 1 的宽（峰值更低）。
func TestDropShadowBlurIsHalfRadius(t *testing.T) {
	const n = 9
	cov := make([]uint8, n*n)
	cov[4*n+4] = 255
	peak := func(blur float32) float64 {
		layers, _, _ := dropShadowLayers(cov, n, 1, []DropShadow{{DY: 0, Blur: blur, Alpha: 1}})
		var best uint8
		for _, v := range layers[0] {
			best = max(best, v)
		}
		return float64(best) / 255
	}
	// σ=1 的二维高斯峰值 = 1/(2πσ²)·(采样归一后约 0.15)；σ=0.5 约 0.5。
	if p2, p1 := peak(2), peak(1); !(p2 < 0.2 && p1 > 0.4 && p1 < 0.7) {
		t.Fatalf("模糊半径 2（σ=1）的峰值应约 0.15、半径 1（σ=0.5）约 0.5，得到 %.3f 与 %.3f", p2, p1)
	}
}

// 第二层的输入是「图标加上第一层的阴影」：它的阴影质量比「不透明度 × 图标质量」多（不透明度取大一点，量化噪声盖不住增量）。
func TestDropShadowSecondLayerSeesFirst(t *testing.T) {
	const n = 9
	cov := make([]uint8, n*n)
	cov[4*n+4] = 255
	layers, m, _ := dropShadowLayers(cov, n, 1, []DropShadow{{DY: 0, Blur: 2, Alpha: 0.5}, {DY: 0, Blur: 1, Alpha: 0.5}})
	if len(layers) != 2 {
		t.Fatalf("应有 2 层阴影，得到 %d", len(layers))
	}
	mass2, _, _ := layerStats(layers[1], m)
	// 第二层的输入质量 = 1（图标）+ 0.5×(1−重叠)，所以阴影质量约 0.5×1.4；只看图标的话是 0.5。
	if mass2 < 0.6 || mass2 > 0.8 {
		t.Fatalf("第二层质量应约 0.7（输入含第一层的阴影），得到 %.4f", mass2)
	}
}

// 空图标没有阴影。
func TestDropShadowFromNothingIsNothing(t *testing.T) {
	layers, _, _ := dropShadowLayers(make([]uint8, 16*16), 16, 1, TailwindDropShadow)
	for k, layer := range layers {
		for i, v := range layer {
			if v != 0 {
				t.Fatalf("第 %d 层第 %d 个像素不该有阴影：%d", k, i, v)
			}
		}
	}
}

// 画出来：白色对勾落在灰底上，阴影只让图标下方的像素变暗，远处不受影响，图标实心处仍是白色。
func TestIconDropShadowPaintsBelowTheIcon(t *testing.T) {
	render := func(shadow bool) (pix func(x, y int) float64) {
		h := openPage(t, still(), 60, 60, func(l *Look, c ui.C) {
			ui.Column(c).PadLeft(20).PadTop(20).Background(gfx.RGBA(128, 128, 128, 1)).Children(func() {
				if shadow {
					l.IconDropShadow(c, icons.Check, 16, tokens.RGBA(255, 255, 255, 1), TailwindDropShadow)
				} else {
					l.Icon(c, icons.Check, 16, tokens.RGBA(255, 255, 255, 1))
				}
			})
		})
		img, err := h.session.Image()
		if err != nil {
			t.Fatal(err)
		}
		return func(x, y int) float64 {
			r, g, b, _ := img.At(x, y).RGBA()
			return float64(r+g+b) / 3 / 257
		}
	}
	plain, shaded := render(false), render(true)
	darker, lighter, changedFar := 0, 0, 0
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			d := shaded(x, y) - plain(x, y)
			inBox := x >= 20-8 && x < 20+16+8 && y >= 20-8 && y < 20+16+8
			if !inBox && math.Abs(d) > 0.5 {
				changedFar++
			}
			switch {
			case d < -0.5:
				darker++
			case d > 0.5:
				lighter++
			}
			// 最深处：黑色 10%（再加第二层 6%）叠在 128 的灰上，最多暗约 128×(1−0.9×0.94)≈20 级。
			if d < -22 {
				t.Fatalf("(%d,%d) 变暗 %.1f 级，超过两层阴影的上限", x, y, -d)
			}
		}
	}
	if darker == 0 {
		t.Fatal("带阴影的图标没有任何像素比不带阴影的暗")
	}
	if lighter != 0 {
		t.Fatalf("阴影不应让任何像素变亮，却有 %d 个", lighter)
	}
	if changedFar != 0 {
		t.Fatalf("图标外 8 像素以外不该有变化，却有 %d 个", changedFar)
	}
	// 图标自己的像素（几乎实心处）几乎不受阴影影响：覆盖度 c 的像素只有 (1−c) 的部分透出底下被压暗的底色。
	solid := 0
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			if plain(x, y) < 240 {
				continue
			}
			solid++
			if math.Abs(shaded(x, y)-plain(x, y)) > 3 {
				t.Fatalf("(%d,%d) 是图标的实心处，不该被阴影改动：%.1f 对 %.1f", x, y, shaded(x, y), plain(x, y))
			}
		}
	}
	if solid == 0 {
		t.Fatal("找不到图标的实心像素，测试前提不成立")
	}
}
