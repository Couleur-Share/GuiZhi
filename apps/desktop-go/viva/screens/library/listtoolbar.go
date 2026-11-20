package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// sortOption 是排序菜单里的一项（渲染层 SORT_OPTIONS）。
type sortOption struct {
	label func() string
	by    string
	order string
}

// sortOptions 是排序菜单的六项，顺序同渲染层；第一项是缺省，当前排序不在表里时按它显示。
func sortOptions() []sortOption {
	return []sortOption{
		{label: locales.LibrarySortUpdatedDesc, by: services.LibrarySortUpdatedAt, order: services.LibraryOrderDesc},
		{label: locales.LibrarySortUpdatedAsc, by: services.LibrarySortUpdatedAt, order: services.LibraryOrderAsc},
		{label: locales.LibrarySortCreatedDesc, by: services.LibrarySortCreatedAt, order: services.LibraryOrderDesc},
		{label: locales.LibrarySortCreatedAsc, by: services.LibrarySortCreatedAt, order: services.LibraryOrderAsc},
		{label: locales.LibrarySortTitleAsc, by: services.LibrarySortTitle, order: services.LibraryOrderAsc},
		{label: locales.LibrarySortTitleDesc, by: services.LibrarySortTitle, order: services.LibraryOrderDesc},
	}
}

// currentSort 返回当前排序对应的那一项；不在表里时取第一项。
func currentSort(list ListView) sortOption {
	options := sortOptions()
	for _, o := range options {
		if o.by == list.SortBy && o.order == list.SortOrder {
			return o
		}
	}
	return options[0]
}

// sortLabel 是排序按钮上的文字：搜索中排序由全文检索的相关度接管，写「相关度」。
func sortLabel(list ListView) string {
	if searchActive(list) {
		return locales.LibrarySortRelevance()
	}
	return currentSort(list).label()
}

// toolbarFocus 是「焦点交给工具条」的交接进度。
//
// 渲染层的工具条是 tabIndex={-1} 的无语义 div：在 Chromium 的可访问树里是被忽略的 generic，平时与可访问性无关。
// Viva 的命中区无论声明不声明角色都会进可访问树（无角色的会被报成无名按钮），所以平时不登记工具条的命中区，
// 只在要接焦点的时候登记。命中区这一帧还不存在，焦点请求要晚一帧才落得下去，因此分两步走（toolbarHit）。
type toolbarFocus uint8

const (
	// toolbarFocusNone：没有交接，工具条不登记命中区。
	toolbarFocusNone toolbarFocus = iota
	// toolbarFocusWanted：已要求把焦点交给工具条，下一帧登记命中区并请求焦点。
	toolbarFocusWanted
	// toolbarFocusHeld：命中区已登记、焦点请求已发出；焦点离开之后撤回命中区。
	toolbarFocusHeld
)

// handOffToToolbar 要求把焦点交给工具条（组合筛选入口因筛选取消而卸载时，免得焦点落到页面起点）。
func (s *Screen) handOffToToolbar(c ui.C) {
	s.pane.toolbarFocus = toolbarFocusWanted
	c.Canvas().RequestFrame() // 命中区要在下一帧登记，别等输入事件
}

// toolbarHit 回答这一帧要不要把工具条登记成命中区，并推进交接进度。
func (s *Screen) toolbarHit(c ui.C) bool {
	p := &s.pane
	canvas := c.Canvas()
	switch p.toolbarFocus {
	case toolbarFocusWanted:
		canvas.RequestFocus(p.toolbarID) // 命中区就在这一帧登记，请求在这一帧末尾生效
		p.toolbarFocus = toolbarFocusHeld
		return true
	case toolbarFocusHeld:
		if canvas.Focused() == p.toolbarID {
			return true
		}
		p.toolbarFocus = toolbarFocusNone // 焦点已经去了别处（点了别的控件，或按 Tab 走了）
	}
	return false
}

