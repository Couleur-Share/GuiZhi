package kit

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// 上下文菜单（渲染层 components/ui/ContextMenu.tsx，台账 B-007）
//
// 渲染层的菜单是 portal 到 body 的 fixed 元素：左上角由调用方给定（顶栏「新建」旁的箭头取「按钮右缘 − 224、下缘 + 4」），
// 宽度随内容收缩（min-w-[160px]），不裁剪内容（悬停底色是方的，不跟随菜单的圆角）。
//
// 行为照渲染层：
//   - 打开时第一个未禁用的项自动聚焦（程序聚焦，鼠标打开时不画聚焦环，键盘操作后才画）；
//   - ↑ ↓ 在未禁用项之间循环，Home / End 跳到首尾（Viva 的 FocusGroup：移动焦点时画聚焦环）；Esc 关闭；
//   - 点菜单外面（含触发按钮）关闭，这一下继续交给下面的控件，触发按钮自己只收起、不再展开；
//   - 点项：先调 OnSelect、再关闭；禁用项不响应（pointer-events-none，也没有悬停）；
//   - 窗口尺寸变化即关闭（window resize → onClose）；
//   - 入场 animate-in fade-in zoom-in-95，时长 duration-quick、曲线 ease-enter；没有退场动画；
//   - 子菜单（Children）：悬停展开、→ 展开并聚焦第一个子项、← 回到父项，详见 contextmenu_sub.go。
//
// 尚未复刻：Tab 在菜单里逐项走（FocusGroup 把菜单当作一个 Tab 停靠点）；焦点离开菜单后 ↑ ↓ 把焦点带回菜单（台账 B-023 (2)(3)）。
// 渲染层的 insetLevel 与 childrenClassName 没有任何调用方（全仓搜索为证），所以不复刻。

const (
	menuViewportMargin float32 = 8   // 面板越出视口时与视口边缘的留白
	menuMinWidth       float32 = 160 // min-w-[160px]（border-box，含边框）
)

// 上下文菜单与 Select 共用背景层与面板层：同一时刻只会开一个（点外面即关闭另一个）。
const (
	bandMenuDismiss = bandSelectDismiss
	bandMenuPanel   = bandSelectPanel
)

// MenuItem 是菜单的一项（渲染层 ContextMenuItem）。
type MenuItem struct {
	Label string
	// Description 是标签下面一行小字，可空。
	Description string
	// Icon 是标签前的图标；ViewBox 为 0 表示没有。
	Icon gfx.Icon
	// Dot 非零时，标签前画一枚纯色圆点来代替图标：12px 的圆在 16px 的图标槽里居中（标签颜色菜单里的色板，
	// 渲染层 <span class="h-3 w-3 rounded-full bg-*">）。颜色是固定的品牌色，不随悬停变化。与 Icon 同时给时画 Icon。
	Dot tokens.Color
	// Shortcut 是右侧的快捷键提示，可空。
	Shortcut string
	// Destructive 为真时文字与图标一直是危险色（悬停也不变成强调色）。
	Destructive bool
	Disabled    bool
	// Children 非空：这一项是带子菜单的父项（行尾是 ChevronRight）。悬停或在它上面按 → 展开；
	// 点击父项本身没有反应（OnSelect 不会被调用）；子项里的 Children 被忽略（渲染层只有一层子菜单）。
	Children []MenuItem
	// OnSelect 在点击或按 Enter / Space 之后、菜单关闭之前调用；可空。
	OnSelect func()
}

// MenuSpec 描述一个菜单。Open 为假时什么都不画。
type MenuSpec struct {
	Open bool
	// X、Y 是菜单左上角（窗口坐标）；越出视口时按渲染层的算法挪回来。
	X, Y  float32
	Items []MenuItem
	// Keep 是触发按钮的外框：落在它上面的点击只关闭菜单、不往下传（渲染层的 ignoreRef）。
	Keep *gfx.Rect
}

