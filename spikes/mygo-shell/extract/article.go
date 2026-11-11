package extract

import (
	"net/url"
	"regexp"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
)

// 常量与 web-extract-article.ts 一一对应。
const (
	markdownLimit        = 200_000
	readabilityMinChars  = 100
	fallbackRatio        = 0.6
	fallbackMinChars     = 500
	regionMinChars       = 200
	titleBlockMaxChars   = 1500
	leadMinChars         = 30
	headingMark          = "data-gz-h1"
	codeLanguageAttrName = "data-gz-lang"
)

var (
	ws             = "[" + jsWS + "]"
	reInvis        = regexp.MustCompile(`(?i)(?:^|;)` + ws + `*(?:display` + ws + `*:` + ws + `*none|visibility` + ws + `*:` + ws + `*hidden)` + ws + `*(?:;|$|!)`)
	reChrome       = regexp.MustCompile(`(?i)(?:^|[` + jsWS + `_-])(?:sidebar|widget|comments?|comment-list|respond|disqus|related|recommend(?:ed|ation)?s?|share|sharing|social|newsletter|subscribe|promo|advert(?:isement)?|ads?|breadcrumbs?|pagination|pager|toc|cookie|consent|feedback)(?:$|[` + jsWS + `_-])`)
	rePerma        = regexp.MustCompile(`^[` + jsWS + `\x{200b}#¶§\x{1f517}∞]*$`)
	reCaptcha      = regexp.MustCompile(`(?i)^(?:just a moment|verify you are human|人机验证|安全验证)`)
	reLoginTitle   = regexp.MustCompile(`(?i)登录|登入|sign in|log in|login`)
	reDisplayNone  = regexp.MustCompile(`(?i)display` + ws + `*:` + ws + `*none|visibility` + ws + `*:` + ws + `*hidden`)
	reDisplayNoneG = regexp.MustCompile(`(?i)(?:display` + ws + `*:` + ws + `*none|visibility` + ws + `*:` + ws + `*hidden)` + ws + `*;?`)

	contentClasses = map[string]bool{"entry-content": true, "post-content": true, "article-content": true, "post-body": true}
	chromeNames    = map[string]bool{
		"sidebar": true, "widget": true, "widget-area": true, "comments": true, "comment-list": true, "comment-respond": true,
		"respond": true, "disqus_thread": true, "related-posts": true, "related-articles": true, "post-navigation": true,
		"social-share": true, "share-buttons": true,
	}
	permalinkAnchors = strings.Join([]string{
		`h1 a[href^="#"]`, `h2 a[href^="#"]`, `h3 a[href^="#"]`, `h4 a[href^="#"]`, `h5 a[href^="#"]`, `h6 a[href^="#"]`, `dt a[href^="#"]`,
		`a.headerlink[href^="#"]`,
	}, ", ")
	linkProtocols = map[string]bool{"http": true, "https": true, "mailto": true, "tel": true}
)

// Article 对应 WebArticle（只保留对拍需要的字段）。
type Article struct {
	Title            string
	Author           string
	PublishedAt      *int64
	Markdown         string
	Source           string
	ReadabilityChars int
	RegionChars      int
	ErrorCode        string
	Truncated        bool
	Status           int
}

func parsePage(src string) *html.Node {
	if !regexp.MustCompile(`(?i)<html(?:` + ws + `|>)`).MatchString(src) {
		src = "<html><body>" + src + "</body></html>"
	}
	doc, _ := html.Parse(strings.NewReader(src))
	return doc
}

func textLength(n *html.Node) int {
	if n == nil {
		return 0
	}
	return utf16Len(removeWS(textContent(n)))
}

// paragraphLength：只计表格之外 <p> 里的文字。
func paragraphLength(node *html.Node) int {
	if node == nil {
		return 0
	}
	total := 0
	type item struct {
		n   *html.Node
		inP bool
	}
	pending := []item{{node, false}}
	for len(pending) > 0 {
		cur := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for c := cur.n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode && cur.inP {
				total += utf16Len(removeWS(c.Data))
			} else if c.Type == html.ElementNode && nodeName(c) != "TABLE" {
				pending = append(pending, item{c, cur.inP || nodeName(c) == "P"})
			}
		}
	}
	return total
}

func absoluteURL(value, base string) *url.URL {
	value = jsTrim(value)
	if value == "" {
		return nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return nil
	}
	r := b.ResolveReference(u)
	if r.Scheme == "" {
		return nil
	}
	return r
}

