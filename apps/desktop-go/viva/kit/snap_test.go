package kit

import (
	"image"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
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

// renderInkOrigin 把黑色 "H" 画在带小数内边距的位置，返回字形墨迹的第一列与第一行。
func renderInkOrigin(t *testing.T, left, top float32) (int, int) {
	t.Helper()
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		ui.Column(c).PadLeft(left).PadTop(top).Background(gfx.RGBA(255, 255, 255, 1)).Children(func() {
			ui.Text(c, "H").FontSize(14).LineHeight(20).Color(gfx.RGBA(0, 0, 0, 1))
		})
	}})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: "snap", Width: 40, Height: 40}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for i := 0; i < 3; i++ {
		if err := session.Frame(); err != nil {
			t.Fatal(err)
		}
	}
	img, err := session.Image()
	if err != nil {
		t.Fatal(err)
	}
	return firstInk(img)
}

// TestVivaLabelRoundsHalfUp 守住 baseline.go 与 text.go 依赖的 Viva 行为：文字盒的左边与顶边带小数时，
// 字形按「四舍五入（半个像素向上）」落到整设备像素，不做子像素定位。
// 渲染层（Chromium + DirectWrite）水平方向是子像素定位，所以左边带 0.5 的文字最多差半个像素，这是 Viva 的限制
// （记在 docs/agents/viva-parity-ledger.md，不是 kit 能补偿的）。
// 依据：2026-11-16 在 0 / 0.25 / 0.5 / 0.75 / 1.0 的小数偏移上实测。
func TestVivaLabelRoundsHalfUp(t *testing.T) {
	baseCol, baseRow := renderInkOrigin(t, 10, 10)
	if baseCol < 0 || baseRow < 0 {
		t.Fatal("基准位置没有画出字形")
	}
	cases := []struct {
		frac  float32
		shift int // 相对整数偏移的整像素位移
	}{
		{0, 0}, {0.25, 0}, {0.49, 0}, {0.5, 1}, {0.75, 1}, {1, 1},
	}
	for _, c := range cases {
		col, row := renderInkOrigin(t, 10+c.frac, 10+c.frac)
		if col-baseCol != c.shift || row-baseRow != c.shift {
			t.Errorf("偏移 +%v：字形起点位移 (%d, %d)，期望 (%d, %d)", c.frac, col-baseCol, row-baseRow, c.shift, c.shift)
		}
	}
}
