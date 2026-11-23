package kit

import (
	"math"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// Markdown 正文的原生排版（渲染层 MarkdownBody 里的 `.prose`）。
//
// 依据：渲染层的 `prose prose-sm dark:prose-invert prose-headings:… prose-p:…` 这一串类名里，凡是来自
// @tailwindcss/typography 的都不生效——tailwind.config.js 的 plugins 只有 tailwindcss-animate，globals.css 里
// 有一段注释写明「本项目没装 @tailwindcss/typography，.prose 的每条规则都得自己写」。所以真正起作用的只有 globals.css 里手写的 `.prose` 规则，
// 排版数值（字号、行高、外边距）都取自那里，而不是 Typography 的 prose-sm 档（0.875rem / 1.7142857）：
//   - 正文字号继承根字号（1rem），行高 1.6（无单位，随各元素自己的字号缩放），颜色 foreground；
//   - 段落 margin-bottom 0.75em；标题 h1/h2/h3 取 1.5 / 1.25 / 1.1em（h4–h6 同正文）、字重 600、margin 1em 0 0.5em；
//   - ul/ol padding-left 1.5em、margin-bottom 0.75em，列表项 margin-bottom 0.35em；圆点、空心圆、方块依嵌套深度轮换，有序列表是十进制；
//   - 独占一行（只含一个 <strong>）的段落当小节标题：margin-top 1.15em、margin-bottom 0.4em、行高 1.45，在父级里排第一个时 margin-top 为 0；
//   - 行内代码：muted 底、padding 0.15em 0.4em、圆角 0.25em、字号 0.9em、等宽字体；代码块：muted 底、padding 0.75em 1em、圆角 0.5em，
//     外面一层 my-3（0.75rem）；引用：左边 3px primary 竖线、padding-left 1em、muted 文字、斜体；链接 primary 加下划线；粗体 700；分隔线 1px 加 1em 外边距。
//
// 垂直外边距按 CSS 折叠：兄弟之间取较大者；容器（ul、li、blockquote 没有垂直方向的内边距与边框）的首尾外边距与首尾子元素折叠。
// 最后一块的下外边距折叠出 .prose 之后，仍落在卡片的内边距里（卡片有 padding），所以由 Prose 的调用方留在盒内（返回盒的 MarginBottom）。
//
// 没有照做的：代码块的横向滚动改为折行（只读卡片里没有横向滚动条）、右上角的语言标签与复制钮不画；链接默认只画样式、不能点（ProseWith 传 OnLink 才可点，Wiki 正文用它）；
// 表格、任务列表复选框、行内 HTML 不渲染（见 markdown.go）。

// 各元素的外观常数（单位 em 的随该元素字号缩放，px 的不缩放）。
const (
	proseLineHeight  = 1.6  // .prose { line-height: 1.6 }
	proseParaBottom  = 0.75 // .prose p
	proseListBottom  = 0.75 // .prose ul, .prose ol
	proseListPad     = 1.5
	proseItemBottom  = 0.35 // .prose li
	proseHeadTop     = 1.0  // .prose h1…h6
	proseHeadBottom  = 0.5
	proseLeadTop     = 1.15 // .prose p:has(> strong:only-child)
	proseLeadBottom  = 0.4
	proseLeadLine    = 1.45
	proseQuoteBar    = 3.0 // blockquote 的左边线（px）
	proseCodeEm      = 0.9 // .prose code
	proseChipPadX    = 0.4
	proseChipPadY    = 0.15
	proseChipRadius  = 0.25
	proseLinkPadX    = 2.0  // 站内链接的 px-0.5（px）
	proseBlockPadY   = 0.75 // .prose pre
	proseBlockPadX   = 1.0
	proseBlockRadius = 0.5
)

// layoutUnit 把长度截断到 1/64 px（Chromium 的 LayoutUnit）：渲染层里 1.6 倍行高的 16px 文字一行是 25.59375 而不是 25.6，
// 一屏正文累计下来差零点几像素，会让行盒顶的取整在某些行上差一行（行盒顶先取整、再加基线行）。
func layoutUnit(v float32) float32 { return float32(math.Floor(float64(v)*64+1e-4)) / 64 }

// proseHeadingEm 是各级标题的字号倍数。
func proseHeadingEm(level int) float32 {
	switch level {
	case 1:
		return 1.5
	case 2:
		return 1.25
	case 3:
		return 1.1
	}
	return 1
}

// proseInherit 是块从祖先继承的文字样式（引用里是 muted 斜体）。
type proseInherit struct {
	color  tokens.Color
	italic bool
}

// proseNode 是排版树的一个节点：块、列表项（item）。top、bottom 是折叠后的外边距（含与首尾子节点折叠的部分）。
type proseNode struct {
	block       *MdBlock
	item        bool
	top, bottom float32
	kids        []*proseNode
	size, line  float32
	marker      string      // 有序列表的记号文字
	bullet      proseBullet // 无序列表的记号图形
	inh         proseInherit
}

// collapse 让容器的上下外边距与首尾子节点折叠。
func (n *proseNode) collapse() {
	if len(n.kids) == 0 {
		return
	}
	n.top = max(n.top, n.kids[0].top)
	n.bottom = max(n.bottom, n.kids[len(n.kids)-1].bottom)
}

// isLeadPara 报告段落是不是「独占一行的粗体」：直接子元素里只有一个 <strong>，别的行内元素（斜体、代码、链接、删除线）一个也没有
// （CSS 的 :only-child 只数元素，文字节点不算，所以粗体后面跟着文字也算）。
func isLeadPara(b *MdBlock) bool {
	bold := 0
	for _, s := range b.Spans {
		if s.Break {
			continue
		}
		plainBold := s.Bold && !s.Italic && !s.Strike && !s.Code && s.Href == ""
		switch {
		case plainBold:
			bold++
		case s.Italic || s.Strike || s.Code || s.Href != "" || s.Bold:
			return false
		}
	}
	// 相邻的粗体片段在 mergeSpans 里已合并成一个；中间夹着普通文字的两段粗体是两个 <strong>
	return bold == 1
}

// proseBuilder 把块树翻成排版树并声明控件。
type proseBuilder struct {
	l     *Look
	base  float32 // 1rem（px）
	opts  ProseOpts
	top   map[*proseNode]bool // 顶层节点：标题锚点只回报它们
	texts int                 // 已声明的文字段数：给每段一个序号，链接命中区的键由它区分（Key 只按文字在容器作用域里排重，不含段落自己的路径）
}

// Prose 把 Markdown 块排成一列，返回外层盒。lead 是它上方已有的外边距（卡片里内容区的 mt-1.5）：它与第一块的上外边距折叠，
// 取较大者作为返回盒的上外边距；最后一块的下外边距作为返回盒的下外边距。调用方不要再给返回盒加垂直外边距。
func (l *Look) Prose(c ui.C, blocks []MdBlock, lead float32) ui.Box {
	return l.ProseFollowed(c, blocks, lead, 0)
}

// ProseFollowed 是 Prose，且让最后一块的下外边距与「下一个兄弟的上外边距 follow」折叠（CSS 的外边距折叠：父盒没有内边距与边框时，
// 末块的下外边距穿出父盒，与下一个兄弟的上外边距取较大者，而不是相加）。ui 的列不折叠外边距，所以这里把返回盒的下外边距减去 follow，
// 调用方给下一个兄弟照常保留它自己的上外边距 follow：两者之和恰好是 max(末块下外边距, follow)。
func (l *Look) ProseFollowed(c ui.C, blocks []MdBlock, lead, follow float32) ui.Box {
	return l.ProseWith(c, blocks, lead, follow, ProseOpts{})
}

// ProseOpts 是 Prose 的可选行为：零值就是只读的排版（链接只画样式）。
type ProseOpts struct {
	// OnLink 非空时，链接是可点的：每一截链接文字登记一块命中区（角色 link、手形光标、键盘可达），
	// 点击在事件遍之后回调 OnLink(href)。Wiki 的 [[双链]] 与外部链接靠它。
	OnLink func(href string)
	// LinkChip 报告某个链接是否画成「站内链接」的底色小块（font-medium、primary/8 底、两侧 2px 内边距；悬停 primary/15）。
	// 为空则链接都只有颜色加下划线。
	LinkChip func(href string) bool
	// Anchor 在每个顶层标题画出时回报它的矩形（窗口坐标）：页内目录据此滚动到标题。b 指向 blocks 里的元素。
	Anchor func(b *MdBlock, r gfx.Rect)
}

// ProseWith 是 ProseFollowed，另带可选行为（链接可点、标题锚点）。
func (l *Look) ProseWith(c ui.C, blocks []MdBlock, lead, follow float32, opts ProseOpts) ui.Box {
	p := &proseBuilder{l: l, base: l.Rem(1), opts: opts}
	nodes := p.build(blocks, proseInherit{color: l.T.Foreground}, 0)
	if opts.Anchor != nil {
		p.top = make(map[*proseNode]bool, len(nodes))
		for _, n := range nodes {
			p.top[n] = true
		}
	}
	top, bottom := lead, float32(0)
	if len(nodes) > 0 {
		top, bottom = max(lead, nodes[0].top), nodes[len(nodes)-1].bottom
	}
	bottom = max(bottom, follow) - follow
	return ui.Column(c).FillWidth().MarginTop(top).MarginBottom(bottom).Children(func() { p.renderKids(c, nodes) })
}

func (p *proseBuilder) build(blocks []MdBlock, inh proseInherit, depth int) []*proseNode {
	l := p.l
	nodes := make([]*proseNode, 0, len(blocks))
	for idx := range blocks {
		b := &blocks[idx]
		n := &proseNode{block: b, inh: inh, size: p.base}
		switch b.Kind {
		case MdParagraph:
			n.line = proseLineHeight * n.size
			switch {
			case b.Tight:
			case isLeadPara(b):
				n.line = proseLeadLine * n.size
				if idx > 0 {
					n.top = proseLeadTop * n.size
				}
				n.bottom = proseLeadBottom * n.size
			default:
				n.bottom = proseParaBottom * n.size
			}
		case MdHeading:
			n.size = p.base * proseHeadingEm(b.Level)
			n.line = proseLineHeight * n.size
			n.top, n.bottom = proseHeadTop*n.size, proseHeadBottom*n.size
		case MdList:
			n.bottom = proseListBottom * p.base
			for k, item := range b.Items {
				li := &proseNode{item: true, inh: inh, size: p.base, bottom: layoutUnit(proseItemBottom * p.base)}
				li.marker, li.bullet = proseMarker(b, k, depth)
				li.kids = p.build(item, inh, depth+1)
				li.collapse()
				n.kids = append(n.kids, li)
			}
		case MdQuote:
			n.kids = p.build(b.Children, proseInherit{color: l.T.MutedForeground, italic: true}, depth)
		case MdCode:
			n.top, n.bottom = l.Space(3), l.Space(3)
			n.size = p.base * proseCodeEm
			n.line = proseLineHeight * p.base
		case MdRule:
			n.top, n.bottom = p.base, p.base
		}
		n.line, n.top, n.bottom = layoutUnit(n.line), layoutUnit(n.top), layoutUnit(n.bottom)
		n.collapse()
		nodes = append(nodes, n)
	}
	return nodes
}

// proseBullet 是无序列表记号的形状（Chromium 把 disc / circle / square 画成图形，不是文字）。
type proseBullet uint8

const (
	bulletNone   proseBullet = iota // 有序列表：记号是文字
	bulletDisc                      // 实心圆
	bulletCircle                    // 空心圆
	bulletSquare                    // 实心方块
)

// proseMarker 是列表项的记号：无序按嵌套深度轮换圆点、空心圆、方块（返回图形），有序是十进制加句点（返回文字）。
func proseMarker(list *MdBlock, index, depth int) (string, proseBullet) {
	if list.Ordered {
		return strconv.Itoa(list.Start+index) + ".", bulletNone
	}
	switch depth % 3 {
	case 0:
		return "", bulletDisc
	case 1:
		return "", bulletCircle
	}
	return "", bulletSquare
}

// bulletGeom 按 Chromium 的 ListMarker::RelativeSymbolMarkerRect 给出记号相对列表项左上角的位置与边长（逻辑 px，未取整）。
// ascent、descent 与行内半行距的取整同 ChromeBaselineRow（正文字体）：记号顶 = 行盒顶 + floor(半行距) + 3*(A - A*2/3)/2，
// 边长 = (A*2/3 + 1)/2（整数除法）；水平方向记号盒右缘距内容左缘 0.5em+3、盒宽 = 边长+2、图形在盒内右移 1px
// （对 14 / 16 / 18px 的 React 渲染逐像素校准）。
func bulletGeom(size, line, scale float32) (dx, dy, side float32) {
	sc := float64(scale)
	f := faceOfWeight(WeightNormal)
	a := int(roundHalfUp(f.ascent * float64(size) * sc))
	d := int(roundHalfUp(f.descent * float64(size) * sc))
	half := int(math.Floor((float64(line)*sc - float64(a+d)) / 2))
	sideDev := (a*2/3 + 1) / 2
	dy = float32(half+3*(a-a*2/3)/2) / scale
	side = float32(sideDev) / scale
	dx = -(size*0.5 + side + 4)
	return dx, dy, side
}

func (p *proseBuilder) renderKids(c ui.C, kids []*proseNode) {
	for i, k := range kids {
		gap := float32(0)
		if i > 0 {
			gap = max(kids[i-1].bottom, k.top)
		}
		p.render(c.Key("n"+strconv.Itoa(i)), k).MarginTop(gap)
	}
}

func (p *proseBuilder) render(c ui.C, n *proseNode) ui.Box {
	l := p.l
	t := l.T
	switch {
	case n.item:
		return p.renderItem(c, n)
	case n.block.Kind == MdParagraph, n.block.Kind == MdHeading:
		return p.renderText(c, n)
	case n.block.Kind == MdList:
		return ui.Column(c).FillWidth().PadSides(0, 0, 0, proseListPad*p.base).Children(func() { p.renderKids(c, n.kids) })
	case n.block.Kind == MdQuote:
		return ui.Column(c).FillWidth().PadSides(0, 0, 0, p.base+proseQuoteBar).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, EdgeLeft, proseQuoteBar, t.Primary) }).
			Children(func() { p.renderKids(c, n.kids) })
	case n.block.Kind == MdCode:
		return p.renderCode(c, n)
	}
	// 分隔线：border-top 1px
	return ui.Column(c).FillWidth().Height(1).Draw(func(d *ui.DrawContext) { l.PaintEdge(d, EdgeTop, 1, t.Border) })
}