// listToolbar 是列表工具条（渲染层 ItemListToolbar）：条目总数、筛选摘要、（回收站里的）清空入口、排序下拉与视图切换。
// flex min-h-9 shrink-0 flex-wrap items-center gap-1 border-b border-border px-3 py-1：放不下时折到下一行。
// 卡片视图与列表视图共用这一条，切换按钮的位置因此保持一致。
func (s *Screen) listToolbar(c ui.C, v View) {
	l := s.Look
	t := l.T
	list := v.List
	// tabIndex={-1}：不在 Tab 顺序里，但能被程序（组合筛选入口卸载后）与点击聚焦；focus:outline-none，没有聚焦环。
	tc := c.Key("toolbar")
	s.pane.toolbarID = ui.ID(tc)
	bar := ui.Flow(tc).FillWidth().Shrink(0).MinHeight(l.Rem(2.25)).Items(ui.Center).
		Gap(l.Space(1)).LineGap(l.Space(1)).
		PadSides(l.Space(1), l.Space(3), l.Space(1)+1, l.Space(3))
	if s.toolbarHit(c) {
		bar = bar.HitWith(s.pane.toolbarID, "", gfx.HitOptions{SkipTab: true})
	}
	bar.Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			// mr-auto text-xs tabular-nums text-muted-foreground：吃掉这一行的剩余宽度，把后面的控件推到行尾。
			l.Text(c.Key("count"), locales.LibraryItemCount(list.Total), kit.TextXS, kit.WeightNormal, t.MutedForeground).Grow(1)
			s.filterSummary(c, v)
			if v.Filters.Scope == services.LibraryScopeTrash && len(list.Entries) > 0 {
				s.emptyTrashButton(c)
			}
			s.sortButton(c, list)
			s.viewToggle(c)
		})
	s.sortMenuView(c, list)
}

// emptyTrashButton 是回收站里的「清空回收站」：inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-xs
// text-destructive transition-colors hover:bg-destructive/10。点击打开确认框，确认后才清空（渲染层 onRequestEmptyTrash）。
func (s *Screen) emptyTrashButton(c ui.C) {
	l := s.Look
	t := l.T
	style := kit.ButtonStyle{
		PadX: l.Space(1.5), PadY: l.Space(1), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.Destructive, BgHover: t.Destructive.WithAlpha(0.1), Transition: kit.DurationDefault,
	}
	b := l.Button(c.Key("empty-trash"), kit.ButtonSpec{Label: locales.LibraryEmptyTrash(), Icon: icons.Trash2, IconSize: l.Rem(0.875)}, style)
	if b.Clicked() {
		s.openConfirm(confirmEmptyTrash, nil)
	}
}

// sortButton 是排序下拉的触发钮：inline-flex shrink-0 items-center gap-0.5 rounded-md px-1.5 py-1 text-xs text-muted-foreground
// transition-colors hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-50；
// 文字最宽 7rem 超出截断，右边是 h-3.5 w-3.5 的箭头，展开时转 180°。搜索中禁用。
func (s *Screen) sortButton(c ui.C, list ListView) {
	l := s.Look
	t := l.T
	p := &s.pane
	searching := searchActive(list)
	label := sortLabel(list)
	c = c.Key("sort")
	id := ui.ID(c)
	// aria-haspopup="menu" 的按钮在浏览器的可访问树里是 combobox（与「创建方式」同理）。
	opts := gfx.HitOptions{Role: gfx.RoleCombobox, Value: label, Disabled: searching, Expandable: true, Expanded: p.sortOpen}
	if !searching {
		opts.Cursor = gfx.PointerCursorHand
	}
	box := ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(0.5)).
		PadSides(l.Space(1), l.Space(1.5), l.Space(1), l.Space(1.5)).
		HitWith(id, locales.LibrarySortBy()+": "+label, opts)
	l.TabStop(box)
	ph := l.HoverProgress(c, id, 0, box.State().Hovered && !searching, kit.DurationDefault)
	bg := t.Accent.WithAlpha(ph)
	fg := kit.MixToken(t.MutedForeground, t.Foreground, ph)
	if searching {
		box = box.Opacity(0.5)
	}
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.75)-2)
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75) - 2})
	}).DrawOver(func(d *ui.DrawContext) { p.sortRect = d.Rect }).Children(func() {
		l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, fg, kit.Ellipsis()).MaxWidth(l.Rem(7)).Shrink(1)
		l.RotatingChevron(c.Key("chevron"), id, l.Rem(0.875), p.sortOpen, fg, kit.DurationDefault)
	})
	if box.Clicked() {
		if p.sortOpen {
			p.sortOpen = false
			return
		}
		// 锚点在点击那一刻取一次：按钮左缘、下缘 + 4（渲染层 getBoundingClientRect），之后布局变了菜单也不跟随。
		p.sortAt = gfx.Point{X: p.sortRect.X, Y: p.sortRect.Y + p.sortRect.Height + 4}
		p.sortOpen = true
	}
}

