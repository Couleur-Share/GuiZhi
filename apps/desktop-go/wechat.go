package main

// 对应 services/web-capture/wechat.ts：公众号正文已在 HTML 里，直接抓取、不运行页面脚本；
// 同一次解析生成原文快照（净化 HTML/CSS + 图片资产）与可编辑 Markdown。
// 净化走 websnap_engine（goja 里跑 TS 原文件打出的包），保证与 Electron 版逐字节一致。
// 已知差异：HTML 解析器是 x/net/html（TS 为 linkedom）；Markdown 为 html-to-markdown（TS 为 Turndown）；SVG 图片不栅格化。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"guizhi-mygo-shell/extract"
)

const wechatEngineVersion = "wechat-html/1"

var reTimeZoneTail = regexp.MustCompile(`(?:Z|[+-]\d\d:\d\d)$`)

func parseISOMillis(s string) (int64, bool) { return legacyDateParse(s) }

var (
	selWechatBody   = cascadia.MustCompile("#js_content")
	selWechatTitle  = cascadia.MustCompile("#activity-name")
	selWechatName   = cascadia.MustCompile("#js_name")
	selDocTitle     = cascadia.MustCompile("title")
	selStyleTag     = cascadia.MustCompile("style")
	selBodyImg      = cascadia.MustCompile("img")
	selBodyImgSrc   = cascadia.MustCompile("img[src]")
	selBodyAnchor   = cascadia.MustCompile("a[href]")
	selBodyStyled   = cascadia.MustCompile("[style]")
	selWechatMedia  = cascadia.MustCompile("iframe,video,audio,mpvoice,mpvideo,mp-miniprogram,wx-open-launch-weapp")
	selWechatMotion = cascadia.MustCompile("animate,animateTransform,set,foreignObject")
)