// renderItem 声明列表项：内容在列表的左内边距右侧，记号是一个绝对定位的盒，右缘贴着内容的左缘（list-style-position: outside，
// 记号文字带一个尾随空格）。
func (p *proseBuilder) renderItem(c ui.C, n *proseNode) ui.Box {
	l := p.l
	line := layoutUnit(proseLineHeight * n.size)
	if n.bullet != bulletNone {
		return ui.Column(c).FillWidth().Draw(func(d *ui.DrawContext) { p.paintBullet(d, n, line) }).
			Children(func() { p.renderKids(c, n.kids) })
	}
	markerW := c.Canvas().TextWidthWeight(n.marker+" ", n.size, WeightNormal)
	return ui.Column(c).FillWidth().Children(func() {
		ui.Row(c.Key("marker")).Absolute().Left(-markerW).Top(0).Width(markerW).Children(func() {
			l.TextPx(c.Key("text"), n.marker, n.size, line, WeightNormal, n.inh.color)
		})
		p.renderKids(c, n.kids)
	})
}

// paintBullet 把无序记号画成图形：位置按 bulletGeom 落到整数物理像素（Chromium 对记号矩形取整），
// 实心圆 / 方块填充，空心圆描 1px 边。
func (p *proseBuilder) paintBullet(d *ui.DrawContext, n *proseNode, line float32) {
	l := p.l
	scale := d.Canvas.PixelScale()
	dx, dy, side := bulletGeom(n.size, line, scale)
	r := gfx.Rect{
		X:      snapEdge(d.Rect.X+dx, scale),
		Y:      snapEdge(d.Rect.Y+dy, scale),
		Width:  side,
		Height: side,
	}
	col := d.Fade(l.Col(n.inh.color))
	switch n.bullet {
	case bulletDisc:
		d.Canvas.Rounded(r, side/2, col)
	case bulletCircle:
		d.Canvas.RoundedOutline(r, side/2, 1/scale, col)
	default:
		d.Canvas.Rounded(r, 0, col)
	}
}

