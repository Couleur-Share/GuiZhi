package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// 色板按钮（显示设置里的主题色带）
//
// 渲染层的写法：`relative h-10 w-10 flex-shrink-0 rounded-full transition-all duration-base`，底色是内联的 hsl 或自定义十六进制；
// 选中 `ring-2 ring-primary ring-offset-2 ring-offset-background`，里面叠一枚 `w-4 h-4 text-white drop-shadow` 的对勾；
// 未选中 `hover:opacity-90`（自定义入口 0.95）。
//
// 对勾的 drop-shadow 是 CSS filter，Viva 没有对应的原语（台账 B-021）：对勾按无阴影画。

// SwatchSpec 描述一枚色板按钮。
type SwatchSpec struct {
	// Name 是 title 与 aria-label。
	Name string
	// Fill 是底色。
	Fill tokens.Color
	// On 表示选中：画选中环与对勾。
	On bool
	// HoverOpacity 是未选中时悬停的不透明度（hover:opacity-90 取 0.9，hover:opacity-95 取 0.95）。
	HoverOpacity float32
}

// Swatch 画一枚 2.5rem 的圆形色板，返回按钮盒（Clicked() 取点击）。
func (l *Look) Swatch(c ui.C, spec SwatchSpec) ui.Box {
	t := l.T
	id := ui.ID(c)
	size := l.Rem(2.5)
	box := ui.Row(c).Width(size).Height(size).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, spec.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: spec.On, Hint: spec.Name, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	p := l.hoverTo(c.Canvas(), id, 0, hovered, DurationBase)
	on := l.hoverTo(c.Canvas(), id, 1, spec.On, DurationBase)

	// 未选中的悬停变淡；选中项没有 hover:opacity。两者的过渡都在 duration-base 内完成。
	opacity := float32(1)
	if spec.HoverOpacity > 0 && spec.HoverOpacity < 1 {
		opacity = 1 - (1-spec.HoverOpacity)*p*(1-on)
	}
	fill := Fade(spec.Fill, opacity)
	ringColor := Fade(t.Primary, on)
	offsetColor := Fade(t.Background, on)

	return box.Draw(func(d *ui.DrawContext) {
		radius := d.Rect.Height / 2
		if d.State.FocusVisible {
			l.PaintRing(d, 2, l.ringColor(), radius)
		}
		// box-shadow 的两层：ring-offset（2px，页面底色）在上，ring（再外 2px，主色）在下。
		if on > 0 {
			l.PaintRing(d, 4, ringColor, radius)
			l.PaintRing(d, 2, offsetColor, radius)
		}
		l.PaintSurface(d, Surface{Bg: fill, Radius: radius})
	}).Children(func() {
		if spec.On {
			l.Icon(c.Key("check"), icons.Check, l.Rem(1), tokens.RGBA(255, 255, 255, 1))
		}
	})
}
