package kit

import (
	"math"
	"unicode/utf8"

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

// TextOpt 调整 Text 里面那个真正的文字节点（对齐、省略号、折行、下划线）。布局相关的设置（Grow、Shrink、边距、宽度上限）
// 加在 Text 返回的外层盒子上。
type TextOpt struct {
	box       func(ui.Box) ui.Box // 作用在文字节点上的设置；空表示没有
	align     textAlign           // 文字在行盒里靠哪一端；下划线要知道文字落在哪一段
	underline bool                // 画下划线（见 Underline）
}

// textAlign 是文字在行盒里的水平位置。
type textAlign uint8

const (
	alignStart  textAlign = iota // 靠行首
	alignCenter                  // 居中
	alignEnd                     // 靠行尾
)

// Ellipsis 让放不下的文字以省略号收尾（CSS truncate）。
func Ellipsis() TextOpt { return TextOpt{box: func(b ui.Box) ui.Box { return b.Ellipsis() }} }

// Wrapped 让文字按可用宽度折行（CSS 默认的 white-space: normal），高度随行数增长；
// Text 默认是单行（不折行），渲染层里会折行的段落（设置项说明、对话框正文）要加这个选项。
func Wrapped() TextOpt { return TextOpt{box: func(b ui.Box) ui.Box { return b.MaxLines(0) }} }

// LineClamp 让文字至多排 n 行，放不下时最后一行以省略号收尾（Tailwind 的 line-clamp-N）。n 不大于 1 时等同 Ellipsis。
func LineClamp(n int) TextOpt {
	return TextOpt{box: func(b ui.Box) ui.Box { return b.MaxLines(n) }}
}

// Centered 让文字在行内居中（text-center）。
func Centered() TextOpt {
	return TextOpt{box: func(b ui.Box) ui.Box { return b.TextCenter() }, align: alignCenter}
}

// Right 让文字靠行尾（text-right）。
func Right() TextOpt {
	return TextOpt{box: func(b ui.Box) ui.Box { return b.TextEnd() }, align: alignEnd}
}

// Underline 在 on 为真时给文字画下划线（CSS 的 hover:underline：宿主悬停时 on 为真，没有过渡，立即出现）。
// Viva 的文字没有下划线样式（台账 B-012），所以自己画一条线，位置、粗细、横向范围取自渲染层的实测
// （scripts/parity/tools/probe-underline.mjs，字号 11–18px、字重 400）：
// 1 个设备像素厚，在 round(行盒顶) + 基线行 + 1 这一行，横向正好是文字的绘制宽度（不超过可用宽度，省略号时取满），颜色同文字。
// 只用于单行文字（折行文字的每一行要各画一条）；不能与会替换外层盒子 Draw 钩子的调用（如 Heading）同用。
func Underline(on bool) TextOpt { return TextOpt{underline: on} }

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
	var underline bool
	align := alignStart
	for _, opt := range opts {
		underline = underline || opt.underline
		if opt.align != alignStart {
			align = opt.align
		}
	}
	outer := ui.Column(c).Children(func() {
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
			if opt.box != nil {
				box = opt.box(box)
			}
		}
	})
	if underline {
		outer = outer.Draw(func(d *ui.DrawContext) { l.paintUnderline(d, s, size, line, weight, color, align) })
	}
	return outer
}

// paintUnderline 画 Underline 的那条线：外层盒子就是渲染层的行盒，线的位置按渲染层的基线模型（ChromeBaselineRow）算。
func (l *Look) paintUnderline(d *ui.DrawContext, s string, size, line, weight float32, color tokens.Color, align textAlign) {
	scale := l.Scale
	if scale <= 0 {
		scale = 1
	}
	sizeDev, lineDev := float64(size)*float64(scale), float64(line)*float64(scale)
	top := math.Floor(float64(d.Rect.Y)*float64(scale) + 0.5) // 行盒顶对设备像素四舍五入
	row := top + float64(ChromeBaselineRow(weight, sizeDev, lineDev)) + 1
	width := min(d.Canvas.TextWidthWeight(s, size, weight), d.Rect.Width)
	x := d.Rect.X
	switch align {
	case alignCenter:
		x += (d.Rect.Width - width) / 2
	case alignEnd:
		x += d.Rect.Width - width
	}
	d.Canvas.Fill(gfx.Rect{X: x, Y: float32(row / float64(scale)), Width: width, Height: 1 / scale}, d.Fade(l.Col(color)))
}

// TextTracked 是带字距的一行文字（CSS letter-spacing；Tailwind 的 tracking-wide 是 0.025em）。em 是字距，单位 em。
//
// Viva 的文字引擎没有字距：这里按自然字宽排字，把整行的宽度加上「每个字符之后一份字距」（CSS 的字距也加在最后一个字符之后），
// 后面的元素因此落在渲染层的位置上；字形本身依次比渲染层靠左，最多差 (字符数-1)×字距（登记见台账 A-013）。
func (l *Look) TextTracked(c ui.C, s string, st TextStyle, weight float32, color tokens.Color, em float32) ui.Box {
	size := l.Rem(st.Size)
	natural := c.Canvas().TextWidthWeight(s, size, weight)
	width := natural + float32(utf8.RuneCountInString(s))*em*size
	return l.Text(c, s, st, weight, color).Width(width).Shrink(0)
}

// Heading 与 Text 相同，但在可访问树里是 level 级标题（渲染层的 h1、h3）。外框就是外层盒子（行盒）。
func (l *Look) Heading(c ui.C, level uint8, s string, st TextStyle, weight float32, color tokens.Color, opts ...TextOpt) ui.Box {
	return l.Text(c, s, st, weight, color, opts...).Draw(func(d *ui.DrawContext) {
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleHeading, s, gfx.HitOptions{Level: level})
	})
}
