package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 文章工具侧栏（渲染层 ArticleToolsPanel）：头部「更多」菜单里的「文章信息与工具」打开的右侧面板，
// 里面是文章信息、标签、内容处理（复制给 AI、AI 摘要入口、来源评论）与网页条目的来源版本。
//
// 面板盖在文章栏的右侧（absolute bottom-4 right-4 top-4 z-30 w-[26rem] max-w-[calc(100%-2rem)]），Esc 只在焦点落在面板里时收起；
// 打开时焦点落到收起钮上。阅读岛（原生视图）在场时，面板与查找条同理抬到更高的一层（见 findBar）。
//
// 面板接的是信息、标签（含 AI 标签建议）、复制给 AI、图中文字、AI 摘要、来源评论与来源版本；媒体预览（M2-6d）
// 的位置在这里留着，台账 B-067。

// toolsUI 是工具侧栏的界面状态。openID 对应渲染层 ItemDetail 的 toolsItemId（条目 ID：换了条目面板自然是关着的）；
// 其余随 ArticleToolsPanel 与来源评论的提供者挂载而生，Store 里的挂载序号变了（换条目、采用版本后重新读取）就整体清空。
type toolsUI struct {
	openID string

	// mountItem 与 mount 记着下面这些状态属于哪一次挂载。
	mountItem string
	mount     int

	// area 是上一帧文章栏的外框（面板相对它摆放，阅读岛在场时据此把面板抬到更高的一层）；
	// scope 是面板的焦点作用域（Esc 只在焦点落进面板时收起它）。
	area  gfx.Rect
	scope gfx.ID

	tags     tagEditorUI
	summary  summaryUI
	handoff  handoffUI
	comments commentsUI
	versions versionsUI
}

// toolsOpenFor 报告某条的工具侧栏是不是开着。
func (s *Screen) toolsOpenFor(id string) bool {
	return id != "" && s.detail.tools.openID == id
}

// toggleTools 开合工具侧栏（头部菜单两项共用：渲染层 setAskItemId(null) 之后 setToolsItemId(开着 ? null : item.id)）。
func (s *Screen) toggleTools(id string) {
	s.closeAsk()
	t := &s.detail.tools
	if t.openID == id {
		t.openID = ""
		return
	}
	t.openID = id
}

// closeTools 收起工具侧栏。
func (s *Screen) closeTools() { s.detail.tools.openID = "" }

// syncTools 让随挂载而生的状态跟上 Store 的挂载序号：条目换了或序号变了就清空（保留 openID、外框与作用域）。
// 条目换了而 Store 还没挂上新条目时序号按 0 算，等它挂上再清一次，效果相同。
func (s *Screen) syncTools(tv ToolsView, id string) {
	t := &s.detail.tools
	mount := 0
	if tv.ItemID == id {
		mount = tv.Mount
	}
	if t.mountItem == id && t.mount == mount {
		return
	}
	t.tags.ai.reset() // 整体覆盖之前先作废在途的标签建议：它的后台回写只认令牌，不会写进新的状态
	*t = toolsUI{openID: t.openID, mountItem: id, mount: mount, area: t.area, scope: t.scope}
}

// toolsEscape 处理 Esc：焦点落在面板里（面板自己的作用域，不含菜单、下拉与标签浮层这些各有作用域的弹层）时收起面板，
// 并告诉调用方按键被拦下了（渲染层在面板上 stopPropagation，专注阅读的退出不再运行）。
func (s *Screen) toolsEscape(focus kit.FocusInfo) bool {
	t := &s.detail.tools
	if t.openID == "" || !focus.Held || t.scope.IsZero() || focus.Scope != t.scope {
		return false
	}
	t.openID = ""
	return true
}

const (
	toolsPanelWidthRem float32 = 26 // w-[26rem]
)

// toolsPanelWidth 是面板宽：min(26rem, 文章栏宽 − 2rem)（max-w-[calc(100%-2rem)]）；文章栏的宽还没量到（首帧）取 26rem。
func toolsPanelWidth(l *kit.Look, area gfx.Rect) float32 {
	w := l.Rem(toolsPanelWidthRem)
	if area.Width > 0 {
		w = min(w, area.Width-l.Rem(2))
	}
	return max(w, 0)
}

