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
// 对勾的 drop-shadow 是 CSS filter，Viva 没有对应的原语：用图标的覆盖度自己算（icondropshadow.go，台账 B-021）。
//
// 环的三种状态（2026-11-18 在渲染层实测，台账 B-004）：
//   - 无环：未选中；
//   - 选中环：`0 0 0 2px 页面底色, 0 0 0 4px 主色`（两层 box-shadow）；
//   - 聚焦环：键盘聚焦时全局的 `button:focus-visible { ring-2 ring-primary/50 ring-offset-0 }` 比色板自己的 ring 类优先级高，
//     整组环被它取代成 `0 0 0 0 底色, 0 0 0 2px 主色/50`——选中的色板聚焦后选中环缩成一圈半透明细环，不是叠加。
//
// 状态之间走 transition-all duration-base：box-shadow 的列表逐层补间（宽度与颜色一起变，缺的层用零宽透明层补齐），
// opacity 同步补间。

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

// 色板环的 box-shadow 宽度（px 字面量，不随字号缩放）。
const (
	swatchOffsetWidth = 2 // ring-offset-2
	swatchRingWidth   = 4 // ring-2 加 ring-offset-2：环的外沿离色板 4px
	swatchFocusWidth  = 2 // 聚焦环：ring-2、没有偏移
)

// Swatch 画一枚 2.5rem 的圆形色板，返回按钮盒（Clicked() 取点击）。
func (l *Look) Swatch(c ui.C, spec SwatchSpec) ui.Box {
	t := l.T
	id := ui.ID(c)
	size := l.Rem(2.5)
	box := ui.Row(c).Width(size).Height(size).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, spec.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: spec.On, Hint: spec.Name, Cursor: gfx.PointerCursorHand})
	state := box.State()
	canvas := c.Canvas()
	p := l.hoverTo(canvas, id, 0, state.Hovered, DurationBase)
	on := l.hoverTo(canvas, id, 1, spec.On, DurationBase)
	focus := l.hoverTo(canvas, id, 2, state.FocusVisible, DurationBase)

	// 未选中的悬停变淡；选中项没有 hover:opacity。opacity 是整枚元素的合成不透明度，环与对勾一起淡。
	opacity := float32(1)
	if spec.HoverOpacity > 0 && spec.HoverOpacity < 1 {
		opacity = 1 - (1-spec.HoverOpacity)*p*(1-on)
	}
	fill := Fade(spec.Fill, opacity)

	// 环：无环 → 选中环 → 聚焦环，逐层补间；第一层（ring-offset）画在最上，第二层（ring）在下。
	selected := []tokens.Shadow{
		{Spread: swatchOffsetWidth, Color: t.Background},
		{Spread: swatchRingWidth, Color: t.Primary},
	}
	focused := []tokens.Shadow{
		{Spread: 0, Color: t.Background},
		{Spread: swatchFocusWidth, Color: t.Primary.WithAlpha(0.5)},
	}
	rings := MixShadows(MixShadows(nil, selected, on), focused, focus)

	return box.Draw(func(d *ui.DrawContext) {
		radius := d.Rect.Height / 2
		for i := len(rings) - 1; i >= 0; i-- {
			if rings[i].Spread > 0 {
				l.PaintRingBand(d, rings[i].Spread, Fade(rings[i].Color, opacity), radius)
			}
		}
		l.PaintSurface(d, Surface{Bg: fill, Radius: radius})
	}).Children(func() {
		if spec.On {
			l.IconDropShadow(c.Key("check"), icons.Check, l.Rem(1), tokens.RGBA(255, 255, 255, 1), TailwindDropShadow)
		}
	})
}
