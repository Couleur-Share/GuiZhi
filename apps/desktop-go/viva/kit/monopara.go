package kit

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// chromeMonoFace 是中文 Windows 下 font-mono 行盒的垂直度量（2026-11-21 在 React 参照里按 10.5/14、12/16、13.5/18 实测：
// 内容区高 13 / 14 / 15，基线 10 / 12 / 13；A=round(0.92·s)=10/11/12，D=round(0.25·s)=3/3/3）。
var chromeMonoFace = face{0.92, 0.25}

// MonoParagraph 画一段已经按渲染层规则折好的等宽原文（font-mono 的 pre，lines 是 WrapMono 的结果）：每行一个 Label，行高固定为 lineH，
// 空行只占行高。折行点由调用方在渲染层的度量下定好，这里保证 Viva 不会再折、也不会把行尾截掉。
//
// Viva 的 Mono 文字是字格模型：每个字符占整数个字格，汉字占两格（gfx.Canvas.MonoCellSize；台账 A-006）。
// 渲染层的 font-mono 在中文 Windows 上是 Consolas，汉字回退到新宋体（NSimSun）：前进量在多数档为 1 em，13.5px 下因新宋体位图吸附到 14px。
// 所以：
//   - 不含汉字（或全角符号）的行用 Mono 画，Label 宽度取 Viva 自己排出来的宽度（字格取整，可能比渲染层宽一点，不会被截）；
//   - 含汉字的整行用界面字体画，字号取 monoWideAdvance(size)（与新宋体汉字前进量一致），基线对齐到 font-mono 的行盒基线（台账 C-015）。
func (l *Look) MonoParagraph(c ui.C, lines []string, size, lineH float32, color tokens.Color) ui.Box {
	st := l.monoStyleOf(size, lineH, color)
	return ui.Column(c).Children(func() {
		for i, line := range lines {
			key := "line" + strconv.Itoa(i)
			if line == "" {
				ui.Column(c.Key(key)).Height(lineH).Shrink(0)
				continue
			}
			l.monoFrag(c, key, line, st, 0)
		}
	})
}

// MonoParagraphRuns 画几段相邻文字拼成的等宽原文（台账 B-063）：渲染层 `<pre>{label}{value}</pre>` 在 DOM 里是两个相邻的文本节点，
// 折行时第二行回到 pre 的左缘。lines 是 WrapMonoRuns 的结果：每一行里每一段的那一截各画成一个 Label（对照的文字序列按文本节点逐个比，
// 不能把几段拼成一个 Label），同一行的几截首尾相接。一行里只有一截时与 MonoParagraph 的画法逐像素相同。
//
// 行内不是最后一截的 Mono 文字（不含汉字的一截）放进渲染层字距宽度（MonoWidth）的槽里，下一截从槽的右缘起：
// Viva 的字格取整之后比渲染层宽一点（A-006），不留槽的话后面一截会整体右移。
func (l *Look) MonoParagraphRuns(c ui.C, lines [][]MonoFrag, size, lineH float32, color tokens.Color) ui.Box {
	st := l.monoStyleOf(size, lineH, color)
	return ui.Column(c).Children(func() {
		for i, line := range lines {
			key := "line" + strconv.Itoa(i)
			switch len(line) {
			case 0:
				ui.Column(c.Key(key)).Height(lineH).Shrink(0)
			case 1:
				l.monoFrag(c, key, line[0].Text, st, 0)
			default:
				ui.Row(c.Key(key)).Height(lineH).Items(ui.Start).Shrink(0).Children(func() {
					for k, frag := range line {
						slot := float32(0)
						if k < len(line)-1 {
							slot = MonoWidth(frag.Text, size)
						}
						l.monoFrag(c, "run"+strconv.Itoa(frag.Run), frag.Text, st, slot)
					}
				})
			}
		}
	})
}

// monoStyle 是一段等宽文字的绘制参数：渲染层字号 size，含汉字的行用界面字体画时的字号 drawSize 与基线平移 dy。
type monoStyle struct {
	size, drawSize, lineH, dy float32
	color                     tokens.Color
}

func (l *Look) monoStyleOf(size, lineH float32, color tokens.Color) monoStyle {
	drawSize := monoWideAdvance(size)
	return monoStyle{size: size, drawSize: drawSize, lineH: lineH, dy: MonoBaselineShift(size, drawSize, lineH, l.Scale), color: color}
}

// monoFrag 画一行（或一行里的一截）非空的等宽文字：含汉字的用界面字体画并对齐 font-mono 的基线，其余用 Mono 画。
// slot 大于 0 时（行内后面还有别的一截）Mono 文字放进这么宽的槽里，见 MonoParagraphRuns。
func (l *Look) monoFrag(c ui.C, key, text string, st monoStyle, slot float32) {
	canvas := c.Canvas()
	if hasWideRune(text) {
		w := canvas.TextWidthWeight(text, st.drawSize, WeightNormal)
		ui.Row(c.Key(key)).Height(st.lineH).Items(ui.Start).Shrink(0).Children(func() {
			box := ui.Text(c.Key("text"), text).
				FontSize(st.drawSize).
				LineHeight(st.lineH).
				Weight(WeightNormal).
				Color(l.Col(st.color)).
				Width(w).
				Shrink(0)
			shiftBaseline(box, st.dy)
		})
		return
	}
	w := canvas.TextWidthStyle(text, gfx.TextStyle{Size: st.size, Mono: true})
	// 基线对齐渲染层 font-mono 的行盒（baseline_mono.go）：行高带小数（leading-relaxed 的 17.875 等）时，Viva 的居中规则与渲染层的取整会差一行。
	dy := MonoBaselineShift(st.size, st.size, st.lineH, l.Scale)
	mono := func(k ui.C) ui.Box {
		box := ui.Text(k, text).Mono().MaxLines(1).FontSize(st.size).LineHeight(st.lineH).Color(l.Col(st.color)).Width(w).Shrink(0)
		shiftBaseline(box, dy)
		return box
	}
	if slot > 0 {
		ui.Row(c.Key(key)).Width(slot).Height(st.lineH).Items(ui.Start).Shrink(0).Children(func() { mono(c.Key("text")) })
		return
	}
	mono(c.Key(key))
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
