package extract

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"golang.org/x/net/html"
)

var (
	languagePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?:^|` + ws + `)highlight-source-([\w+#.-]+)`),
		regexp.MustCompile(`(?:^|` + ws + `)(?:language|lang)-([\w+#.-]+)`),
		regexp.MustCompile(`(?:^|` + ws + `)highlight-([\w+#.-]+)`),
		regexp.MustCompile(`(?:^|` + ws + `)brush:` + ws + `*([\w+#.-]+)`),
	}
	reLangValue  = regexp.MustCompile(`^[a-z0-9_+#.-]{1,30}$`)
	notLanguages = map[string]bool{"default": true, "none": true, "text": true, "plain": true, "plaintext": true, "nohighlight": true, "notranslate": true}
	blockInCode  = map[string]bool{
		"ADDRESS": true, "ARTICLE": true, "ASIDE": true, "BLOCKQUOTE": true, "DD": true, "DIV": true, "DL": true, "DT": true,
		"FIGURE": true, "FOOTER": true, "H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true, "HEADER": true,
		"LI": true, "OL": true, "P": true, "SECTION": true, "TABLE": true, "TR": true, "UL": true,
	}
	reBlankLine = regexp.MustCompile(`^` + ws + `*$`)
	reTrailWS   = regexp.MustCompile(ws + `+$`)
	reBackticks = regexp.MustCompile("`+")
	reNewlineWS = regexp.MustCompile(ws + `*\n` + ws + `*`)
)

// codeLanguage 从代码块自身、内层 code 与外层两级容器的 class / data-lang 推断语言。
func codeLanguage(pre *html.Node) string {
	parent := parentElement(pre)
	var grand *html.Node
	if parent != nil {
		grand = parentElement(parent)
	}
	for _, node := range []*html.Node{pre, qs(pre, "code"), parent, grand} {
		if node == nil {
			continue
		}
		declared, ok := attr(node, "data-lang")
		if !ok {
			declared, ok = attr(node, "data-language")
		}
		classes := getAttr(node, "class")
		candidates := []string{}
		if ok {
			candidates = append(candidates, declared)
		} else {
			candidates = append(candidates, "")
		}
		for _, p := range languagePatterns {
			if m := p.FindStringSubmatch(classes); m != nil {
				candidates = append(candidates, m[1])
			} else {
				candidates = append(candidates, "")
			}
		}
		for _, c := range candidates {
			v := strings.ToLower(jsTrim(c))
			if v != "" && reLangValue.MatchString(v) && !notLanguages[v] {
				return v
			}
		}
	}
	return ""
}

// codeText：<br> 与按行排列的块元素换行；其余照搬文本节点。
func codeText(pre *html.Node) string {
	var sb strings.Builder
	afterBlock := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for _, c := range children(n) {
			switch c.Type {
			case html.TextNode:
				if afterBlock && reBlankLine.MatchString(c.Data) && strings.Contains(c.Data, "\n") {
					continue
				}
				sb.WriteString(c.Data)
				afterBlock = false
			case html.ElementNode:
				if nodeName(c) == "BR" {
					sb.WriteString("\n")
					afterBlock = false
					continue
				}
				block := blockInCode[nodeName(c)]
				if s := sb.String(); block && s != "" && !strings.HasSuffix(s, "\n") {
					sb.WriteString("\n")
				}
				walk(c)
				if block {
					if !strings.HasSuffix(sb.String(), "\n") {
						sb.WriteString("\n")
					}
					afterBlock = true
				}
			}
		}
	}
	walk(pre)
	s := strings.ReplaceAll(sb.String(), "\u00a0", " ")
	s = strings.TrimLeft(s, "\n")
	return reTrailWS.ReplaceAllString(s, "")
}

func ownerTable(n *html.Node) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if nodeName(p) == "TABLE" {
			return p
		}
	}
	return nil
}

func ownRows(table *html.Node) []*html.Node {
	var out []*html.Node
	for _, r := range qsa(table, "tr") {
		if ownerTable(r) == table {
			out = append(out, r)
		}
	}
	return out
}