func (p *proseBuilder) renderText(c ui.C, n *proseNode) ui.Box {
	l := p.l
	b := n.block
	weight := WeightNormal
	if b.Kind == MdHeading {
		weight = WeightSemibold
	}
	layout := newProseText(l, c.Canvas(), p.runs(b.Spans, n, weight), n.size, n.line, weight)
	layout.key, layout.onLink, layout.seq = c, p.opts.OnLink, p.texts
	p.texts++
	box := ui.CustomLayout(c, layout).FillWidth()
	if b.Kind == MdHeading {
		plain, level := MdPlain(b.Spans), uint8(b.Level)
		anchor := p.opts.Anchor
		if !p.top[n] {
			anchor = nil
		}
		return box.Draw(func(d *ui.DrawContext) {
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleHeading, plain, gfx.HitOptions{Level: level})
			if anchor != nil {
				anchor(b, d.Rect)
			}
			layout.draw(d)
		})
	}
	return box.Draw(layout.draw)
}

// runs 把行内片段翻成带样式的文字段。
func (p *proseBuilder) runs(spans []MdSpan, n *proseNode, base float32) []proseRun {
	t := p.l.T
	out := make([]proseRun, 0, len(spans))
	for _, s := range spans {
		if s.Break {
			out = append(out, proseRun{brk: true, size: n.size, weight: base, color: n.inh.color})
			continue
		}
		r := proseRun{text: s.Text, size: n.size, weight: base, color: n.inh.color, italic: n.inh.italic || s.Italic, strike: s.Strike}
		if s.Bold {
			r.weight = WeightBold
		}
		if s.Href != "" {
			r.color, r.underline, r.href = t.Primary, true, s.Href
			if chip := p.opts.LinkChip; chip != nil && chip(s.Href) {
				r.linkChip = true
				if !s.Bold && r.weight < WeightMedium {
					r.weight = WeightMedium
				}
			}
		}
		if s.Code {
			r.size, r.mono, r.chip = n.size*proseCodeEm, true, true
		}
		out = append(out, r)
	}
	return out
}

