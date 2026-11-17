package kit

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// CloseDialog（渲染层 components/ui/CloseDialog.tsx）
//
// 关闭应用时的询问：「最小化到托盘」「退出应用」二选一，可勾「记住我的选择」。z-[9999]，与 Modal 同一层。
//
//   - 整窗遮罩 bg-background/60（淡入）与居中的面板：max-w-sm（24rem）、rounded-2xl、border-border，外观取 app-wallpaper-panel-strong；
//   - 页眉 px-6 py-4 border-b：标题 text-lg font-semibold，右端关闭钮（p-2 -mr-2 rounded-xl，图标 w-5 h-5）；
//   - 内容 p-6 space-y-4：说明（text-sm muted）、两枚选项（w-full gap-3 p-4 rounded-xl border；左边是 p-2 rounded-lg 的图标块，
//     悬停时整枚选项 bg-accent、border-{primary|destructive}/50，图标块反色），以及「记住我的选择」复选框；
//   - Esc、点遮罩、点关闭钮都是「只关对话框」（Cancel：主进程据此复位待处理的关闭动作）；选项的结果带上复选框的当前值；
//   - 每次打开都把复选框复位（渲染层 useEffect([isOpen])）；
//   - 入场 animate-in fade-in zoom-in-95 duration-base ease-enter；卸载即消失，没有退场动画（台账 A-008）；
//   - 没有焦点陷阱（渲染层不拦 Tab）；打开时焦点落在对话框容器本身（不可见），关闭后回到打开之前的控件；
//   - 遮罩后面的模糊没有（台账 A-007）。

// CloseDialogChoice 是用户在一帧里对关闭对话框做的事。
type CloseDialogChoice uint8

const (
	CloseDialogNone     CloseDialogChoice = iota
	CloseDialogMinimize                   // 点了「最小化到托盘」
	CloseDialogExit                       // 点了「退出应用」
	CloseDialogCancel                     // 点了关闭钮、点了遮罩，或按了 Esc：只关对话框，不选动作
)

// CloseDialogResult 是一帧里的结果；Remember 只在 Minimize 与 Exit 时有意义。
type CloseDialogResult struct {
	Choice   CloseDialogChoice
	Remember bool
}

// CloseDialogSpec 描述关闭对话框。Open 为假时什么都不画；文字都是调用方本地化好的。
type CloseDialogSpec struct {
	Open                                 bool
	Title, Message                       string
	MinimizeText, ExitText, RememberText string
	// CloseLabel 是页眉关闭钮的可访问名。
	CloseLabel string
}

// CloseDialogState 是关闭对话框的界面状态：由持有它的屏幕保存。每一帧都要经过 CloseDialog，
// Open 为假的那一帧由它复位（渲染层每次打开都清掉「记住我的选择」）。
type CloseDialogState struct {
	remember bool
}