type pageMeta struct {
	title         string
	author        string
	publishedAt   *int64
	requiresLogin bool
	baseURL       string
}

func readMeta(doc *html.Node, pageURL string) pageMeta {
	titleNode := qs(doc, "head > title")
	if titleNode == nil {
		titleNode = qs(doc, "title")
	}
	title := jsTrim(collapseWS(textContent(titleNode)))
	if title == "" {
		title = jsTrim(collapseWS(textContent(qs(doc, "h1"))))
	}
	title = sliceUTF16(title, 300)
	var base *url.URL
	if b := qs(doc, "base[href]"); b != nil {
		base = absoluteURL(getAttr(b, "href"), pageURL)
	}
	var authors []string
	for _, m := range qsa(doc, `meta[name="author"]`) {
		authors = append(authors, getAttr(m, "content"))
	}
	baseURL := pageURL
	if base != nil && (base.Scheme == "http" || base.Scheme == "https") {
		baseURL = base.String()
	}
	return pageMeta{
		title:         title,
		author:        sliceUTF16(jsTrim(strings.Join(authors, " ")), 300),
		publishedAt:   publishedFromMeta(doc),
		requiresLogin: qs(doc, `input[type="password"]`) != nil && reLoginTitle.MatchString(title),
		baseURL:       baseURL,
	}
}

func restoreEditorLines(doc *html.Node) {
	for _, editor := range qsa(doc, ".cm-editor, .CodeMirror") {
		if !isConnected(editor) {
			continue
		}
		lines := qsa(editor, ".cm-line, .CodeMirror-line")
		if len(lines) == 0 {
			continue
		}
		pre, code := createElement("pre"), createElement("code")
		parts := make([]string, len(lines))
		for i, l := range lines {
			parts[i] = textContent(l)
		}
		code.AppendChild(textNode(strings.Join(parts, "\n")))
		pre.AppendChild(code)
		replaceWith(editor, pre)
	}
}

func revealDisclosures(doc *html.Node) {
	var parts []string
	for _, scope := range []string{"details", "[role='tabpanel']"} {
		parts = append(parts, scope+"[hidden]", scope+"[aria-hidden='true']", scope+"[style]", scope+" [hidden]", scope+" [aria-hidden='true']", scope+" [style]")
	}
	for _, n := range qsa(doc, strings.Join(parts, ", ")) {
		removeAttr(n, "hidden")
		if getAttr(n, "aria-hidden") == "true" {
			removeAttr(n, "aria-hidden")
		}
		if style, ok := attr(n, "style"); ok && style != "" && reDisplayNone.MatchString(style) {
			setAttr(n, "style", reDisplayNoneG.ReplaceAllString(style, ""))
		}
	}
}

type lead struct{ html, text string }

type titleBlockT struct {
	heading, headingText string
	leads                []lead
}

func normalized(s string) string { return removeWS(s) }

func titleBlock(doc *html.Node) *titleBlockT {
	heading := qs(doc, "h1")
	if heading == nil || closest(heading, "main, [role='main'], article") == nil {
		return nil
	}
	for node := parentElement(heading); node != nil; node = parentElement(node) {
		if name := nodeName(node); name == "BODY" || name == "MAIN" || name == "ARTICLE" {
			break
		}
		if textLength(node) > titleBlockMaxChars {
			return nil
		}
		after := false
		var leads []lead
		for _, child := range qsa(node, "h1, p") {
			if child == heading {
				after = true
			} else if after && nodeName(child) == "P" && textLength(child) >= leadMinChars && len(leads) < 3 &&
				closest(child, "details, aside, nav, footer, form, figure") == nil {
				leads = append(leads, lead{outerHTML(child), normalized(textContent(child))})
			}
		}
		if len(leads) > 0 {
			return &titleBlockT{outerHTML(heading), normalized(textContent(heading)), leads}
		}
	}
	return nil
}

func restoreTitleBlock(article *html.Node, block *titleBlockT) {
	if block == nil {
		return
	}
	text := normalized(textContent(article))
	var missing []lead
	for _, l := range block.leads {
		if !strings.Contains(text, l.text) {
			missing = append(missing, l)
		}
	}
	if len(missing) == 0 {
		return
	}
	holder := createElement("div")
	var sb strings.Builder
	if !strings.Contains(text, block.headingText) {
		sb.WriteString(block.heading)
	}
	for _, l := range missing {
		sb.WriteString(l.html)
	}
	setInnerHTML(holder, sb.String())
	insertFirst(article, holder)
}