// MenuState 是菜单的界面状态，由持有它的屏幕保存。每一帧都要经过 ContextMenu，
// Open 为假的那一帧由它负责复位，所以菜单不必由调用方按条件声明。
type MenuState struct {
	opened       bool    // 已经出过一帧：记下当时的窗口尺寸
	viewW, viewH float32 // 打开时的窗口尺寸：变了就关闭
	focused      bool    // 已把键盘交给第一项：打开时只请求一次，不在用户挪动焦点之后再拉回去

	// 子菜单（只在 Items 里有 Children 时用到，见 contextmenu_sub.go）。
	sub     menuSub
	hits    menuHits
	wraps   []menuWrapLayout // 每个根项外层的版面，跨帧保留
	subRect gfx.Rect         // 这一帧子面板画出来的外框（悬停桥在根面板绘制收尾时据此登记）
}

// menuSurface 是面板的表面（渲染层 rounded-md border border-border bg-popover shadow-lg）。
func (l *Look) menuSurface() Surface {
	t := l.T
	return Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75) - 2, Shadows: ShadowLG}
}

// ContextMenu 声明菜单：整窗的背景层加一块项面板。返回这一帧是否要求关闭（选了项、点了外面、按了 Esc 或窗口尺寸变了）。
func (l *Look) ContextMenu(c ui.C, st *MenuState, spec MenuSpec) (closed bool) {
	if !spec.Open {
		*st = MenuState{}
		return false
	}
	bounds := c.Bounds()
	if st.opened && (bounds.Width != st.viewW || bounds.Height != st.viewH) {
		*st = MenuState{}
		return true
	}
	if !st.opened {
		st.opened, st.viewW, st.viewH = true, bounds.Width, bounds.Height
	}

	canvas := c.Canvas()
	items := spec.Items
	width, height := l.menuSize(canvas, items)
	rect := menuRect(spec.X, spec.Y, width, height, bounds.Width, bounds.Height)

	if l.dismissLayer(c.Key("menu-dismiss"), dismissSpec{Band: bandMenuDismiss, PassPress: true, Keep: spec.Keep}) {
		*st = MenuState{}
		return true
	}

	hasSub := menuHasSub(items)
	if hasSub {
		l.subStep(canvas, st, items, rect)
	}

	scope := ui.ID(c.Key("menu-scope"))
	motion := l.Motion(DurationQuick, EaseEnter)
	surface := l.menuSurface()
	padY := l.Space(1) // py-1

	var firstID gfx.ID
	panel := l.floatingPanel(c.Key("menu-panel"), bandMenuPanel, rect).
		PadSides(1+padY, 1, 1+padY, 1).
		FocusGroup(ui.FocusVertical).
		Draw(func(d *ui.DrawContext) {
			enterPresence(d.Canvas, scope, 0, motion, 0.95, center(d.Rect))
			if !st.focused && !firstID.IsZero() {
				d.Canvas.RequestFocus(firstID)
				st.focused = true
			}
			l.PaintSurface(d, surface)
			// 面板是不透明的：按在面板边距上不能落到下面的背景层（那会关闭菜单）。
			d.Canvas.Block(d.Rect)
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleMenu, "", gfx.HitOptions{})
		}).
		DrawOver(func(d *ui.DrawContext) {
			d.Canvas.EndPresence()
			// 悬停桥向父项一侧伸出的那一条：在所有根项之后登记，压在它们之上（渲染层里桥的 z-index 高于根项）。
			if st.sub.open && !st.hits.subWrap.IsZero() {
				d.Canvas.HitWithOptions(st.hits.subWrap, l.bridgeStrip(st.subRect, st.sub.left), "", gfx.HitOptions{SkipFocus: true})
			}
		})
	if hasSub {
		right, left := panel.Shortcut(0, "right"), panel.Shortcut(0, "left")
		l.subKeys(canvas, st, items, rect, right, left)
		if len(st.wraps) != len(items) {
			st.wraps = make([]menuWrapLayout, len(items))
		}
	}
	st.hits.begin() // 上一帧登记的身份已经用完（subStep、subKeys），这一帧重建

	var chosen *MenuItem
	rowPad := l.Space(1.5)
	panel.Children(func() {
		for i := range items {
			it := &items[i]
			popup := len(it.Children) > 0
			open := popup && st.sub.open && st.sub.parent == i
			declare := func() {
				id, clicked := l.menuRow(c.Key("item"+strconv.Itoa(i)), *it, menuRowSpec{padY: rowPad, popup: popup, expanded: open})
				st.hits.rows = append(st.hits.rows, id)
				if it.Disabled {
					return
				}
				if firstID.IsZero() {
					firstID = id
				}
				// 点带子菜单的父项没有反应（渲染层 onClick 里 hasChildren 直接 return）。
				if clicked && !popup && chosen == nil {
					chosen = it
				}
			}
			if !hasSub {
				declare()
				continue
			}
			// 带子菜单的菜单：根项外面包一层 div.relative 的对应物。
			wc := c.Key("wrap" + strconv.Itoa(i))
			wrapID := ui.ID(wc)
			st.hits.wraps = append(st.hits.wraps, wrapID)
			model := &st.wraps[i]
			*model = menuWrapLayout{}
			if open {
				w, h := l.subMenuSize(canvas, it.Children)
				*model = menuWrapLayout{left: st.sub.left, top: st.sub.top, gap: l.Space(1), panelW: w, panelH: h}
			}
			ui.CustomLayout(wc, model).FillWidth().
				Draw(func(d *ui.DrawContext) {
					d.Canvas.HitWithOptions(wrapID, d.Rect, "", gfx.HitOptions{SkipFocus: true})
				}).
				Children(func() {
					declare()
					if open {
						if picked := l.subPanel(c, st, it.Children); picked != nil && chosen == nil {
							chosen = picked
						}
					}
				})
		}
	})

	if chosen != nil {
		if fn := chosen.OnSelect; fn != nil {
			fn()
		}
		*st = MenuState{}
		return true
	}
	return false
}

