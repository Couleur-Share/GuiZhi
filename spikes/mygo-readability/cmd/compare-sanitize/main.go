// 对拍 sanitize-html：Go 的 CleanHTML 与产品 cleanHtml 在 35 个攻击用例与 59 个真实网页上比较。
// 两边输出都重新解析为规范 DOM、去掉 style 后比较。
// 用法：go run ./cmd/compare-sanitize <golden 目录>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"guizhi-mygo-readability/extract"
)

type item struct {
	ID   string `json:"id"`
	HTML string `json:"html"`
	Out  string `json:"out"`
}

func canon(s string) (string, string) {
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, _ := html.ParseFragment(strings.NewReader(s), ctx)
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	var strip func(n *html.Node)
	var text strings.Builder
	strip = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		if n.Type == html.ElementNode {
			kept := n.Attr[:0]
			for _, a := range n.Attr {
				if a.Key != "style" {
					kept = append(kept, a)
				}
			}
			n.Attr = kept
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			strip(c)
		}
	}
	strip(root)
	var sb strings.Builder
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&sb, c)
	}
	return sb.String(), strings.Join(strings.Fields(text.String()), "")
}

func main() {
	data, _ := os.ReadFile(filepath.Join(os.Args[1], "sanitize-golden.json"))
	var items []item
	_ = json.Unmarshal(data, &items)
	allow := func(u string) bool { return u == "local-image://wechat-"+strings.Repeat("a", 64)+".png" }
	var sameDOM, sameText, diffText, attackBad, attackN int
	for _, it := range items {
		goOut := extract.CleanHTML(it.HTML, allow)
		cg, tg := canon(goOut)
		cj, tj := canon(it.Out)
		isAttack := strings.HasPrefix(it.ID, "attack-")
		if isAttack {
			attackN++
		}
		switch {
		case cg == cj:
			sameDOM++
		case tg == tj:
			sameText++
			if isAttack {
				attackBad++
			}
			a, b := around(cj, cg)
			fmt.Printf("[结构不同] %s (ts %d 字节, go %d 字节)\n   ts: %s\n   go: %s\n", it.ID, len(cj), len(cg), a, b)
		default:
			diffText++
			if isAttack {
				attackBad++
			}
			fmt.Printf("[文字不同] %s (ts %d 字, go %d 字)\n   ts: %s\n   go: %s\n", it.ID, len([]rune(tj)), len([]rune(tg)), clip(cj), clip(cg))
		}
	}
	fmt.Printf("共 %d 条（攻击 %d）：DOM 完全一致 %d；文字一致但结构不同 %d；文字不同 %d；攻击用例中不一致 %d\n", len(items), attackN, sameDOM, sameText, diffText, attackBad)
}

// around 返回两个字符串在首个差异位置附近的片段。
func around(a, b string) (string, string) {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := i - 60
	if lo < 0 {
		lo = 0
	}
	cut := func(s string) string {
		hi := i + 120
		if hi > len(s) {
			hi = len(s)
		}
		if lo > len(s) {
			return ""
		}
		return strings.ReplaceAll(s[lo:hi], "\n", "↵")
	}
	return cut(a), cut(b)
}

func clip(s string) string {
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
