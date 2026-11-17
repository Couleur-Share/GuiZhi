package shell

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 顶栏窗口按钮：Tailwind w-11（2.75rem），渲染层把它们画成 text-foreground/70 的线性图标。
const windowButtonRem = 2.75

// vivaWindowControlWidth 是 Viva 在自绘标题栏里画的每枚窗口按钮的宽度（逻辑像素，见台账 A-005）。
const vivaWindowControlWidth = app.WindowControlWidth

// Tailwind red-500：关闭按钮悬停的底色。
var red500 = tokens.RGBA(239, 68, 68, 1)

// topBar 是顶栏（渲染层 TopBar）：h-12、border-b、pl-4；左边收起按钮，中间常驻搜索，右边更新提示与「新建」，最右是窗口按钮。
//
// 整栏是拖动窗口的区域（MovesWindow），里面的按钮与搜索框照常接收自己的点击。
func (w *Window) topBar(c ui.C) {
	l := w.Look
	t := l.T
	wide := l.ViewportWidth(c) >= smBreakpoint
	ui.Row(c).FillWidth().Height(l.Rem(3)).Shrink(0).Items(ui.Center).
		PadSides(0, 0, 1, l.Space(4)).MovesWindow().
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.UiToolbarBg, Shadow: t.UiToolbarShadow})
		}).
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.UiToolbarBorder) }).
		Children(func() {
			w.sidebarToggle(c)
			w.searchBox(c, wide)
			w.topActions(c, wide)
			w.windowControls(c)
		})
}

// sidebarToggle 是左端的收起/展开按钮：外面 w-8 shrink-0，里面 h-8 w-8 rounded-lg，muted 图标，悬停 bg-accent/60。
func (w *Window) sidebarToggle(c ui.C) {
	l := w.Look
	label := locales.CommonCollapse()
	if w.sidebarCollapsed(c) {
		label = locales.CommonExpand()
	}
	c = c.Key("toggle")
	ui.Row(c).Width(l.Rem(2)).Shrink(0).Children(func() {
		w.iconButton(c, icons.PanelLeft, l.Rem(1), label, l.Rem(0.5), func() { w.toggleSidebar(c) })
	})
}

// iconButton 是一枚 2rem 见方的图标按钮（h-8 w-8）：muted 前景，悬停 bg-accent/60 与前景色；返回是否被点击并已回调。
func (w *Window) iconButton(c ui.C, icon gfx.Icon, size float32, label string, radius float32, onClick func()) {
	l := w.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).Size(l.Rem(2), l.Rem(2)).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label})
	fg := t.MutedForeground
	surface := kit.Surface{Radius: radius}
	if box.State().Hovered {
		fg = t.Foreground
		surface.Bg = t.Accent.WithAlpha(0.6)
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		l.Icon(c, icon, size, fg).Shrink(0)
	})
	if box.Clicked() && onClick != nil {
		onClick()
	}
}

