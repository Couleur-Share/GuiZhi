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
// 状态与过渡（2026-11-18 在渲染层实测，scripts/parity/tools/probe-states.mjs，台账 B-004）：
//   - 过渡取决于变化之后的样式：选中项是 `transition: all 180ms`（duration-base，标准曲线）；
//     未选中项带 `.app-settings-subtle`，它自己写了 `background-color / border-color / box-shadow 160ms ease` 盖掉了 Tailwind 的 transition-all，
//     所以转向「未选中」时底色、边框、阴影走 160 ms 的 ease，而文字颜色与 transform 不在过渡清单里，直接切换；
//   - 悬停：未选中项多一层 `shadow`（160 ms 补间）、整枚上移 2px（`-translate-y-0.5`，随根字号）；选中项只上移。
//     上移在未选中项上没有过渡（瞬间到位），在选中项上走 180 ms；按下（:active）时回到原位；
//   - 键盘聚焦（:focus-visible）：全局的 ring-2 ring-primary/50，也是 box-shadow，随同一条过渡长出来。
//
// 上移是 transform：版面不动，绘制与命中区一起挪走（Chromium 的悬停按挪动后的几何判定），
// 所以指针停在按钮下沿 2px 内时，渲染层会逐帧来回触发悬停（Chromium 在命中区变动后补发假的鼠标移动）；
// Viva 只在指针事件时重算悬停，指针不动就不闪（台账 A-022，用户裁决接受）。Viva 没有盒子级的变换，
// 这里用「固定占位的槽 + 上下互相抵消的外边距」让按钮在槽里整体上移，绘制与命中区同步。

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
	canvas := c.Canvas()
	radius := l.Rem(1)
	liftPx := l.Rem(0.125) // -translate-y-0.5
	// 内边距之外是 app-settings-subtle 的 1px 边框；选中项在渲染层没有边框，但同一行的按钮被网格拉到同样的高度、
	// 内容居中，所以版面相同，这里给所有按钮统一留出边框。
	padY, padX := l.Space(2.5)+1, l.Space(4)+1

	var box ui.Box
	// 槽占住网格单元（flex-1，高度由内容与同行最高的一枚决定）；按钮在槽里整体上移，版面不动。
	ui.Row(c).Grow(1).Basis(0).MinWidth(0).Items(ui.Stretch).Children(func() {
		id := ui.ID(c)
		row := ui.Row(c).Grow(1).MinWidth(0).Items(ui.Center).Justify(ui.Center).PadSides(padY, padX, padY, padX)
		box = row.HitWith(id, spec.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: spec.On, Cursor: gfx.PointerCursorHand})
		state := box.State()

		// 过渡的时长与曲线由「变化之后的样式」决定（见文件头）。
		toSelected := l.Motion(DurationBase, EaseStandard) // 变成选中：transition-all duration-base
		toSubtle := l.Motion(DurationSurface, EaseDefault) // 变成未选中：.app-settings-subtle 的 160ms ease
		surface := toSubtle                                // 底色、边框、阴影
		transform := gfx.Motion{}                          // 上移：未选中项没有 transform 过渡
		text := gfx.Motion{}                               // 文字颜色：转向未选中时没有过渡
		if spec.On {
			surface, transform, text = toSelected, toSelected, toSelected
		}
		on := canvas.Animate(id, 0, boolProgress(spec.On), surface)
		textOn := canvas.Animate(id, 1, boolProgress(spec.On), text)
		hoverShadow := canvas.Animate(id, 2, boolProgress(state.Hovered), toSubtle)
		raised := canvas.Animate(id, 3, boolProgress(state.Hovered && !state.Pressed), transform)
		ring := canvas.Animate(id, 4, boolProgress(state.FocusVisible), surface)

		fg := MixToken(t.Foreground, t.PrimaryForeground, textOn)
		bg := MixToken(t.SettingsSubtleBg, t.Primary, on)
		border := MixToken(t.SettingsSubtleBorder, t.Primary, on)
		// 阴影：未选中悬停时 shadow，选中时 shadow-lg shadow-primary/20；两者之间按进度插值。
		shadows := MixShadows(MixShadows(nil, ShadowBase, hoverShadow), ShadowLGOf(t.Primary.WithAlpha(0.2)), on)
		lift := liftPx * raised

		box = box.MarginTop(-lift).MarginBottom(lift).Draw(func(d *ui.DrawContext) {
			// 聚焦环是 box-shadow：只在边框盒外侧，用空心环（未选中的底色是半透明的）。
			if ring > 0 {
				l.PaintRingBand(d, 2*ring, t.Primary.WithAlpha(0.5*ring), radius)
			}
			l.PaintSurface(d, Surface{Bg: bg, Border: border, BorderW: 1, Radius: radius, Shadows: shadows})
		}).Children(func() {
			if spec.Content != nil {
				spec.Content(fg)
			}
		})
	})
	return box
}

// boolProgress 把布尔状态换成动画目标。
func boolProgress(on bool) float32 {
	if on {
		return 1
	}
	return 0
}
