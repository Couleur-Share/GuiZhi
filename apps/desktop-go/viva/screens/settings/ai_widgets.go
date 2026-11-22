package settings

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// 「AI 模型服务」分区共用的小零件：文本框、品牌字母徽标、状态点、胶囊徽标、小图标按钮与分隔线。
// 外观取自渲染层 components/settings/ai-workbench 里的 Tailwind 类。

// aiTone 是端点状态的色调（EndpointStatus.tone）。
type aiTone uint8

const (
	toneReady aiTone = iota
	toneWarning
	toneError
)

// toneColor 是状态点的颜色：emerald-500 / amber-500 / red-500。
func toneColor(tone aiTone) tokens.Color {
	switch tone {
	case toneReady:
		return tokens.Tailwind("emerald-500")
	case toneError:
		return tokens.Tailwind("red-500")
	}
	return tokens.Tailwind("amber-500")
}

// aiFieldSpec 描述一个 AI 分区里的文本框；各处的外观差别（底色、边框、圆角、高度、内边距）都在这里。
type aiFieldSpec struct {
	Name, Placeholder string
	Height            float32
	Radius            float32
	Bg, Border        tokens.Color
	BorderW           float32
	Left, Right       float32 // 文字区内边距（不含边框）
	Size              float32 // 字号（逻辑像素）；0 取 0.875rem
	Mono              bool
	PlaceholderInk    tokens.Color
	Overlay           func(c ui.C)
	Grow              bool
}

// aiField 声明一个文本框（Look.Input 加表面与聚焦环），返回里面的输入元素。
func aiField(l *kit.Look, c ui.C, f *ui.Field, s aiFieldSpec) ui.Box {
	t := l.T
	size := s.Size
	if size == 0 {
		size = l.Rem(0.875)
	}
	surface := kit.Surface{Bg: s.Bg, Border: s.Border, BorderW: s.BorderW, Radius: s.Radius}
	left, right := s.Left+s.BorderW, s.Right+s.BorderW
	name := s.Name
	if name == "" {
		name = s.Placeholder
	}
	wrap := ui.Stack(c).Height(s.Height)
	if s.Grow {
		wrap = wrap.Grow(1).Shrink(1).Basis(0).MinWidth(0)
	} else {
		wrap = wrap.FillWidth()
	}
	var box ui.Box
	var ring float32
	wrap.Children(func() {
		anim := ui.ID(c.Key("ring"))
		ui.Row(c.Key("surface")).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
			l.PaintInputRing(d, ring, surface.Radius)
			l.PaintSurface(d, surface)
		})
		box = l.Input(c.Key("input"), f, name, s.Placeholder, kit.InputSpec{
			Size: size, Weight: kit.WeightNormal, Color: t.Foreground, Height: s.Height, Left: left, Right: right,
			Placeholder: s.PlaceholderInk, Mono: s.Mono,
		}).Absolute().Inset(0)
		ring = l.InputRing(c, anim, box.State().Focused)
		if s.Overlay != nil {
			s.Overlay(c)
		}
	})
	return box
}

// modalField 是弹窗表单的文本框（`h-10 w-full rounded-lg bg-muted px-3 text-sm`）。
func modalField(l *kit.Look, c ui.C, f *ui.Field, name, placeholder string) ui.Box {
	return aiField(l, c, f, aiFieldSpec{
		Name: name, Placeholder: placeholder, Height: l.Rem(2.5), Radius: l.Rem(0.75),
		Bg: l.T.Muted, Left: l.Space(3), Right: l.Space(3), PlaceholderInk: l.T.MutedForeground,
	})
}

// lockedField 是被锁定的文本框（disabled:opacity-60）：只读显示当前值，不可编辑也不进 Tab 顺序。
func lockedField(l *kit.Look, c ui.C, text string) {
	t := l.T
	ui.Row(c).FillWidth().Height(l.Rem(2.5)).Items(ui.Center).PadSides(0, l.Space(3), 0, l.Space(3)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.6), Radius: l.Rem(0.75)})
		}).Children(func() {
		l.Text(c.Key("text"), text, kit.TextSM, kit.WeightNormal, t.Foreground.WithAlpha(0.6), kit.Ellipsis()).MinWidth(0)
	})
}

// fieldLabel 是表单项的标签（`mb-1 block text-xs text-muted-foreground`）。
func fieldLabel(l *kit.Look, c ui.C, text string) {
	l.Text(c, text, kit.TextXS, kit.WeightNormal, l.T.MutedForeground).MarginBottom(l.Space(1))
}

