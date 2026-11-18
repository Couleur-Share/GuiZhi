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

// scopeTab 是范围分段控件的一格。
type scopeTab struct {
	scope string
	label string
	icon  gfx.Icon
	count int
}

// scopeTabs 是范围分段控件（渲染层 LibraryScopeTabs）：未归合集 / 全部 / 收藏 / 归档。
// 范围是与集合、平台、标签正交的第一层筛选，因此叠加分面时仍显示它。外层 pt-3，四格等宽（grid grid-cols-4 gap-1）、
// 圆角底板 rounded-lg bg-sidebar-accent/40 p-1。
func (s *Screen) scopeTabs(c ui.C, v View) {
	l := s.Look
	t := l.T
	n := func(pick func(*services.LibraryCounts) int) int {
		if v.Counts == nil {
			return 0
		}
		return pick(v.Counts)
	}
	tabs := []scopeTab{
		{services.LibraryScopeUncategorized, locales.LibraryScopeUncategorized(), icons.Inbox, n(func(c *services.LibraryCounts) int { return c.Uncategorized })},
		{services.LibraryScopeAll, locales.LibraryScopeAllShort(), icons.Layers, n(func(c *services.LibraryCounts) int { return c.All })},
		{services.LibraryScopeFavorites, locales.LibraryScopeFavorites(), icons.Star, n(func(c *services.LibraryCounts) int { return c.Favorites })},
		{services.LibraryScopeArchived, locales.LibraryScopeArchived(), icons.Archive, n(func(c *services.LibraryCounts) int { return c.Archived })},
	}
	ui.Column(c.Key("tabs-wrap")).FillWidth().PadTop(l.Space(3)).Children(func() {
		ui.Row(c.Key("tabs")).FillWidth().Gap(l.Space(1)).Pad(l.Space(1)).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.SidebarAccent.WithAlpha(0.4), Radius: l.Rem(0.75)})
			}).Children(func() {
			for _, tab := range tabs {
				s.scopeTab(c, tab, v.Filters.Scope == tab.scope)
			}
		})
	})
}

// scopeTab 是一格：relative flex flex-col items-center justify-center rounded-md py-2 transition-all duration-base；
// 选中 bg-primary/15 text-foreground shadow-sm，否则 text-sidebar-foreground/80，悬停 bg-sidebar-accent text-sidebar-foreground。
// 图标 mb-1 h-4 w-4，名称 max-w-full truncate px-0.5 text-xs font-medium leading-none，右上角 absolute 的计数徽标。
func (s *Screen) scopeTab(c ui.C, tab scopeTab, active bool) {
	l := s.Look
	t := l.T
	c = c.Key("tab-" + tab.scope)
	id := ui.ID(c)
	name := tab.label
	if tab.count > 0 {
		name += " " + countLabel(tab.count) // 徽标不是 aria-hidden，名称取自全部内容
	}
	radius := l.Rem(0.75) - 2 // rounded-md
	box := ui.Column(c).Grow(1).Basis(0).MinWidth(0).Items(ui.Center).Justify(ui.Center).
		PadSides(l.Space(2), 0, l.Space(2), 0).
		HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Selected: active, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)

	ph := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationBase)
	pa := l.HoverProgress(c, id, 1, active, kit.DurationBase)
	bg := kit.MixToken(kit.MixToken(tokens.Transparent, t.SidebarAccent, ph), t.Primary.WithAlpha(0.15), pa)
	fg := kit.MixToken(kit.MixToken(t.SidebarForeground.WithAlpha(0.8), t.SidebarForeground, ph), t.Foreground, pa)
	shadows := kit.MixShadows(nil, kit.ShadowSM, pa)

	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), radius) // 全局 button:focus-visible
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: radius, Shadows: shadows})
	}).Children(func() {
		l.Icon(c.Key("icon"), tab.icon, l.Rem(1), fg).MarginBottom(l.Space(1))
		l.TextPx(c.Key("label"), tab.label, l.Rem(0.75), l.Rem(0.75), kit.WeightMedium, fg, kit.Ellipsis(), kit.Centered()).
			FillWidth().PadSides(0, 2, 0, 2)
		if tab.count > 0 {
			s.countBadge(c.Key("badge"), tab.count, active)
		}
	})
	if box.Clicked() {
		s.Store.SetScope(s.Ctx, tab.scope)
	}
}

// countBadge 是页签右上角的计数徽标：absolute right-1 top-1 min-w-[15px] rounded-full px-1 text-center text-xs font-medium leading-[15px]；
// 选中 bg-primary/20 text-foreground，否则 bg-foreground/10 text-foreground/80。
func (s *Screen) countBadge(c ui.C, count int, active bool) {
	l := s.Look
	t := l.T
	bg, fg := t.Foreground.WithAlpha(0.1), t.Foreground.WithAlpha(0.8)
	if active {
		bg, fg = t.Primary.WithAlpha(0.2), t.Foreground
	}
	const height = 15 // leading-[15px]：px 字面量
	ui.Row(c).Absolute().Top(l.Space(1)).Right(l.Space(1)).MinWidth(height).Height(height).
		Items(ui.Center).Justify(ui.Center).PadSides(0, l.Space(1), 0, l.Space(1)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: 9999}) }).
		Children(func() {
			l.TextPx(c.Key("n"), countLabel(count), l.Rem(0.75), height, kit.WeightMedium, fg)
		})
}
