package kit

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// TextRuns 是一行由几段相邻文字拼成的文字：每一段画成一个 Label，宽度取各段自己的自然宽度，首尾相接。
//
// 渲染层里 `{状态}{" · 阶段"}`、`{n} {单位}` 这样的 JSX 在 DOM 里是几个相邻的文本节点，
// 可访问性快照、文字行矩形都按文本节点各记一条（对照的 L1 文字序列与 L2 文字行都按这个口径比）。
// Viva 的一个 Label 对应一条，所以视觉上连成一句的文字要拆成同样的几段画，对照才逐段对得上。
//
// 每段保留原文（含首尾空白，渲染层的文本节点也带着），空白不会被折叠；整段只有空白的（JSX 里两个表达式之间的单个空格）
// 不画 Label，只占它的宽度——渲染层的取文字也跳过空白节点。
// 只排单行、不折行：用于短文字（状态、计数）；会折行的长文字不要用它。
func (l *Look) TextRuns(c ui.C, st TextStyle, weight float32, color tokens.Color, runs ...string) ui.Box {
	return l.TextRunsPx(c, l.Rem(st.Size), l.Rem(st.Line), weight, color, runs...)
}

// TextRunsPx 与 TextRuns 相同，字号与行高直接给逻辑像素（渲染层里 text-[11px] 这类 px 字面量不随字号档缩放）。
func (l *Look) TextRunsPx(c ui.C, size, line, weight float32, color tokens.Color, runs ...string) ui.Box {
	canvas := c.Canvas()
	return ui.Row(c).Items(ui.Start).Children(func() {
		for i, run := range runs {
			key := "run" + strconv.Itoa(i)
			width := canvas.TextWidthWeight(run, size, weight)
			if strings.TrimSpace(run) == "" {
				ui.Column(c.Key(key)).Width(width).Shrink(0) // 只占宽度的空白
				continue
			}
			l.TextPx(c.Key(key), run, size, line, weight, color).Width(width).Shrink(0)
		}
	})
}
