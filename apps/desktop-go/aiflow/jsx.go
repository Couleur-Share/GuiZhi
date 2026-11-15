package aiflow

// JS 语义小工具。
//
// aiflow 要与渲染层的 TS 实现逐字节对拍：字符串长度与截取按 UTF-16 码元，trim 与正则 \s 用 JS 的空白集
// （Go 的 unicode.IsSpace 多一个 U+0085、少一个 U+FEFF），大小写转换走完整的 Unicode 映射（ß → SS）。
// 这些是叶子工具，每个都有 jsx_test.go 里的用例固定；与主包 jsvalue.go 的同名工具各自独立，
// 因为 aiflow 不能反向依赖 package main。
//
// 已知且有意的差异：JS 字符串可以含孤立代理项（把 emoji 从中间切开），Go 字符串不行——
// 这种位置统一落成 U+FFFD。JS 版发出去的是孤立代理项转义，部分服务商会拒收，U+FFFD 对模型更友好。

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// jsSpace 是 JS String.prototype.trim 与正则 \s 共用的空白集（WhiteSpace + LineTerminator）。
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsSpaceClass 是 jsSpace 的正则字符类写法（放进 [...] 里用），给需要 \s 的 JS 语义的 Go 正则用——
// RE2 的 \s 只有 ASCII 空白，会漏掉 U+00A0、全角空格等。
const jsSpaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

func jsTrim(s string) string      { return strings.TrimFunc(s, jsSpace) }
func jsTrimStart(s string) string { return strings.TrimLeftFunc(s, jsSpace) }

// u16Len 对应 s.length（UTF-16 码元数）。
func u16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// u16Slice 对应 s.slice(from, to)，下标按 UTF-16 码元；越界按 JS 规则截到边界。
// 切口落在代理对中间时该位置输出 U+FFFD（见文件头说明）。
func u16Slice(s string, from, to int) string {
	if from < 0 {
		from = 0
	}
	if to <= from {
		return ""
	}
	if len(s) <= from { // 字节数不超过起点，码元数更不可能超过
		return ""
	}
	var b strings.Builder
	unit := 0
	for _, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		next := unit + w
		switch {
		case next <= from:
		case unit >= to:
			return b.String()
		case unit >= from && next <= to:
			b.WriteRune(r)
		default:
			b.WriteRune(utf8.RuneError)
		}
		unit = next
	}
	return b.String()
}

// u16Head 对应 s.slice(0, n)。
func u16Head(s string, n int) string { return u16Slice(s, 0, n) }

// u16At 返回第 index 个码元；越界返回 ok=false。
func u16At(s string, index int) (uint16, bool) {
	if index < 0 {
		return 0, false
	}
	unit := 0
	for _, r := range s {
		if r >= 0x10000 {
			hi, lo := utf16.EncodeRune(r)
			if unit == index {
				return uint16(hi), true
			}
			if unit+1 == index {
				return uint16(lo), true
			}
			unit += 2
			continue
		}
		if unit == index {
			return uint16(r), true
		}
		unit++
	}
	return 0, false
}

// jsUpper / jsLower 对应 toUpperCase / toLowerCase（与区域无关的完整 Unicode 映射，含 ß→SS、末尾 Σ→ς）。
func jsUpper(s string) string { return cases.Upper(language.Und).String(s) }
func jsLower(s string) string { return cases.Lower(language.Und).String(s) }

// asciiCollationOrder 是 ICU 根排序（Node 的 localeCompare 默认）下可打印 ASCII 的升序：
// 空白 < 标点 < 符号 < 数字 < 字母，同一字母小写在大写之前。由 Node 实测得到（见 jsx_test.go 的核对用例）。
const asciiCollationOrder = " _-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$0123456789aAbBcCdDeEfFgGhHiIjJkKlLmMnNoOpPqQrRsStTuUvVwWxXyYzZ"

var asciiPrimary, asciiTertiary [128]int

func init() {
	primary := 0
	for i := 0; i < len(asciiCollationOrder); i++ {
		c := asciiCollationOrder[i]
		lower := c
		if c >= 'A' && c <= 'Z' {
			lower = c + 32
			asciiTertiary[c] = 1
			asciiPrimary[c] = asciiPrimary[lower]
			continue
		}
		primary++
		asciiPrimary[c] = primary
	}
}

