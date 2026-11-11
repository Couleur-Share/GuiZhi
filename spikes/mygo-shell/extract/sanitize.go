package extract

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// 移植自 snapshot-sanitize.ts 的 cleanHtml（sanitize-html 白名单），不含 style 属性的 CSS 清理：
// 本函数一律丢弃 style 属性，CSS 白名单需要 css-tree 的 Go 对应物，另行验证。

var sanitizeTags = strset("div section article main p span br hr h1 h2 h3 h4 h5 h6 b strong i em u s small sub sup blockquote pre code ul ol li table thead tbody tfoot tr td th caption colgroup col a img figure figcaption svg g path circle ellipse rect line polyline polygon text tspan")

var sanitizeNonText = strset("script style textarea option noscript iframe object embed form foreignobject")

var sanitizeSVGAttrs = strset("viewbox xmlns d x y x1 x2 y1 y2 cx cy r rx ry points fill stroke stroke-width transform text-anchor")

// 每个标签额外允许的属性（与全局属性取并集，对应 sanitize-html 的 allowedAttributes）。
var sanitizeTagAttrs = map[string]map[string]bool{
	"a":   strset("href title id class style"),
	"img": strset("src alt width height style class"),
	"td":  strset("colspan rowspan style"),
	"th":  strset("colspan rowspan style"),
	"ol":  strset("start style"),
	"svg": strset("width height style"),
}

var sanitizeGlobalAttrs = strset("id class style title lang dir")

var sanitizeSchemes = strset("https http local-image")

var (
	svgPaintOK    = regexp.MustCompile(`(?i)^(#[a-f0-9]{3,8}|[a-z]+|rgba?\([\d.,%\s]+\))$`)
	hrefKeep      = regexp.MustCompile(`(?i)^(https?://|#[\w-])`)
	schemeRe      = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9.+\-]*):`)
	hrefCtrlStrip = regexp.MustCompile(`[\x00-\x20]+`)
	sanitizeVoid  = strset("br hr img col")
)

func strset(s string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.Fields(s) {
		m[f] = true
	}
	return m
}

// urlSchemeAllowed 对应 sanitize-html 的 naughtyHref：去掉空白与控制字符后取协议，
// 不在白名单则拒绝；无协议时相对路径放行，协议相对路径（//host）拒绝。
func urlSchemeAllowed(raw string) bool {
	h := hrefCtrlStrip.ReplaceAllString(raw, "")
	m := schemeRe.FindStringSubmatch(h)
	if m == nil {
		return !strings.HasPrefix(h, "//")
	}
	return sanitizeSchemes[strings.ToLower(m[1])]
}

// CleanHTML 清理不可信 HTML 片段。allowImage 判断 img 的 src 是否是本快照登记的本地资源。
func CleanHTML(src string, allowImage func(string) bool) string {
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, _ := html.ParseFragment(strings.NewReader(src), ctx)
	var b strings.Builder
	for _, n := range nodes {
		writeClean(&b, n, allowImage)
	}
	return b.String()
}

func writeClean(b *strings.Builder, n *html.Node, allowImage func(string) bool) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(html.EscapeString(n.Data))
	case html.ElementNode:
		tag := strings.ToLower(n.Data)
		// htmlparser2 在 SVG 内保留标签大小写，因此 foreignObject 匹配不到 "foreignobject"：
		// 产品里该条目实际不生效，内容仍会保留（再经白名单过滤）。这里照搬该行为。
		nonTextKey := tag
		if n.Namespace == "svg" {
			nonTextKey = n.Data
		}
		if sanitizeNonText[nonTextKey] {
			return
		}
		if !sanitizeTags[tag] {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				writeClean(b, c, allowImage)
			}
			return
		}
		attrs := map[string]string{}
		var order []string
		for _, a := range n.Attr {
			k := strings.ToLower(a.Key)
			if _, dup := attrs[k]; !dup {
				order = append(order, k)
				attrs[k] = a.Val
			}
		}
		// 与 transformTags 一致：先改写，再按白名单过滤
		for _, k := range []string{"fill", "stroke"} {
			if v, ok := attrs[k]; ok && v != "" && !svgPaintOK.MatchString(v) {
				delete(attrs, k)
			}
		}
		if tag == "img" {
			if v := attrs["src"]; v != "" && allowImage(v) {
				attrs["src"] = v
			} else {
				delete(attrs, "src")
				if attrs["alt"] == "" {
					attrs["alt"] = "图片未保存"
					order = append(order, "alt")
				}
			}
		}
		if v, ok := attrs["href"]; tag == "a" && ok && v != "" && !hrefKeep.MatchString(v) {
			delete(attrs, "href")
		}
		b.WriteByte('<')
		b.WriteString(tag)
		seen := map[string]bool{}
		for _, k := range order {
			v, ok := attrs[k]
			if !ok || seen[k] || k == "style" || v == "" { // sanitize-html 丢弃空值属性
				continue
			}
			seen[k] = true
			if !(sanitizeGlobalAttrs[k] || sanitizeSVGAttrs[k] || sanitizeTagAttrs[tag][k]) {
				continue
			}
			if (k == "href" || k == "src") && !urlSchemeAllowed(v) {
				continue
			}
			b.WriteByte(' ')
			b.WriteString(k)
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(v))
			b.WriteByte('"')
		}
		if sanitizeVoid[tag] {
			b.WriteString(" />")
			return
		}
		b.WriteByte('>')
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeClean(b, c, allowImage)
		}
		b.WriteString("</" + tag + ">")
	}
}
