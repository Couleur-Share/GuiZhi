package kit

import (
	"math"
	"strings"
	"unicode"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 搜索命中词的高亮（台账 B-041）
//
// 渲染层（highlight-text.tsx 的 highlightText）把搜索词在文字里的每一处命中（大小写不敏感）包进
// `<mark class="rounded-sm bg-amber-400/90 px-0.5 text-amber-950">`：行内盒，琥珀底、深琥珀字、两侧各 0.125rem 的内边距、
// 圆角 calc(var(--radius) - 4px)。Viva 的文字是整段绘制，没有行内盒，所以这里自己排：把整段文字按命中切成若干段，
// 每段在每一行里画成一个文字片段（一个 Label，对应渲染层「一个文本节点的一个行片段」），命中的片段先在文字下面铺底色。
//
// 2026-11-20 在 React 参照里量到的规则（scripts/parity/tools/probe-highlight.mjs）：
//   - 底色矩形 = 文字内容区（高 = A + D，与 ChromeContentRows 同一把尺子）向左右各延伸一份内边距；上下不延伸。
//     行高不受影响（mark 的字体与行高继承自宿主）；内边距占位，后面的文字顺延；折行时把内边距算进断点的宽度。
//   - box-decoration-break 是 slice：命中跨行时，内边距与圆角只出现在第一片的左侧与最后一片的右侧，断开处是平的。
//   - 命中词里的文字按宿主的字号字重排，字色是 amber-950，不跟随宿主（选中卡片的反色也不影响它）；悬停下划线仍是宿主的颜色，
//     每个文字片段各一条，内边距处不画（所以命中词两侧会各留一个 2px 的缺口）。
//   - 省略号（text-overflow / line-clamp）按整行的内容排：取最长的前缀，使「前缀文字的右缘 + 省略号」放得下——前缀末尾的命中词
//     若恰好收在这里，它的右内边距不算（省略号从文字的右缘起画，压在内边距上）。省略号永远是宿主的字色与字重的普通文字，
//     落在命中词里面或紧挨命中词时也不是 amber-950。
//   - 被省略号盖掉的文字不画，但行内盒还在原位置：每个命中词的底色按「没有省略号」的版面照画，被截掉的命中词也有底色
//     （2026-11-20 的 probe-mark-ellipsis 在 React 里逐像素量过），超出盒子右缘的部分被 overflow 裁掉、裁处是平的。
//
// 空白按 CSS 的 white-space: normal 折叠（newMarkModel，与 TextNoKern 的 collapseSpaces 同一规则：只有空格、制表符、换行、回车折叠）。
//
// 没有照做的（登记在台账 B-041）：省略号落在命中词里或紧挨命中词时，渲染层的文字节点（可访问树与采集到的文字）把省略号算在
// 命中词的文本节点里，这里省略号是单独的一片文字（颜色不同，画在一个 Label 里做不到），整行文字的片段数因此比渲染层多一片。

// MarkSpan 是命中词在原文里的一段（字节下标，左闭右开）。
type MarkSpan struct{ Start, End int }

// MarkSpans 复刻 highlightText 的匹配：命中词先去首尾空白；在原文里逐字符按小写比较（大小写不敏感），
// 从左到右、互不重叠；命中词或原文为空时没有命中。与 JavaScript 的 toLowerCase 相比只差特殊的大小写映射（İ、词尾的 Σ），
// 这里取每个字符的简单小写映射。
func MarkSpans(text, query string) []MarkSpan {
	needle := lowerRunes([]rune(strings.TrimSpace(query)))
	if len(needle) == 0 || text == "" {
		return nil
	}
	var runes []rune
	var offsets []int
	for i, r := range text {
		runes = append(runes, unicode.ToLower(r))
		offsets = append(offsets, i)
	}
	offsets = append(offsets, len(text))
	var spans []MarkSpan
	for i := 0; i+len(needle) <= len(runes); {
		if runesEqual(runes[i:i+len(needle)], needle) {
			spans = append(spans, MarkSpan{Start: offsets[i], End: offsets[i+len(needle)]})
			i += len(needle)
			continue
		}
		i++
	}
	return spans
}

func lowerRunes(rs []rune) []rune {
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HasMarks 报告 text 里有没有命中：没有命中时调用方照旧用普通文字（TextNoKern 或 Text），不必走高亮的排版。
func HasMarks(text, query string) bool { return len(MarkSpans(text, query)) > 0 }

// MarkOptions 是 TextMarked 的可选项。
type MarkOptions struct {
	// MaxLines 至多排几行，放不下时最后一行以省略号收尾（line-clamp-N）；不大于 0 表示不限（整段折行）。
	MaxLines int
	// Truncate 单行不折行，放不下时以省略号收尾（truncate：white-space: nowrap + text-overflow: ellipsis）；优先于 MaxLines。
	Truncate bool
	// Underline 给每个文字片段画下划线（hover:underline）：颜色同宿主文字，命中词里的片段也是宿主的颜色，内边距处不画。
	Underline bool
}

// 命中词的样式（渲染层 mark 的类名）。
var (
	markBg = tokens.Tailwind("amber-400").WithAlpha(0.9)
	markFg = tokens.Tailwind("amber-950")
)

// markModel 是整段文字按命中切开、折叠空白之后的样子：full 是折叠后的整段文字，各段在 full 里占 [starts[i], ends[i])。
type markModel struct {
	full   string
	starts []int
	ends   []int
	marked []bool
}

// newMarkModel 把 text 按 spans 切成交替的普通段与命中段，并按 white-space: normal 折叠空白（跨段也折叠；首尾的空白去掉）。
// 折叠后留下的那个空格属于空白开始的那一段。空了的段丢掉。
func newMarkModel(text string, spans []MarkSpan) *markModel {
	type item struct {
		r   rune
		run int
	}
	var items []item
	var runMarked []bool
	pos := 0
	addRun := func(s string, marked bool) {
		idx := len(runMarked)
		runMarked = append(runMarked, marked)
		for _, r := range s {
			if isCollapsibleSpace(r) {
				if len(items) > 0 && items[len(items)-1].r != ' ' {
					items = append(items, item{' ', idx})
				}
				continue
			}
			items = append(items, item{r, idx})
		}
	}
	for _, sp := range spans {
		if sp.Start > pos {
			addRun(text[pos:sp.Start], false)
		}
		addRun(text[sp.Start:sp.End], true)
		pos = sp.End
	}
	if pos < len(text) {
		addRun(text[pos:], false)
	}
	for len(items) > 0 && items[len(items)-1].r == ' ' {
		items = items[:len(items)-1]
	}
	m := &markModel{}
	var b strings.Builder
	for i := 0; i < len(items); {
		j := i
		for j < len(items) && items[j].run == items[i].run {
			j++
		}
		start := b.Len()
		for _, it := range items[i:j] {
			b.WriteRune(it.r)
		}
		m.starts = append(m.starts, start)
		m.ends = append(m.ends, b.Len())
		m.marked = append(m.marked, runMarked[items[i].run])
		i = j
	}
	m.full = b.String()
	return m
}

// markFrag 是一行里的一个文字片段（对应渲染层的一个文本节点在这一行的片段）。
type markFrag struct {
	text   string  // 画的文字：被省略号截断的普通文字片段是「前缀 + …」
	marked bool    // 命中的片段：文字用 markFg（底色见 markBox）
	x, w   float32 // 文字的左缘（相对盒的左缘，不含内边距）与排版宽度
	pl, pr float32 // 命中片段两侧的内边距（占位）；断开处是 0
}

// markBox 是一个命中片段的底色矩形（相对盒的左缘，含两侧内边距）；圆角只在这一片是命中词的起点、终点时才有（slice）。
type markBox struct {
	x0, x1         float32
	roundL, roundR bool
}

// markResult 是一次排版的结果：lines 是每行要画的文字片段，boxes 是每行要铺的底色（两者不一一对应：
// 被省略号截掉的命中词没有文字、底色还在）；natural 是整段排成一行的宽度（含内边距，不含截断）。
type markResult struct {
	lines   [][]markFrag
	boxes   [][]markBox
	natural float32
}

// markKey 是一次排版结果的缓存键：同一段文字、命中词、字号字重、宽度、内边距与行数设置，排出的结果相同。
type markKey struct {
	text, query         string
	size, weight, width float32
	pad                 float32
	maxLines            int
	truncate            bool
}

// markBuilder 是一次排版用的度量与规则。
type markBuilder struct {
	look   *Look
	canvas *gfx.Canvas
	m      *markModel
	size   float32
	weight float32
	pad    float32
}

// measure 是一段不含内边距的文字排成一行的宽度：500 及以上字重不做字偶距（逐字符求和，台账 A-026），常规字重量整行（带字偶距）。
func (b *markBuilder) measure(s string) float32 {
	if b.weight >= WeightMedium-0.01 {
		return b.look.NoKernWidth(b.canvas, s, b.size, b.weight)
	}
	return b.canvas.TextWidthWeight(s, b.size, b.weight)
}

// width 是 full[a:z] 排成一行占的宽度：各段的文字宽度之和，加上范围里含起点的命中段的左内边距、含终点的命中段的右内边距。
func (b *markBuilder) width(a, z int) float32 {
	var w float32
	for i := range b.m.starts {
		rs, re := b.m.starts[i], b.m.ends[i]
		lo, hi := max(a, rs), min(z, re)
		if lo >= hi {
			continue
		}
		w += b.measure(b.m.full[lo:hi])
		if b.m.marked[i] {
			if a <= rs {
				w += b.pad
			}
			if z >= re {
				w += b.pad
			}
		}
	}
	return w
}

// markUnit 是一个断行单元（词加它后面的空格）；solid 是去掉行尾空格的宽度（放不放得下看它），full 含空格（放下之后占去的行宽）。
type markUnit struct {
	a, z        int
	solid, full float32
}

func (b *markBuilder) unit(a, z int) markUnit {
	t := z
	for t > a && b.m.full[t-1] == ' ' {
		t--
	}
	return markUnit{a: a, z: z, solid: b.width(a, t), full: b.width(a, z)}
}

// wrap 把整段文字折成若干行（字节范围，行尾的空格悬挂在范围里）。规则同 wrapLine：断行机会取自 splitUnits，
// 前面还有机会就整段换行，一行里放不下一个单元时才在字符之间断开（break-word）。
func (b *markBuilder) wrap(width float32) [][2]int {
	parts := splitUnits(b.m.full, func(string) float32 { return 0 })
	units := make([]markUnit, 0, len(parts))
	pos := 0
	for _, p := range parts {
		units = append(units, b.unit(pos, pos+len(p.text)))
		pos += len(p.text)
	}
	var (
		lines     [][2]int
		lineStart int
		cur       int
		used      float32
	)
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case used+u.solid <= width+1e-3:
			used += u.full
			cur = u.z
		case cur > lineStart:
			lines = append(lines, [2]int{lineStart, cur})
			lineStart, used = cur, 0
			i--
		default:
			k := b.fit(u.a, u.z, width)
			lines = append(lines, [2]int{lineStart, k})
			lineStart, cur, used = k, k, 0
			if k < u.z {
				units[i] = b.unit(k, u.z)
				i--
			}
		}
	}
	if cur > lineStart || len(lines) == 0 {
		lines = append(lines, [2]int{lineStart, cur})
	}
	return lines
}

