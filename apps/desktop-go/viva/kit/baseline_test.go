package kit

import (
	"image"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// TestBaselineShiftKnownCases 钉住三处在渲染层实测过的案例（Microsoft YaHei UI，设备像素 1:1）：
// 模型预测与 2026-11-16 在 settings.general 上量到的 Viva 与 React 墨迹差一致。
func TestBaselineShiftKnownCases(t *testing.T) {
	cases := []struct {
		name         string
		weight       float32
		size, line   float32
		wantShiftDev float32
	}{
		{"h1 18px/600/行高 28", WeightSemibold, 18, 28, 1},
		{"label 14px/500/行高 20", WeightMedium, 14, 20, 1},
		{"描述 12px/400/行高 16", WeightNormal, 12, 16, 1},
		{"select 值 14px/400/行高 20", WeightNormal, 14, 20, 0},
	}
	for _, c := range cases {
		if got := BaselineShift(c.weight, c.size, c.line, 1); got != c.wantShiftDev {
			t.Errorf("%s：平移 %v，期望 %v", c.name, got, c.wantShiftDev)
		}
	}
}

// TestVivaBaselineConstant 守住抄来的 Viva 常数：用无头渲染一行 "HHHH"，量出基线（H 竖笔的底边），
// 与 VivaBaselineRow 的公式比较。Viva 升级后这个常数变了，这里会先红。
func TestVivaBaselineConstant(t *testing.T) {
	type sample struct {
		size, height float32
	}
	for _, s := range []sample{{12, 16}, {14, 20}, {18, 28}, {16, 24}, {20, 28}, {24, 32}} {
		got := renderedBaselineRow(t, s.size, s.height)
		want := VivaBaselineRow(int(s.height), float64(s.size))
		if got != want {
			t.Errorf("字号 %v 行高 %v：渲染出的基线行 %d，公式 %d", s.size, s.height, got, want)
		}
	}
}

// renderedBaselineRow 画一行黑色 "HHHH"（顶边对齐到 Y=10），返回基线相对文字盒顶的行数。
func renderedBaselineRow(t *testing.T, size, height float32) int {
	t.Helper()
	const top = 10
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		ui.Column(c).Pad(top).Background(gfx.RGBA(255, 255, 255, 1)).Children(func() {
			ui.Text(c, "HHHH").FontSize(size).LineHeight(height).Color(gfx.RGBA(0, 0, 0, 1))
		})
	}})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: "baseline", Width: 120, Height: 80}), &canvas, headless.Options{Scale: 1})
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
	return lastInkRow(img, 0, top, 120, int(height)+8) + 1 - top
}

// lastInkRow 返回区域内最后一行含“明显深色”像素的行号（绝对行号）。
func lastInkRow(img image.Image, x, y, w, h int) int {
	last := -1
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			r, g, b, _ := img.At(xx, yy).RGBA()
			if (r+g+b)/3 < 0x8000 {
				last = yy
				break
			}
		}
	}
	return last
}