func prepare(doc *html.Node) {
	for _, n := range qsa(doc, "script, style") {
		remove(n)
	}
	restoreEditorLines(doc)
	revealDisclosures(doc)
	for _, pre := range qsa(doc, "pre") {
		if lang := codeLanguage(pre); lang != "" {
			setAttr(pre, codeLanguageAttrName, lang)
		}
	}
	for _, h := range qsa(doc, "h1") {
		setAttr(h, headingMark, "")
	}
	for _, a := range qsa(doc, permalinkAnchors) {
		if rePerma.MatchString(textContent(a)) && qs(a, "img") == nil {
			remove(a)
		}
	}
}

func hasContentClass(n *html.Node) bool {
	if getAttr(n, "itemprop") == "articleBody" {
		return true
	}
	for _, name := range strings.FieldsFunc(strings.ToLower(getAttr(n, "class")), isJSSpace) {
		if contentClasses[name] {
			return true
		}
	}
	return false
}

func isContentNode(n *html.Node) bool { return nodeName(n) == "ARTICLE" || hasContentClass(n) }

func innermostUnique(nodes []*html.Node) *html.Node {
	var leaves []*html.Node
	for _, n := range nodes {
		leaf := true
		for _, o := range nodes {
			if o != n && contains(n, o) {
				leaf = false
				break
			}
		}
		if leaf {
			leaves = append(leaves, n)
		}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	return nil
}

func semanticRegion(doc *html.Node) *html.Node {
	var byClass []*html.Node
	for _, n := range qsa(doc, "[class], [itemprop]") {
		if hasContentClass(n) {
			byClass = append(byClass, n)
		}
	}
	for _, list := range [][]*html.Node{byClass, qsa(doc, "main, article, [role='main']")} {
		if region := innermostUnique(list); region != nil && textLength(region) >= regionMinChars {
			return region
		}
	}
	return nil
}

func cleanRegion(region *html.Node) *html.Node {
	for _, button := range qsa(region, "td button, th button") {
		span := createElement("span")
		for button.FirstChild != nil {
			c := button.FirstChild
			button.RemoveChild(c)
			span.AppendChild(c)
		}
		replaceWith(button, span)
	}
	for _, n := range qsa(region, "script, style, noscript, template, form, button, input, select, textarea, [hidden], [aria-hidden='true']") {
		remove(n)
	}
	for _, n := range qsa(region, "*") {
		if !contains(region, n) {
			continue
		}
		names := strings.ToLower(getAttr(n, "class") + " " + getAttr(n, "id"))
		chrome := false
		for _, name := range strings.FieldsFunc(names, isJSSpace) {
			if chromeNames[name] {
				chrome = true
				break
			}
		}
		if chrome || getAttr(n, "role") == "complementary" ||
			(reChrome.MatchString(names) && qs(n, "pre, table, h1, h2") == nil) {
			remove(n)
		}
	}
	for _, n := range qsa(region, "aside, nav, footer, header") {
		if !contains(region, n) {
			continue
		}
		inside := false
		for p := parentElement(n); p != nil && !inside; p = parentElement(p) {
			inside = isContentNode(p)
		}
		if !inside {
			remove(n)
		}
	}
	return region
}

func wechatRoot(doc *html.Node, pageURL string) *html.Node {
	if u := absoluteURL(pageURL, pageURL); u == nil || u.Hostname() != "mp.weixin.qq.com" {
		return nil
	}
	root := qs(doc, "#js_content")
	if root == nil {
		return nil
	}
	removeAttr(root, "style")
	for _, img := range qsa(root, "img[data-src]") {
		if src := absoluteURL(getAttr(img, "data-src"), pageURL); src != nil && (src.Scheme == "http" || src.Scheme == "https") {
			setAttr(img, "src", src.String())
		}
	}
	return root
}

func absolutize(root *html.Node, base string) {
	for _, link := range qsa(root, "a[href]") {
		if t := absoluteURL(getAttr(link, "href"), base); t != nil && linkProtocols[t.Scheme] {
			setAttr(link, "href", t.String())
		} else {
			removeAttr(link, "href")
		}
	}
	for _, img := range qsa(root, "img") {
		declared := jsTrim(getAttr(img, "src"))
		lazy := ""
		for _, name := range []string{"data-src", "data-original", "data-lazy-src"} {
			if v := getAttr(img, name); v != "" {
				lazy = v
				break
			}
		}
		candidate := declared
		if declared == "" || strings.HasPrefix(strings.ToLower(declared), "data:") {
			candidate = lazy
			if candidate == "" {
				if f := strings.Fields(jsTrim(getAttr(img, "srcset"))); len(f) > 0 {
					candidate = f[0]
				}
			}
		}
		if src := absoluteURL(candidate, base); src != nil && (src.Scheme == "http" || src.Scheme == "https") {
			setAttr(img, "src", src.String())
		} else {
			remove(img)
		}
	}
}

func restoreHeadings(root *html.Node) {
	for _, h := range qsa(root, "h2["+headingMark+"]") {
		h1 := createElement("h1")
		for h.FirstChild != nil {
			c := h.FirstChild
			h.RemoveChild(c)
			h1.AppendChild(c)
		}
		replaceWith(h, h1)
	}
}

func readabilityContent(doc *html.Node) *html.Node {
	p := readability.NewParser()
	p.CharThresholds = readabilityMinChars
	applyProductOptions(&p)
	art, err := p.ParseAndMutate(doc, nil)
	if err != nil || art.Node == nil || art.Node.Type != html.ElementNode {
		return nil
	}
	return art.Node
}

func truncate(md string) (string, bool) {
	if utf16Len(md) <= markdownLimit {
		return md, false
	}
	count := 0
	for i := range md { // i 是码点的字节下标
		if count == markdownLimit {
			return md[:i], true
		}
		count++
	}
	return md, false
}
func classify(status int, requiresLogin bool, title, text, markdown string) string {
	switch {
	case status == 401 || status == 407 || requiresLogin:
		return "login"
	case status == 403 || status == 429:
		return "restricted"
	case status >= 400:
		return "network"
	case reCaptcha.MatchString(title):
		return "captcha"
	case text == "" || markdown == "":
		return "empty"
	}
	return ""
}

// ExtractWebArticle 对应 TS 的 extractWebArticle。
func ExtractWebArticle(src, pageURL string, status int) Article {
	doc := parsePage(src)
	meta := readMeta(doc, pageURL)
	title := meta.title
	source := "readability"
	readabilityChars, regionChars := 0, 0
	prepare(doc)
	root := wechatRoot(doc, pageURL)
	if root != nil {
		source = "wechat"
		t := jsTrim(textContent(qs(doc, "#activity-name")))
		if t == "" {
			if og := qs(doc, `meta[property="og:title"]`); og != nil {
				t = jsTrim(getAttr(og, "content"))
			}
		}
		if t == "" {
			t = title
		}
		title = sliceUTF16(t, 300)
	} else {
		regionDoc := parsePage(src)
		prepare(regionDoc)
		block := titleBlock(regionDoc)
		region := semanticRegion(regionDoc)
		article := readabilityContent(doc)
		var cleaned *html.Node
		if region != nil {
			cleaned = cleanRegion(region)
		}
		readabilityChars = paragraphLength(article)
		regionChars = paragraphLength(cleaned)
		if cleaned != nil && regionChars >= fallbackMinChars && float64(readabilityChars) < fallbackRatio*float64(regionChars) {
			root, source = cleaned, "region"
		} else {
			root = article
			if article != nil && textLength(article) < readabilityMinChars {
				page := parsePage(src)
				prepare(page)
				for _, n := range qsa(page, "[style]") {
					if reInvis.MatchString(getAttr(n, "style")) {
						remove(n)
					}
				}
				var body *html.Node
				if b := bodyOf(page); b != nil {
					body = cleanRegion(b)
				}
				if body != nil && textLength(body) > 0 {
					root, source = body, "page"
				}
			}
			if root != nil {
				restoreTitleBlock(root, block)
			}
		}
	}
	markdown, text := "", ""
	if root != nil {
		for _, n := range qsa(root, "script, style, noscript, template") {
			remove(n)
		}
		absolutize(root, meta.baseURL)
		restoreHeadings(root)
		text = jsTrim(textContent(root))
		markdown = htmlToMarkdown(root)
	} else {
		source = "none"
	}
	errCode := classify(status, meta.requiresLogin, title, text, markdown)
	clipped, truncated := truncate(markdown)
	if title == "" {
		title = pageURL
	}
	return Article{Title: title, Author: meta.author, PublishedAt: meta.publishedAt, Markdown: clipped, Source: source, ReadabilityChars: readabilityChars, RegionChars: regionChars, ErrorCode: errCode, Truncated: truncated, Status: status}
}

func publishedFromMeta(doc *html.Node) *int64 {
	n := qs(doc, `meta[property="article:published_time"]`)
	if n == nil {
		return nil
	}
	return publishedTime(getAttr(n, "content"))
}
