package ask

import (
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 围绕本文的历史会话列表（渲染层 ArticleHistoryList）：只列这篇文章的本文会话，筛选条件独立于侧栏的全局历史
// （自己的 HistoryStore 实例）。点标题打开会话，悬停或聚焦在行内时浮出重命名与删除。

// articleHistoryUI 是列表的界面状态。列表随「历史」钮展开而挂载、收起而卸载（卸载时整体清空，再展开重读）。
type articleHistoryUI struct {
	mounted, shown bool
	itemID         string // 列表当前属于哪篇文章
	search         ui.Field
	seen           string // 搜索框上一次被读到的文字：与它不同就是用户又敲了字，重排防抖
	stop           func()
	rename         renameState
	confirm        kit.ConfirmState
	deleting       *services.AskSessionMeta
	rows           kit.HoverGroups
}

// articleHistorySearchDebounce 是本文历史搜索的防抖（渲染层 setTimeout 200 ms）。
const articleHistorySearchDebounce = 200 * time.Millisecond

// articleHistoryStore 取（或建）本文历史的仓库：与全局历史同一套逻辑，筛选条件各用各的。
func (s *Screen) articleHistoryStore() *HistoryStore {
	if s.artHistory == nil {
		s.artHistory = NewHistoryStore(s.Store.svc, s.Store.guard, s.Store.invalidate)
		s.artHistory.Ctx = s.Store.Ctx
	}
	return s.artHistory
}

// articleQuery 是本文历史的筛选条件：这篇文章的本文会话，加搜索词。
func articleQuery(itemID, search string) services.AskSessionQuery {
	return services.AskSessionQuery{Scope: services.AskScopeArticle, ItemID: itemID, Search: search}
}

// syncArticleSearch 让搜索框的防抖跟上输入：文字每变一次重排计时，停下后按新搜索词重读。
func (s *Screen) syncArticleSearch(store *HistoryStore, itemID string) {
	h := &s.art.hist
	text := h.search.Text()
	if text == h.seen {
		return
	}
	h.seen = text
	if h.stop != nil {
		h.stop()
	}
	h.stop = s.after(articleHistorySearchDebounce, func() {
		s.spawn(func() { store.Filter(articleQuery(itemID, text)) })
	})
}

// articleHistory 是列表：max-h-56 shrink-0 overflow-auto border-b border-border p-2。
// onOpen 在点了某个会话的标题时调用（界面线程）。
func (s *Screen) articleHistory(c ui.C, itemID string, onOpen func(id string)) {
	l := s.Look
	t := l.T
	h := &s.art.hist
	h.shown = true
	store := s.articleHistoryStore()
	if !h.mounted || h.itemID != itemID {
		h.mounted, h.itemID = true, itemID
		h.search.SetText("")
		h.seen = ""
		s.spawn(func() { store.Filter(articleQuery(itemID, "")) })
	}
	s.syncArticleSearch(store, itemID)
	h.rows.Begin()
	view := store.View()

	ui.Column(c.Key("history")).FillWidth().Shrink(0).PadSides(0, 0, 1, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			pad := l.Space(2)
			l.ScrollArea(c.Key("scroll"), [4]float32{pad, pad, pad, pad}, func() {
				// mb-2 w-full rounded border border-border bg-background px-2 py-1 text-xs
				ui.Column(c.Key("search-wrap")).FillWidth().MarginBottom(l.Space(2)).Children(func() {
					s.boxedInput(c.Key("search"), &h.search, locales.ArticleAskHistorySearch(), locales.ArticleAskHistorySearchPlaceholder(),
						l.Rem(0.75), l.Rem(1)+l.Space(2)+2, l.Space(2), l.Rem(0.25))
				})
				for _, e := range view.Entries {
					s.articleHistoryRow(c, e, view.Loading, onOpen)
				}
				s.articleHistoryFooter(c, store, view)
			}).FillWidth().MaxHeight(l.Rem(14))
		})

	s.renameDialog(c, "article-rename", &h.rename, store)
	s.articleHistoryDelete(c, store)
}

