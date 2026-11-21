package kit

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 不做字偶距的文字（台账 A-026 / B-044）
//
// Chromium 对 500 / 600 字重的文字不做字偶距（font-kerning 取 auto 与 none 的宽度逐位相同），Viva 对这两档做：
// 带拉丁字母的整行比渲染层窄几个像素（「技术论坛：SQLite WAL 模式的备份坑」在 12.25 px 下窄 1.69 px）。
// 对「宽度由文字内容决定」的元素（知识库表格的标题钮：钮宽 = 文字宽），这几个像素让钮的外框与后面的图标位置偏移；
// 可用宽度恰落在两种宽度之间时还会折行翻转（渲染层折成两行、Viva 一行放得下），其后每一行整体偏移一个行高。
//
// 所以这类文字的**占位与折行**自己算，字形仍交给 Viva 画：
//   - 宽度 = 逐字符前进量之和。单个字符没有字偶对，它的宽度就是前进量；渲染层不做字偶距，整行宽度正是这个和
//     （2026-11-19 在 React 参照里量到的钮宽 206.78 / 253.89 / 234.06 与此一致，见 textnokern_test.go）；
//   - 折行用 WrapText（Chromium 的断行机会：空白之后、连字符之后、汉字之间，避头尾，整段放不下才逐字符断开），
//     度量取上面的宽度；至多 maxLines 行，放不下时最后一行以省略号收尾（Tailwind 的 line-clamp-N）；
//   - 画出来的字形仍是 Viva 带字偶距的字形，比占位窄一点：一行里拉丁字母之后的字形最多比渲染层靠左几个像素，
//     这部分仍由 A-026 的登记承担。

// NoKernOptions 是 TextNoKern 的可选项。
type NoKernOptions struct {
	// MaxLines 至多排几行，放不下时最后一行以省略号收尾（line-clamp-N）；不大于 0 表示不限（整段折行）。
	MaxLines int
	// Underline 给每一行画下划线（hover:underline）：位置与粗细同 Underline，横向是这一行文字的占位宽度。
	Underline bool
}

// advanceKey 是一个字符在某字号字重下的前进量的缓存键。
type advanceKey struct {
	size, weight float32
	r            rune
}

// noKernKey 是一次折行结果的缓存键：同一段文字、同一字号字重、同一宽度、同一行数上限，折出的行相同。
type noKernKey struct {
	text                string
	size, weight, width float32
	maxLines            int
}

// noKernLinesMax 是折行结果缓存的条数上限：超过就整体清空重来（可见的标题只有几十条，滚过一万条库才会碰到）。
const noKernLinesMax = 1024

// noKernCache 记住 TextNoKern 的度量：单个字符的前进量（逐字符求和用）与折行结果（同一批标题每帧都要排，稳态下零分配）。
// 字体换代（FontGeneration 变了：换字体、字体回退文件载入）之后全部作废重建。Look 只在界面线程上用，不加锁。
type noKernCache struct {
	generation uint32
	advance    map[advanceKey]float32
	lines      map[noKernKey][]string
	marked     map[markKey]*markResult // 带命中词高亮的文字（marktext.go）的排版结果
	models     map[modelKey]*markModel // 同上：（原文，命中词）→ 命中模型，没有命中为 nil
	pre        preCache                // 保留换行的文字（prewrap.go，文字稿）最近一次的排版结果
}

// noKern 返回当前字体代的缓存；字体换代后先清空。
func (l *Look) noKern(canvas *gfx.Canvas) *noKernCache {
	cache := &l.advances
	if generation := canvas.FontGeneration(); cache.advance == nil || cache.generation != generation {
		cache.advance, cache.lines, cache.generation = make(map[advanceKey]float32, 256), make(map[noKernKey][]string, 64), generation
		cache.marked = make(map[markKey]*markResult, 16)
		cache.models = make(map[modelKey]*markModel, 16)
		cache.pre = preCache{}
	}
	return cache
}

// runeAdvance 是字符 r 在 size、weight 下的前进量（单字符串没有字偶对）。
func (l *Look) runeAdvance(canvas *gfx.Canvas, r rune, size, weight float32) float32 {
	cache := l.noKern(canvas)
	key := advanceKey{size: size, weight: weight, r: r}
	w, ok := cache.advance[key]
	if !ok {
		w = canvas.TextWidthWeight(string(r), size, weight)
		cache.advance[key] = w
	}
	return w
}

// NoKernWidth 是 s 排成一行在 Chromium 里的宽度：500 / 600 字重不做字偶距，等于逐字符前进量之和。
func (l *Look) NoKernWidth(canvas *gfx.Canvas, s string, size, weight float32) float32 {
	var w float32
	for _, r := range s {
		w += l.runeAdvance(canvas, r, size, weight)
	}
	return w
}

