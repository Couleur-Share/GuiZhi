package kit

import (
	"image"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// firstInk 返回区域内第一处“明显深色”像素所在的列与行（找不到返回 -1）。
func firstInk(img image.Image) (col, row int) {
	col, row = -1, -1
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if (r+g+bl)/3 < 0x8000 {
				if col < 0 || x < col {
					col = x
				}
				if row < 0 {
					row = y
				}
			}
		}
	}
	return col, row
}

// renderInkOrigins 把黑色 "H" 并排画在同一页的若干格里：第 i 格的左内边距与顶内边距都是 offs[i]（可带小数）。
// 返回每格里字形墨迹的第一列与第一行（相对该格左上角，没画出字形时为 -1）。
// 一页多格而不是每个偏移各开一个无头会话：每个会话都要新建一次 D3D 设备。格宽取整数，不改变各格的小数偏移。
func renderInkOrigins(t *testing.T, offs []float32) [][2]int {
	t.Helper()
	const width = 40
	img := renderCells(t, len(offs), width, func(c ui.C, i int) {
		ui.Column(c).Width(width).PadLeft(offs[i]).PadTop(offs[i]).Children(func() {
			ui.Text(c, "H").FontSize(14).LineHeight(20).Color(gfx.RGBA(0, 0, 0, 1))
		})
	})
	out := make([][2]int, len(offs))
	for i := range offs {
		cell := img.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(i*width, 0, (i+1)*width, cellH))
		col, row := firstInk(cell)
		if col >= 0 {
			col -= i * width
		}
		out[i] = [2]int{col, row}
	}
	return out
}

// TestVivaLabelRoundsHalfUp 守住 baseline.go 与 text.go 依赖的 Viva 行为：文字盒的左边与顶边带小数时，
// 字形按「四舍五入（半个像素向上）」落到整设备像素，不做子像素定位。
// 渲染层（Chromium + DirectWrite）水平方向是子像素定位，所以左边带 0.5 的文字最多差半个像素，这是 Viva 的限制
// （记在 docs/agents/viva-parity-ledger.md，不是 kit 能补偿的）。
// 依据：2026-11-16 在 0 / 0.25 / 0.5 / 0.75 / 1.0 的小数偏移上实测。
func TestVivaLabelRoundsHalfUp(t *testing.T) {
	cases := []struct {
		frac  float32
		shift int // 相对整数偏移的整像素位移
	}{
		{0, 0}, {0.25, 0}, {0.49, 0}, {0.5, 1}, {0.75, 1}, {1, 1},
	}
	offs := []float32{10} // 第 0 格是整数偏移的基准
	for _, c := range cases {
		offs = append(offs, 10+c.frac)
	}
	origins := renderInkOrigins(t, offs)
	base := origins[0]
	if base[0] < 0 || base[1] < 0 {
		t.Fatal("基准位置没有画出字形")
	}
	for i, c := range cases {
		got := origins[i+1]
		if got[0]-base[0] != c.shift || got[1]-base[1] != c.shift {
			t.Errorf("偏移 +%v：字形起点位移 (%d, %d)，期望 (%d, %d)", c.frac, got[0]-base[0], got[1]-base[1], c.shift, c.shift)
		}
	}
}
