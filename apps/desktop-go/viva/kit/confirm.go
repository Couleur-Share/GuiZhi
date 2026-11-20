package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// ConfirmSpec 描述一个确认对话框（渲染层 components/ui/ConfirmDialog.tsx 的属性）。Open 为假时什么都不画。
type ConfirmSpec struct {
	Open  bool
	Title string // 可为空：无标题时可访问名取确认按钮的文字
	// Message 是说明文字，居中、可折行。
	Message string
	// Body 非空时代替 Message 声明说明区：渲染层的 message 可以是任意节点（彻底删除的确认框里是两段话加一个复选框，
	// 外层 `space-y-3 text-left`）。Body 在一个撑满宽度、子项间距 12px、左对齐的竖排里声明；这个竖排仍是 mb-6 的说明区。
	Body func(c ui.C)
	// Description 是对话框的可访问描述（aria-describedby 指向的说明区的文字）；空则取 Message。给了 Body 时应当给它。
	Description string
	// ConfirmText、CancelText 缺省时与渲染层一样是英文的 "Confirm"、"Cancel"；调用方应当总是传已本地化的文字。
	ConfirmText, CancelText string
	// Destructive 为真时顶部多一个红色警示圈，确认钮是红色（渲染层 variant="destructive"）。
	Destructive bool
	// Loading 为真时确认钮转圈、两枚按钮都禁用，Esc 与 Enter 不再起作用（点遮罩仍然关闭——渲染层没有禁它）。
	Loading bool
}

// ConfirmState 是确认对话框的界面状态：由持有它的屏幕保存。每一帧都要经过 ConfirmDialog，
// Open 为假的那一帧由它负责复位，所以对话框不必由调用方按条件声明。
type ConfirmState struct {
	focused bool // 已经把键盘交给取消按钮：打开时只请求一次，不在用户挪动焦点之后再拉回去
}

// ConfirmResult 是一帧里用户对对话框做的事。
type ConfirmResult uint8

const (
	ConfirmNone      ConfirmResult = iota
	ConfirmAccepted                // 点了确认，或按了 Enter
	ConfirmDismissed               // 点了取消、点了遮罩，或按了 Esc
)