// fit 取 full[a:z] 开头尽量多、宽度不超过 width 的字符（至少一个，不劈开字素）：整段放不下一个空行时的断开点。
func (b *markBuilder) fit(a, z int, width float32) int {
	k := nextBoundary(b.m.full, a)
	for k < z {
		next := nextBoundary(b.m.full, k)
		if next > z || b.width(a, next) > width+1e-3 {
			break
		}
		k = next
	}
	return min(k, z)
}

// cut 在 [a,z) 这一行里找省略号的位置：最长的前缀 [a,k)，使「前缀文字的右缘 + 省略号宽度」不超过 avail。
// 前缀里的命中词整段在内时左右内边距都算（后面的文字顺延）；但前缀恰好收在命中词末尾时，它的右内边距不算——
// 省略号从文字的右缘起画，压在内边距上。k == z 表示整行加省略号也放得下，没有字符被截掉。
func (b *markBuilder) cut(a, z int, avail float32) int {
	ell := b.measure("\u2026")
	k := a
	for k < z {
		next := nextBoundary(b.m.full, k)
		if next > z || b.width(a, next)-b.trailingPad(a, next)+ell > avail+1e-3 {
			break
		}
		k = next
	}
	return k
}

// trailingPad 是 full[a:z] 末尾恰好收在 z 的那个命中词的右内边距，没有则为 0（width 把它算进去了，省略号的位置要扣掉）。
func (b *markBuilder) trailingPad(a, z int) float32 {
	for i := range b.m.starts {
		if b.m.marked[i] && b.m.ends[i] == z && max(a, b.m.starts[i]) < z {
			return b.pad
		}
	}
	return 0
}

