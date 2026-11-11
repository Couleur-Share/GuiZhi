package main

// JS 值模型：用来在 Go 里复刻 TS 里「JSON.parse → 对象展开/改写 → JSON.stringify」这类逻辑，
// 保证输出与 JS 逐字节一致。要点：
//   - 对象保持属性顺序，但「数组下标样式的键」（"0"、"12"…）按数值升序排在其余键之前（JS 规范的 OrdinaryOwnPropertyKeys）。
//   - undefined 与 null 区分；JSON.stringify 丢弃值为 undefined 的属性，数组里的 undefined 写成 null。
//   - 数字按 Number::toString 的规则输出（1e21、1e-7、-0 → 0 等）。
//   - 已知差异：孤立代理项（\ud83d 这类单独出现的转义）在 Go 字符串里无法保存，解析时换成 U+FFFD。

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type jsUndefinedT struct{}
type jsNullT struct{}

var (
	jsUndefined = jsUndefinedT{}
	jsNull      = jsNullT{}
)

// jsObj 保存插入顺序；读写与遍历顺序由 keys() 给出。
type jsObj struct {
	order []string
	m     map[string]any
}

type jsArr []any

func newObj() *jsObj { return &jsObj{m: map[string]any{}} }

// set 对应 obj[k] = v：键已存在则保留原位置。
func (o *jsObj) set(k string, v any) {
	if _, ok := o.m[k]; !ok {
		o.order = append(o.order, k)
	}
	o.m[k] = v
}

func (o *jsObj) get(k string) any {
	if v, ok := o.m[k]; ok {
		return v
	}
	return jsUndefined
}