// toolsPanel 声明工具侧栏（文章栏最后一个子项，叠在头部与内容面板之上）。回收站里的条目没有面板；面板关着时什么也不画。
func (s *Screen) toolsPanel(c ui.C, v View, item services.LibraryItem, caps ItemCaps, lifted bool) {
	t := &s.detail.tools
	if caps.Trashed || !s.toolsOpenFor(item.ID) {
		return
	}
	l := s.Look
	width := toolsPanelWidth(l, t.area)
	body := func(panel ui.Box) {
		panel.Children(func() {
			s.toolsHeader(c, item)
			s.toolsBody(c, v, item, caps)
		})
	}
	if !lifted {
		panel := s.toolsShell(ui.Column(c.Key("tools")).Absolute().Top(l.Space(4)).Right(l.Space(4)).Bottom(l.Space(4)).Width(width), c)
		body(panel)
		return
	}
	// 阅读岛是原生视图，盖住同层里画在它之后的东西：面板改用 ui.Overlay 放在更高的一层，位置与页面层里一致（窗口坐标、
	// 取上一帧量到的文章栏外框，首帧没有就下一帧再画）；详情在表格视图的详情浮层里时，原生视图在浮层的层上，要再高一层。
	r := t.area
	if r.Width <= 0 || r.Height <= 0 {
		c.Canvas().RequestFrame()
		return
	}
	band := kit.BandOverIsland
	if s.detail.modal.open {
		band = kit.BandOverIslandInModal
	}
	bounds := gfx.Rect{X: r.X + r.Width - l.Space(4) - width, Y: r.Y + l.Space(4), Width: width, Height: max(r.Height-l.Space(8), 0)}
	layer := ui.Overlay(c.Key("tools-lift"), ui.OverlaySpec{Band: band, Bounds: bounds})
	if s.detail.modal.open {
		layer = l.WithinModal(&s.detail.modal.state, layer)
	}
	layer.Children(func() {
		body(s.toolsShell(ui.Column(c.Key("tools")).FillWidth().FillHeight(), c))
	})
}

// toolsShell 给面板的列加上外观：flex-col overflow-hidden rounded-xl border border-border bg-background shadow-xl。
// 面板是不透明的（按在面板空白处不能落到下面的内容），可访问树里是一个带名字的区域；焦点作用域在这里开、在 DrawOver 里关。
func (s *Screen) toolsShell(panel ui.Box, c ui.C) ui.Box {
	l := s.Look
	t := l.T
	tools := &s.detail.tools
	tools.scope = ui.ID(c.Key("tools-scope"))
	scope := tools.scope
	return panel.Pad(1).
		Draw(func(d *ui.DrawContext) {
			// 不声明 ReturnFocus：渲染层 effect 清理里的 previous.focus() 拿到的是 body——ContextMenu 在选中菜单项时被摘掉，
			// 焦点已落到 body——所以关闭后焦点哪里也不在；照原样（见 TestToolsPanelLeavesNoFocusWhenItCloses）。
			d.Canvas.PushFocusScope(scope)
			l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75), Shadows: kit.ShadowXL})
			d.Canvas.Block(d.Rect)
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, locales.ArticleReaderTools(), gfx.HitOptions{})
		}).
		DrawOver(func(d *ui.DrawContext) { d.Canvas.PopFocusScope() })
}

// toolsHeader 是面板头部：flex shrink-0 items-start gap-3 border-b border-border/60 p-4。
// 左边是标题与条目标题（单行截断），右边是 32px 的收起钮；面板一打开焦点就落在收起钮上。
func (s *Screen) toolsHeader(c ui.C, item services.LibraryItem) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("head")).FillWidth().Shrink(0).Items(ui.Start).Gap(l.Space(3)).PadSides(l.Space(4), l.Space(4), l.Space(4)+1, l.Space(4)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			ui.Column(c.Key("titles")).Grow(1).Shrink(1).Basis(0).MinWidth(0).Children(func() {
				l.Heading(c.Key("title"), 2, locales.ArticleReaderTools(), kit.TextSM, kit.WeightSemibold, t.Foreground)
				l.Text(c.Key("item"), item.Title, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).MarginTop(l.Space(1))
			})
			closeName := locales.ArticleReaderCloseTools()
			box := l.Button(c.Key("close"), kit.ButtonSpec{Icon: icons.X, IconSize: l.Rem(1), Center: true, Name: closeName, Autofocus: true}, kit.ButtonStyle{
				Height: l.Rem(2), Width: l.Rem(2), Radius: l.Rem(0.75),
				Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent, Transition: kit.DurationDefault,
			})
			if box.Clicked() {
				s.closeTools()
			}
		})
}