// place 给一行片段排位置（相对盒的左缘）：文字挨着排，命中片段两侧让出内边距，后面的文字顺延。
func (b *markBuilder) place(row []markFrag) []markFrag {
	var x float32
	for i := range row {
		f := &row[i]
		f.x = x + f.pl
		f.w = b.measure(f.text)
		x = f.x + f.w + f.pr
	}
	return row
}

// boxesOf 是一行片段里各命中片段的底色矩形。
func boxesOf(row []markFrag) []markBox {
	var out []markBox
	for _, f := range row {
		if f.marked {
			out = append(out, markBox{x0: f.x - f.pl, x1: f.x + f.w + f.pr, roundL: f.pl > 0, roundR: f.pr > 0})
		}
	}
	return out
}

// clipBoxes 把底色矩形裁到盒宽之内（overflow: hidden）：整个在盒外的丢掉，伸出右缘的在右缘处裁平。
func clipBoxes(boxes []markBox, width float32) []markBox {
	out := boxes[:0]
	for _, bx := range boxes {
		if bx.x0 >= width-1e-3 {
			continue
		}
		if bx.x1 > width+1e-3 {
			bx.x1, bx.roundR = width, false
		}
		out = append(out, bx)
	}
	return out
}

// frags 把 full[a:z] 切成各段在这一行里的文字片段（不含位置）；z 已经去掉行尾空格。
func (b *markBuilder) frags(a, z int) []markFrag {
	var out []markFrag
	for i := range b.m.starts {
		rs, re := b.m.starts[i], b.m.ends[i]
		lo, hi := max(a, rs), min(z, re)
		if lo >= hi {
			continue
		}
		f := markFrag{text: b.m.full[lo:hi], marked: b.m.marked[i]}
		if f.marked {
			if a <= rs {
				f.pl = b.pad
			}
			if z >= re {
				f.pr = b.pad
			}
		}
		out = append(out, f)
	}
	return out
}