// searchBox 是顶栏中间的常驻搜索（flex-1 justify-center px-3 里的 max-w-lg）。
// 知识库模块下是可输入的搜索框；其余模块是只读的入口，点击、聚焦与按键都打开命令面板。
func (w *Window) searchBox(c ui.C, wide bool) {
	l := w.Look
	t := l.T
	library := w.module() == ModuleLibrary
	placeholder := locales.HeaderSearch()
	if !library {
		placeholder = locales.HeaderOmniSearch()
	}
	height := l.Rem(2.25)
	ui.Row(c.Key("search")).Grow(1).MinWidth(0).Justify(ui.Center).PadX(l.Space(3)).Children(func() {
		ui.Stack(c).FillWidth().MaxWidth(l.Rem(32)).Height(height).Children(func() {
			// 底层的面板：pointer-events-none absolute inset-0 rounded-lg border，取搜索栏的语义令牌。
			ui.Row(c).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{
					Bg: t.UiSearchBg, Border: t.UiSearchBorder, BorderW: 1, Radius: l.Rem(0.5), Shadow: t.UiSearchShadow,
				})
			})
			// 输入区：h-9 w-full border border-transparent pl-9（边框 1px 透明，所以文字从 37px 起）text-sm，pr-32（知识库）/ pr-28。
			// 输入框铺满整个搜索框；占位符与输入内容走同一条绘制路径，基线按渲染层 <input> 的规则放（kit.Input 的推导）。
			right := l.Rem(7)
			field := &w.trigger
			if library {
				right = l.Rem(8)
				field = &w.search
			}
			// Placeholder 取与 Viva 自带占位符相同的 MutedForeground，只为走 kit.Input 的叠画路径：占位符放不下时与 Chromium
			// 一样在内容框边缘硬裁剪；Viva 自带的占位符会改成「前缀 + …」的省略（大字号、窄窗口下 800×600 会比渲染层少显示「命令」二字）。
			input := l.Input(c, field, placeholder, placeholder, kit.InputSpec{
				Size: l.Rem(0.875), Weight: kit.WeightNormal, Color: t.Foreground,
				Height: height, Left: l.Rem(2.25) + 1, Right: right, Placeholder: t.MutedForeground,
			}).Absolute().Inset(0)
			if !library {
				// 只读入口：点击、聚焦、按键都打开命令面板（渲染层 readOnly + onFocus/onClick/onKeyDown），输入框自己不收字。
				if field.Text() != "" {
					field.SetText("")
				}
				if input.Clicked() && w.OnOpenPalette != nil {
					w.OnOpenPalette()
				}
			}
			// 左侧的放大镜：absolute left-3，垂直居中，h-4 w-4 muted。
			ui.Row(c).Absolute().Left(l.Space(3)).Top((height-l.Rem(1))/2).Size(l.Rem(1), l.Rem(1)).Children(func() {
				l.Icon(c, icons.Search, l.Rem(1), t.MutedForeground).Shrink(0)
			})
			// 右侧的快捷键提示：absolute right-2，垂直居中；sm 以上才显示。
			if wide && w.SearchShortcut != "" {
				w.shortcutChip(c, height)
			}
		})
	})
}

// shortcutChip 是搜索框右端的快捷键提示按钮：rounded-md border border-border/70 bg-background/60 px-1.5 py-0.5 font-mono text-[10px]。
func (w *Window) shortcutChip(c ui.C, height float32) {
	l := w.Look
	t := l.T
	label := locales.HeaderOpenCommandPalette()
	ui.Row(c.Key("chip")).Absolute().Right(l.Space(2)).Top(0).Height(height).Items(ui.Center).Children(func() {
		id := ui.ID(c)
		box := ui.Row(c).Items(ui.Center).PadSides(l.Space(0.5)+1, l.Space(1.5)+1, l.Space(0.5)+1, l.Space(1.5)+1).
			HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label})
		fg := t.MutedForeground
		bg := t.Background.WithAlpha(0.6)
		if box.State().Hovered {
			fg = t.Foreground
			bg = t.Accent.WithAlpha(0.6)
		}
		box.Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.375)})
		}).Children(func() {
			// text-[10px] font-mono：行高继承 1.5 倍（15px）。Viva 的固定间距文字按整数字格排（宽度取整、行高取字格高），
			// 与 Chromium 按字体真实字距排的结果不同（台账 A-006）：这里把行盒与宽度做成渲染层的值，字形仍按 Viva 的字格画。
			ui.Row(c).Width(kit.MonoAdvance(w.SearchShortcut, 10)).Height(15).Items(ui.Center).Children(func() {
				ui.Text(c, w.SearchShortcut).FontSize(10).Color(l.Col(fg)).Mono()
			})
		})
		if box.Clicked() && w.OnOpenPalette != nil {
			w.OnOpenPalette()
		}
	})
}

// topActions 是搜索右边的一组：ml-2 gap-1 pr-1；有更新时先是虚线边框的更新按钮与一条竖分隔线，然后是「新建」拆分按钮。
func (w *Window) topActions(c ui.C, wide bool) {
	l := w.Look
	t := l.T
	ui.Row(c.Key("actions")).Shrink(0).Items(ui.Center).Gap(l.Space(1)).MarginLeft(l.Space(2)).PadRight(l.Space(1)).Children(func() {
		if w.Update {
			w.updateButton(c, wide)
			ui.Row(c).Size(1, l.Rem(1.25)).MarginX(l.Space(1)).Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Border})
			})
		}
		w.newItemButton(c, wide)
	})
}

