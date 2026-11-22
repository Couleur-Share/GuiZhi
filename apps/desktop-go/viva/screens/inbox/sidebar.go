package inbox

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// filterRowSpec 是侧栏筛选组里的一行（渲染层 SidebarInboxPanel 的 ROWS）。
type filterRowSpec struct {
	filter Filter
	label  string
	icon   gfx.Icon
}

// filterRows 按渲染层的顺序给出六档筛选：「未归合集」是可选整理，「语义索引」「Wiki 编译」是后台任务，各自在界面上另起一组。
func filterRows() []filterRowSpec {
	return []filterRowSpec{
		{FilterAll, locales.InboxFilterAll(), icons.Inbox},
		{FilterReviewRequired, locales.InboxFilterReviewRequired(), icons.CircleHelp},
		{FilterImportIssue, locales.InboxFilterImportIssue(), icons.TriangleAlert},
		{FilterUnclassified, locales.InboxFilterUnclassified(), icons.Boxes},
		{FilterSemanticPending, locales.InboxFilterSemanticPending(), icons.ScanSearch},
		{FilterWikiPending, locales.InboxFilterWikiPending(), icons.Network},
	}
}

// Panel 是二级侧栏里的内容（渲染层 SidebarInboxPanel）：分组标题与六档筛选。
//
// 外层是 flex h-full min-h-0 flex-col overflow-y-auto px-2 py-3：内容溢出时在这里滚动。
// 筛选行与分组说明同属一个 space-y-0.5 的列：Tailwind 3 的 space-y 选择器（三个类的特异性）压过分组说明自带的 mt-4，
// 所以说明与相邻两行之间都只隔 2px，而不是 1rem——这里照 CSS 的实际结果排。
func (s *Screen) Panel(c ui.C) {
	l := s.Look
	t := l.T
	s.enter()
	v := s.Store.View()
	l.ScrollArea(c.Key("panel-scroll"), [4]float32{l.Space(3), l.Space(2), l.Space(3), l.Space(2)}, func() {
		// px-3 pb-2 text-xs font-medium uppercase tracking-wide text-sidebar-foreground/80；uppercase：Latin 字母大写，中文不受影响。
		ui.Row(c.Key("heading")).FillWidth().PadSides(0, l.Space(3), l.Space(2), l.Space(3)).Children(func() {
			l.TextTracked(c.Key("label"), strings.ToUpper(locales.NavInbox()), kit.TextXS, kit.WeightMedium,
				t.SidebarForeground.WithAlpha(0.8), 0.025)
		})
		ui.Column(c.Key("filters")).FillWidth().Gap(l.Space(0.5)).Children(func() {
			for _, row := range filterRows() {
				switch row.filter {
				case FilterUnclassified:
					s.sectionNote(c, "optional", locales.InboxOptionalSection())
				case FilterSemanticPending:
					s.sectionNote(c, "background", locales.InboxBackgroundSection())
				}
				s.filterRow(c, row, v.CountOf(row.filter), v.Filter == row.filter)
			}
		})
	}).Grow(1).FillWidth().MinHeight(0)
}

// sectionNote 是分组之间的说明：mx-3 border-t border-sidebar-border pb-2 pt-3 text-xs text-muted-foreground。
// 左右外边距用外层的内边距表达，顶部 1px 边框占内边距的一部分（border-box）。
func (s *Screen) sectionNote(c ui.C, key, label string) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("note-"+key)).FillWidth().PadSides(0, l.Space(3), 0, l.Space(3)).Children(func() {
		ui.Row(c.Key("rule")).FillWidth().PadSides(l.Space(3)+1, 0, l.Space(2), 0).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.SidebarBorder) }).
			Children(func() {
				l.Text(c.Key("text"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).Shrink(1).MinWidth(0)
			})
	})
}

// filterRow 是一行筛选：图标、名称与计数徽标。整行是切换钮（aria-pressed）。
//
// flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors；
// 选中 bg-primary/15 text-foreground，否则 text-sidebar-foreground/80，悬停 bg-sidebar-accent/50 text-sidebar-foreground。
func (s *Screen) filterRow(c ui.C, row filterRowSpec, count int, active bool) {
	l := s.Look
	t := l.T
	c = c.Key("filter-" + string(row.filter))
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(1.5), l.Space(3), l.Space(1.5), l.Space(3)).
		HitWith(id, row.label+" "+strconv.Itoa(count), gfx.HitOptions{Role: gfx.RoleButton, Selected: active, Cursor: gfx.PointerCursorHand})
	l.TabStop(box) // 侧栏滚动区里的 Tab 停靠点：滚出视野时由 kit 滚进来（台账 B-030）

	// 选中与悬停各用一个进度量：悬停中点选时底色从 accent/50 补间到 primary/15，与 CSS 的过渡一致。
	ph := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	pa := l.HoverProgress(c, id, 1, active, kit.DurationDefault)
	bg := kit.MixToken(kit.MixToken(tokens.Transparent, t.SidebarAccent.WithAlpha(0.5), ph), t.Primary.WithAlpha(0.15), pa)
	fg := kit.MixToken(kit.MixToken(t.SidebarForeground.WithAlpha(0.8), t.SidebarForeground, ph), t.Foreground, pa)

	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.75))
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75)})
	}).Children(func() {
		l.Icon(c.Key("icon"), row.icon, l.Rem(1), fg)
		// 名称：min-w-0 flex-1 truncate text-left
		l.Text(c.Key("label"), row.label, kit.TextSM, kit.WeightNormal, fg, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
		s.countChip(c, count, fg)
	})
	if box.Clicked() {
		s.Store.SetFilter(row.filter)
	}
}

// countChip 是计数徽标：rounded-full border border-sidebar-border px-1.5 py-0.5 text-xs tabular-nums；文字取行的当前文字色。
func (s *Screen) countChip(c ui.C, count int, fg tokens.Color) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("count")).Shrink(0).Items(ui.Center).
		PadSides(l.Space(0.5)+1, l.Space(1.5)+1, l.Space(0.5)+1, l.Space(1.5)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.SidebarBorder, BorderW: 1, Radius: 9999})
		}).Children(func() {
		l.Text(c, strconv.Itoa(count), kit.TextXS, kit.WeightNormal, fg)
	})
}
