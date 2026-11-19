package library

import (
	"errors"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// itemList 是列表栏里的内容（渲染层 components/library/ItemList）：flex h-full min-h-0 flex-col，
// 从上到下依次是工具条、「本页 / 全部符合筛选」一行、滚动的条目区与分页条。
//
// 多选时顶部的工具条换成批量条、批量结果栏与右键菜单随批量操作一起迁入（M2-3）。
func (s *Screen) itemList(c ui.C, v View) {
	ui.Column(c.Key("item-list")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		s.listToolbar(c, v)
		s.selectRow(c, v)
		s.listBody(c, v)
		s.pager(c, v.List, false)
	})
}

// selectRow 是工具条下面的一行：两枚 ghost 小按钮「本页」与「全部符合筛选」（flex px-2）。
// 「全部符合筛选」要冻结整个筛选结果再批量处理，随批量操作迁入（M2-3，台账 B-038）。
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
	s.pane.window.Rows(c, kit.RowsSpec{
		Name: "cards", N: len(list.Entries), View: view,
		Epoch: kit.RowEpoch{RootPx: l.RootPx, Variant: int(locales.CurrentLang())},
		Key:   func(i int) string { return list.Entries[i].ID },
		Row:   func(i int) ui.Box { return s.entryRow(c, list, list.Entries[i], query) },
	})
}

// selectAllMatching 冻结整个筛选结果并全选（渲染层 library-workflow.store 的 selectAllMatching）。
// 它属于批量操作的一部分，随 M2-3 一起迁入；在那之前点击只给出说明（台账 B-038）。
func (s *Screen) selectAllMatching() {
	s.Guard.Report(locales.LibraryActionSelectAllMatching(), errSelectAllPending)
}

// errSelectAllPending 是「全部符合筛选」在批量操作迁入之前的失败原因：用户点了就要有交代，不能一声不响。
var errSelectAllPending = errors.New("批量选择尚未迁入原生界面，请先用「本页」")

// searchActive 报告搜索词是否非空（去掉首尾空白之后）：搜索中排序由相关度接管。
func searchActive(list ListView) bool { return strings.TrimSpace(list.Search) != "" }