// renderCode 声明代码块：外层 my-3 由外边距给出，里面是 muted 底的圆角块（padding .75em 1em），文字等宽、折行显示。
func (p *proseBuilder) renderCode(c ui.C, n *proseNode) ui.Box {
	l := p.l
	t := l.T
	padY, padX := proseBlockPadY*p.base, proseBlockPadX*p.base
	layout := &proseCode{look: l, canvas: c.Canvas(), text: strings.TrimRight(n.block.Text, "\n"), size: n.size, line: n.line}
	return ui.Column(c).FillWidth().PadSides(padY, padX, padY, padX).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, Surface{Bg: t.Muted, Radius: proseBlockRadius * p.base})
		}).
		Children(func() {
			ui.CustomLayout(c.Key("code"), layout).FillWidth().Draw(layout.draw)
		})
}

// ---- 行内排版 ----

// proseRun 是一段同样式的文字。
type proseRun struct {
	text      string
	size      float32
	weight    float32
	italic    bool
	mono      bool
	strike    bool
	underline bool
	chip      bool // 行内代码的底色块
	linkChip  bool // 站内链接的底色小块（ProseOpts.LinkChip）
	href      string
	brk       bool // 硬换行
	color     tokens.Color
}

// proseFrag 是一行里属于同一段文字的一截；x 是文字的起点（相对行首，已含行内代码块左侧的内边距）。
type proseFrag struct {
	run          int
	text         string
	x, w         float32
	chipL, chipR bool // 这一截是否含行内代码块的左、右端（折行时只有真正的两端才有内边距与圆角）
}