func ownCells(row *html.Node) []*html.Node {
	var out []*html.Node
	for _, c := range elementChildren(row) {
		if name := nodeName(c); name == "TD" || name == "TH" {
			out = append(out, c)
		}
	}
	return out
}

func captionOf(table *html.Node) *html.Node {
	for _, c := range elementChildren(table) {
		if nodeName(c) == "CAPTION" {
			return c
		}
	}
	return nil
}

func spanOf(cell *html.Node, name string, limit int) int {
	v := strings.TrimSpace(getAttr(cell, name))
	if v == "" {
		return 1
	}
	// parseInt：取开头的整数部分。
	end := 0
	for end < len(v) && (v[end] >= '0' && v[end] <= '9' || (end == 0 && (v[end] == '-' || v[end] == '+'))) {
		end++
	}
	n, err := strconv.Atoi(v[:end])
	if err != nil || n <= 1 {
		return 1
	}
	if n > limit {
		return limit
	}
	return n
}

const layoutBlocks = "h1, h2, h3, h4, h5, h6, pre, blockquote, hr, form, table"

func isDataTable(table *html.Node) bool {
	role := strings.ToLower(getAttr(table, "role"))
	if role == "presentation" || role == "none" || getAttr(table, "datatable") == "0" {
		return false
	}
	if qs(table, "table") != nil {
		return false
	}
	rows := ownRows(table)
	width := 0
	for _, row := range rows {
		w := 0
		for _, c := range ownCells(row) {
			w += spanOf(c, "colspan", 64)
		}
		if w > width {
			width = w
		}
	}
	if len(rows) == 0 || width == 0 {
		return false
	}
	if getAttr(table, "summary") != "" {
		return true
	}
	if cp := captionOf(table); cp != nil && jsTrim(textContent(cp)) != "" {
		return true
	}
	if qs(table, "th, thead, tfoot, col, colgroup") != nil {
		return true
	}
	if len(rows) <= 1 || width <= 1 {
		return false
	}
	for _, row := range rows {
		for _, cell := range ownCells(row) {
			if qs(cell, layoutBlocks) != nil || len(qsa(cell, "p, li")) > 2 ||
				utf16Len(jsTrim(collapseWS(textContent(cell)))) > 400 {
				return false
			}
		}
	}
	return true
}

func cellMarkdown(content string) string {
	content = reNewlineWS.ReplaceAllString(content, " ")
	content = strings.ReplaceAll(content, "|", `\|`)
	return jsTrim(content)
}

type mdState struct {
	kinds     map[*html.Node]bool
	converted map[*html.Node]string
}

func (s *mdState) isPipe(table *html.Node) bool {
	if table == nil {
		return false
	}
	k, ok := s.kinds[table]
	if !ok {
		k = isDataTable(table)
		s.kinds[table] = k
	}
	return k
}

func (s *mdState) inPipeCell(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if name := nodeName(p); name == "TD" || name == "TH" {
			return s.isPipe(ownerTable(p))
		}
	}
	return false
}

func (s *mdState) pipeTable(table *html.Node) string {
	rows := ownRows(table)
	grid := make([][]*string, len(rows))
	set := func(r, c int, v string) {
		for len(grid[r]) <= c {
			grid[r] = append(grid[r], nil)
		}
		grid[r][c] = &v
	}
	for r, row := range rows {
		column := 0
		for _, cell := range ownCells(row) {
			for column < len(grid[r]) && grid[r][column] != nil {
				column++
			}
			colspan := spanOf(cell, "colspan", 64)
			rowspan := spanOf(cell, "rowspan", len(rows)-r)
			for dr := 0; dr < rowspan; dr++ {
				for dc := 0; dc < colspan; dc++ {
					v := ""
					if dr == 0 && dc == 0 {
						v = cellMarkdown(s.converted[cell])
					}
					set(r+dr, column+dc, v)
				}
			}
			column += colspan
		}
	}
	var body [][]string
	for _, cells := range grid {
		row := make([]string, len(cells))
		any := false
		for i, c := range cells {
			if c != nil {
				row[i] = *c
				if *c != "" {
					any = true
				}
			}
		}
		if any {
			body = append(body, row)
		}
	}
	width := 0
	for _, r := range body {
		if len(r) > width {
			width = len(r)
		}
	}
	if len(body) == 0 || width == 0 {
		return ""
	}
	line := func(cells []string) string {
		out := make([]string, width)
		for i := range out {
			if i < len(cells) {
				out[i] = cells[i]
			}
		}
		return "| " + strings.Join(out, " | ") + " |"
	}
	var sb strings.Builder
	sb.WriteString("\n\n")
	if cp := captionOf(table); cp != nil {
		if t := cellMarkdown(s.converted[cp]); t != "" {
			sb.WriteString(t + "\n\n")
		}
	}
	sb.WriteString(line(body[0]))
	seps := make([]string, width)
	for i := range seps {
		seps[i] = "---"
	}
	sb.WriteString("\n| " + strings.Join(seps, " | ") + " |")
	for _, cells := range body[1:] {
		sb.WriteString("\n" + line(cells))
	}
	sb.WriteString("\n\n")
	return sb.String()
}