// ConfirmDialog 是模态确认对话框（渲染层 ConfirmDialog，z-[99999]）：整窗的半透明遮罩加居中的面板。
//
// 行为照渲染层：
//   - 打开后键盘落在「取消」按钮上（防止误操作；渲染层是 50 ms 后聚焦，这里在打开的那一帧请求，差别只在 50 ms 内的按键），
//     关闭时焦点回到打开之前的控件；
//   - Esc 关闭、Enter 无论焦点在哪都确认（渲染层是 document 级的按键监听，取消按钮有焦点时按 Enter 也是确认）；
//     加载中两者都不响应；
//   - 没有 Tab 陷阱：渲染层不拦 Tab，焦点可以走到遮罩后面的页面（这是渲染层的现状，不改）；
//   - 只有入场动画（淡入 + 从 95% 放大，180 ms），关闭即消失（台账 A-008）；
//   - 遮罩后面的模糊没有（台账 A-007）。
//
// 面板外观取 app-wallpaper-panel-strong：令牌背景、令牌边框、令牌阴影，它们带 !important，盖掉了 shadow-2xl 与 border-border。
func (l *Look) ConfirmDialog(c ui.C, st *ConfirmState, spec ConfirmSpec) ConfirmResult {
	if !spec.Open {
		st.focused = false
		return ConfirmNone
	}
	t := l.T
	result := ConfirmNone
	confirmText, cancelText := spec.ConfirmText, spec.CancelText
	if confirmText == "" {
		confirmText = "Confirm"
	}
	if cancelText == "" {
		cancelText = "Cancel"
	}

	scrim := t.Background.WithAlpha(0.6)
	if l.dismissLayer(c.Key("scrim"), dismissSpec{Band: bandConfirm, Scrim: &scrim, NoEscape: spec.Loading}) {
		result = ConfirmDismissed
	}

	name := spec.Title
	if name == "" {
		name = confirmText
	}
	description := spec.Description
	if description == "" {
		description = spec.Message
	}
	// ui.Key 只作用于紧接着的一次声明：每个键取出来就立刻交给 ui.ID 或节点，不要先存起来再用。
	// 两枚按钮的 ID 在面板里面取（身份挂在面板的作用域下，Box.Shortcut 才认得「焦点在面板里」）；
	// 面板的 Draw 在整棵树声明完之后才执行，那时 cancelID 已经有值。
	var cancelID, confirmID gfx.ID
	var (
		scope     = ui.ID(c.Key("scope"))
		radius    = l.Rem(0.75) // rounded-xl
		surface   = Surface{Bg: t.UiPanelStrongBg, Border: t.UiPanelStrongBorder, BorderW: 1, Radius: radius, Shadow: t.UiPanelStrongShadow}
		motion    = l.Motion(DurationBase, EaseDefault)
		maxWidth  = l.Rem(24)      // max-w-sm
		panelPad  = 1 + l.Space(6) // border + p-6
		hostInset = l.Space(4)     // 外层 p-4：窗口很窄时面板离窗口边至少这么远
	)
	host := ui.Overlay(c.Key("host"), ui.OverlaySpec{Band: bandConfirm, Bounds: c.Bounds()}).
		Justify(ui.Center).Items(ui.Center).Pad(hostInset)
	host.Children(func() {
		// 面板是带键的容器：Box.Shortcut 靠它界定「焦点在面板里」，不需要给面板自己一个命中区（那会在无障碍树里多出一个空按钮）。
		panel := ui.Column(c.Key("panel")).FillWidth().MaxWidth(maxWidth).Pad(panelPad).
			Draw(func(d *ui.DrawContext) {
				enterPresence(d.Canvas, scope, 0, motion, 0.95, center(d.Rect))
				d.Canvas.PushFocusScope(scope)
				d.Canvas.ReturnFocus(scope)
				if !st.focused {
					d.Canvas.RequestFocus(cancelID)
					st.focused = true
				}
				l.PaintSurface(d, surface)
				// 面板是不透明的：按在面板边距或说明文字上不能落到下面的遮罩（那会关闭对话框）。
				d.Canvas.Block(d.Rect)
				d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, name, gfx.HitOptions{Description: description})
			}).
			DrawOver(func(d *ui.DrawContext) {
				d.Canvas.PopFocusScope()
				d.Canvas.EndPresence()
			})
		if !spec.Loading && panel.Shortcut(0, "enter") {
			result = ConfirmAccepted
		}
		panel.Children(func() {
			if spec.Destructive {
				l.dialogIcon(c.Key("icon"), icons.TriangleAlert, tokens.Tailwind("red-100"), tokens.Tailwind("red-900").WithAlpha(0.3),
					tokens.Tailwind("red-600"), tokens.Tailwind("red-400"))
			}
			if spec.Title != "" {
				l.Heading(c.Key("title"), 3, spec.Title, TextLG, WeightSemibold, t.Foreground, Centered(), Wrapped()).MarginBottom(l.Space(2))
			}
			if spec.Body != nil {
				// 说明区里是一个 space-y-3 text-left 的块：段落靠左，不再居中。
				ui.Column(c.Key("message")).FillWidth().Gap(l.Space(3)).MarginBottom(l.Space(6)).Children(func() { spec.Body(c.Key("body")) })
			} else {
				l.Text(c.Key("message"), spec.Message, TextSM, WeightNormal, t.MutedForeground, Centered(), Wrapped()).MarginBottom(l.Space(6))
			}
			ui.Row(c.Key("buttons")).FillWidth().Gap(l.Space(3)).Children(func() {
				cancelID, confirmID = ui.ID(c.Key("cancel")), ui.ID(c.Key("confirm"))
				if l.dialogButton(c.Key("cancel-button"), cancelID, cancelText, l.cancelButtonStyle(), spec.Loading, false, 0.5, t.UiPanelStrongBg).Clicked() {
					result = ConfirmDismissed
				}
				confirm := l.confirmButtonStyle(spec.Destructive)
				if l.dialogButton(c.Key("confirm-button"), confirmID, confirmText, confirm, spec.Loading, spec.Loading, 0.6, t.UiPanelStrongBg).Clicked() {
					result = ConfirmAccepted
				}
			})
		})
	})
	return result
}

// dialogIcon 是对话框顶部的警示圈：w-12 h-12 的圆，里面 w-6 h-6 的图标，居中，下边距 mb-4。
// 浅色与深色下的底色、图标色不同（dark:bg-*-900/30、dark:text-*-400），都是 Tailwind 色阶，不是主题令牌。
func (l *Look) dialogIcon(c ui.C, ic gfx.Icon, lightBg, darkBg, lightFg, darkFg tokens.Color) {
	bg, fg := lightBg, lightFg
	if l.Dark {
		bg, fg = darkBg, darkFg
	}
	disc := l.Rem(3)
	ui.Row(c).FillWidth().Justify(ui.Center).MarginBottom(l.Space(4)).Children(func() {
		ui.Row(c.Key("disc")).Size(disc, disc).Shrink(0).Justify(ui.Center).Items(ui.Center).
			Draw(func(d *ui.DrawContext) {
				d.Canvas.Rounded(SnapRect(d.Rect, d.Canvas.PixelScale()), disc/2, d.Fade(l.Col(bg)))
			}).
			Children(func() { l.Icon(c, ic, l.Rem(1.5), fg) })
	})
}