// updateButton 是「有新版本」按钮：h-8 gap-1.5 rounded-lg px-3 text-sm font-medium text-primary，虚线边框；窄窗口只剩图标。
func (w *Window) updateButton(c ui.C, wide bool) {
	l := w.Look
	t := l.T
	label := locales.SettingsUpdateAvailable()
	text := label
	if w.UpdateVersion != "" {
		text = locales.SettingsNewVersion(w.UpdateVersion)
	}
	c = c.Key("update")
	id := ui.ID(c)
	box := ui.Row(c).Height(l.Rem(2)).Shrink(0).Items(ui.Center).Gap(l.Space(1.5)).PadX(l.Space(3)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton})
	bg := t.Primary.WithAlpha(0.1)
	if box.State().Hovered {
		bg = t.Primary.WithAlpha(0.2)
	}
	box.Draw(func(d *ui.DrawContext) {
		// 渲染层是虚线边框（border-dashed）；Chromium 的虚线节奏还没有对照采集，这里先画实线：台账 B-006。
		l.PaintSurface(d, kit.Surface{Bg: bg, Border: t.Primary.WithAlpha(0.5), BorderW: 1, Radius: l.Rem(0.5)})
	}).Children(func() {
		l.Icon(c, icons.Download, l.Rem(1), t.Primary).Shrink(0)
		if wide {
			l.Text(c, text, kit.TextSM, kit.WeightMedium, t.Primary)
		}
	})
	if box.Clicked() && w.OnUpdate != nil {
		w.OnUpdate()
	}
}

// newItemButton 是「新建」拆分按钮：主按钮（Plus 与文字，sm 以上才有文字）、1px 分隔线、下拉箭头按钮。
// 两半各自悬停变 bg-primary/90；主按钮只有左侧圆角，箭头按钮只有右侧圆角。
// 两半各画各的底，中间 1px 分隔线下面是页面底色（渲染层的两个按钮各自有 bg-primary，分隔线 bg-primary-foreground/25
// 直接叠在页面上，浅色下是纯白）。
func (w *Window) newItemButton(c ui.C, wide bool) {
	l := w.Look
	t := l.T
	c = c.Key("new")
	newLabel := locales.HeaderNew()
	caretLabel := locales.HeaderCreateMenu()
	var mainHover, caretHover bool
	radius := l.Rem(0.5)
	bgFor := func(hover bool) tokens.Color {
		if hover {
			return t.Primary.WithAlpha(0.9)
		}
		return t.Primary
	}
	// paintHalf 把圆角矩形的另一侧挪到裁剪区之外：side 为 -1 时只留左半（左侧圆角），为 1 时只留右半。
	paintHalf := func(d *ui.DrawContext, bg tokens.Color, side int) {
		shifted := *d
		shifted.Rect = d.Rect
		shifted.Rect.Width += radius
		if side > 0 {
			shifted.Rect.X -= radius
		}
		l.PaintSurface(&shifted, kit.Surface{Bg: bg, Radius: radius, Clip: d.Rect})
	}
	group := ui.Row(c).Height(l.Rem(2)).Shrink(0).Items(ui.Stretch)
	group.Children(func() {
		// 主按钮：pl-2.5 pr-2 gap-1.5 text-sm font-medium text-primary-foreground。
		mc := c.Key("main")
		id := ui.ID(mc)
		main := ui.Row(mc).Items(ui.Center).Gap(l.Space(1.5)).PadSides(0, l.Space(2), 0, l.Space(2.5)).
			HitWith(id, newLabel, gfx.HitOptions{Role: gfx.RoleButton})
		mainHover = main.State().Hovered
		main.Draw(func(d *ui.DrawContext) { paintHalf(d, bgFor(mainHover), -1) }).Children(func() {
			l.Icon(c, icons.Plus, l.Rem(1), t.PrimaryForeground).Shrink(0)
			if wide {
				l.Text(c, newLabel, kit.TextSM, kit.WeightMedium, t.PrimaryForeground)
			}
		})
		if main.Clicked() && w.OnNewCapture != nil {
			w.OnNewCapture()
		}
		// 分隔线：w-px bg-primary-foreground/25。
		ui.Row(c).Width(1).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.PrimaryForeground.WithAlpha(0.25)})
		})
		// 下拉箭头：w-6，ChevronDown h-3.5 w-3.5。
		cc := c.Key("caret")
		caretID := ui.ID(cc)
		caret := ui.Row(cc).Width(l.Rem(1.5)).Items(ui.Center).Justify(ui.Center).
			HitWith(caretID, caretLabel, gfx.HitOptions{Role: gfx.RoleCombobox, Expandable: true, Expanded: w.createMenuOpen, Hint: caretLabel})
		caretHover = caret.State().Hovered
		caret.Draw(func(d *ui.DrawContext) {
			w.caretRect = d.Rect
			paintHalf(d, bgFor(caretHover), 1)
		}).Children(func() {
			// 展开时箭头转 180°（transition-transform，没有写 duration-*：Tailwind 默认 150 ms）。
			l.RotatingChevron(c, caretID, l.Rem(0.875), w.createMenuOpen, t.PrimaryForeground, kit.DurationDefault).Shrink(0)
		})
		// 展开时点箭头由菜单的背景层接走（Keep）：只收起、不再传到这里，所以这里只会在收起状态下收到点击。
		if caret.Clicked() && !w.createMenuOpen {
			w.createMenuOpen = true
			// 渲染层在点击时取一次锚点：按钮右缘 − 224、下缘 + 4（逻辑像素字面量，不随字号档缩放）。
			w.createMenuAt = gfx.Point{X: w.caretRect.Right() - 224, Y: w.caretRect.Bottom() + 4}
		}
	})
}