// jsLocaleCompare 近似 a.localeCompare(b)：ASCII 部分与 ICU 根排序一致（模型 id 基本都是 ASCII），
// 非 ASCII 字符按码点排在 ASCII 之后——中文按拼音排序之类的区域规则不复刻。
func jsLocaleCompare(a, b string) int {
	rank := func(r rune) int {
		if r < 128 && asciiPrimary[r] != 0 {
			return asciiPrimary[r]
		}
		return 1000 + int(r)
	}
	ra, rb := []rune(a), []rune(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		if x, y := rank(ra[i]), rank(rb[i]); x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	if len(ra) != len(rb) {
		if len(ra) < len(rb) {
			return -1
		}
		return 1
	}
	for i := range ra { // 主权重全等：再按大小写区分，小写在前
		var x, y int
		if ra[i] < 128 {
			x = asciiTertiary[ra[i]]
		}
		if rb[i] < 128 {
			y = asciiTertiary[rb[i]]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// jsNFKC 对应 s.normalize("NFKC")。
func jsNFKC(s string) string { return norm.NFKC.String(s) }

// jsCollapseSpaces 对应 s.replace(/\s+/g, repl)。
func jsCollapseSpaces(s, repl string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if jsSpace(r) {
			if !inSpace {
				b.WriteString(repl)
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// jsRemoveSpaces 对应 s.replace(/\s+/g, "")。
func jsRemoveSpaces(s string) string { return jsCollapseSpaces(s, "") }

// jsFields 对应 s.split(/\s+/).filter(Boolean)。
func jsFields(s string) []string { return strings.FieldsFunc(s, jsSpace) }

// jsTruthy 对应 JS 真值判断（只覆盖 JSON 解码后会出现的类型）。
func jsTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	}
	return true // 对象、数组
}

// jsToString 对应模板字符串里的 ${v}：覆盖 JSON 解码后的类型。
func jsToString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return x
	case float64:
		return jsNumberString(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e == nil {
				parts[i] = ""
			} else {
				parts[i] = jsToString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// jsNumberString 对应 Number.prototype.toString（十进制）。
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
	abs := math.Abs(f)
	if abs >= 1e21 || abs < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64) // 如 1e+21、1.5e-07
		mant, exp, _ := strings.Cut(s, "e")
		sign := exp[0]
		digits := strings.TrimLeft(exp[1:], "0")
		if digits == "" {
			digits = "0"
		}
		return mant + "e" + string(sign) + digits
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// jsNumber 近似 Number(v)；ok=false 表示 NaN。
func jsNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x)
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, true
	case string:
		s := jsTrim(x)
		switch {
		case s == "":
			return 0, true
		case s == "Infinity" || s == "+Infinity":
			return math.Inf(1), true
		case s == "-Infinity":
			return math.Inf(-1), true
		case reJSRadix.MatchString(s):
			return parseRadixLiteral(s)
		case !reJSDecimal.MatchString(s):
			return 0, false // 含 "inf"/"nan"/十六进制浮点等 ParseFloat 认而 JS 不认的写法
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// reJSDecimal 是 JS 十进制数字字面量（StrDecimalLiteral，不含 Infinity）。
var reJSDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// reJSRadix 是 JS 的非十进制整数字面量：0x / 0o / 0b，不允许符号与分隔符。
var reJSRadix = regexp.MustCompile(`^0(?:[xX][0-9a-fA-F]+|[oO][0-7]+|[bB][01]+)$`)

func parseRadixLiteral(s string) (float64, bool) {
	base := 16
	switch s[1] {
	case 'o', 'O':
		base = 8
	case 'b', 'B':
		base = 2
	}
	n, ok := new(big.Int).SetString(s[2:], base)
	if !ok {
		return 0, false
	}
	f, _ := new(big.Float).SetInt(n).Float64() // 就近舍入，与 JS 的 MV→Number 一致
	return f, true
}

// numberOrZero 对应 `Number(v) || 0`。
func numberOrZero(v any) float64 {
	f, ok := jsNumber(v)
	if !ok || f == 0 {
		return 0
	}
	return f
}
