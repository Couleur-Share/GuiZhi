package aiflow

// 流式回答的增量抽取（对应 services/knowledge-ai/answer-stream.ts）。
//
// Agent 协议要求模型输出 {"action":"answer","text":"..."}，流式下拿到的是半截 JSON：
// 直接把原始分块喷到界面上，用户看到的是转义符和花括号。这里只认 "text" 后面那段字符串字面量，边到边解转义。
//
// 与 TS 一样，结果完全由「到目前为止收到的原始文本」决定，和分块边界无关；
// 内部按 UTF-16 码元处理（TS 的下标、slice、\u 转义都是码元语义）。
// 已知差异：孤立代理项（只来了 \ud83d、低半部分还没到，或根本不成对）在 Go 里落成 U+FFFD，见 jsx.go 文件头。

import "unicode/utf16"

// AnswerStream 累积流式输出并抽取回答文本。零值可用。
type AnswerStream struct {
	raw []uint16
}

// NewAnswerStream 对应 createAnswerStreamState。
func NewAnswerStream() *AnswerStream { return &AnswerStream{} }

// Push 喂入一块原始输出，返回到目前为止的完整回答文本（尚未开始则为空串）。
//
// 返回全量而非增量：调用方直接覆盖显示即可。动作不是 answer、或 "text" 尚未出现时一律返回空——
// 推理模型会在 JSON 之前先吐一大段思考，那些不该进回答区。
func (s *AnswerStream) Push(chunk string) string {
	s.raw = append(s.raw, utf16.Encode([]rune(chunk))...)

	if !hasAnswerAction(s.raw) {
		return ""
	}
	valueStart, ok := findTextValueStart(s.raw)
	if !ok {
		return ""
	}
	fragment := s.raw[valueStart:]
	if end := findStringEnd(s.raw, valueStart); end != -1 {
		fragment = s.raw[valueStart:end]
	}
	return decodeJSONStringFragment(fragment)
}

func isJSSpaceUnit(u uint16) bool { return jsSpace(rune(u)) }

// skipSpace 返回 from 之后第一个非空白码元的下标。
func skipSpace(units []uint16, from int) int {
	for from < len(units) && isJSSpaceUnit(units[from]) {
		from++
	}
	return from
}

// hasUnits 判断 units 从 at 开始是否恰好是 literal（literal 全是 ASCII）。
// foldCase 为真时按 JS 非 Unicode 模式的 /i 比较：只有 ASCII 字母不分大小写，
// 非 ASCII 字符（如长 s U+017F、开尔文符号 U+212A）不会折叠成 ASCII。
func hasUnits(units []uint16, at int, literal string, foldCase bool) bool {
	if at < 0 || at+len(literal) > len(units) {
		return false
	}
	for i := 0; i < len(literal); i++ {
		got, want := units[at+i], uint16(literal[i])
		if foldCase {
			if got >= 'A' && got <= 'Z' {
				got += 'a' - 'A'
			}
			if want >= 'A' && want <= 'Z' {
				want += 'a' - 'A'
			}
		}
		if got != want {
			return false
		}
	}
	return true
}

// hasAnswerAction 对应 /"action"\s*:\s*"answer"/i.test(raw)。
func hasAnswerAction(raw []uint16) bool {
	const key, value = `"action"`, `"answer"`
	for i := range raw {
		if !hasUnits(raw, i, key, true) {
			continue
		}
		j := skipSpace(raw, i+len(key))
		if j >= len(raw) || raw[j] != ':' {
			continue
		}
		j = skipSpace(raw, j+1)
		if hasUnits(raw, j, value, true) {
			return true
		}
	}
	return false
}

// findTextValueStart 对应 /"text"\s*:\s*"/.exec(raw)：返回开引号之后的下标。
func findTextValueStart(raw []uint16) (int, bool) {
	const key = `"text"`
	for i := range raw {
		if !hasUnits(raw, i, key, false) {
			continue
		}
		j := skipSpace(raw, i+len(key))
		if j >= len(raw) || raw[j] != ':' {
			continue
		}
		j = skipSpace(raw, j+1)
		if j < len(raw) && raw[j] == '"' {
			return j + 1, true
		}
	}
	return 0, false
}

// findStringEnd 找到字符串字面量的结束位置（未转义的引号）；未闭合返回 -1。
func findStringEnd(raw []uint16, from int) int {
	for index := from; index < len(raw); {
		switch raw[index] {
		case '\\':
			index += 2
		case '"':
			return index
		default:
			index++
		}
	}
	return -1
}

// jsParseInt16 对应 Number.parseInt(text, 16)：跳过前导空白，接受正负号与 0x 前缀，
// 取最长的十六进制数字前缀；没有数字返回 ok=false（NaN）。
func jsParseInt16(units []uint16) (int, bool) {
	i := skipSpace(units, 0)
	sign := 1
	if i < len(units) && (units[i] == '-' || units[i] == '+') {
		if units[i] == '-' {
			sign = -1
		}
		i++
	}
	if i+1 < len(units) && units[i] == '0' && (units[i+1] == 'x' || units[i+1] == 'X') {
		i += 2
	}
	value, digits := 0, 0
	for ; i < len(units); i++ {
		d := hexDigitValue(units[i])
		if d < 0 {
			break
		}
		value = value*16 + d
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	return sign * value, true
}

func hexDigitValue(u uint16) int {
	switch {
	case u >= '0' && u <= '9':
		return int(u - '0')
	case u >= 'a' && u <= 'f':
		return int(u-'a') + 10
	case u >= 'A' && u <= 'F':
		return int(u-'A') + 10
	}
	return -1
}

// decodeJSONStringFragment 是 JSON 字符串转义还原；末尾半个转义序列按未完成处理，留到下一块。
func decodeJSONStringFragment(fragment []uint16) string {
	out := make([]uint16, 0, len(fragment))
	for index := 0; index < len(fragment); {
		char := fragment[index]
		if char != '\\' {
			out = append(out, char)
			index++
			continue
		}
		if index+1 >= len(fragment) {
			break
		}
		escape := fragment[index+1]
		switch escape {
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'u':
			end := index + 6
			if end > len(fragment) {
				end = len(fragment)
			}
			hex := fragment[index+2 : end]
			if len(hex) < 4 {
				return string(utf16.Decode(out))
			}
			// String.fromCharCode(code)：取模 2^16；NaN 时什么都不输出
			if code, ok := jsParseInt16(hex); ok {
				out = append(out, uint16(code&0xFFFF))
			}
			index += 6
			continue
		default:
			// \" \\ \/ 以及未知转义一律取原字符
			out = append(out, escape)
		}
		index += 2
	}
	return string(utf16.Decode(out))
}
