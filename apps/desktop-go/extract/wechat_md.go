package extract

// 对应 wechat.ts 里的 Turndown 配置：atx 标题、围栏代码块、默认无序列表标记 "*"，
// 并补一条规则——没有 src 的图片输出「[图片未保存]」。Markdown 与 Turndown 的字节级差异已列入接受清单。

import (
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// WechatHTMLToMarkdown 把已净化的公众号正文 HTML 转成 Markdown（等价于 turndown(html).trim()）。
func WechatHTMLToMarkdown(fragment string) string {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(
				commonmark.WithBulletListMarker("*"),
				commonmark.WithEmDelimiter("_"),
				commonmark.WithListEndComment(false), // 相邻列表之间不写 <!--THE END-->，Turndown 没有这个产物
			),
		),
	)
	conv.Register.RendererFor("img", converter.TagTypeInline, func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		if getAttr(n, "src") != "" {
			return converter.RenderTryNext
		}
		_, _ = w.WriteString("\n\n[图片未保存]\n\n")
		return converter.RenderSuccess
	}, converter.PriorityEarly)
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return ""
	}
	wrapper := createElement("div")
	for _, n := range nodes {
		wrapper.AppendChild(n)
	}
	out, err := conv.ConvertNode(wrapper)
	if err != nil {
		return ""
	}
	return jsTrim(string(out))
}

// DiscourseHTMLToMarkdown 对应 discourse.ts 的 Turndown 配置（atx 标题、围栏代码块、"-" 列表，丢弃 script/style/noscript/iframe）。
// 与 Turndown 的字节级差异属已接受差异。
func DiscourseHTMLToMarkdown(fragment string) string {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(
				commonmark.WithBulletListMarker("-"),
				commonmark.WithEmDelimiter("_"),
				commonmark.WithListEndComment(false), // 相邻列表之间不写 <!--THE END-->，Turndown 没有这个产物
			),
		),
	)
	for _, tag := range []string{"script", "style", "noscript", "iframe"} {
		conv.Register.TagType(tag, converter.TagTypeRemove, converter.PriorityStandard)
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return ""
	}
	wrapper := createElement("div")
	for _, n := range nodes {
		wrapper.AppendChild(n)
	}
	out, err := conv.ConvertNode(wrapper)
	if err != nil {
		return ""
	}
	return jsTrim(string(out))
}

// NodeseekHTMLToMarkdown 对应 import/nodeseek.ts 的 Turndown 配置（atx 标题、围栏代码块、默认 "*" 列表，
// 丢弃 script/style/noscript/iframe/form）。与 Turndown 的字节级差异属已接受差异。
func NodeseekHTMLToMarkdown(fragment string) string {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(
				commonmark.WithBulletListMarker("*"),
				commonmark.WithEmDelimiter("_"),
				commonmark.WithListEndComment(false),
			),
		),
	)
	for _, tag := range []string{"script", "style", "noscript", "iframe", "form"} {
		conv.Register.TagType(tag, converter.TagTypeRemove, converter.PriorityStandard)
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return ""
	}
	wrapper := createElement("div")
	for _, n := range nodes {
		wrapper.AppendChild(n)
	}
	out, err := conv.ConvertNode(wrapper)
	if err != nil {
		return ""
	}
	return jsTrim(string(out))
}
