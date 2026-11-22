package kit

import (
	"math"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 等宽文字（font-mono）的基线对齐（台账 B-022）
//
// B-022 的「Viva 偏下 1 像素」有两个成因，都出在「按比例字体的度量去对齐等宽文字」上：
//
//  1. 渲染层一侧：font-mono 在中文 Windows 上解析成 Consolas，行盒的 ascent、descent 是 Consolas 的
//     （chromeMonoFace：A = round(0.92·s)、D = round(0.25·s)），不是 baseline.go 里 Microsoft YaHei UI 的
//     （A = round(1.0156·s)、D = round(0.2544·s)）。行盒顶到基线 = floor((L − (A+D))/2) + A，与比例字体是同一条规则，只是换了度量；
//     <input> 里的内部编辑器同样居中，基线 = round(框顶 + 框高/2 + (A − D)/2)。十六进制色值框（`InputPad`）用了 YaHei 的 A、D，
//     (A − D)/2 与 Consolas 的差零到一行，就是大字号档那 1 个像素；快捷键提示与提示详情原文则根本没有做基线平移。
//  2. Viva 一侧：Mono 标签在这一版 Viva（v0.6.4）里，墨迹基线与比例字体一样落在纹理第 floor(H/2 + 0.378·s) 行（H 是标签纹理高，
//     VivaBaselineRow），并不在 gfx.Canvas.MonoBaseline 给的「字体自己的基线」上：2026-11-22 用无头渲染 8–24px（步长 0.25）
//     共 65 个字号实测，量到的墨迹基线与 VivaBaselineRow(字格高, s) 在每个字号都相等，而与 MonoBaseline 在约 20 个字号上差 1 行
//     （TestVivaMonoInkBaseline 抽样钉住这一条）。所以 Viva 一侧的公式沿用比例字体的，只需把渲染层一侧换成 Consolas 的度量。
//
// 所以等宽文字的位移 = ChromeMonoBaselineRow − VivaBaselineRow（MonoBaselineShift），输入框的内边距换成 Consolas 的 A、D（InputPadMono）。
// Viva 一侧若以后改成真的站在 MonoBaseline 上，TestVivaMonoInkBaseline 会先红。

// chromeMonoAD 是渲染层 font-mono 的 ascent、descent 按字号（设备像素）取整后的行数。
func chromeMonoAD(sizeDev float64) (a, d float64) {
	return roundHalfUp(chromeMonoFace.ascent * sizeDev), roundHalfUp(chromeMonoFace.descent * sizeDev)
}

// ChromeMonoBaselineRow 是渲染层一行 font-mono 文字（只含 Consolas 有字形的字符）的基线到行盒顶的距离（设备像素，整数）。
func ChromeMonoBaselineRow(sizeDev, lineDev float64) int {
	a, d := chromeMonoAD(sizeDev)
	return int(math.Floor((lineDev-(a+d))/2) + a)
}

// ChromeMonoInputBaselineY 是渲染层 font-mono 的 <input> 里一行文字的基线落在哪一设备像素行（绝对坐标）；公式同 ChromeInputBaselineY，
// 只是 A、D 换成 Consolas 的。
func ChromeMonoInputBaselineY(sizeDev, boxTopDev, boxHeightDev float64) int {
	a, d := chromeMonoAD(sizeDev)
	return int(math.Floor(boxTopDev + boxHeightDev/2 + (a-d)/2 + 0.5))
}

// MonoBaselineShift 返回让 Viva 的一行文字（字号 drawSize、标签盒高 lineH）落在渲染层 font-mono（字号 size、行高 lineH）
// 基线位置所需的纵向平移（逻辑像素，取整到设备像素）；正值向下。
// drawSize 与 size 相同时是 Viva 的 Mono 文字；含汉字的行用界面字体画，字号是 monoWideAdvance(size)（见 MonoParagraph）。
// 用法同 BaselineShift：文字盒 PadTop(dy)、MarginBottom(−dy)（dy < 0 时反过来），版面高度不变。
func MonoBaselineShift(size, drawSize, lineH, scale float32) float32 {
	if scale <= 0 {
		scale = 1
	}
	sc := float64(scale)
	chrome := ChromeMonoBaselineRow(float64(size)*sc, float64(lineH)*sc)
	viva := VivaBaselineRow(VivaLabelRows(lineH, scale), float64(drawSize)*sc)
	return float32(chrome-viva) / scale
}

// shiftBaseline 把文字盒的基线平移 dy 逻辑像素（正值向下）而不改变它占的版面高度：上内边距加、下外边距减同一个量。
func shiftBaseline(box ui.Box, dy float32) {
	switch {
	case dy > 0:
		box.PadTop(dy).MarginBottom(-dy)
	case dy < 0:
		box.MarginTop(dy).PadBottom(-dy)
	}
}

// MonoText 画一行 font-mono 文字（字号 size，行盒高 lineH，只含 Consolas 有字形的字符）：用 Viva 的 Mono 字体，基线已对齐渲染层。
// 单行按「折行文字」的路径画（MaxLines(1)）：标签盒高就是 lineH，纹理顶落在行盒顶，没有「字格高居中进行盒」带来的小数顶边。
// 宽度不设，由调用方放进已按渲染层字距留好位置的容器里（kit.MonoAdvance）。
func (l *Look) MonoText(c ui.C, text string, size, lineH float32, color tokens.Color) ui.Box {
	box := ui.Text(c, text).Mono().MaxLines(1).FontSize(size).LineHeight(lineH).Color(l.Col(color))
	shiftBaseline(box, MonoBaselineShift(size, size, lineH, l.Scale))
	return box
}

// InputPadMono 是 InputPad 的等宽版：返回让 Viva 输入框里那一行 Mono 文字落在渲染层 font-mono <input> 文字位置所需的上、下内边距
// （逻辑像素）。Viva 一侧与比例字体相同（行盒高 size+6 的标签，基线行 VivaBaselineRow），渲染层一侧的 A、D 取 Consolas 的：
//
//	top = 框高/2 + (A − D)/2 − VivaBaselineRow
//
// 则 Viva 的基线 = round(框顶 + top) + VivaBaselineRow = round(框顶 + 框高/2 + (A − D)/2) = ChromeMonoInputBaselineY，
// 与框顶的小数部分无关（推导同 InputPad）。
func InputPadMono(size, height, scale float32) (top, bottom float32) {
	if scale <= 0 {
		scale = 1
	}
	sizeDev := float64(size) * float64(scale)
	a, d := chromeMonoAD(sizeDev)
	rv := VivaBaselineRow(VivaLabelRows(size+6, scale), sizeDev) // components.inputLineBox：fontSize+6
	return inputPadAt(a, d, rv, size, height, scale)
}

// inputPadAt 是 InputPad 与 InputPadMono 共用的算式：a、d 是渲染层字体的取整 ascent / descent（设备像素），rv 是 Viva 一侧的基线行。
func inputPadAt(a, d float64, rv int, size, height, scale float32) (top, bottom float32) {
	line := size + 6 // components.inputLineBox：fontSize+6
	topDev := float64(height)*float64(scale)/2 + (a-d)/2 - float64(rv)
	top = float32(topDev / float64(scale))
	return top, height - line - top
}