// toolsBody 是面板的滚动区：min-h-0 flex-1 space-y-6 overflow-y-auto p-4。三块：文章信息与标签、内容处理、（网页条目的）来源版本。
func (s *Screen) toolsBody(c ui.C, v View, item services.LibraryItem, caps ItemCaps) {
	l := s.Look
	pad := l.Space(4)
	l.ScrollAreaView(c.Key("scroll"), [4]float32{pad, pad, pad, pad}, func(kit.ScrollView) {
		ui.Column(c.Key("sections")).FillWidth().Gap(l.Space(6)).Children(func() {
			s.toolsInfo(c, v, item)
			// 媒体预览（渲染层 MediaPreview，空时整块隐藏）暂留空，待裁决，台账 C-016。
			s.toolsProcessing(c, v, item, caps)
			if item.ItemType == services.LibraryTypeWebpage {
				s.toolsVersions(c, v, item)
			}
		})
	}).Grow(1).FillWidth().MinHeight(0)
}

// toolsSection 是带上边线的一块：border-t border-border/60 pt-4（上边线一像素，之后留 1rem）。
func (s *Screen) toolsSection(c ui.C) ui.Box {
	l := s.Look
	t := l.T
	return ui.Column(c).FillWidth().PadSides(l.Space(4)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) })
}

// toolsInfo 是「文章信息」一块：三列的 dl（内容类型、正文长度、最近更新），下面 mt-5 一条上边线，标签标题与标签行。
func (s *Screen) toolsInfo(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("info")).FillWidth().Children(func() {
		// grid grid-cols-3 gap-4 text-sm：三个等宽的轨道。
		ui.Row(c.Key("dl")).FillWidth().Gap(l.Space(4)).Children(func() {
			cell := func(key, label, value string) {
				ui.Column(c.Key(key)).Grow(1).Shrink(1).Basis(0).MinWidth(0).Children(func() {
					l.Text(c.Key("dt"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginBottom(l.Space(1))
					l.Text(c.Key("dd"), value, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
				})
			}
			cell("type", locales.ArticleReaderType(), kit.GetItemTypeMeta(item.ItemType).Label)
			cell("length", locales.ArticleReaderLength(), locales.LibraryWordCount(s.derivedOf(item).words))
			cell("updated", locales.ArticleReaderUpdated(), kit.FormatItemTime(item.UpdatedAt, s.nowTime(), s.loc(), s.Locale))
		})
		// mt-5 border-t border-border/60 pt-4
		s.toolsSection(c.Key("tags")).MarginTop(l.Space(5)).Children(func() {
			l.Heading(c.Key("title"), 3, locales.ArticleReaderTags(), kit.TextSM, kit.WeightMedium, t.Foreground)
			s.tagEditor(c, v, item)
		})
	})
}

// toolsProcessing 是「内容处理」一块：space-y-3 border-t border-border/60 pt-4。标题行右边是「复制给 AI」，
// 下面一排 flex-wrap gap-2：图中文字卡片（图片条目，M2-6c）、AI 摘要入口（非音视频）、来源评论卡片。
func (s *Screen) toolsProcessing(c ui.C, v View, item services.LibraryItem, caps ItemCaps) {
	l := s.Look
	t := l.T
	s.toolsSection(c.Key("processing")).Gap(l.Space(3)).Children(func() {
		ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).Children(func() {
			l.Heading(c.Key("title"), 3, locales.ArticleReaderProcessing(), kit.TextSM, kit.WeightMedium, t.Foreground)
			s.handoffButton(c.Key("handoff"), v, item)
		})
		ui.Flow(c.Key("cards")).FillWidth().Items(ui.Center).Gap(l.Space(2)).LineGap(l.Space(2)).Children(func() {
			s.ocrCard(c.Key("ocr"), v, item)
			if !caps.IsMediaItem {
				s.summaryCard(c.Key("summary"), v, item)
			}
			s.commentsCard(c.Key("comments"), v, item)
		})
	})
}

// actionChipStyle 是详情页轻量动作按钮的外观（渲染层 ACTION_CHIP）：inline-flex h-7 shrink-0 items-center gap-1.5 rounded-lg border
// border-border/70 px-2.5 text-xs text-muted-foreground，悬停边框、底与字变 primary（/50、/5、实色）。
func (s *Screen) actionChipStyle() kit.ButtonStyle {
	l := s.Look
	t := l.T
	return kit.ButtonStyle{
		Height: l.Rem(1.75), PadX: l.Space(2.5), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.MutedForeground, FgHover: t.Primary, BgHover: t.Primary.WithAlpha(0.05),
		Border: t.Border.WithAlpha(0.7), BorderHover: t.Primary.WithAlpha(0.5), BorderW: 1,
		Transition: kit.DurationDefault, DisabledOpacity: 0.6,
	}
}
