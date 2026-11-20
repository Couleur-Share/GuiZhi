package library

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 列设置（渲染层 ColumnConfigMenu）：勾选要显示的列，加一键恢复默认。切换之后菜单保持打开，方便一次调整多列；
// 只在点菜单与触发钮之外、按 Esc、再点触发钮时收起。复选框列与操作列是表格骨架，不在列表里。

// columnConfigButton 是「列设置」触发钮（aria-haspopup="menu" 的按钮在浏览器的可访问树里是 combobox）与它的菜单。
//
// inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors：
// 收起时 border-border text-muted-foreground，悬停 bg-accent text-foreground；展开时 border-primary/30 bg-primary/10 text-primary。
func (s *Screen) columnConfigButton(c ui.C) {
	l := s.Look
	t := l.T
	tb := &s.table
	tb.menu.Group.Begin()
	label := locales.LibraryColumnConfig()
	cc := c.Key("columns")
	id := ui.ID(cc)
	tb.menuTrigger = id
	tb.menu.Group.Add(kit.PopoverGroup, id)
	open := tb.menu.Open
	radius := l.Rem(0.75) - 2 // rounded-md
	box := ui.Row(cc).Shrink(0).Items(ui.Center).Gap(l.Space(1.5)).
		PadSides(l.Space(1.5)+1, l.Space(2.5)+1, l.Space(1.5)+1, l.Space(2.5)+1).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleCombobox, Value: label, Expandable: true, Expanded: open, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	// 展开时没有 hover: 变体（条件类整组换掉了），悬停不再改变外观。
	ph := l.HoverProgress(cc, id, 0, box.State().Hovered && !open, kit.DurationDefault)
	po := l.HoverProgress(cc, id, 1, open, kit.DurationDefault)
	fg := kit.MixToken(kit.MixToken(t.MutedForeground, t.Foreground, ph), t.Primary, po)
	bg := kit.MixToken(kit.MixToken(t.Accent.WithAlpha(0), t.Accent, ph), t.Primary.WithAlpha(0.1), po)
	border := kit.MixToken(t.Border, t.Primary.WithAlpha(0.3), po)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: radius})
	}).DrawOver(func(d *ui.DrawContext) { tb.menuRect = d.Rect }).Children(func() {
		l.Icon(cc.Key("icon"), icons.Settings, l.Rem(0.875), fg)
		l.Text(cc.Key("label"), label, kit.TextXS, kit.WeightNormal, fg)
	})
	if box.Clicked() {
		if tb.menu.Open {
			tb.menu.Close()
		} else {
			// 锚点在点击那一刻取一次：菜单右缘对齐触发钮右缘（right = 视口宽 - rect.right），top = rect.bottom + 4。
			tb.menu.Show(gfx.Point{X: tb.menuRect.X + tb.menuRect.Width, Y: tb.menuRect.Y + tb.menuRect.Height + 4})
		}
	}
	if !tb.menu.Open {
		return
	}
	insets := [4]float32{l.Space(2), 0, l.Space(2), 0} // py-2
	l.Popover(c.Key("columns-menu"), &tb.menu, kit.PopoverSpec{
		Name:        label,
		Width:       l.Rem(15), // w-60
		Radius:      l.Rem(0.75),
		Shadows:     kit.ShadowXL,
		Insets:      &insets,
		Role:        gfx.RoleMenu,
		AnchorRight: true,
		Persistent:  true,
		Trigger:     tb.menuRect,
		Content:     func() { s.columnMenuBody(c.Key("columns-menu")) },
	})
}