type proseLine []proseFrag

// proseText 排一段带行内样式的文字（段落、标题）：把各段文字拼成一串按 Chromium 的断行机会（unitEnds）折行，
// 每个断行单元按所含各段各自的字体量宽。折出的行缓存在这里，量高与画共用同一宽度时只折一次。
type proseText struct {
	look   *Look
	canvas *gfx.Canvas
	runs   []proseRun
	size   float32 // 段落基准字号
	line   float32 // 行高
	weight float32 // 段落基准字重（行盒基线按它算）

	runes       []rune
	runOf       []int
	first, last []int // 各段的首、末字符在 runes 里的下标；没有字符为 -1

	lines  []proseLine
	width  float32
	filled bool

	key    ui.C // 本段的键：链接命中区的标识由它派生
	seq    int  // 本段在一次 Prose 里的序号：同一容器里多段带链接的文字，命中区的键不能重名
	onLink func(href string)
}

func newProseText(l *Look, canvas *gfx.Canvas, runs []proseRun, size, line, weight float32) *proseText {
	b := &proseText{look: l, canvas: canvas, runs: runs, size: size, line: line, weight: weight}
	b.first, b.last = make([]int, len(runs)), make([]int, len(runs))
	for i := range runs {
		b.first[i], b.last[i] = -1, -1
	}
	space := true // 段首或硬换行之后：吃掉前导空白（white-space: normal）
	emit := func(ri int, r rune) {
		b.runes = append(b.runes, r)
		b.runOf = append(b.runOf, ri)
		if b.first[ri] < 0 {
			b.first[ri] = len(b.runes) - 1
		}
		b.last[ri] = len(b.runes) - 1
	}
	trimSpace := func() {
		for n := len(b.runes); n > 0 && b.runes[n-1] == ' '; n-- {
			b.runes, b.runOf = b.runes[:n-1], b.runOf[:n-1]
		}
	}
	for ri, r := range runs {
		if r.brk {
			trimSpace()
			emit(ri, '\n')
			space = true
			continue
		}
		for _, ch := range r.text {
			if isCollapsibleSpace(ch) {
				if !space {
					emit(ri, ' ')
					space = true
				}
				continue
			}
			emit(ri, ch)
			space = false
		}
	}
	trimSpace()
	// 末字符下标可能因为裁掉了尾部空白而越界：收回到真实范围内
	for i := range runs {
		if b.last[i] >= len(b.runes) {
			b.last[i] = len(b.runes) - 1
		}
		if b.first[i] >= len(b.runes) {
			b.first[i], b.last[i] = -1, -1
		}
	}
	return b
}

// fragMono 报告一截文字是否按等宽字体画：行内代码里含汉字的部分回退成界面字体（等宽字体的汉字另有取整规则，见 monopara.go）。
func fragMono(r *proseRun, text string) bool { return r.mono && !hasWideRune(text) }