// runAt 是字节位置 k 所在的段。
func (b *markBuilder) runAt(k int) int {
	for i := range b.m.starts {
		if k >= b.m.starts[i] && k < b.m.ends[i] {
			return i
		}
	}
	return len(b.m.starts) - 1
}

// truncated 是 [a,z) 这一行在 width 宽的盒子里以省略号收尾之后的样子：要画的文字片段与要铺的底色。
func (b *markBuilder) truncated(a, z int, width float32) ([]markFrag, []markBox) {
	const ellipsis = "\u2026"
	// 底色按没有省略号的整行来排：被省略号盖掉的文字不画，行内盒与它的底色还在原位置，伸出盒子右缘的被裁掉。
	boxes := clipBoxes(boxesOf(b.place(b.frags(a, z))), width)
	k := b.cut(a, z, width)
	kept := b.place(b.frags(a, k))
	var edge float32 // 前缀文字的右缘（不含末尾命中词的右内边距）：省略号从这里起画
	if n := len(kept); n > 0 {
		edge = kept[n-1].x + kept[n-1].w
	}
	// 被截掉的第一个字符与前缀的最后一个字符同属一段普通文字：省略号接在这一段里（同一个文字片段，宿主的字色）。
	if n := len(kept); n > 0 && !kept[n-1].marked && b.runAt(k) == b.runAt(k-1) {
		kept[n-1].text += ellipsis
		kept[n-1].w = b.measure(kept[n-1].text)
		return kept, boxes
	}
	// 其余情形省略号单独成片：落在命中词里或紧挨命中词时它也是宿主的字色，不是 amber-950。
	return append(kept, markFrag{text: ellipsis, x: edge, w: b.measure(ellipsis)}), boxes
}

// layout 排整段文字：lines 是各行要画的文字片段、boxes 是各行要铺的底色，位置已按行内累加算好。
func (b *markBuilder) layout(width float32, maxLines int, truncate bool) *markResult {
	full := b.m.full
	res := &markResult{natural: b.width(0, len(full))}
	add := func(row []markFrag, boxes []markBox) {
		res.lines = append(res.lines, row)
		res.boxes = append(res.boxes, boxes)
	}
	whole := func(a, z int) {
		row := b.place(b.frags(a, z))
		add(row, boxesOf(row))
	}
	switch {
	case truncate:
		if res.natural > width+1e-3 {
			add(b.truncated(0, len(full), width))
		} else {
			whole(0, len(full))
		}
	default:
		lines := b.wrap(width)
		clamped := maxLines > 0 && len(lines) > maxLines
		if clamped {
			lines = lines[:maxLines]
		}
		for i, ln := range lines {
			a, z := ln[0], ln[1]
			for z > a && full[z-1] == ' ' {
				z--
			}
			if clamped && i == len(lines)-1 {
				add(b.truncated(a, z, width))
				continue
			}
			whole(a, z)
		}
	}
	return res
}