// brandBadge 是品牌图标的替身：渲染层按供应商或模型家族画 15 套品牌 SVG（<img>，有 alt 但不是文本节点），
// Viva 没有这批图标，只画底色徽标加一个居中的实心圆点。圆点不是文字：换成首字母会多出一个文本节点，
// 把后面的文字序列整体错位（M3-2b 实测，AI 页 L1 一次错位 41 处）。
// category 与 fontPx 暂不参与绘制，留给补齐品牌图标时按分类选图。
func brandBadge(l *kit.Look, c ui.C, category string, box float32, bg, fg tokens.Color, radius float32, fontPx float32) {
	_, _ = category, fontPx
	badge := ui.Row(c).Size(box, box).Shrink(0).Items(ui.Center).Justify(ui.Center).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: radius}) })
	badge.Children(func() { statusDot(l, c.Key("mark"), box/2, fg, 0, 0) })
}

// statusDot 是状态点：size 的圆，ringW 大于 0 时外面带一圈同色半透明的环（ring-[3px]）。
func statusDot(l *kit.Look, c ui.C, size float32, color tokens.Color, ringW float32, ringAlpha float32) {
	ui.Row(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		if ringW > 0 {
			l.PaintRing(d, ringW, color.WithAlpha(ringAlpha), size/2)
		}
		l.PaintSurface(d, kit.Surface{Bg: color, Radius: size / 2})
	})
}

// iconBadge 是模型行里的能力小图标（h-7 w-7 rounded-md）：主色徽标用 primary/10 底，其余是描边的弱化色。悬停出说明。
func iconBadge(l *kit.Look, c ui.C, ic gfx.Icon, label string, primary bool) {
	t := l.T
	surface := kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.5)}
	fg := t.MutedForeground
	if primary {
		surface = kit.Surface{Bg: t.Primary.WithAlpha(0.1), Radius: l.Rem(0.5)}
		fg = t.Primary
	}
	size := l.Rem(1.75)
	box := ui.Row(c).Size(size, size).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(ui.ID(c), label, gfx.HitOptions{Hint: label}).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) })
	box.Children(func() { l.Icon(c.Key("icon"), ic, l.Rem(0.875), fg) })
}

// routeBadge 是模型行里的路由徽标（`h-7 rounded-md bg-primary/10 px-2 text-[11px] font-medium text-primary`）。
func routeBadge(l *kit.Look, c ui.C, label string) {
	t := l.T
	ui.Row(c).Height(l.Rem(1.75)).Shrink(0).Items(ui.Center).PadSides(0, l.Space(2), 0, l.Space(2)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Radius: l.Rem(0.5)})
		}).
		Children(func() { l.TextPx(c.Key("label"), label, 11, 16.5, kit.WeightMedium, t.Primary) })
}

// smallIconBtn 是模型行与端点头部的小图标按钮：size 边长，悬停底色 muted，可带描边与悬停色。
// 禁用的按钮不派发指针事件，提示由外层承接（hint 挂在这里，禁用时仍可悬停出说明）。
func smallIconBtn(l *kit.Look, c ui.C, ic gfx.Icon, name, hint string, size float32, disabled bool, st kit.ButtonStyle) bool {
	spec := kit.ButtonSpec{Name: name, Hint: hint, Icon: ic, IconSize: l.Rem(0.875), Center: true, Disabled: disabled}
	st.Height, st.Width = size, size
	if st.Radius == 0 {
		st.Radius = l.Rem(0.5)
	}
	st.Transition = kit.DurationDefault
	st.DisabledOpacity = 0.4
	return l.Button(c, spec, st).Clicked()
}

// sectionDivider 是侧栏里的分组标题：左边文字 text-[12px] muted，右边 h-px 的分隔线占满。
func sectionDivider(l *kit.Look, c ui.C, label string, marginTop, marginBottom float32) {
	t := l.T
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
		l.TextPx(c.Key("label"), label, 12, 18, kit.WeightNormal, t.MutedForeground).Shrink(0)
		ui.Row(c.Key("line")).Grow(1).Height(1).Draw(func(d *ui.DrawContext) { d.Canvas.Fill(d.Rect, d.Fade(l.Col(t.Border))) })
	}).MarginTop(marginTop).MarginBottom(marginBottom)
}

// eyeToggle 是口令框右侧的「显示/隐藏」小钮（`h-7 w-7 rounded-md`，图标 1rem，悬停底色 muted）。
func eyeToggle(l *kit.Look, c ui.C, show *bool, showName, hideName string, size float32) ui.Box {
	name, icon := showName, icons.Eye
	if *show {
		name, icon = hideName, icons.EyeOff
	}
	st := kit.ButtonStyle{Height: size, Width: size, Radius: l.Rem(0.375), Fg: l.T.MutedForeground, FgHover: l.T.Foreground, BgHover: l.T.Muted, Transition: kit.DurationDefault}
	box := l.Button(c, kit.ButtonSpec{Name: name, Hint: name, Icon: icon, IconSize: l.Rem(1), Center: true}, st)
	if box.Clicked() {
		*show = !*show
	}
	return box
}
