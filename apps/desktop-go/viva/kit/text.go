package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 字重：CSS font-weight 到 Viva 字重刻度（0 / 0.15 / 0.23 / 0.40 对应 400 / 500 / 600 / 700）。
const (
	WeightNormal   = gfx.WeightRegular
	WeightMedium   = gfx.WeightMedium
	WeightSemibold = gfx.WeightSemibold
	WeightBold     = gfx.WeightBold
)

// TextStyle 是 Tailwind 的一档字号：字号与行高，单位 rem（text-sm = 0.875rem / 1.25rem）。
type TextStyle struct{ Size, Line float32 }

// Tailwind 默认字号档。
var (
	TextXS   = TextStyle{0.75, 1}
	TextSM   = TextStyle{0.875, 1.25}
	TextBase = TextStyle{1, 1.5}
	TextLG   = TextStyle{1.125, 1.75}
	TextXL   = TextStyle{1.25, 1.75}
	Text2XL  = TextStyle{1.5, 2}
)

// TextOpt 调整 Text 里面那个真正的文字节点（对齐、省略号、折行）。布局相关的设置（Grow、Shrink、边距、宽度上限）
// 加在 Text 返回的外层盒子上。
type TextOpt func(ui.Box) ui.Box

// Ellipsis 让放不下的文字以省略号收尾（CSS truncate）。
func Ellipsis() TextOpt { return func(b ui.Box) ui.Box { return b.Ellipsis() } }

// Wrapped 让文字按可用宽度折行（CSS 默认的 white-space: normal），高度随行数增长；
// Text 默认是单行（不折行），渲染层里会折行的段落（设置项说明、对话框正文）要加这个选项。
func Wrapped() TextOpt { return func(b ui.Box) ui.Box { return b.MaxLines(0) } }

// Centered 让文字在行内居中（text-center）。
func Centered() TextOpt { return func(b ui.Box) ui.Box { return b.TextCenter() } }

// Right 让文字靠行尾（text-right）。
func Right() TextOpt { return func(b ui.Box) ui.Box { return b.TextEnd() } }

// Text 是一行文字：字号与行高取字号档，颜色取令牌。
//
// 返回的是外层盒子，占的版面恰好是一个行盒（行高）；文字节点在里面按渲染层（Chromium）的基线位置整体平移，
// 平移用内边距与反向边距，不改变外层盒子的占位。依据与模型见 baseline.go。
func (l *Look) Text(c ui.C, s string, st TextStyle, weight float32, color tokens.Color, opts ...TextOpt) ui.Box {
	return l.TextPx(c, s, l.Rem(st.Size), l.Rem(st.Line), weight, color, opts...)
}

// TextPx 与 Text 相同，字号与行高直接给逻辑像素：渲染层里 text-[13px] 这类 px 字面量不随字号档缩放。
// line 在 Tailwind 里没有显式行高时是继承来的 1.5 倍字号（例如 13px 的行高是 19.5px）。
func (l *Look) TextPx(c ui.C, s string, size, line, weight float32, color tokens.Color, opts ...TextOpt) ui.Box {
	dy := BaselineShift(weight, size, line, l.Scale)
	return ui.Column(c).Children(func() {
		box := ui.Text(c, s).
			FontSize(size).
			LineHeight(line).
			Weight(weight).
			Color(l.Col(color)).
			FillWidth()
		switch {
		case dy > 0:
			box = box.PadTop(dy).MarginBottom(-dy)
		case dy < 0:
			box = box.MarginTop(dy).PadBottom(-dy)
		}
		for _, opt := range opts {
			box = opt(box)
		}
	})
}

// Heading 与 Text 相同，但在可访问树里是 level 级标题（渲染层的 h1、h3）。外框就是外层盒子（行盒）。
func (l *Look) Heading(c ui.C, level uint8, s string, st TextStyle, weight float32, color tokens.Color) ui.Box {
	return l.Text(c, s, st, weight, color).Draw(func(d *ui.DrawContext) {
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleHeading, s, gfx.HitOptions{Level: level})
	})
}
