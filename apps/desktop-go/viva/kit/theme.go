package kit

import "github.com/vivaui/viva/gfx"

// themeOf 把渲染层语义令牌映射成 Viva 的 gfx.Theme：Viva 自带控件（滚动条、焦点环、弹层等）读它的默认外观；
// 本包自己复刻的控件直接读 Look.T，保证每个值都是渲染层 globals.css 的求值结果。
func themeOf(l *Look) gfx.Theme {
	t := l.T
	th := gfx.LightTheme()
	if l.Dark {
		th = gfx.DarkTheme()
	}
	th.Background = gfxColor(t.Background)
	th.Foreground = gfxColor(t.Foreground)
	th.Card, th.CardForeground, th.CardElevated = gfxColor(t.Card), gfxColor(t.CardForeground), gfxColor(t.Card)
	th.Popover, th.PopoverForeground = gfxColor(t.Popover), gfxColor(t.PopoverForeground)
	th.Muted, th.MutedForeground = gfxColor(t.Muted), gfxColor(t.MutedForeground)
	th.Border = gfxColor(t.Border)
	th.Hover = gfxColor(t.Accent)
	th.Input = gfxColor(t.Input)
	th.Primary, th.PrimaryForeground = gfxColor(t.Primary), gfxColor(t.PrimaryForeground)
	th.Secondary, th.SecondaryForeground = gfxColor(t.Secondary), gfxColor(t.SecondaryForeground)
	th.Accent, th.AccentForeground = gfxColor(t.Accent), gfxColor(t.AccentForeground)
	th.Ring = gfxColor(t.Ring)
	th.Danger, th.DangerForeground = gfxColor(t.Destructive), gfxColor(t.DestructiveForeground)
	// 渲染层的 ::selection 是 primary/20，文字色不变。
	th.Selection, th.SelectionForeground = gfxColor(t.Primary).WithAlpha(0.2), gfxColor(t.Foreground)
	th.Metrics = metricsOf(l)
	return th
}

// metricsOf 是 Tailwind 默认尺度在当前根字号下的值：控件高 2.5rem、控件圆角 0.5rem、卡片圆角 0.75rem、
// 字号 0.75/0.875/1.125rem。具体控件的几何不读这里，按渲染层各组件的类名单独给。
func metricsOf(l *Look) gfx.Metrics {
	return gfx.Metrics{
		ControlHeight: l.Rem(2.5),
		RadiusControl: l.Rem(0.5),
		RadiusSurface: l.Rem(0.75),
		FontSizeS:     l.Rem(0.75),
		FontSizeM:     l.Rem(0.875),
		FontSizeL:     l.Rem(1.125),
		PadControlX:   l.Rem(0.75),
		PadSurface:    l.Rem(1),
		GapItem:       l.Rem(0.5),
	}
}