func isArrayIndexKey(k string) (uint64, bool) {
	if k == "" || (len(k) > 1 && k[0] == '0') || len(k) > 10 {
		return 0, false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil || n >= 4294967295 {
		return 0, false
	}
	return n, true
}

// keys 返回 JS 的自有属性顺序：数组下标键升序在前，其余按插入顺序。
func (o *jsObj) keys() []string {
	var idx []string
	var rest []string
	for _, k := range o.order {
		if _, ok := isArrayIndexKey(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Slice(idx, func(i, j int) bool {
		a, _ := isArrayIndexKey(idx[i])
		b, _ := isArrayIndexKey(idx[j])
		return a < b
	})
	return append(idx, rest...)
}

// spreadInto 对应 `{...src}` 里 src 的部分：对象按属性顺序，数组与字符串按下标，其余（数字、布尔、null、undefined）什么也不加。
func spreadInto(dst *jsObj, src any) {
	switch s := src.(type) {
	case *jsObj:
		for _, k := range s.keys() {
			dst.set(k, s.m[k])
		}
	case jsArr:
		for i, v := range s {
			dst.set(strconv.Itoa(i), v)
		}
	case string:
		for i, u := range toUTF16(s) {
			dst.set(strconv.Itoa(i), unitToString(u))
		}
	}
}

func vTruthy(v any) bool {
	switch x := v.(type) {
	case jsUndefinedT, jsNullT:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true // 对象与数组恒为真
}

func isNullish(v any) bool {
	switch v.(type) {
	case jsUndefinedT, jsNullT:
		return true
	}
	return false
}

// jsIsObject 对应 `typeof v === "object" && v !== null`（数组也是对象）。
func jsIsObject(v any) bool {
	switch v.(type) {
	case *jsObj, jsArr:
		return true
	}
	return false
}

// jsGet 对应 `v.key`：只有对象有自有属性；对 null/undefined 属性访问会抛错，这里由 jsGetStrict 处理。
func jsGet(v any, key string) any {
	if o, ok := v.(*jsObj); ok {
		return o.get(key)
	}
	return jsUndefined
}

// jsOptGet 对应 `v?.key`。
func jsOptGet(v any, key string) any {
	if isNullish(v) {
		return jsUndefined
	}
	return jsGet(v, key)
}

// jsToString 对应 String(v)（用于被隐式转成字符串的位置，如 new URL(v)、数组 join）。
func jsToString(v any) string {
	switch x := v.(type) {
	case jsUndefinedT:
		return "undefined"
	case jsNullT:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return jsNumberString(x)
	case string:
		return x
	case jsArr:
		parts := make([]string, len(x))
		for i, e := range x {
			if isNullish(e) {
				parts[i] = ""
			} else {
				parts[i] = jsToString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// jsNumberString 实现 Number.prototype.toString()（十进制）。
func jsNumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.ddddde±XX
	mant, expStr, _ := strings.Cut(e, "e")
	exp10, _ := strconv.Atoi(expStr)
	digits := strings.Replace(mant, ".", "", 1)
	k := len(digits)
	n := exp10 + 1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	expSign := "+"
	if n-1 < 0 {
		expSign = "-"
	}
	abs := n - 1
	if abs < 0 {
		abs = -abs
	}
	if k == 1 {
		return fmt.Sprintf("%s%se%s%d", sign, digits, expSign, abs)
	}
	return fmt.Sprintf("%s%s.%se%s%d", sign, digits[:1], digits[1:], expSign, abs)
}

// ── JSON.stringify ──

func jsQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if u, ok := wtf8Surrogate(s[i:]); ok {
				fmt.Fprintf(&b, `\u%04x`, u)
				i += 3
				continue
			}
		}
		i += size
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsStringify 对应 JSON.stringify(v)。顶层 undefined 返回 ok=false（JS 返回 undefined）。
func jsStringify(v any) (string, bool) {
	var b strings.Builder
	ok := writeJSON(&b, v, false)
	return b.String(), ok
}

func writeJSON(b *strings.Builder, v any, inArray bool) bool {
	switch x := v.(type) {
	case jsUndefinedT:
		if inArray {
			b.WriteString("null")
			return true
		}
		return false
	case jsNullT:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(jsNumberString(x))
		}
	case string:
		b.WriteString(jsQuote(x))
	case jsArr:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e, true)
		}
		b.WriteByte(']')
	case *jsObj:
		b.WriteByte('{')
		first := true
		for _, k := range x.keys() {
			var sub strings.Builder
			if !writeJSON(&sub, x.m[k], false) {
				continue
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString(jsQuote(k))
			b.WriteByte(':')
			b.WriteString(sub.String())
		}
		b.WriteByte('}')
	default:
		return false
	}
	return true
}

// ── JSON.parse（严格语法）──

type jsonParser struct {
	s   string
	pos int
}

var errJSONSyntax = errors.New("JSON 语法错误")

func jsParse(s string) (any, error) {
	p := &jsonParser{s: s}
	p.ws()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.pos != len(p.s) {
		return nil, errJSONSyntax
	}
	return v, nil
}

func (p *jsonParser) ws() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) value(depth int) (any, error) {
	if depth > 5000 || p.pos >= len(p.s) {
		return nil, errJSONSyntax
	}
	switch c := p.s[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		p.pos++
		arr := jsArr{}
		p.ws()
		if p.pos < len(p.s) && p.s[p.pos] == ']' {
			p.pos++
			return arr, nil
		}
		for {
			p.ws()
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
			p.ws()
			if p.pos >= len(p.s) {
				return nil, errJSONSyntax
			}
			if p.s[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.s[p.pos] == ']' {
				p.pos++
				return arr, nil
			}
			return nil, errJSONSyntax
		}
	case c == '"':
		return p.str()
	case c == 't' && strings.HasPrefix(p.s[p.pos:], "true"):
		p.pos += 4
		return true, nil
	case c == 'f' && strings.HasPrefix(p.s[p.pos:], "false"):
		p.pos += 5
		return false, nil
	case c == 'n' && strings.HasPrefix(p.s[p.pos:], "null"):
		p.pos += 4
		return jsNull, nil
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return nil, errJSONSyntax
}

func (p *jsonParser) object(depth int) (any, error) {
	p.pos++
	o := newObj()
	p.ws()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return o, nil
	}
	for {
		p.ws()
		if p.pos >= len(p.s) || p.s[p.pos] != '"' {
			return nil, errJSONSyntax
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		p.ws()
		if p.pos >= len(p.s) || p.s[p.pos] != ':' {
			return nil, errJSONSyntax
		}
		p.pos++
		p.ws()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		o.set(k.(string), v) // 重复键：后值覆盖，位置保留
		p.ws()
		if p.pos >= len(p.s) {
			return nil, errJSONSyntax
		}
		if p.s[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.s[p.pos] == '}' {
			p.pos++
			return o, nil
		}
		return nil, errJSONSyntax
	}
}

func (p *jsonParser) number() (any, error) {
	start := p.pos
	if p.s[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.s) {
		return nil, errJSONSyntax
	}
	if p.s[p.pos] == '0' {
		p.pos++
	} else if p.s[p.pos] >= '1' && p.s[p.pos] <= '9' {
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return nil, errJSONSyntax
	}
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		p.pos++
		d := p.pos
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == d {
			return nil, errJSONSyntax
		}
	}
	if p.pos < len(p.s) && (p.s[p.pos] == 'e' || p.s[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
			p.pos++
		}
		d := p.pos
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == d {
			return nil, errJSONSyntax
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil && !math.IsInf(f, 0) {
		return nil, errJSONSyntax
	}
	return f, nil // 1e999 解析为 Infinity，与 JS 一致
}

func (p *jsonParser) str() (any, error) {
	p.pos++
	var b strings.Builder
	for {
		if p.pos >= len(p.s) {
			return nil, errJSONSyntax
		}
		c := p.s[p.pos]
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c < 0x20:
			return nil, errJSONSyntax
		case c == '\\':
			p.pos++
			if p.pos >= len(p.s) {
				return nil, errJSONSyntax
			}
			esc := p.s[p.pos]
			p.pos++
			switch esc {
			case '"', '\\', '/':
				b.WriteByte(esc)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				r, ok := p.hex4()
				if !ok {
					return nil, errJSONSyntax
				}
				if utf16.IsSurrogate(r) {
					// 高代理项后紧跟 \uDC00-\uDFFF 才能合成；否则是孤立代理项（已知差异：换成 U+FFFD）
					if r < 0xdc00 && strings.HasPrefix(p.s[p.pos:], `\u`) {
						save := p.pos
						p.pos += 2
						if r2, ok2 := p.hex4(); ok2 && r2 >= 0xdc00 && r2 <= 0xdfff {
							b.WriteRune(utf16.DecodeRune(r, r2))
							continue
						}
						p.pos = save
					}
					b.WriteString(unitToString(uint16(r)))
				} else {
					b.WriteRune(r)
				}
			default:
				return nil, errJSONSyntax
			}
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *jsonParser) hex4() (rune, bool) {
	if p.pos+4 > len(p.s) {
		return 0, false
	}
	n, err := strconv.ParseUint(p.s[p.pos:p.pos+4], 16, 32)
	if err != nil {
		return 0, false
	}
	p.pos += 4
	return rune(n), true
}

// wtf8Surrogate 识别以 WTF-8 保存的孤立代理项：ED [A0-BF] [80-BF]。
func wtf8Surrogate(s string) (uint16, bool) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2] >= 0x80 && s[2] <= 0xBF {
		return uint16(0xD000 | uint16(s[1]&0x3F)<<6 | uint16(s[2]&0x3F)), true
	}
	return 0, false
}

// unitToString 把一个 UTF-16 码元转成字符串；孤立代理项用 WTF-8 表示。
func unitToString(u uint16) string {
	if u >= 0xD800 && u <= 0xDFFF {
		return string([]byte{0xED, 0x80 | byte(u>>6&0x3F), 0x80 | byte(u&0x3F)})
	}
	return string(rune(u))
}

// toUTF16 按 JS 字符串的码元序列展开（识别 WTF-8 孤立代理项）。
func toUTF16(s string) []uint16 {
	var out []uint16
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if u, ok := wtf8Surrogate(s[i:]); ok {
				out = append(out, u)
				i += 3
				continue
			}
		}
		i += size
		if r >= 0x10000 {
			r1, r2 := utf16.EncodeRune(r)
			out = append(out, uint16(r1), uint16(r2))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}
