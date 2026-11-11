package main

// 对应 packages/shared/utils/evidence.ts 与 evidence-sanitizer.ts：
// 问答历史里的链接与证据快照在落库前脱敏（去凭证、去 fragment、去敏感查询参数），并按「来源清除」标记打码。
// 输出必须与 JS 逐字节一致（含属性顺序、undefined 丢弃、对象展开的怪异行为），见 jsvalue.go。

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

var (
	// JS 的 \s 与 RE2 的 \s 不同，这里显式列出 JS 的空白集合
	evidenceURLRe = regexp.MustCompile(`https?://[^\t\n\x{0b}\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}<>"）)\]]+`)
	// JS 的 /.../i 使用简单大小写折叠，与 Go 的 (?i) 一致
	sensitiveParamRe = regexp.MustCompile(`(?i)token|key|auth|secret|password|credential|signature|^sig$|^x-amz-|^x-goog-`)
)

var errNotString = errors.New("text.replace is not a function")

// evidenceURL 对应 evidenceUrl(value)：不可用返回 ok=false（JS 的 undefined）。
func evidenceURL(value any) (string, bool) {
	if !vTruthy(value) {
		return "", false
	}
	u, err := whatwg.Parse(jsToString(value))
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
			if sensitiveParamRe.MatchString(p[0]) {
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

// evidenceText 对应 evidenceText(text)：text 必须是字符串，否则 JS 在 text.replace 处抛 TypeError。
func evidenceText(text any) (string, error) {
	s, ok := text.(string)
	if !ok {
		return "", errNotString
	}
	return evidenceURLRe.ReplaceAllStringFunc(s, func(m string) string {
		if out, ok := evidenceURL(m); ok {
			return out
		}
		return "[链接不可用]"
	}), nil
}

// sanitizeEvidenceTarget 对应同名函数：真值才展开；selection 为真值时必须是字符串。
func sanitizeEvidenceTarget(target any) (any, error) {
	if !vTruthy(target) {
		return target, nil
	}
	out := newObj()
	spreadInto(out, target)
	if sel := jsGet(target, "selection"); vTruthy(sel) {
		t, err := evidenceText(sel)
		if err != nil {
			return nil, err
		}
		out.set("selection", t)
	}
	return out, nil
}

var errNotFunction = errors.New("x.some is not a function")

// sanitizeEvidenceJSON 对应 sanitizeEvidenceJson(json, cleared)。
func sanitizeEvidenceJSON(raw string, cleared map[string]bool) (string, error) {
	parsed, err := jsParse(raw)
	if err != nil {
		return "", err
	}
	messages, ok := parsed.(jsArr)
	if !ok {
		return "", errors.New("会话消息格式无效")
	}
	has := func(id any) bool {
		s, ok := id.(string)
		return ok && cleared[s]
	}
	// someCleared 对应 `(a ?? b ?? []).some(id => cleared.has(id))`
	someCleared := func(raw any) (bool, error) {
		list := jsGet(raw, "sourceItemIds")
		if isNullish(list) {
			list = jsOptGet(jsGet(raw, "evidence"), "sourceItemIds")
		}
		if isNullish(list) {
			return false, nil
		}
		arr, ok := list.(jsArr)
		if !ok {
			return false, errNotFunction
		}
		for _, e := range arr {
			if has(e) {
				return true, nil
			}
		}
		return false, nil
	}

	var source func(raw any, inherited any) (any, error)
	source = func(raw any, inherited any) (any, error) {
		if !vTruthy(raw) || !jsIsObject(raw) {
			return raw, nil
		}
		// id = raw.sourceId ?? raw.target?.itemId ?? (raw.kind === 'item' ? raw.refId : undefined) ?? inheritedId
		id := jsGet(raw, "sourceId")
		if isNullish(id) {
			id = jsOptGet(jsGet(raw, "target"), "itemId")
		}
		if isNullish(id) {
			if k, ok := jsGet(raw, "kind").(string); ok && k == "item" {
				id = jsGet(raw, "refId")
			} else {
				id = jsUndefined
			}
		}
		if isNullish(id) {
			id = inherited
		}
		hit := vTruthy(id) && has(id)
		if !hit {
			sc, err := someCleared(raw)
			if err != nil {
				return nil, err
			}
			hit = sc
		}
		if hit {
			out := newObj()
			out.set("ordinal", jsGet(raw, "ordinal"))
			out.set("kind", jsGet(raw, "kind"))
			out.set("refId", jsGet(raw, "refId"))
			out.set("sourceId", id)
			out.set("title", "来源已清除")
			out.set("cleared", true)
			if ev := jsGet(raw, "evidence"); vTruthy(ev) {
				e := newObj()
				e.set("version", float64(1))
				e.set("kind", jsGet(raw, "kind"))
				e.set("sourceId", id)
				e.set("title", "来源已清除")
				e.set("text", "")
				e.set("fingerprint", "")
				e.set("capturedAt", jsGet(ev, "capturedAt"))
				e.set("reviewStatus", "clear")
				e.set("reviewReasons", jsArr{})
				e.set("cleared", true)
				out.set("evidence", e)
			}
			return out, nil
		}
		out := newObj()
		spreadInto(out, raw)
		if t, ok := jsGet(raw, "text").(string); ok {
			s, err := evidenceText(t)
			if err != nil {
				return nil, err
			}
			out.set("text", s)
		}
		if u := jsGet(raw, "url"); vTruthy(u) {
			if s, ok := evidenceURL(u); ok {
				out.set("url", s)
			} else {
				out.set("url", jsUndefined)
			}
		}
		if t := jsGet(raw, "target"); vTruthy(t) {
			v, err := sanitizeEvidenceTarget(t)
			if err != nil {
				return nil, err
			}
			out.set("target", v)
		}
		if ev := jsGet(raw, "evidence"); vTruthy(ev) {
			v, err := source(ev, id)
			if err != nil {
				return nil, err
			}
			out.set("evidence", v)
		}
		return out, nil
	}

	mapSources := func(list jsArr, inherited any) (jsArr, error) {
		out := make(jsArr, len(list))
		for i, s := range list {
			v, err := source(s, inherited)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}

	result := make(jsArr, len(messages))
	for i, message := range messages {
		if !vTruthy(message) || !jsIsObject(message) {
			result[i] = message
			continue
		}
		context := jsGet(message, "context")
		// contextCleared = context?.target?.itemId && cleared.has(context.target.itemId)
		itemID := jsOptGet(jsOptGet(context, "target"), "itemId")
		var contextCleared any
		if vTruthy(itemID) {
			contextCleared = has(itemID)
		} else {
			contextCleared = itemID
		}
		out := newObj()
		spreadInto(out, message)
		for _, key := range []string{"evidenceSources", "sources"} {
			if list, ok := jsGet(message, key).(jsArr); ok {
				m, err := mapSources(list, jsUndefined)
				if err != nil {
					return "", err
				}
				out.set(key, m)
			}
		}
		if vTruthy(context) {
			out.set("evidenceCleared", contextCleared)
			if vTruthy(contextCleared) {
				out.set("context", jsUndefined)
			} else {
				c := newObj()
				spreadInto(c, context)
				t, err := sanitizeEvidenceTarget(jsGet(context, "target"))
				if err != nil {
					return "", err
				}
				c.set("target", t)
				var sources any = jsUndefined
				if srcs := jsGet(context, "sources"); !isNullish(srcs) {
					list, ok := srcs.(jsArr)
					if !ok {
						return "", errors.New("context.sources.map is not a function")
					}
					inherited := jsOptGet(jsGet(context, "target"), "itemId")
					m, err := mapSources(list, inherited)
					if err != nil {
						return "", err
					}
					sources = m
				}
				c.set("sources", sources)
				out.set("context", c)
			}
		}
		result[i] = out
	}
	s, _ := jsStringify(result)
	return s, nil
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
		case c == '%' && i+2 < len(s)+0 && isHex(s[i+1]) && isHex(s[i+2]):
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