// columnMenuBody 是菜单里的内容：页眉、列表（max-h-72 可滚动）、页脚的「重置」。
func (s *Screen) columnMenuBody(c ui.C) {
	l := s.Look
	t := l.T
	label := locales.LibraryColumnConfig()
	// border-b border-border px-3 pb-2：标题行继承根字号的 1rem / 1.5rem 行高（比 text-xs 的行盒高），文字落在基线上。
	// 渲染层实测文字盒上沿离行顶 4、5、6px（根字号 14、16、18px，即 0.5rem-3px），下沿离行底恒为 3px
	// （字体上下缘取整后，行内基线以下比 text-xs 的行盒多出的部分）；按 0.3125rem / 0.1875rem 推算在小、大档各差 0.375px，
	// 大档上落笔取整差一行（M2-2 T2：table-columns-open 大字号档，台账 B-046 (6)）。
	ui.Column(c.Key("head")).FillWidth().PadSides(0, l.Space(3), l.Space(2)+1, l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			ui.Column(c.Key("title-line")).FillWidth().PadSides(l.Rem(0.5)-3, 0, 3, 0).Children(func() {
				// text-xs font-semibold uppercase tracking-wider
				l.TextTracked(c.Key("title"), strings.ToUpper(label), kit.TextXS, kit.WeightSemibold, t.MutedForeground, 0.05)
			})
			l.Text(c.Key("hint"), locales.LibraryColumnConfigHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).
				MarginTop(l.Space(0.5))
		})

	// max-h-72 overflow-y-auto py-1
	l.ScrollAreaView(c.Key("list"), [4]float32{l.Space(1), 0, l.Space(1), 0}, func(kit.ScrollView) {
		for _, col := range s.table.cols {
			if col.ID == colCheckbox || col.ID == colActions {
				continue
			}
			s.columnMenuItem(c, col)
		}
	}).FillWidth().MaxHeight(l.Rem(18))

	// border-t border-border pt-1：「重置」。
	ui.Column(c.Key("foot")).FillWidth().PadSides(1+l.Space(1), 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
		Children(func() { s.columnMenuReset(c) })
}

// columnMenuItem 是一列的开关：flex w-full items-center gap-3 px-3 py-1.5 text-left transition-colors hover:bg-accent；
// 显示中是主色的睁眼图标与前景色文字，隐藏时是灰色的闭眼图标与灰色文字。
func (s *Screen) columnMenuItem(c ui.C, col tableColumn) {
	l := s.Look
	t := l.T
	ic := c.Key("item-" + string(col.ID))
	id := ui.ID(ic)
	box := ui.Row(ic).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(3)).
		PadSides(l.Space(1.5), l.Space(3), l.Space(1.5), l.Space(3)).
		HitWith(id, col.Label(), gfx.HitOptions{Role: gfx.RoleMenuItem, Selected: col.Visible, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	p := l.HoverProgress(ic, id, 0, box.State().Hovered, kit.DurationDefault)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), 0)
		}
		l.PaintSurface(d, kit.Surface{Bg: kit.MixToken(t.Accent.WithAlpha(0), t.Accent, p)})
	}).Children(func() {
		text := t.MutedForeground
		if col.Visible {
			l.Icon(ic.Key("icon"), icons.Eye, l.Rem(1), t.Primary)
			text = t.Foreground
		} else {
			l.Icon(ic.Key("icon"), icons.EyeOff, l.Rem(1), t.MutedForeground)
		}
		l.Text(ic.Key("label"), col.Label(), kit.TextSM, kit.WeightNormal, text, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
	})
	if box.Clicked() {
		s.toggleColumn(col.ID)
	}
}

// columnMenuReset 是「重置」：flex w-full items-center gap-2 px-3 py-1.5 text-sm text-muted-foreground transition-colors
// hover:bg-accent hover:text-foreground；恢复默认列并收起菜单。
func (s *Screen) columnMenuReset(c ui.C) {
	l := s.Look
	t := l.T
	rc := c.Key("reset")
	id := ui.ID(rc)
	label := locales.CommonReset()
	box := ui.Row(rc).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(1.5), l.Space(3), l.Space(1.5), l.Space(3)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	p := l.HoverProgress(rc, id, 0, box.State().Hovered, kit.DurationDefault)
	fg := kit.MixToken(t.MutedForeground, t.Foreground, p)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), 0)
		}
		l.PaintSurface(d, kit.Surface{Bg: kit.MixToken(t.Accent.WithAlpha(0), t.Accent, p)})
	}).Children(func() {
		l.Icon(rc.Key("icon"), icons.RotateCcw, l.Rem(1), fg)
		l.Text(rc.Key("label"), label, kit.TextSM, kit.WeightNormal, fg)
	})
	if box.Clicked() {
		s.resetColumns()
		s.table.menu.Close()
	}
}
