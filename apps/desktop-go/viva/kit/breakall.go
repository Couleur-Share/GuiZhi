package kit

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 逐字折行（Tailwind 的 break-all，CSS word-break: break-all）
//
// Viva 的折行（gfx.WrapLines）按 UAX #14 的断点折：一个超过整行的「词」（网址、路径）优先在最后一个 / \ - _ . & ? = 之后断开，
// 实在没有才在字素之间断。渲染层的 break-all 把字母与数字都当作可断的字，行满处在最后放得下的位置断，不挑分隔符：
// 「…/answer/98765432」换行后剩一个「1」，而不是整段数字折到下一行。两边折行点不同，同样宽的容器里行数与行尾都会差一截，
// 下面所有元素的位置跟着错，所以 break-all 的文字在这里自己折行（2026-11-17 在任务详情弹窗的来源字段里量到）。
//
// 复刻的规则（Chromium 的 break-all，把字母、数字当表意字处理，其余字符照 UAX #14）：
//   - 行满处在最后放得下的字素边界断开（贪心），行尾的空格悬挂：占位但不参与「放不放得下」，换行后行首的空格去掉；
//   - 断点不能落在「不得起行」的字符之前（关闭类标点、, . : ; ! ? ) ] } % 与 / \ -），也不能落在「不得落尾」的字符之后（开启类括号）；
//   - 最后放得下的位置不允许断时往前退到允许的断点；退到头也没有就在最后放得下的位置硬断；
//   - 硬换行（\n）保留。

const (
	// breakAllNoBefore 是 break-all 里不得起行的字符：monowrap.go 的行首禁用集，再加 UAX #14 里「不得在其前断开」的 / \ -
	// （SY、PR、HY 类；字母与数字在 break-all 里当表意字，它们之前才可断）。
	breakAllNoBefore = noLineStart + "/\\-"
	// breakAllNoAfter 是不得落尾的字符：开启类括号与引号（monowrap.go 的行尾禁用集）。
	breakAllNoAfter = noLineEnd
)

// WrapBreakAll 把 text 在 width 宽的盒子里按 break-all 折成若干行；measure 量一段文字（不含换行）排出来的宽度。
// 返回的每一行不含换行符；空文字返回一行空串。width 不大于 0 时只按硬换行切分。
func WrapBreakAll(text string, width float32, measure func(string) float32) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out []string
	for _, hard := range strings.Split(text, "\n") {
		out = appendBreakAll(out, hard, width, measure)
	}
	return out
}

// appendBreakAll 折一个硬行，把折出的行接到 dst 后面。
func appendBreakAll(dst []string, line string, width float32, measure func(string) float32) []string {
	if width <= 0 {
		return append(dst, line)
	}
	for {
		if measure(strings.TrimRight(line, " ")) <= width+1e-3 {
			return append(dst, line)
		}
		cut := breakAllCut(line, width, measure)
		dst = append(dst, strings.TrimRight(line[:cut], " "))
		if line = strings.TrimLeft(line[cut:], " "); line == "" {
			return dst
		}
	}
}

// breakAllCut 是 line（整行放不下 width）折行的位置：字节偏移，大于 0 且小于 len(line)。
func breakAllCut(line string, width float32, measure func(string) float32) int {
	// 最后放得下的字素边界：行首至它的文字（去掉行尾空格）不宽于 width；宽度随文字单调增长，按边界二分。
	lo, hi := 0, len(line) // line[:lo] 放得下（空串放得下），line[:hi] 放不下
	for hi-lo > 1 {
		mid := gfx.GraphemeStart(line, (lo+hi)/2)
		if mid <= lo {
			if mid = nextBoundary(line, lo); mid >= hi {
				break
			}
		}
		if measure(strings.TrimRight(line[:mid], " ")) <= width+1e-3 {
			lo = mid
		} else {
			hi = mid
		}
	}
	fit := lo
	if fit == 0 {
		return nextBoundary(line, 0) // 一个字符也放不下：它独占一行
	}
	// 退到允许断开的边界。
	for cut := fit; cut > 0; cut = prevBoundary(line, cut) {
		if breakAllAllowed(line, cut) {
			return cut
		}
	}
	return fit
}

// breakAllAllowed 报告能否在 line 的字节偏移 cut（0 < cut < len(line)）处断开。
func breakAllAllowed(line string, cut int) bool {
	prev, _ := utf8.DecodeLastRuneInString(line[:cut])
	next, _ := utf8.DecodeRuneInString(line[cut:])
	if next == ' ' {
		return true // 空格悬挂在行尾，不会起行
	}
	return !strings.ContainsRune(breakAllNoBefore, next) && !strings.ContainsRune(breakAllNoAfter, prev)
}

// nextBoundary 是 i 之后的第一个字素边界（i 不在末尾）。
func nextBoundary(s string, i int) int {
	for j := i + 1; j < len(s); j++ {
		if gfx.GraphemeStart(s, j) == j {
			return j
		}
	}
	return len(s)
}

// prevBoundary 是 i 之前的最后一个字素边界（i 大于 0）。
func prevBoundary(s string, i int) int {
	return gfx.GraphemeStart(s, i-1)
}

// breakAllLayout 排 break-all 的文字：宽度由外层给，高度随折出的行数增长。折出的行存在这里，画的时候取用。
// Viva 的自定义版面每帧先量高度、再画；两处用同一宽度时只折一次。
type breakAllLayout struct {
	canvas *gfx.Canvas
	text   string
	size   float32
	weight float32
	line   float32

	lines  []string
	width  float32
	filled bool
}

// linesAt 返回 text 在 width 宽的盒子里折出的行；同一宽度再问时直接用上一次的结果。
func (b *breakAllLayout) linesAt(width float32) []string {
	if !b.filled || b.width != width {
		measure := func(s string) float32 { return b.canvas.TextWidthWeight(s, b.size, b.weight) }
		b.lines, b.width, b.filled = WrapBreakAll(b.text, width, measure), width, true
	}
	return b.lines
}

func (b *breakAllLayout) MeasureWidth(*ui.LayoutContext) float32 { return 0 } // 宽度由外层给（FillWidth）
func (b *breakAllLayout) PlaceWidth(*ui.LayoutContext, ui.Rect)  {}
func (b *breakAllLayout) MeasureHeight(ctx *ui.LayoutContext) float32 {
	return float32(max(len(b.linesAt(ctx.ContentWidth())), 1)) * b.line
}
func (b *breakAllLayout) PlaceHeight(*ui.LayoutContext, ui.Rect) {}

// TextBreakAll 是逐字折行的文字（break-all）：字号、行高与颜色同 Text，宽度由外层给（占满可用宽度），高度随折出的行数增长。
// 每一行按 Text 的基线位置画（见 baseline.go）；折行的规则见本文件头。
func (l *Look) TextBreakAll(c ui.C, s string, st TextStyle, weight float32, color tokens.Color) ui.Box {
	size, line := l.Rem(st.Size), l.Rem(st.Line)
	layout := &breakAllLayout{canvas: c.Canvas(), text: s, size: size, weight: weight, line: line}
	dy := BaselineShift(weight, size, line, l.Scale)
	return ui.CustomLayout(c, layout).FillWidth().Draw(func(d *ui.DrawContext) {
		style := gfx.TextStyle{Size: size, Weight: weight, Color: d.Fade(l.Col(color))}
		for i, text := range layout.linesAt(d.Rect.Width) {
			d.Canvas.Label(text, gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + dy + float32(i)*line, Width: d.Rect.Width, Height: line}, style)
		}
	})
}
