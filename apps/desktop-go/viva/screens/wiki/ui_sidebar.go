package wiki

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// sidebarGroup 是侧栏里的一组筛选（SidebarWikiPanel 的 groups）。
type sidebarGroup struct {
	key   string
	title string
	rows  []CatalogFilter
}

// sidebarGroups 按渲染层的顺序给出三组：浏览、按类型、需要留意。
func sidebarGroups() []sidebarGroup {
	return []sidebarGroup{
		{"browse", locales.WikiPanelBrowse(), []CatalogFilter{FilterAll}},
		{"kind", locales.WikiPanelByKind(), []CatalogFilter{FilterTopic, FilterEntity, FilterConcept}},
		{"attention", locales.WikiPanelNeedsAttention(), []CatalogFilter{FilterOrphan, FilterManual}},
	}
}

// filterLabel 是筛选行的名称。
func filterLabel(f CatalogFilter) string {
	switch f {
	case FilterTopic:
		return locales.WikiKindTopic()
	case FilterEntity:
		return locales.WikiKindEntity()
	case FilterConcept:
		return locales.WikiKindConcept()
	case FilterOrphan:
		return locales.WikiPanelOrphan()
	case FilterManual:
		return locales.WikiPanelManual()
	}
	return locales.WikiPanelAll()
}

// Panel 是二级侧栏里的内容（渲染层 SidebarWikiPanel）：目录的筛选轴，图谱视图下换成一块「按节点浏览」的提示。
//
// 外层是 flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3 pt-3：内容溢出时在这里滚动；孤立页说明靠 mt-auto 贴在底部，
// 所以内容列至少撑满视口，说明前放一个撑开的占位。
func (s *Screen) Panel(c ui.C) {
	l := s.Look
	s.enter()
	v := s.Store.View()
	counts := v.Counts()
	l.ScrollAreaView(c.Key("panel-scroll"), [4]float32{l.Space(3), l.Space(2), l.Space(3), l.Space(2)}, func(view kit.ScrollView) {
		ui.Column(c.Key("panel-col")).FillWidth().MinHeight(view.Viewport).Children(func() {
			if v.Mode == ViewGraph {
				s.graphHint(c)
				return
			}
			for i, g := range sidebarGroups() {
				s.sidebarGroup(c, g, i == 0, counts, v.Filter)
			}
			if counts.Orphan > 0 {
				ui.Row(c.Key("spacer")).Grow(1)
				s.orphanHint(c)
			}
		})
	}).Grow(1).FillWidth().MinHeight(0)
}

// sidebarGroup 是一组：标题行（文字加一条淡线）与筛选行。pt-2 first:pt-0。
func (s *Screen) sidebarGroup(c ui.C, g sidebarGroup, first bool, counts CatalogCounts, active CatalogFilter) {
	l := s.Look
	t := l.T
	top := l.Space(2)
	if first {
		top = 0
	}
	gc := c.Key("group-" + g.key)
	ui.Column(gc).FillWidth().PadSides(top, 0, 0, 0).Children(func() {
		// flex items-center gap-2 px-3 pb-1：text-xs font-medium uppercase tracking-wide text-sidebar-foreground/80，后面一条 h-px 淡线。
		ui.Row(gc.Key("title")).FillWidth().Items(ui.Center).Gap(l.Space(2)).PadSides(0, l.Space(3), l.Space(1), l.Space(3)).Children(func() {
			l.TextTracked(gc.Key("label"), strings.ToUpper(g.title), kit.TextXS, kit.WeightMedium, t.SidebarForeground.WithAlpha(0.8), 0.025).Shrink(0)
			ui.Row(gc.Key("rule")).Grow(1).Height(1).Draw(func(d *ui.DrawContext) {
				d.Canvas.Fill(d.Rect, d.Fade(l.Col(t.SidebarBorder.WithAlpha(0.6))))
			})
		})
		ui.Column(gc.Key("rows")).FillWidth().Gap(l.Space(0.5)).Children(func() {
			for _, f := range g.rows {
				s.filterRow(gc, f, counts.For(f), active == f)
			}
		})
	})
}

