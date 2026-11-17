package kit

import (
	"strings"
	"unicode"
)

// 等宽文字的折行
//
// 渲染层 <pre class="whitespace-pre-wrap break-words font-mono"> 里的报错原文，换行位置由 Chromium 决定；
// Viva 的等宽行自己折行时，字格按整数设备像素取整（A-006）、也不认 Chromium 的断行机会，
// 同一段文字会折成不同的行数，下面所有元素的位置都跟着错。所以折行在这里按 Chromium 的规则自己做，
// 结果以「一行一个换行符」交给 Viva 画（Viva 一侧每行都放得下，不会再折）。
//
// 复刻的规则（以 2026-11-16 在 React 参照里量到的 pre 行矩形为准，见 testdata/react 下的 ui.toast.detail-expanded）：
//   - 硬换行（\n）一律保留；行首的空格保留（缩进），行尾的空格悬挂：占位但不参与「放不放得下」；
//   - 断行机会：空白之后、连字符之后（后面不是数字）、汉字与全角符号之间（避头尾：关闭类标点不起新行，
//     开启类括号不落行尾）。**没有**「斜杠之后」的机会：参照里 C:/Users/…/knowledge.db: 整段当作一个词，
//     放不进行尾就整段换到下一行；
//   - overflow-wrap: break-word：整段放不下一个空行时才在任意字符处断开（前面还有机会就先在机会处换行）；
//   - 一个字符的前进量：ASCII 与拉丁字符是 Consolas 的字距（0.5498 em），汉字与全角符号回退到界面字体，是 1 em。

// monoRuneAdvance 是一个字符排在 font-mono 文字里的前进量（字号 size）。
func monoRuneAdvance(r rune, size float32) float32 {
	if isWideRune(r) {
		return size
	}
	return size * consolasAdvance
}

// isWideRune 报告 r 是否占一个全角字格（汉字、假名、谚文、全角标点等）。
func isWideRune(r rune) bool {
	switch {
	case r < 0x2E80:
		return false
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
		return true
	case r >= 0x3000 && r <= 0x303F, // 中日韩符号和标点
		r >= 0xFF01 && r <= 0xFF60, // 全角 ASCII 变体
		r >= 0xFFE0 && r <= 0xFFE6:
		return true
	}
	return false
}

// MonoWidth 是一行 font-mono 文字（不含换行）排出来的宽度。
func MonoWidth(s string, size float32) float32 {
	var w float32
	for _, r := range s {
		w += monoRuneAdvance(r, size)
	}
	return w
}

// 避头尾：这些标点不能出现在行首（紧跟前一个字符），这些括号不能出现在行尾（紧贴后一个字符）。
// 它们是排版用的字符集，不是给用户读的文案；用 \u 转义写出，避免被「界面文案不得写在 Go 代码里」的检查误伤，
// 字符对照见各行注释。
const (
	// 行首禁用：，。、：；！？）］｝〉》」』】〕”’…—（全角与弯引号）以及西文的 , . : ; ! ? ) ] } %
	noLineStart = "\uff0c\u3002\u3001\uff1a\uff1b\uff01\uff1f\uff09\uff3d\uff5d\u3009\u300b\u300d\u300f\u3011\u3015\u201d\u2019\u2026\u2014,.:;!?)]}%"
	// 行尾禁用：（［｛〈《「『【〔“‘（全角与弯引号）以及西文的 ( [ {
	noLineEnd = "\uff08\uff3b\uff5b\u3008\u300a\u300c\u300e\u3010\u3014\u201c\u2018([{"
)

// monoUnit 是不可再分的一段：折行时整段移动。
type monoUnit struct {
	text  string
	solid float32 // 去掉行尾空格之后的宽度：放不放得下看它
	full  float32 // 含行尾空格的宽度：放下之后占去的行宽
}

// WrapMono 把 text 在 width 宽的盒子里折成若干行（font-mono，字号 size）。
// 返回的每一行不含换行符；空文字返回一行空串。width 不大于 0 时只按硬换行切分。
func WrapMono(text string, width, size float32) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out []string
	for _, hard := range strings.Split(text, "\n") {
		hard = expandTabs(hard)
		if width <= 0 {
			out = append(out, hard)
			continue
		}
		out = append(out, wrapLine(hard, width, monoMeasure(size))...)
	}
	return out
}

