package ask

// 回答正文里的 `[n]` 引用标记（渲染层 components/ask/qa-citations.ts 的逐行移植）。
//
// 模型按提示词在句末标 `[1]`；这里把标记改写成可点击的锚点链接（交给 Markdown 渲染），
// 按正文里首次出现的顺序重排展示编号，并在复制回答时保持与屏幕一致。围栏代码块与行内代码里的方括号数字不是引用
// （正则字符组、数组下标都长这样）。
//
// 正则里的 \s 用 JS 的空白集合（RE2 的 \s 只有 ASCII 空白，会漏掉 U+00A0、全角空格）。

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// CiteHrefPrefix 是引用锚点的链接前缀；点击时用 ParseCiteHref 取回原始编号。
const CiteHrefPrefix = "#qa-cite="

const jsSpaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// reCitationMarker 对应 /[[【]\s*(\d{1,3}(?:\s*[,，、]\s*\d{1,3})*)\s*[\]】]/g：兼容全角括号与多编号。
var reCitationMarker = regexp.MustCompile(
	`[\[\x{3010}][` + jsSpaceClass + `]*([0-9]{1,3}(?:[` + jsSpaceClass + `]*[,\x{ff0c}\x{3001}][` + jsSpaceClass + `]*[0-9]{1,3})*)[` + jsSpaceClass + `]*[\]\x{3011}]`)

var (
	reFenceLine  = regexp.MustCompile("^[" + jsSpaceClass + "]*(?:```|~~~)")
	reInlineCode = regexp.MustCompile("`[^`]*`")
)

// CitationLabels 是「原始编号 → 展示编号」，保持建立时的顺序（正文首次出现的顺序在前）。
type CitationLabels struct {
	labels map[int]int
	order  []int
}

// Get 返回原始编号对应的展示编号。
func (l *CitationLabels) Get(ordinal int) (int, bool) {
	if l == nil {
		return 0, false
	}
	label, ok := l.labels[ordinal]
	return label, ok
}

// LabelOf 返回展示编号；没有登记的编号沿用原编号（`labels.get(o) ?? o`）。
func (l *CitationLabels) LabelOf(ordinal int) int {
	if label, ok := l.Get(ordinal); ok {
		return label
	}
	return ordinal
}

// Ordinals 按展示编号从小到大返回原始编号。
func (l *CitationLabels) Ordinals() []int {
	if l == nil {
		return nil
	}
	return slices.Clone(l.order)
}

