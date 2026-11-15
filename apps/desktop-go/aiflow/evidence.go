package aiflow

// 证据链接脱敏（对应 packages/shared/utils/evidence.ts 的 evidenceUrl）：去凭证、去 fragment、去敏感查询参数。
// Wiki 编译的模型身份串里带着脱敏后的接口地址，必须与渲染层逐字节一致，否则旧任务会被误判为「模型已变化」。
//
// 与主包 evidence.go 的 evidenceURL 同源（同样用符合 WHATWG 规范的 URL 解析库）。aiflow 不能 import package main，
// 所以这里留一份只含 URL 的精简实现；切换阶段主包改为薄包装 aiflow 时合并去重。

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

// sensitiveParamRe 对应 /token|key|auth|secret|password|credential|signature|^sig$|^x-amz-|^x-goog-/i。
// 不用 (?i)：Go 的大小写折叠会把开尔文符号 K（U+212A）当成 k、长 s（U+017F）当成 s，
// 而 JS 非 Unicode 模式的 /i 里非 ASCII 字符不会折叠成 ASCII。所以只把 ASCII 字母转小写再匹配。
var sensitiveParamRe = regexp.MustCompile(`token|key|auth|secret|password|credential|signature|^sig$|^x-amz-|^x-goog-`)

// isSensitiveParam 判断查询参数名是否敏感（ASCII 范围内不区分大小写）。
func isSensitiveParam(name string) bool {
	lowered := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, name)
	return sensitiveParamRe.MatchString(lowered)
}

// EvidenceURL 对应 evidenceUrl：不可用（空、解析失败、非 http(s)）返回 ok=false，即 JS 的 undefined。
func EvidenceURL(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	u, err := whatwg.Parse(value)
	if err != nil {
		return "", false
	}
	if p := u.Protocol(); p != "https:" && p != "http:" {
		return "", false
	}
	u.SetUsername("")
	u.SetPassword("")
	u.SetHash("")
	// 对应 `for (const name of [...url.searchParams.keys()]) if (敏感) url.searchParams.delete(name)`。
	// 不用 SearchParams：规范里只有真的发生 delete 才会按 x-www-form-urlencoded 重写查询串，
	// 没有命中时查询串必须原样保留，而第三方库在读取参数时就会重写它。
	if q := strings.TrimPrefix(u.Search(), "?"); q != "" {
		pairs := parseFormURLEncoded(q)
		drop := map[string]bool{}
		for _, p := range pairs {
			if isSensitiveParam(p[0]) {
				drop[p[0]] = true
			}
		}
		if len(drop) > 0 {
			var kept [][2]string
			for _, p := range pairs {
				if !drop[p[0]] {
					kept = append(kept, p)
				}
			}
			u.SetSearch(serializeFormURLEncoded(kept)) // 空串 → 查询串置为 null，href 不再带 "?"
		}
	}
	return u.Href(false), true
}

// parseFormURLEncoded 对应 WHATWG 的 application/x-www-form-urlencoded 解析：
// 按 & 切分、跳过空段、首个 = 分名值、+ 当空格、百分号解码后按 UTF-8 宽松解码。
func parseFormURLEncoded(q string) [][2]string {
	var out [][2]string
	for _, seq := range strings.Split(q, "&") {
		if seq == "" {
			continue
		}
		name, value, _ := strings.Cut(seq, "=")
		out = append(out, [2]string{formDecode(name), formDecode(value)})
	}
	return out
}

func formDecode(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '+':
			b = append(b, ' ')
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b = append(b, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
		default:
			b = append(b, c)
		}
	}
	return lossyUTF8(b)
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// lossyUTF8 实现 WHATWG UTF-8 decode（无 BOM 处理、遇到非法序列输出 U+FFFD）：
// 与 Go 的区别是「被截断的合法前缀」整体只产生一个 U+FFFD，而不是每个字节一个。
func lossyUTF8(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		var need int
		lower, upper := byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lower = 0xA0
			} else if c == 0xED {
				upper = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lower = 0x90
			} else if c == 0xF4 {
				upper = 0x8F
			}
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := i + 1
		ok := true
		for k := 0; k < need; k++ {
			if j >= len(b) || b[j] < lower || b[j] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xBF
			j++
		}
		if ok {
			out.Write(b[i:j])
		} else {
			out.WriteRune(utf8.RuneError)
		}
		i = j
	}
	return out.String()
}

func serializeFormURLEncoded(pairs [][2]string) string {
	enc := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case c == ' ':
				b.WriteByte('+')
			case c == '*' || c == '-' || c == '.' || c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
				b.WriteByte(c)
			default:
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
		return b.String()
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = enc(p[0]) + "=" + enc(p[1])
	}
	return strings.Join(parts, "&")
}
