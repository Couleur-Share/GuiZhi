package main

// 对应 packages/db/src/knowledge.ts 的 toPlainText / makeSnippet / makeSearchSnippet。
// JavaScript 的字符串长度与下标按 UTF-16 码元计，这里同样按 UTF-16 截取，保证与 TS 逐字一致；
// JavaScript 正则的 \s 含 Unicode 空白、多行 ^ 认 \r \u2028 \u2029，而 RE2 都不认，所以单独处理。

import (
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	snippetMaxLength    = 160
	searchSnippetLead   = 30
	snippetSourceLength = 2000
	searchSourceLength  = 12000
)

// jsS 与 JavaScript 正则的 \s 等价的字符类内容（含 \v 与 BOM）。
const jsS = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var (
	reFence      = regexp.MustCompile("(?s)```.*?```")
	reInlineCode = regexp.MustCompile("`([^`]*)`")
	reImage      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reLink       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reEmphasis   = regexp.MustCompile(`[*_~]{1,3}([^*_~]+)[*_~]{1,3}`)
	reSpaces     = regexp.MustCompile(`[` + jsS + `]+`)
	reMetaFirst  = regexp.MustCompile(`^>[` + jsS + `]*平台[:：]`)
	// 多行 ^ 的三条规则：锚在子串开头，由 replaceLineStart 保证只在行首尝试
	reHeading = regexp.MustCompile(`^[` + jsS + `]{0,3}#{1,6}[` + jsS + `]+`)
	reQuote   = regexp.MustCompile(`^[` + jsS + `]{0,3}>[` + jsS + `]?`)
	reBullet  = regexp.MustCompile(`^[` + jsS + `]{0,3}[-*+][` + jsS + `]+`)
)

func isJSLineTerminator(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }

// replaceLineStart 等价于 JS 的 s.replace(/^…/gm, "")：每个行首位置尝试匹配，匹配后从末尾继续。
func replaceLineStart(s string, re *regexp.Regexp) string {
	var out strings.Builder
	prev := rune(-1) // -1 表示字符串开头
	for i := 0; i < len(s); {
		atLineStart := prev == -1 || isJSLineTerminator(prev)
		if atLineStart {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[1] > 0 {
				i += loc[1]
				prev, _ = utf8.DecodeLastRuneInString(s[:i])
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		out.WriteRune(r)
		prev = r
		i += size
	}
	return out.String()
}

// videoMetaBody 对应 parseVideoMetaBlock(content)?.body：首行是 `> 平台：…` 时去掉开头的引用行。
func videoMetaBody(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	if !reMetaFirst.MatchString(lines[0]) {
		return "", false
	}
	end := 0
	for end < len(lines) && strings.HasPrefix(lines[end], ">") {
		end++
	}
	body := strings.Join(lines[end:], "\n")
	return strings.TrimLeft(body, "\n"), true
}

func toPlainText(content string) string {
	source := content
	if body, ok := videoMetaBody(content); ok && strings.TrimFunc(body, jsSpace) != "" {
		source = body
	}
	s := reFence.ReplaceAllString(source, " ")
	s = reInlineCode.ReplaceAllString(s, "$1")
	s = reImage.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = replaceLineStart(s, reHeading)
	s = replaceLineStart(s, reQuote)
	s = replaceLineStart(s, reBullet)
	s = reEmphasis.ReplaceAllString(s, "$1")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimFunc(s, jsSpace)
}

func u16(s string) []uint16     { return utf16.Encode([]rune(s)) }
func fromU16(u []uint16) string { return string(utf16.Decode(u)) }

func makeSnippet(content string) string {
	plain := u16(toPlainText(content))
	if len(plain) > snippetMaxLength {
		return fromU16(plain[:snippetMaxLength]) + "…"
	}
	return fromU16(plain)
}

func indexOfU16(hay, needle []uint16) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func makeSearchSnippet(content, searchTerm string) string {
	plainStr := toPlainText(content)
	plain := u16(plainStr)
	lower := u16(strings.ToLower(plainStr))
	hit := -1
	for _, token := range strings.FieldsFunc(strings.ToLower(searchTerm), jsSpace) {
		at := indexOfU16(lower, u16(token))
		if at >= 0 && (hit < 0 || at < hit) {
			hit = at
		}
	}
	if hit < 0 || hit <= searchSnippetLead {
		return makeSnippet(content)
	}
	start := hit - searchSnippetLead
	end := start + snippetMaxLength
	if end > len(plain) {
		return "…" + fromU16(plain[start:])
	}
	return "…" + fromU16(plain[start:end]) + "…"
}
