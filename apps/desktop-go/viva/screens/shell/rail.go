package shell

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// Module 是 rail 上的一个模块入口（渲染层 AppModule）。
type Module string

const (
	ModuleLibrary Module = "library"
	ModuleInbox   Module = "inbox"
	ModuleAsk     Module = "ask"
	ModuleWiki    Module = "wiki"
	ModuleImports Module = "imports"
)

// railItem 是 rail 上的一枚模块按钮。
type railItem struct {
	module Module
	label  func() string
	icon   gfx.Icon
}

// railGroups 是 rail 的分组（渲染层 RAIL_GROUPS）：资产流转 / 智能探索 / 任务队列，组间一条细线。
var railGroups = [][]railItem{
	{{ModuleLibrary, locales.NavLibrary, icons.LibraryBig}, {ModuleInbox, locales.NavInbox, icons.Inbox}},
	{{ModuleAsk, locales.NavAsk, icons.MessagesSquare}, {ModuleWiki, locales.NavWiki, icons.Network}},
	{{ModuleImports, locales.NavImports, icons.Download}},
}

// rail 是左侧模块栏（渲染层 SidebarRail，layout="rail"）。
//
// 结构：aside（w-20，右边 1px 边框，bg-sidebar-accent/25）里套一层同宽同色的 rail 容器。
// 容器与 aside 的内容盒同为 w-20，aside 的 1px 边框占掉最右一列，所以容器被裁掉最右一列：
// 底色在这一列只叠一层，其余各列叠两层（半透明色叠两次）。
func (w *Window) rail(c ui.C) {
	l := w.Look
	t := l.T
	tint := t.SidebarAccent.WithAlpha(0.25)
	ui.Column(c).Width(l.Rem(5)).Shrink(0).FillHeight().
		PadSides(l.Space(3), l.Space(2), l.Space(3), l.Space(2)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: tint})
			inner := *d
			inner.Rect.Width--
			l.PaintSurface(&inner, kit.Surface{Bg: tint})
		}).
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeRight, 1, t.SidebarBorder.WithAlpha(0.6)) }).
		Children(func() {
			ui.Column(c).FillWidth().Grow(1).Gap(l.Space(1)).Children(func() {
				for i, group := range railGroups {
					c := c.Key("group" + strconv.Itoa(i))
					ui.Column(c).FillWidth().Gap(l.Space(2)).Children(func() {
						if i > 0 {
							// mx-3 my-1 h-px bg-sidebar-border/70
							ui.Row(c).FillWidth().Height(1).MarginSides(l.Space(1), l.Space(3), l.Space(1), l.Space(3)).
								Draw(func(d *ui.DrawContext) {
									l.PaintSurface(d, kit.Surface{Bg: t.SidebarBorder.WithAlpha(0.7)})
								})
						}
						for _, it := range group {
							w.railButton(c.Key(string(it.module)), it)
						}
					})
				}
			})
			w.railSettings(c)
		})
}

// railButton 画一枚模块按钮：图标盒（h-9 w-9）在上、标签（text-xs leading-none）在下，整体 rounded-2xl。
// 选中：bg-primary/15 text-foreground shadow-sm，图标盒 bg-primary/10；其余 text-sidebar-foreground/80，悬停 bg-sidebar-accent/50。
func (w *Window) railButton(c ui.C, it railItem) {
	l := w.Look
	t := l.T
	label := it.label()
	active := w.Page != PageSettings && w.module() == it.module
	id := ui.ID(c)
	box := ui.Column(c).FillWidth().Items(ui.Center).Justify(ui.Center).Gap(l.Space(1.5)).
		PadSides(l.Space(3), l.Space(2), l.Space(3), l.Space(2)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton})
	hovered := box.State().Hovered
	fg := t.SidebarForeground.WithAlpha(0.8)
	surface := kit.Surface{Radius: l.Rem(1)}
	switch {
	case active:
		fg = t.Foreground
		surface.Bg, surface.Shadows = t.Primary.WithAlpha(0.15), kit.ShadowSM
	case hovered:
		fg = t.SidebarForeground
		surface.Bg = t.SidebarAccent.WithAlpha(0.5)
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		ui.Stack(c).Size(l.Rem(2.25), l.Rem(2.25)).Shrink(0).Children(func() {
			ui.Row(c).Fill().Items(ui.Center).Justify(ui.Center).
				Draw(func(d *ui.DrawContext) {
					if active {
						l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Radius: l.Rem(1)})
					}
				}).
				Children(func() { l.Icon(c, it.icon, l.Rem(1.25), fg).Shrink(0) })
			w.railBadge(c, it.module)
		})
		l.Text(c, label, kit.TextStyle{Size: 0.75, Line: 0.75}, kit.WeightMedium, fg, kit.Centered())
	})
	if box.Clicked() && w.OnModule != nil {
		w.OnModule(it.module)
	}
}