// menuRect 按渲染层的算法摆放面板：越过视口右（下）缘就把它挪到「视口宽（高）− 面板宽（高）− 8」。
// 渲染层是在入场动画刚开始时量的面板（zoom-in-95，宽高是终态的 95%），只有真的越界时才有 5% 以内的差别；
// 这里用终态尺寸。受支持的窗口尺寸下创建方式菜单不会越界。
func menuRect(x, y, w, h, vw, vh float32) gfx.Rect {
	left, top := x, y
	if x+w > vw {
		left = vw - w - menuViewportMargin
	}
	if y+h > vh {
		top = vh - h - menuViewportMargin
	}
	return gfx.Rect{X: left, Y: top, Width: w, Height: h}
}

// menuSize 是面板的宽高：宽度是最宽的一项的内容宽（w-max，不小于 160）加边框，高度是各项加 py-1 与边框。
func (l *Look) menuSize(canvas *gfx.Canvas, items []MenuItem) (width, height float32) {
	height = 2 + 2*l.Space(1)
	width = menuMinWidth
	for i := range items {
		w, h := l.menuItemSize(canvas, items[i], l.Space(1.5), len(items[i].Children) > 0)
		width = max(width, w+2)
		height += h
	}
	return width, height
}

// menuItemSize 是一项的宽高（border-box）：px-3，竖向内边距 padY（根项 py-1.5，子项 py-2），图标 16px、gap-2、
// 文字区取标签与说明里较宽的一行，快捷键提示与父项的 ChevronRight 前面各有 gap-2 加 ml-2。
// 行高是标签一行（text-sm，20px）加说明一行（text-xs，16px）。
func (l *Look) menuItemSize(canvas *gfx.Canvas, it MenuItem, padY float32, popup bool) (w, h float32) {
	padX, gap := l.Space(3), l.Space(2)
	labelSize, descSize := l.Rem(TextSM.Size), l.Rem(TextXS.Size)
	text := canvas.TextWidthWeight(it.Label, labelSize, WeightNormal)
	h = l.Rem(TextSM.Line)
	if it.Description != "" {
		text = max(text, canvas.TextWidthWeight(it.Description, descSize, WeightNormal))
		h += l.Rem(TextXS.Line)
	}
	w = 2*padX + text
	if it.Icon.ViewBox > 0 || it.Dot != (tokens.Color{}) {
		w += l.Rem(1) + gap
	}
	if it.Shortcut != "" {
		w += gap + l.Space(2) + canvas.TextWidthWeight(it.Shortcut, descSize, WeightNormal)
	}
	if popup {
		w += gap + l.Space(2) + l.Rem(1)
	}
	return w, h + 2*padY
}

