package kit

import (
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// underlineCases 是渲染层里实测的下划线位置（scripts/parity/tools/probe-underline.mjs，字重 400、设备缩放 1）：
// 下划线行 − round(行盒顶)。它恰好等于「基线行 + 1」，所以公式随字号与行高走。
var underlineCases = []struct {
	size, line float32
	offset     int
}{
	{11, 16.5, 13}, {12, 18, 14}, {12.25, 17.5, 14}, {13, 19.5, 15},
	{14, 20, 16}, {15.75, 22.5, 18}, {16, 24, 19}, {18, 27, 21},
}

const underlineSample = "本地优先软件" // 六个全角字，宽度 = 6 × 字号

// 下划线在渲染层实测的那一行：1 行厚，横向正好是文字宽度；行盒顶带小数时按四舍五入落位（.5 进位）。
func TestTextUnderlineMatchesChromium(t *testing.T) {
	fracs := []float32{0, 0.25, 0.5, 0.75}
	const cellW, cellH, left, firstTop = 200, 56, 10, 10
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	h := openPage(t, still(), 800, 600, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			for i, uc := range underlineCases {
				for j, frac := range fracs {
					cc := c.Key(fmt.Sprintf("u-%d-%d", i, j))
					top := float32(firstTop+i*cellH) + frac
					ui.Column(cc).Absolute().Left(float32(left + j*cellW)).Top(top).Children(func() {
						l.TextPx(cc.Key("t"), underlineSample, uc.size, uc.line, WeightNormal, black, Underline(true))
					})
				}
			}
		})
	})
	h.frames(3)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	for i, uc := range underlineCases {
		for j, frac := range fracs {
			top := float32(firstTop+i*cellH) + frac
			x := left + j*cellW
			width := 6 * uc.size
			rows := underlineRows(img, x-2, int(top)-2, int(width)+6, int(uc.line)+10, float64(width)*0.85)
			want := int(math.Floor(float64(top)+0.5)) + uc.offset
			if len(rows) != 1 || rows[0] != want {
				t.Errorf("字号 %v 行高 %v 行盒顶 %v：下划线行 %v，期望只有第 %d 行", uc.size, uc.line, top, rows, want)
			}
		}
	}
}

// 没有 Underline（或 on 为假）时不画线：整行看不到「大半个文字宽度都是深色」的一行。
func TestTextWithoutUnderlineDrawsNoLine(t *testing.T) {
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	h := openPage(t, still(), 400, 120, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			ui.Column(c.Key("a")).Absolute().Left(10).Top(10).Children(func() {
				l.TextPx(c.Key("a-t"), underlineSample, 14, 20, WeightNormal, black)
			})
			ui.Column(c.Key("b")).Absolute().Left(10).Top(50).Children(func() {
				l.TextPx(c.Key("b-t"), underlineSample, 14, 20, WeightNormal, black, Underline(false))
			})
		})
	})
	h.frames(3)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	for _, top := range []int{10, 50} {
		if rows := underlineRows(img, 8, top-2, 6*14+6, 30, 6*14*0.85); len(rows) != 0 {
			t.Errorf("行盒顶 %d：没有下划线却画出了线 %v", top, rows)
		}
	}
}

// 下划线的横向范围：左缘是文字左缘，右缘是文字右缘（不超过可用宽度，放不下时取满）。
func TestTextUnderlineExtent(t *testing.T) {
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	const size, line = 14, 20
	cases := []struct {
		name  string
		avail float32 // 文字所在行盒的宽度
		opts  []TextOpt
		x0    float32 // 期望的线左缘
		x1    float32 // 期望的线右缘
	}{
		{"start", 300, nil, 10, 10 + 6*size},
		{"truncated-fills-width", 40, []TextOpt{Ellipsis()}, 10, 10 + 40},
		{"center", 300, []TextOpt{Centered()}, 10 + (300-6*size)/2, 10 + (300+6*size)/2},
		{"end", 300, []TextOpt{Right()}, 10 + 300 - 6*size, 10 + 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := openPage(t, still(), 400, 80, func(l *Look, c ui.C) {
				ui.Column(c).Fill().Background(white).Children(func() {
					ui.Column(c.Key("box")).Absolute().Left(10).Top(10).Width(tc.avail).Children(func() {
						opts := append([]TextOpt{Underline(true)}, tc.opts...)
						l.TextPx(c.Key("t"), underlineSample, size, line, WeightNormal, black, opts...).FillWidth()
					})
				})
			})
			h.frames(3)
			img, err := h.session.Image()
			if err != nil {
				t.Fatal(err)
			}
			row := 10 + 16 // 14px 字、行高 20：基线行 15，下划线在 +16
			first, last := -1, -1
			for x := 0; x < 400; x++ {
				if darkness(img, x, row) > 0.5 {
					if first < 0 {
						first = x
					}
					last = x
				}
			}
			// 半像素边缘：左缘取覆盖过半的第一列，右缘同理；允许差 1 列。
			if math.Abs(float64(first)-float64(tc.x0)) > 1 || math.Abs(float64(last+1)-float64(tc.x1)) > 1 {
				t.Errorf("线的横向范围 [%d, %d]，期望 [%.1f, %.1f]", first, last+1, tc.x0, tc.x1)
			}
		})
	}
}

// darkness 是像素 (x, y) 的暗度，0（白）到 1（黑）。
func darkness(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	return 1 - float64(r+g+b)/3/0xffff
}

// underlineRows 返回区域内「深色像素数不少于 minDark」的行号（绝对行号）。
func underlineRows(img image.Image, x, y, w, h int, minDark float64) []int {
	var rows []int
	for yy := y; yy < y+h; yy++ {
		dark := 0
		for xx := x; xx < x+w; xx++ {
			if darkness(img, xx, yy) > 0.5 {
				dark++
			}
		}
		if float64(dark) >= minDark {
			rows = append(rows, yy)
		}
	}
	return rows
}
