package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
)

// 渲染层 Select（components/ui/Select.tsx）的几何常数。
const (
	selectMenuGap         float32 = 4   // 面板与触发器的间距
	selectViewportMargin  float32 = 12  // 面板与视口边缘的最小留白
	selectMinMenuHeight   float32 = 160 // 低于这个高度就认为「放不下」，翻到另一侧展开
	selectMaxMenuHeight   float32 = 280
	selectDefaultMinWidth float32 = 180 // 面板最小宽度（px）：窄触发器的面板仍有这么宽
)

// SelectOption 是下拉的一个选项。
type SelectOption struct {
	Value string
	Label string
}

// SelectState 是一个下拉的界面状态：由持有它的屏幕保存。
type SelectState struct {
	Open bool
	// anchor 是触发器上一帧的外框（页面坐标）：面板据此定位。展开发生在触发器画过之后，所以那一刻的位置总是有效的。
	anchor gfx.Rect
}

// SelectSpec 描述一个下拉。
type SelectSpec struct {
	Label   string // 可访问名（aria-label）
	Value   string // 当前值
	Options []SelectOption
	Width   float32 // 触发器容器宽度（调用方按 Tailwind 的 w-40 等换算成像素）；负数撑满容器（w-full）
	// MenuMinWidth 是面板最小宽度（px）；0 取 180。窄触发器（如分页每页条数）需要调小。
	MenuMinWidth float32
	// AlignEnd 让面板与触发器右边缘对齐；贴右侧边缘的控件用它才不会越界。
	AlignEnd bool
	// Trigger 非空时触发按钮用这一套样式（渲染层的 triggerClassName 覆盖），此时 Width 不起作用：按钮按内容收缩。
	Trigger *SelectTriggerStyle
	// Placeholder 是没有选中任何选项时触发按钮上显示的文字（渲染层 placeholder ?? t('common.select')），用弱化色；
	// 空表示什么也不显示（沿用老行为）。
	Placeholder string
}

// Select 是带弹层的下拉（渲染层 Select）：触发按钮加一块选项面板。返回这一帧选中的值与是否发生了选择。
//
// 面板是浮层：点触发器展开/收起，点面板外或按 Esc 关闭，选中后关闭并把值交给调用方；
// 点面板外的这一下继续交给下面的控件（与网页的 mousedown 外部点击一致），点触发器则只收起。
func (l *Look) Select(c ui.C, st *SelectState, spec SelectSpec) (picked string, changed bool) {
	display, muted := "", false
	for _, o := range spec.Options {
		if o.Value == spec.Value {
			display = o.Label
			break
		}
	}
	if display == "" && spec.Placeholder != "" {
		display, muted = spec.Placeholder, true
	}
	var trigger ui.Box
	if spec.Trigger != nil {
		trigger = l.selectTriggerCompact(c, spec.Label, display, muted, *spec.Trigger, st.Open)
	} else {
		trigger = l.SelectTrigger(c, spec.Label, display, muted, spec.Width, st.Open)
	}
	trigger = trigger.DrawOver(func(d *ui.DrawContext) { st.anchor = d.Rect })
	if trigger.Clicked() {
		st.Open = !st.Open
	}
	if !st.Open {
		return "", false
	}
	picked, changed = l.selectPanel(c, st, spec)
	if changed {
		st.Open = false
	}
	return picked, changed
}

// selectPanelRect 按渲染层的算法定位面板（Select.tsx 的 updatePosition）：
// 下方放不下（< 160px）且上方更宽裕时向上展开；面板可以比触发器宽，所以左边界要夹回视口内；
// 最大高度取 280 与可用空间（至少 160）中较小的。返回面板的页面坐标矩形。
func selectPanelRect(anchor gfx.Rect, vw, vh, contentH, minWidth float32, alignEnd bool) gfx.Rect {
	spaceBelow := vh - (anchor.Y + anchor.Height) - selectMenuGap - selectViewportMargin
	spaceAbove := anchor.Y - selectMenuGap - selectViewportMargin
	openUp := spaceBelow < selectMinMenuHeight && spaceAbove > spaceBelow

	width := max(anchor.Width, minWidth)
	preferred := anchor.X
	if alignEnd {
		preferred = anchor.X + anchor.Width - width
	}
	maxLeft := max(selectViewportMargin, vw-width-selectViewportMargin)
	left := min(max(selectViewportMargin, preferred), maxLeft)

	space := spaceBelow
	if openUp {
		space = spaceAbove
	}
	height := min(contentH, min(selectMaxMenuHeight, max(space, selectMinMenuHeight)))
	// 向上展开时锚定底边：面板高度由选项条数决定，不是 maxHeight。
	top := anchor.Y + anchor.Height + selectMenuGap
	if openUp {
		top = anchor.Y - selectMenuGap - height
	}
	return gfx.Rect{X: left, Y: top, Width: width, Height: height}
}

