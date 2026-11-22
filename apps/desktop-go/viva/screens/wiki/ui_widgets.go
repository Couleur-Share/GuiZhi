package wiki

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 渲染层 Tailwind 的圆角：rounded-lg 是 --radius（0.75rem），rounded-md 与 rounded-sm 依次小 2px 与 4px。
func (s *Screen) radiusLG() float32 { return s.Look.Rem(0.75) }
func (s *Screen) radiusMD() float32 { return s.Look.Rem(0.75) - 2 }
func (s *Screen) radiusSM() float32 { return s.Look.Rem(0.75) - 4 }

// 渲染层任意 px 字号（text-[10px]、text-[11px]）不随字号档缩放，行高是 Tailwind 预检样式给的 1.5 倍。
const (
	px10Line = 15
	px11Line = 16.5
)

// kindLabel 是页面类型的名称（WikiKindBadge）；未知类型归入主题，与渲染层的回退一致。
func kindLabel(kind string) string {
	switch kind {
	case services.WikiKindEntity:
		return locales.WikiKindEntity()
	case services.WikiKindConcept:
		return locales.WikiKindConcept()
	}
	return locales.WikiKindTopic()
}

// kindIcon 是侧栏里类型筛选的图标。
func kindIcon(f CatalogFilter) gfx.Icon {
	switch f {
	case FilterTopic:
		return icons.List
	case FilterEntity:
		return icons.Box
	case FilterConcept:
		return icons.Lightbulb
	case FilterOrphan:
		return icons.Unlink
	case FilterManual:
		return icons.Pencil
	}
	return icons.Layers
}

// kindBadge 是类型徽标（WikiKindBadge）：inline-flex shrink-0 rounded-full bg-muted/70 px-1.5 py-0.5 text-[10px] text-muted-foreground。
func (s *Screen) kindBadge(c ui.C, kind string) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("kind")).Shrink(0).Items(ui.Center).
		PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.7), Radius: 9999})
		}).Children(func() {
		l.TextPx(c.Key("label"), kindLabel(kind), 10, px10Line, kit.WeightNormal, t.MutedForeground)
	})
}

// hitButton 把 box 登记成可点的按钮：命中区（按钮角色、手形光标、可选的悬停提示）、Tab 停靠点与悬停进度。
// id 由调用方在声明盒子之前取（ui.ID 会消耗 c 上的键，盒子的身份才跟着键走）。selected 对应 aria-pressed。
// 返回登记后的盒子与悬停进度（0..1，按 duration-default 过渡）。
func (s *Screen) hitButton(c ui.C, id gfx.ID, box ui.Box, name, hint string, selected bool) (ui.Box, float32) {
	l := s.Look
	opts := gfx.HitOptions{Role: gfx.RoleButton, Selected: selected, Hint: hint, Cursor: gfx.PointerCursorHand}
	if hint != "" {
		opts.Description = l.HintDescription(id, hint)
	}
	box = box.HitWith(id, name, opts)
	l.TabStop(box)
	return box, l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
}

// textLink 是纯文字的链接钮（text-primary，悬停加下划线；always 为真时一直带下划线）：命中区是整个盒子，键盘可达。
// 返回这一帧是否被点了。
func (s *Screen) textLink(c ui.C, label string, style kit.TextStyle, always bool) bool {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	box, _ := s.hitButton(c, id, ui.Row(c).Shrink(0).Items(ui.Center), label, "", false)
	hovered := box.State().Hovered
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), s.radiusSM())
		}
	}).Children(func() {
		l.Text(c.Key("text"), label, style, kit.WeightNormal, t.Primary, kit.Underline(hovered || always))
	})
	return box.Clicked()
}

// chipButton 是详情页里的相关页面小块（RelatedChips）：inline-flex max-w-56 rounded-md border border-border/70 bg-background/60
// px-1.5 py-0.5 text-[11px] text-foreground，悬停边框 primary/40、底色 primary/5。返回这一帧是否被点了。
func (s *Screen) chipButton(c ui.C, label string) bool {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).Shrink(0).MaxWidth(l.Rem(14)).Items(ui.Center).
		PadSides(l.Space(0.5)+1, l.Space(1.5)+1, l.Space(0.5)+1, l.Space(1.5)+1)
	box, hover := s.hitButton(c, id, box, label, "", false)
	bg := kit.MixToken(t.Background.WithAlpha(0.6), t.Primary.WithAlpha(0.05), hover)
	border := kit.MixToken(t.Border.WithAlpha(0.7), t.Primary.WithAlpha(0.4), hover)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), s.radiusMD())
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: s.radiusMD()})
	}).Children(func() {
		l.TextPx(c.Key("label"), label, 11, px11Line, kit.WeightNormal, t.Foreground, kit.Ellipsis()).Shrink(1).MinWidth(0)
	})
	return box.Clicked()
}

// uiPrimaryStyle 是通用按钮（components/ui/Button.tsx）的 primary 变体、md 档：h-10 px-5 text-sm font-semibold rounded-xl
// bg-primary text-primary-foreground focus:ring-primary。与渲染层的差异：没有 shadow-sm 的主色阴影与悬停时的放大（kit 的通用按钮
// 只做了 ghost 与 secondary，见 uibutton.go 里的台账 B-039 说明）。
func (s *Screen) uiPrimaryStyle() kit.ButtonStyle {
	t := s.Look.T
	st := s.Look.UIGhostButton(kit.UIButtonMD)
	st.Fg, st.Bg, st.BgHover = t.PrimaryForeground, t.Primary, t.Primary
	st.Ring = t.Primary
	return st
}

// rotIcon 画一枚旋转了 angle 弧度的图标（展开箭头的 rotate transition）。
func (s *Screen) rotIcon(c ui.C, ic gfx.Icon, size float32, color tokens.Color, angle float32) {
	l := s.Look
	col := l.Col(color)
	ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		tint := d.Fade(col)
		if angle == 0 {
			kit.PaintIcon(d.Canvas, ic, d.Rect, tint)
			return
		}
		d.Canvas.DrawIconTransformed(ic, d.Rect, tint, gfx.IconTransform{Rotate: angle})
	})
}

// ghostFg 是纯图标或次要文字的前景：muted-foreground，悬停 foreground（transition-colors）。
func (s *Screen) ghostFg(hover float32) tokens.Color {
	t := s.Look.T
	return kit.MixToken(t.MutedForeground, t.Foreground, hover)
}
