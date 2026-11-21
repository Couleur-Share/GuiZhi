package kit

import (
	"math"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// monoRefPx 是量到「含汉字的行比界面字体居中的基线低 1px」时的字号（text-xs，中档字号档的 12px）。
const monoRefPx = 12

// MonoParagraph 画一段已经按渲染层规则折好的等宽原文（font-mono 的 pre，lines 是 WrapMono 的结果）：每行一个 Label，行高固定为 lineH，
// 空行只占行高。折行点由调用方在渲染层的度量下定好，这里保证 Viva 不会再折、也不会把行尾截掉。
//
// Viva 的 Mono 文字是字格模型：每个字符占整数个字格，汉字占两格（gfx.Canvas.MonoCellSize；台账 A-006）。
// 渲染层的 font-mono 在中文 Windows 上是 Consolas，汉字回退到界面字体、前进量是 1 em，不是两个字格——同样的汉字行，
// Viva 的 Mono 比渲染层宽约六分之一，会多折行。所以：
//   - 不含汉字（或全角符号）的行用 Mono 画，Label 宽度取 Viva 自己排出来的宽度（字格取整，可能比渲染层宽一点，不会被截）；
//   - 含汉字的整行用界面字体画：汉字的宽度与位置和渲染层一致；行里的拉丁字符因此是界面字体的字形，不是 Consolas（台账 C-015）。
func (l *Look) MonoParagraph(c ui.C, lines []string, size, lineH float32, color tokens.Color) ui.Box {
	canvas := c.Canvas()
	// 行盒的基线由首选字体（Consolas）定：Chromium 里汉字落在 Consolas 的基线上，比界面字体自己居中的基线低约 1px（12px 字号实测），
	// 所以含汉字的行整行下移 drop（按字号换算，取整）。
	drop := float32(math.Round(float64(size / monoRefPx)))
	return ui.Column(c).Children(func() {
		for i, line := range lines {
			key := "line" + strconv.Itoa(i)
			switch {
			case line == "":
				ui.Column(c.Key(key)).Height(lineH).Shrink(0)
			case hasWideRune(line):
				w := canvas.TextWidthWeight(line, size, WeightNormal)
				ui.Row(c.Key(key)).Height(lineH).Items(ui.Start).Shrink(0).Children(func() {
					l.TextPx(c.Key("text"), line, size, lineH, WeightNormal, color).Width(w).Shrink(0).MarginTop(drop)
				})
			default:
				w := canvas.TextWidthStyle(line, gfx.TextStyle{Size: size, Mono: true})
				ui.Text(c.Key(key), line).Mono().MaxLines(1).FontSize(size).LineHeight(lineH).Color(l.Col(color)).Width(w).Shrink(0)
			}
		}
	})
}

// hasWideRune 报告 s 里有没有汉字、假名、谚文或全角符号（isWideRune）。
func hasWideRune(s string) bool {
	for _, r := range s {
		if isWideRune(r) {
			return true
		}
	}
	return false
}
