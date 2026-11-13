package main

// jsonc-parser（microsoft/node-jsonc-parser）中 scanner / parseTree / setProperty / format 的移植，
// 只覆盖 mcp.json 一键安装用到的路径：解析校验 + 在 ["mcpServers","guizhi"] 处按 2 空格缩进写入。
// 偏移一律按 UTF-16 码元，与 JS 字符串下标一致，保证与 TS 逐字节相同的编辑结果。

import (
	"fmt"
	"strconv"
)

type jsoncKind int

const (
	jkUnknown jsoncKind = iota
	jkOpenBrace
	jkCloseBrace
	jkOpenBracket
	jkCloseBracket
	jkComma
	jkColon
	jkNull
	jkTrue
	jkFalse
	jkString
	jkNumber
	jkLineComment
	jkBlockComment
	jkLineBreak
	jkTrivia
	jkEOF
)

type scanErr int

const (
	seNone scanErr = iota
	seUnexpectedEndOfComment
	seUnexpectedEndOfString
	seUnexpectedEndOfNumber
	seInvalidUnicode
	seInvalidEscape
	seInvalidCharacter
)

type jsoncScanner struct {
	text        []uint16
	pos         int
	tokenOffset int
	token       jsoncKind
	value       []uint16
	err         scanErr
}

func newJSONCScanner(text []uint16) *jsoncScanner { return &jsoncScanner{text: text} }

func (s *jsoncScanner) tokenLength() int { return s.pos - s.tokenOffset }

func jsIsDigit(c uint16) bool     { return c >= '0' && c <= '9' }
func jsIsLineBreak(c uint16) bool { return c == '\n' || c == '\r' }
func jsIsWS(c uint16) bool        { return c == ' ' || c == '\t' }

func jsIsUnknownContent(c uint16) bool {
	if jsIsWS(c) || jsIsLineBreak(c) {
		return false
	}
	switch c {
	case '}', ']', '{', '[', '"', ':', ',', '/':
		return false
	}
	return true
}

func (s *jsoncScanner) at(i int) uint16 {
	if i < len(s.text) {
		return s.text[i]
	}
	return 0xFFFF // 越界：不会等于任何被比较的字符
}

func (s *jsoncScanner) scanHex(count int, exact bool) int {
	digits, value := 0, 0
	for digits < count || !exact {
		ch := s.at(s.pos)
		switch {
		case ch >= '0' && ch <= '9':
			value = value*16 + int(ch-'0')
		case ch >= 'A' && ch <= 'F':
			value = value*16 + int(ch-'A') + 10
		case ch >= 'a' && ch <= 'f':
			value = value*16 + int(ch-'a') + 10
		default:
			goto done
		}
		s.pos++
		digits++
	}
done:
	if digits < count {
		return -1
	}
	return value
}

func (s *jsoncScanner) scanString() []uint16 {
	var result []uint16
	start := s.pos
	for {
		if s.pos >= len(s.text) {
			result = append(result, s.text[start:s.pos]...)
			s.err = seUnexpectedEndOfString
			break
		}
		ch := s.text[s.pos]
		if ch == '"' {
			result = append(result, s.text[start:s.pos]...)
			s.pos++
			break
		}
		if ch == '\\' {
			result = append(result, s.text[start:s.pos]...)
			s.pos++
			if s.pos >= len(s.text) {
				s.err = seUnexpectedEndOfString
				break
			}
			ch2 := s.text[s.pos]
			s.pos++
			switch ch2 {
			case '"':
				result = append(result, '"')
			case '\\':
				result = append(result, '\\')
			case '/':
				result = append(result, '/')
			case 'b':
				result = append(result, '\b')
			case 'f':
				result = append(result, '\f')
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case 'u':
				if v := s.scanHex(4, true); v >= 0 {
					result = append(result, uint16(v))
				} else {
					s.err = seInvalidUnicode
				}
			default:
				s.err = seInvalidEscape
			}
			start = s.pos
			continue
		}
		if ch <= 0x1f {
			if jsIsLineBreak(ch) {
				result = append(result, s.text[start:s.pos]...)
				s.err = seUnexpectedEndOfString
				break
			}
			s.err = seInvalidCharacter // 标记错误但字符串继续
		}
		s.pos++
	}
	return result
}

