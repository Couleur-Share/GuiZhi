package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// rowMenu 是合集与标签行的菜单（渲染层 SidebarLibraryPanel 的 menuState 与 menuTriggerRef）。
type rowMenu struct {
	st    kit.MenuState
	open  bool
	x, y  float32
	items []kit.MenuItem
	// trigger 是打开菜单的「更多」按钮；右键打开时为零。「更多」按钮再次点击时据此收起，右键则始终重开。
	trigger gfx.ID
	// rects 是各「更多」按钮上一帧的外框：落在触发按钮上的点击只关闭菜单（Keep），键盘触发时菜单锚在按钮左下角。
	rects map[gfx.ID]gfx.Rect
	// up 是最近一次指针松开的位置：鼠标点「更多」时菜单开在光标处（渲染层 menuAnchor 取 clientX/clientY）。
	up gfx.Point
}

func (m *rowMenu) noteRect(id gfx.ID, r gfx.Rect) {
	if m.rects == nil {
		m.rects = map[gfx.ID]gfx.Rect{}
	}
	m.rects[id] = r
}

func (s *Screen) closeMenu() {
	s.menu.open, s.menu.trigger, s.menu.items = false, gfx.ID{}, nil
}

// toggleMenu 响应「更多」按钮的点击（渲染层 openRowMenu 的 click 分支）：菜单正由这枚按钮开着就收起，否则在 e.At 处打开。
func (s *Screen) toggleMenu(e moreEvent, items []kit.MenuItem) {
	m := &s.menu
	if m.open && m.trigger == e.ID {
		s.closeMenu()
		return
	}
	m.trigger, m.open, m.x, m.y, m.items = e.ID, true, e.At.X, e.At.Y, items
}

// popMenu 响应右键：始终在指针处重开，没有触发按钮。
func (s *Screen) popMenu(at gfx.Point, items []kit.MenuItem) {
	m := &s.menu
	m.trigger, m.open, m.x, m.y, m.items = gfx.ID{}, true, at.X, at.Y, items
}

// moreAnchor 是菜单该开的位置：键盘（Enter 或空格）点「更多」时是按钮的左下角，鼠标点则是光标处。
func (s *Screen) moreAnchor(canvas *gfx.Canvas, id gfx.ID) gfx.Point {
	if canvas.ClickedByKey(id) {
		r := s.menu.rects[id]
		return gfx.Point{X: r.X, Y: r.Y + r.Height}
	}
	return s.menu.up
}

// watchPointerUp 记下「更多」按钮上最近一次指针松开的位置。指针事件的那一遍绘制先于随后的点击那一遍。
func (s *Screen) watchPointerUp(canvas *gfx.Canvas, id gfx.ID) {
	if ev, ok := canvas.PointerOn(id); ok && ev.Kind == gfx.PointerUp {
		s.menu.up = gfx.Point{X: ev.X, Y: ev.Y}
	}
}

// watchContext 在行上（行本身、主按钮、「更多」按钮）右键松开时调用 fn：渲染层的 contextmenu 事件在 Windows 上随鼠标松开触发。
func (s *Screen) watchContext(canvas *gfx.Canvas, fn func(gfx.Point), ids ...gfx.ID) {
	for _, id := range ids {
		if ev, ok := canvas.PointerOn(id); ok && ev.Kind == gfx.PointerUp && ev.Button == gfx.PointerButtonSecondary {
			canvas.Consume()
			fn(gfx.Point{X: ev.X, Y: ev.Y})
			return
		}
	}
}

// rowMenuView 声明行菜单。每一帧都要经过 ContextMenu：关着的那一帧它负责复位。
func (s *Screen) rowMenuView(c ui.C, _ View) {
	m := &s.menu
	var keep *gfx.Rect
	if !m.trigger.IsZero() {
		r := m.rects[m.trigger]
		keep = &r
	}
	if s.Look.ContextMenu(c.Key("row-menu"), &m.st, kit.MenuSpec{Open: m.open, X: m.x, Y: m.y, Items: m.items, Keep: keep}) {
		s.closeMenu()
	}
}

// collectionMenuItems 是合集行的菜单：重命名、删除合集（不会删除其中的条目）。
func (s *Screen) collectionMenuItems(col services.LibraryCollection) []kit.MenuItem {
	return []kit.MenuItem{
		{Label: locales.LibraryRename(), Icon: icons.Pencil, OnSelect: func() { s.openEdit(editRenameCollection, col, services.LibraryTag{}) }},
		{
			Label: locales.LibraryDeleteCollection(), Description: locales.LibraryDeleteCollectionDesc(),
			Icon: icons.CircleX, Destructive: true, OnSelect: func() { s.deleteCollection(col) },
		},
	}
}

// tagMenuItems 是标签行的菜单：重命名、颜色（十个色点的子菜单）、删除标签。
func (s *Screen) tagMenuItems(tag services.LibraryTag) []kit.MenuItem {
	var colors []kit.MenuItem
	for _, tc := range tagColors() {
		key := tc.Key
		colors = append(colors, kit.MenuItem{Label: tc.Label(), Dot: tc.Dot, OnSelect: func() { s.setTagColor(tag.ID, key) }})
	}
	return []kit.MenuItem{
		{Label: locales.LibraryRename(), Icon: icons.Pencil, OnSelect: func() { s.openEdit(editRenameTag, services.LibraryCollection{}, tag) }},
		{Label: locales.LibraryTagColor(), Icon: icons.Tag, Children: colors},
		{Label: locales.LibraryDeleteTag(), Icon: icons.CircleX, Destructive: true, OnSelect: func() { s.deleteTag(tag) }},
	}
}

// setTagColor 换标签颜色（后台）。失败的提示由 Store 走变更失败通道。
func (s *Screen) setTagColor(id, colorKey string) {
	s.Store.Spawn(func() { s.Store.SetTagColor(s.Ctx, id, colorKey) })
}

// deleteCollection 删合集只丢合集本身，条目留作未归合集：先隐藏、8 秒内可撤销，不再弹确认（渲染层 deleteCollectionWithUndo）。
// 正筛着这个合集时范围回到「全部」；分面里残留的合集 ID 与渲染层一致（setScope 不清分面，台账登记）。
func (s *Screen) deleteCollection(col services.LibraryCollection) {
	deletion := s.Store.ScheduleDeleteCollection(s.Ctx, col.ID, 0)
	if deletion == nil {
		return
	}
	if s.Store.View().Filters.CollectionID == col.ID {
		s.Store.SetScope(s.Ctx, services.LibraryScopeAll)
	}
	s.undoToast(locales.LibraryCollectionDeleted(col.Name), deletion)
}

// deleteTag 删标签只摘掉标签本身，条目保留：规则同 deleteCollection。
func (s *Screen) deleteTag(tag services.LibraryTag) {
	deletion := s.Store.ScheduleDeleteTag(s.Ctx, tag.ID, 0)
	if deletion == nil {
		return
	}
	if s.Store.View().Filters.TagID == tag.ID {
		s.Store.SetScope(s.Ctx, services.LibraryScopeAll)
	}
	s.undoToast(locales.LibraryTagDeleted(tag.Name), deletion)
}

func (s *Screen) undoToast(message string, deletion *Deletion) {
	if s.Guard.Notify != nil {
		s.Guard.Notify.Undo(message, locales.CommonUndo(), deletion.Undo)
	}
}
