package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// white 是渲染层的 bg-white（开关旋钮选中态）。
var white = tokens.RGBA(255, 255, 255, 1)

// Switch 复刻渲染层设置页的 ToggleSwitch（默认尺寸）：h-7 w-12 border-2 的全圆角按钮，
// 旋钮 h-5 w-5，top-0.5，left 为 2px / 22px（px 字面量，不随字号缩放）。
//
//	选中：bg-primary border-primary，旋钮白；
//	未选中：bg-muted border-border，旋钮 muted-foreground/50；深色下 bg-primary/20 border-primary/40，旋钮 primary/60。
//
// *on 在点击时由 Viva 写回；返回的 Box 的 Clicked/Changed 照常可用。
func (l *Look) Switch(c ui.C, on *bool, label string) ui.Box {
	t := l.T
	checked := *on
	var fill, border, knob tokens.Color
	switch {
	case checked:
		fill, border, knob = t.Primary, t.Primary, white
	case l.Dark:
		fill, border, knob = t.Primary.WithAlpha(0.2), t.Primary.WithAlpha(0.4), t.Primary.WithAlpha(0.6)
	default:
		fill, border, knob = t.Muted, t.Border, t.MutedForeground.WithAlpha(0.5)
	}
	width, height := l.Rem(3), l.Rem(1.75)
	diameter, top := l.Rem(1.25), l.Rem(0.125)
	left := float32(2)
	if checked {
		left = 22
	}
	return ui.SwitchBase(c, on, label).
		Size(width, height).
		HitOutset(0, 0, 0, 0).
		Radius(height/2).
		Background(l.Col(fill)).
		Border(2, l.Col(border)).
		Draw(func(d *ui.DrawContext) {
			// 旋钮相对按钮的内边距盒（边框之内）定位：border-2 + top / left
			bounds := gfx.Rect{X: d.Rect.X + 2 + left, Y: d.Rect.Y + 2 + top, Width: diameter, Height: diameter}
			d.Canvas.Rounded(bounds, diameter/2, d.Fade(l.Col(knob)))
		})
}