// badgeRingColor 是角标的描边色。角标的类名是 ring-2 ring-sidebar-background，但 tailwind.config.js 的 sidebar 色
// 没有 background 这一项，ring-sidebar-background 不生成样式，ring-2 用 Tailwind 的缺省环色 rgb(59 130 246 / 0.5)
// （渲染层事实：角标的 box-shadow 是 rgba(59, 130, 246, 0.5) 0px 0px 0px 2px）。此前这里画成了 Sidebar 色。
var badgeRingColor = tokens.RGBA(59, 130, 246, 0.5)

// railBadge 画图标盒右上角的数字角标（处理中心的待处理数、采集的进行中数；ring-2 描边，缺省环色，见 badgeRingColor）。
//
// 渲染层另有一个采集忙碌时的琥珀色呼吸圆点，但它只在「没有角标」时出现，而采集的忙碌条件（进行中数 > 0）
// 恰好就是它有角标的条件，所以那个圆点在现行源码里不会被渲染，这里不画（盘点见台账「渲染层不可达分支」）。
func (w *Window) railBadge(c ui.C, m Module) {
	l := w.Look
	t := l.T
	count := 0
	switch m {
	case ModuleInbox:
		count = w.InboxTotal
	case ModuleImports:
		count = w.ActiveImports
	}
	switch {
	case count > 0:
		text := strconv.Itoa(count)
		if count > 99 {
			text = "99+"
		}
		// -right-1 -top-1：h-4 min-w-4 rounded-full bg-foreground px-1 text-xs font-semibold leading-none text-background。
		ui.Row(c).Absolute().Top(-l.Space(1)).Right(-l.Space(1)).Height(l.Rem(1)).MinWidth(l.Rem(1)).
			Items(ui.Center).Justify(ui.Center).PadX(l.Space(1)).
			Draw(func(d *ui.DrawContext) {
				// box-shadow 列表是「ring-2、shadow-sm」：环在上、shadow-sm 在下，背景盖在两者之内；
				// 环只出现在盒子外侧，所以用空心环（PaintRingBand），不会透出环色。
				l.PaintSurface(d, kit.Surface{Radius: l.Rem(1), Shadows: kit.ShadowSM})
				l.PaintRingBand(d, 2, badgeRingColor, l.Rem(1))
				l.PaintSurface(d, kit.Surface{Bg: t.Foreground, Radius: l.Rem(1)})
			}).
			Children(func() {
				l.Text(c, text, kit.TextStyle{Size: 0.75, Line: 0.75}, kit.WeightSemibold, t.Background)
			})
	}
}

// railSettings 是底部的设置齿轮（mt-auto pt-4）：h-11 w-11 rounded-2xl，居中；
// 在设置页时高亮（bg-primary/15 text-foreground shadow-sm），否则 text-sidebar-foreground/60，悬停 bg-sidebar-accent/50。
func (w *Window) railSettings(c ui.C) {
	l := w.Look
	t := l.T
	label := locales.HeaderSettings()
	active := w.Page == PageSettings
	c = c.Key("settings")
	ui.Row(c).FillWidth().PadTop(l.Space(4)).Justify(ui.Center).Items(ui.Center).Children(func() {
		id := ui.ID(c)
		box := ui.Row(c).Size(l.Rem(2.75), l.Rem(2.75)).Shrink(0).Items(ui.Center).Justify(ui.Center).
			HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton})
		hovered := box.State().Hovered
		fg := t.SidebarForeground.WithAlpha(0.6)
		surface := kit.Surface{Radius: l.Rem(1)}
		switch {
		case active:
			fg = t.Foreground
			surface.Bg, surface.Shadows = t.Primary.WithAlpha(0.15), kit.ShadowSM
		case hovered:
			fg = t.SidebarForeground
			surface.Bg = t.SidebarAccent.WithAlpha(0.5)
		}
		box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
			l.Icon(c, icons.Settings, l.Rem(1.25), fg).Shrink(0)
		})
		if box.Clicked() && w.OnSettings != nil {
			w.OnSettings()
		}
	})
}
