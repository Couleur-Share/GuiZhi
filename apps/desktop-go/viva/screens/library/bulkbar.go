package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// selectionBar 是列表栏顶部：有多选时是批量条，否则是工具条（渲染层 ItemList 与 ItemTableView 的
// `selectionIds.length > 0 ? <ItemBulkBar /> : <ItemListToolbar />`）。两者互相卸载：被换下去的那一个，
// 它的菜单、浮层与展开状态随之丢掉（渲染层里它们是组件自己的状态）。
// wide 为真是表格视图（图标带文字），否则是卡片视图（只放图标，文案走提示）。
func (s *Screen) selectionBar(c ui.C, v View, wide bool) {
	if len(v.List.Selection) == 0 {
		s.bulk.resetBar()
		s.listToolbar(c, v)
		return
	}
	s.pane.resetToolbar()
	s.bulkBar(c, v, wide)
}

// resetBar 丢掉批量条自己的浮层状态（条卸载）。
func (b *bulkState) resetBar() {
	b.move.close()
	b.bulkTag.pop.Close()
}

// resetToolbar 丢掉工具条自己的浮层状态（条卸载）。
func (p *listPane) resetToolbar() {
	p.sortOpen = false
	p.filter.Close()
	p.toolbarFocus = toolbarFocusNone
}

// bulkBar 是批量操作条（渲染层 ItemBulkBar）：flex h-9 shrink-0 items-center gap-1 border-b border-border bg-primary/5，
// 宽版 px-4、窄版 px-3。回收站里是「恢复」与「彻底删除」，其余范围是移动、打标签、收藏、归档、移到回收站；
// 选中的条目里有微信公众号来源的，再多一个补采钮。
func (s *Screen) bulkBar(c ui.C, v View, wide bool) {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	ids := selection(v)
	trash := v.Filters.Scope == services.LibraryScopeTrash
	padX := l.Space(3)
	if wide {
		padX = l.Space(4)
	}

	// 微信公众号来源：只看本页上的条目（渲染层的 entries 就是当前页）。
	var wechat []string
	for _, id := range ids {
		for _, e := range v.List.Entries {
			if e.ID == id && e.Platform == "wechat" {
				wechat = append(wechat, id)
				break
			}
		}
	}

	ui.Row(c.Key("bulk-bar")).FillWidth().Shrink(0).Height(l.Rem(2.25)).Items(ui.Center).Gap(l.Space(1)).
		PadSides(0, padX, 1, padX).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.05)})
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border)
		}).Children(func() {
		// 取消选择：inline-flex h-7 w-7 rounded-md text-muted-foreground hover:bg-muted/60 hover:text-foreground
		clearLabel := locales.LibraryClearSelection()
		size := l.Rem(1.75)
		if l.Button(c.Key("clear"), kit.ButtonSpec{Name: clearLabel, Icon: icons.X, Center: true, Hint: clearLabel}, kit.ButtonStyle{
			Height: size, Width: size, Radius: l.Rem(0.75) - 2,
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
		}).Clicked() {
			s.Store.ClearSelection()
		}

		// min-w-0 flex-1 truncate text-xs font-medium text-foreground
		l.Text(c.Key("count"), locales.LibrarySelectedCount(len(ids)), kit.TextXS, kit.WeightMedium, t.Foreground, kit.Ellipsis()).
			Grow(1).Shrink(1).MinWidth(0)

		if trash {
			if s.bulkAction(c, wide, bulkActionSpec{key: "restore", label: locales.LibraryRestore(), icon: icons.RotateCcw}).clicked {
				s.restore(ids)
			}
			if s.bulkAction(c, wide, bulkActionSpec{key: "delete-forever", label: locales.LibraryDeleteForever(), icon: icons.CircleX, destructive: true}).clicked {
				s.openConfirm(confirmDeleteForever, ids)
			}
		} else {
			// 菜单与浮层开在点击的光标处（渲染层取 event.clientX / clientY），键盘点击是原点。
			if r := s.bulkAction(c, wide, bulkActionSpec{key: "move", label: locales.LibraryBulkMoveTitle(), icon: icons.FolderInput}); r.clicked {
				s.bulk.move.show(s.pointerAnchor(canvas, r.id))
			}
			if r := s.bulkAction(c, wide, bulkActionSpec{key: "tag", label: locales.LibraryBulkAddTag(), icon: icons.Tag}); r.clicked {
				s.bulk.bulkTag.show(s.pointerAnchor(canvas, r.id))
			}
			if s.bulkAction(c, wide, bulkActionSpec{key: "favorite", label: locales.LibraryBulkFavorite(), icon: icons.Star}).clicked {
				s.bulkUpdate(ids, services.LibraryEntryPatch{IsFavorite: services.BoolPtr(true)})
			}
			if s.bulkAction(c, wide, bulkActionSpec{key: "archive", label: locales.LibraryArchive(), icon: icons.Archive}).clicked {
				s.setStatus(ids, "archived")
			}
			if s.bulkAction(c, wide, bulkActionSpec{key: "trash", label: locales.LibraryMoveToTrash(), icon: icons.Trash2, destructive: true}).clicked {
				s.trashWithUndo(ids)
			}
		}
		if !trash && len(wechat) > 0 {
			if s.bulkAction(c, wide, bulkActionSpec{key: "wechat", label: locales.LibraryBulkSupplementWechat(), icon: icons.RotateCcw}).clicked {
				s.supplementWechat(wechat)
			}
		}
	})
	s.moveMenuView(c, v)
	s.bulkTagView(c, v)
}

// bulkActionSpec 描述批量条里的一枚动作。
type bulkActionSpec struct {
	key, label  string
	icon        gfx.Icon
	destructive bool
}

// bulkActionResult 是一枚动作的结果：有没有被点击，以及它的命中盒标识（打开菜单时取键盘还是鼠标的锚点）。
type bulkActionResult struct {
	clicked bool
	id      gfx.ID
}

// bulkAction 是批量条里的一枚动作（渲染层 BulkAction）：inline-flex h-7 shrink-0 items-center justify-center gap-1.5
// rounded-md transition-colors，危险的是 text-destructive hover:bg-destructive/10，其余 text-foreground hover:bg-muted/60；
// 宽版 px-2.5 text-xs、图标带文字（不挂 title，同一句话不弹两遍），窄版 w-7 只有图标、文案走 title。
func (s *Screen) bulkAction(c ui.C, wide bool, spec bulkActionSpec) bulkActionResult {
	l := s.Look
	t := l.T
	var id gfx.ID
	bs := kit.ButtonSpec{Name: spec.label, Icon: spec.icon, IDOut: &id}
	st := kit.ButtonStyle{
		Height: l.Rem(1.75), Radius: l.Rem(0.75) - 2, Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.Foreground, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
	}
	if spec.destructive {
		st.Fg, st.BgHover = t.Destructive, t.Destructive.WithAlpha(0.1)
	}
	if wide {
		bs.Label = spec.label
		st.PadX = l.Space(2.5)
	} else {
		bs.Center, bs.Hint = true, spec.label
		st.Width = l.Rem(1.75)
	}
	box := l.Button(c.Key(spec.key), bs, st)
	// 打开菜单的钮要记下指针松开的位置：指针事件的那一遍绘制先于随后的点击那一遍，所以在点击被读到之前登记。
	s.watchPointerUp(c.Canvas(), id)
	return bulkActionResult{clicked: box.Clicked(), id: id}
}
