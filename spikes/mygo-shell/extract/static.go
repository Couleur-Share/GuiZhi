package extract

// 对应 web-capture/web-static-route.ts：只凭静态 HTML 的正面证据接受正文，不把字数相近当成完整性。
//
// 与 TS 版（linkedom）的差别：Go 用 HTML5 规范的解析（x/net/html），页面有 <html> 但没有 <body> 时
// 也会补出 body；linkedom 不补，TS 版在那种页面上会因 body 为空而抛 TypeError。

import (
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/net/html"
)

type StaticDecision struct {
	Route           string   `json:"route"` // static | render | terminal
	Reasons         []string `json:"reasons"`
	TextChars       int      `json:"textChars"`
	SemanticRegions int      `json:"semanticRegions"`
	HasScripts      bool     `json:"hasScripts"`
}

var (
	reVerifyTitle = regexp.MustCompile(`(?i)^(?:just a moment|verify you are human|人机验证|安全验证)`)
	reScriptType  = regexp.MustCompile(`(?:java|ecma)script`)
	reHiddenStyle = regexp.MustCompile(`(?i)display\s*:\s*none|visibility\s*:\s*hidden`)
	reLoadingText = regexp.MustCompile(`(?i)^(?:loading(?:\s+(?:content|article|data))?[.…!\s]*|正在加载[.…!\s]*|加载中[.…!\s]*|please enable javascript[.!\s]*)$`)
	reLoginTitleS = regexp.MustCompile(`(?i)登录|登入|sign in|log in|login`)
)

func cloneNode(n *html.Node) *html.Node {
	c := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	c.Attr = append([]html.Attribute(nil), n.Attr...)
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.AppendChild(cloneNode(ch))
	}
	return c
}

func titleText(doc *html.Node) string {
	if t := qs(doc, "title"); t != nil {
		return jsTrim(textContent(t))
	}
	return ""
}

// IsWebVerificationPage 对应 isWebVerificationPage：Cloudflare 等人机验证页。
func IsWebVerificationPage(src string) bool {
	doc := parsePage(src)
	if reVerifyTitle.MatchString(titleText(doc)) {
		return true
	}
	return qs(doc, "#challenge-stage, #cf-wrapper, script[src*='/orchestrate/chl_page/']") != nil
}

// AssessStaticPage 对应 assessStaticPage。
func AssessStaticPage(src string, status int) StaticDecision {
	doc := parsePage(src)
	regions := qsa(doc, `main, article, [role="main"]`)
	root := bodyOf(doc)
	if len(regions) == 1 {
		root = regions[0]
	}
	executable := false
	for _, n := range qsa(doc, "script") {
		typ := strings.ToLower(getAttr(n, "type"))
		if typ == "" || typ == "module" || reScriptType.MatchString(typ) {
			executable = true
			break
		}
	}
	hasScripts := executable || qs(doc, "[onload], [onclick], [onerror]") != nil

	var clean *html.Node
	if root != nil {
		clean = cloneNode(root)
		for _, n := range qsa(clean, "script, style, noscript, template, nav, header, footer, form") {
			remove(n)
		}
	}
	textChars := 0
	if clean != nil {
		// JS 的 \s 与 Go 的 \s 不同（含 NBSP、BOM 等），用 domx 的 jsWS 版本折叠
		textChars = utf16Len(jsTrim(collapseWS(textContent(clean))))
	}
	stats := StaticDecision{TextChars: textChars, SemanticRegions: len(regions), HasScripts: hasScripts}
	title := titleText(doc)
	if status >= 400 || reVerifyTitle.MatchString(title) ||
		(qs(doc, `input[type="password"]`) != nil && reLoginTitleS.MatchString(title)) {
		stats.Route = "terminal"
		stats.Reasons = []string{"HTTP 或登录验证错误保持原结果"}
		return stats
	}
	if root == nil {
		stats.Route = "render"
		stats.Reasons = []string{"静态页面无正文"}
		return stats
	}

	var reasons []string
	if textChars == 0 {
		reasons = append(reasons, "静态页面无正文")
	}
	if qs(doc, `meta[http-equiv="refresh" i]`) != nil {
		reasons = append(reasons, "存在延迟跳转")
	}
	for node := root; node != nil; node = parentElement(node) {
		if matches(node, `[hidden], [aria-hidden="true"]`) || reHiddenStyle.MatchString(getAttr(node, "style")) {
			reasons = append(reasons, "正文区域初始隐藏")
			break
		}
	}
	const pending = `[aria-busy="true"], [data-loading="true"], [data-load-more], template[shadowrootmode]`
	if matches(root, pending) || qs(root, pending) != nil {
		reasons = append(reasons, "存在未加载内容标记")
	}
	loading := false
	for _, n := range qsa(clean, `p, div, span, [role="status"]`) {
		if closest(n, "pre, code") != nil {
			continue
		}
		if reLoadingText.MatchString(jsTrim(textContent(n))) {
			loading = true
			break
		}
	}
	if loading {
		reasons = append(reasons, "正文含加载占位")
	}
	if hasCustomElement(root) {
		reasons = append(reasons, "正文含自定义组件")
	}
	for _, a := range qsa(root, "pre a[href]") {
		if jsTrim(textContent(a)) == "" && qs(a, "img, svg") == nil && strings.Contains(getAttr(a, "href"), "#") {
			reasons = append(reasons, "代码含空锚点，需渲染后核对")
			break
		}
	}
	if hasScripts && len(regions) != 1 {
		reasons = append(reasons, "有脚本但正文区域不唯一")
	}
	if hasScripts && textChars < 400 {
		reasons = append(reasons, "有脚本且静态正文证据不足")
	}
	if len(reasons) > 0 {
		stats.Route, stats.Reasons = "render", reasons
		return stats
	}
	stats.Route = "static"
	if hasScripts {
		stats.Reasons = []string{"唯一正文区域且未发现动态占位"}
	} else {
		stats.Reasons = []string{"无可执行脚本的静态内容"}
	}
	return stats
}

// hasCustomElement 对应 root.querySelectorAll("*").some(tagName.includes("-"))：只看后代，不含 root 本身。
func hasCustomElement(root *html.Node) bool {
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && strings.Contains(c.Data, "-") {
				return true
			}
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(root)
}

// StaticPageLinks 对应 staticPageLinks：前 2000 个 a[href] 解析为 http(s) 绝对链接。
func StaticPageLinks(src, pageURL string) []string {
	doc := parsePage(src)
	nodes := qsa(doc, "a[href]")
	if len(nodes) > 2000 {
		nodes = nodes[:2000]
	}
	out := []string{}
	for _, n := range nodes {
		u, err := whatwg.ParseRef(pageURL, getAttr(n, "href"))
		if err != nil {
			continue
		}
		if p := u.Protocol(); p == "http:" || p == "https:" {
			out = append(out, u.Href(false))
		}
	}
	return out
}
