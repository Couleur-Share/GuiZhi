package library

import (
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// itemList 是列表栏里的内容（渲染层 components/library/ItemList）：flex h-full min-h-0 flex-col，
// 从上到下依次是工具条、「本页 / 全部符合筛选」一行、滚动的条目区与分页条。
//
// 多选时顶部的工具条换成批量条；批量结果条在它下面；条目右键菜单与确认框是浮层，声明在最后。
// 卡片视图没有标签选择浮层（详情栏里的标签行自带 + 入口），切过来时丢掉表格视图留下的那一个。
func (s *Screen) itemList(c ui.C, v View) {
	s.bulk.picker.pop.Close()
	s.bulk.picker.ai.reset()
	ui.Column(c.Key("item-list")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		s.selectionBar(c, v, false)
		s.batchStrip(c, v)
		s.selectRow(c, v)
		s.listBody(c, v)
		s.pager(c, v.List, false)
		s.entryMenuView(c, v)
		s.itemConfirmView(c, v)
	})
}

// selectRow 是工具条下面的一行：两枚 ghost 小按钮「本页」与「全部符合筛选」（flex px-2）。
// 「全部符合筛选」冻结整个筛选结果并全选，之后的批量操作都作用于这一组（selectAllMatching）。
func (s *Screen) selectRow(c ui.C, v View) {
	l := s.Look
	ui.Row(c.Key("select-row")).FillWidth().Shrink(0).PadSides(0, l.Space(2), 0, l.Space(2)).Children(func() {
		style := l.UIGhostButton(kit.UIButtonSM)
		if l.Button(c.Key("page"), kit.ButtonSpec{Label: locales.LibrarySelectPage()}, style).Clicked() {
			s.Store.SelectPage()
		}
		if l.Button(c.Key("all"), kit.ButtonSpec{Label: locales.LibrarySelectAllMatching()}, style).Clicked() {
			s.selectAllMatching()
		}
	})
}

// listBody 是滚动的条目区：min-h-0 flex-1 overflow-y-auto pb-2。
// 里面按「读取中、读取失败、空、列表」四选一——读取失败只在还没有任何条目时占满这一区（读到过的列表保留着），
// 读取失败不能画成空态。
func (s *Screen) listBody(c ui.C, v View) {
	l := s.Look
	list := v.List
	empty := len(list.Entries) == 0
	l.ScrollAreaView(c.Key("scroll"), [4]float32{0, 0, l.Space(2), 0}, func(view kit.ScrollView) {
		switch {
		case list.Loading && empty:
			// flex h-32 items-center justify-center：一枚 sm 的 muted 转圈。
			ui.Row(c.Key("loading")).FillWidth().Height(l.Rem(8)).Items(ui.Center).Justify(ui.Center).Children(func() {
				l.Spinner(c, l.Rem(1), l.T.MutedForeground)
			})
		case list.LoadError != "" && empty:
			if l.LoadErrorState(c, list.LoadError) {
				s.Store.Spawn(func() { s.Store.FetchList(s.Ctx) })
			}
		case empty:
			message := locales.LibraryListEmpty()
			if v.Filters.Scope == services.LibraryScopeTrash {
				message = locales.LibraryTrashEmpty()
			}
			// flex h-32 items-center justify-center px-6 text-center text-sm text-muted-foreground
			ui.Row(c.Key("empty")).FillWidth().Height(l.Rem(8)).Items(ui.Center).Justify(ui.Center).
				PadSides(0, l.Space(6), 0, l.Space(6)).Children(func() {
				l.Text(c, message, kit.TextSM, kit.WeightNormal, l.T.MutedForeground, kit.Centered(), kit.Wrapped())
			})
		default:
			s.cardRows(c, v, view)
		}
	}).FillWidth().Grow(1).MinHeight(0)
}

// cardRows 是卡片行：relative w-full，每行 absolute left-0 top-0 w-full px-2 pt-1.5。
// 渲染层用虚拟列表（预估 64、预渲染 8 行）；这里是 kit.RowWindow，行高随标题行数（一到两行）与搜索时的摘要变化。
func (s *Screen) cardRows(c ui.C, v View, view kit.ScrollView) {
	l := s.Look
	list := v.List
	query := list.searchTerm()
	s.pane.window.Seed = cardSeed
	s.pane.window.Overscan = 8
	s.pane.reveal.noteReveal(list, &s.pane.window)
	s.pane.window.Rows(c, kit.RowsSpec{
		Name: "cards", N: len(list.Entries), View: view,
		Epoch: kit.RowEpoch{RootPx: l.RootPx, Variant: int(locales.CurrentLang())},
		Key:   func(i int) string { return list.Entries[i].ID },
		Row:   func(i int) ui.Box { return s.entryRow(c, list, list.Entries[i], query) },
	})
}

// searchActive 报告搜索词是否非空（去掉首尾空白之后）：搜索中排序由相关度接管。
func searchActive(list ListView) bool { return strings.TrimSpace(list.Search) != "" }
