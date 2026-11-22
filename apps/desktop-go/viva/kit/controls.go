package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// white 是渲染层的 bg-white（开关旋钮选中态）。
var white = tokens.RGBA(255, 255, 255, 1)

// SwitchSpec 描述一枚开关。
type SwitchSpec struct {
	// Label 是可访问名（渲染层的 aria-label）。
	Label string
	// Disabled 对应 `disabled`：整枚开关 opacity-50，点击与键盘都不起作用。
	Disabled bool
	// Compact 是紧凑尺寸（ToggleSwitch size="compact"）：h-5 w-9 border，旋钮 h-4 w-4，top 1px，left 为 1px / 17px（px 字面量）。
	Compact bool
}

// Switch 复刻渲染层设置页的 ToggleSwitch（默认尺寸）：h-7 w-12 border-2 的全圆角按钮，
// 旋钮 h-5 w-5，top-0.5，left 为 2px / 22px（px 字面量，不随字号缩放）。
//
//	选中：bg-primary border-primary，旋钮白；
//	未选中：bg-muted border-border，旋钮 muted-foreground/50；深色下 bg-primary/20 border-primary/40，旋钮 primary/60。
//
// 状态变化走过渡：按钮与旋钮都是 `transition-all duration-base`——底色、边框、旋钮的颜色与 left 在 duration-base 内
// 按标准曲线补间（标准档 180 ms、减弱档 110 ms、关闭档直接到位）；键盘聚焦时的全局聚焦环（ring-2 ring-primary/50）也是 box-shadow，
// 同样补间：环的宽度从 0 长到 2px、不透明度从 0 到 0.5。没有悬停与按下的样式。
//
// *on 在点击时由 Viva 写回；返回的 Box 的 Clicked/Changed 照常可用。
func (l *Look) Switch(c ui.C, on *bool, label string) ui.Box {
	return l.SwitchWith(c, on, SwitchSpec{Label: label})
}

// SwitchWith 是带选项的 Switch。
func (l *Look) SwitchWith(c ui.C, on *bool, spec SwitchSpec) ui.Box {
	t := l.T
	checked := *on
	canvas := c.Canvas()
	// 过渡进度挂在专用的动画 ID 上：SwitchBase 的命中 ID 由 *on 的地址决定，取不到。
	anim := ui.ID(c.Key("anim"))
	width, height := l.Rem(3), l.Rem(1.75)
	diameter, top := l.Rem(1.25), l.Rem(0.125)
	border, off, travel := float32(2), float32(2), float32(20) // 边框宽、未选中的 left、旋钮行程（px 字面量）
	if spec.Compact {
		width, height = l.Rem(2.25), l.Rem(1.25)
		diameter, top = l.Rem(1), 1
		border, off, travel = 1, 1, 16
	}
	radius := height / 2

	// flex-shrink-0：窄行里让说明文字让位，开关保持原宽。
	track := ui.SwitchBase(c, on, spec.Label).Size(width, height).Shrink(0).HitOutset(0, 0, 0, 0)
	if spec.Disabled {
		track = track.Disabled(true)
	} else {
		l.noteStop(track)
	}
	state := track.State()

	// 过渡进度：选中、键盘聚焦、禁用各一条，时长与曲线都是 transition-all duration-base 的标准曲线。
	p := l.hoverTo(canvas, anim, 0, checked, DurationBase)
	ring := l.hoverTo(canvas, anim, 1, state.FocusVisible && !spec.Disabled, DurationBase)
	dim := l.hoverTo(canvas, anim, 2, spec.Disabled, DurationBase)

	offFill, offBorder, offKnob := t.Muted, t.Border, t.MutedForeground.WithAlpha(0.5)
	if l.Dark {
		offFill, offBorder, offKnob = t.Primary.WithAlpha(0.2), t.Primary.WithAlpha(0.4), t.Primary.WithAlpha(0.6)
	}
	opacity := 1 - 0.5*dim // disabled:opacity-50
	fill := fade(MixToken(offFill, t.Primary, p), opacity)
	borderInk := fade(MixToken(offBorder, t.Primary, p), opacity)
	knob := fade(MixToken(offKnob, white, p), opacity)
	left := off + travel*p // 默认 2px → 22px，紧凑 1px → 17px，px 字面量
	trackSurface := Surface{Bg: fill, Border: borderInk, BorderW: border, Radius: radius}

	return track.Draw(func(d *ui.DrawContext) {
		// 聚焦环只出现在边框盒外侧（box-shadow 的环不进盒子里面）：用空心环，半透明的轨道底色才不会透出环色。
		if ring > 0 {
			l.PaintRingBand(d, 2*ring, fade(t.Primary.WithAlpha(0.5), ring*opacity), radius)
		}
		// 轨道与旋钮都按 Chromium 的规则吸附到设备像素（见 surface.go）；
		// 旋钮相对按钮的内边距盒（边框之内）定位：border-2 + top / left，直径与 top 随根字号缩放。
		l.PaintSurface(d, trackSurface)
		bounds := gfx.Rect{X: d.Rect.X + border + left, Y: d.Rect.Y + border + top, Width: diameter, Height: diameter}
		d.Canvas.Rounded(SnapRect(bounds, d.Canvas.PixelScale()), diameter/2, d.Fade(l.Col(knob)))
	})
}