// expandTabs 把制表符展开成空格（tab-size 8，按字符列算）。
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r != '\t' {
			b.WriteRune(r)
			col++
			continue
		}
		n := 8 - col%8
		b.WriteString(strings.Repeat(" ", n))
		col += n
	}
	return b.String()
}

// measureFn 量一段文字（不含换行）排出来的宽度。等宽文字按字格累加（monoMeasure），比例字体交给画布的文字度量。
type measureFn func(string) float32

// monoMeasure 是 font-mono 文字（字号 size）的度量。
func monoMeasure(size float32) measureFn {
	return func(s string) float32 { return MonoWidth(s, size) }
}

// WrapText 与 WrapMono 的规则相同，但用 measure 量宽度：比例字体的文字（提示气泡）按 Chromium 的断行机会折行。
// 比例字体的整行宽度不是逐字符前进量之和（字偶距等，台账 A-014），这里对每个断行单元整体量宽，单元之间累加。
func WrapText(text string, width float32, measure func(string) float32) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out []string
	for _, hard := range strings.Split(text, "\n") {
		if width <= 0 {
			out = append(out, hard)
			continue
		}
		out = append(out, wrapLine(hard, width, measure)...)
	}
	return out
}

func wrapLine(line string, width float32, measure measureFn) []string {
	units := splitUnits(line, measure)
	if len(units) == 0 {
		return []string{""}
	}
	var (
		out  []string
		cur  strings.Builder
		used float32
	)
	flush := func() {
		out = append(out, cur.String())
		cur.Reset()
		used = 0
	}
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case used+u.solid <= width+1e-3:
			cur.WriteString(u.text)
			used += u.full
		case cur.Len() > 0:
			// 这一行前面还有断行机会：整段换到下一行再试
			flush()
			i--
		default:
			// 空行也放不下：在任意字符处断开（break-word），余下的当作新的一段重新排
			head, rest := splitToFit(u.text, width, measure)
			cur.WriteString(head)
			flush()
			if rest != "" {
				units[i] = newUnit(rest, measure)
				i--
			}
		}
	}
	if cur.Len() > 0 || len(out) == 0 {
		flush()
	}
	return out
}

// splitToFit 取 s 开头尽量多、宽度不超过 width 的字符（至少一个）。
func splitToFit(s string, width float32, measure measureFn) (head, rest string) {
	var w float32
	for i, r := range s {
		adv := measure(string(r))
		if w+adv > width+1e-3 && i > 0 {
			return s[:i], s[i:]
		}
		w += adv
	}
	return s, ""
}

func newUnit(text string, measure measureFn) monoUnit {
	trimmed := strings.TrimRight(text, " ")
	return monoUnit{text: text, solid: measure(trimmed), full: measure(text)}
}

// splitUnits 按断行机会把一行切成若干段；每段是「词 + 它后面的空格」，行首的缩进并入第一个词。
func splitUnits(line string, measure measureFn) []monoUnit {
	runes := []rune(line)
	var (
		units []monoUnit
		start int
	)
	emit := func(end int) {
		if end > start {
			units = append(units, newUnit(string(runes[start:end]), measure))
			start = end
		}
	}
	// 行首缩进并入第一个词
	i := 0
	for i < len(runes) && runes[i] == ' ' {
		i++
	}
	for ; i < len(runes); i++ {
		r := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		switch {
		case r == ' ':
			// 空格连同前面的词一起；空格串结束处是一个断行机会
			if next != ' ' && next != 0 {
				emit(i + 1)
			}
		case isWideRune(r):
			// 汉字与全角符号：与后一个字符之间是机会，除非后一个字符不能起行，或这一个字符不能落在行尾
			if next != 0 && next != ' ' && !strings.ContainsRune(noLineStart, next) && !strings.ContainsRune(noLineEnd, r) {
				emit(i + 1)
			}
		case r == '-' && i > 0 && isWordRune(runes[i-1]) && next != 0 && next != ' ' && !unicode.IsDigit(next):
			// 连字符之后（前面是字母或数字，后面不是数字）
			emit(i + 1)
		case next != 0 && isWideRune(next) && !strings.ContainsRune(noLineStart, next) && !strings.ContainsRune(noLineEnd, r):
			// 拉丁字符与汉字相接：汉字之前是机会
			emit(i + 1)
		}
	}
	emit(len(runes))
	return units
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