// modelKey 是 markModelOf 的缓存键：命中的结果只取决于原文与（去过首尾空白的）命中词。
type modelKey struct{ text, query string }

// markModelOf 返回 s 在 query（已去首尾空白）下的命中模型，没有命中时返回 nil。
// 列表每帧都要把可见的每一行（卡片与表格各两处文字）问一遍，而命中与否在文字与命中词不变时不会变，
// 所以连「没有命中」的结果一起缓存，稳态帧里不再逐字扫描、不再分配（与折行缓存同一个字体代、同一个清空规则）。
func (l *Look) markModelOf(canvas *gfx.Canvas, s, query string) *markModel {
	cache := l.noKern(canvas)
	key := modelKey{text: s, query: query}
	if model, ok := cache.models[key]; ok {
		return model
	}
	var model *markModel
	if spans := MarkSpans(s, query); len(spans) > 0 {
		model = newMarkModel(s, spans)
	}
	if len(cache.models) >= noKernLinesMax {
		clear(cache.models)
	}
	cache.models[key] = model
	return model
}

// markLayout 是 TextMarked 的版面：宽度由外层给，高度随折出的行数增长；同一宽度只排一次。
type markLayout struct {
	look     *Look
	canvas   *gfx.Canvas
	text     string
	query    string
	model    *markModel
	size     float32
	weight   float32
	line     float32
	pad      float32
	maxLines int
	truncate bool

	res   *markResult
	width float32
}

func (b *markLayout) resultAt(width float32) *markResult {
	if b.res != nil && b.width == width {
		return b.res
	}
	cache := b.look.noKern(b.canvas)
	key := markKey{text: b.text, query: b.query, size: b.size, weight: b.weight, width: width, pad: b.pad, maxLines: b.maxLines, truncate: b.truncate}
	res, ok := cache.marked[key]
	if !ok {
		builder := &markBuilder{look: b.look, canvas: b.canvas, m: b.model, size: b.size, weight: b.weight, pad: b.pad}
		res = builder.layout(width, b.maxLines, b.truncate)
		if len(cache.marked) >= noKernLinesMax {
			clear(cache.marked)
		}
		cache.marked[key] = res
	}
	b.res, b.width = res, width
	return res
}

func (b *markLayout) MeasureWidth(ctx *ui.LayoutContext) float32 {
	return b.resultAt(ctx.ContentWidth()).natural
}
func (b *markLayout) PlaceWidth(*ui.LayoutContext, ui.Rect) {}
func (b *markLayout) MeasureHeight(ctx *ui.LayoutContext) float32 {
	return float32(max(len(b.resultAt(ctx.ContentWidth()).lines), 1)) * b.line
}
func (b *markLayout) PlaceHeight(*ui.LayoutContext, ui.Rect) {}

// TextMarked 是带命中词高亮的文字：s 里 query 的每一处命中画琥珀底。size、line 是逻辑像素；返回的盒子占满可用宽度，
// 自然宽度是整段排成一行的宽度（含命中词的内边距），高度是行数乘行高。没有命中时返回 false 的第二个结果，调用方改用普通文字。
func (l *Look) TextMarked(c ui.C, s, query string, size, line, weight float32, color tokens.Color, opt MarkOptions) (ui.Box, bool) {
	canvas := c.Canvas()
	query = strings.TrimSpace(query)
	model := l.markModelOf(canvas, s, query)
	if model == nil {
		return ui.Box{}, false
	}
	layout := &markLayout{
		look: l, canvas: canvas, text: s, query: query, model: model,
		size: size, weight: weight, line: line, pad: l.Space(0.5), maxLines: opt.MaxLines, truncate: opt.Truncate,
	}
	dy := BaselineShift(weight, size, line, l.Scale)
	radius := l.Rem(0.75) - 4 // rounded-sm：calc(var(--radius) - 4px)
	box := ui.CustomLayout(c, layout).FillWidth().Draw(func(d *ui.DrawContext) {
		res := layout.resultAt(d.Rect.Width)
		hostFill := d.Fade(l.Col(color))
		markFill := d.Fade(l.Col(markFg))
		for i, row := range res.lines {
			top := d.Rect.Y + float32(i)*line
			// 这一行的底色全部铺完再画文字：省略号压在底色上。
			for _, bx := range res.boxes[i] {
				l.paintMarkBg(d, top, d.Rect.X+bx.x0, bx.x1-bx.x0, size, line, weight, radius, bx.roundL, bx.roundR)
			}
			for _, f := range row {
				x := d.Rect.X + f.x
				fill := hostFill
				if f.marked {
					fill = markFill
				}
				style := gfx.TextStyle{Size: size, Weight: weight, Color: fill}
				d.Canvas.Label(f.text, gfx.Rect{X: x, Y: top + dy, Width: max(f.w+2, 1), Height: line}, style)
				if opt.Underline && f.text != "" {
					l.fillUnderline(d, top, x, f.w, size, line, weight, color)
				}
			}
		}
	})
	return box, true
}

