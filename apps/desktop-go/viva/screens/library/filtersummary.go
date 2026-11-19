package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 组合筛选（渲染层 components/library/LibraryFilterSummary）
//
// 单个分面（合集、标签、平台）由侧栏的高亮表达；两个以上叠加时，工具条上出现「组合筛选 · N」，
// 点开是一块列出各条件的弹层，每条可单独取消，也可一键清空。弹层的几何与交互见 kit.Popover。

// filterEntry 是组合筛选里的一项：kind 是分面名（合集 / 标签 / 平台），name 是选中的值，remove 取消它。
type filterEntry struct {
	kind, name string
	remove     func()
}

// activeFilters 按渲染层的顺序（合集、标签、平台）列出当前叠加的分面。
// 名字取不到时（列表还没读到，或那一项已被删）退回分面名，与渲染层的 `?? t("library.collection")` 一致。
func (s *Screen) activeFilters(v View) []filterEntry {
	f := v.Filters
	var out []filterEntry
	if f.CollectionID != "" {
		name := locales.LibraryCollection()
		for _, col := range v.Collections {
			if col.ID == f.CollectionID {
				name = col.Name
				break
			}
		}
		out = append(out, filterEntry{kind: locales.LibraryCollection(), name: name,
			remove: func() { s.Store.SelectCollection(s.Ctx, f.CollectionID) }}) // 再选一次已选的就是取消
	}
	if f.TagID != "" {
		name := locales.LibraryTags()
		for _, tag := range v.Tags {
			if tag.ID == f.TagID {
				name = tag.Name
				break
			}
		}
		out = append(out, filterEntry{kind: locales.LibraryTags(), name: name,
			remove: func() { s.Store.SelectTag(s.Ctx, f.TagID) }})
	}
	if f.Platform != "" {
		out = append(out, filterEntry{kind: locales.LibraryPlatforms(), name: kit.PlatformLabel(f.Platform),
			remove: func() { s.Store.SelectPlatform(s.Ctx, f.Platform) }})
	}
	return out
}

// filterSummary 是工具条上的入口与它的弹层；只有叠加了两个以上分面才出现，少于两个时弹层一并撤掉
// （渲染层里整个组件卸载，状态随之复位）。
func (s *Screen) filterSummary(c ui.C, v View) {
	p := &s.pane
	filters := s.activeFilters(v)
	p.filter.Group.Begin()
	if len(filters) < 2 {
		p.filter.Close()
		return
	}
	s.filterButton(c, len(filters))
	if !p.filter.Open {
		return
	}
	closed, byKey := s.Look.Popover(c.Key("filters"), &p.filter, kit.PopoverSpec{
		Name:    locales.LibraryActiveFilters(),
		Width:   s.Look.Rem(18),   // w-72
		Margin:  s.Look.Space(2),  // 视口留白 8，也是 max-w-[calc(100vw-16px)] 的两侧
		Pad:     s.Look.Space(3),  // p-3
		Radius:  s.Look.Rem(0.75), // rounded-xl
		Shadows: kit.ShadowXL,     // shadow-xl
		Trigger: p.filterRect,     // 点触发钮只收起
		Content: func() { s.filterPanel(c.Key("filters"), filters) },
	})
	if closed && byKey {
		c.Canvas().RequestFocus(p.filterTrigger) // Esc：buttonRef.current?.focus()
	}
}

// filterButton 是「组合筛选 · N」：inline-flex shrink-0 items-center gap-1 rounded-md bg-primary/10 px-2 py-1 text-xs text-primary
// transition-colors hover:bg-primary/15 focus-visible:ring-2 focus-visible:ring-ring；右边 12px 的箭头，展开时转 180°。
func (s *Screen) filterButton(c ui.C, count int) {
	l := s.Look
	t := l.T
	p := &s.pane
	c = c.Key("filter-button")
	id := ui.ID(c)
	p.filterTrigger = id
	p.filter.Group.Add(kit.PopoverGroup, id)
	radius := l.Rem(0.75) - 2 // rounded-md
	box := ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1)).PadSides(l.Space(1), l.Space(2), l.Space(1), l.Space(2)).
		HitWith(id, locales.LibraryCombinedFilters(count), gfx.HitOptions{
			Role: gfx.RoleButton, Expandable: true, Expanded: p.filter.Open, Cursor: gfx.PointerCursorHand,
		})
	l.TabStop(box)
	ph := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	bg := t.Primary.WithAlpha(0.10 + 0.05*ph) // bg-primary/10 → hover:bg-primary/15
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Ring, radius) // focus-visible:ring-2 focus-visible:ring-ring
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: radius})
	}).DrawOver(func(d *ui.DrawContext) { p.filterRect = d.Rect }).Children(func() {
		l.Text(c.Key("label"), locales.LibraryCombinedFilters(count), kit.TextXS, kit.WeightNormal, t.Primary)
		l.RotatingChevron(c.Key("chevron"), id, l.Rem(0.75), p.filter.Open, t.Primary, kit.DurationDefault)
	})
	if box.Clicked() {
		if p.filter.Open {
			p.filter.Close()
			return
		}
		// 锚点在点击那一刻取一次：触发钮的左缘、下缘 + 6（getBoundingClientRect），之后布局变了面板也不跟随。
		p.filter.Show(gfx.Point{X: p.filterRect.X, Y: p.filterRect.Y + p.filterRect.Height + 6})
	}
}

