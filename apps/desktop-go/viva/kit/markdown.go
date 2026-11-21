package kit

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 精简的 Markdown 解析器（只读展示用）：把 AI 摘要这类短文本解析成块与行内片段，供 prose.go 用原生控件排版。
//
// 渲染层用 react-markdown + remark-gfm + remark-cjk-friendly 渲染摘要；这里覆盖摘要里常见的结构，不追求 CommonMark 全量：
//   - 块：ATX 与 Setext 标题、段落、无序与有序列表（可嵌套，区分紧凑与宽松）、引用、围栏与缩进代码块、分隔线；
//   - 行内：粗体、斜体、粗斜体、删除线（只认 ~~，与渲染层 singleTilde:false 一致）、行内代码、链接与图片（图片只留替代文字）、
//     尖括号自动链接与裸 http(s) 地址、反斜杠转义、常见字符实体、硬换行；
//   - 强调的左右界规则放宽到「开头后不是空白、结尾前不是空白」，等价于 remark-cjk-friendly 对中文标点两侧的放宽；
//     下划线强调另外要求两侧不是字母数字（snake_case 不会被误伤）。
//
// 没有覆盖的（按原文当普通文字显示）：GFM 表格、任务列表复选框、引用式链接、行内 HTML、脚注。

// MdKind 是块的种类。
type MdKind uint8

// 块的种类。
const (
	MdParagraph MdKind = iota
	MdHeading
	MdList
	MdQuote
	MdCode
	MdRule
)

// MdSpan 是一段同样式的行内文字。样式标志可叠加（粗斜体、链接里的粗体……）。
type MdSpan struct {
	Text                       string
	Bold, Italic, Strike, Code bool
	// Href 非空表示它在链接里（只有 http、https、mailto 开头的地址才会成为链接）。
	Href string
	// Break 表示这是一个硬换行（Text 为空）。
	Break bool
}

// MdBlock 是一个块。字段按 Kind 取用：段落与标题用 Spans；列表用 Ordered、Start、Loose、Items；引用用 Children；代码用 Text、Lang。
type MdBlock struct {
	Kind  MdKind
	Level int // 标题级别 1–6
	Spans []MdSpan
	// Tight 表示这个段落直接在紧凑列表的列表项里：渲染层不生成 <p>，行内内容直接在 <li> 里（没有段落的外边距）。
	Tight    bool
	Ordered  bool
	Start    int
	Loose    bool
	Items    [][]MdBlock
	Children []MdBlock
	Text     string // 代码块的原文（不含围栏）
	Lang     string // 围栏的语言标注

	// blankBefore 是它与前一个兄弟块之间隔着空行（列表宽松判定用）。
	blankBefore bool
}

// mdMaxDepth 是块嵌套（引用套列表套引用……）的上限：超过的部分当普通段落，防止恶意输入撑爆栈。
const mdMaxDepth = 12

// ParseMarkdown 把 src 解析成块。空输入返回 nil。
func ParseMarkdown(src string) []MdBlock {
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		lines[i] = expandLeadingTabs(line)
	}
	return parseBlocks(lines, 0)
}

// MdPlain 把一串行内片段拼成纯文字（硬换行写成 \n）：测试与可访问名用。
func MdPlain(spans []MdSpan) string {
	var b strings.Builder
	for _, s := range spans {
		if s.Break {
			b.WriteByte('\n')
			continue
		}
		b.WriteString(s.Text)
	}
	return b.String()
}

