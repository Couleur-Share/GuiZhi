package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// PaletteLayer（渲染层 GlobalCommandPalette 的外壳）
//
// 命令面板这类「顶部对齐的整窗搜索浮层」：fixed inset-0 z-[100000] flex justify-center bg-background/55 px-4 pt-[12vh]，
// 里面一块 max-w-2xl 的面板（rounded-2xl border border-border bg-background shadow-2xl，overflow-hidden）。
//   - 层号取最高的一层（BandTop）：渲染层它的 z-index 与 Tooltip 同为 100000，高于 Toast 与 ConfirmDialog（99999），所以盖得住它们；
//   - 点遮罩（只有点在遮罩上才算，渲染层 target === currentTarget）与 Esc 关闭；Tab 与指针都限制在面板里，关闭后焦点回到打开之前的控件；
//   - 没有入场与退场动画（渲染层没写 animate-*）；遮罩后面的模糊没有（台账 A-007，Viva 没有模糊原语）。

const (
	// BandTop 是浮层的最高一层：命令面板用它；与 Tooltip 同层，Tooltip 在窗口根最后声明，所以仍在它之上。
	BandTop = bandTooltip

	paletteScrimAlpha  = 0.55 // bg-background/55
	paletteTopFraction = 0.12 // pt-[12vh]
)

// PaletteLayerSpec 描述一个命令面板外壳。Open 为假时什么都不画。
type PaletteLayerSpec struct {
	Open bool
	// Label 是对话框的可访问名（aria-label）。
	Label string
	// Body 画面板里的内容（面板本身只给表面、圆角、边框与宽度上限；内容的高度由它自己约束）。
	Body func(c ui.C)
}

// PaletteLayer 声明命令面板，返回这一帧是否被要求关闭（点了遮罩，或按了 Esc）。
// 调用方在收到 true 时自己把面板关掉：关闭是否成立由它决定。
func (l *Look) PaletteLayer(c ui.C, spec PaletteLayerSpec) bool {
	if !spec.Open {
		return false
	}
	t := l.T
	var (
		scope   = ui.ID(c.Key("scope"))
		scrimID = ui.ID(c.Key("scrim"))
		bounds  = c.Bounds()
		radius  = l.Rem(1) // rounded-2xl
		surface = Surface{Bg: t.Background, Border: t.Border, BorderW: 1, Radius: radius, Shadows: Shadow2XL}
		inset   = l.Space(4) // px-4
	)
	// 整窗一层：遮罩（吸收背后的点击）与面板在同一个焦点作用域里，次序同 Modal——先进入作用域、画遮罩并登记命中区，
	// 面板随后声明，最后在作用域还开着时认领焦点陷阱。
	host := ui.Overlay(c.Key("host"), ui.OverlaySpec{Band: BandTop, Bounds: bounds}).
		Items(ui.Center).Justify(ui.Start).PadSides(bounds.Height*paletteTopFraction, inset, 0, inset).
		Draw(func(d *ui.DrawContext) {
			d.Canvas.PushFocusScope(scope)
			d.Canvas.Fill(d.Rect, d.Fade(l.Col(t.Background.WithAlpha(paletteScrimAlpha))))
			d.Canvas.HitWithOptions(scrimID, d.Rect, "", gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Backdrop: true})
			d.Canvas.SetEscapeTarget(scrimID)
		}).
		DrawOver(func(d *ui.DrawContext) {
			d.Canvas.TrapFocus(scope)
			d.Canvas.PopFocusScope()
		})
	host.Children(func() {
		// h-fit w-full max-w-2xl：宽度随窗口、上限 42rem，高度随内容；边框 1px 在盒内，内容缩进一个边框宽（overflow-hidden）。
		ui.Column(c.Key("panel")).FillWidth().MaxWidth(l.Rem(42)).MinHeight(0).Pad(1).Clip().
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, surface)
				// 面板是不透明的：按在面板上不能落到下面的遮罩（那会关闭面板）。
				d.Canvas.Block(d.Rect)
				d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, spec.Label, gfx.HitOptions{})
			}).
			Children(func() {
				if spec.Body != nil {
					spec.Body(c)
				}
			})
	})

	// 点遮罩与 Esc 都以对遮罩的一次点击送达：指针点的带点击次数，Esc 的次数为 0。
	if canvas := c.Canvas(); canvas.Clicked(scrimID) {
		return true
	}
	return false
}
