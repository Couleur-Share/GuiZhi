package kit

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 保留换行与空白的文字，可带命中词高亮（渲染层文字稿面板的 whitespace-pre-wrap，台账 B-067）
//
// 渲染层的文字稿是一个 <p class="whitespace-pre-wrap text-base leading-8">，里面按查找词把命中处包成 <mark>
// （highlightText，与知识库搜索同一套样式）。与 TextMarked 的区别只有空白：white-space: pre-wrap 保留换行与连续空格，
// 行首缩进留着、行尾空格悬挂，制表符按 tab-size 8 展开；而 TextMarked 是 white-space: normal（折叠空白）。
//
// 排法：先按硬换行（\n）切成若干段，每段单独折行与铺命中底色——折行、内边距、断开处的圆角都复用 marktext.go 的排版器
// （markBuilder），只是命中模型不折叠空白（newPreModel）；空行占一个行高。整份文字的结果按（文字、命中词、字号字重、宽度）
// 缓存一份：文字稿可以有几万字，宽度与命中词不变时稳态帧不重排。
//
// 绘制只画看得见的行（Window 回报视口）：文字稿几万字、几千行时，每帧给每一行都落一次笔不划算。

// PreWrapView 是 TextPreWrap 要的两个回调。
type PreWrapView struct {
	// Window 回报此刻看得见的竖向范围（窗口坐标的上沿与下沿）；nil 表示全部都画。
	Window func() (lo, hi float32)
	// Layout 在每次绘制时回报这一帧的版面：文字块的矩形（窗口坐标，已含滚动）、命中数，以及第 i 处命中所在行的上沿（窗口坐标）。
	// 调用方据此更新查找条的计数、把当前命中滚进视口（渲染层 useMarkFindNavigation）。matchTop 只在回调期间有效。
	Layout func(rect gfx.Rect, matches int, matchTop func(i int) float32)
}

// preKey 是整份排版结果的缓存键。
type preKey struct {
	text, query         string
	size, weight, width float32
	pad                 float32
}

// preResult 是一次排版的结果：rows 是每个视觉行要画的文字片段，boxes 是每行要铺的底色，marks 是各处命中的起点所在的行号。
type preResult struct {
	rows  [][]markFrag
	boxes [][]markBox
	marks []int
}

// preCache 记最近一次的排版结果（只留一份：文字稿面板同一时刻只有一份文字）。
type preCache struct {
	key preKey
	res *preResult
	ok  bool
}

// newPreModel 把一行（不含换行）按 spans 切成交替的普通段与命中段，不折叠空白（white-space: pre-wrap）。
func newPreModel(text string, spans []MarkSpan) *markModel {
	m := &markModel{full: text}
	add := func(a, z int, marked bool) {
		if z > a {
			m.starts, m.ends, m.marked = append(m.starts, a), append(m.ends, z), append(m.marked, marked)
		}
	}
	pos := 0
	for _, sp := range spans {
		add(pos, sp.Start, false)
		add(sp.Start, sp.End, true)
		pos = sp.End
	}
	add(pos, len(text), false)
	return m
}

// buildPreWrap 排整份文字。命中词先去首尾空白（MarkSpans），按展开制表符之后的每一硬行匹配：命中不会跨过换行。
func (b *markBuilder) buildPreWrap(text, query string, width float32) *preResult {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	res := &preResult{}
	for _, hard := range strings.Split(text, "\n") {
		hard = expandTabs(hard)
		b.m = newPreModel(hard, MarkSpans(hard, query))
		laid := b.layout(width, 0, false)
		base := len(res.rows)
		for i, row := range laid.lines {
			for _, f := range row {
				// 命中的起点：这一片带着左内边距（延续到下一行的片段没有）。
				if f.marked && f.pl > 0 {
					res.marks = append(res.marks, base+i)
				}
			}
		}
		res.rows = append(res.rows, laid.lines...)
		res.boxes = append(res.boxes, laid.boxes...)
	}
	return res
}