// isCollapsibleSpace 报告 r 在 white-space: normal 下会不会被折叠。Chromium 实测（scripts/parity/tools/probe-collapse.mjs，
// 14px、「a」「b」之间放一个或两个同样的字符）只有空格、制表符、换行、回车折成一个空格；不换行空格 U+00A0、全角空格 U+3000、
// 各种宽度的空格（U+2000 起）、换页、竖制表符、U+0085、行分隔符与段分隔符都原样保留（各占各自的宽度）。
// 所以不能用 unicode.IsSpace：它把后面这些也算空白，全角空格会被折成窄得多的半角空格。
func isCollapsibleSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// collapseSpaces 按 CSS 的 white-space: normal 折叠空白：连续的空格、制表符、换行、回车变成一个空格，首尾的空白去掉。
func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if isCollapsibleSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// noKernLayout 排一段不做字偶距的文字：宽度由外层给，高度随折出的行数增长。折出的行存在这里，画的时候取用。
// Viva 的自定义版面每帧先量高度、再画；两处用同一宽度时只折一次。
type noKernLayout struct {
	look     *Look
	canvas   *gfx.Canvas
	text     string // 已折叠空白
	size     float32
	weight   float32
	line     float32
	maxLines int

	lines  []string
	width  float32
	filled bool
}

func (b *noKernLayout) measure(s string) float32 {
	return b.look.NoKernWidth(b.canvas, s, b.size, b.weight)
}

// linesAt 返回文字在 width 宽的盒子里折出的行（至多 maxLines 行）；同一宽度再问时直接用上一次的结果。
func (b *noKernLayout) linesAt(width float32) []string {
	if b.filled && b.width == width {
		return b.lines
	}
	cache := b.look.noKern(b.canvas)
	key := noKernKey{text: b.text, size: b.size, weight: b.weight, width: width, maxLines: b.maxLines}
	lines, ok := cache.lines[key]
	if !ok {
		lines = WrapText(b.text, width, b.measure)
		if b.maxLines > 0 && len(lines) > b.maxLines {
			lines = lines[:b.maxLines]
			lines[b.maxLines-1] = b.ellipsize(lines[b.maxLines-1], width)
		}
		if len(cache.lines) >= noKernLinesMax {
			clear(cache.lines)
		}
		cache.lines[key] = lines
	}
	b.lines, b.width, b.filled = lines, width, true
	return b.lines
}

// ellipsize 让被截断的最后一行以省略号收尾：保留尽量多的字符，使它们加上省略号不宽于 width；不在一个字符内部切开。
func (b *noKernLayout) ellipsize(line string, width float32) string {
	const ellipsis = "\u2026"
	line = strings.TrimRight(line, " ")
	ell := b.measure(ellipsis)
	if b.measure(line)+ell <= width+1e-3 {
		return line + ellipsis
	}
	room := width - ell
	var used float32
	cut := 0
	for cut < len(line) {
		next := nextBoundary(line, cut)
		w := b.measure(line[cut:next])
		if used+w > room+1e-3 {
			break
		}
		used += w
		cut = next
	}
	return line[:cut] + ellipsis
}

// MeasureWidth 是整段排成一行的宽度（元素按内容定宽时用它；外层放不下时由 flex 收缩）。
func (b *noKernLayout) MeasureWidth(*ui.LayoutContext) float32 { return b.measure(b.text) }
func (b *noKernLayout) PlaceWidth(*ui.LayoutContext, ui.Rect)  {}
func (b *noKernLayout) MeasureHeight(ctx *ui.LayoutContext) float32 {
	return float32(max(len(b.linesAt(ctx.ContentWidth())), 1)) * b.line
}
func (b *noKernLayout) PlaceHeight(*ui.LayoutContext, ui.Rect) {}

// TextNoKern 是按渲染层「不做字偶距」的宽度占位、折行的文字（500 / 600 字重，台账 A-026 / B-044）。
// size、line 是逻辑像素；返回的盒子占满可用宽度，自然宽度（按内容定宽的父盒用它）是整段排成一行的宽度，
// 高度是行数乘行高。每一行按 Text 的基线位置画（见 baseline.go）。
func (l *Look) TextNoKern(c ui.C, s string, size, line, weight float32, color tokens.Color, opt NoKernOptions) ui.Box {
	layout := &noKernLayout{look: l, canvas: c.Canvas(), text: collapseSpaces(s), size: size, weight: weight, line: line, maxLines: opt.MaxLines}
	dy := BaselineShift(weight, size, line, l.Scale)
	return ui.CustomLayout(c, layout).FillWidth().Draw(func(d *ui.DrawContext) {
		fill := d.Fade(l.Col(color))
		style := gfx.TextStyle{Size: size, Weight: weight, Color: fill}
		for i, text := range layout.linesAt(d.Rect.Width) {
			text = strings.TrimRight(text, " ")
			top := d.Rect.Y + float32(i)*line
			d.Canvas.Label(text, gfx.Rect{X: d.Rect.X, Y: top + dy, Width: d.Rect.Width, Height: line}, style)
			if opt.Underline && text != "" {
				l.fillUnderline(d, top, d.Rect.X, min(layout.measure(text), d.Rect.Width), size, line, weight, color)
			}
		}
	})
}