// htmlToMarkdown 对应 TS 的 htmlToMarkdown：这里直接转换节点树（TS 经 innerHTML 字符串）。
func htmlToMarkdown(root *html.Node) string {
	st := &mdState{kinds: map[*html.Node]bool{}, converted: map[*html.Node]string{}}
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(
				commonmark.WithBulletListMarker("-"),
				commonmark.WithEmDelimiter("_"),
				// 相邻列表之间不插 <!--THE END-->：Turndown 没有这行注释，留着会进入正文、全文索引与 AI 上下文
				commonmark.WithListEndComment(false),
			),
		),
	)
	for _, tag := range []string{"script", "style", "noscript", "iframe", "template", "canvas", "button", "select", "textarea", "input"} {
		conv.Register.TagType(tag, converter.TagTypeRemove, converter.PriorityStandard)
	}
	conv.Register.RendererFor("pre", converter.TagTypeBlock, func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		text := codeText(n)
		if jsTrim(text) == "" {
			return converter.RenderSuccess
		}
		if st.inPipeCell(n) {
			inline := jsTrim(collapseWS(text))
			longest := 0
			for _, run := range reBackticks.FindAllString(inline, -1) {
				if len(run)+1 > longest {
					longest = len(run) + 1
				}
			}
			if longest < 1 {
				longest = 1
			}
			ticks := strings.Repeat("`", longest)
			pad := ""
			if len(ticks) > 1 {
				pad = " "
			}
			w.WriteString(ticks + pad + inline + pad + ticks)
			return converter.RenderSuccess
		}
		longest := 3
		for _, run := range reBackticks.FindAllString(text, -1) {
			if len(run)+1 > longest {
				longest = len(run) + 1
			}
		}
		fence := strings.Repeat("`", longest)
		w.WriteString("\n\n" + fence + getAttr(n, codeLanguageAttrName) + "\n" + text + "\n" + fence + "\n\n")
		return converter.RenderSuccess
	}, converter.PriorityEarly)
	conv.Register.RendererFor("table", converter.TagTypeBlock, func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		if !st.isPipe(n) {
			return converter.RenderTryNext
		}
		// 单元格先按普通规则转换、暂存内容，再统一排成网格。
		store := func(cell *html.Node) {
			var buf bytes.Buffer
			ctx.RenderChildNodes(ctx, &buf, cell)
			st.converted[cell] = buf.String()
		}
		for _, row := range ownRows(n) {
			for _, cell := range ownCells(row) {
				store(cell)
			}
		}
		if cp := captionOf(n); cp != nil {
			store(cp)
		}
		w.WriteString(st.pipeTable(n))
		return converter.RenderSuccess
	}, converter.PriorityEarly)

	wrapper := createElement("div")
	for root.FirstChild != nil {
		c := root.FirstChild
		root.RemoveChild(c)
		wrapper.AppendChild(c)
	}
	out, err := conv.ConvertNode(wrapper)
	if err != nil {
		return ""
	}
	return jsTrim(string(out))
}