// filterRow 是一行筛选：图标、名称与计数徽标。整行是切换钮（aria-pressed）。
//
// flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors；选中 bg-primary/15 text-foreground，
// 否则 text-sidebar-foreground/80，悬停 bg-sidebar-accent/50 text-sidebar-foreground。选中时名称 font-medium。
func (s *Screen) filterRow(c ui.C, f CatalogFilter, count int, active bool) {
	l := s.Look
	t := l.T
	c = c.Key("filter-" + string(f))
	label := filterLabel(f)
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(1.5), l.Space(3), l.Space(1.5), l.Space(3))
	box, ph := s.hitButton(c, id, box, label+" "+strconv.Itoa(count), "", active)
	pa := l.HoverProgress(c, id, 1, active, kit.DurationDefault)
	bg := kit.MixToken(kit.MixToken(tokens.Transparent, t.SidebarAccent.WithAlpha(0.5), ph), t.Primary.WithAlpha(0.15), pa)
	fg := kit.MixToken(kit.MixToken(t.SidebarForeground.WithAlpha(0.8), t.SidebarForeground, ph), t.Foreground, pa)
	weight := float32(kit.WeightNormal)
	if active {
		weight = kit.WeightMedium
	}
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), s.radiusLG())
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: s.radiusLG()})
	}).Children(func() {
		l.Icon(c.Key("icon"), kindIcon(f), l.Rem(1), fg).Shrink(0)
		l.Text(c.Key("label"), label, kit.TextSM, weight, fg, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
		s.countChip(c, count, active, fg)
	})
	if box.Clicked() {
		s.Store.SetFilter(f)
	}
}

// countChip 是计数徽标：rounded-full border px-1.5 py-0.5 text-xs tabular-nums；
// 选中 border-primary/20 bg-primary/10 text-foreground，否则 border-sidebar-border bg-sidebar-accent/80 文字取行的颜色。
func (s *Screen) countChip(c ui.C, count int, active bool, fg tokens.Color) {
	l := s.Look
	t := l.T
	surface := kit.Surface{Border: t.SidebarBorder, Bg: t.SidebarAccent.WithAlpha(0.8), BorderW: 1, Radius: 9999}
	if active {
		surface.Border, surface.Bg = t.Primary.WithAlpha(0.2), t.Primary.WithAlpha(0.1)
	}
	ui.Row(c.Key("count")).Shrink(0).Items(ui.Center).
		PadSides(l.Space(0.5)+1, l.Space(1.5)+1, l.Space(0.5)+1, l.Space(1.5)+1).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		l.Text(c, strconv.Itoa(count), kit.TextXS, kit.WeightNormal, fg)
	})
}

// graphHint 是图谱视图下侧栏里的提示块：rounded-lg border border-dashed border-sidebar-border px-3 py-3 text-center，
// 一句说明加一个回到目录视图的链接。
func (s *Screen) graphHint(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("graph-hint")).FillWidth().Items(ui.Center).PadSides(l.Space(3)+1, l.Space(3)+1, l.Space(3)+1, l.Space(3)+1).
		Draw(func(d *ui.DrawContext) { l.PaintDashedBorder(d, t.SidebarBorder, 1, s.radiusLG()) }).
		Children(func() {
			l.Text(c.Key("text"), locales.WikiPanelGraphHint(), kit.TextXS, kit.WeightNormal, t.SidebarForeground.WithAlpha(0.6), kit.Centered(), kit.Wrapped())
			ui.Row(c.Key("back-wrap")).MarginTop(l.Space(2)).Children(func() {
				if s.textLink(c.Key("back"), locales.WikiViewCatalog(), kit.TextXS, false, 0) {
					s.spawn(func() { s.Store.SetViewMode(ViewCatalog) })
				}
			})
		})
}

// orphanHint 是侧栏底部的孤立页说明：mt-auto px-3 pt-4 text-xs leading-relaxed text-sidebar-foreground/75。
func (s *Screen) orphanHint(c ui.C) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("orphan-hint")).FillWidth().PadSides(l.Space(4), l.Space(3), 0, l.Space(3)).Children(func() {
		l.TextPx(c.Key("text"), locales.WikiPanelOrphanHint(), l.Rem(0.75), l.Rem(0.75)*1.625, kit.WeightNormal,
			t.SidebarForeground.WithAlpha(0.75), kit.Wrapped()).Shrink(1).MinWidth(0)
	})
}
