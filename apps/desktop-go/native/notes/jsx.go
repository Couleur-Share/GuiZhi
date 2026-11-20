package notes

// JS 语义小工具：本包要与渲染层 packages/shared/utils 里的 TS 实现逐字节对拍，
// trim 与正则 \s 用 JS 的空白集（Go 的 unicode.IsSpace 多一个 U+0085、少一个 U+FEFF），
// 字符串截取按 UTF-16 码元。与主包、aiflow 里的同名工具各自独立（本包不能反向依赖 package main）。

import (
	"strings"
	"unicode/utf16"
)

// jsSpace 是 JS String.prototype.trim 与正则 \s 共用的空白集（WhiteSpace + LineTerminator）。
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsSpaceClass 是 jsSpace 的正则字符类写法（放进 [...] 里用）：RE2 的 \s 只有 ASCII 空白，会漏掉 U+00A0、全角空格等。
const jsSpaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// sp 对应 JS 正则里的 \s，可以直接拼进正则（其后接量词）。
const sp = "[" + jsSpaceClass + "]"

func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// utf16Slice 对应 s.slice(0, n)，n 按 UTF-16 码元；切口落在代理对中间时该位置落成 U+FFFD（Go 字符串装不下孤立代理项）。
func utf16Slice(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= n {
		return s
	}
	return string(utf16.Decode(u[:n]))
}

// jsDotRest 对应正则里 `(.+)$` 的可匹配条件：非空且不含 \r \n \u2028 \u2029。
func jsDotRest(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, "\r\n\u2028\u2029")
}
