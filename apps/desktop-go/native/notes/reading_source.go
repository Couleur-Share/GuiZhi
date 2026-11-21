package notes

// 阅读来源的取文与改写（packages/shared/utils/reading-source.ts 的 readingSourceText 与 replaceReadingSource）：
// 详情页「编辑原文」只改选中来源那一段，其余小节、视频元数据块与换行形式原样保留。
//
// 改写的结果直接写进条目正文，所以要与 TS 逐字节一致（native/notes/testdata/reading-source-golden.json 由渲染层单测导出，
// 本包的对拍测试逐条重放）。TS 里的小节标题匹配式 /^##\s*(讨论总结|正文|讨论(?:（[^\n]*）)?)\s*$/gm 用了 JS 多行模式：
// ^ 与 $ 认 \n \r U+2028 U+2029 四种行终止符，且 \s 能跨行；RE2 的多行只认 \n，所以这里手写匹配，不用正则。

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// 阅读来源的两种取值（ThemedReadingSourceKind）。
const (
	SourceBody    = "body"
	SourceSummary = "summary"
)

// Shape 是条目正文的形态：只有论坛与图片条目的正文按小节组织，其余整篇都是正文。
type Shape int

const (
	ShapePlain Shape = iota
	ShapeForum
	ShapeImage
)

// ErrNoSummary 是非论坛条目取讨论总结时的失败（TS 抛同样文字的 Error）。
var ErrNoSummary = errors.New("此条目没有讨论总结")

// ReadingSourceText 取条目在来源 kind 下的原文（readingSourceText）。
func ReadingSourceText(content string, shape Shape, kind string) (string, error) {
	if shape == ShapeForum {
		sections := SplitForumSections(content)
		if kind == SourceSummary {
			return sections.Summary, nil
		}
		return sections.Body, nil
	}
	if kind == SourceSummary {
		return "", ErrNoSummary
	}
	body := content
	if stripped, has := VideoMetaBody(content); has {
		body = stripped
	}
	if shape == ShapeImage {
		return SplitImageSections(body).Caption, nil
	}
	return body, nil
}

// ReplaceReadingSource 把 replacement 写成来源 kind 的新原文，返回整篇新正文（replaceReadingSource）。
// 与当前原文相同时原样返回 content（连换行形式都不动）；其余情况下元数据块之后的部分改写，再还原成 content 原来的换行形式。
func ReplaceReadingSource(content string, shape Shape, kind, replacement string) (string, error) {
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	next := strings.ReplaceAll(replacement, "\r\n", "\n")
	// TS 在这里拿未规整换行的 content 与 replacement 比较；非论坛条目取总结在此抛错。
	current, err := ReadingSourceText(content, shape, kind)
	if err != nil {
		return "", err
	}
	if current == replacement {
		return content, nil
	}
	body, prefix := normalized, ""
	if meta := ParseVideoMeta(normalized); meta != nil {
		// 正文是 normalized 的后缀（去掉了元数据引用块与其后的空行），前缀就是前面被去掉的那一截。
		body = meta.Body
		prefix = normalized[:len(normalized)-len(body)]
	}
	var result string
	switch shape {
	case ShapeForum:
		result = replaceForumSection(body, kind, next)
	case ShapeImage:
		result = replaceImageCaption(body, next)
	default:
		if kind != SourceBody {
			return "", ErrNoSummary
		}
		result = next
	}
	return strings.ReplaceAll(prefix+result, "\n", eol), nil
}

// headingMatch 是一个小节标题的匹配：[index, end) 是整段匹配（含标题行末尾吞掉的空白），title 是标题名。
type headingMatch struct {
	index, end int
	title      string
}

// replaceForumSection 改写论坛正文里 kind 对应的小节：同名小节可能出现多次（都要处理），
// 正文来源还包括第一个小节标题之前的游离内容；一个都没有就在末尾补一个新小节。
func replaceForumSection(body, kind, next string) string {
	matches := forumHeadingMatches(body)
	type span struct{ start, end int }
	var ranges []span
	first := len(body)
	if len(matches) > 0 {
		first = matches[0].index
	}
	if kind == SourceBody && first > 0 {
		ranges = append(ranges, span{0, first})
	}
	for i, m := range matches {
		if (kind == SourceSummary && m.title == forumSummaryTitle) || (kind == SourceBody && m.title == forumBodyTitle) {
			end := len(body)
			if i+1 < len(matches) {
				end = matches[i+1].index
			}
			ranges = append(ranges, span{m.end, end})
		}
	}
	if len(ranges) == 0 {
		title := forumBodyTitle
		if kind == SourceSummary {
			title = forumSummaryTitle
		}
		return body + "\n\n## " + title + "\n\n" + next + "\n"
	}
	// 从后往前换，前面的区间位置不受影响；第一段放新内容，其余同名小节清空。
	result := body
	for i := len(ranges) - 1; i >= 0; i-- {
		filler := "\n\n"
		if i == 0 {
			filler = "\n\n" + next + "\n\n"
		}
		result = result[:ranges[i].start] + filler + result[ranges[i].end:]
	}
	return result
}

