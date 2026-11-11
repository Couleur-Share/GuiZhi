// Package extract 是 web-extract-article.ts / web-extract-markdown.ts 的 Go 移植（spike）。
// 本文件是最小的 DOM 辅助：用 golang.org/x/net/html 的节点树实现 querySelector、closest、textContent 等。
package extract

import (
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// jsWS 是 JavaScript 正则 \s 的字符集（与 RE2 的 \s 不同：含 \v、NBSP、Unicode 空格与 U+FEFF）。
const jsWS = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var wsRun = regexp.MustCompile(`[` + jsWS + `]+`)

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// jsTrim 等价于 String.prototype.trim。
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// removeWS 去掉全部 JS 空白。
func removeWS(s string) string { return wsRun.ReplaceAllString(s, "") }

// collapseWS 把连续空白压成一个空格（再 trim 由调用方决定）。
func collapseWS(s string) string { return wsRun.ReplaceAllString(s, " ") }

// utf16Len 返回 JS 的 .length（UTF-16 码元数）。
func utf16Len(s string) int {
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

// sliceUTF16 等价于 s.slice(0, n)（按 UTF-16 码元，不会产生半个代理对时才截断完整字符）。
func sliceUTF16(s string, n int) string {
	if utf16Len(s) <= n {
		return s
	}
	u := utf16.Encode([]rune(s))
	return string(utf16.Decode(u[:n]))
}

var selCache = map[string]cascadia.Selector{}

func sel(s string) cascadia.Selector {
	if c, ok := selCache[s]; ok {
		return c
	}
	c := cascadia.MustCompile(s)
	selCache[s] = c
	return c
}

// qsa 等价于 root.querySelectorAll(s)：只含后代，不含 root 自身，按文档顺序。
func qsa(root *html.Node, s string) []*html.Node {
	var out []*html.Node
	for _, n := range sel(s).MatchAll(root) {
		if n != root {
			out = append(out, n)
		}
	}
	return out
}

func qs(root *html.Node, s string) *html.Node {
	if all := qsa(root, s); len(all) > 0 {
		return all[0]
	}
	return nil
}

// closest 等价于 el.closest(s)：从自身开始向上找。
func closest(n *html.Node, s string) *html.Node {
	c := sel(s)
	for ; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && c.Match(n) {
			return n
		}
	}
	return nil
}

func matches(n *html.Node, s string) bool {
	return n.Type == html.ElementNode && sel(s).Match(n)
}

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key && a.Namespace == "" {
			return a.Val, true
		}
	}
	return "", false
}

func getAttr(n *html.Node, key string) string {
	v, _ := attr(n, key)
	return v
}

func hasAttr(n *html.Node, key string) bool {
	_, ok := attr(n, key)
	return ok
}

func setAttr(n *html.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key && n.Attr[i].Namespace == "" {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func removeAttr(n *html.Node, key string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if !(a.Key == key && a.Namespace == "") {
			out = append(out, a)
		}
	}
	n.Attr = out
}

// nodeName 与 DOM 的 nodeName 一致：元素为大写标签名。
func nodeName(n *html.Node) string {
	if n.Type == html.ElementNode {
		return strings.ToUpper(n.Data)
	}
	return ""
}

func remove(n *html.Node) {
	if n != nil && n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

func replaceWith(old, repl *html.Node) {
	if old.Parent == nil {
		return
	}
	old.Parent.InsertBefore(repl, old)
	old.Parent.RemoveChild(old)
}

func createElement(tag string) *html.Node {
	return &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
}

func textNode(s string) *html.Node { return &html.Node{Type: html.TextNode, Data: s} }

// textContent 等价于 Node.textContent（元素与文档：后代文本节点相连，不含注释）。
func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				sb.WriteString(c.Data)
			} else if c.Type == html.ElementNode {
				walk(c)
			}
		}
	}
	walk(n)
	return sb.String()
}

func children(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, c)
	}
	return out
}

func elementChildren(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// contains 等价于 a.contains(b)（含 a 自身）。
func contains(a, b *html.Node) bool {
	for ; b != nil; b = b.Parent {
		if a == b {
			return true
		}
	}
	return false
}

// isConnected：沿父链能走到文档节点。
func isConnected(n *html.Node) bool {
	for ; n != nil; n = n.Parent {
		if n.Type == html.DocumentNode {
			return true
		}
	}
	return false
}

// parentElement：父节点是元素时才返回。
func parentElement(n *html.Node) *html.Node {
	if n.Parent != nil && n.Parent.Type == html.ElementNode {
		return n.Parent
	}
	return nil
}

func outerHTML(n *html.Node) string {
	var sb strings.Builder
	html.Render(&sb, n)
	return sb.String()
}

func innerHTML(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		html.Render(&sb, c)
	}
	return sb.String()
}

// setInnerHTML 等价于 el.innerHTML = s（以 el 为上下文解析片段）。
func setInnerHTML(el *html.Node, s string) {
	for el.FirstChild != nil {
		el.RemoveChild(el.FirstChild)
	}
	nodes, err := html.ParseFragment(strings.NewReader(s), el)
	if err != nil {
		return
	}
	for _, c := range nodes {
		el.AppendChild(c)
	}
}

func insertFirst(parent, child *html.Node) {
	if parent.FirstChild != nil {
		parent.InsertBefore(child, parent.FirstChild)
	} else {
		parent.AppendChild(child)
	}
}

// bodyOf 返回 document.body。
func bodyOf(doc *html.Node) *html.Node { return qs(doc, "body") }