// runWidth 是一段文字（run 的一截）排成一行的宽度。
func (b *proseText) runWidth(ri int, text string) float32 {
	r := &b.runs[ri]
	switch {
	case text == "" || r.brk:
		return 0
	case fragMono(r, text):
		return b.canvas.TextWidthStyle(text, gfx.TextStyle{Size: r.size, Mono: true})
	case !r.italic && (r.weight == WeightMedium || r.weight == WeightSemibold):
		return b.look.NoKernWidth(b.canvas, text, r.size, r.weight) // 500 / 600 字重渲染层不做字偶距
	case r.italic:
		return b.canvas.TextWidthStyle(text, gfx.TextStyle{Size: r.size, Weight: r.weight, Italic: true})
	}
	return b.canvas.TextWidthWeight(text, r.size, r.weight)
}

// padded 报告这一段两端带内边距（行内代码块与站内链接小块）。
func (r *proseRun) padded() bool { return r.chip || r.linkChip }

func (b *proseText) chipPad(ri int) float32 {
	if b.runs[ri].linkChip {
		return proseLinkPadX
	}
	return proseChipPadX * b.runs[ri].size
}

// rangeWidth 是 runes[a:e] 的宽度：按所属的段分组量宽，行内代码块的两端加内边距。
func (b *proseText) rangeWidth(a, e int) float32 {
	var w float32
	for a < e {
		ri := b.runOf[a]
		z := a
		for z < e && b.runOf[z] == ri {
			z++
		}
		w += b.runWidth(ri, string(b.runes[a:z]))
		if b.runs[ri].padded() {
			if a == b.first[ri] {
				w += b.chipPad(ri)
			}
			if z-1 == b.last[ri] {
				w += b.chipPad(ri)
			}
		}
		a = z
	}
	return w
}

// buildLine 把 runes[a:e] 排成一行（行尾的空格悬挂，不占位、不画）。
func (b *proseText) buildLine(a, e int) proseLine {
	for e > a && b.runes[e-1] == ' ' {
		e--
	}
	var line proseLine
	var x float32
	for a < e {
		ri := b.runOf[a]
		z := a
		for z < e && b.runOf[z] == ri {
			z++
		}
		f := proseFrag{run: ri, text: string(b.runes[a:z])}
		chip := b.runs[ri].padded()
		if chip && a == b.first[ri] {
			f.chipL = true
			x += b.chipPad(ri)
		}
		f.x, f.w = x, b.runWidth(ri, f.text)
		x += f.w
		if chip && z-1 == b.last[ri] {
			f.chipR = true
			x += b.chipPad(ri)
		}
		line = append(line, f)
		a = z
	}
	return line
}

// linesAt 返回在 width 宽的盒子里折出的行；同一宽度再问时直接用上一次的结果。
func (b *proseText) linesAt(width float32) []proseLine {
	if b.filled && b.width == width {
		return b.lines
	}
	if width <= 0 {
		width = 1e9 // 宽度还不知道（首帧）：按一行排
	}
	const eps = 1e-3
	var lines []proseLine
	segStart := 0
	for segStart <= len(b.runes) {
		segEnd := segStart
		for segEnd < len(b.runes) && b.runes[segEnd] != '\n' {
			segEnd++
		}
		lines = append(lines, b.wrapSegment(segStart, segEnd, width, eps)...)
		segStart = segEnd + 1
	}
	b.lines, b.width, b.filled = lines, width, true
	return lines
}

// wrapSegment 把 runes[s0:s1]（不含硬换行）贪心折行：与 WrapText 同一套规则（unitEnds 的断行机会、行尾空格悬挂、
// 整个单元一行也放不下时逐字符硬断）。
func (b *proseText) wrapSegment(s0, s1 int, width, eps float32) []proseLine {
	var lines []proseLine
	lineStart, lineEnd := s0, s0
	var used float32
	flush := func() {
		lines = append(lines, b.buildLine(lineStart, lineEnd))
		lineStart, used = lineEnd, 0
	}
	ua := s0
	for _, rel := range unitEnds(b.runes[s0:s1]) {
		ub := s0 + rel
		for ua < ub {
			trim := ub
			for trim > ua && b.runes[trim-1] == ' ' {
				trim--
			}
			solid := b.rangeWidth(ua, trim)
			if used+solid <= width+eps {
				lineEnd = ub
				used += solid + b.rangeWidth(trim, ub)
				ua = ub
				break
			}
			if lineEnd > lineStart {
				flush() // 这一行前面还有内容：整个单元换到下一行再试
				continue
			}
			k := ua
			var acc float32
			for k < ub {
				adv := b.rangeWidth(k, k+1)
				if acc+adv > width+eps && k > ua {
					break
				}
				acc += adv
				k++
			}
			lineEnd = k
			flush()
			ua = k
		}
	}
	if lineEnd > lineStart || len(lines) == 0 { // 最后一行；整段刚好在硬断处收尾时不再多出一个空行
		flush()
	}
	return lines
}