// replaceImageCaption 改写图片条目的文案：新文案放最前，原有的纯图片段落与「图中文字」小节保留。
func replaceImageCaption(body, next string) string {
	parts := SplitImageSections(body)
	end := len(body)
	if parts.Recognized != "" {
		if at := strings.Index(body, parts.Recognized); at >= 0 {
			end = at
		}
	}
	var pieces []string
	if next != "" {
		pieces = append(pieces, next)
	}
	for _, block := range reBlankBlocks.Split(body[:end], -1) {
		if block != "" && reImageOnly.MatchString(jsTrim(block)) {
			pieces = append(pieces, block)
		}
	}
	if parts.Recognized != "" {
		pieces = append(pieces, parts.Recognized)
	}
	return strings.Join(pieces, "\n\n")
}

// jsLineTerminator 是 JS 正则多行模式下 ^ 与 $ 认的行终止符。
func jsLineTerminator(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }

// atLineStart 对应多行模式的 ^：位置在文首，或前一个字符是行终止符。
func atLineStart(s string, pos int) bool {
	if pos == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:pos])
	return jsLineTerminator(r)
}

// skipSpace 对应贪婪的 \s*：返回从 pos 起连续空白之后的位置。
func skipSpace(s string, pos int) int {
	for pos < len(s) {
		r, size := utf8.DecodeRuneInString(s[pos:])
		if !jsSpace(r) {
			break
		}
		pos += size
	}
	return pos
}

// trailingBlank 对应 \s*$（多行模式）：先吞掉全部空白，不在文末时按贪婪回退到「下一个字符是行终止符」的最后一个位置，
// 所以标题行后面连着的空行会被吞到只剩最后一个换行。返回匹配结束的位置。
func trailingBlank(s string, pos int) (int, bool) {
	run := skipSpace(s, pos)
	if run == len(s) {
		return run, true
	}
	for p := run; p > pos; {
		_, size := utf8.DecodeLastRuneInString(s[:p])
		p -= size
		if r, _ := utf8.DecodeRuneInString(s[p:]); jsLineTerminator(r) {
			return p, true
		}
	}
	return 0, false
}

// forumHeadingMatches 按 JS 的 matchAll 语义找全部小节标题：/^##\s*(讨论总结|正文|讨论(?:（[^\n]*）)?)\s*$/gm。
// 备选项按顺序试，第一个能接上 \s*$ 的算数；「讨论」后的括号是贪婪的，从该行最后一个「）」往前找。
func forumHeadingMatches(body string) []headingMatch {
	var out []headingMatch
	from := 0
	for from < len(body) {
		rel := strings.Index(body[from:], "##")
		if rel < 0 {
			break
		}
		at := from + rel
		if atLineStart(body, at) {
			if m, ok := matchForumHeading(body, at); ok {
				out = append(out, m)
				from = m.end
				continue
			}
		}
		from = at + 1
	}
	return out
}

// matchForumHeading 在 at（已确认在行首、以 ## 开头）处尝试匹配一个小节标题。
func matchForumHeading(body string, at int) (headingMatch, bool) {
	pos := skipSpace(body, at+2) // 备选项都以非空白字符开头，\s* 只有吞满这一种取法能接上
	rest := body[pos:]
	for _, title := range []string{forumSummaryTitle, forumBodyTitle} {
		if strings.HasPrefix(rest, title) {
			if end, ok := trailingBlank(body, pos+len(title)); ok {
				return headingMatch{at, end, title}, true
			}
		}
	}
	if !strings.HasPrefix(rest, forumTalkTitle) {
		return headingMatch{}, false
	}
	after := pos + len(forumTalkTitle)
	if strings.HasPrefix(body[after:], parenOpenFull) {
		inner := after + len(parenOpenFull)
		lineEnd := len(body)
		if nl := strings.IndexByte(body[inner:], '\n'); nl >= 0 {
			lineEnd = inner + nl
		}
		for limit := lineEnd; limit > inner; {
			c := strings.LastIndex(body[inner:limit], parenCloseFull)
			if c < 0 {
				break
			}
			closeAt := inner + c
			if end, ok := trailingBlank(body, closeAt+len(parenCloseFull)); ok {
				return headingMatch{at, end, body[pos : closeAt+len(parenCloseFull)]}, true
			}
			limit = closeAt // 再往前找更靠前的「）」（对应 [^\n]* 的回退）
		}
	}
	if end, ok := trailingBlank(body, after); ok {
		return headingMatch{at, end, forumTalkTitle}, true
	}
	return headingMatch{}, false
}
