package capture

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 标签行：已加的标签小块、输入框，输入时在行下面浮出已有标签的建议（渲染层 CaptureDialog 的标签区）。

// tagRowView 是标签行：relative flex flex-wrap items-center gap-1.5 rounded-xl border border-border px-3 py-2。
// 输入框回车添加；点建议添加（建议不抢走输入框的焦点）；点小块上的 × 移除。
//
// 渲染层里「输入框为空时退格删掉最后一个标签」在这里做不到：Viva（v0.6.4，Windows 与无头窗口一致）在文本框取得键盘时把
// Backspace 当作文本编辑命令直接消化，既不交给 OnKey、也不交给 OnShortcut（只有可打印字符与 Ctrl/Cmd+Enter 才会），
// 输入已空时它什么也不改，应用就无从得知。移除只剩点 ×（它是可 Tab 聚焦的按钮，键盘也能用）。
func (d *Dialog) tagRowView(c ui.C, st state) {
	l := d.Look
	t := l.T
	row := ui.Flow(c.Key("tags")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).LineGap(l.Space(1.5)).
		PadSides(l.Space(2)+1, l.Space(3)+1, l.Space(2)+1, l.Space(3)+1).
		Draw(func(dc *ui.DrawContext) {
			d.tagRow = dc.Rect
			l.PaintSurface(dc, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		})
	row.Children(func() {
		l.Icon(c.Key("icon"), icons.Tag, l.Rem(0.875), t.MutedForeground).Shrink(0)
		for _, name := range d.tagNames {
			if d.tagChip(c.Key("chip-"+name), name) {
				d.removeTag(name)
			}
		}
		placeholder := locales.CaptureTagsPlaceholder()
		input := l.Input(c.Key("tag-input"), &d.tagDraft, placeholder, placeholder, kit.InputSpec{
			Size: l.Rem(0.75), Weight: kit.WeightNormal, Color: t.Foreground, Placeholder: t.MutedForeground.WithAlpha(0.6),
			Height: l.Rem(1.5),
		}).Grow(1).Shrink(1).Basis(0).MinWidth(128) // h-6 min-w-32 flex-1
		if input.Submitted() {
			d.addTag(d.tagDraft.Text())
		}
	})
	if matches := d.matchingTags(st.tags); len(matches) > 0 {
		d.suggestions(c.Key("suggest"), matches)
	}
}

// tagChip 是一个已加的标签：inline-flex h-6 items-center gap-1 rounded-full bg-muted px-2.5 text-xs text-foreground，后面一枚移除钮。
// 返回是否点了移除。
func (d *Dialog) tagChip(c ui.C, name string) (remove bool) {
	l := d.Look
	t := l.T
	ui.Row(c).Shrink(0).Height(l.Rem(1.5)).Items(ui.Center).Gap(l.Space(1)).PadSides(0, l.Space(2.5), 0, l.Space(2.5)).
		Draw(func(dc *ui.DrawContext) { l.PaintSurface(dc, kit.Surface{Bg: t.Muted, Radius: 9999}) }).
		Children(func() {
			l.Text(c.Key("name"), name, kit.TextXS, kit.WeightNormal, t.Foreground)
			remove = d.chipRemove(c.Key("remove"), name, t.Foreground)
		})
	return remove
}

// chipRemove 是小块里的移除钮：rounded-full opacity-60 transition-opacity hover:opacity-100，里面一枚 h-3 w-3 的 X；可访问名是「移除标签 {name}」。
func (d *Dialog) chipRemove(c ui.C, name string, fg tokens.Color) bool {
	l := d.Look
	t := l.T
	size := l.Rem(0.75)
	id := ui.ID(c)
	box := ui.Column(c).Size(size, size).Shrink(0).
		HitWith(id, locales.LibraryRemoveTag(name), gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	p := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	ink := fg.WithAlpha(fg.A * (0.6 + 0.4*p))
	box.Draw(func(dc *ui.DrawContext) {
		if dc.State.FocusVisible {
			l.PaintRingBand(dc, 2, t.Primary.WithAlpha(0.5), 9999)
		}
	}).Children(func() {
		l.Icon(c.Key("icon"), icons.X, size, ink)
	})
	return box.Clicked()
}

// suggestions 是输入框下面的已有标签建议：role=listbox 的浮层，absolute left-8 right-2 top-full z-20 mt-1，
// rounded-lg border border-border bg-popover p-1 shadow-lg；每项 role=option，flex w-full rounded-md px-2 py-1.5 text-xs，悬停 bg-accent。
//
// 渲染层里它是行内的绝对定位元素，会被对话框内容区的滚动裁掉；这里画在对话框之上的浮层里，不被裁，也不改变版面。
// 标签行还没画过（外框未知）时不画。选项不取焦点（渲染层 onMouseDown preventDefault），点一下输入框仍然在输入。
func (d *Dialog) suggestions(c ui.C, matches []services.LibraryTag) {
	row := d.tagRow
	if row.Width <= 0 {
		return
	}
	l := d.Look
	t := l.T
	rowH := l.Rem(1) + l.Space(3) // text-xs 的 1rem 行高加 py-1.5
	pad := 1 + l.Space(1)         // 边框加 p-1
	left, right := l.Rem(2), l.Rem(0.5)
	rect := gfx.Rect{
		// 绝对定位相对标签行的内边距盒（去掉 1px 边框）：top-full 是内边距盒的下沿，再加 mt-1。
		X: row.X + 1 + left, Y: row.Y + row.Height - 1 + l.Space(1),
		Width: row.Width - 2 - left - right, Height: 2*pad + float32(len(matches))*rowH,
	}
	// 浮层按层号画在对话框之后、已在 Modal 的焦点作用域之外：借用这个作用域，选项才点得到。
	panel := l.WithinModalHooks(&d.modal, ui.Overlay(c, ui.OverlaySpec{Band: kit.BandOverIslandInModal, Bounds: rect}).Pad(pad),
		func(dc *ui.DrawContext) {
			l.PaintSurface(dc, kit.Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75), Shadows: kit.ShadowLG})
			// 面板是不透明的：按在它的边距上不能落到下面的对话框（那会把焦点抢走）。
			dc.Canvas.Block(dc.Rect)
			dc.Canvas.ContainerWithOptions(dc.Rect, gfx.RoleListBox, locales.CaptureExistingTags(), gfx.HitOptions{})
		}, nil)
	panel.Children(func() {
		for _, tag := range matches {
			if d.suggestion(c.Key("opt-"+tag.ID), tag.Name) {
				d.addTag(tag.Name)
			}
		}
	})
}

// suggestion 是建议里的一项。返回是否被点了。
func (d *Dialog) suggestion(c ui.C, name string) bool {
	l := d.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Items(ui.Center).PadSides(l.Space(1.5), l.Space(2), l.Space(1.5), l.Space(2)).
		HitWith(id, name, gfx.HitOptions{Role: gfx.RoleOption, SkipFocus: true, SkipTab: true, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	box.Draw(func(dc *ui.DrawContext) {
		if hovered {
			l.PaintSurface(dc, kit.Surface{Bg: t.Accent, Radius: l.Rem(0.75) - 2})
		}
	}).Children(func() {
		l.Text(c.Key("name"), name, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
	})
	return box.Clicked()
}