// filterPanel 是弹层里的内容：页眉（「当前筛选」与「清空筛选」）加各条件的行（divide-y divide-border/60）。
func (s *Screen) filterPanel(c ui.C, filters []filterEntry) {
	l := s.Look
	t := l.T
	p := &s.pane
	group := &p.filter.Group

	// 取消之后弹层关闭、焦点回到触发钮；入口因此卸载（只剩一项）时，焦点交给工具条，免得落到页面起点。
	apply := func(action func(), remaining int) {
		p.filter.Close()
		action()
		if remaining > 1 {
			c.Canvas().RequestFocus(p.filterTrigger)
			return
		}
		s.handOffToToolbar(c)
	}

	// mb-2 flex items-center justify-between gap-3
	ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).MarginBottom(l.Space(2)).Children(func() {
		l.Text(c.Key("title"), locales.LibraryActiveFilters(), kit.TextXS, kit.WeightMedium, t.PopoverForeground)
		// rounded px-1 py-0.5 text-xs text-muted-foreground hover:bg-accent hover:text-foreground（没有 transition）
		clear := l.Button(c.Key("clear"), kit.ButtonSpec{Label: locales.LibraryClearFilters(), Groups: group, Group: kit.PopoverGroup, Autofocus: true}, kit.ButtonStyle{
			PadX: l.Space(1), PadY: l.Space(0.5), Radius: l.Rem(0.25), Text: kit.TextXS, Weight: kit.WeightNormal,
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent, Transition: kit.DurationNone,
		})
		if clear.Clicked() {
			apply(func() { s.Store.ClearFacetFilters(s.Ctx) }, 0)
		}
	})

	// divide-y divide-border/60：第一行以外，每行顶上一条 1px 的分隔线（计入行高）。
	ui.Column(c.Key("rows")).FillWidth().Children(func() {
		for i, f := range filters {
			s.filterRow(c.Key("row-"+f.kind), f, i > 0, func() { apply(f.remove, len(filters)-1) })
		}
	})
}

// filterRow 是一个条件：flex items-start gap-3 py-2.5；分面名（w-12 pt-1 text-xs 灰）、值（flex-1 break-words pt-0.5 text-sm）、
// 取消钮（p-1 rounded-md，14px 的 X）。
func (s *Screen) filterRow(c ui.C, f filterEntry, divider bool, onRemove func()) {
	l := s.Look
	t := l.T
	topPad := l.Space(2.5)
	if divider {
		topPad++ // border-top 1px
	}
	ui.Row(c).FillWidth().Items(ui.Start).Gap(l.Space(3)).PadSides(topPad, 0, l.Space(2.5), 0).
		Draw(func(d *ui.DrawContext) {
			if divider {
				l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6))
			}
		}).Children(func() {
		ui.Column(c.Key("kind")).Width(l.Rem(3)).Shrink(0).PadSides(l.Space(1), 0, 0, 0).Children(func() {
			l.Text(c.Key("kind-text"), f.kind, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
		ui.Column(c.Key("name")).Grow(1).MinWidth(0).PadSides(l.Space(0.5), 0, 0, 0).Children(func() {
			l.Text(c.Key("name-text"), f.name, kit.TextSM, kit.WeightNormal, t.PopoverForeground, kit.Wrapped())
		})
		remove := l.Button(c.Key("remove"), kit.ButtonSpec{
			Name: locales.LibraryRemoveFilter(f.kind, f.name), Icon: icons.X, IconSize: l.Rem(0.875),
			Groups: &s.pane.filter.Group, Group: kit.PopoverGroup,
		}, kit.ButtonStyle{
			PadX: l.Space(1), PadY: l.Space(1), Radius: l.Rem(0.75) - 2,
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent, Transition: kit.DurationDefault,
		})
		if remove.Clicked() {
			onRemove()
		}
	})
}
