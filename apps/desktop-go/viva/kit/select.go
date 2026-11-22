package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// slotChevron 是触发按钮箭头的旋转动画槽位（同一个控件 ID 上的动画用槽位区分）。
const slotChevron uint8 = 1

// SelectTrigger 复刻渲染层 Select 的触发按钮：w-full h-10 px-3 rounded-lg bg-muted，文字 text-sm，
// 右侧 16px 的 ChevronDown（muted-foreground），gap-2。悬停 bg-muted/80；获得焦点或展开时 ring-2 ring-primary/30；
// 整体 transition-all duration-quick（背景与环渐变），展开时箭头 rotate-180（transition-transform duration-base）。
// width 是外层容器宽度（调用方按 Tailwind 的 w-40 等换算成像素）；open 是列表是否展开。
//
// 对外的可访问语义是带弹层的下拉触发器（combobox，展开状态与当前值）：渲染层是
// button[aria-haspopup=listbox][aria-expanded]，L1 的角色映射把两者归为同一类。
func (l *Look) SelectTrigger(c ui.C, label, value string, muted bool, width float32, open bool) ui.Box {
	return l.selectTrigger(c, label, value, muted, width, open, nil)
}

// selectTrigger 是 SelectTrigger 的实现；lead 非空时在文字前画一枚前导标记（渲染层选项里的品牌图标，
// <span className="flex items-center gap-2">图标 + 文字</span>），占一个 gap。
func (l *Look) selectTrigger(c ui.C, label, value string, muted bool, width float32, open bool, lead func(c ui.C)) ui.Box {
	t := l.T
	id := ui.ID(c)
	ring := l.Col(t.Primary.WithAlpha(0.3))
	pad := l.Rem(0.75)
	chevron := l.Rem(1)
	gap := l.Rem(0.5)
	box := ui.Row(c).Height(l.Rem(2.5))
	if width < 0 { // 负宽度表示撑满容器（w-full）
		box = box.FillWidth()
	} else {
		// 网页里弹性项的 min-width:auto 是内容的最小尺寸，但不超过显式宽度：
		// 行被挤窄时触发按钮最窄收到「文字 + px-3 + 箭头 + gap」，不会把当前值截成省略号。
		minW := 2*pad + chevron + gap + c.Canvas().TextWidthWeight(value, l.Rem(TextSM.Size), WeightNormal)
		box = box.Width(width).MinWidth(min(minW, width))
	}
	box = box.
		Radius(l.Rem(0.75)).
		Background(l.Col(t.Muted)).
		Items(ui.Center).
		PadX(pad).
		Gap(gap).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleCombobox, Expandable: true, Expanded: open, Value: value}).
		Hover(ui.Look{}.Background(l.Col(t.Muted.WithAlpha(0.8)))).
		Focused(ui.Look{}.Ring(2, ring)).
		TransitionWith(l.Motion(DurationQuick, EaseStandard))
	l.noteStop(box)
	if open {
		box = box.Ring(2, ring)
	}
	fg := t.Foreground
	if muted { // 没有选中项：显示的是占位文字，text-muted-foreground
		fg = t.MutedForeground
	}
	return box.Children(func() {
		if lead != nil {
			lead(c.Key("lead"))
		}
		l.Text(c, value, TextSM, WeightNormal, fg, Ellipsis()).Grow(1).Shrink(1)
		l.RotatingChevron(c, id, chevron, open, l.T.MutedForeground, DurationBase)
	})
}

// RotatingChevron 画触发按钮右侧的 ChevronDown：展开时旋转 180°（transition-transform，标准缓动，时长 dur：
// Select 是 duration-base，顶栏「新建」的箭头没有写 duration-*，用 Tailwind 默认的 150 ms）。
// 静止（0° 与 180°）时用精确栅格化的 ChevronDown / ChevronUp——lucide 的 chevron-up 正是 chevron-down 绕中心转半圈，
// 几何完全相同；只有动画中途才用 Viva 原生的旋转绘制。
func (l *Look) RotatingChevron(c ui.C, id gfx.ID, size float32, open bool, color tokens.Color, dur DurationName) ui.Box {
	col := l.Col(color)
	target := float32(0)
	if open {
		target = 1
	}
	motion := l.Motion(dur, EaseStandard)
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		mark := l.markIcon(d)
		tint := d.Fade(col)
		turn := d.Canvas.Animate(id, slotChevron, target, motion)
		switch {
		case turn <= 0.001:
			PaintIcon(d.Canvas, icons.ChevronDown, d.Rect, tint)
		case turn >= 0.999:
			PaintIcon(d.Canvas, icons.ChevronUp, d.Rect, tint)
		default:
			d.Canvas.DrawIconTransformed(icons.ChevronDown, d.Rect, tint, gfx.IconTransform{Rotate: math.Pi * turn})
		}
		l.reportIcon(d, mark)
	})
}