// preWrapLayout 是 TextPreWrap 的版面：宽度由外层给，高度随折出的行数增长；同一宽度只排一次。
type preWrapLayout struct {
	look          *Look
	canvas        *gfx.Canvas
	text, query   string
	size, weight  float32
	line, pad     float32
	res           *preResult
	width         float32
	filledByWidth bool
}

func (b *preWrapLayout) resultAt(width float32) *preResult {
	if b.filledByWidth && b.width == width {
		return b.res
	}
	cache := b.look.noKern(b.canvas)
	key := preKey{text: b.text, query: b.query, size: b.size, weight: b.weight, width: width, pad: b.pad}
	if !cache.pre.ok || cache.pre.key != key {
		builder := &markBuilder{look: b.look, canvas: b.canvas, size: b.size, weight: b.weight, pad: b.pad}
		cache.pre = preCache{key: key, res: builder.buildPreWrap(b.text, b.query, width), ok: true}
	}
	b.res, b.width, b.filledByWidth = cache.pre.res, width, true
	return b.res
}

// 文字块的宽度由外层定（FillWidth），自然宽度没有意义，取 0。
func (b *preWrapLayout) MeasureWidth(*ui.LayoutContext) float32 { return 0 }
func (b *preWrapLayout) PlaceWidth(*ui.LayoutContext, ui.Rect)  {}
func (b *preWrapLayout) PlaceHeight(*ui.LayoutContext, ui.Rect) {}
func (b *preWrapLayout) MeasureHeight(ctx *ui.LayoutContext) float32 {
	return float32(max(len(b.resultAt(ctx.ContentWidth()).rows), 1)) * b.line
}

// TextPreWrap 是保留换行与空白的文字（pre-wrap），query 非空（去首尾空白后）时把每一处命中画成 mark。
// size、line 是逻辑像素；返回的盒子占满可用宽度，高度是行数乘行高。
func (l *Look) TextPreWrap(c ui.C, s, query string, size, line, weight float32, color tokens.Color, view PreWrapView) ui.Box {
	layout := &preWrapLayout{
		look: l, canvas: c.Canvas(), text: s, query: strings.TrimSpace(query),
		size: size, weight: weight, line: line, pad: l.Space(0.5),
	}
	dy := BaselineShift(weight, size, line, l.Scale)
	radius := l.Rem(0.75) - 4 // rounded-sm：calc(var(--radius) - 4px)
	return ui.CustomLayout(c, layout).FillWidth().Draw(func(d *ui.DrawContext) {
		res := layout.resultAt(d.Rect.Width)
		if view.Layout != nil {
			view.Layout(d.Rect, len(res.marks), func(i int) float32 { return d.Rect.Y + float32(res.marks[i])*line })
		}
		first, last := 0, len(res.rows)-1
		if view.Window != nil {
			lo, hi := view.Window()
			first = max(int((lo-d.Rect.Y)/line)-1, 0)
			last = min(int((hi-d.Rect.Y)/line)+1, last)
		}
		hostFill := d.Fade(l.Col(color))
		markFill := d.Fade(l.Col(markFg))
		for i := first; i <= last; i++ {
			top := d.Rect.Y + float32(i)*line
			// 这一行的底色全部铺完再画文字。
			for _, bx := range res.boxes[i] {
				l.paintMarkBg(d, top, d.Rect.X+bx.x0, bx.x1-bx.x0, size, line, weight, radius, bx.roundL, bx.roundR)
			}
			for _, f := range res.rows[i] {
				fill := hostFill
				if f.marked {
					fill = markFill
				}
				style := gfx.TextStyle{Size: size, Weight: weight, Color: fill}
				d.Canvas.Label(f.text, gfx.Rect{X: d.Rect.X + f.x, Y: top + dy, Width: max(f.w+2, 1), Height: line}, style)
			}
		}
	})
}