// parseOrdinals 把 `1, 2、3` 这样的编号组拆成整数；分隔符是半角与全角逗号、顿号。
func parseOrdinals(group string) []int {
	var out []int
	for _, token := range strings.FieldsFunc(group, func(r rune) bool { return r == ',' || r == '\uff0c' || r == '\u3001' }) {
		n, err := strconv.Atoi(jsTrim(token))
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// rewriteOutsideCode 逐行改写标记：围栏代码块整块跳过，行内代码段跳过。
// 与 JS 一致：用 split(/(`[^`]*`)/g) 切段，凡以反引号开头的段都按代码段处理（含没有闭合的孤立反引号开头的那一段）。
func rewriteOutsideCode(markdown string, rewrite func(match string, ordinals []int) string) string {
	rewriteSegment := func(segment string) string {
		return reCitationMarker.ReplaceAllStringFunc(segment, func(match string) string {
			sub := reCitationMarker.FindStringSubmatch(match)
			if ordinals := parseOrdinals(sub[1]); ordinals != nil {
				return rewrite(match, ordinals)
			}
			return match
		})
	}
	lines := strings.Split(markdown, "\n")
	inFence := false
	for i, line := range lines {
		if reFenceLine.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		var b strings.Builder
		last := 0
		emit := func(segment string) {
			if strings.HasPrefix(segment, "`") {
				b.WriteString(segment)
			} else {
				b.WriteString(rewriteSegment(segment))
			}
		}
		for _, loc := range reInlineCode.FindAllStringIndex(line, -1) {
			emit(line[last:loc[0]])
			emit(line[loc[0]:loc[1]])
			last = loc[1]
		}
		emit(line[last:])
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// BuildCitationLabels 求展示编号：按正文里首次出现的顺序从 1 连续编号，正文未提到的来源排在其后。
// 智能体检索按发现顺序给资料编号、只有读过的才算来源，原始编号常是 [3][5][12]。
func BuildCitationLabels(markdown string, ordinals []int) *CitationLabels {
	valid := make(map[int]bool, len(ordinals))
	for _, o := range ordinals {
		valid[o] = true
	}
	seen := map[int]bool{}
	var order []int
	visit := func(o int) {
		if valid[o] && !seen[o] {
			seen[o] = true
			order = append(order, o)
		}
	}
	if markdown != "" {
		rewriteOutsideCode(markdown, func(match string, found []int) string {
			for _, o := range found {
				visit(o)
			}
			return match
		})
	}
	for _, o := range ordinals {
		visit(o)
	}
	labels := &CitationLabels{labels: make(map[int]int, len(order)), order: order}
	for i, o := range order {
		labels.labels[o] = i + 1
	}
	return labels
}

type citationPart struct {
	ordinal, label int
	linked         bool
}

// relabel 给了展示编号时，指不到任何来源的编号直接去掉：留着原数字会和重排后的编号撞号，让读者以为它指向另一份资料。
func relabel(ordinals []int, valid map[int]bool, labels *CitationLabels) []citationPart {
	var parts []citationPart
	for _, o := range ordinals {
		if labels != nil && !valid[o] {
			continue
		}
		parts = append(parts, citationPart{ordinal: o, label: labels.LabelOf(o), linked: valid[o]})
	}
	return parts
}

func ordinalSet(ordinals []int) map[int]bool {
	set := make(map[int]bool, len(ordinals))
	for _, o := range ordinals {
		set[o] = true
	}
	return set
}

func anyValid(ordinals []int, valid map[int]bool) bool {
	for _, o := range ordinals {
		if valid[o] {
			return true
		}
	}
	return false
}

// LinkifyCitations 把引用标记改写成 Markdown 锚点链接。labels 为 nil 表示沿用原编号：没有对应来源的编号原样留着
// （做成死链比不可点更糟）；给了 labels 则指不到来源的编号去掉。
func LinkifyCitations(markdown string, validOrdinals []int, labels *CitationLabels) string {
	valid := ordinalSet(validOrdinals)
	if len(valid) == 0 || markdown == "" {
		return markdown
	}
	return rewriteOutsideCode(markdown, func(match string, ordinals []int) string {
		if !anyValid(ordinals, valid) {
			if labels != nil {
				return ""
			}
			return match
		}
		parts := relabel(ordinals, valid, labels)
		if len(parts) == 1 {
			// 单编号时整个 `[1]` 都可点，链接文字里的方括号要转义成字面量
			return fmt.Sprintf(`[\[%d\]](%s%d)`, parts[0].label, CiteHrefPrefix, parts[0].ordinal)
		}
		inner := make([]string, len(parts))
		for i, part := range parts {
			if part.linked {
				inner[i] = fmt.Sprintf("[%d](%s%d)", part.label, CiteHrefPrefix, part.ordinal)
			} else {
				inner[i] = strconv.Itoa(part.label)
			}
		}
		return `\[` + strings.Join(inner, ", ") + `\]`
	})
}

// RenumberCitations 复制回答时与屏幕上的编号保持一致：纯文本的 `[1, 2]`，指不到来源的编号去掉。
func RenumberCitations(markdown string, validOrdinals []int, labels *CitationLabels) string {
	valid := ordinalSet(validOrdinals)
	if len(valid) == 0 || markdown == "" {
		return markdown
	}
	return rewriteOutsideCode(markdown, func(_ string, ordinals []int) string {
		if !anyValid(ordinals, valid) {
			return ""
		}
		parts := relabel(ordinals, valid, labels)
		texts := make([]string, len(parts))
		for i, part := range parts {
			texts[i] = strconv.Itoa(part.label)
		}
		return "[" + strings.Join(texts, ", ") + "]"
	})
}

// ParseCiteHref 取回锚点链接里的原始编号；不是引用锚点（或编号不是纯数字）返回 false。
// 本包生成的链接编号都是纯数字；渲染层对 `#qa-cite=` 之后的内容用 Number() 宽松解析，这里只认纯数字，
// 其余写法（空串、科学计数法）不会由 LinkifyCitations 产生，按普通链接处理。
func ParseCiteHref(href string) (int, bool) {
	rest, ok := strings.CutPrefix(href, CiteHrefPrefix)
	if !ok || rest == "" {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}

// SortOrdinalsByLabel 按展示编号从小到大排来源编号（来源 chip 的顺序）；展示编号相同时保持原顺序。
func SortOrdinalsByLabel(ordinals []int, labels *CitationLabels) []int {
	out := slices.Clone(ordinals)
	slices.SortStableFunc(out, func(a, b int) int { return labels.LabelOf(a) - labels.LabelOf(b) })
	return out
}
