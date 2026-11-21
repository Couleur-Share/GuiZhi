package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// inlineSpec 描述一枚内容自画的按钮式控件：头部的合集 chip、来源链接、更多钮，以及面板里内容不止「图标加文字」的按钮。
// 命中区、悬停过渡、聚焦环、禁用淡出与 kit.Button 同一套规则；内容（几段图标与文字）由 body 按 fg 自己画。
type inlineSpec struct {
	// Name 是可访问名；Hint 是渲染层的 title（悬停 320 ms 出气泡）；Role 缺省是按钮。
	// Description 是可访问描述（渲染层 title 与名字不同时，Chromium 把它当描述，如来源链接的完整地址）；
	// Value 是弹出菜单触发钮此刻显示的值（按钮里的文字，Chromium 的 combobox 值）。
	Name, Hint, Description, Value string
	Role                           gfx.Role
	Disabled                       bool
	// Popup 为真时它是 aria-haspopup="menu" 的触发钮（可访问树里是 combobox），Open 是菜单此刻开着。
	Popup, Open bool
	// Disclosure 为真时它是展开收起钮（aria-expanded，仍是按钮），Open 是它此刻展开着。不要与 Popup 同设。
	Disclosure bool

	Fg, FgHover         tokens.Color
	Bg, BgHover         tokens.Color
	Border, BorderHover tokens.Color
	BorderW             float32
	Radius              float32
	Height, Width       float32
	// PadX 与 PadY 已含边框。
	PadX, PadY float32
	Gap        float32
	// Ring 是键盘聚焦环的颜色，零值取 ring-primary/50。
	Ring tokens.Color
	// DisabledOpacity 是禁用时整枚的不透明度，0 取 0.5。
	DisabledOpacity float32
	// Rect 非空时，每一帧写回这枚的外框（菜单的锚点与 Keep 用）。
	Rect *gfx.Rect
	// MaxFill 为真时外框不超过容器的宽度（max-w-full）：内容放不下时由里面的子项自己收窄（见 versionsChip）。
	MaxFill bool
}

// inlineButton 声明一枚自画内容的按钮，返回外层盒（调用方用 Clicked 取点击）。
func (s *Screen) inlineButton(c ui.C, spec inlineSpec, body func(fg tokens.Color)) ui.Box {
	l := s.Look
	id := ui.ID(c)
	role := spec.Role
	switch {
	case spec.Popup:
		role = gfx.RoleCombobox
	case role == 0:
		role = gfx.RoleButton
	}
	opts := gfx.HitOptions{Role: role, Disabled: spec.Disabled, Hint: spec.Hint, Description: spec.Description, Expandable: spec.Popup || spec.Disclosure, Expanded: (spec.Popup || spec.Disclosure) && spec.Open}
	if spec.Popup {
		opts.Value = spec.Value
	}
	if !spec.Disabled {
		opts.Cursor = gfx.PointerCursorHand
	}
	row := ui.Row(c).Shrink(0).Items(ui.Center).Gap(spec.Gap).PadSides(spec.PadY, spec.PadX, spec.PadY, spec.PadX)
	if spec.Height > 0 {
		row = row.Height(spec.Height)
	}
	if spec.Width > 0 {
		row = row.Width(spec.Width).Justify(ui.Center)
	}
	if spec.MaxFill {
		row = row.MaxWidthPercent(100)
	}
	box := row.HitWith(id, spec.Name, opts)
	if !spec.Disabled {
		l.TabStop(box)
	}
	ph := l.HoverProgress(c, id, 0, box.State().Hovered && !spec.Disabled, kit.DurationDefault)
	pick := func(base, hover tokens.Color) tokens.Color {
		if hover == (tokens.Color{}) {
			return base
		}
		return kit.MixToken(base, hover, ph)
	}
	fg, bg, border := pick(spec.Fg, spec.FgHover), pick(spec.Bg, spec.BgHover), pick(spec.Border, spec.BorderHover)
	if spec.Disabled {
		op := spec.DisabledOpacity
		if op == 0 {
			op = 0.5
		}
		box = box.Opacity(op)
	}
	ring := spec.Ring
	if ring == (tokens.Color{}) {
		ring = l.T.Primary.WithAlpha(0.5)
	}
	box = box.Draw(func(d *ui.DrawContext) {
		if spec.Rect != nil {
			*spec.Rect = d.Rect
		}
		if d.State.FocusVisible {
			l.PaintRing(d, 2, ring, spec.Radius)
		}
		surface := kit.Surface{Bg: bg, Radius: spec.Radius}
		if spec.BorderW > 0 {
			surface.Border, surface.BorderW = border, spec.BorderW
		}
		l.PaintSurface(d, surface)
	})
	return box.Children(func() { body(fg) })
}
