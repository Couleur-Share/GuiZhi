package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 分段按钮（显示设置里的主题模式、字号档、动效偏好）
//
// 渲染层的写法：`py-2.5 px-4 rounded-2xl text-[13px] font-medium transition-all duration-base`，
// 选中 `bg-primary text-primary-foreground shadow-lg shadow-primary/20`，
// 其余 `app-settings-subtle text-foreground hover:shadow`（底色、1px 边框、1rem 圆角取设置页的轻底令牌），
// 全部带 `hover:-translate-y-0.5 active:translate-y-0`。
//
// 悬停上移 2px 属于控件状态与动效（台账 B-004）：版面不动、命中区随变换移动，会带来按钮下沿的边缘闪动，尚未复刻。

// SegmentSpec 描述一枚分段按钮。
type SegmentSpec struct {
	// Name 是可访问名：渲染层按钮的文字，或 aria-label。
	Name string
	// On 是 aria-pressed，也决定选中外观。
	On bool
	// Content 在按钮里声明内容；fg 是此刻（含状态过渡）的文字色。
	Content func(fg tokens.Color)
}

// SegmentGrid 是一排等宽的分段按钮（grid grid-cols-N gap-3）：按钮横向均分，高度取最高的一枚。
// 内边距由调用方决定（主题模式与字号档的 p-4，动效偏好是 space-y-3 里的一块）。
func (l *Look) SegmentGrid(c ui.C, body func()) ui.Box {
	return ui.Row(c).FillWidth().Gap(l.Space(3)).Children(body)
}

// Segment 画一枚分段按钮，返回按钮盒（Clicked() 取点击）。须放在 SegmentGrid 里。
func (l *Look) Segment(c ui.C, spec SegmentSpec) ui.Box {
	t := l.T
	id := ui.ID(c)
	radius := l.Rem(1)
	// 内边距之外是 app-settings-subtle 的 1px 边框；选中项在渲染层没有边框，但同一行的按钮被网格拉到同样的高度、
	// 内容居中，所以版面相同，这里给所有按钮统一留出边框。
	padY, padX := l.Space(2.5)+1, l.Space(4)+1
	row := ui.Row(c).Items(ui.Center).Justify(ui.Center).Grow(1).Basis(0).MinWidth(0).
		PadSides(padY, padX, padY, padX)
	box := row.HitWith(id, spec.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: spec.On, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	p := l.hoverTo(c.Canvas(), id, 0, hovered, DurationBase)
	on := l.hoverTo(c.Canvas(), id, 1, spec.On, DurationBase)

	fg := MixToken(t.Foreground, t.PrimaryForeground, on)
	bg := MixToken(t.SettingsSubtleBg, t.Primary, on)
	border := MixToken(t.SettingsSubtleBorder, t.Primary, on)
	// 阴影：未选中悬停时 shadow，选中时 shadow-lg shadow-primary/20；两者之间按进度插值。
	shadows := MixShadows(MixShadows(nil, ShadowBase, p), ShadowLGOf(t.Primary.WithAlpha(0.2)), on)

	return box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, l.ringColor(), radius)
		}
		l.PaintSurface(d, Surface{Bg: bg, Border: border, BorderW: 1, Radius: radius, Shadows: shadows})
	}).Children(func() {
		if spec.Content != nil {
			spec.Content(fg)
		}
	})
}
