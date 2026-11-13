package main

// 对应 packages/db/src/fts.ts：FTS5 中文按字分词预处理与 MATCH 查询构造。
// 行为必须与 TS 逐字一致，由 library_test.go 对着 TS 生成的 golden.json 校验。

import (
	"sort"
	"strings"
	"unicode"
)

// isCjkChar 对应 CJK_PATTERN：/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\u3040-\u30ff\uac00-\ud7af]/
func isCjkChar(r rune) bool {
	switch {
	case r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0x3040 && r <= 0x30ff,
		r >= 0xac00 && r <= 0xd7af:
		return true
	}
	return false
}

// jsSpace 复刻 JavaScript 正则 \s 与 String.prototype.trim 的空白集合，
// 与 Go 的 unicode.IsSpace 在 U+0085、U+FEFF 上不同。
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// SegmentTextForFts 把文本预处理为「CJK 逐字分隔、其余词保持原样」的可索引形式。
func SegmentTextForFts(text string) string {
	if text == "" {
		return ""
	}
	var parts []string
	var ascii strings.Builder
	flush := func() {
		parts = append(parts, strings.FieldsFunc(ascii.String(), jsSpace)...)
		ascii.Reset()
	}
	for _, r := range text {
		if isCjkChar(r) {
			flush()
			parts = append(parts, string(r))
		} else {
			ascii.WriteRune(r)
		}
	}
	flush()
	return strings.Join(parts, " ")
}

func quoteFtsString(v string) string { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }

// hasSearchableToken：纯标点片段分词后是空 phrase，会把整个 AND 查询清零。
func hasSearchableToken(v string) bool {
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

// FtsMatchMode 对应 TS 的 "phrase" | "recall"。
type FtsMatchMode string

const (
	FtsPhrase FtsMatchMode = "phrase"
	FtsRecall FtsMatchMode = "recall"
)

const maxRecallClauses = 8

var cjkStopwords = func() [][]rune {
	words := []string{
		"为什么", "是不是", "怎么样", "怎么办",
		"怎么", "怎样", "如何", "什么", "哪些", "哪个", "多少",
		"可以", "能否", "需要", "应该", "我们", "你们", "他们",
		"这个", "那个", "这些", "那些", "一下", "一个", "没有",
		"以及", "还是", "或者", "并且", "但是", "因为", "所以",
		"的", "了", "着", "是", "在", "和", "与", "及", "或",
		"吗", "呢", "吧", "啊", "有", "会", "要", "对", "把",
		"被", "从", "到", "给", "让", "用", "跟", "向", "并",
		"都", "也", "又", "很", "太", "就", "才", "再", "还",
	}
	rs := make([][]rune, len(words))
	for i, w := range words {
		rs[i] = []rune(w)
	}
	// 按长度倒序、同长度保持原顺序（JS 的 sort 自 ES2019 起稳定）
	sort.SliceStable(rs, func(a, b int) bool { return len(rs[a]) > len(rs[b]) })
	return rs
}()

func hasPrefixRunes(rest, word []rune) bool {
	if len(rest) < len(word) {
		return false
	}
	for i, r := range word {
		if rest[i] != r {
			return false
		}
	}
	return true
}

// splitCjkRunForRecall 按虚词切开一段连续中文，返回实词片段。
func splitCjkRunForRecall(run string) []string {
	chars := []rune(run)
	var segments []string
	var buf []rune
	flush := func() {
		if len(buf) > 0 {
			segments = append(segments, string(buf))
			buf = nil
		}
	}
	for i := 0; i < len(chars); {
		matched := 0
		for _, w := range cjkStopwords {
			if hasPrefixRunes(chars[i:], w) {
				matched = len(w)
				break
			}
		}
		if matched > 0 {
			flush()
			i += matched
			continue
		}
		buf = append(buf, chars[i])
		i++
	}
	flush()
	return segments
}

func spacedRunes(s string) string {
	rs := []rune(s)
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

// BuildFtsMatchQuery 把用户搜索串构造成 FTS5 MATCH 查询；返回 ok=false 表示没有可检索内容。
func BuildFtsMatchQuery(search string, mode FtsMatchMode) (string, bool) {
	trimmed := strings.TrimFunc(search, jsSpace)
	if trimmed == "" {
		return "", false
	}
	var clauses []string
	var cjkRun []rune
	var ascii strings.Builder

	flushCjk := func() {
		if len(cjkRun) == 0 {
			return
		}
		run := string(cjkRun)
		cjkRun = nil
		if mode == FtsPhrase {
			clauses = append(clauses, quoteFtsString(spacedRunes(run)))
			return
		}
		segments := splitCjkRunForRecall(run)
		var meaningful []string
		for _, s := range segments {
			if len([]rune(s)) > 1 {
				meaningful = append(meaningful, s)
			}
		}
		kept := meaningful
		if len(kept) == 0 {
			if len(segments) > 0 {
				kept = segments
			} else {
				kept = []string{run}
			}
		}
		for _, s := range kept {
			clauses = append(clauses, quoteFtsString(spacedRunes(s)))
		}
	}
	flushAscii := func() {
		word := strings.TrimFunc(ascii.String(), jsSpace)
		if word != "" && hasSearchableToken(word) {
			clauses = append(clauses, quoteFtsString(word)+"*")
		}
		ascii.Reset()
	}

	for _, r := range trimmed {
		switch {
		case isCjkChar(r):
			flushAscii()
			cjkRun = append(cjkRun, r)
		case jsSpace(r):
			flushAscii()
			flushCjk()
		default:
			flushCjk()
			ascii.WriteRune(r)
		}
	}
	flushAscii()
	flushCjk()

	if len(clauses) == 0 {
		return "", false
	}
	if mode == FtsPhrase {
		return strings.Join(clauses, " AND "), true
	}
	if len(clauses) > maxRecallClauses {
		clauses = clauses[:maxRecallClauses]
	}
	return strings.Join(clauses, " OR "), true
}
