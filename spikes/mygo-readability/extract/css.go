package extract

import (
	"io"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

// 移植自 snapshot-sanitize.ts 的 cleanCss（css-tree 白名单）：
// 属性白名单、url() 必须由调用方映射、拒绝 expression/var/env/paint/image-set、
// position 只允许 relative/absolute/static、样式表里的 at 规则整体移除、输出中 < 转义为 \3c 。
//
// 与产品的有意差异：函数名先解码 CSS 转义再匹配（产品直接比较原文，\i\6d age-set(\ 能绕过黑名单，
// 只是被 CSP 挡住）；无法解析的声明一律丢弃（产品会原样保留 css-tree 解析失败的片段）。

var cssProps = strset("color background background-color background-image background-size background-position background-repeat background-origin background-clip border border-width border-style border-color border-radius border-top border-right border-bottom border-left border-top-left-radius border-top-right-radius border-bottom-left-radius border-bottom-right-radius box-shadow box-sizing width height min-width max-width min-height max-height margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left font-family font-size font-weight font-style font-variant line-height letter-spacing word-spacing text-align text-indent text-decoration text-transform text-shadow white-space word-break overflow-wrap vertical-align display flex flex-direction flex-wrap flex-grow flex-shrink flex-basis align-items align-self align-content justify-content gap row-gap column-gap grid-template-columns grid-template-rows grid-column grid-row position top left right bottom transform transform-origin opacity overflow overflow-x overflow-y list-style-type list-style-position border-collapse border-spacing table-layout float clear content object-fit object-position fill stroke stroke-width")

var cssBlockedFuncs = strset("expression var env paint image-set -webkit-image-set")

// CSSURLMapper 把 url() 的值映射为本快照资源；ok=false 表示不允许，整条声明丢弃。
type CSSURLMapper func(url string) (mapped string, ok bool)

// cssUnescape 解码 CSS 转义：\\HEX{1,6}[空白] 与 \\字符。
func cssUnescape(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '\\' || i+1 >= len(rs) {
			b.WriteRune(rs[i])
			continue
		}
		j := i + 1
		k := j
		for k < len(rs) && k-j < 6 && isHex(rs[k]) {
			k++
		}
		if k > j {
			n, _ := strconv.ParseInt(string(rs[j:k]), 16, 32)
			if n == 0 || n > 0x10FFFF || (n >= 0xD800 && n <= 0xDFFF) {
				n = 0xFFFD
			}
			b.WriteRune(rune(n))
			if k < len(rs) && (rs[k] == ' ' || rs[k] == '\t' || rs[k] == '\n') {
				k++
			}
			i = k - 1
			continue
		}
		b.WriteRune(rs[j])
		i = j
	}
	return b.String()
}

func isHex(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

func cssQuote(s string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\"", "\\\"") + "\""
}

// cleanValue 处理一个声明的值记号，返回清理后的值文本；ok=false 表示整条声明应丢弃。
func cleanValue(prop string, toks []css.Token, mapURL CSSURLMapper) (string, bool) {
	// 末尾的 !important 单独处理
	important := false
	for len(toks) > 0 && toks[len(toks)-1].TokenType == css.WhitespaceToken {
		toks = toks[:len(toks)-1]
	}
	if n := len(toks); n >= 2 && toks[n-2].TokenType == css.DelimToken && string(toks[n-2].Data) == "!" && strings.EqualFold(string(toks[n-1].Data), "important") {
		important = true
		toks = toks[:n-2]
	}
	var b strings.Builder
	depth := 0
	pendingSpace := false
	emit := func(s string) {
		if pendingSpace && b.Len() > 0 {
			b.WriteByte(' ')
		}
		pendingSpace = false
		b.WriteString(s)
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		data := string(t.Data)
		switch t.TokenType {
		case css.WhitespaceToken, css.CommentToken:
			pendingSpace = true
		case css.FunctionToken:
			name := strings.ToLower(cssUnescape(strings.TrimSuffix(data, "(")))
			if cssBlockedFuncs[name] {
				return "", false
			}
			depth++
			if name == "url" {
				// 带引号的 url("...")：函数记号后跟字符串
				j := i + 1
				for j < len(toks) && toks[j].TokenType == css.WhitespaceToken {
					j++
				}
				if j >= len(toks) || toks[j].TokenType != css.StringToken {
					return "", false
				}
				raw := string(toks[j].Data)
				u := cssUnescape(raw[1 : len(raw)-1])
				k := j + 1
				for k < len(toks) && toks[k].TokenType == css.WhitespaceToken {
					k++
				}
				if k >= len(toks) || toks[k].TokenType != css.RightParenthesisToken {
					return "", false
				}
				mapped, ok := mapURL(u)
				if !ok {
					return "", false
				}
				emit("url(" + mapped + ")")
				depth--
				i = k
				continue
			}
			emit(data)
		case css.URLToken:
			inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(data, data[:4]), ")"))
			mapped, ok := mapURL(cssUnescape(inner))
			if !ok {
				return "", false
			}
			emit("url(" + mapped + ")")
		case css.LeftParenthesisToken:
			depth++
			emit(data)
		case css.RightParenthesisToken:
			depth--
			if depth < 0 {
				return "", false
			}
			pendingSpace = false
			b.WriteString(data)
		case css.StringToken:
			emit(cssQuote(cssUnescape(data[1 : len(data)-1])))
		case css.BadStringToken, css.BadURLToken, css.LeftBraceToken, css.RightBraceToken, css.SemicolonToken, css.ColonToken:
			return "", false
		case css.CommaToken:
			pendingSpace = false
			b.WriteString(data)
		default:
			emit(data)
		}
	}
	if depth != 0 {
		return "", false
	}
	v := b.String()
	if v == "" {
		return "", false
	}
	if prop == "position" {
		switch strings.ToLower(strings.ReplaceAll(v, " ", "")) {
		case "relative", "absolute", "static":
		default:
			return "", false
		}
	}
	if important {
		v += "!important"
	}
	return v, true
}

