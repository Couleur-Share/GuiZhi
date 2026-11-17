package shell

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// ModuleView 是一个模块在主窗口里的两块（渲染层 module-chunks 的 MODULE_PANELS 与 MODULE_WORKSPACES）：
// 二级侧栏里的导航内容，和占满右侧的工作区。两者都是整块声明，由模块自己管内部结构。
type ModuleView struct {
	Panel     func(c ui.C)
	Workspace func(c ui.C)
}

// panelBounds 是二级侧栏的宽度范围（渲染层 ui.store.ts 的 SIDEBAR_PANEL_WIDTH_*）。
var panelBounds = settings.PaneWidths[0]

// PanelBounds 返回二级侧栏宽度的缺省与上下限（逻辑像素），供装配代码初始化与夹取已保存的宽度。
func PanelBounds() (def, lo, hi float32) {
	return float32(panelBounds.Default), float32(panelBounds.Min), float32(panelBounds.Max)
}

// panelWidth 是二级侧栏此刻的宽度：夹在范围内（保存的值可能被手改坏）。
func (w *Window) panelWidth() float32 {
	def, lo, hi := PanelBounds()
	if w.PanelResizer.Width == 0 {
		return def
	}
	return min(max(w.PanelResizer.Width, lo), hi)
}

// body 是顶栏下面的区域（渲染层 relative flex min-h-0 flex-1 overflow-hidden）：
// 首页时左边是二级侧栏，右边是工作区；紧凑布局下侧栏展开时浮在工作区上方，后面垫一层遮罩。
func (w *Window) body(c ui.C) {
	home := w.Page == PageHome
	collapsed := w.sidebarCollapsed(c)
	compact := w.compact(c)
	ui.Row(c).Grow(1).FillWidth().MinHeight(0).Clip().Children(func() {
		if home && !compact && !collapsed {
			w.panelAside(c, false)
		}
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Clip().Children(func() { w.content(c) })
		if home && compact && !collapsed {
			w.panelScrim(c)
			w.panelAside(c, true)
		}
	})
}

// panelScrim 是紧凑布局下侧栏浮层后面的遮罩：absolute inset-0 bg-black/20，点它收起；tabIndex=-1，不进 Tab 序列。
func (w *Window) panelScrim(c ui.C) {
	l := w.Look
	c = c.Key("panel-scrim")
	id := ui.ID(c)
	scrim := ui.Row(c).Absolute().Inset(0).
		HitWith(id, locales.CommonCollapse(), gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, Cursor: gfx.PointerCursorDefault}).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: tokens.Color{A: 0.2}}) // bg-black/20
		})
	// 点遮罩或按 Esc 收起浮层（渲染层 SidebarView：Escape 且事件未被别处处理）。
	if scrim.Clicked() || ui.Shortcut(c, 0, "escape") {
		w.CompactSidebarOpen = false
	}
}

// panelAside 是二级侧栏（渲染层 SidebarView layout="panel"）：
// aside（右边 1px 边框，底色与边框、阴影都取 panel-strong 令牌）里是当前模块的面板，
// 面板右缘有一条 8 px 的拖拽把手。overlay 为真时它绝对定位在左边，盖在工作区上（紧凑布局）。
func (w *Window) panelAside(c ui.C, overlay bool) {
	l := w.Look
	t := l.T
	view := w.Modules[w.module()]
	aside := ui.Row(c.Key("panel-aside")).Width(w.panelWidth()).Shrink(0).Clip().PadSides(0, 1, 0, 0)
	if overlay {
		aside = aside.Absolute().Top(0).Left(0).Bottom(0)
	} else {
		aside = aside.FillHeight()
	}
	aside.Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.UiPanelStrongBg, Shadow: t.UiPanelStrongShadow})
		l.PaintEdge(d, kit.EdgeRight, 1, t.UiPanelStrongBorder)
	}).Children(func() {
		// SidebarPanel：relative flex min-w-0 flex-1 flex-col bg-sidebar-background/85
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Sidebar.WithAlpha(0.85)})
		}).Children(func() {
			if view.Panel != nil {
				view.Panel(c)
			}
		})
		// SidebarOverlays：absolute inset-y-0 right-0 z-10 flex，里面一条 ColumnResizer。
		def, lo, hi := PanelBounds()
		ui.Row(c.Key("panel-resizer")).Absolute().Top(0).Right(0).Bottom(0).Children(func() {
			l.ColumnResizer(c, &w.PanelResizer, kit.ColumnResizerSpec{
				Name: locales.SidebarResizeAria(), Min: lo, Max: hi, Default: def,
				OnCommit: func(width float32) {
					if w.OnPanelWidth != nil {
						w.OnPanelWidth(width)
					}
				},
			})
		})
	})
}
