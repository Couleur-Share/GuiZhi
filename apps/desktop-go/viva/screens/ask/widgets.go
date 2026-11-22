package ask

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// textBtn 是行内文字按钮的外观：渲染层里没有写样式类的 <button>（只继承文字色与字号），
// 以及 text-primary underline、hover:text-foreground 这类只改文字的按钮。
type textBtn struct {
	// Style 是字号档；Px 与 Line 非零时盖过它（text-[11px] 这类 px 字面量不随字号档缩放，行高是继承来的 1.5 倍）。
	Style    kit.TextStyle
	Px, Line float32
	Weight   float32
	Fg       tokens.Color
	// FgHover 非零时悬停（或聚焦）后文字色在 150 ms 里补间到它（hover:text-foreground）。
	FgHover tokens.Color
	// Underline 让文字一直带下划线；UnderlineHover 只在悬停时带（hover:underline）。
	Underline, UnderlineHover bool
	// Hidden 让按钮透明但仍可点、仍在可访问树里（opacity-0 group-hover:opacity-100：显隐只是不透明度）。
	Hidden bool
	// Disabled 让按钮不可点并降低不透明度（disabled:opacity-40）。
	Disabled bool
	// Groups 与 Group 非空时把命中区登记到悬停分组里（group-hover 用）：行内的每个命中区都要登记，指针才算还在行内。
	Groups *kit.HoverGroups
	Group  string
	Hint   string
	// Icon 非零时文字前有一枚图标（IconSize 缺省 0.75rem），与文字同色，间距 Gap（缺省 0.25rem）。
	Icon     gfx.Icon
	IconSize float32
	Gap      float32
	// Expanded 非 nil 时按钮是展开收起钮（aria-expanded）。
	Expanded *bool
	// Radius 是聚焦环的圆角；0 取 0.25rem。
	Radius float32
}

// textButton 声明一枚行内文字按钮，返回这一帧是否被点了。c 由调用方带键，键决定命中身份。
func (s *Screen) textButton(c ui.C, label string, b textBtn) bool {
	l := s.Look
	id := ui.ID(c)
	if b.Groups != nil && b.Group != "" {
		b.Groups.Add(b.Group, id)
	}
	opts := gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Hint: b.Hint, Disabled: b.Disabled}
	if b.Expanded != nil {
		opts.Expandable, opts.Expanded = true, *b.Expanded
	}
	if b.Disabled {
		opts.Cursor = gfx.PointerCursorDefault
	}
	box := ui.Row(c).Shrink(0).HitWith(id, label, opts)
	l.TabStop(box)
	hovered := box.State().Hovered && !b.Disabled
	fg := b.Fg
	if b.FgHover != (tokens.Color{}) {
		p := l.HoverProgress(c, id, 0, hovered, kit.DurationDefault)
		fg = kit.MixToken(b.Fg, b.FgHover, p)
	}
	if b.Hidden {
		fg = fg.WithAlpha(0)
	} else if b.Disabled {
		fg = fg.WithAlpha(fg.A * 0.4)
	}
	radius := b.Radius
	if radius == 0 {
		radius = l.Rem(0.25)
	}
	gap := b.Gap
	if gap == 0 {
		gap = l.Space(1)
	}
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, l.T.Primary.WithAlpha(0.5), radius)
		}
	}).Items(ui.Center).Gap(gap).Children(func() {
		if b.Icon.ViewBox != 0 {
			size := b.IconSize
			if size == 0 {
				size = l.Rem(0.75)
			}
			l.Icon(c.Key("icon"), b.Icon, size, fg)
		}
		under := kit.Underline(b.Underline || (b.UnderlineHover && hovered))
		if b.Px > 0 {
			l.TextPx(c.Key("text"), label, b.Px, b.Line, b.Weight, fg, under)
			return
		}
		l.Text(c.Key("text"), label, b.Style, b.Weight, fg, under)
	})
	return box.Clicked() && !b.Disabled
}

// corner 是 paintCornered 里圆角较小的那个角。
type corner uint8

const (
	cornerBottomRight corner = iota
	cornerBottomLeft
)

// paintCornered 画一块「大圆角、其中一个角较小」的表面（rounded-2xl rounded-br-md / rounded-bl-md）：Viva 的表面只有一个统一的圆角，
// 所以把矩形切成互不重叠的三块——一块是除去小角那个象限的两条带（按大圆角画），一块是小角所在的象限（按小圆角画）。
// 三块的裁剪边都落在整设备像素上（Surface.Clip 吸附），同色相接没有缝；带半透明底色时也不会因重叠而加深。
func (s *Screen) paintCornered(d *ui.DrawContext, surf kit.Surface, at corner, small float32) {
	l := s.Look
	r, big := d.Rect, surf.Radius
	if small >= big || r.Width < 2*big || r.Height < 2*big {
		l.PaintSurface(d, surf)
		return
	}
	var quad, band, side gfx.Rect
	switch at {
	case cornerBottomLeft:
		quad = gfx.Rect{X: r.X, Y: r.Y + r.Height - big, Width: big, Height: big}
		band = gfx.Rect{X: r.X + big, Y: r.Y, Width: r.Width - big, Height: r.Height}
		side = gfx.Rect{X: r.X, Y: r.Y, Width: big, Height: r.Height - big}
	default:
		quad = gfx.Rect{X: r.X + r.Width - big, Y: r.Y + r.Height - big, Width: big, Height: big}
		band = gfx.Rect{X: r.X, Y: r.Y, Width: r.Width - big, Height: r.Height}
		side = gfx.Rect{X: r.X + r.Width - big, Y: r.Y, Width: big, Height: r.Height - big}
	}
	a, b, q := surf, surf, surf
	a.Clip, b.Clip = band, side
	q.Clip, q.Radius = quad, small
	l.PaintSurface(d, a)
	l.PaintSurface(d, b)
	l.PaintSurface(d, q)
}

// markAlert 把盒子标成带直播属性的区域（role="alert"）。Viva 没有 alert 角色，用 status + assertive 表达（台账 A-012）。
func markAlert(d *ui.DrawContext) {
	d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
}

// markStatus 把盒子标成礼貌的状态区域（role="status"）。
func markStatus(d *ui.DrawContext) {
	d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LivePolite})
}

// boxedInput 声明一个带边框的单行输入框（border border-border bg-background，聚焦时全局的 ring-2 ring-primary/50），
// 返回输入框这一帧是否有焦点。height 是整框高度（含边框）；pad 是左右内边距（不含边框）。
func (s *Screen) boxedInput(c ui.C, f *ui.Field, name, placeholder string, size, height, pad, radius float32) (focused bool) {
	l := s.Look
	t := l.T
	var ring float32
	ui.Column(c).FillWidth().Height(height).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: t.Border, BorderW: 1, Radius: radius})
			l.PaintInputRing(d, ring, radius)
		}).
		Children(func() {
			box := l.Input(c.Key("input"), f, name, placeholder, kit.InputSpec{
				Size: size, Weight: kit.WeightNormal, Color: t.Foreground, Placeholder: tokens.Tailwind("gray-400"),
				Height: height, Left: pad + 1, Right: pad + 1,
			}).FillWidth()
			focused = box.State().Focused
			// 渲染层的输入框没有写 transition-shadow：聚焦环立即出现，不补间。
			ring = 0
			if focused {
				ring = 1
			}
		})
	return focused
}

