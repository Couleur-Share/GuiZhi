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
	// HidePanel 非空且返回真时，这一帧整块二级侧栏（含紧凑布局下的浮层与遮罩）不挂载：知识库的专注阅读（渲染层 App.tsx 的 !(isFocusReadingMode && appModule === "library")）。
	HidePanel func() bool
	// OccludePanelShadow 非空时，每帧在补画二级侧栏右缘外阴影（paintPanelEdgeShadow）时询问：工作区里有没有层级比侧栏高的不透明横条
	// 盖住了阴影所在的那一列（知识库表格横向滚动后吸附的表头 thead，z-30 高于侧栏的 z-20，台账 B-048）。
	// 返回画布绝对逻辑坐标下被盖住的纵向区间 [top, bottom]；ok 为假或 bottom <= top 表示没有。
	// 只在整条阴影都落在这条横条之下时才报告（横条的左缘到不了侧栏右缘就不算盖住）。
	// 调用时机是整行的 DrawOver：Viva 先画完子元素的 Draw 再跑父级的 DrawOver（viva@v0.6.4 ui/paint.go 的 132–143 行），
	// 所以工作区在本帧 Draw 里记下的几何，这里读到的就是同一帧的。
	OccludePanelShadow func() (top, bottom float32, ok bool)
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
	view := w.Modules[w.module()]
	home := w.Page == PageHome && !(view.HidePanel != nil && view.HidePanel()) // 专注阅读：不挂载侧栏，其余照首页的布局排
	collapsed := w.sidebarCollapsed(c)
	compact := w.compact(c)
	inline := home && !compact && !collapsed // 侧栏在行内（不是紧凑布局下的浮层）
	ui.Row(c).Grow(1).FillWidth().MinHeight(0).Clip().DrawOver(func(d *ui.DrawContext) {
		if inline {
			w.paintPanelEdgeShadow(d)
		}
	}).Children(func() {
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

// paintPanelEdgeShadow 把二级侧栏的外阴影补画到它右边的工作区上（台账 B-046 (7)(c)）。
//
// 渲染层的 aside 是 z-20 的定位元素，box-shadow 投在比它先画的工作区之上：浅色主题（0 1px 2px 0 rgba(0,0,0,.04)）
// 在右缘外两列像素里是 3、1 个灰阶，沿整个窗格的高度（像素实测：各档字号的 table、table-scrolled 在侧栏右缘的两列都比 Viva 暗）。
// Viva 的侧栏画在工作区之前、又自带裁剪（Clip），阴影要么被工作区盖住、要么被自己的裁剪截掉，所以放在整行的 DrawOver 里补画：
// 投影盒子按侧栏的矩形算（盒子自己的部分挖掉），只留落在右缘外那一条里的部分，宽度取阴影的三倍标准差加水平偏移。
//
// 渲染层里吸附的表头（z-30）盖在这条阴影之上：表格横向滚动、表头压住这几列像素时（table-scrolled 的表头一段），
// 表头不透明的底色把阴影盖掉。Viva 的补画在整行的 DrawOver 里、晚于一切子元素，所以靠 ModuleView.OccludePanelShadow
// 问工作区要被盖住的纵向区间，把目标条切成上下两段（台账 B-048）：投影盒子仍是整个侧栏，只收窄裁剪条，高斯投影在切口处严丝合缝。
//
// 残余（台账 B-048）：表头左缘落在阴影条里面、还没越过侧栏右缘时（横向滚动量落在一个 reach 宽的窗口内），表头只盖住条的一部分，
// 工作区一侧不报告，这一段仍按整条补画——与此前的行为相同，只在这个窗口里多出 1–5 个灰阶。
func (w *Window) paintPanelEdgeShadow(d *ui.DrawContext) {
	l := w.Look
	sh := l.T.UiPanelStrongShadow
	reach := sh.Blur*1.5 + max(sh.X, -sh.X) + 1
	aside := gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: w.panelWidth(), Height: d.Rect.Height}
	strip := gfx.Rect{X: aside.X + aside.Width, Y: aside.Y, Width: reach, Height: aside.Height}
	var top, bottom float32
	var occluded bool
	if view := w.Modules[w.module()]; view.OccludePanelShadow != nil {
		top, bottom, occluded = view.OccludePanelShadow()
	}
	for _, part := range shadowStripParts(strip, top, bottom, occluded) {
		l.PaintCastShadow(d, aside, part, 0, sh)
	}
}

// shadowStripParts 把阴影目标条按被盖住的纵向区间 [top, bottom] 切成上下最多两段（高度大于 0 才保留）；没有被盖住时就是整条。
// 上段是 [strip.Y, min(top, 条下沿)]，下段是 [max(bottom, strip.Y), 条下沿]；区间完全在条外时落成整条（一段）。
func shadowStripParts(strip gfx.Rect, top, bottom float32, occluded bool) []gfx.Rect {
	if !occluded || bottom <= top {
		return []gfx.Rect{strip}
	}
	end := strip.Y + strip.Height
	var parts []gfx.Rect
	if upper := min(top, end); upper > strip.Y {
		parts = append(parts, gfx.Rect{X: strip.X, Y: strip.Y, Width: strip.Width, Height: upper - strip.Y})
	}
	if lower := max(bottom, strip.Y); lower < end {
		parts = append(parts, gfx.Rect{X: strip.X, Y: lower, Width: strip.Width, Height: end - lower})
	}
	return parts
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
		// SidebarPanel：relative flex min-w-0 flex-1 flex-col bg-sidebar-background/85。
		// 这个 bg 类在渲染层不生成任何样式（tailwind.config.js 的 sidebar 色只有 DEFAULT、foreground、accent、border，没有 background），
		// aside 与这一层都是透明的：侧栏底色只有 aside 的 panel-strong。此前这里按类名画了 Sidebar@85%，
		// 深色主题下整块侧栏比渲染层暗 6–7 个灰阶（(20,24,31) 对 (14,18,24)），L3 的逐通道容差 12 把它放过了
		// （2026-11-20 在 table 的深色变体上逐像素读得，台账 G-019）；浅色主题的 Sidebar 是白色，叠在白底上看不出。
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
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