func (b *proseText) MeasureWidth(*ui.LayoutContext) float32 {
	var w float32
	for _, ln := range b.linesAt(1e9) {
		if n := len(ln); n > 0 {
			w = max(w, ln[n-1].x+ln[n-1].w)
		}
	}
	b.filled = false // 这次是按一行排的试算，不留给真正的宽度用
	return w
}
func (b *proseText) PlaceWidth(*ui.LayoutContext, ui.Rect) {}
func (b *proseText) MeasureHeight(ctx *ui.LayoutContext) float32 {
	return float32(max(len(b.linesAt(ctx.ContentWidth())), 1)) * b.line
}
func (b *proseText) PlaceHeight(*ui.LayoutContext, ui.Rect) {}

// draw 画每一行的每一截：先铺行内代码的底色，再画字，再画下划线与删除线。
// 同一行里字号、字重、字体不同的几截要落在同一条基线上：基线行取段落基准字体的 ChromeBaselineRow，
// 每一截再按自己的字体算出与它的差（Viva 一侧的落笔位置由 BaselineShift 换成渲染层的基线）。
func (b *proseText) draw(d *ui.DrawContext) {
	l := b.look
	t := l.T
	scale := d.Canvas.PixelScale()
	if scale <= 0 {
		scale = 1
	}
	parentRow := float32(ChromeBaselineRow(b.weight, float64(b.size*scale), float64(b.line*scale)))
	for i, ln := range b.linesAt(d.Rect.Width) {
		top := d.Rect.Y + float32(i)*b.line
		baseline := top + parentRow/scale
		for fi, f := range ln {
			r := &b.runs[f.run]
			x := d.Rect.X + f.x
			var linkID gfx.ID
			if b.onLink != nil && r.href != "" {
				linkID = ui.ID(b.key.Key("lk" + strconv.Itoa(b.seq) + "." + strconv.Itoa(i) + "." + strconv.Itoa(fi)))
				b.linkHit(d, linkID, f, r, x, top)
			}
			if r.linkChip {
				b.paintLinkChip(d, f, r, x, baseline, scale, linkID)
			}
			if r.chip {
				pad := b.chipPad(f.run)
				a := float32(roundHalfUp(chromeMonoFace.ascent*float64(r.size*scale))) / scale
				dsc := float32(roundHalfUp(chromeMonoFace.descent*float64(r.size*scale))) / scale
				padY := proseChipPadY * r.size
				left, right := x, x+f.w
				if f.chipL {
					left -= pad
				}
				if f.chipR {
					right += pad
				}
				l.PaintSurfaceAt(d, gfx.Rect{X: left, Y: baseline - a - padY, Width: right - left, Height: a + dsc + 2*padY},
					Surface{Bg: t.Muted, Radius: proseChipRadius * r.size})
			}
			fill := d.Fade(l.Col(r.color))
			mono := fragMono(r, f.text)
			var y float32
			if mono {
				// 行内代码与前后文字共用一条基线（渲染层里同一行的行内盒按基线对齐），不是各自行盒的基线行
				y = top + (parentRow-float32(VivaBaselineRow(VivaLabelRows(b.line, scale), float64(r.size*scale))))/scale
			} else {
				own := float32(ChromeBaselineRow(r.weight, float64(r.size*scale), float64(b.line*scale)))
				y = top + (parentRow-own)/scale + BaselineShift(r.weight, r.size, b.line, scale)
			}
			style := gfx.TextStyle{Size: r.size, Weight: r.weight, Italic: r.italic, Color: fill, Mono: mono}
			d.Canvas.Label(f.text, gfx.Rect{X: x, Y: y, Width: f.w + 2*r.size, Height: b.line}, style)
			if r.underline {
				l.fillUnderline(d, top, x, f.w, b.size, b.line, b.weight, r.color)
			}
			if r.strike {
				row := float32(math.Floor(float64((baseline-0.3*r.size)*scale)+0.5)) / scale
				d.Canvas.Fill(gfx.Rect{X: x, Y: row, Width: f.w, Height: 1 / scale}, fill)
			}
		}
	}
}

