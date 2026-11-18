package library

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// moreEvent 是「更多」按钮被点击：触发按钮的身份与菜单该打开的位置。
type moreEvent struct {
	ID gfx.ID
	At gfx.Point
}

// rowSpec 描述一行（渲染层 LibrarySidebarRow 的属性）。
type rowSpec struct {
	// Key 在侧栏里唯一：行的身份与悬停分组都取它。
	Key string
	// Icon 画 16×16 图标槽里的内容；fg 是行此刻的文字色（图标取 currentColor 时用它）。
	Icon  func(c ui.C, fg tokens.Color)
	Label string
	// Count 为 0 时不画计数；大于 99 写 99+。
	Count  int
	Active bool
	// More 非空时行尾有「更多」按钮，值是它的可访问名；OnMore 在它被点击时调用。
	More   string
	OnMore func(moreEvent)
	// OnContext 非空时，行上任意位置右键（松开）都会调用，位置是指针处。
	OnContext func(at gfx.Point)
	OnClick   func()
}

const rowMinHeightPx = 32 // min-h-[32px]：px 字面量，不随字号档缩放

// sidebarRow 声明一行（渲染层 LibrarySidebarRow，知识库侧栏的通用行：智能视图、合集、平台、标签、回收站）。
//
// group/row relative flex min-h-[32px] items-center rounded-lg transition-colors duration-smooth：选中 bg-primary/15 text-primary，
// 否则 text-sidebar-foreground/70，悬停 bg-sidebar-accent/50 text-sidebar-foreground。行里是整行的主按钮（图标、名称）
// 与行尾 24px 的槽：槽里的计数与「更多」按钮共用一个位置，悬停或焦点在行内时交叉淡入淡出，行内元素不位移。
func (s *Screen) sidebarRow(c ui.C, spec rowSpec) {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	hasMore := spec.More != ""
	// Viva 的 Key 只命名「下一个元素」，且同一容器里不能重名：行、主按钮与「更多」按钮的身份都在这里（行所在的容器）里
	// 取，名字带上行的唯一键。
	rowID := ui.ID(c.Key("row-" + spec.Key))
	mainID := ui.ID(c.Key("main-" + spec.Key))
	ids := []gfx.ID{rowID, mainID}
	var moreID gfx.ID
	if hasMore {
		moreID = ui.ID(c.Key("more-" + spec.Key))
		ids = append(ids, moreID)
	}

	// 悬停与「焦点在行内」按整行算（CSS 的 group-hover / group-focus-within）：行本身与两枚按钮都登记进同一个分组。
	for _, id := range ids {
		s.groups.Add(spec.Key, id)
	}
	hovered := s.groups.Hovered(canvas) == spec.Key
	within := s.groups.Focused(canvas) == spec.Key

	// 悬停与选中各一个进度量：悬停中点选时底色从 accent/50 补间到 primary/15，与 CSS 的过渡一致。
	ph := l.HoverProgress(c, rowID, 0, hovered, kit.DurationSmooth)
	pa := l.HoverProgress(c, rowID, 1, spec.Active, kit.DurationSmooth)
	bg := kit.MixToken(kit.MixToken(tokens.Transparent, t.SidebarAccent.WithAlpha(0.5), ph), t.Primary.WithAlpha(0.15), pa)
	fg := kit.MixToken(kit.MixToken(t.SidebarForeground.WithAlpha(0.7), t.SidebarForeground, ph), t.Primary, pa)

	var mainBox, moreBox ui.Box
	ui.Row(c.Key("box-"+spec.Key)).FillWidth().MinHeight(rowMinHeightPx).Items(ui.Center).
		HitWith(rowID, "", gfx.HitOptions{SkipFocus: true, SkipTab: true}).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75)}) }).
		Children(func() {
			mainBox = s.rowMain(c.Key("main"), mainID, spec, fg)
			s.rowSlot(c.Key("slot"), spec, moreID, hovered, within, fg, &moreBox)
		})

	if mainBox.Clicked() && spec.OnClick != nil {
		spec.OnClick()
	}
	if hasMore && moreBox.Clicked() && spec.OnMore != nil {
		spec.OnMore(moreEvent{ID: moreID, At: s.moreAnchor(canvas, moreID)})
	}
	if spec.OnContext != nil {
		s.watchContext(canvas, spec.OnContext, ids...)
	}
	if hasMore {
		s.watchPointerUp(canvas, moreID)
	}
}