func (s *jsoncScanner) scanNumber() []uint16 {
	start := s.pos
	if s.at(s.pos) == '0' {
		s.pos++
	} else {
		s.pos++
		for s.pos < len(s.text) && jsIsDigit(s.text[s.pos]) {
			s.pos++
		}
	}
	if s.pos < len(s.text) && s.text[s.pos] == '.' {
		s.pos++
		if s.pos < len(s.text) && jsIsDigit(s.text[s.pos]) {
			s.pos++
			for s.pos < len(s.text) && jsIsDigit(s.text[s.pos]) {
				s.pos++
			}
		} else {
			s.err = seUnexpectedEndOfNumber
			return s.text[start:s.pos]
		}
	}
	end := s.pos
	if s.pos < len(s.text) && (s.text[s.pos] == 'E' || s.text[s.pos] == 'e') {
		s.pos++
		if s.pos < len(s.text) && (s.text[s.pos] == '+' || s.text[s.pos] == '-') {
			s.pos++
		}
		if s.pos < len(s.text) && jsIsDigit(s.text[s.pos]) {
			s.pos++
			for s.pos < len(s.text) && jsIsDigit(s.text[s.pos]) {
				s.pos++
			}
			end = s.pos
		} else {
			s.err = seUnexpectedEndOfNumber
		}
	}
	return s.text[start:end]
}

func (s *jsoncScanner) scan() jsoncKind {
	s.value = nil
	s.err = seNone
	s.tokenOffset = s.pos
	if s.pos >= len(s.text) {
		s.tokenOffset = len(s.text)
		s.token = jkEOF
		return s.token
	}
	code := s.text[s.pos]
	if jsIsWS(code) {
		for s.pos < len(s.text) && jsIsWS(s.text[s.pos]) {
			s.pos++
		}
		s.token = jkTrivia
		return s.token
	}
	if jsIsLineBreak(code) {
		s.pos++
		if code == '\r' && s.at(s.pos) == '\n' {
			s.pos++
		}
		s.token = jkLineBreak
		return s.token
	}
	switch code {
	case '{':
		s.pos++
		s.token = jkOpenBrace
		return s.token
	case '}':
		s.pos++
		s.token = jkCloseBrace
		return s.token
	case '[':
		s.pos++
		s.token = jkOpenBracket
		return s.token
	case ']':
		s.pos++
		s.token = jkCloseBracket
		return s.token
	case ':':
		s.pos++
		s.token = jkColon
		return s.token
	case ',':
		s.pos++
		s.token = jkComma
		return s.token
	case '"':
		s.pos++
		s.value = s.scanString()
		s.token = jkString
		return s.token
	case '/':
		start := s.pos - 0
		if s.at(s.pos+1) == '/' {
			s.pos += 2
			for s.pos < len(s.text) && !jsIsLineBreak(s.text[s.pos]) {
				s.pos++
			}
			s.value = s.text[start:s.pos]
			s.token = jkLineComment
			return s.token
		}
		if s.at(s.pos+1) == '*' {
			s.pos += 2
			safeLength := len(s.text) - 1
			closed := false
			for s.pos < safeLength {
				ch := s.text[s.pos]
				if ch == '*' && s.text[s.pos+1] == '/' {
					s.pos += 2
					closed = true
					break
				}
				s.pos++
			}
			if !closed {
				s.pos++
				s.err = seUnexpectedEndOfComment
			}
			end := s.pos
			if end > len(s.text) {
				end = len(s.text)
			}
			s.value = s.text[start:end]
			s.token = jkBlockComment
			return s.token
		}
		s.pos++
		s.token = jkUnknown
		return s.token
	case '-':
		s.pos++
		if s.pos == len(s.text) || !jsIsDigit(s.text[s.pos]) {
			s.token = jkUnknown
			return s.token
		}
		s.pos--
		s.value = s.scanNumber()
		s.token = jkNumber
		return s.token
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		s.value = s.scanNumber()
		s.token = jkNumber
		return s.token
	}
	// 其余：读到下一个分隔符为止的整个词，是 true/false/null 就是关键字，否则 Unknown
	for s.pos < len(s.text) && jsIsUnknownContent(s.text[s.pos]) {
		s.pos++
	}
	if s.tokenOffset != s.pos {
		word := fromU16(s.text[s.tokenOffset:s.pos])
		switch word {
		case "true":
			s.token = jkTrue
		case "false":
			s.token = jkFalse
		case "null":
			s.token = jkNull
		default:
			s.token = jkUnknown
		}
		return s.token
	}
	s.pos++
	s.token = jkUnknown
	return s.token
}