// sortMenuView 声明排序菜单：当前项前面是 primary 色的对勾，其余项留一个同样宽的空槽，文字对齐。
// 每一帧都要经过 ContextMenu：关着的那一帧它负责复位。
func (s *Screen) sortMenuView(c ui.C, list ListView) {
	l := s.Look
	p := &s.pane
	current := currentSort(list)
	var items []kit.MenuItem
	for _, o := range sortOptions() {
		item := kit.MenuItem{Label: o.label(), OnSelect: func() { s.Store.SetSort(s.Ctx, o.by, o.order) }}
		if o.by == current.by && o.order == current.order {
			item.Icon, item.IconColor = icons.Check, l.T.Primary
		} else {
			item.Blank = true
		}
		items = append(items, item)
	}
	keep := p.sortRect
	if l.ContextMenu(c.Key("sort-menu"), &p.sortMenu, kit.MenuSpec{Open: p.sortOpen, X: p.sortAt.X, Y: p.sortAt.Y, Items: items, Keep: &keep}) {
		p.sortOpen = false
	}
}

// viewOption 是视图切换里的一枚按钮（渲染层 VIEW_OPTIONS）。
type viewOption struct {
	mode, label string
	icon        gfx.Icon
}

func viewOptions() []viewOption {
	return []viewOption{
		{ViewCard, locales.LibraryViewCard(), icons.LayoutGrid},
		{ViewList, locales.LibraryViewList(), icons.List},
	}
}

// viewToggle 是卡片/列表的切换：relative flex shrink-0 items-center overflow-hidden rounded-md border border-border bg-muted/30，
// 里面一块 bg-primary 的滑块（absolute h-full w-1/2 rounded-[3px]，left 在 0% 与 50% 之间过渡 duration-base ease-standard）
// 垫在两枚 p-1.5 的图标按钮下面；选中的图标是 primary-foreground，其余 muted-foreground、悬停 foreground。
func (s *Screen) viewToggle(c ui.C) {
	l := s.Look
	t := l.T
	mode := s.ViewMode()
	c = c.Key("view-toggle")
	id := ui.ID(c)
	radius := l.Rem(0.75) - 2 // rounded-md
	target := float32(0)
	if mode == ViewList {
		target = 1
	}
	motion := l.Motion(kit.DurationBase, kit.EaseStandard)
	ui.Row(c).Shrink(0).Items(ui.Center).Pad(1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.3), Border: t.Border, BorderW: 1, Radius: radius})
			frac := d.Canvas.Animate(id, 0, target, motion)
			paintToggleThumb(d, t.Primary, frac, radius-1)
		}).Children(func() {
		for _, o := range viewOptions() {
			s.viewButton(c, o.mode, o.label, o.icon, mode == o.mode)
		}
	})
}

// viewButton 是切换里的一枚：relative z-10 p-1.5，图标 h-3.5 w-3.5，颜色走 transition-colors duration-base。
func (s *Screen) viewButton(c ui.C, mode, label string, icon gfx.Icon, active bool) {
	l := s.Look
	t := l.T
	c = c.Key(mode)
	id := ui.ID(c)
	box := ui.Row(c).Pad(l.Space(1.5)).Items(ui.Center).Justify(ui.Center).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Selected: active, Hint: label, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	pa := l.HoverProgress(c, id, 0, active, kit.DurationBase)
	ph := l.HoverProgress(c, id, 1, box.State().Hovered && !active, kit.DurationBase)
	fg := kit.MixToken(kit.MixToken(t.MutedForeground, t.Foreground, ph), t.PrimaryForeground, pa)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), 0)
		}
	}).Children(func() {
		l.Icon(c.Key("icon"), icon, l.Rem(0.875), fg)
	})
	if box.Clicked() {
		s.setViewMode(mode)
	}
}

// paintToggleThumb 画切换里的滑块：容器内沿（去掉 1px 边框）的左半或右半，frac 是 0（左）到 1（右）的位置。
// 滑块自己的圆角是 3px，但容器 overflow-hidden 会把贴着容器内圆角的那一侧再裁成内圆角（innerRadius），
// 所以靠外的两个角取内圆角、靠里的两个角取 3px，随位置在两者之间过渡。
func paintToggleThumb(d *ui.DrawContext, color tokens.Color, frac, innerRadius float32) {
	inner := gfx.Rect{X: d.Rect.X + 1, Y: d.Rect.Y + 1, Width: d.Rect.Width - 2, Height: d.Rect.Height - 2}
	half := inner.Width / 2
	thumb := gfx.Rect{X: inner.X + half*frac, Y: inner.Y, Width: half, Height: inner.Height}
	const own = 3 // rounded-[3px]
	left := own + (innerRadius-own)*(1-frac)
	right := own + (innerRadius-own)*frac
	kit.PaintRoundedCorners(d, thumb, color, left, right, right, left)
}