// rowMain 是行的主按钮：flex min-w-0 flex-1 items-center gap-2 rounded-lg py-1.5 pl-3 pr-1 text-left outline-none
// focus-visible:ring-2 focus-visible:ring-primary/40。名称取自内容（图标 aria-hidden），aria-pressed 是选中。
func (s *Screen) rowMain(c ui.C, id gfx.ID, spec rowSpec, fg tokens.Color) ui.Box {
	l := s.Look
	t := l.T
	box := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(1.5), l.Space(1), l.Space(1.5), l.Space(3)).
		HitWith(id, spec.Label, gfx.HitOptions{Role: gfx.RoleButton, Selected: spec.Active, Cursor: gfx.PointerCursorHand})
	l.TabStop(box) // 侧栏滚动区里的 Tab 停靠点：滚出视野时由 kit 滚进来（台账 B-030）
	weight := kit.WeightNormal
	if spec.Active {
		weight = kit.WeightMedium
	}
	return box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.4), l.Rem(0.75))
		}
	}).Children(func() {
		// 图标槽：flex h-4 w-4 shrink-0 items-center justify-center text-sm leading-none
		ui.Row(c.Key("icon")).Size(l.Rem(1), l.Rem(1)).Shrink(0).Items(ui.Center).Justify(ui.Center).Children(func() {
			spec.Icon(c.Key("glyph"), fg)
		})
		l.Text(c.Key("label"), spec.Label, kit.TextSM, weight, fg, kit.Ellipsis()).Shrink(1).MinWidth(0)
	})
}

// rowSlot 是行尾的槽：relative mr-1.5 flex h-6 w-6 shrink-0 items-center justify-center。
// 计数 text-[10px] leading-4（有「更多」按钮时，悬停或焦点在行内就淡出）与「更多」按钮 absolute inset-0（悬停或键盘聚焦时淡入）。
func (s *Screen) rowSlot(c ui.C, spec rowSpec, moreID gfx.ID, hovered, within bool, fg tokens.Color, more *ui.Box) {
	l := s.Look
	t := l.T
	size := l.Rem(1.5)
	ui.Stack(c).Size(size, size).Shrink(0).MarginRight(l.Space(1.5)).Children(func() {
		if spec.Count > 0 {
			color := t.SidebarForeground.WithAlpha(0.4)
			if spec.Active {
				color = t.Primary.WithAlpha(0.7)
			}
			po := float32(1)
			if spec.More != "" {
				// group-hover/row:opacity-0 group-focus-within/row:opacity-0，transition-opacity duration-quick
				po = l.HoverProgress(c, moreID, 1, !(hovered || within), kit.DurationQuick)
			}
			ui.Row(c.Key("count")).Absolute().Inset(0).Items(ui.Center).Justify(ui.Center).Opacity(po).Children(func() {
				l.TextPx(c.Key("n"), countLabel(spec.Count), 10, 16, kit.WeightNormal, color)
			})
		}
		if spec.More != "" {
			*more = s.moreButton(c.Key("more"), moreID, spec.More, hovered, fg)
		}
	})
}

// countLabel 是计数的写法：超过 99 写 99+。
func countLabel(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// moreButton 是「更多」按钮：absolute inset-0 flex items-center justify-center rounded opacity-0 transition-opacity duration-quick
// hover:bg-foreground/10 focus-visible:opacity-100 group-hover/row:opacity-100，里面是 16px 的省略号。
// 只有可访问名，没有 title：省略号的含义不必解释，而它一点就开菜单，气泡除了和菜单挤在一起没有别的作用。
func (s *Screen) moreButton(c ui.C, id gfx.ID, name string, rowHovered bool, fg tokens.Color) ui.Box {
	l := s.Look
	t := l.T
	box := ui.Row(c).Absolute().Inset(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	state := box.State()
	po := l.HoverProgress(c, id, 0, rowHovered || state.FocusVisible, kit.DurationQuick)
	return box.Opacity(po).Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.25)) // 全局 button:focus-visible
		}
		if d.State.Hovered {
			l.PaintSurface(d, kit.Surface{Bg: t.Foreground.WithAlpha(0.1), Radius: l.Rem(0.25)}) // hover:bg-foreground/10，rounded
		}
		s.menu.noteRect(id, d.Rect)
	}).Children(func() {
		l.Icon(c.Key("icon"), icons.Ellipsis, l.Rem(1), fg) // 按钮没有自己的文字色：继承行的 currentColor
	})
}