// ── parseTree ──

type jsoncNode struct {
	kind     string // object | array | property | string | number | boolean | null
	offset   int
	length   int
	parent   *jsoncNode
	children []*jsoncNode
	value    string // 仅 string / property 名
	truthy   bool   // 字面量转 JS 布尔后的值（用于 `parsed.x?.[y]` 的真值判断）
}

type jsoncParseOpts struct{ allowTrailingComma bool }

// parseJSONCTree 返回根节点（可能为 nil）与错误个数；与 jsonc-parser 的 parseTree 一样对错误容忍、尽量建树。
func parseJSONCTree(text []uint16, opts jsoncParseOpts) (root *jsoncNode, errCount int) {
	sc := newJSONCScanner(text)
	holder := &jsoncNode{kind: "array"}
	cur := holder
	onError := func() { errCount++ }
	onValue := func(n *jsoncNode) *jsoncNode {
		n.parent = cur
		cur.children = append(cur.children, n)
		return n
	}
	ensurePropertyComplete := func(end int) {
		if cur.kind == "property" {
			cur.length = end - cur.offset
			cur = cur.parent
		}
	}
	scanNext := func() jsoncKind {
		for {
			tok := sc.scan()
			if sc.err != seNone {
				onError()
			}
			switch tok {
			case jkLineComment, jkBlockComment, jkTrivia, jkLineBreak:
				continue
			case jkUnknown:
				onError()
				continue
			}
			return tok
		}
	}
	handleError := func(skipUntilAfter, skipUntil []jsoncKind) {
		onError()
		if len(skipUntilAfter)+len(skipUntil) > 0 {
			tok := sc.token
			for tok != jkEOF {
				if containsKind(skipUntilAfter, tok) {
					scanNext()
					break
				} else if containsKind(skipUntil, tok) {
					break
				}
				tok = scanNext()
			}
		}
	}
	var parseValue func() bool
	parseString := func(isValue bool) bool {
		if sc.token != jkString {
			return false
		}
		name := fromU16(sc.value)
		off, ln := sc.tokenOffset, sc.tokenLength()
		if isValue {
			onValue(&jsoncNode{kind: "string", offset: off, length: ln, value: name, truthy: name != ""})
			ensurePropertyComplete(off + ln)
		} else {
			prop := onValue(&jsoncNode{kind: "property", offset: off, length: -1})
			cur = prop
			onValue(&jsoncNode{kind: "string", offset: off, length: ln, value: name})
		}
		scanNext()
		return true
	}
	parseLiteral := func() bool {
		var n *jsoncNode
		switch sc.token {
		case jkNumber:
			f, ok := parseJSNumber(fromU16(sc.value))
			if !ok {
				onError()
				f = 0
			}
			n = &jsoncNode{kind: "number", truthy: f != 0}
		case jkNull:
			n = &jsoncNode{kind: "null"}
		case jkTrue:
			n = &jsoncNode{kind: "boolean", truthy: true}
		case jkFalse:
			n = &jsoncNode{kind: "boolean"}
		default:
			return false
		}
		n.offset, n.length = sc.tokenOffset, sc.tokenLength()
		onValue(n)
		ensurePropertyComplete(n.offset + n.length)
		scanNext()
		return true
	}
	parseProperty := func() bool {
		if sc.token != jkString {
			handleError(nil, []jsoncKind{jkCloseBrace, jkComma})
			return false
		}
		parseString(false)
		if sc.token == jkColon {
			scanNext()
			if !parseValue() {
				handleError(nil, []jsoncKind{jkCloseBrace, jkComma})
			}
		} else {
			handleError(nil, []jsoncKind{jkCloseBrace, jkComma})
		}
		return true
	}
	parseObject := func() bool {
		obj := onValue(&jsoncNode{kind: "object", offset: sc.tokenOffset, length: -1, truthy: true})
		cur = obj
		scanNext()
		needsComma := false
		for sc.token != jkCloseBrace && sc.token != jkEOF {
			if sc.token == jkComma {
				if !needsComma {
					handleError(nil, nil)
				}
				ensurePropertyComplete(sc.tokenOffset)
				scanNext()
				if sc.token == jkCloseBrace && opts.allowTrailingComma {
					break
				}
			} else if needsComma {
				handleError(nil, nil)
			}
			if !parseProperty() {
				handleError(nil, []jsoncKind{jkCloseBrace, jkComma})
			}
			needsComma = true
		}
		if sc.token == jkCloseBrace {
			ensurePropertyComplete(sc.tokenOffset)
			cur.length = sc.tokenOffset + sc.tokenLength() - cur.offset
			cur = cur.parent
			ensurePropertyComplete(sc.tokenOffset + sc.tokenLength())
			scanNext()
		} else {
			ensurePropertyComplete(sc.tokenOffset)
			cur.length = sc.tokenOffset - cur.offset
			cur = cur.parent
			ensurePropertyComplete(sc.tokenOffset)
			handleError([]jsoncKind{jkCloseBrace}, nil)
		}
		return true
	}
	parseArray := func() bool {
		arr := onValue(&jsoncNode{kind: "array", offset: sc.tokenOffset, length: -1, truthy: true})
		cur = arr
		scanNext()
		needsComma := false
		for sc.token != jkCloseBracket && sc.token != jkEOF {
			if sc.token == jkComma {
				if !needsComma {
					handleError(nil, nil)
				}
				scanNext()
				if sc.token == jkCloseBracket && opts.allowTrailingComma {
					break
				}
			} else if needsComma {
				handleError(nil, nil)
			}
			if !parseValue() {
				handleError(nil, []jsoncKind{jkCloseBracket, jkComma})
			}
			needsComma = true
		}
		if sc.token == jkCloseBracket {
			cur.length = sc.tokenOffset + sc.tokenLength() - cur.offset
			cur = cur.parent
			ensurePropertyComplete(sc.tokenOffset + sc.tokenLength())
			scanNext()
		} else {
			cur.length = sc.tokenOffset - cur.offset
			cur = cur.parent
			ensurePropertyComplete(sc.tokenOffset)
			handleError([]jsoncKind{jkCloseBracket}, nil)
		}
		return true
	}
	parseValue = func() bool {
		switch sc.token {
		case jkOpenBracket:
			return parseArray()
		case jkOpenBrace:
			return parseObject()
		case jkString:
			return parseString(true)
		default:
			return parseLiteral()
		}
	}
	scanNext()
	if sc.token == jkEOF {
		handleError(nil, nil)
		return nil, errCount
	}
	if !parseValue() {
		handleError(nil, nil)
	} else if sc.token != jkEOF {
		handleError(nil, nil)
	}
	if len(holder.children) > 0 {
		root = holder.children[0]
		root.parent = nil
	}
	return root, errCount
}

