package kit

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
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
//   - 入场 animate-in fade-in zoom-in-95，时长 duration-quick、曲线 ease-enter；没有退场动画。
//
// 尚未复刻：子菜单（children）与缩进项（insetLevel）；Tab 在菜单里逐项走；焦点离开菜单后 ↑ ↓ 把焦点带回菜单。
// 这些都登记在台账 B-023。当前唯一的使用方（创建方式菜单）用不到前两项；后两项是键盘行为差异（FocusGroup 把菜单当作一个 Tab 停靠点）。

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
	// Shortcut 是右侧的快捷键提示，可空。
	Shortcut string
	// Destructive 为真时文字与图标一直是危险色（悬停也不变成强调色）。
	Destructive bool
	Disabled    bool
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
	width, height := l.menuSize(canvas, spec.Items)
	rect := menuRect(spec.X, spec.Y, width, height, bounds.Width, bounds.Height)

	if l.dismissLayer(c.Key("menu-dismiss"), dismissSpec{Band: bandMenuDismiss, PassPress: true, Keep: spec.Keep}) {
		*st = MenuState{}
		return true
	}

	t := l.T
	surface := Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75) - 2, Shadows: ShadowLG} // rounded-md
	scope := ui.ID(c.Key("menu-scope"))
	motion := l.Motion(DurationQuick, EaseEnter)
	padY := l.Space(1) // py-1

	var firstID gfx.ID
	picked := -1
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
		DrawOver(func(d *ui.DrawContext) { d.Canvas.EndPresence() })

	panel.Children(func() {
		for i, it := range spec.Items {
			id, clicked := l.menuRow(c.Key("item"+strconv.Itoa(i)), it)
			if it.Disabled {
				continue
			}
			if firstID.IsZero() {
				firstID = id
			}
			if clicked && picked < 0 {
				picked = i
			}
		}
	})

	if picked >= 0 {
		if fn := spec.Items[picked].OnSelect; fn != nil {
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
	for _, it := range items {
		w, h := l.menuItemSize(canvas, it)
		width = max(width, w+2)
		height += h
	}
	return width, height
}

// menuItemSize 是一项的宽高（border-box）：px-3 py-1.5，图标 16px、gap-2、文字区取标签与说明里较宽的一行，
// 快捷键提示前面有 gap-2 加 ml-2。行高是标签一行（text-sm，20px）加说明一行（text-xs，16px）。
func (l *Look) menuItemSize(canvas *gfx.Canvas, it MenuItem) (w, h float32) {
	padX, padY, gap := l.Space(3), l.Space(1.5), l.Space(2)
	labelSize, descSize := l.Rem(TextSM.Size), l.Rem(TextXS.Size)
	text := canvas.TextWidthWeight(it.Label, labelSize, WeightNormal)
	h = l.Rem(TextSM.Line)
	if it.Description != "" {
		text = max(text, canvas.TextWidthWeight(it.Description, descSize, WeightNormal))
		h += l.Rem(TextXS.Line)
	}
	w = 2*padX + text
	if it.Icon.ViewBox > 0 {
		w += l.Rem(1) + gap
	}
	if it.Shortcut != "" {
		w += gap + l.Space(2) + canvas.TextWidthWeight(it.Shortcut, descSize, WeightNormal)
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

// menuRow 声明一项（渲染层的 button[role=menuitem]）：w-full flex items-center gap-2 px-3 py-1.5 text-sm，
// 悬停 bg-accent text-accent-foreground（transition-colors，Tailwind 默认的 150 ms），危险项文字恒为 text-destructive，
// 禁用项 opacity-50 且 pointer-events-none。返回它的身份与这一帧是否被点中。
//
// 键盘聚焦环是全局的 button:focus-visible（ring-2 ring-primary/50），菜单不裁剪内容，环画在项外面、压住菜单边框。
func (l *Look) menuRow(c ui.C, it MenuItem) (gfx.ID, bool) {
	t := l.T
	id := ui.ID(c)
	opts := gfx.HitOptions{Role: gfx.RoleMenuItem, Disabled: it.Disabled}
	if !it.Disabled {
		opts.Cursor = gfx.PointerCursorHand
	}
	row := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(1.5), l.Space(3), l.Space(1.5), l.Space(3)).
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
		if it.Icon.ViewBox > 0 {
			l.Icon(c, it.Icon, l.Rem(1), fg).Shrink(0)
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
	})
	return id, row.Clicked()
}
