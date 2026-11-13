package main

// 本文问答的「AI 阅读页」来源。对应 article-ask-context.ts 的 themed 分支：
// 版本经 themedReadingDocument 渲染为 HTML 后取可见文字，并带上草稿实际引用过的就绪资料。

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// htmlVisibleText 对应 article-ask-context.ts 的 htmlText：去掉脚本/样式/导航/按钮/输入控件，
// 块级元素末尾补空行，取 body 文字。
func htmlVisibleText(src string) string {
	if !strings.Contains(src, "<html") {
		src = "<html><body>" + src + "</body></html>"
	}
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return ""
	}
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if body == nil && n.Type == html.ElementNode && n.DataAtom == atom.Body {
			body = n
			return
		}
		for c := n.FirstChild; c != nil && body == nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if body == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			sb.WriteString(n.Data)
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Nav, atom.Button, atom.Input, atom.Textarea:
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			switch n.DataAtom {
			case atom.P, atom.Div, atom.H1, atom.H2, atom.H3, atom.Li, atom.Tr, atom.Section:
				sb.WriteString("\n\n")
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(body)
	return sb.String()
}

func (l *Library) themedArticleSource(ctx context.Context, itemID string, t articleTarget, question string) (themedArticleSource, error) {
	version, err := l.themedPages().getVersion(ctx, *t.VersionID)
	if err != nil {
		return themedArticleSource{}, err
	}
	kind := "body"
	if t.SourceKind != nil {
		kind = *t.SourceKind
	}
	if version == nil || themedStr(version, "itemId") != itemID || themedStr(version, "sourceKind") != kind {
		return themedArticleSource{}, errors.New("正在查看的 AI 阅读版本已不存在或不属于本文")
	}
	doc, err := themedCall("document", themedJSON(version), nil, nil)
	if err != nil {
		return themedArticleSource{}, err
	}
	out := themedArticleSource{text: htmlVisibleText(doc), original: jsToString(jsGet(jsGet(version, "source"), "content"))}
	rec, _ := jsGet(version, "reconstruction").(*jsObj)
	if rec == nil {
		return out, nil
	}
	used := map[string]bool{}
	if draft, ok := rec.get("draft").(jsArr); ok {
		for _, sec := range draft {
			if ids, ok := jsGet(sec, "referenceIds").(jsArr); ok {
				for _, id := range ids {
					used[jsToString(id)] = true
				}
			}
		}
	}
	refs, _ := rec.get("references").(jsArr)
	for _, ref := range refs {
		txt, _ := jsGet(ref, "text").(string)
		if len(out.prior) >= 3 {
			break
		}
		if themedStr(ref, "status") != "ready" || strings.TrimSpace(txt) == "" || !used[themedStr(ref, "id")] {
			continue
		}
		s := articleSource{Kind: "web", Title: "AI 阅读页已有资料 · " + themedStr(ref, "title"), URL: themedStr(ref, "url"),
			Text: selectArticleContext(txt, question, "", 1200).Text}
		if v, ok := jsGet(ref, "capturedAt").(float64); ok {
			c := int64(v)
			s.CapturedAt = &c
		}
		out.prior = append(out.prior, s)
	}
	return out, nil
}