func containsKind(list []jsoncKind, k jsoncKind) bool {
	for _, x := range list {
		if x == k {
			return true
		}
	}
	return false
}

// parseJSNumber 对应 Number(tokenValue)：scanner 已保证形状，只处理溢出为 Infinity 的情况之外的普通数。
func parseJSNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, true // 溢出得到 ±Inf，与 Number() 一致
		}
		return 0, false
	}
	return f, true
}

// findNodeByPath 对应 findNodeAtLocation（路径只含字符串段）。
func findNodeByPath(root *jsoncNode, path []string) *jsoncNode {
	for _, seg := range path {
		if root == nil || root.kind != "object" {
			return nil
		}
		var found *jsoncNode
		for _, prop := range root.children {
			if len(prop.children) == 2 && prop.children[0].value == seg {
				found = prop.children[1]
				break
			}
		}
		root = found
	}
	return root
}

// ── format ──

type jsoncEdit struct {
	offset, length int
	content        []uint16
}

func applyEdit(text []uint16, e jsoncEdit) []uint16 {
	out := make([]uint16, 0, len(text)-e.length+len(e.content))
	out = append(out, text[:e.offset]...)
	out = append(out, e.content...)
	return append(out, text[e.offset+e.length:]...)
}

func jsRepeat(unit []uint16, n int) []uint16 {
	var out []uint16
	for i := 0; i < n; i++ {
		out = append(out, unit...)
	}
	return out
}

