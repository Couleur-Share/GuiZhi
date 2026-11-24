package kit

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// TextFlow 是几段相邻文字拼成的、会折行的行内文字：每一段在每一行里的那一截各画成一个 Label，同一行的几截首尾相接。
//
// 渲染层里 `{前缀}{标题}` 这样的 JSX 在 DOM 里是几个相邻的文本节点，视觉上是一段会折行的话：后一段接在前一段之后排，
// 放不下就在后一段里换行，第二行回到容器的左缘。可访问性快照与文字行矩形都按文本节点各记一条（对照的 L1 文字序列与 L2 文字行
// 都按这个口径比），所以不能把几段拼成一个 Label 画（那样节点数对不上），也不能用 TextRuns（它只排单行）。
// 折行按 Chromium 的断行机会（WrapText：汉字之间、空格之后、避头尾）在拼起来的整段文字上做，再按各段的范围把每一行拆回各段。
//
// width 是容器宽度（逻辑像素），不大于 0 表示还不知道（首帧），按一行排；调用方在宽度变化时再出一帧即可。
// 各段共用一套字号、字重与颜色。文字里不能有硬换行（行内文字没有）。
func (l *Look) TextFlow(c ui.C, st TextStyle, weight float32, color tokens.Color, width float32, runs ...string) ui.Box {
	return l.TextFlowPx(c, l.Rem(st.Size), l.Rem(st.Line), weight, color, width, runs...)
}

// TextFlowPx 与 TextFlow 相同，字号与行高直接给逻辑像素（渲染层里 text-[11px] 这类 px 字面量不随字号档缩放）。
func (l *Look) TextFlowPx(c ui.C, size, line, weight float32, color tokens.Color, width float32, runs ...string) ui.Box {
	return l.textFlow(c, size, line, weight, color, width, false, WrapText, runs)
}

// TextFlowBreakAll 与 TextFlow 相同，只是折行按 break-all（Tailwind 的 break-all：字母与数字也逐字可断，见 breakall.go）：
// 渲染层里 `{状态} · {网址}` 这样带 break-all 的几个相邻文本节点，网址放不下时在最后放得下的字符处断开，
// 不是在 / . 之类的分隔符之后断。各段仍各画成一个 Label（节点数与渲染层的文本节点一一对应）。
func (l *Look) TextFlowBreakAll(c ui.C, st TextStyle, weight float32, color tokens.Color, width float32, runs ...string) ui.Box {
	return l.textFlow(c, l.Rem(st.Size), l.Rem(st.Line), weight, color, width, false, wrapBreakAllTiled, runs)
}

// wrapBreakAllTiled 是 WrapBreakAll 的「首尾相接」版：WrapBreakAll 折行时去掉了行尾悬挂的空格与下一行行首的空格，
// 各行拼起来比原文短；textFlow 要按各行长度在拼起来的整段上数偏移，所以把去掉的空格并回上一行的行尾（它们本来就悬挂在那里）。
func wrapBreakAllTiled(text string, width float32, measure func(string) float32) []string {
	rows := WrapBreakAll(text, width, measure)
	out := make([]string, 0, len(rows))
	pos := 0
	for i, row := range rows {
		start := pos + strings.Index(text[pos:], row)
		if i > 0 && start > pos {
			out[i-1] += text[pos:start]
		}
		pos = start + len(row)
		out = append(out, row)
	}
	if n := len(out); n > 0 {
		out[n-1] += text[pos:]
	}
	return out
}

// TextFlowCentered 与 TextFlow 相同，只是每一行在容器里居中（渲染层的 text-align: center，按钮里的文字默认就是这样）：
// 折成几行，每一行的几截作为一个整体各自居中。
func (l *Look) TextFlowCentered(c ui.C, st TextStyle, weight float32, color tokens.Color, width float32, runs ...string) ui.Box {
	return l.textFlow(c, l.Rem(st.Size), l.Rem(st.Line), weight, color, width, true, WrapText, runs)
}

func (l *Look) textFlow(c ui.C, size, line, weight float32, color tokens.Color, width float32, center bool, wrap func(string, float32, func(string) float32) []string, runs []string) ui.Box {
	canvas := c.Canvas()
	measure := func(s string) float32 { return canvas.TextWidthWeight(s, size, weight) }
	joined := strings.Join(runs, "")
	rows := []string{joined}
	if width > 0 && joined != "" {
		rows = wrap(joined, width, measure)
	}
	outer := ui.Column(c)
	if center {
		outer = outer.Items(ui.Center)
	}
	return outer.Children(func() {
		off := 0 // 本行在拼起来的整段文字里的起点（字节）；WrapText 的各行首尾相接，正好铺满整段
		for i, row := range rows {
			end := off + len(row)
			ui.Row(c.Key("row" + strconv.Itoa(i))).Height(line).Items(ui.Start).Children(func() {
				start := 0
				for k, run := range runs {
					lo, hi := max(off, start), min(end, start+len(run))
					start += len(run)
					if lo >= hi {
						continue
					}
					frag := joined[lo:hi]
					key := "frag" + strconv.Itoa(k)
					// 行尾的空白在 Chromium 里悬挂：不占宽、不进文字行矩形、居中时也不算。落在行尾的那一截，Label 的宽度只算到
					// 最后一个非空白字符，文字仍是完整的一截（可访问性文字序列要能拼回整段；Label 比文字窄时 Viva 不折行）。
					shown := frag
					if hi == end {
						shown = strings.TrimRight(frag, " \t")
					}
					w := measure(shown)
					if strings.TrimSpace(frag) == "" {
						ui.Column(c.Key(key)).Width(w).Shrink(0) // 只占宽度的空白
						continue
					}
					l.TextPx(c.Key(key), frag, size, line, weight, color).Width(w).Shrink(0)
				}
			})
			off = end
		}
	})
}