// articleHistoryRow 是一行会话：group flex items-center gap-0.5。标题钮（ghost sm、左对齐、占满）在 busy 时禁用；
// 重命名与删除两枚 24px 的图标钮在行内悬停或聚焦时才显现。
func (s *Screen) articleHistoryRow(c ui.C, e services.AskSessionMeta, busy bool, onOpen func(id string)) {
	l := s.Look
	t := l.T
	h := &s.art.hist
	group := "article-session:" + e.ID
	title := e.Title
	if title == "" {
		title = locales.AskUntitledSession()
	}
	c = c.Key("session-" + e.ID)
	hot := h.rows.Active(c.Canvas(), group)
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(0.5)).Children(func() {
		b := l.Button(c.Key("open"), kit.ButtonSpec{Label: title, Disabled: busy, Groups: &h.rows, Group: group}, l.UIGhostButton(kit.UIButtonSM)).
			Grow(1).Shrink(1).MinWidth(0)
		if b.Clicked() {
			onOpen(e.ID)
		}
		size := l.Rem(1.5)
		base := kit.ButtonStyle{
			Height: size, Width: size, Radius: l.Rem(0.25), Transition: kit.DurationDefault,
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent,
		}
		icon := func(key string, name string, ic kit.ButtonSpec, st kit.ButtonStyle) bool {
			ic.Name, ic.Hint, ic.Center, ic.Groups, ic.Group = name, name, true, &h.rows, group
			ic.IconSize = l.Rem(0.875)
			box := l.Button(c.Key(key), ic, st).Shrink(0)
			if !hot {
				box = box.Opacity(0)
			}
			return box.Clicked()
		}
		if icon("rename", locales.AskRenameSession(), kit.ButtonSpec{Icon: icons.Pencil}, base) {
			entry := e
			h.rename.session = &entry
			h.rename.field.SetText(entry.Title)
		}
		remove := base
		remove.FgHover = t.Destructive
		if icon("delete", locales.AskDeleteSession(), kit.ButtonSpec{Icon: icons.Trash2}, remove) {
			entry := e
			h.deleting = &entry
		}
	})
}

// articleHistoryFooter 是列表底部：没有匹配（无错误、不在加载时）的说明、读取失败的提示（带重试）、「加载更多」与加载中的指示。
func (s *Screen) articleHistoryFooter(c ui.C, store *HistoryStore, view HistoryView) {
	l := s.Look
	t := l.T
	if !view.Loading && len(view.Entries) == 0 && view.Error == "" {
		l.Text(c.Key("empty"), locales.ArticleAskHistoryNoMatch(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
	}
	if view.Error != "" {
		ui.Flow(c.Key("error")).FillWidth().Items(ui.Center).Gap(l.Space(1)).LineGap(0).
			Draw(func(d *ui.DrawContext) { markAlert(d) }).
			Children(func() {
				l.Text(c.Key("text"), view.Error, kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped()).Shrink(1).MinWidth(0)
				b := l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.CommonRetry()}, l.UIGhostButton(kit.UIButtonSM))
				if b.Clicked() {
					s.spawn(func() { store.Load(false) })
				}
			})
	}
	if view.HasMore {
		b := l.Button(c.Key("more"), kit.ButtonSpec{Label: locales.ImportsLoadMore(), Disabled: view.Loading}, l.UISecondaryButton(kit.UIButtonSM))
		if b.Clicked() {
			s.spawn(func() { store.Load(true) })
		}
	}
	if view.Loading {
		ui.Column(c.Key("loading")).FillWidth().Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
			l.Text(c.Key("text"), locales.CommonLoading(), kit.TextXS, kit.WeightNormal, t.Foreground)
		})
	}
}

// articleHistoryDelete 是删除确认；确认后删除并重读本文历史（渲染层 deleteSession(...).then(重读)）。
func (s *Screen) articleHistoryDelete(c ui.C, store *HistoryStore) {
	l := s.Look
	h := &s.art.hist
	spec := kit.ConfirmSpec{
		ConfirmText: locales.CommonConfirm(), CancelText: locales.CommonCancel(), Destructive: true,
		Title: locales.AskDeleteSession(),
	}
	if h.deleting != nil {
		spec.Open = true
		spec.Message = locales.AskDeleteSessionConfirm(h.deleting.Title)
	}
	switch l.ConfirmDialog(c.Key("article-delete"), &h.confirm, spec) {
	case kit.ConfirmAccepted:
		if target := h.deleting; target != nil {
			id := target.ID
			s.spawn(func() {
				s.Store.DeleteSession(id)
				store.Load(false)
			})
		}
		h.deleting = nil
	case kit.ConfirmDismissed:
		h.deleting = nil
	}
}