func u16eq(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isEOLAt(text []uint16, i int) bool { return text[i] == '\r' || text[i] == '\n' }

func detectEOL(text []uint16) []uint16 {
	for i, ch := range text {
		if ch == '\r' {
			if i+1 < len(text) && text[i+1] == '\n' {
				return toUTF16("\r\n")
			}
			return toUTF16("\r")
		} else if ch == '\n' {
			return toUTF16("\n")
		}
	}
	return toUTF16("\n")
}

// formatRange 对应 format(documentText, range, {insertSpaces: true, tabSize: 2})，keepLines=false。
func formatRange(doc []uint16, rangeOffset, rangeLength int) []jsoncEdit {
	const tabSize = 2
	rangeStart, rangeEnd := rangeOffset, rangeOffset+rangeLength
	formatTextStart := rangeStart
	for formatTextStart > 0 && !isEOLAt(doc, formatTextStart-1) {
		formatTextStart--
	}
	endOffset := rangeEnd
	for endOffset < len(doc) && !isEOLAt(doc, endOffset) {
		endOffset++
	}
	formatText := doc[formatTextStart:endOffset]
	nChars := 0
	for _, ch := range formatText {
		if ch == ' ' {
			nChars++
		} else if ch == '\t' {
			nChars += tabSize
		} else {
			break
		}
	}
	initialIndentLevel := nChars / tabSize
	eol := detectEOL(doc)
	indentValue := toUTF16("  ")
	indentLevel, numberLineBreaks := 0, 0
	sc := newJSONCScanner(formatText)
	hasError := false
	newLinesAndIndent := func() []uint16 {
		return append(append([]uint16{}, eol...), jsRepeat(indentValue, initialIndentLevel+indentLevel)...)
	}
	scanNext := func() jsoncKind {
		tok := sc.scan()
		numberLineBreaks = 0
		for tok == jkTrivia || tok == jkLineBreak {
			if tok == jkLineBreak {
				numberLineBreaks = 1
			}
			tok = sc.scan()
		}
		hasError = tok == jkUnknown || sc.err != seNone
		return tok
	}
	var edits []jsoncEdit
	addEdit := func(text []uint16, start, end int) {
		if !hasError && start < rangeEnd && end > rangeStart && !u16eq(doc[start:end], text) {
			edits = append(edits, jsoncEdit{offset: start, length: end - start, content: text})
		}
	}
	first := scanNext()
	if first != jkEOF {
		firstStart := sc.tokenOffset + formatTextStart
		addEdit(jsRepeat(indentValue, initialIndentLevel), formatTextStart, firstStart)
	}
	for first != jkEOF {
		firstEnd := sc.tokenOffset + sc.tokenLength() + formatTextStart
		second := scanNext()
		var replace []uint16
		needsLineBreak := false
		for numberLineBreaks == 0 && (second == jkLineComment || second == jkBlockComment) {
			commentStart := sc.tokenOffset + formatTextStart
			addEdit(toUTF16(" "), firstEnd, commentStart)
			firstEnd = sc.tokenOffset + sc.tokenLength() + formatTextStart
			needsLineBreak = second == jkLineComment
			if needsLineBreak {
				replace = newLinesAndIndent()
			} else {
				replace = nil
			}
			second = scanNext()
		}
		switch {
		case second == jkCloseBrace:
			if first != jkOpenBrace {
				indentLevel--
				replace = newLinesAndIndent()
			}
		case second == jkCloseBracket:
			if first != jkOpenBracket {
				indentLevel--
				replace = newLinesAndIndent()
			}
		default:
			switch first {
			case jkOpenBracket, jkOpenBrace:
				indentLevel++
				replace = newLinesAndIndent()
			case jkComma:
				replace = newLinesAndIndent()
			case jkLineComment:
				replace = newLinesAndIndent()
			case jkBlockComment:
				if numberLineBreaks > 0 {
					replace = newLinesAndIndent()
				} else if !needsLineBreak {
					replace = toUTF16(" ")
				}
			case jkColon:
				if !needsLineBreak {
					replace = toUTF16(" ")
				}
			case jkString:
				if second == jkColon && !needsLineBreak {
					replace = nil
				}
			case jkNull, jkTrue, jkFalse, jkNumber, jkCloseBrace, jkCloseBracket:
				if (second == jkLineComment || second == jkBlockComment) && !needsLineBreak {
					replace = toUTF16(" ")
				} else if second != jkComma && second != jkEOF {
					hasError = true
				}
			case jkUnknown:
				hasError = true
			}
			if numberLineBreaks > 0 && (second == jkLineComment || second == jkBlockComment) {
				replace = newLinesAndIndent()
			}
		}
		if second == jkEOF {
			replace = nil
		}
		secondStart := sc.tokenOffset + formatTextStart
		addEdit(replace, firstEnd, secondStart)
		first = second
	}
	return edits
}

// withFormatting 对应 setProperty 内部的同名函数：套用编辑后只格式化被改动的行，合并成一个编辑。
func withFormatting(text []uint16, edit jsoncEdit) jsoncEdit {
	newText := applyEdit(text, edit)
	begin := edit.offset
	end := edit.offset + len(edit.content)
	if edit.length == 0 || len(edit.content) == 0 {
		for begin > 0 && !isEOLAt(newText, begin-1) {
			begin--
		}
		for end < len(newText) && !isEOLAt(newText, end) {
			end++
		}
	}
	edits := formatRange(newText, begin, end-begin)
	for i := len(edits) - 1; i >= 0; i-- {
		cur := edits[i]
		newText = applyEdit(newText, cur)
		if cur.offset < begin {
			begin = cur.offset
		}
		if cur.offset+cur.length > end {
			end = cur.offset + cur.length
		}
		end += len(cur.content) - cur.length
	}
	editLength := len(text) - (len(newText) - end) - begin
	return jsoncEdit{offset: begin, length: editLength, content: append([]uint16{}, newText[begin:end]...)}
}

// jsoncSetProperty 对应 modify(text, path, value, {formattingOptions})；value 是已序列化的 JSON 文本。
// 路径缺失的中间层会连同值一起补出来，与 jsonc-parser 一致。
func jsoncSetProperty(text []uint16, path []string, valueJSON string) ([]uint16, error) {
	root, _ := parseJSONCTree(text, jsoncParseOpts{})
	p := append([]string{}, path...)
	value := valueJSON
	var parent *jsoncNode
	var last string
	for len(p) > 0 {
		last = p[len(p)-1]
		p = p[:len(p)-1]
		parent = findNodeByPath(root, p)
		if parent == nil {
			value = "{" + jsonString(last) + ":" + value + "}"
		} else {
			break
		}
	}
	if parent == nil {
		off, ln := 0, 0
		if root != nil {
			off, ln = root.offset, root.length
		}
		e := withFormatting(text, jsoncEdit{offset: off, length: ln, content: toUTF16(value)})
		return applyEdit(text, e), nil
	}
	if parent.kind != "object" {
		return nil, fmt.Errorf("Can not add index to parent of type %s", parent.kind)
	}
	if existing := findNodeByPath(parent, []string{last}); existing != nil {
		e := withFormatting(text, jsoncEdit{offset: existing.offset, length: existing.length, content: toUTF16(value)})
		return applyEdit(text, e), nil
	}
	newProperty := jsonString(last) + ": " + value
	var edit jsoncEdit
	if n := len(parent.children); n > 0 {
		prev := parent.children[n-1]
		edit = jsoncEdit{offset: prev.offset + prev.length, content: toUTF16("," + newProperty)}
	} else {
		edit = jsoncEdit{offset: parent.offset + 1, content: toUTF16(newProperty)}
	}
	return applyEdit(text, withFormatting(text, edit)), nil
}