// dialogButtonStyle 是对话框按钮在各状态下的外观（渲染层 className 里的取值）。
type dialogButtonStyle struct {
	Bg, BgHover tokens.Color // 静止与悬停的底色；两者相等表示悬停没有变化
	Border      tokens.Color
	BorderW     float32
	Shadow      tokens.Shadow
	Fg          tokens.Color
	Ring        bool // 键盘聚焦时是否画 ring-2 ring-primary/50
}

// cancelButtonStyle 是「取消」：border border-border app-wallpaper-surface hover:bg-accent。
// app-wallpaper-surface 的 background、border-color、box-shadow 都带 !important：它盖掉了 hover:bg-accent（悬停没有变化）、
// border-border，也盖掉了全局 button:focus-visible 的 ring（ring 是 box-shadow 实现的），所以键盘聚焦时也没有环。
func (l *Look) cancelButtonStyle() dialogButtonStyle {
	t := l.T
	return dialogButtonStyle{
		Bg: t.UiSurfaceBg, BgHover: t.UiSurfaceBg,
		Border: t.UiSurfaceBorder, BorderW: 1, Shadow: t.UiSurfaceShadow,
		Fg: t.Foreground,
	}
}

// confirmButtonStyle 是「确认」：text-white，默认 bg-primary hover:bg-primary/90，危险 bg-red-600 hover:bg-red-700。
func (l *Look) confirmButtonStyle(destructive bool) dialogButtonStyle {
	t := l.T
	if destructive {
		return dialogButtonStyle{Bg: tokens.Tailwind("red-600"), BgHover: tokens.Tailwind("red-700"), Fg: tokens.White, Ring: true}
	}
	return dialogButtonStyle{Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Fg: tokens.White, Ring: true}
}

// dialogButton 是对话框底部的一枚按钮：flex-1 h-10 px-4 rounded-lg text-sm font-medium，transition-colors（Tailwind 默认 150 ms）。
// disabled 时降低不透明度（渲染层 disabled:opacity-*）；spinner 为真时文字前有一枚转圈（gap-2）。
// surface 是面板色（对话框面板的底），禁用时文字按整层淡出算（disabledInk，台账 C-008 / B-058）。
func (l *Look) dialogButton(c ui.C, id gfx.ID, label string, st dialogButtonStyle, disabled, spinner bool, disabledOpacity float32, surface tokens.Color) ui.Box {
	radius := l.Rem(0.75) // rounded-lg
	padX := l.Space(4)    // px-4
	// flex-1 的基准尺寸是 0%，但 border-box 下一个元素的外尺寸不能小于它的内边距加边框：
	// 取消（有 1px 边框）的基准是 34，确认（无边框）是 32，空闲空间再平分，所以两枚按钮差 2px（162 / 160），不是等宽。
	row := ui.Row(c).Height(l.Rem(2.5)).Grow(1).Shrink(1).Basis(2*padX+2*st.BorderW).
		Items(ui.Center).Justify(ui.Center).Gap(l.Rem(0.5)).PadX(padX).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: disabled, Cursor: gfx.PointerCursorHand}).
		Draw(func(d *ui.DrawContext) {
			p := l.hoverTo(d.Canvas, id, 0, d.State.Hovered && !disabled, DurationDefault)
			if st.Ring && d.State.FocusVisible {
				l.PaintRing(d, 2, l.T.Primary.WithAlpha(0.5), radius)
			}
			bg, border := MixToken(st.Bg, st.BgHover, p), st.Border
			if disabled {
				bg, border = fade(bg, disabledOpacity), fade(border, disabledOpacity)
			}
			l.PaintSurface(d, Surface{Bg: bg, Border: border, BorderW: st.BorderW, Radius: radius, Shadow: st.Shadow})
		})
	return row.Children(func() {
		fg := st.Fg
		if disabled {
			fg = disabledInk(st.Fg, st.Bg, disabledOpacity, surface)
		}
		if spinner {
			l.Spinner(c.Key("spinner"), l.Rem(1), fg)
		}
		l.Text(c.Key("label"), label, TextSM, WeightMedium, fg)
	})
}

// fade 把令牌颜色的透明度乘以 factor（渲染层的 opacity-50 之类作用在整个元素上，这里逐个图元乘）。
func fade(c tokens.Color, factor float32) tokens.Color { return c.WithAlpha(c.A * factor) }
