package kit

import (
	"image"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// TestChromeMonoBaselineRowKnown 钉住渲染层 font-mono（Consolas：A = round(0.92·s)、D = round(0.25·s)）行盒基线的代表值，
// 手算：基线行 = floor((行高 − (A+D))/2) + A。
func TestChromeMonoBaselineRowKnown(t *testing.T) {
	cases := []struct {
		size, line float64
		want       int
	}{
		{10, 15, 10},   // A=9 D=3 → floor(3/2)+9
		{10.5, 14, 10}, // A=10 D=3 → floor(1/2)+10
		{12, 16, 12},   // A=11 D=3 → floor(2/2)+11
		{13.5, 18, 13}, // A=12 D=3 → floor(3/2)+12 = 13
		{12, 32, 20},   // 设备像素 12 / 32（缩放 2 的 6px 字）：A=11 D=3 → floor(18/2)+11
	}
	for _, c := range cases {
		if got := ChromeMonoBaselineRow(c.size, c.line); got != c.want {
			t.Errorf("字号 %v 行高 %v：基线行 %d，期望 %d", c.size, c.line, got, c.want)
		}
	}
}

// TestChromeMonoInputBaselineYKnown 钉住 <input> 基线公式 floor(框顶 + 框高/2 + (A−D)/2 + 0.5) 的两个手算值。
func TestChromeMonoInputBaselineYKnown(t *testing.T) {
	// 12px：A=11 D=3 → (A−D)/2 = 4；框顶 20、框高 32 → floor(20+16+4+0.5) = 40。
	if got := ChromeMonoInputBaselineY(12, 20, 32); got != 40 {
		t.Errorf("12px 框高 32：基线 %d，期望 40", got)
	}
	// 13.5px：A=12 D=3 → 4.5；框顶 20.25、框高 36 → floor(20.25+18+4.5+0.5) = 43。
	if got := ChromeMonoInputBaselineY(13.5, 20.25, 36); got != 43 {
		t.Errorf("13.5px 框高 36：基线 %d，期望 43", got)
	}
}

// TestMonoBaselineShiftKnown：位移 = 渲染层基线行 − Viva 基线行（比例规则 floor(H/2 + 0.378·s)），缩放 1 与 2 都要对。
func TestMonoBaselineShiftKnown(t *testing.T) {
	for _, c := range []struct {
		size, line, scale float32
	}{{10, 15, 1}, {10.5, 14, 1}, {12, 16, 1}, {13.5, 18, 1}, {12, 16, 2}} {
		sc := float64(c.scale)
		want := float32(ChromeMonoBaselineRow(float64(c.size)*sc, float64(c.line)*sc)-
			VivaBaselineRow(VivaLabelRows(c.line, c.scale), float64(c.size)*sc)) / c.scale
		if got := MonoBaselineShift(c.size, c.size, c.line, c.scale); got != want {
			t.Errorf("字号 %v 行高 %v 缩放 %v：位移 %v，期望 %v", c.size, c.line, c.scale, got, want)
		}
	}
	if got := MonoBaselineShift(12, 12, 16, 0); got != MonoBaselineShift(12, 12, 16, 1) {
		t.Errorf("缩放 ≤ 0 应按 1 处理：%v", got)
	}
}

// TestVivaMonoInkBaseline 守住 B-022 的 Viva 一侧前提：一行 MaxLines(1) 的 Mono 文字，墨迹基线落在 VivaBaselineRow(标签行数, 字号) 上
// （与比例字体同一条规则，而不是字体自己的 MonoBaseline）。Viva 升级后这条规则变了，这里会先红。
func TestVivaMonoInkBaseline(t *testing.T) {
	sizes := []float32{8, 9.5, 10, 10.5, 11.25, 12, 12.25, 13.5, 14, 15.75, 16, 18, 20.5, 24}
	const top, width = 10, 120
	lineOf := func(s float32) float32 { return s + 6 }
	img := renderCells(t, len(sizes), width, func(c ui.C, i int) {
		ui.Column(c).Width(width).Pad(top).Children(func() {
			ui.Text(c, "HHHH").Mono().MaxLines(1).FontSize(sizes[i]).LineHeight(lineOf(sizes[i])).Color(gfx.RGBA(0, 0, 0, 1))
		})
	})
	for i, s := range sizes {
		got := lastFaintInkRow(img, i*width, top, width, int(lineOf(s))+8) + 1 - top
		want := VivaBaselineRow(VivaLabelRows(lineOf(s), 1), float64(s))
		if got != want {
			t.Errorf("Mono 字号 %v 行高 %v：墨迹基线行 %d，VivaBaselineRow %d", s, lineOf(s), got, want)
		}
	}
}

// TestMonoTextLandsOnChromeBaseline：Look.MonoText 画出的一行，墨迹基线 = 文字盒顶 + ChromeMonoBaselineRow（盒顶取整数，缩放 1）。
func TestMonoTextLandsOnChromeBaseline(t *testing.T) {
	look := NewLook(DefaultSpec())
	black := tokens.RGBA(0, 0, 0, 1)
	type spec struct{ size, line float32 }
	specs := []spec{{10, 15}, {10.5, 14}, {12, 16}, {13.5, 18}, {12.25, 17.5}, {15.75, 22.5}}
	const top, width = 10, 120
	img := renderCells(t, len(specs), width, func(c ui.C, i int) {
		ui.Column(c).Width(width).Pad(top).Children(func() {
			look.MonoText(c, "HHHH", specs[i].size, specs[i].line, black)
		})
	})
	for i, s := range specs {
		got := lastFaintInkRow(img, i*width, 0, width, cellH) + 1 - top
		want := ChromeMonoBaselineRow(float64(s.size), float64(s.line))
		if got != want {
			t.Errorf("MonoText 字号 %v 行高 %v：基线行 %d，Chromium 模型 %d", s.size, s.line, got, want)
		}
	}
}

// TestInputPadMonoLandsOnChromeBaseline：等宽输入框（InputSpec.Mono）里的内容，基线落在 ChromeMonoInputBaselineY，与框顶小数无关。
func TestInputPadMonoLandsOnChromeBaseline(t *testing.T) {
	look := NewLook(DefaultSpec())
	black := tokens.RGBA(0, 0, 0, 1)
	type box struct{ size, height float32 }
	boxes := []box{{12, 32}, {13.5, 36}, {10.5, 28}, {12.25, 31.5}, {15.75, 40.5}}
	fracs := []float32{0, 0.25, 0.5, 0.75}
	const cellInputW = 140
	fields := make([]ui.Field, len(boxes)*len(fracs))
	for i := range fields {
		fields[i].SetText("HHHH")
	}
	topOf := func(i int) float32 { return 20 + fracs[i%len(fracs)] }
	img := renderCells(t, len(fields), cellInputW, func(c ui.C, i int) {
		b := boxes[i/len(fracs)]
		ui.Column(c).Width(cellInputW).PadTop(topOf(i)).PadLeft(10).Children(func() {
			look.Input(c, &fields[i], "测试", "", InputSpec{Size: b.size, Weight: WeightNormal, Color: black, Height: b.height, Mono: true}).Width(100)
		})
	})
	for i := range fields {
		b, top := boxes[i/len(fracs)], topOf(i)
		got := lastFaintInkRow(img, i*cellInputW, 0, cellInputW, cellH) + 1
		want := ChromeMonoInputBaselineY(float64(b.size), float64(top), float64(b.height))
		if got != want {
			t.Errorf("Mono 输入框 字号 %v 框高 %v 框顶 %v：Viva 基线行 %d，Chromium 模型 %d", b.size, b.height, top, got, want)
		}
	}
}

// lastFaintInkRow 同 lastInkRow，但把“稍浅的灰”也算墨迹：Consolas 在小字号、小数像素基线上，H 的竖笔最底一行覆盖不足一半，
// 灰度高于 lastInkRow 的阈值，会被当成没有墨迹而量到横笔上（偏高约 3 行）。
func lastFaintInkRow(img image.Image, x, y, w, h int) int {
	last := -1
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			r, g, b, _ := img.At(xx, yy).RGBA()
			if (r+g+b)/3 < 0xE000 {
				last = yy
				break
			}
		}
	}
	return last
}