// CleanCSS 清理 CSS。stylesheet=false 时输入是声明列表（style 属性），否则是完整样式表。
func CleanCSS(src string, mapURL CSSURLMapper, stylesheet bool) string {
	p := css.NewParser(parse.NewInputString(src), !stylesheet)
	var out strings.Builder
	var selectors []string
	var decls []string
	atDepth := 0
	nested := 0 // 规则集里嵌套的规则（CSS nesting）：整体丢弃，避免选择器被错误合并
	inRule := false
	rsOpen := false // 当前是否在规则集的花括号内
	flush := func() {
		if len(selectors) > 0 { // 与产品一致：声明被清空的规则也保留，输出 sel{}
			out.WriteString(strings.Join(selectors, ",") + "{" + strings.Join(decls, ";") + "}")
		}
		selectors, decls = nil, nil
		inRule = false
	}
	selText := func(vals []css.Token) string {
		var b strings.Builder
		for _, t := range vals {
			if t.TokenType == css.CommentToken {
				continue
			}
			b.Write(t.Data)
		}
		return strings.TrimSpace(b.String())
	}
	for {
		gt, _, data := p.Next()
		if gt == css.ErrorGrammar {
			if p.Err() != io.EOF {
				// 解析错误：已收集的内容保留，与 css-tree 容错行为相近
				_ = p.Err()
			}
			break
		}
		switch gt {
		case css.AtRuleGrammar:
		case css.BeginAtRuleGrammar:
			atDepth++
		case css.EndAtRuleGrammar:
			atDepth--
		case css.QualifiedRuleGrammar:
			if nested > 0 {
				continue
			}
			if atDepth == 0 {
				selectors = append(selectors, string(data)+selText(p.Values()))
				inRule = true
			}
		case css.BeginRulesetGrammar:
			if rsOpen && atDepth == 0 {
				nested++
				continue
			}
			if nested > 0 {
				nested++
				continue
			}
			if atDepth == 0 {
				rsOpen = true
				selectors = append(selectors, string(data)+selText(p.Values()))
				inRule = true
			}
		case css.EndRulesetGrammar:
			if nested > 0 {
				nested--
				continue
			}
			if atDepth == 0 {
				rsOpen = false
				flush()
			}
		case css.DeclarationGrammar:
			if atDepth > 0 || nested > 0 || (stylesheet && !inRule) {
				continue
			}
			prop := string(data)
			if !cssProps[strings.ToLower(prop)] {
				continue
			}
			if v, ok := cleanValue(strings.ToLower(prop), p.Values(), mapURL); ok {
				decls = append(decls, prop+":"+v)
			}
		}
	}
	if stylesheet {
		flush()
		return strings.ReplaceAll(out.String(), "<", "\\3c ")
	}
	return strings.ReplaceAll(strings.Join(decls, ";"), "<", "\\3c ")
}