// paintMarkBg 画命中片段的底色：矩形是这一行文字的内容区（高 A + D，顶在行盒顶之下半行距处）向左右延伸内边距；
// 左右两侧各自在这一片是命中词的起点、终点时才有圆角（box-decoration-break: slice）。
func (l *Look) paintMarkBg(d *ui.DrawContext, lineTop, x, w, size, line, weight, radius float32, roundLeft, roundRight bool) {
	scale := l.Scale
	if scale <= 0 {
		scale = 1
	}
	sizeDev, lineDev := float64(size)*float64(scale), float64(line)*float64(scale)
	ascent, descent := ChromeContentRows(weight, sizeDev)
	top := math.Floor(float64(lineTop)*float64(scale)+0.5) + math.Floor((lineDev-float64(ascent+descent))/2)
	var rl, rr float32
	if roundLeft {
		rl = radius
	}
	if roundRight {
		rr = radius
	}
	rect := gfx.Rect{X: x, Y: float32(top / float64(scale)), Width: w, Height: float32(ascent+descent) / scale}
	snapped := SnapRect(rect, d.Canvas.PixelScale())
	if snapped.Width <= 0 || snapped.Height <= 0 {
		return
	}
	d.Canvas.DrawIcon(l.markShape(snapped.Width, snapped.Height, rl, rr), snapped, d.Fade(l.Col(markBg)))
}

// markShapeKey 是命中底色形状的缓存键：吸附后的宽高与左右两侧的圆角半径（逻辑像素）。
type markShapeKey struct{ w, h, rl, rr float32 }

// markShapeMax 是形状缓存的条数上限：同一次搜索里命中词的宽度只有几种，超过就整体清空重来。
const markShapeMax = 256

// markShape 返回宽 w、高 h、左侧圆角 rl、右侧圆角 rr 的圆角矩形，用填充图标（Stroke 为 0）表达。
// 与 PaintRoundedCorners 同一条路径（四段三次曲线逼近圆弧，半径不超过半边长），区别只在谁出三角形：
// 直接 DrawPath 每帧都要把曲线展平、耳切三角化（开着搜索的 100 行表格上，每个命中片段每帧约 10 μs，占稳态帧的一成）；
// Viva 对 Icon 的三角化按图标缓存，之后每帧只做平移与着色，得到的三角形与直接画路径逐个相同，所以像素不变。
func (l *Look) markShape(w, h, rl, rr float32) gfx.Icon {
	key := markShapeKey{w, h, rl, rr}
	if icon, ok := l.markShapes[key]; ok {
		return icon
	}
	limit := min(w, h) / 2
	rl, rr = clampRadius(rl, limit), clampRadius(rr, limit)
	const k = arcKappa
	icon := gfx.Icon{ViewBox: w, ViewHeight: h, Commands: []gfx.PathCommand{
		gfx.MoveTo(rl, 0),
		gfx.LineTo(w-rr, 0),
		gfx.CubicTo(w-rr+k*rr, 0, w, rr-k*rr, w, rr),
		gfx.LineTo(w, h-rr),
		gfx.CubicTo(w, h-rr+k*rr, w-rr+k*rr, h, w-rr, h),
		gfx.LineTo(rl, h),
		gfx.CubicTo(rl-k*rl, h, 0, h-rl+k*rl, 0, h-rl),
		gfx.LineTo(0, rl),
		gfx.CubicTo(0, rl-k*rl, rl-k*rl, 0, rl, 0),
		gfx.ClosePath(),
	}}
	if l.markShapes == nil || len(l.markShapes) >= markShapeMax {
		l.markShapes = make(map[markShapeKey]gfx.Icon, 16)
	}
	l.markShapes[key] = icon
	return icon
}
