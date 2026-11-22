package wiki

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// moreMenu 是详情头部「更多」钮的菜单状态：开合、左上角位置、按钮外框（点按钮本身只收起、不再展开）。
type moreMenu struct {
	st   kit.MenuState
	open bool
	at   gfx.Point
	rect gfx.Rect
}

func (m *moreMenu) show(at gfx.Point) { m.open, m.at = true, at }
func (m *moreMenu) close()            { m.open = false }

// moreButton 是「更多」钮与它的菜单：inline-flex h-7 w-7 rounded-md border border-border text-muted-foreground hover:bg-accent hover:text-foreground，
// 菜单开在按钮下方 4px，横向取按钮中心（渲染层取点击的 clientX，落在按钮宽度之内）；菜单开着时再点按钮是收起。
func (s *Screen) moreButton(c ui.C, v View) {
	l := s.Look
	t := l.T
	d := &s.detail
	mc := c.Key("more")
	id := ui.ID(mc)
	box := ui.Row(mc).Shrink(0).Width(l.Rem(1.75)).Height(l.Rem(1.75)).Items(ui.Center).Justify(ui.Center)
	box, hover := s.hitButton(mc, id, box, locales.CommonMore(), "", false)
	bg := kit.MixToken(tokens.Transparent, t.Accent, hover)
	fg := s.ghostFg(hover)
	box.Draw(func(dc *ui.DrawContext) {
		d.more.rect = dc.Rect
		if dc.State.FocusVisible {
			l.PaintRing(dc, 2, t.Primary.WithAlpha(0.5), s.radiusMD())
		}
		l.PaintSurface(dc, kit.Surface{Bg: bg, Border: t.Border, BorderW: 1, Radius: s.radiusMD()})
	}).Children(func() {
		l.Icon(mc.Key("icon"), icons.Ellipsis, l.Rem(0.875), fg)
	})
	if box.Clicked() {
		if d.more.open {
			d.more.close()
		} else {
			r := d.more.rect
			d.more.show(gfx.Point{X: r.X + r.Width/2, Y: r.Y + r.Height + 4})
		}
	}
	s.moreMenuView(c, v)
}

// moreMenuView 声明「更多」菜单。每一帧都要经过 ContextMenu：关着的那一帧它负责复位。
func (s *Screen) moreMenuView(c ui.C, v View) {
	d := &s.detail
	var items []kit.MenuItem
	if d.more.open && v.Detail != nil {
		items = s.moreMenuItems(v)
	}
	keep := d.more.rect
	if s.Look.ContextMenu(c.Key("more-menu"), &d.more.st, kit.MenuSpec{
		Open: d.more.open, X: d.more.at.X, Y: d.more.at.Y, Items: items, Keep: &keep,
	}) {
		d.more.close()
	}
}

// moreMenuItems 是菜单内容：有历史版本时「恢复上一版」，手动编辑过时「交回自动编译」，最后是不可逆的「删除页面」。
func (s *Screen) moreMenuItems(v View) []kit.MenuItem {
	var items []kit.MenuItem
	if len(v.Revisions) > 0 {
		when := kit.FormatItemTime(v.Revisions[0].CreatedAt, s.nowTime(), s.loc(), s.Locale)
		items = append(items, kit.MenuItem{
			Label: locales.WikiRestorePrevious(), Description: locales.WikiRestorePreviousHint(when), Icon: icons.RotateCcw,
			OnSelect: func() { s.spawn(func() { s.Store.RestorePreviousRevision() }) },
		})
	}
	if v.Detail.Page.ManualEditedAt != nil {
		items = append(items, kit.MenuItem{
			Label: locales.WikiReleaseToAuto(), Description: locales.WikiReleaseToAutoHint(), Icon: icons.WandSparkles,
			OnSelect: func() { s.spawn(func() { s.Store.ReleaseToAuto() }) },
		})
	}
	return append(items, kit.MenuItem{
		Label: locales.WikiDeletePage(), Icon: icons.Trash2, Destructive: true,
		OnSelect: func() { s.detail.confirmOpen = true },
	})
}

// deleteConfirm 是删除确认框：删除不可逆，指向它的链接会变成断链。每一帧都要经过 ConfirmDialog。
func (s *Screen) deleteConfirm(c ui.C, v View) {
	d := &s.detail
	if v.Detail == nil {
		d.confirmOpen = false
	}
	title := ""
	if v.Detail != nil {
		title = v.Detail.Page.Title
	}
	switch s.Look.ConfirmDialog(c.Key("delete-confirm"), &d.confirm, kit.ConfirmSpec{
		Open: d.confirmOpen, Title: locales.WikiDeletePage(), Message: locales.WikiDeletePageConfirm(title),
		ConfirmText: locales.CommonDelete(), CancelText: locales.CommonCancel(), Destructive: true,
	}) {
	case kit.ConfirmAccepted:
		d.confirmOpen = false
		if v.Detail != nil {
			id := v.Detail.Page.ID
			s.spawn(func() { s.Store.DeletePage(id) })
		}
	case kit.ConfirmDismissed:
		d.confirmOpen = false
	}
}
