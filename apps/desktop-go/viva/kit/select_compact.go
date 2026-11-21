package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// SelectTriggerStyle 是渲染层 Select 的 triggerClassName 覆盖的一种形态：触发按钮不再撑满容器，
// 而是按内容收缩的小按钮（分页条里的每页条数：flex h-7 min-w-[56px] items-center justify-between gap-1 rounded-md
// border border-border bg-muted px-2 text-xs text-foreground transition-colors hover:bg-accent）。
//
// 默认的触发按钮（w-full h-10 px-3 rounded-lg bg-muted，见 SelectTrigger）整个被 triggerClassName 替换掉，
// 所以这里是另一套完整样式，不是在默认样式上改几处。键盘聚焦才画环（focus-visible:ring-2 ring-primary/50），点击聚焦不画。
// 里面的文字恒为 text-foreground，箭头恒为 16px 的 text-muted-foreground，展开时旋转 180°（与默认触发器相同）。
type SelectTriggerStyle struct {
	Height   float32 // h-7
	MinWidth float32 // min-w-[56px]（px 字面量）
	PadX     float32 // px-2（不含边框）
	Gap      float32 // gap-1
	Radius   float32 // rounded-md
	BorderW  float32
	Border   tokens.Color
	Bg       tokens.Color
	BgHover  tokens.Color
	// FontPx 与 LinePx 是文字的字号与行高（text-xs 是 0.75rem / 1rem）。
	FontPx, LinePx float32
	// Transition 是悬停底色的过渡档位（transition-colors 没写时长，取 DurationDefault）。
	Transition DurationName
	// TextStart 让文字靠左（triggerClassName 里写了 text-left，如网页快照的宽度下拉）；缺省居中（<button> 的默认对齐，分页条的每页条数）。
	TextStart bool
}

// selectTriggerCompact 画 SelectTriggerStyle 描述的触发按钮；可访问语义与默认触发器相同（带弹层的下拉触发器）。
func (l *Look) selectTriggerCompact(c ui.C, label, value string, muted bool, st SelectTriggerStyle, open bool) ui.Box {
	t := l.T
	id := ui.ID(c)
	// border-box：边框占在盒内，文字区的左右留白是边框加内边距。
	pad := st.PadX + st.BorderW
	box := ui.Row(c).Height(st.Height).MinWidth(st.MinWidth).Shrink(0).Items(ui.Center).Gap(st.Gap).
		PadSides(0, pad, 0, pad).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleCombobox, Expandable: true, Expanded: open, Value: value, Cursor: gfx.PointerCursorHand})
	l.noteStop(box)
	hover := l.hoverTo(c.Canvas(), id, 0, box.State().Hovered, st.Transition)
	bg := MixToken(st.Bg, st.BgHover, hover)
	fg := t.Foreground
	if muted { // 没有选中项：显示的是占位文字，text-muted-foreground
		fg = t.MutedForeground
	}
	return box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), st.Radius)
		}
		l.PaintSurface(d, Surface{Bg: bg, Border: st.Border, BorderW: st.BorderW, Radius: st.Radius})
	}).Children(func() {
		// 文字所在的 span 是 flex-1，而 triggerClassName 里没有 text-left：<button> 默认居中对齐，
		// 内容比 span 窄时（「20」）文字落在 span 正中，不是靠左。
		opts := []TextOpt{Ellipsis()}
		if !st.TextStart {
			opts = append(opts, Centered())
		}
		l.TextPx(c, value, st.FontPx, st.LinePx, WeightNormal, fg, opts...).Grow(1).Shrink(1).MinWidth(0)
		l.RotatingChevron(c, id, l.Rem(1), open, t.MutedForeground, DurationBase)
	})
}