// newMenu 声明创建方式菜单（渲染层 NewItemButton 的 ContextMenu）：三项，点箭头展开。
// 它是浮层，要在整个页面声明完之后才声明（见 View）。
func (w *Window) newMenu(c ui.C) {
	l := w.Look
	keep := w.caretRect
	items := []kit.MenuItem{
		{Label: locales.CaptureTitle(), Description: locales.HeaderNewCaptureDesc(), Icon: icons.Sparkles,
			Shortcut: w.CaptureShortcut, OnSelect: w.OnNewCapture},
		{Label: locales.CaptureBlankNote(), Description: locales.HeaderNewBlankNoteDesc(), Icon: icons.StickyNote,
			OnSelect: w.OnNewBlankNote},
		{Label: locales.HeaderNewImportFiles(), Description: locales.HeaderNewImportFilesDesc(), Icon: icons.FileUp,
			OnSelect: w.OnImportFiles},
	}
	if l.ContextMenu(c, &w.createMenu, kit.MenuSpec{
		Open: w.createMenuOpen, X: w.createMenuAt.X, Y: w.createMenuAt.Y, Items: items, Keep: &keep,
	}) {
		w.createMenuOpen = false
	}
}

// windowControls 画最小化、最大化与关闭三个按钮（渲染层 WindowControls：ml-1，每个 w-11，h-12 满高）。
// 真窗口里这三个按钮由 Viva 画在窗口右上角，这里只留出位置（台账 A-005）。
func (w *Window) windowControls(c ui.C) {
	l := w.Look
	t := l.T
	c = c.Key("controls")
	width := 3*l.Rem(windowButtonRem) + l.Space(1)
	if !w.OwnControls {
		// Viva 的按钮固定 46px 宽，三枚共 138px：留位取两者较大者，避免压住「新建」。
		ui.Row(c).Width(max(width, 3*vivaWindowControlWidth)).Height(l.Rem(3)).Shrink(0)
		return
	}
	maxLabel := locales.CommonMaximize()
	if w.Maximized {
		maxLabel = locales.CommonRestore()
	}
	ui.Row(c).Height(l.Rem(3)).Shrink(0).MarginLeft(l.Space(1)).Items(ui.Stretch).Children(func() {
		button := func(key string, icon gfx.Icon, size float32, label string, closeButton bool, onClick func()) {
			c := c.Key(key)
			id := ui.ID(c)
			box := ui.Row(c).Width(l.Rem(windowButtonRem)).Items(ui.Center).Justify(ui.Center).
				HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton})
			fg := t.Foreground.WithAlpha(0.7)
			surface := kit.Surface{}
			if box.State().Hovered {
				fg, surface.Bg = t.Foreground, t.Muted
				if closeButton {
					fg, surface.Bg = tokens.RGBA(255, 255, 255, 1), red500
				}
			}
			box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
				l.Icon(c, icon, size, fg).Shrink(0)
			})
			if box.Clicked() && onClick != nil {
				onClick()
			}
		}
		button("min", icons.Minus, l.Rem(1), locales.CommonMinimize(), false, w.OnMinimize)
		button("max", icons.Square, l.Rem(0.875), maxLabel, false, w.OnMaximize)
		button("close", icons.X, l.Rem(1), locales.CommonClose(), true, w.OnClose)
	})
}