// CloseDialog 声明关闭对话框并返回这一帧用户做的事。调用方在收到非 None 的结果时自己把 Open 置假。
func (l *Look) CloseDialog(c ui.C, st *CloseDialogState, spec CloseDialogSpec) CloseDialogResult {
	if !spec.Open {
		st.remember = false
		return CloseDialogResult{}
	}
	t := l.T
	result := CloseDialogResult{}

	enter := l.Motion(DurationBase, EaseEnter)
	// 没有退场动画：撤掉时按最后一帧回放的时长压到最短（enterPresence 对面板做同样的处理）。
	instant := gfx.Motion{Duration: time.Nanosecond, Easing: enter.Easing}

	var (
		scope   = ui.ID(c.Key("scope"))
		scrimID = ui.ID(c.Key("scrim"))
		radius  = l.Rem(1) // rounded-2xl
		surface = Surface{Bg: t.UiPanelStrongBg, Border: t.UiPanelStrongBorder, BorderW: 1, Radius: radius, Shadow: t.UiPanelStrongShadow}
	)
	// 整窗一层：遮罩与面板在同一个焦点作用域里（与 Modal 同一套次序），但不认领焦点陷阱——渲染层没有。
	host := ui.Overlay(c.Key("host"), ui.OverlaySpec{Band: bandModal, Bounds: c.Bounds()}).
		Justify(ui.Center).Items(ui.Center).Pad(l.Space(4)). // 外层 p-4
		Draw(func(d *ui.DrawContext) {
			d.Canvas.PushFocusScope(scope)
			l.paintModalScrim(d, scrimID, enter, instant, false)
		}).
		DrawOver(func(d *ui.DrawContext) { d.Canvas.PopFocusScope() })
	host.Children(func() {
		// 面板：border（1px）在盒内，页眉与内容区缩进一个边框宽。
		panel := ui.Column(c.Key("panel")).FillWidth().MaxWidth(l.Rem(24)).Pad(1).
			Draw(func(d *ui.DrawContext) {
				enterPresence(d.Canvas, scope, 0, enter, 0.95, center(d.Rect))
				d.Canvas.ReturnFocus(scope)
				l.PaintSurface(d, surface)
				// 面板是不透明的：按在面板上不能落到下面的遮罩（那会关闭对话框）。
				d.Canvas.Block(d.Rect)
				d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, spec.Title, gfx.HitOptions{Description: spec.Message})
			}).
			DrawOver(func(d *ui.DrawContext) { d.Canvas.EndPresence() })
		panel.Children(func() {
			if l.closeDialogHeader(c.Key("header"), spec) {
				result.Choice = CloseDialogCancel
			}
			ui.Column(c.Key("body")).FillWidth().Pad(l.Space(6)).Gap(l.Space(4)).Children(func() {
				l.Text(c.Key("message"), spec.Message, TextSM, WeightNormal, t.MutedForeground, Wrapped())
				ui.Column(c.Key("options")).FillWidth().Gap(l.Space(3)).Children(func() {
					if l.closeDialogOption(c.Key("minimize"), icons.Minus, spec.MinimizeText, t.Primary, t.PrimaryForeground) {
						result.Choice = CloseDialogMinimize
					}
					if l.closeDialogOption(c.Key("exit"), icons.LogOut, spec.ExitText, t.Destructive, t.DestructiveForeground) {
						result.Choice = CloseDialogExit
					}
				})
				l.Checkbox(c.Key("remember"), &st.remember, CheckboxSpec{Label: spec.RememberText})
			})
		})
	})

	// 点遮罩与 Esc 都以对遮罩的一次点击送达：指针点的带点击次数，Esc 的次数为 0；两者都是 Cancel（渲染层的 handleCancel）。
	if canvas := c.Canvas(); result.Choice == CloseDialogNone && canvas.Clicked(scrimID) {
		result.Choice = CloseDialogCancel
	}
	// 选项的结果带上复选框当前的值（渲染层的 handleMinimize / handleExit 读的就是 rememberChoice）。
	result.Remember = st.remember
	return result
}

// closeDialogHeader 是页眉：flex items-center justify-between px-6 py-4 border-b border-border。标题 text-lg font-semibold，
// 关闭钮 p-2 -mr-2：它的右缘比内容区的右缘（px-6）再外移 0.5rem，所以页眉的右内边距是 px-4。返回关闭钮是否被点击。
func (l *Look) closeDialogHeader(c ui.C, spec CloseDialogSpec) bool {
	t := l.T
	closed := false
	padY := l.Space(4)
	ui.Row(c).FillWidth().Shrink(0).Items(ui.Center).Justify(ui.Between).
		PadSides(padY, l.Space(4), padY+1, l.Space(6)). // 下边框 1px 在盒内
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, EdgeBottom, 1, t.Border) }).
		Children(func() {
			l.Heading(c.Key("title"), 2, spec.Title, TextLG, WeightSemibold, t.Foreground)
			if l.closeButton(c.Key("close"), spec.CloseLabel, DurationDefault) {
				closed = true
			}
		})
	return closed
}

// closeDialogOption 是一枚选项：w-full flex items-center gap-3 p-4 rounded-xl border border-border，
// hover:bg-accent hover:border-<accent>/50，transition-all（150 ms）；左边的图标块 p-2 rounded-lg，
// 平时 bg-<accent>/10 text-<accent>，随整枚选项悬停（group-hover）变成 bg-<accent> text-<accent>-foreground；文字 font-medium text-foreground。
// 返回是否被点击。
func (l *Look) closeDialogOption(c ui.C, icon gfx.Icon, label string, accent, accentFg tokens.Color) bool {
	t := l.T
	id := ui.ID(c)
	radius := l.Rem(0.75)
	row := ui.Row(c).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(3)).Pad(l.Space(4)+1).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	p := l.hoverTo(c.Canvas(), id, 0, row.State().Hovered, DurationDefault)
	row = row.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, l.ringColor(), radius)
		}
		l.PaintSurface(d, Surface{
			Bg:     t.Accent.WithAlpha(t.Accent.A * p),
			Border: MixToken(t.Border, accent.WithAlpha(0.5), p), BorderW: 1, Radius: radius,
		})
	})
	row.Children(func() {
		side := l.Rem(2.25) // p-2 + 20px 图标 + p-2
		ui.Row(c.Key("icon")).Size(side, side).Shrink(0).Items(ui.Center).Justify(ui.Center).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, Surface{Bg: MixToken(accent.WithAlpha(0.1), accent, p), Radius: radius})
			}).
			Children(func() { l.Icon(c.Key("glyph"), icon, l.Rem(1.25), MixToken(accent, accentFg, p)) })
		l.Text(c.Key("label"), label, TextBase, WeightMedium, t.Foreground)
	})
	return row.Clicked()
}