// linkHit 登记一截链接文字的命中区（手形光标），并在点击到达时（事件遍之后）回调 onLink。
// 角色：普通链接是 link；站内链接（linkChip，渲染层 WikiMarkdown 的 [[双链]]）在渲染层里是 <button>，所以是 button。
func (b *proseText) linkHit(d *ui.DrawContext, id gfx.ID, f proseFrag, r *proseRun, x, top float32) {
	role := gfx.RoleLink
	if r.linkChip {
		role = gfx.RoleButton
	}
	// 站内链接小块的 px-0.5 在按钮盒里：命中区（与可访问外框）连两侧的内边距一起，折行的真正两端才有。
	left, right := x, x+f.w
	if r.linkChip {
		if f.chipL {
			left -= proseLinkPadX
		}
		if f.chipR {
			right += proseLinkPadX
		}
	}
	d.Canvas.HitWithOptions(id, gfx.Rect{X: left, Y: top, Width: right - left, Height: b.line}, f.text,
		gfx.HitOptions{Role: role, Cursor: gfx.PointerCursorHand})
	if d.Canvas.Clicked(id) {
		href, open := r.href, b.onLink
		d.Canvas.AfterPaint(func() { open(href) })
	}
}

// paintLinkChip 铺站内链接的底色小块（rounded-sm bg-primary/8 px-0.5，悬停 bg-primary/15）：高度取界面字体的上下伸展，
// 左右在折行的真正两端各多出 2px 内边距。
func (b *proseText) paintLinkChip(d *ui.DrawContext, f proseFrag, r *proseRun, x, baseline, scale float32, id gfx.ID) {
	face := faceOfWeight(r.weight)
	a := float32(roundHalfUp(face.ascent*float64(r.size*scale))) / scale
	dsc := float32(roundHalfUp(face.descent*float64(r.size*scale))) / scale
	left, right := x, x+f.w
	if f.chipL {
		left -= proseLinkPadX
	}
	if f.chipR {
		right += proseLinkPadX
	}
	alpha := float32(0.08)
	if !id.IsZero() && d.Canvas.Hovered() == id {
		alpha = 0.15
	}
	b.look.PaintSurfaceAt(d, gfx.Rect{X: left, Y: baseline - a, Width: right - left, Height: a + dsc},
		Surface{Bg: b.look.T.Primary.WithAlpha(alpha), Radius: 2})
}

// ---- 代码块 ----

// proseCode 排一块等宽代码：按渲染层 font-mono 的规则折行（WrapMono），每行一个 Label，行高固定。
type proseCode struct {
	look   *Look
	canvas *gfx.Canvas
	text   string
	size   float32
	line   float32

	lines  []string
	width  float32
	filled bool
}

func (b *proseCode) linesAt(width float32) []string {
	if !b.filled || b.width != width {
		b.lines, b.width, b.filled = WrapMono(b.text, width, b.size), width, true
	}
	return b.lines
}

func (b *proseCode) MeasureWidth(*ui.LayoutContext) float32 { return 0 }
func (b *proseCode) PlaceWidth(*ui.LayoutContext, ui.Rect)  {}
func (b *proseCode) MeasureHeight(ctx *ui.LayoutContext) float32 {
	return float32(max(len(b.linesAt(ctx.ContentWidth())), 1)) * b.line
}
func (b *proseCode) PlaceHeight(*ui.LayoutContext, ui.Rect) {}

// draw 画每一行：不含汉字的行用等宽字体，含汉字的行用界面字体按新宋体的字宽画（与 MonoParagraph 同一套）；颜色是 foreground/0.92。
func (b *proseCode) draw(d *ui.DrawContext) {
	l := b.look
	fill := d.Fade(l.Col(l.T.Foreground.WithAlpha(0.92)))
	scale := d.Canvas.PixelScale()
	if scale <= 0 {
		scale = 1
	}
	wideSize := monoWideAdvance(b.size)
	dy := MonoBaselineShift(b.size, wideSize, b.line, scale)
	for i, text := range b.linesAt(d.Rect.Width) {
		if text == "" {
			continue
		}
		top := d.Rect.Y + float32(i)*b.line
		if hasWideRune(text) {
			w := d.Canvas.TextWidthWeight(text, wideSize, WeightNormal)
			d.Canvas.Label(text, gfx.Rect{X: d.Rect.X, Y: top + dy, Width: w + wideSize, Height: b.line}, gfx.TextStyle{Size: wideSize, Weight: WeightNormal, Color: fill})
			continue
		}
		w := d.Canvas.TextWidthStyle(text, gfx.TextStyle{Size: b.size, Mono: true})
		d.Canvas.Label(text, gfx.Rect{X: d.Rect.X, Y: top, Width: w + b.size, Height: b.line}, gfx.TextStyle{Size: b.size, Color: fill, Mono: true})
	}
}
