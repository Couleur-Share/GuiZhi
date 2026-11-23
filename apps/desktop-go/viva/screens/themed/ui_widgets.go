package themed

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// 本文件是主题阅读各块界面共用的小控件。

// hitButton 把 box 登记成可点的按钮：命中区（按钮角色、手形光标）、Tab 停靠点与悬停进度。
// id 由调用方在声明盒子之前取（ui.ID 会消耗 c 上的键，盒子的身份才跟着键走）。
// 返回登记后的盒子与悬停进度（0..1，按 duration-default 过渡）。
func (s *Screen) hitButton(c ui.C, id gfx.ID, box ui.Box, name string) (ui.Box, float32) {
	l := s.Look
	box = box.HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	return box, l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
}

// hitDisclosure 是 hitButton 的折叠面板版：展开状态走 Expanded（读屏播报「已展开 / 已折叠」），不占 Selected。
func (s *Screen) hitDisclosure(c ui.C, id gfx.ID, box ui.Box, name string, open bool) (ui.Box, float32) {
	l := s.Look
	box = box.HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: open, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	return box, l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
}

// compactPrimary 是主操作的小按钮：与 compactButton 同尺寸，主色底。
func (s *Screen) compactPrimary() kit.ButtonStyle {
	t := s.Look.T
	st := s.compactButton()
	st.Fg, st.Bg, st.Border, st.BgHover = t.PrimaryForeground, t.Primary, t.Primary, t.Primary.WithAlpha(0.9)
	st.Surface = tokens.Color{}
	return st
}

// textLink 是纯文字的链接钮（text-primary，悬停加下划线）：命中区是整个盒子，键盘可达。返回这一帧是否被点了。
func (s *Screen) textLink(c ui.C, label string, style kit.TextStyle) bool {
	return s.textLinkAs(c, label, style, gfx.RoleButton)
}

// textLinkAs 是 textLink 指定可访问角色的写法：渲染层的 <a href> 是 link，<button> 是 button。
//
// boxH>0 时命中盒的高度固定为 boxH（居中在文字行里）：行内 <a> 的盒子是内容区（字号 14 时 18px），比行高（20px）矮。
func (s *Screen) textLinkAs(c ui.C, label string, style kit.TextStyle, role gfx.Role, boxH ...float32) bool {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).Shrink(0).Items(ui.Center).HitWith(id, label, gfx.HitOptions{Role: role, Cursor: gfx.PointerCursorHand})
	if len(boxH) > 0 && boxH[0] > 0 {
		box = box.Height(boxH[0])
	}
	l.TabStop(box)
	hovered := box.State().Hovered
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.25))
		}
	}).Children(func() {
		l.Text(c.Key("text"), label, style, kit.WeightNormal, t.Primary, kit.Underline(hovered))
	})
	return box.Clicked()
}

// alertLine 是一行带直播属性（role=alert）的危险色文字：失败原因，折行显示。
func (s *Screen) alertLine(c ui.C, text string) {
	l := s.Look
	l.Text(c, text, kit.TextSM, kit.WeightNormal, l.T.Destructive, kit.Wrapped()).
		Draw(func(d *ui.DrawContext) {
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
		})
}

// statusLine 是一行带直播属性（role=status）的弱化色文字：进行中的说明。
func (s *Screen) statusLine(c ui.C, text string) {
	l := s.Look
	l.Text(c, text, kit.TextSM, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped()).
		Draw(func(d *ui.DrawContext) {
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LivePolite})
		})
}

// bodyText 是对话框与卡片里的一段文字：text-sm，折行。
func (s *Screen) bodyText(c ui.C, text string, color tokens.Color) {
	s.Look.Text(c, text, kit.TextSM, kit.WeightNormal, color, kit.Wrapped())
}

// smallText 是 text-xs 的弱化说明，折行。
func (s *Screen) smallText(c ui.C, text string) ui.Box {
	l := s.Look
	return l.Text(c, text, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
}

// uiPrimaryStyle 是通用按钮（components/ui/Button.tsx）的 primary 变体、md 档。
func (s *Screen) uiPrimaryStyle() kit.ButtonStyle {
	t := s.Look.T
	st := s.Look.UIGhostButton(kit.UIButtonMD)
	st.Fg, st.Bg, st.BgHover = t.PrimaryForeground, t.Primary, t.Primary
	st.Ring = t.Primary
	return st
}

// compactButton 是工具栏与卡片里的小按钮（渲染层 themeButton）：min-h-7 rounded-lg border border-border px-2.5 py-1 text-xs hover:bg-accent。
func (s *Screen) compactButton() kit.ButtonStyle {
	l := s.Look
	t := l.T
	return kit.ButtonStyle{
		Height: l.Rem(1.75), PadX: l.Space(2.5), Radius: l.Rem(0.75), Gap: l.Space(1), Text: kit.TextXS,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Accent,
		Transition: kit.DurationNone, Surface: t.UiPanelBg,
	}
}

// card 是带边框的圆角块（rounded-lg border border-border p-3）。
func (s *Screen) card(c ui.C, body func()) {
	s.cardWith(c, s.Look.Space(3)+1, s.Look.Space(2), body)
}

// cardWith 是 card 的内边距（含边框）与子项间距可调的写法：生成设置里的块是 p-4 / space-y-3。
func (s *Screen) cardWith(c ui.C, pad, gap float32, body func()) {
	l := s.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(gap).Pad(pad).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(body)
}

// relaxedText 是带 leading-relaxed（行高 1.625 倍字号）的折行文字。
func (s *Screen) relaxedText(c ui.C, text string, st kit.TextStyle, color tokens.Color, opts ...kit.TextOpt) ui.Box {
	st.Line = st.Size * 1.625
	return s.Look.Text(c, text, st, kit.WeightNormal, color, append([]kit.TextOpt{kit.Wrapped()}, opts...)...)
}