func nodeAttr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func nodeSetAttr(n *html.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func nodeOuterHTML(n *html.Node) string {
	var buf bytes.Buffer
	_ = html.Render(&buf, n)
	return buf.String()
}

// styleDecl 是 style 属性里的一条声明。
type styleDecl struct{ name, value string }

// parseStyleAttr 按分号拆分声明（括号与引号内的分号不算），忽略没有冒号的片段。
func parseStyleAttr(s string) []styleDecl {
	var parts []string
	depth, start := 0, 0
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			if depth > 0 {
				depth--
			}
		case r == ';' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	var out []styleDecl
	for _, p := range parts {
		idx := strings.Index(p, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(p[:idx])
		if name == "" {
			continue
		}
		out = append(out, styleDecl{strings.ToLower(name), strings.TrimSpace(p[idx+1:])})
	}
	return out
}

// removeStyleProperty 对应 element.style.removeProperty(name)；只在确有该属性时改写 style 属性。
func removeStyleProperty(n *html.Node, name string) {
	raw, ok := nodeAttr(n, "style")
	if !ok {
		return
	}
	decls := parseStyleAttr(raw)
	kept := make([]string, 0, len(decls))
	removed := false
	for _, d := range decls {
		if d.name == name {
			removed = true
			continue
		}
		kept = append(kept, d.name+": "+d.value+";")
	}
	if removed {
		nodeSetAttr(n, "style", strings.Join(kept, " "))
	}
}

// styleValue 取某个属性值（去掉首尾空白，保留 !important）。
func styleValue(n *html.Node, name string) string {
	raw, ok := nodeAttr(n, "style")
	if !ok {
		return ""
	}
	val := ""
	for _, d := range parseStyleAttr(raw) {
		if d.name == name {
			val = d.value
		}
	}
	// 与 linkedom 一致：带 !important 的值不等于裸值（display: none !important 不会被当作 none）
	return strings.TrimSpace(val)
}

func replaceNode(old, repl *html.Node) {
	if old.Parent == nil {
		return
	}
	old.Parent.InsertBefore(repl, old)
	old.Parent.RemoveChild(old)
}

func newElement(tag string, atomID atom.Atom) *html.Node {
	return &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atomID}
}

// captureWechat 对应 captureWechat；失败以 error 返回（调用方折成结果或任务失败）。
func (l *Library) captureWechat(parent context.Context, rawURL, taskID string) (webCaptureResult, error) {
	if !isWechatURL(rawURL) {
		return webCaptureResult{}, errors.New("不是微信公众号地址")
	}
	ctx, cancel := context.WithTimeout(parent, 180*time.Second)
	defer cancel()
	fetcher := l.webFetcher
	if fetcher == nil {
		fetcher = defaultSafeFetcher
	}
	fetched, err := fetcher.fetchHTML(ctx, rawURL, safeRequestOptions{})
	if err != nil {
		return webCaptureResult{}, err
	}
	if !isWechatURL(fetched.FinalURL) {
		return webCaptureResult{}, errors.New("公众号地址跳转到了其他平台")
	}
	doc, err := html.Parse(strings.NewReader(markExplicitTbody(fetched.HTML)))
	if err != nil {
		return webCaptureResult{}, err
	}
	unwrapImpliedTbody(doc)
	body := cascadia.Query(doc, selWechatBody)
	if body == nil || (jsTrim(nodeText(body)) == "" && cascadia.Query(body, selBodyImg) == nil) {
		return webCaptureResult{}, errors.New("未获取到公众号正文，页面可能要求验证、已失效或受访问限制；请打开原文确认后重试")
	}
	meta := func(selector string) string {
		sel, err := cascadia.Parse(selector)
		if err != nil {
			return ""
		}
		if n := cascadia.Query(doc, sel); n != nil {
			if v, ok := nodeAttr(n, "content"); ok {
				return jsTrim(v)
			}
		}
		return ""
	}
	title := ""
	if n := cascadia.Query(doc, selWechatTitle); n != nil {
		title = jsTrim(nodeText(n))
	}
	if title == "" {
		title = meta(`meta[property="og:title"]`)
	}
	if title == "" {
		if n := cascadia.Query(doc, selDocTitle); n != nil {
			title = nodeText(n)
		}
	}
	if title == "" {
		title = rawURL
	}
	account := ""
	if n := cascadia.Query(doc, selWechatName); n != nil {
		account = jsTrim(nodeText(n))
	}
	author := meta(`meta[name="author"]`)
	date := meta(`meta[property="article:published_time"]`)
	var publishedAt *int64
	if reTimeZoneTail.MatchString(date) {
		if ms, ok := parseISOMillis(date); ok {
			publishedAt = &ms
		}
	}
	warnings := []string{}

	// 只移除已知入口容器的延迟显示属性，保留作者设置的其他样式。
	removeStyleProperty(body, "visibility")
	removeStyleProperty(body, "opacity")
	if styleValue(body, "display") == "none" {
		removeStyleProperty(body, "display")
	}
	for _, img := range cascadia.QueryAll(body, selBodyImg) {
		src, _ := nodeAttr(img, "data-src")
		if src == "" {
			src, _ = nodeAttr(img, "src")
		}
		if src != "" {
			nodeSetAttr(img, "src", src)
		}
	}
	for _, media := range cascadia.QueryAll(body, selWechatMedia) {
		p := newElement("p", atom.P)
		p.AppendChild(&html.Node{Type: html.TextNode, Data: "音视频或互动内容：请打开原文查看"})
		cover, _ := nodeAttr(media, "poster")
		if cover == "" {
			cover, _ = nodeAttr(media, "data-cover")
		}
		if cover != "" {
			img := newElement("img", atom.Img)
			img.Attr = []html.Attribute{{Key: "src", Val: cover}}
			p.AppendChild(img)
		}
		replaceNode(media, p)
		warnings = append(warnings, "音视频或互动内容仅保留说明与原文入口")
	}
	if cascadia.Query(body, selWechatMotion) != nil {
		warnings = append(warnings, "动态 SVG 装饰已降级为静态内容")
	}

	base, err := whatwg.Parse(fetched.FinalURL)
	if err != nil {
		return webCaptureResult{}, err
	}
	absolute := func(value string) (string, bool) {
		u, err := whatwg.ParseRef(base.Href(false), value)
		if err != nil {
			return "", false
		}
		if p := u.Protocol(); (p != "https:" && p != "http:") || u.Username() != "" || u.Password() != "" {
			return "", false
		}
		return u.Href(false), true
	}
	urls := []string{}
	urlSeen := map[string]bool{}
	type unsup struct{ key, reason string }
	var unsupported []unsup
	unsupSeen := map[string]int{}
	collect := func(value string) (string, bool) {
		if u, ok := absolute(value); ok {
			if !urlSeen[u] {
				urlSeen[u] = true
				urls = append(urls, u)
			}
			return u, true
		}
		if value != "" {
			key := utf16Slice(value, 200)
			if i, ok := unsupSeen[key]; ok {
				unsupported[i].reason = "图片或样式资源地址不支持离线保存"
			} else {
				unsupSeen[key] = len(unsupported)
				unsupported = append(unsupported, unsup{key, "图片或样式资源地址不支持离线保存"})
			}
		}
		return "", false
	}
	for _, img := range cascadia.QueryAll(body, selBodyImgSrc) {
		v, _ := nodeAttr(img, "src")
		collect(v)
	}
	coverURL := meta(`meta[property="og:image"]`)
	if coverURL != "" {
		collect(coverURL)
	}
	for _, a := range cascadia.QueryAll(body, selBodyAnchor) {
		href, _ := nodeAttr(a, "href")
		if !strings.HasPrefix(href, "#") {
			abs, _ := absolute(href)
			nodeSetAttr(a, "href", abs)
		}
	}
	for _, n := range cascadia.QueryAll(body, selBodyStyled) {
		v, _ := nodeAttr(n, "style")
		if _, err := snapshotCleanCSSFn(v, collect, false); err != nil {
			return webCaptureResult{}, err
		}
	}
	bodyStyle, _ := nodeAttr(body, "style")
	if _, err := snapshotCleanCSSFn(bodyStyle, collect, false); err != nil {
		return webCaptureResult{}, err
	}
	var styleParts []string
	for _, s := range cascadia.QueryAll(doc, selStyleTag) {
		styleParts = append(styleParts, nodeText(s))
	}
	stylesheet, err := snapshotCleanCSSFn(strings.Join(styleParts, "\n"), collect, true)
	if err != nil {
		return webCaptureResult{}, err
	}

	assets, failures, release := l.collectSnapshotAssets(ctx, urls)
	ok := false
	defer func() {
		if !ok {
			release()
		}
	}()
	for _, u := range unsupported {
		failures = append(failures, snapshotFailure{u.key, u.reason})
	}
	mapping := map[string]string{}
	for _, a := range assets {
		mapping[a.SourceURL] = "local-image://" + a.FileName
	}
	mapURL := func(value string) (string, bool) {
		abs, ok := absolute(value)
		if !ok {
			return "", false
		}
		v, ok := mapping[abs]
		return v, ok
	}
	cleanHTML, err := snapshotCleanHTMLFn(nodeOuterHTML(body), mapURL)
	if err != nil {
		return webCaptureResult{}, err
	}
	css, err := snapshotCleanCSSFn(stylesheet, mapURL, true)
	if err != nil {
		return webCaptureResult{}, err
	}
	warnings = uniqueStrings(warnings)

	snap := newObj()
	snap.set("formatVersion", float64(1))
	snap.set("policyVersion", float64(1))
	snap.set("adapterVersion", wechatEngineVersion)
	snap.set("html", cleanHTML)
	snap.set("css", css)
	snap.set("hash", "")
	snap.set("account", account)
	snap.set("author", author)
	if publishedAt != nil {
		snap.set("publishedAt", float64(*publishedAt))
	} else {
		snap.set("publishedAt", jsNull)
	}
	assetArr := jsArr{}
	for _, a := range assets {
		o := newObj()
		o.set("fileName", a.FileName)
		o.set("sha256", a.SHA256)
		o.set("sourceUrl", a.SourceURL)
		o.set("bytes", float64(a.Bytes))
		assetArr = append(assetArr, o)
	}
	snap.set("assets", assetArr)
	failArr := jsArr{}
	for _, f := range failures {
		o := newObj()
		o.set("url", f.URL)
		o.set("reason", f.Reason)
		failArr = append(failArr, o)
	}
	snap.set("failures", failArr)
	warnArr := make(jsArr, len(warnings))
	for i, w := range warnings {
		warnArr[i] = w
	}
	snap.set("warnings", warnArr)
	if cover, ok := mapURL(meta(`meta[property="og:image"]`)); ok {
		snap.set("cover", cover)
	}
	snapJSON, _ := jsStringify(snap)
	hash, err := snapshotHashOf(snapJSON)
	if err != nil {
		return webCaptureResult{}, err
	}
	snap.set("hash", hash)

	markdown := extract.WechatHTMLToMarkdown(cleanHTML) + "\n\n---\n\n来源：<" + fetched.FinalURL + ">"
	paragraphs := []webParagraph{}
	for i, p := range reParagraphSplit.Split(markdown, -1) {
		paragraphs = append(paragraphs, webParagraph{ID: fmt.Sprintf("p%d", i+1), Text: p})
	}
	sum := sha256.Sum256([]byte(markdown))
	confidence := "unknown"
	if publishedAt != nil {
		confidence = "exact"
	}
	resultWarnings := append([]string{}, warnings...)
	if len(failures) > 0 {
		resultWarnings = append(resultWarnings, fmt.Sprintf("%d 个图片资源未保存，可重试补采", len(failures)))
	}
	ok = true
	return webCaptureResult{
		TaskID: taskID, EntryURL: rawURL, FinalURL: fetched.FinalURL, Title: title, Author: author,
		PublishedAt: publishedAt, DateConfidence: confidence, Markdown: markdown, Links: []string{},
		Paragraphs: paragraphs, ContentHash: hex.EncodeToString(sum[:]), CapturedAt: nowMs(),
		EngineVersion: wechatEngineVersion, Complete: len(failures) == 0 && len(warnings) == 0,
		Truncated: false, Warnings: resultWarnings, Snapshot: snap, SnapshotAssets: assetNames(assets), release: release,
	}, nil
}

func assetNames(assets []snapshotAsset) []string {
	out := make([]string, len(assets))
	for i, a := range assets {
		out[i] = a.FileName
	}
	return out
}

const explicitTbodyMark = "data-gz-explicit"

// markExplicitTbody 给源码里显式写出的 <tbody> 打标记：x/net/html 按 HTML 规范会给 <table><tr> 补出 tbody，
// 而 TS 用的 linkedom 不补。标记后可以只去掉补出来的那些，让序列化结果与 linkedom 一致。
func markExplicitTbody(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out.String()
		}
		raw := z.Raw()
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			if name, _ := z.TagName(); string(name) == "tbody" {
				s := string(raw)
				if i := strings.IndexAny(s, " \t\r\n/>"); i > 0 {
					out.WriteString(s[:i] + " " + explicitTbodyMark + s[i:])
					continue
				}
			}
		}
		out.Write(raw)
	}
}

// unwrapImpliedTbody 去掉没有标记的 tbody（解析器补出来的），并清掉显式 tbody 上的标记。
func unwrapImpliedTbody(root *html.Node) {
	var list []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "tbody" {
				list = append(list, c)
			}
			walk(c)
		}
	}
	walk(root)
	for _, tb := range list {
		marked := false
		for i, a := range tb.Attr {
			if a.Key == explicitTbodyMark {
				marked = true
				tb.Attr = append(tb.Attr[:i], tb.Attr[i+1:]...)
				break
			}
		}
		if marked || tb.Parent == nil {
			continue
		}
		for tb.FirstChild != nil {
			c := tb.FirstChild
			tb.RemoveChild(c)
			tb.Parent.InsertBefore(c, tb)
		}
		tb.Parent.RemoveChild(tb)
	}
}
