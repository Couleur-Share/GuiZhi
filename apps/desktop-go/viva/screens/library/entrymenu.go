package library

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 条目菜单（渲染层 item-menus.tsx 的 useItemMenus）：卡片与表格共用同一套语义，按当前范围分支——
// 回收站只给恢复与彻底删除，其余范围给收藏、置顶、归档、移到回收站。

// entryMenuItems 是右键或「更多」在条目 e 上打开的菜单（buildEntryMenu）。
func (s *Screen) entryMenuItems(v View, e services.LibraryEntry, tags bool) []kit.MenuItem {
	// 右键落在多选组上时，动作作用于整组。
	if len(v.List.Selection) > 1 && v.List.IsSelected(e.ID) {
		return s.bulkMenuItems(v)
	}
	id := e.ID
	if v.Filters.Scope == services.LibraryScopeTrash {
		return []kit.MenuItem{
			{Label: locales.LibraryRestore(), Icon: icons.RotateCcw, OnSelect: func() { s.restore([]string{id}) }},
			{Label: locales.LibraryDeleteForever(), Icon: icons.CircleX, Destructive: true,
				OnSelect: func() { s.openConfirm(confirmDeleteForever, []string{id}) }},
		}
	}
	favorite := locales.LibraryFavorite()
	if e.IsFavorite {
		favorite = locales.LibraryUnfavorite()
	}
	pin := locales.LibraryPin()
	if e.IsPinned {
		pin = locales.LibraryUnpin()
	}
	items := []kit.MenuItem{
		{Label: favorite, Icon: icons.Star, OnSelect: func() { s.toggleFavorite(id) }},
		{Label: pin, Icon: icons.Pin, OnSelect: func() { s.togglePinned(id) }},
	}
	if tags {
		items = append(items, kit.MenuItem{Label: locales.LibraryEditTags(), Icon: icons.Tag, OnSelect: func() { s.openTagPicker(e) }})
	}
	items = append(items, kit.MenuItem{Label: locales.LibraryAiHandoff(), Icon: icons.Bot, Children: []kit.MenuItem{
		{Label: locales.LibraryAiHandoffCopyFull(), Icon: icons.ClipboardCopy, OnSelect: func() { s.copyHandoff(id, true) }},
		{Label: locales.LibraryAiHandoffCopyBrief(), Icon: icons.FileText, OnSelect: func() { s.copyHandoff(id, false) }},
		{Label: locales.LibraryAiHandoffSave(), Icon: icons.FileDown, OnSelect: func() { s.saveHandoff(id) }},
	}})
	if e.Status == "archived" {
		items = append(items, kit.MenuItem{Label: locales.LibraryUnarchive(), Icon: icons.ArchiveRestore,
			OnSelect: func() { s.setStatus([]string{id}, "active") }})
	} else {
		items = append(items, kit.MenuItem{Label: locales.LibraryArchive(), Icon: icons.Archive,
			OnSelect: func() { s.setStatus([]string{id}, "archived") }})
	}
	return append(items, kit.MenuItem{Label: locales.LibraryMoveToTrash(), Icon: icons.Trash2, Destructive: true,
		OnSelect: func() { s.trashWithUndo([]string{id}) }})
}

// bulkMenuItems 是多选组的菜单（buildBulkMenu）：回收站里恢复与彻底删除，其余归档与移到回收站。
// 菜单项每一帧按当前选择重新生成（渲染层每次渲染都重新 build，闭包里取的也是最新一次渲染的选择）。
func (s *Screen) bulkMenuItems(v View) []kit.MenuItem {
	ids := selection(v)
	n := len(ids)
	if v.Filters.Scope == services.LibraryScopeTrash {
		return []kit.MenuItem{
			{Label: locales.LibraryBulkRestore(n), Icon: icons.RotateCcw, OnSelect: func() { s.restore(ids) }},
			{Label: locales.LibraryBulkDeleteForever(n), Icon: icons.CircleX, Destructive: true,
				OnSelect: func() { s.openConfirm(confirmDeleteForever, ids) }},
		}
	}
	return []kit.MenuItem{
		{Label: locales.LibraryBulkArchive(n), Icon: icons.Archive, OnSelect: func() { s.setStatus(ids, "archived") }},
		{Label: locales.LibraryBulkMoveToTrash(n), Icon: icons.Trash2, Destructive: true, OnSelect: func() { s.trashWithUndo(ids) }},
	}
}

// moveMenuItems 是批量条「移动到合集」的菜单（buildMoveMenu）：先是「未归合集」，再是各合集。
func (s *Screen) moveMenuItems(v View) []kit.MenuItem {
	ids := selection(v)
	items := make([]kit.MenuItem, 0, len(v.Collections)+1)
	items = append(items, kit.MenuItem{Label: locales.LibraryNoCollection(), OnSelect: func() { s.moveToCollection(ids, "") }})
	for _, col := range v.Collections {
		collectionID := col.ID
		items = append(items, kit.MenuItem{Label: col.Name, OnSelect: func() { s.moveToCollection(ids, collectionID) }})
	}
	return items
}

// entryMenuView 声明条目菜单。每一帧都要经过 ContextMenu：关着的那一帧它负责复位。
// 条目取右键那一刻的快照（菜单开着时条目被改了，菜单文字不跟着变），菜单项只在开着的帧里生成，
// 「是否落在多选组上」按当前选择判断——渲染层的 buildEntryMenu 在每次渲染时求值，与这里一致。
func (s *Screen) entryMenuView(c ui.C, v View) {
	m := &s.bulk.entry
	var items []kit.MenuItem
	if m.open {
		items = s.entryMenuItems(v, m.entry, m.tags)
	}
	if s.Look.ContextMenu(c.Key("entry-menu"), &m.st, kit.MenuSpec{Open: m.open, X: m.at.X, Y: m.at.Y, Items: items}) {
		m.close()
	}
}

// moveMenuView 声明批量条的「移动到合集」菜单。
func (s *Screen) moveMenuView(c ui.C, v View) {
	m := &s.bulk.move
	var items []kit.MenuItem
	if m.open {
		items = s.moveMenuItems(v)
	}
	if s.Look.ContextMenu(c.Key("move-menu"), &m.st, kit.MenuSpec{Open: m.open, X: m.at.X, Y: m.at.Y, Items: items}) {
		m.close()
	}
}
