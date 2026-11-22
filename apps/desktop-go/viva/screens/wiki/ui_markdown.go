package wiki

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// wikiLinkOrigin 是站内 [[链接]] 在 Markdown 解析器里的占位地址：解析器只放行 http、https、mailto 的链接，
// 而 PreprocessLinks 产出的是 `#wiki=<目标>`（页内片段）。渲染前把它改写成「占位源 + 片段」，点击时再还原。
// .invalid 是保留的顶级域，不会解析到任何真实站点。
const wikiLinkOrigin = "https://wiki.invalid/"

// mdCache 缓存一页正文的解析结果：Markdown 解析与标题锚点的出现序号只在正文变了时算一次，
// 块指针跨帧稳定（锚点回调用块指针找回标题对应的锚点名）。
type mdCache struct {
	body   string
	valid  bool
	blocks []kit.MdBlock
	slugs  map[*kit.MdBlock]string
}

// get 返回 body 的块与每个二、三级标题的锚点名（与页内目录一致：同名标题按出现序号区分）。
func (m *mdCache) get(body string) ([]kit.MdBlock, map[*kit.MdBlock]string) {
	if m.valid && m.body == body {
		return m.blocks, m.slugs
	}
	src := strings.ReplaceAll(PreprocessLinks(body), "]("+HrefPrefix, "]("+wikiLinkOrigin+HrefPrefix)
	m.blocks = kit.ParseMarkdown(src)
	m.slugs = map[*kit.MdBlock]string{}
	used := map[string]int{}
	for i := range m.blocks {
		b := &m.blocks[i]
		if b.Kind != kit.MdHeading || (b.Level != 2 && b.Level != 3) {
			continue
		}
		text := kit.MdPlain(b.Spans)
		base := Slugify(text, 0)
		occurrence := used[base]
		used[base] = occurrence + 1
		m.slugs[b] = Slugify(text, occurrence)
	}
	m.body, m.valid = body, true
	return m.blocks, m.slugs
}

// wikiMarkdown 渲染一页正文（渲染层 WikiMarkdown）：[[链接]] 画成站内小块、点击跳页；http(s) 外链经系统浏览器打开；
// 其余地址（mailto、片段）不可点。二、三级标题的位置记进 d.anchors，页内目录据此滚动。
func (s *Screen) wikiMarkdown(c ui.C, body string) {
	d := &s.detail
	blocks, slugs := d.md.get(body)
	if d.anchors == nil {
		d.anchors = map[string]gfx.Rect{}
	}
	s.Look.ProseWith(c.Key("prose"), blocks, 0, 0, kit.ProseOpts{
		OnLink:   s.openLink,
		LinkChip: func(href string) bool { return strings.HasPrefix(href, wikiLinkOrigin+HrefPrefix) },
		Anchor: func(b *kit.MdBlock, r gfx.Rect) {
			if slug, ok := slugs[b]; ok {
				d.anchors[slug] = r
			}
		},
	})
}

// openLink 处理正文里的链接点击：站内链接跳页（找不到由 Store 弹提示），http(s) 外链交给系统浏览器。
func (s *Screen) openLink(href string) {
	if rest, ok := strings.CutPrefix(href, wikiLinkOrigin); ok {
		if target, ok := ParseHref(rest); ok {
			s.spawn(func() { s.Store.OpenByLinkTarget(target) })
		}
		return
	}
	lower := strings.ToLower(href)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return
	}
	if s.Hooks.OpenExternal == nil {
		return
	}
	if err := s.Hooks.OpenExternal(href); err != nil {
		s.Guard.Report(locales.LibraryMediaMetaOpenSource(), err)
	}
}