// expandLeadingTabs 把行首的制表符展开成 4 个空格。
func expandLeadingTabs(line string) string {
	n := 0
	for n < len(line) && (line[n] == '\t' || line[n] == ' ') {
		n++
	}
	if !strings.ContainsRune(line[:n], '\t') {
		return line
	}
	var b strings.Builder
	col := 0
	for _, r := range line[:n] {
		if r == '\t' {
			step := 4 - col%4
			b.WriteString(strings.Repeat(" ", step))
			col += step
			continue
		}
		b.WriteByte(' ')
		col++
	}
	return b.String() + line[n:]
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

func isBlankLine(s string) bool { return strings.TrimSpace(s) == "" }

// ---- 块 ----

func parseBlocks(lines []string, depth int) []MdBlock {
	var out []MdBlock
	blank := false
	add := func(b MdBlock) {
		b.blankBefore = blank && len(out) > 0
		out = append(out, b)
		blank = false
	}
	for i := 0; i < len(lines); {
		line := lines[i]
		if isBlankLine(line) {
			blank = true
			i++
			continue
		}
		indent := leadingSpaces(line)
		text := line[indent:]
		switch {
		case depth >= mdMaxDepth:
			var b MdBlock
			b, i = parseParagraph(lines, i, depth)
			add(b)
		case indent >= 4:
			var b MdBlock
			b, i = parseIndentedCode(lines, i)
			add(b)
		case fenceOpen(text) != (fence{}):
			var b MdBlock
			b, i = parseFence(lines, i, indent)
			add(b)
		case atxHeading(text) > 0:
			level := atxHeading(text)
			add(MdBlock{Kind: MdHeading, Level: level, Spans: parseInline(atxContent(text, level))})
			i++
		case isRule(text):
			add(MdBlock{Kind: MdRule})
			i++
		case strings.HasPrefix(text, ">"):
			var b MdBlock
			b, i = parseQuote(lines, i, depth)
			add(b)
		case listMarkerAt(line) != nil:
			var b MdBlock
			b, i = parseList(lines, i, depth)
			add(b)
		default:
			var b MdBlock
			b, i = parseParagraph(lines, i, depth)
			add(b)
		}
	}
	return out
}

// startsBlock 报告 line 是否会打断一个正在进行的段落。
func startsBlock(line string) bool {
	indent := leadingSpaces(line)
	if indent >= 4 {
		return false
	}
	text := line[indent:]
	if fenceOpen(text) != (fence{}) || atxHeading(text) > 0 || isRule(text) || strings.HasPrefix(text, ">") {
		return true
	}
	if m := listMarkerAt(line); m != nil {
		// 列表项只有在非空、且（无序或从 1 开始的有序）时才能打断段落
		return !m.empty && (!m.ordered || m.number == 1)
	}
	return false
}

func parseParagraph(lines []string, i, depth int) (MdBlock, int) {
	var parts []string
	for ; i < len(lines); i++ {
		line := lines[i]
		if isBlankLine(line) {
			break
		}
		if len(parts) > 0 {
			if level := setextLevel(line); level > 0 {
				return MdBlock{Kind: MdHeading, Level: level, Spans: parseInline(strings.Join(parts, "\n"))}, i + 1
			}
			if depth < mdMaxDepth && startsBlock(line) {
				break
			}
		}
		parts = append(parts, strings.TrimLeft(line, " "))
	}
	return MdBlock{Kind: MdParagraph, Spans: parseInline(strings.Join(parts, "\n"))}, i
}

// setextLevel 报告 line 是不是 Setext 标题的下划线：=== 是一级，--- 是二级。
func setextLevel(line string) int {
	if leadingSpaces(line) >= 4 {
		return 0
	}
	t := strings.TrimSpace(line)
	if t == "" {
		return 0
	}
	switch {
	case strings.Trim(t, "=") == "":
		return 1
	case strings.Trim(t, "-") == "":
		return 2
	}
	return 0
}

func parseIndentedCode(lines []string, i int) (MdBlock, int) {
	var body []string
	for ; i < len(lines); i++ {
		line := lines[i]
		if isBlankLine(line) {
			body = append(body, strings.TrimPrefix(line, "    "))
			continue
		}
		if leadingSpaces(line) < 4 {
			break
		}
		body = append(body, line[4:])
	}
	for len(body) > 0 && isBlankLine(body[len(body)-1]) {
		body = body[:len(body)-1]
	}
	return MdBlock{Kind: MdCode, Text: strings.Join(body, "\n")}, i
}

// fence 是围栏的开头：字符与长度；零值表示不是围栏。
type fence struct {
	char byte
	n    int
	lang string
}

func fenceOpen(text string) fence {
	if text == "" || (text[0] != '`' && text[0] != '~') {
		return fence{}
	}
	ch := text[0]
	n := 0
	for n < len(text) && text[n] == ch {
		n++
	}
	if n < 3 {
		return fence{}
	}
	info := strings.TrimSpace(text[n:])
	if ch == '`' && strings.Contains(info, "`") {
		return fence{}
	}
	lang := info
	if k := strings.IndexAny(lang, " \t"); k >= 0 {
		lang = lang[:k]
	}
	return fence{char: ch, n: n, lang: lang}
}

func parseFence(lines []string, i, indent int) (MdBlock, int) {
	open := fenceOpen(lines[i][indent:])
	var body []string
	i++
	for ; i < len(lines); i++ {
		line := lines[i]
		t := strings.TrimLeft(line, " ")
		if leadingSpaces(line) < 4 && len(t) >= open.n && strings.Trim(t, string(open.char)) == "" {
			i++
			break
		}
		strip := min(leadingSpaces(line), indent)
		body = append(body, line[strip:])
	}
	return MdBlock{Kind: MdCode, Text: strings.Join(body, "\n"), Lang: open.lang}, i
}

// atxHeading 返回 # 开头的标题级别（1–6）；不是标题返回 0。
func atxHeading(text string) int {
	n := 0
	for n < len(text) && text[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n < len(text) && text[n] != ' ' && text[n] != '\t' {
		return 0
	}
	return n
}

// atxContent 取标题的文字：去掉开头的 # 与结尾的闭合 #。
func atxContent(text string, level int) string {
	s := strings.TrimSpace(text[level:])
	if t := strings.TrimRight(s, "#"); t != s && (t == "" || t[len(t)-1] == ' ' || t[len(t)-1] == '\t') {
		s = strings.TrimSpace(t)
	}
	return s
}

// isRule 报告 text 是不是分隔线：三个以上相同的 - * _，中间只许有空格。
func isRule(text string) bool {
	var ch byte
	n := 0
	for i := 0; i < len(text); i++ {
		switch c := text[i]; c {
		case ' ', '\t':
		case '-', '*', '_':
			if ch != 0 && c != ch {
				return false
			}
			ch = c
			n++
		default:
			return false
		}
	}
	return n >= 3
}

func parseQuote(lines []string, i, depth int) (MdBlock, int) {
	var inner []string
	for ; i < len(lines); i++ {
		line := lines[i]
		indent := leadingSpaces(line)
		if indent < 4 && strings.HasPrefix(line[indent:], ">") {
			t := line[indent+1:]
			t = strings.TrimPrefix(t, " ")
			inner = append(inner, t)
			continue
		}
		// 懒惰延续：上一行有字、这一行不是新块的开头，仍属于引用里的段落
		if !isBlankLine(line) && len(inner) > 0 && !isBlankLine(inner[len(inner)-1]) && !startsBlock(line) {
			inner = append(inner, line)
			continue
		}
		break
	}
	return MdBlock{Kind: MdQuote, Children: parseBlocks(inner, depth+1)}, i
}

// ---- 列表 ----

// listMarker 是一行开头的列表标记。
type listMarker struct {
	indent  int // 标记前的空格数
	ordered bool
	number  int
	// width 是从行首到内容起点的列数（标记前的空格、标记与它后面的 1–4 个空格）。
	width int
	empty bool // 标记后面没有内容
}

// listMarkerAt 解析 line 开头的列表标记；不是列表项返回 nil。
func listMarkerAt(line string) *listMarker {
	indent := leadingSpaces(line)
	if indent >= 4 {
		return nil
	}
	t := line[indent:]
	var m listMarker
	m.indent = indent
	n := 0
	switch {
	case t == "":
		return nil
	case t[0] == '-' || t[0] == '+' || t[0] == '*':
		n = 1
	case t[0] >= '0' && t[0] <= '9':
		for n < len(t) && n < 10 && t[n] >= '0' && t[n] <= '9' {
			n++
		}
		if n > 9 || n >= len(t) || (t[n] != '.' && t[n] != ')') {
			return nil
		}
		m.ordered = true
		m.number, _ = strconv.Atoi(t[:n])
		n++
	default:
		return nil
	}
	rest := t[n:]
	if rest == "" || isBlankLine(rest) {
		m.empty = true
		m.width = indent + n + 1
		return &m
	}
	if rest[0] != ' ' {
		return nil
	}
	spaces := leadingSpaces(rest)
	if spaces >= 5 {
		spaces = 1 // 后面是缩进代码：内容从标记后一个空格起
	}
	m.width = indent + n + spaces
	return &m
}

func parseList(lines []string, i, depth int) (MdBlock, int) {
	first := listMarkerAt(lines[i])
	list := MdBlock{Kind: MdList, Ordered: first.ordered, Start: first.number}
	for i < len(lines) {
		m := listMarkerAt(lines[i])
		if m == nil || m.ordered != first.ordered {
			break
		}
		if isRule(lines[i][m.indent:]) {
			break
		}
		var itemLines []string
		head := ""
		if len(lines[i]) > m.width {
			head = lines[i][m.width:]
		}
		itemLines = append(itemLines, head)
		j := i + 1
		for ; j < len(lines); j++ {
			line := lines[j]
			if isBlankLine(line) {
				itemLines = append(itemLines, "")
				continue
			}
			if leadingSpaces(line) >= m.width {
				itemLines = append(itemLines, line[m.width:])
				continue
			}
			// 缩进不够：只有紧跟在有字的行之后、又不是新块开头的行才是懒惰延续
			if !isBlankLine(lines[j-1]) && len(itemLines) > 0 && !startsBlock(line) && listMarkerAt(line) == nil {
				itemLines = append(itemLines, strings.TrimLeft(line, " "))
				continue
			}
			break
		}
		// 去掉列表项末尾的空行，记下它们（之后若还有同一个列表的下一项，就是项间有空行）
		trailing := 0
		for len(itemLines) > 1 && itemLines[len(itemLines)-1] == "" { // 第 0 行是标记行本身，不算尾部空行
			itemLines = itemLines[:len(itemLines)-1]
			trailing++
		}
		blocks := parseBlocks(itemLines, depth+1)
		for k := 1; k < len(blocks); k++ {
			if blocks[k].blankBefore {
				list.Loose = true
			}
		}
		list.Items = append(list.Items, blocks)
		i = j - trailing
		// 空行之后还是同一个列表的下一项吗
		next := j
		if m2 := listMarkerAt(safeLine(lines, next)); next < len(lines) && m2 != nil && m2.ordered == first.ordered && !isRule(lines[next][m2.indent:]) {
			if trailing > 0 {
				list.Loose = true
			}
			i = next
			continue
		}
		break
	}
	if !list.Loose {
		for _, item := range list.Items {
			for k := range item {
				if item[k].Kind == MdParagraph {
					item[k].Tight = true
				}
			}
		}
	}
	return list, i
}

func safeLine(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

// ---- 行内 ----

// parseInline 解析一段行内文字（可含 \n：软换行）。
func parseInline(s string) []MdSpan {
	return mergeSpans(parseInlineStyled(s, MdSpan{}, 0))
}

// mdInlineMaxDepth 是强调与链接互相嵌套的上限。
const mdInlineMaxDepth = 8

// parseInlineStyled 在已有样式 st 下解析 s；st 里只用样式标志与 Href。
func parseInlineStyled(s string, st MdSpan, depth int) []MdSpan {
	var out []MdSpan
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		sp := st
		sp.Text = buf.String()
		out = append(out, sp)
		buf.Reset()
	}
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			next := s[i+1]
			switch {
			case next == '\n':
				flush()
				out = append(out, MdSpan{Break: true})
				i += 2
			case isASCIIPunct(next):
				buf.WriteByte(next)
				i += 2
			default:
				buf.WriteByte(c)
				i++
			}
		case c == '`':
			n := runLen(s, i, '`')
			if j := findBacktickClose(s, i+n, n); j >= 0 {
				flush()
				code := strings.ReplaceAll(s[i+n:j], "\n", " ")
				if len(code) >= 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
					code = code[1 : len(code)-1]
				}
				sp := st
				sp.Text, sp.Code = code, true
				out = append(out, sp)
				i = j + n
			} else {
				buf.WriteString(s[i : i+n])
				i += n
			}
		case c == '\n':
			trimmed := strings.TrimRight(buf.String(), " ")
			hard := len(buf.String())-len(trimmed) >= 2
			buf.Reset()
			buf.WriteString(trimmed)
			if hard {
				flush()
				out = append(out, MdSpan{Break: true})
			} else {
				buf.WriteByte('\n')
			}
			i++
			for i < len(s) && s[i] == ' ' {
				i++
			}
		case (c == '*' || c == '_') && depth < mdInlineMaxDepth:
			if consumed, spans, ok := parseEmphasis(s, i, st, depth); ok {
				flush()
				out = append(out, spans...)
				i += consumed
				break
			}
			n := runLen(s, i, c)
			buf.WriteString(s[i : i+n])
			i += n
		case c == '~' && depth < mdInlineMaxDepth:
			n := runLen(s, i, '~')
			if n == 2 {
				if j := findDelimClose(s, i+2, '~', 2); j > i+2 && !startsWithSpace(s[i+2:]) {
					flush()
					inner := st
					inner.Strike = true
					out = append(out, parseInlineStyled(s[i+2:j], inner, depth+1)...)
					i = j + 2
					break
				}
			}
			buf.WriteString(s[i : i+n])
			i += n
		case (c == '[' || (c == '!' && i+1 < len(s) && s[i+1] == '[')) && depth < mdInlineMaxDepth:
			image := c == '!'
			start := i
			if image {
				start++
			}
			if text, dest, end, ok := parseLinkAt(s, start); ok {
				flush()
				inner := st
				switch {
				case image:
					// 图片只留替代文字
				case st.Href == "" && safeHref(dest):
					inner.Href = dest
				}
				out = append(out, parseInlineStyled(text, inner, depth+1)...)
				i = end
				break
			}
			buf.WriteByte(c)
			i++
		case c == '<':
			if end := strings.IndexByte(s[i:], '>'); end > 1 && st.Href == "" {
				target := s[i+1 : i+end]
				if strings.IndexAny(target, " \n") < 0 && safeHref(target) {
					flush()
					sp := st
					sp.Text, sp.Href = target, target
					out = append(out, sp)
					i += end + 1
					break
				}
			}
			buf.WriteByte(c)
			i++
		case c == 'h' && st.Href == "" && (i == 0 || !isWordByte(s[i-1])) && hasURLPrefix(s[i:]):
			j := i
			for j < len(s) && s[j] != ' ' && s[j] != '\n' && s[j] != '<' && s[j] != '\t' {
				j++
			}
			url := trimURLTail(s[i:j])
			if len(url) <= len("https://") {
				buf.WriteByte(c)
				i++
				break
			}
			flush()
			sp := st
			sp.Text, sp.Href = url, url
			out = append(out, sp)
			i += len(url)
		case c == '&':
			if r, n := decodeEntity(s[i:]); n > 0 {
				buf.WriteString(r)
				i += n
				break
			}
			buf.WriteByte(c)
			i++
		default:
			buf.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

func isASCIIPunct(b byte) bool {
	return (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~')
}

func isWordByte(b byte) bool {
	return b >= 0x80 || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func runLen(s string, i int, ch byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == ch {
		n++
	}
	return n
}

func startsWithSpace(s string) bool {
	if s == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

func endsWithSpace(s string) bool {
	if s == "" {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.IsSpace(r)
}

// findBacktickClose 找 from 之后长度恰好为 n 的反引号串，返回它的起点；没有返回 -1。
func findBacktickClose(s string, from, n int) int {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		k := runLen(s, i, '`')
		if k == n {
			return i
		}
		i += k
	}
	return -1
}

// skipLiteral 返回 s[i] 处需要整体跳过的内容长度（转义字符、行内代码），没有返回 0：找闭合定界符时不能落进这些内容。
func skipLiteral(s string, i int) int {
	switch s[i] {
	case '\\':
		if i+1 < len(s) {
			return 2
		}
	case '`':
		n := runLen(s, i, '`')
		if j := findBacktickClose(s, i+n, n); j >= 0 {
			return j + n - i
		}
		return n
	}
	return 0
}

// findDelimClose 找 from 之后长度恰好为 n、前面不是空白的 ch 串，返回它的起点；没有返回 -1。
func findDelimClose(s string, from int, ch byte, n int) int {
	for i := from; i < len(s); {
		if skip := skipLiteral(s, i); skip > 0 {
			i += skip
			continue
		}
		if s[i] != ch {
			i++
			continue
		}
		k := runLen(s, i, ch)
		if k == n && i > from && !endsWithSpace(s[:i]) {
			return i
		}
		i += k
	}
	return -1
}

// parseEmphasis 尝试把 s[i] 起的 * 或 _ 串解析成强调：返回消耗的字节数与结果。
func parseEmphasis(s string, i int, st MdSpan, depth int) (consumed int, spans []MdSpan, ok bool) {
	ch := s[i]
	n := runLen(s, i, ch)
	if n > 3 || startsWithSpace(s[i+n:]) {
		return 0, nil, false
	}
	if ch == '_' && i > 0 && isAlnumBefore(s, i) {
		return 0, nil, false
	}
	j := findDelimClose(s, i+n, ch, n)
	if j < 0 {
		return 0, nil, false
	}
	if ch == '_' && j+n < len(s) && isAlnumAfter(s, j+n) {
		return 0, nil, false
	}
	inner := st
	switch n {
	case 1:
		inner.Italic = true
	case 2:
		inner.Bold = true
	default:
		inner.Bold, inner.Italic = true, true
	}
	return j + n - i, parseInlineStyled(s[i+n:j], inner, depth+1), true
}

func isAlnumBefore(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isAlnumAfter(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// parseLinkAt 解析 s[start] 处的 [text](dest "title")：返回链接文字、目的地、整个结构结束后的下标。
func parseLinkAt(s string, start int) (text, dest string, end int, ok bool) {
	depth := 0
	closeIdx := -1
	for i := start; i < len(s); {
		if skip := skipLiteral(s, i); skip > 0 {
			i += skip
			continue
		}
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
		i++
	}
	if closeIdx < 0 || closeIdx+1 >= len(s) || s[closeIdx+1] != '(' {
		return "", "", 0, false
	}
	// 目的地：到配平的右括号为止，去掉可选的标题与尖括号
	parens := 1
	k := closeIdx + 2
	for ; k < len(s); k++ {
		if s[k] == '\\' {
			k++
			continue
		}
		if s[k] == '(' {
			parens++
		} else if s[k] == ')' {
			parens--
			if parens == 0 {
				break
			}
		}
	}
	if parens != 0 {
		return "", "", 0, false
	}
	raw := strings.TrimSpace(s[closeIdx+2 : k])
	if strings.HasPrefix(raw, "<") {
		if e := strings.IndexByte(raw, '>'); e > 0 {
			raw = raw[1:e]
		}
	} else if sp := strings.IndexAny(raw, " \t\n"); sp >= 0 {
		raw = raw[:sp]
	}
	return s[start+1 : closeIdx], raw, k + 1, true
}

// safeHref 报告 dest 能不能作为链接地址：与渲染层 resolveSafeHref 一致，只放行 http、https（再加 mailto）。
func safeHref(dest string) bool {
	d := strings.ToLower(strings.TrimSpace(dest))
	return strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://") || strings.HasPrefix(d, "mailto:")
}

func hasURLPrefix(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// trimURLTail 去掉裸地址末尾的标点（句号、逗号、不成对的右括号等）。
func trimURLTail(url string) string {
	for url != "" {
		last := url[len(url)-1]
		switch {
		case strings.IndexByte(".,:;!?'\"*_~", last) >= 0:
			url = url[:len(url)-1]
		case last == ')' && strings.Count(url, ")") > strings.Count(url, "("):
			url = url[:len(url)-1]
		default:
			return url
		}
	}
	return url
}

var mdEntities = map[string]string{"&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": "\"", "&#39;": "'", "&apos;": "'", "&nbsp;": "\u00a0"}

// decodeEntity 解码 s 开头的字符实体，返回替换文字与消耗的字节数；不是实体返回 0。
func decodeEntity(s string) (string, int) {
	end := strings.IndexByte(s, ';')
	if end < 2 || end > 10 {
		return "", 0
	}
	ent := s[:end+1]
	if r, ok := mdEntities[ent]; ok {
		return r, len(ent)
	}
	if ent[1] == '#' {
		var code int64
		var err error
		if len(ent) > 3 && (ent[2] == 'x' || ent[2] == 'X') {
			code, err = strconv.ParseInt(ent[3:end], 16, 32)
		} else {
			code, err = strconv.ParseInt(ent[2:end], 10, 32)
		}
		if err == nil && code > 0 && utf8.ValidRune(rune(code)) {
			return string(rune(code)), len(ent)
		}
	}
	return "", 0
}

// mergeSpans 合并相邻的同样式文字片段，去掉空片段。
func mergeSpans(spans []MdSpan) []MdSpan {
	var out []MdSpan
	for _, sp := range spans {
		if !sp.Break && sp.Text == "" {
			continue
		}
		if n := len(out); n > 0 && !sp.Break && !out[n-1].Break && !sp.Code && !out[n-1].Code &&
			sp.Bold == out[n-1].Bold && sp.Italic == out[n-1].Italic && sp.Strike == out[n-1].Strike && sp.Href == out[n-1].Href {
			out[n-1].Text += sp.Text
			continue
		}
		out = append(out, sp)
	}
	return out
}
