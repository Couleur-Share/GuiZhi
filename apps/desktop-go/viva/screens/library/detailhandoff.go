package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 「复制给 AI」（渲染层 AiHandoffButton）：把条目序列化成一段自包含的 Markdown，粘进 AI IDE 就能让对方了解这条内容。
// 主按钮直接复制完整版（最常用的一步），旁边的箭头展开另外两种出口：复制精简版、另存为 .md 文件。
// 任务在后台跑，期间两枚按钮都禁用（Store 的 HandoffBusy）；空条目（正文与文字稿都只有空白）只会导出一份光有 front matter 的壳，也禁用。

// handoffUI 是交接稿按钮的界面状态：箭头菜单，与箭头钮上一帧的外框（菜单的锚点与 Keep）。
type handoffUI struct {
	menu  anchoredMenu
	arrow gfx.Rect
}

// handoffButton 声明交接稿按钮：flex items-center，主钮 32×32（图标 h-4 w-4），箭头钮 16×32（图标 h-3 w-3），
// 都是 rounded-lg text-muted-foreground hover:bg-accent/60 hover:text-foreground disabled:opacity-40。
func (s *Screen) handoffButton(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	t := l.T
	h := &s.detail.tools.handoff
	id := item.ID
	disabled := jsTrim(item.Content) == "" && jsTrim(item.Transcript) == "" || v.Detail.Tools.HandoffBusy
	copyFull := locales.LibraryAiHandoffCopyFull()
	run := func(task func()) { s.Store.RunHandoff(task) }
	ui.Row(c).Items(ui.Center).Children(func() {
		main := l.Button(c.Key("copy"), kit.ButtonSpec{Icon: icons.Bot, IconSize: l.Rem(1), Center: true, Name: copyFull, Hint: copyFull, Disabled: disabled}, kit.ButtonStyle{
			Height: l.Rem(2), Width: l.Rem(2), Radius: l.Rem(0.75),
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6),
			Transition: kit.DurationDefault, DisabledOpacity: 0.4,
		})
		if main.Clicked() {
			run(func() { s.Store.CopyAiHandoff(s.Ctx, id, true) })
		}
		arrow := s.inlineButton(c.Key("more"), inlineSpec{
			Name: locales.LibraryAiHandoffMore(), Popup: true, Open: h.menu.open, Disabled: disabled,
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6),
			Radius: l.Rem(0.75), Height: l.Rem(2), Width: l.Rem(1), DisabledOpacity: 0.4, Rect: &h.arrow,
		}, func(fg tokens.Color) {
			l.Icon(c.Key("icon"), icons.ChevronDown, l.Rem(0.75), fg)
		})
		if arrow.Clicked() {
			if h.menu.open {
				h.menu.close()
			} else {
				r := h.arrow
				h.menu.show(gfx.Point{X: r.X, Y: r.Y + r.Height + 4})
			}
		}
	})
	s.handoffMenuView(c, id)
}

// handoffMenuView 声明箭头的菜单：三项，位置在箭头钮的左下方 4px。每一帧都要经过 ContextMenu（关着的那一帧它负责复位）。
func (s *Screen) handoffMenuView(c ui.C, id string) {
	h := &s.detail.tools.handoff
	var items []kit.MenuItem
	if h.menu.open {
		items = []kit.MenuItem{
			{Label: locales.LibraryAiHandoffCopyFull(), Icon: icons.ClipboardCopy,
				OnSelect: func() { s.Store.RunHandoff(func() { s.Store.CopyAiHandoff(s.Ctx, id, true) }) }},
			{Label: locales.LibraryAiHandoffCopyBrief(), Icon: icons.FileText,
				OnSelect: func() { s.Store.RunHandoff(func() { s.Store.CopyAiHandoff(s.Ctx, id, false) }) }},
			{Label: locales.LibraryAiHandoffSave(), Icon: icons.FileDown,
				OnSelect: func() { s.Store.RunHandoff(func() { s.Store.SaveAiHandoff(s.Ctx, id) }) }},
		}
	}
	keep := h.arrow
	if s.Look.ContextMenu(c.Key("handoff-menu"), &h.menu.st, kit.MenuSpec{
		Open: h.menu.open, X: h.menu.at.X, Y: h.menu.at.Y, Items: items, Keep: &keep,
	}) {
		h.menu.close()
	}
}