// selectPanel 声明整窗背景层与选项面板；返回选中的值。
func (l *Look) selectPanel(c ui.C, st *SelectState, spec SelectSpec) (picked string, changed bool) {
	t := l.T
	bounds := c.Bounds()
	rowH := l.Rem(2.25) // py-2 + text-sm 的 1.25rem 行高
	padY := l.Space(1)  // 列表容器 py-1
	contentH := 2 + 2*padY + float32(len(spec.Options))*rowH
	minWidth := spec.MenuMinWidth
	if minWidth <= 0 {
		minWidth = selectDefaultMinWidth
	}
	rect := selectPanelRect(st.anchor, bounds.Width, bounds.Height, contentH, minWidth, spec.AlignEnd)

	if l.dismissLayer(c, dismissSpec{Band: bandSelectDismiss, PassPress: true, Keep: &st.anchor}) {
		st.Open = false
		return "", false
	}

	radius := l.Rem(0.75) // rounded-lg
	inner := gfx.Rect{X: rect.X + 1, Y: rect.Y + 1, Width: rect.Width - 2, Height: rect.Height - 2}
	surface := Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: radius, Shadows: ShadowLG}
	id := ui.ID(c)
	motion := l.Motion(DurationQuick, EaseEnter)
	panel := l.floatingPanel(c, bandSelectPanel, rect).
		Pad(1).
		Draw(func(d *ui.DrawContext) {
			enterPresence(d.Canvas, id, 0, motion, 0.95, center(d.Rect))
			l.PaintSurface(d, surface)
			// 面板是不透明的：按在面板边距上不能落到下面的背景层（那会关闭面板）。
			d.Canvas.Block(d.Rect)
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleListBox, spec.Label, gfx.HitOptions{})
		}).
		DrawOver(func(d *ui.DrawContext) { d.Canvas.EndPresence() })
	panel.Children(func() {
		l.ScrollArea(c, [4]float32{padY, 0, padY, 0}, func() {
			ui.Column(c).FillWidth().Children(func() {
				for _, opt := range spec.Options {
					if l.selectOption(c.Key(opt.Value), opt, opt.Value == spec.Value, inner, radius-1) {
						picked, changed = opt.Value, true
					}
				}
			})
		}).Fill()
	})
	return picked, changed
}

// selectOption 是面板里的一行（渲染层 OptionItem）：w-full px-3 py-2 text-sm，gap-2；
// 选中项 bg-primary/10 text-primary font-medium 并在右侧带 16px 的对勾，其余 text-foreground、悬停 bg-muted/50。
//
// 面板 overflow-hidden 会把行的背景裁到圆角内：这里把行背景画成「面板内圆角矩形」与「本行矩形」的交集，
// 首尾两行的外侧角因此跟随面板的圆角（内半径 = 外半径 − 边框）。
func (l *Look) selectOption(c ui.C, opt SelectOption, selected bool, inner gfx.Rect, innerRadius float32) bool {
	t := l.T
	id := ui.ID(c)
	color, weight := t.Foreground, WeightNormal
	if selected {
		color, weight = t.Primary, WeightMedium
	}
	row := ui.Row(c).FillWidth().
		Items(ui.Center).
		Gap(l.Rem(0.5)).
		PadX(l.Rem(0.75)).
		PadY(l.Rem(0.5)).
		HitWith(id, opt.Label, gfx.HitOptions{Role: gfx.RoleOption, Selected: selected, Cursor: gfx.PointerCursorHand}).
		Draw(func(d *ui.DrawContext) {
			scale := d.Canvas.PixelScale()
			rowRect := SnapRect(d.Rect, scale)
			// 键盘聚焦：全局的 button:focus-visible { ring-2 ring-primary/50 }；环在面板 overflow-hidden 之内。
			if d.State.FocusVisible {
				d.Canvas.Clip(SnapRect(inner, scale))
				ring := gfx.Rect{X: rowRect.X - 2, Y: rowRect.Y - 2, Width: rowRect.Width + 4, Height: rowRect.Height + 4}
				d.Canvas.Fill(ring, d.Fade(l.Col(t.Primary.WithAlpha(0.5))))
				d.Canvas.Unclip()
			}
			fill := t.Muted.WithAlpha(0.5 * l.hoverTo(d.Canvas, id, 0, d.State.Hovered, DurationInstant))
			if selected {
				fill = t.Primary.WithAlpha(0.1)
			}
			if fill.A <= 0 {
				return
			}
			d.Canvas.Clip(rowRect)
			d.Canvas.Rounded(SnapRect(inner, scale), innerRadius, d.Fade(l.Col(fill)))
			d.Canvas.Unclip()
		})
	row.Children(func() {
		l.Text(c, opt.Label, TextSM, weight, color, Ellipsis()).Grow(1).Shrink(1)
		if selected {
			l.Icon(c, icons.Check, l.Rem(1), color).Shrink(0)
		}
	})
	return row.Clicked()
}
