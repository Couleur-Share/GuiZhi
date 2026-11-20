package library

import (
	"errors"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 详情头部的菜单与外部动作：「更多」菜单（渲染层 MoreActionsButton）、来源链接的打开与重新采集、尚未迁移的入口。

// detailMore 是头部的「更多」钮与它的菜单：aria-haspopup="menu"、aria-expanded。菜单靠右对齐按钮右缘（渲染层 rect.right - 160），
// 开在按钮下方 4px；菜单开着时再点按钮是收起。
func (s *Screen) detailMore(c ui.C, item services.LibraryItem, caps ItemCaps) {
	l := s.Look
	d := &s.detail
	mc := c.Key("more")
	style := s.actionButtonStyle()
	box := s.inlineButton(mc, inlineSpec{
		Name: locales.LibraryMoreActions(), Popup: true, Open: d.more.open,
		Fg: style.Fg, FgHover: style.FgHover, BgHover: style.BgHover,
		Radius: style.Radius, Height: style.Height, Width: style.Width, Ring: style.Ring, Rect: &d.moreRect,
	}, func(fg tokens.Color) {
		l.Icon(mc.Key("icon"), icons.Ellipsis, l.Rem(1), fg)
	})
	if box.Clicked() {
		if d.more.open {
			d.more.close()
		} else {
			r := d.moreRect
			d.more.show(gfx.Point{X: r.X + r.Width - moreMenuWidth, Y: r.Y + r.Height + 4})
		}
	}
	s.moreMenuView(c, item, caps)
}

// moreMenuView 声明「更多」菜单。每一帧都要经过 ContextMenu：关着的那一帧它负责复位。
func (s *Screen) moreMenuView(c ui.C, item services.LibraryItem, caps ItemCaps) {
	d := &s.detail
	var items []kit.MenuItem
	if d.more.open {
		items = s.moreMenuItems(item, caps)
	}
	keep := d.moreRect
	if s.Look.ContextMenu(c.Key("more-menu"), &d.more.st, kit.MenuSpec{
		Open: d.more.open, X: d.more.at.X, Y: d.more.at.Y, Items: items, Keep: &keep,
	}) {
		d.more.close()
	}
}

// moreMenuItems 是「更多」菜单的内容：文章信息与工具、（来源支持时的）来源评论，然后是置顶、归档与移到回收站。
// 文章工具侧栏与来源评论随 M2-6 迁入：那两项画出来、点击说明归属（台账 B-060）。
func (s *Screen) moreMenuItems(item services.LibraryItem, caps ItemCaps) []kit.MenuItem {
	tools := locales.ArticleReaderTools()
	items := []kit.MenuItem{
		{Label: tools, Icon: icons.SlidersHorizontal, OnSelect: func() { s.notMigrated(tools, "M2-6", "B-060") }},
	}
	if caps.CommentsSupported {
		// 已有来源评论时渲染层写「查看来源评论」；评论随 M2-6 读取，这里恒为「采集评论」。
		label := locales.LibraryCollectSourceComments()
		items = append(items, kit.MenuItem{Label: label, Icon: icons.MessageCircle, OnSelect: func() { s.notMigrated(label, "M2-6", "B-060") }})
	}
	id := item.ID
	pin := locales.LibraryPin()
	if item.IsPinned {
		pin = locales.LibraryUnpin()
	}
	items = append(items, kit.MenuItem{Label: pin, Icon: icons.Pin, OnSelect: func() { s.togglePinned(id) }})
	if item.Status == services.LibraryStatusArchived {
		items = append(items, kit.MenuItem{Label: locales.LibraryUnarchive(), Icon: icons.ArchiveRestore,
			OnSelect: func() { s.setStatus([]string{id}, services.LibraryStatusActive) }})
	} else {
		items = append(items, kit.MenuItem{Label: locales.LibraryArchive(), Icon: icons.Archive,
			OnSelect: func() { s.setStatus([]string{id}, services.LibraryStatusArchived) }})
	}
	return append(items, kit.MenuItem{Label: locales.LibraryMoveToTrash(), Icon: icons.Trash2, Destructive: true,
		OnSelect: func() { s.trashWithUndo([]string{id}) }})
}

// notMigrated 是尚未迁移的入口的统一反应：弹一条失败提示，把它归属的里程碑与台账编号写进可复制的详情（台账 B-060）。
// 不进应用错误日志：它不是故障，点一次记一条只会淹没真正的错误。
func (s *Screen) notMigrated(action, milestone, ledger string) {
	if s.Guard.Notify == nil {
		return
	}
	s.Guard.Notify.Error(locales.CommonOperationFailed(action), locales.NativeLibraryNotMigrated(action, milestone, ledger))
}

// errNoOpener 说明没有接线「用系统浏览器打开链接」（只在测试与对照里出现）。
var errNoOpener = errors.New("没有接线外部浏览器")

// openSource 用系统默认浏览器打开来源链接（渲染层 <a target=_blank>）；失败弹出带原因的提示。
func (s *Screen) openSource(href string) {
	action := locales.LibraryMediaMetaOpenSource()
	open := s.OpenExternal
	if open == nil {
		s.Guard.Report(action, errNoOpener)
		return
	}
	if err := open(href); err != nil {
		s.Guard.Report(action, err)
	}
}

// refreshSource 是来源 chip 旁的「重新采集并待确认」：公众号走补采，其余把来源链接排进采集队列。
// 刷新结果故意进「未分类」并打上「待确认来源更新」标签，且强制建副本：它既不能覆盖原条目，也不该悄悄混回已整理的知识库，
// 等用户在导入队列里逐项对比后再决定（渲染层 SourceChip 同）。
func (s *Screen) refreshSource(item services.LibraryItem, sv sourceView) {
	if sv.wechat {
		ids := []string{item.ID}
		s.Store.Spawn(func() { s.Store.SupplementWechat(s.Ctx, ids) })
		return
	}
	action := locales.LibraryRefreshSource()
	if s.Imports == nil {
		s.Guard.Report(action, errNoImports)
		return
	}
	tags := make([]string, 0, len(item.Tags)+1)
	for _, tag := range item.Tags {
		tags = append(tags, tag.Name)
	}
	tags = append(tags, locales.LibrarySourceRefreshTag())
	input := services.EnqueueImportInput{
		Kind: "url", Input: sv.href, CollectionID: nil, RefreshOfItemID: item.ID, TagNames: tags, ForceDuplicate: true,
	}
	s.Guard.Go(action, func() error {
		_, err := s.Imports.Enqueue(s.Ctx, []services.EnqueueImportInput{input})
		return err
	}, nil)
}

// errNoImports 说明没有接线采集队列（只在测试与对照里出现）。
var errNoImports = errors.New("没有接线采集队列")
