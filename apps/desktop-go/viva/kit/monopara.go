package kit

import (
	"math"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// chromeMonoFace 是中文 Windows 下 font-mono 行盒的垂直度量（2026-11-21 在 React 参照里按 10.5/14、12/16、13.5/18 实测：
// 内容区高 13 / 14 / 15，基线 10 / 12 / 13；A=round(0.92·s)=10/11/12，D=round(0.25·s)=3/3/3）。
var chromeMonoFace = face{0.92, 0.25}

// monoBaselineShift 返回让 Viva 界面字体（字号 drawSize、行高 lineH）落在渲染层 font-mono（字号 size、行高 lineH）
// 基线位置所需的纵向平移（逻辑像素，取整到设备像素）。
func monoBaselineShift(size, drawSize, lineH, scale float32) float32 {
	if scale <= 0 {
		scale = 1
	}
	sc := float64(scale)
	a := roundHalfUp(chromeMonoFace.ascent * float64(size) * sc)
	d := roundHalfUp(chromeMonoFace.descent * float64(size) * sc)
	chrome := int(math.Floor((float64(lineH)*sc-(a+d))/2) + a)
	viva := VivaBaselineRow(VivaLabelRows(lineH, scale), float64(drawSize)*sc)
	return float32(chrome-viva) / scale
}

// MonoParagraph 画一段已经按渲染层规则折好的等宽原文（font-mono 的 pre，lines 是 WrapMono 的结果）：每行一个 Label，行高固定为 lineH，
// 空行只占行高。折行点由调用方在渲染层的度量下定好，这里保证 Viva 不会再折、也不会把行尾截掉。
//
// Viva 的 Mono 文字是字格模型：每个字符占整数个字格，汉字占两格（gfx.Canvas.MonoCellSize；台账 A-006）。
// 渲染层的 font-mono 在中文 Windows 上是 Consolas，汉字回退到新宋体（NSimSun）：前进量在多数档为 1 em，13.5px 下因新宋体位图吸附到 14px。
// 所以：
//   - 不含汉字（或全角符号）的行用 Mono 画，Label 宽度取 Viva 自己排出来的宽度（字格取整，可能比渲染层宽一点，不会被截）；
//   - 含汉字的整行用界面字体画，字号取 monoWideAdvance(size)（与新宋体汉字前进量一致），基线对齐到 font-mono 的行盒基线（台账 C-015）。
func (l *Look) MonoParagraph(c ui.C, lines []string, size, lineH float32, color tokens.Color) ui.Box {
	canvas := c.Canvas()
	drawSize := monoWideAdvance(size)
	dy := monoBaselineShift(size, drawSize, lineH, l.Scale)
	return ui.Column(c).Children(func() {
		for i, line := range lines {
			key := "line" + strconv.Itoa(i)
			switch {
			case line == "":
				ui.Column(c.Key(key)).Height(lineH).Shrink(0)
			case hasWideRune(line):
				w := canvas.TextWidthWeight(line, drawSize, WeightNormal)
				ui.Row(c.Key(key)).Height(lineH).Items(ui.Start).Shrink(0).Children(func() {
					box := ui.Text(c.Key("text"), line).
						FontSize(drawSize).
						LineHeight(lineH).
						Weight(WeightNormal).
						Color(l.Col(color)).
						Width(w).
						Shrink(0)
					switch {
					case dy > 0:
						box.PadTop(dy).MarginBottom(-dy)
					case dy < 0:
						box.MarginTop(dy).PadBottom(-dy)
					}
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
