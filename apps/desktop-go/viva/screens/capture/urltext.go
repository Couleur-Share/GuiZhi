package capture

// 从自由文本里抠 http(s) 链接：渲染层 packages/shared/utils/url-text.ts 的 Go 对应实现。
// 那些尾部修剪规则是一条条实测踩出来的；改动必须与 TS 同步，并由 urltext_test.go 的表守住。

import (
	"strings"
	"unicode/utf8"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

// jsSpace 是 ECMAScript 的 WhiteSpace 与 LineTerminator（`\s` 与 String.prototype.trim 共用的集合）。
// 与 Go 的 unicode.IsSpace 不同：JavaScript 含 U+FEFF、不含 U+0085。
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// jsTrim 是 JavaScript 的 String.prototype.trim。
func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// jsFields 是 `s.split(/\s+/).filter(Boolean)`：按 JavaScript 的空白切分，丢掉空段。
func jsFields(s string) []string { return strings.FieldsFunc(s, jsSpace) }

// urlStopChars 是正文里切链接时要排除的字符（空白之外）：成对包裹符与全角句读。
// 全角句读必须排除——小红书口令的逗号紧贴链接；全角字母数字不排除，路径里出现中文是合法的。
const urlStopChars = "<>\"'`" +
	"\uff0c\u3002\uff01\uff1f\uff1b\uff1a\u3001\uff08\uff09" + // ，。！？；：、（）
	"\u3010\u3011\u300a\u300b\u300c\u300d\u300e\u300f" + // 【】《》「」『』
	"\u2026\u201c\u201d\u2018\u2019" // …“”’

// trailingPunctuation 是跟在链接后面时属于句子而非 URL 的半角句读（`/` `#` 之类不在其中）。
const trailingPunctuation = ".,;:!?"

// hasASCIIPrefixFold 报告 s 是否以 prefix 开头，只对 ASCII 字母不分大小写
// （JavaScript 的 /i 在非 Unicode 模式下不会把 U+017F 当成 s，strings.EqualFold 会）。
func hasASCIIPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

// IsHTTPURLLike 报告输入整体是否是单个 http(s) 链接（isHttpUrlLike）：两端空白不算，内部不能有空白，
// 必须以 http:// 或 https:// 开头（不分大小写），且能被 WHATWG URL 解析器解析成 http(s) 协议。
func IsHTTPURLLike(value string) bool {
	trimmed := jsTrim(value)
	if trimmed == "" || strings.IndexFunc(trimmed, jsSpace) >= 0 {
		return false
	}
	if !hasASCIIPrefixFold(trimmed, "http://") && !hasASCIIPrefixFold(trimmed, "https://") {
		return false
	}
	u, err := whatwg.Parse(trimmed)
	return err == nil && (u.Protocol() == "http:" || u.Protocol() == "https:")
}

// urlTailUnbalanced 报告末尾的右括号是否多出来了：`/wiki/Foo_(bar)` 的右括号要留，`(见 https://x.com/a)` 里多出来的要去掉。
func urlTailUnbalanced(url string) bool {
	switch last, _ := utf8.DecodeLastRuneInString(url); last {
	case ')':
		return strings.Count(url, ")") > strings.Count(url, "(")
	case ']':
		return strings.Count(url, "]") > strings.Count(url, "[")
	}
	return false
}

// trimURLTail 修剪链接尾巴上的句读与多出来的右括号。
func trimURLTail(value string) string {
	url := value
	for url != "" {
		last, size := utf8.DecodeLastRuneInString(url)
		if !strings.ContainsRune(trailingPunctuation, last) && !urlTailUnbalanced(url) {
			break
		}
		url = url[:len(url)-size]
	}
	return url
}

// ExtractURLsFromText 从一段文本里按出现顺序抠出去重后的 http(s) 链接（extractUrlsFromText）。
// 对应正则 /https?:\/\/[^\s<>"'`，。！？；：、（）【】《》「」『』…“”‘’]+/gi：链接体至少一个字符。
func ExtractURLsFromText(value string) []string {
	var urls []string
	seen := map[string]bool{}
	for i := 0; i < len(value); {
		n := 0
		switch rest := value[i:]; {
		case hasASCIIPrefixFold(rest, "https://"):
			n = len("https://")
		case hasASCIIPrefixFold(rest, "http://"):
			n = len("http://")
		}
		if n == 0 {
			i++
			continue
		}
		j := i + n
		for j < len(value) {
			r, size := utf8.DecodeRuneInString(value[j:])
			if jsSpace(r) || strings.ContainsRune(urlStopChars, r) {
				break
			}
			j += size
		}
		if j == i+n { // [^…]+ 至少一个字符
			i += n
			continue
		}
		url := trimURLTail(value[i:j])
		if IsHTTPURLLike(url) && !seen[url] {
			seen[url] = true
			urls = append(urls, url)
		}
		i = j
	}
	return urls
}