// menuItemName 是一项的可访问名：按钮的名称取自内容，依次是标签、说明与快捷键提示（图标 aria-hidden）。
func menuItemName(it MenuItem) string {
	parts := []string{it.Label}
	if it.Description != "" {
		parts = append(parts, it.Description)
	}
	if it.Shortcut != "" {
		parts = append(parts, it.Shortcut)
	}
	return strings.Join(parts, " ")
}

// menuRowSpec 是一行的版式参数：根项与子项只有竖向内边距不同，父项多一个 ChevronRight 与展开状态。
type menuRowSpec struct {
	padY     float32 // 竖向内边距：根项 py-1.5，子项 py-2
	popup    bool    // 带子菜单：aria-haspopup，行尾画 ChevronRight
	expanded bool    // 子菜单展开中（aria-expanded）
}

// menuRow 声明一项（渲染层的 button[role=menuitem]）：w-full flex items-center gap-2 px-3 py-1.5 text-sm，
// 悬停 bg-accent text-accent-foreground（transition-colors，Tailwind 默认的 150 ms），危险项文字恒为 text-destructive，
// 禁用项 opacity-50 且 pointer-events-none。返回它的身份与这一帧是否被点中。
//
// 键盘聚焦环是全局的 button:focus-visible（ring-2 ring-primary/50），菜单不裁剪内容，环画在项外面、压住菜单边框。
func (l *Look) menuRow(c ui.C, it MenuItem, rs menuRowSpec) (gfx.ID, bool) {
	t := l.T
	id := ui.ID(c)
	opts := gfx.HitOptions{Role: gfx.RoleMenuItem, Disabled: it.Disabled, Expandable: rs.popup, Expanded: rs.expanded}
	if !it.Disabled {
		opts.Cursor = gfx.PointerCursorHand
	}
	row := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(rs.padY, l.Space(3), rs.padY, l.Space(3)).
		HitWith(id, menuItemName(it), opts)
	// 过渡进度在声明时取一次：文字与图标的颜色要在声明里定下，不能等到绘制。
	p := l.hoverTo(c.Canvas(), id, 0, row.State().Hovered && !it.Disabled, DurationDefault)

	base, hover := t.Foreground, t.AccentForeground
	if it.Destructive {
		base, hover = t.Destructive, t.Destructive
	}
	fg, muted := MixToken(base, hover, p), t.MutedForeground
	if it.Disabled {
		fg, muted = fade(fg, 0.5), fade(muted, 0.5)
	}
	row = row.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, l.ringColor(), 0)
		}
		if p > 0 {
			l.PaintSurface(d, Surface{Bg: t.Accent.WithAlpha(p)})
		}
	})
	row.Children(func() {
		switch {
		case it.Icon.ViewBox > 0:
			l.Icon(c, it.Icon, l.Rem(1), fg).Shrink(0)
		case it.Dot != (tokens.Color{}):
			l.menuDot(c, it.Dot)
		}
		ui.Column(c.Key("text")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			l.Text(c, it.Label, TextSM, WeightNormal, fg, Ellipsis())
			if it.Description != "" {
				l.Text(c, it.Description, TextXS, WeightNormal, muted, Ellipsis())
			}
		})
		if it.Shortcut != "" {
			l.Text(c, it.Shortcut, TextXS, WeightNormal, muted).MarginLeft(l.Space(2)).Shrink(0)
		}
		if rs.popup {
			l.Icon(c, icons.ChevronRight, l.Rem(1), muted).MarginLeft(l.Space(2)).Shrink(0)
		}
	})
	return id, row.Clicked()
}

// menuDot 是菜单项前的色点：16px 的图标槽，里面居中一枚 12px 的圆（渲染层 w-4 h-4 的包裹里放 h-3 w-3 rounded-full）。
func (l *Look) menuDot(c ui.C, color tokens.Color) {
	slot, dot := l.Rem(1), l.Rem(0.75)
	ui.Row(c.Key("dot")).Size(slot, slot).Shrink(0).Items(ui.Center).Justify(ui.Center).Children(func() {
		ui.Row(c.Key("dot-fill")).Size(dot, dot).Shrink(0).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, Surface{Bg: color, Radius: dot / 2})
		})
	})
}
