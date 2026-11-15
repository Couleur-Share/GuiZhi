package aiflow

// 有序 JSON 对象与 JS 风格的序列化。
//
// 请求体要和 TS 的 JSON.stringify 逐字节一致：键序是插入序（删除再添加会排到最后，
// 例如 token 参数重试时 max_completion_tokens → max_tokens），字符串不转义 < > & 与 U+2028/2029，
// 控制字符用小写 \u00xx。encoding/json 都不满足，所以这里自己写一个很小的编码器。

import (
	"encoding/json"
	"math"
	"strings"
)

// obj 是保持插入序的 JSON 对象。
type obj struct {
	keys []string
	vals map[string]any
}

func newObj() *obj { return &obj{vals: map[string]any{}} }

// set 写入键；已存在的键保持原位置（与 JS 对象赋值一致）。
func (o *obj) set(k string, v any) *obj {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

// del 删除键（其余键保持顺序）。
func (o *obj) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *obj) has(k string) bool { _, ok := o.vals[k]; return ok }

// String 返回 JSON.stringify(o) 的等价文本。
func (o *obj) String() string {
	var b strings.Builder
	writeJS(&b, o)
	return b.String()
}

// jsonText 把任意受支持的值序列化成 JSON.stringify 的等价文本。
func jsonText(v any) string {
	var b strings.Builder
	writeJS(&b, v)
	return b.String()
}

func writeJS(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		b.WriteString(jsQuote(x))
	case int:
		b.WriteString(jsNumberString(float64(x)))
	case int64:
		b.WriteString(jsNumberString(float64(x)))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null") // JSON.stringify(NaN) === "null"
		} else {
			b.WriteString(jsNumberString(x))
		}
	case json.RawMessage:
		b.Write(x)
	case *obj:
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsQuote(k))
			b.WriteByte(':')
			writeJS(b, x.vals[k])
		}
		b.WriteByte('}')
	case []string:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsQuote(e))
		}
		b.WriteByte(']')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJS(b, e)
		}
		b.WriteByte(']')
	case []*obj:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJS(b, e)
		}
		b.WriteByte(']')
	default:
		// 不认识的类型走标准库兜底（只会出现在测试夹具里），保证不会静默丢字段
		raw, err := json.Marshal(x)
		if err != nil {
			b.WriteString("null")
			return
		}
		b.Write(raw)
	}
}

// jsonIndent 对应 JSON.stringify(v, null, spaces)：子元素各占一行并缩进，键值之间是 ": "，
// 空数组与空对象仍写成 [] 与 {}。
func jsonIndent(v any, spaces int) string {
	var b strings.Builder
	writeJSIndent(&b, v, strings.Repeat(" ", spaces), 0)
	return b.String()
}

// asAnySlice 把受支持的切片类型统一成 []any，便于递归时共用一份缩进逻辑。
func asAnySlice(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case []string:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out, true
	case []*obj:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out, true
	}
	return nil, false
}

func writeJSIndent(b *strings.Builder, v any, unit string, depth int) {
	newline := func(level int) {
		b.WriteByte('\n')
		for i := 0; i < level; i++ {
			b.WriteString(unit)
		}
	}
	if o, ok := v.(*obj); ok {
		if len(o.keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, k := range o.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(depth + 1)
			b.WriteString(jsQuote(k))
			b.WriteString(": ")
			writeJSIndent(b, o.vals[k], unit, depth+1)
		}
		newline(depth)
		b.WriteByte('}')
		return
	}
	if list, ok := asAnySlice(v); ok {
		if len(list) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, e := range list {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(depth + 1)
			writeJSIndent(b, e, unit, depth+1)
		}
		newline(depth)
		b.WriteByte(']')
		return
	}
	writeJS(b, v)
}

// jsQuote 对应 JSON.stringify(string)：不转义 < > & 与 U+2028/2029，控制字符用小写 \u00xx，
// 非法 UTF-8 字节按 U+FFFD 输出。
func jsQuote(s string) string {
	const hexd = "0123456789abcdef"
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
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
				b.WriteString(`\u00`)
				b.WriteByte(hexd[r>>4&0xF])
				b.WriteByte(hexd[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
