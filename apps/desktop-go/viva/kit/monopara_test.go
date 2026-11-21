package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 一行一个 Label：空行只占行高、不画；含汉字的行用界面字体（不是 Mono）并整行下移 drop，不含汉字的行用 Mono 画，
// 宽度取 Viva 自己排出的宽度（不会被截）；各行的间距都是行高。
func TestMonoParagraphDrawsOneLabelPerLine(t *testing.T) {
	lines := []string{"本地优先的软件", "plain ascii text", "", "混排 mixed 行"}
	const size, lineH = 12, 16
	var canvas *gfx.Canvas
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		canvas = c.Canvas()
		ui.Column(c).Pad(10).Children(func() {
			l.MonoParagraph(c.Key("pre"), lines, size, lineH, l.T.Foreground)
		})
	})
	texts := h.session.Snapshot().Texts
	if len(texts) != 3 {
		t.Fatalf("应有三个 Label（空行不画），实际 %d 个：%+v", len(texts), texts)
	}
	wide0, ascii, wide3 := texts[0], texts[1], texts[2]
	if wide0.Value != lines[0] || ascii.Value != lines[1] || wide3.Value != lines[3] {
		t.Fatalf("Label 的文字不对：%q %q %q", wide0.Value, ascii.Value, wide3.Value)
	}
	if wide0.Style.Mono || wide3.Style.Mono {
		t.Error("含汉字的行应该用界面字体，不是 Mono（Mono 把汉字排成两个字格，与渲染层的 1 em 不同）")
	}
	if !ascii.Style.Mono {
		t.Error("不含汉字的行应该用 Mono")
	}
	// 第 i 行的行盒顶是 top + i×lineH；size 12 / lineH 16 时 Consolas 基线与 Viva 基线同为 12，dy = 0。
	top := ascii.Bounds.Y - lineH
	if got := wide0.Bounds.Y - top; math.Abs(float64(got)) > 0.01 {
		t.Errorf("第一行（含汉字）相对行盒顶位移 %v，期望 0", got)
	}
	if got := wide3.Bounds.Y - top; math.Abs(float64(got-3*lineH)) > 0.01 {
		t.Errorf("第四行（含汉字，前面隔一个空行）在行盒顶之下 %v，期望 %v", got, 3*lineH)
	}
	// 汉字行的宽度是界面字体的自然宽度（按 monoWideAdvance(size)）；Mono 行的宽度不小于 Viva 自己排出来的宽度（否则行尾会被截）。
	if got, want := wide0.Bounds.Width, canvas.TextWidthWeight(lines[0], size, WeightNormal); math.Abs(float64(got-want)) > 0.01 {
		t.Errorf("汉字行宽 %v，期望界面字体的自然宽度 %v", got, want)
	}
	if got, min := ascii.Bounds.Width, canvas.TextWidthStyle(lines[1], gfx.TextStyle{Size: size, Mono: true}); got+0.01 < min {
		t.Errorf("Mono 行宽 %v，小于 Viva 排出来的宽度 %v：行尾会被截", got, min)
	}
}

// 三档字号下（10.5/14、12/16、13.5/18）的基线平移与大档汉字字号吸附：
//   - 小档 10.5/14：font-mono 基线 10、Viva 基线 10，dy = 0，字号 10.5；
//   - 中档 12/16：font-mono 基线 12、Viva 基线 12，dy = 0，字号 12；
//   - 大档 13.5/18：新宋体位图吸附到 14px，font-mono 基线 13、Viva 基线 14，dy = -1，字号 14。
func TestMonoParagraphAcrossFontTiers(t *testing.T) {
	cases := []struct {
		name      string
		size, lh  float32
		wantSize  float32
		wantShift float32
	}{
		{"small", 10.5, 14, 10.5, 0},
		{"medium", 12, 16, 12, 0},
		{"large", 13.5, 18, 14, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{"本地优先的软件", "ascii"}
			h := openPage(t, still(), 400, 120, func(l *Look, c ui.C) {
				ui.Column(c).Pad(10).Children(func() {
					l.MonoParagraph(c.Key("pre"), lines, tc.size, tc.lh, l.T.Foreground)
				})
			})
			texts := h.session.Snapshot().Texts
			if len(texts) != 2 {
				t.Fatalf("应有两个 Label，实际 %d", len(texts))
			}
			wide, ascii := texts[0], texts[1]
			if math.Abs(float64(wide.Style.Size-tc.wantSize)) > 0.01 {
				t.Errorf("汉字行字号 %v，期望 %v", wide.Style.Size, tc.wantSize)
			}
			top := ascii.Bounds.Y - tc.lh
			if got := wide.Bounds.Y - top; math.Abs(float64(got-tc.wantShift)) > 0.01 {
				t.Errorf("汉字行相对行盒顶位移 %v，期望 %v", got, tc.wantShift)
			}
		})
	}
}

// 汉字与全角符号算「宽字符」：只有拉丁字符、数字与半角标点的行才走 Mono。
func TestHasWideRune(t *testing.T) {
	for s, want := range map[string]bool{
		"plain ascii 123": false, "": false, "a，b": true, "汉": true, "かな": true, "한글": true, "全角Ａ": true, "—": false, "·": false,
	} {
		if got := hasWideRune(s); got != want {
			t.Errorf("hasWideRune(%q) = %v，期望 %v", s, got, want)
		}
	}
}
