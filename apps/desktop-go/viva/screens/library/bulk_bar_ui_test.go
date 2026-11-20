package library

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测批量条、条目菜单与「移动到合集」菜单的界面级行为（台账 B-038、B-040、B-043）：点了哪里、服务收到什么、画面发生什么。
// Store 自己的规则（逐条记账、重试、冻结）在 store_batch_test.go，这里只验证界面把点击正确地接到了 Store 上。
//
// 数据同 list_ui_test.go：服务里 GenerateEntries(100)，「全部」范围 80 条、每页 20 条，回收站 5 条；
// 夹具里的合集是「工作」（c1）与「阅读」（c2）。

// bothViews 在卡片与表格两种视图里各跑一遍 fn（每个子测试一个新夹具）。
func bothViews(t *testing.T, fn func(t *testing.T, r *rig), mutate ...func(*rig)) {
	t.Helper()
	for _, view := range []string{ViewCard, ViewList} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			r := wsRig(t, mutate...)
			if view == ViewList {
				r.toTable()
			}
			fn(t, r)
		})
	}
}

// inTable 报告工作区是不是表格视图（只有表格有「本页」复选框的表头）。
func (r *rig) inTable() bool { return r.has(gfx.RoleCheckbox, locales.LibrarySelectPageRows()) }

// selectIDs 经 Store 选中一组条目：选择的来路（点击、修饰键、复选框、全选）在 list_ui_test.go 里测。
func (r *rig) selectIDs(ids []string) {
	r.t.Helper()
	r.store.SetSelection(ids)
	r.frames(3)
}

// entryPoint 是条目上一个能点中整行或整张卡片的位置：卡片取中心，表格取标题格里标题钮右边的空白。
func (r *rig) entryPoint(e services.LibraryEntry) (x, y float32) {
	r.t.Helper()
	if r.inTable() {
		return r.rowBlank(e)
	}
	n, ok := r.cardOf(e)
	if !ok {
		r.t.Fatalf("找不到条目 %q 的卡片", e.Title)
	}
	return centerOf(n)
}

// rightClickEntry 在条目上点右键，返回点击位置。
func (r *rig) rightClickEntry(e services.LibraryEntry) (x, y float32) {
	r.t.Helper()
	x, y = r.entryPoint(e)
	if err := r.session.RightClick(x, y); err != nil {
		r.t.Fatalf("右键条目 %q 失败：%v", e.Title, err)
	}
	r.frames(3)
	return x, y
}

// lastBatch 是服务收到的最后一次批量执行；一次也没有就失败。
func (r *rig) lastBatch() servicestest.BatchCall {
	r.t.Helper()
	calls := r.svc.Batches()
	if len(calls) == 0 {
		r.t.Fatal("服务没有收到批量执行")
	}
	return calls[len(calls)-1]
}

// goTrash 切到回收站范围（侧栏的「回收站」行）。
func (r *rig) goTrash() {
	r.t.Helper()
	r.click(gfx.RoleButton, locales.LibraryScopeTrash())
}

// ---- 批量条 ----

// 有多选时批量条取代工具条（渲染层 `selectionIds.length > 0 ? <ItemBulkBar /> : <ItemListToolbar />`）；
// 「取消选择」钮与 Esc 都清掉多选、换回工具条；Esc 不吞按键（菜单与对话框自己的 Esc 照常运行）。
func TestBulkBarReplacesTheToolbarWhileSomethingIsSelected(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		toggle := locales.LibraryViewList()
		if r.inTable() {
			toggle = locales.LibraryViewCard()
		}
		clearLabel := locales.LibraryClearSelection()
		if !r.has(gfx.RoleButton, toggle) || r.has(gfx.RoleButton, clearLabel) {
			t.Fatalf("没有多选时应是工具条（有视图切换钮「%s」）、没有批量条", toggle)
		}
		ids := r.ids(0, 2)
		r.selectIDs(ids)
		if !r.hasText(locales.LibrarySelectedCount(3)) || !r.has(gfx.RoleButton, clearLabel) {
			t.Fatalf("选中 3 条应出现批量条；画面文字 %q", r.session.Texts())
		}
		if r.has(gfx.RoleButton, toggle) {
			t.Fatal("批量条取代工具条：视图切换钮不该还在")
		}

		r.click(gfx.RoleButton, clearLabel)
		if got := r.view().List.Selection; len(got) != 0 {
			t.Fatalf("点「取消选择」应清掉多选，实际 %v", got)
		}
		if !r.has(gfx.RoleButton, toggle) || r.has(gfx.RoleButton, clearLabel) {
			t.Fatal("清掉多选后应换回工具条")
		}

		r.selectIDs(ids)
		r.keys("Escape")
		if got := r.view().List.Selection; len(got) != 0 {
			t.Fatalf("Esc 应清掉多选，实际 %v", got)
		}
		r.selectIDs(ids)
		if res := r.screen.ListKey(gfx.KeyEvent{Key: gfx.KeyEscape}, &r.canvas); res.Handled || len(r.view().List.Selection) != 0 {
			t.Fatalf("Esc 清掉多选但不能吞掉按键，Handled=%v 多选 %v", res.Handled, r.view().List.Selection)
		}
		if res := r.screen.ListKey(gfx.KeyEvent{Key: gfx.KeyEscape}, &r.canvas); res.Handled || res.Repaint {
			t.Fatalf("没有多选时 Esc 与列表无关：%+v", res)
		}
	})
}

// hasHint 报告画面上有没有声明了悬停提示 text 的命中区（提示层本身见 kit/tooltip_test.go）。
func (r *rig) hasHint(text string) bool {
	for _, h := range r.session.Snapshot().Hits {
		if h.Hint == text && !h.Block {
			return true
		}
	}
	return false
}

// 卡片的批量条窄，动作只有图标（文案走提示）；表格的批量条是图标带文字。两者的可访问名都是文案。
func TestBulkBarActionsAreIconOnlyOnCardsAndLabelledInTheTable(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.selectIDs(r.ids(0, 1))
		for _, label := range []string{
			locales.LibraryBulkMoveTitle(), locales.LibraryBulkAddTag(), locales.LibraryBulkFavorite(),
			locales.LibraryArchive(), locales.LibraryMoveToTrash(),
		} {
			if !r.has(gfx.RoleButton, label) {
				t.Errorf("批量条缺少动作「%s」", label)
			}
			// 只有图标的钮把文案挂成悬停提示（渲染层的 title）。
			if !r.inTable() && !r.hasHint(label) {
				t.Errorf("卡片视图里的动作「%s」只有图标，应声明悬停提示", label)
			}
			// 「归档」与侧栏范围页签的文字相同，画面上的文字分不出是哪一处，只查另外四个。
			if label == locales.LibraryArchive() {
				continue
			}
			if got := r.hasText(label); got != r.inTable() {
				t.Errorf("动作「%s」的文字：表格才画出文字，实际 inTable=%v 画出=%v", label, r.inTable(), got)
			}
		}
	})
}

// 收藏与归档：对选中的每一条发一个批次，字段与状态都对。
func TestBulkFavoriteAndArchiveSendOneBatchForTheSelection(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		ids := r.ids(1, 3)
		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryBulkFavorite())
		b := r.lastBatch()
		if b.Command.Kind != services.LibraryBatchUpdate || b.Command.Patch == nil || b.Command.Patch.IsFavorite == nil || !*b.Command.Patch.IsFavorite {
			t.Fatalf("收藏应是改 isFavorite=true 的批量更新，实际 %+v", b.Command)
		}
		if !slices.Equal(b.IDs, ids) {
			t.Fatalf("批次里的条目应是选中的 %v，实际 %v", ids, b.IDs)
		}

		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryArchive())
		b = r.lastBatch()
		if b.Command.Kind != services.LibraryBatchStatus || b.Command.Status != "archived" || !slices.Equal(b.IDs, ids) {
			t.Fatalf("归档应是对选中条目改状态为 archived，实际 %+v %v", b.Command, b.IDs)
		}
		if len(r.svc.Batches()) != 2 {
			t.Fatalf("两次点击应只发两个批次，实际 %d", len(r.svc.Batches()))
		}
	})
}

// 移到回收站：成功才弹带「撤销」的提示，撤销就把同一组恢复；有失败时不能再补一句「已移到回收站」。
func TestBulkTrashOffersUndoOnlyWhenEverythingWasMoved(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		ids := r.ids(1, 3)
		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryMoveToTrash())
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchTrash || !slices.Equal(b.IDs, ids) {
			t.Fatalf("应发一个移到回收站的批次，实际 %+v %v", b.Command, b.IDs)
		}
		if !r.hasText(locales.LibraryMovedToTrash(3)) {
			t.Fatalf("成功应弹「已移到回收站」；画面文字 %q", r.session.Texts())
		}
		r.click(gfx.RoleButton, locales.CommonUndo())
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchRestore || !slices.Equal(b.IDs, ids) {
			t.Fatalf("撤销应恢复同一组，实际 %+v %v", b.Command, b.IDs)
		}
	})
	bothViews(t, func(t *testing.T, r *rig) {
		ids := r.ids(1, 3)
		r.svc.FailBatchItem(ids[1], "磁盘已满")
		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryMoveToTrash())
		if r.hasText(locales.LibraryMovedToTrash(3)) || r.hasText(locales.LibraryMovedToTrash(2)) || r.has(gfx.RoleButton, locales.CommonUndo()) {
			t.Fatalf("有失败时不该弹成功与撤销；画面文字 %q", r.session.Texts())
		}
		if !r.hasText(r.view().Batch.Notice) || r.view().Batch.Notice == "" {
			t.Fatalf("结果条应写汇总 %q；画面文字 %q", r.view().Batch.Notice, r.session.Texts())
		}
	})
}

// 移动到合集：菜单是「未归合集」加各合集；点一项把选中的条目改到那个合集（空字符串是移出合集）。
func TestBulkMoveMenuMovesToACollectionOrOutOfOne(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		ids := r.ids(0, 2)
		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryBulkMoveTitle())
		for _, label := range []string{locales.LibraryNoCollection(), "工作", "阅读"} {
			if !r.has(gfx.RoleMenuItem, label) {
				t.Fatalf("移动菜单缺「%s」", label)
			}
		}
		r.click(gfx.RoleMenuItem, "阅读")
		b := r.lastBatch()
		if b.Command.Patch == nil || b.Command.Patch.CollectionID == nil || *b.Command.Patch.CollectionID != "c2" || !slices.Equal(b.IDs, ids) {
			t.Fatalf("应把选中条目改到合集 c2，实际 %+v %v", b.Command, b.IDs)
		}
		if r.has(gfx.RoleMenuItem, "阅读") {
			t.Fatal("选了一项之后菜单应收起")
		}

		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryBulkMoveTitle())
		r.click(gfx.RoleMenuItem, locales.LibraryNoCollection())
		b = r.lastBatch()
		if b.Command.Patch == nil || b.Command.Patch.CollectionID == nil || *b.Command.Patch.CollectionID != "" {
			t.Fatalf("「未归合集」应把合集改成空字符串，实际 %+v", b.Command.Patch)
		}

		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryBulkMoveTitle())
		r.keys("Escape")
		if r.has(gfx.RoleMenuItem, "工作") {
			t.Fatal("Esc 应收起移动菜单")
		}
	})
}

// 菜单开在点击的光标处（渲染层取 event.clientX / clientY）。
func TestBulkMoveMenuOpensAtThePointer(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.selectIDs(r.ids(0, 1))
		button := r.mustNode(gfx.RoleButton, locales.LibraryBulkMoveTitle())
		x, y := centerOf(button)
		r.clickAt(x, y)
		item := r.mustNode(gfx.RoleMenuItem, locales.LibraryNoCollection())
		if item.Bounds.X < x || item.Bounds.X > x+12 || item.Bounds.Y < y || item.Bounds.Y > y+12 {
			t.Fatalf("菜单应开在光标 (%.0f, %.0f) 处，第一项在 %+v", x, y, item.Bounds)
		}
	})
}

// 键盘（回车、空格）点批量钮：浏览器合成的 click 事件坐标是原点，不是按钮也不是鼠标最后停的地方——
// React 实测（scripts/parity/tools/probe-library-bulkmenu.mjs，读数 library-bulkmenu.json）：回车与空格时 click 事件是 (0, 0)，
// 「移动到合集」菜单开在 (0, 0)，批量标签浮层被夹到距窗口边 8 px 的 (8, 8)；鼠标点击时都在光标处。
// 菜单与浮层的内边距不同，所以断言的是第一项 / 输入框相对原点或 8 px 边距的小范围，而不是整个面板的外框。
func TestKeyboardActivatedBulkMenusOpenAtTheOrigin(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		toButton := func(label string) {
			t.Helper()
			for range 150 {
				r.keys("Tab")
				if got, _, ok := r.focusedLabel(); ok && got == label {
					return
				}
			}
			t.Fatalf("按 Tab 一路走下去没有轮到「%s」", label)
		}
		// 先用鼠标点一次这个钮再按 Esc 收起（多选也被清掉）：界面记下鼠标最后松开的位置，键盘激活若取了它，就和原点对不上。
		mouseOnce := func(label string) {
			t.Helper()
			r.selectIDs(r.ids(0, 1))
			x, y := centerOf(r.mustNode(gfx.RoleButton, label))
			r.clickAt(x, y)
			r.keys("Escape")
			if len(r.view().List.Selection) != 0 {
				t.Fatalf("Esc 之后多选应已清空（渲染层批量条的 window 监听，与探针读数一致），实际 %v", r.view().List.Selection)
			}
		}

		mouseOnce(locales.LibraryBulkMoveTitle())
		r.selectIDs(r.ids(0, 1))
		toButton(locales.LibraryBulkMoveTitle())
		r.keys("Enter")
		item := r.mustNode(gfx.RoleMenuItem, locales.LibraryNoCollection())
		if item.Bounds.X < 0 || item.Bounds.X > 12 || item.Bounds.Y < 0 || item.Bounds.Y > 12 {
			t.Fatalf("回车打开的「移动到合集」菜单应在窗口原点，第一项在 %+v", item.Bounds)
		}

		r.keys("Escape")
		mouseOnce(locales.LibraryBulkAddTag())
		r.selectIDs(r.ids(0, 1))
		toButton(locales.LibraryBulkAddTag())
		r.press("space")
		field := r.mustNode(gfx.RoleTextField, locales.LibraryTagsNewPlaceholder())
		if field.Bounds.X < 8 || field.Bounds.X > 8+24 || field.Bounds.Y < 8 || field.Bounds.Y > 8+80 {
			t.Fatalf("空格打开的批量标签浮层应夹在距窗口边 8 px 处，输入框在 %+v", field.Bounds)
		}
	})
}

// 微信公众号来源的条目才有补采钮；它只补采选中里属于公众号的那些。
func TestBulkWechatSupplementOnlyAppearsForWechatSources(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		const wechat = "item-000001"
		label := locales.LibraryBulkSupplementWechat()
		r.selectIDs([]string{"item-000000", "item-000002"})
		if r.has(gfx.RoleButton, label) {
			t.Fatal("选中里没有公众号来源时不该有补采钮")
		}
		r.selectIDs([]string{"item-000000", wechat})
		r.click(gfx.RoleButton, label)
		if got := r.svc.Supplemented(); len(got) != 1 || !slices.Equal(got[0], []string{wechat}) {
			t.Fatalf("应只补采公众号来源的那一条，实际 %v", got)
		}
	}, func(r *rig) {
		entries := servicestest.GenerateEntries(100)
		entries[1].Platform = "wechat" // item-000001
		r.svc.SeedEntries(entries)
	})
}

// 回收站里的批量条只有「恢复」与「彻底删除」（回收站里的条目即使来自公众号，也没有补采钮）。
func TestBulkBarInTheTrashOffersRestoreAndDeleteForeverOnly(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		ids := r.ids(0, 1)
		r.selectIDs(ids)
		for _, label := range []string{
			locales.LibraryBulkMoveTitle(), locales.LibraryBulkAddTag(), locales.LibraryBulkFavorite(),
			locales.LibraryArchive(), locales.LibraryMoveToTrash(), locales.LibraryBulkSupplementWechat(),
		} {
			if r.has(gfx.RoleButton, label) {
				t.Errorf("回收站里的批量条不该有「%s」", label)
			}
		}
		r.click(gfx.RoleButton, locales.LibraryRestore())
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchRestore || !slices.Equal(b.IDs, ids) {
			t.Fatalf("「恢复」应发恢复批次，实际 %+v %v", b.Command, b.IDs)
		}
	}, func(r *rig) {
		entries := servicestest.GenerateEntries(100)
		for i := range entries {
			if entries[i].DeletedAt != 0 {
				entries[i].Platform = "wechat" // 回收站里全是公众号来源：补采钮仍不该出现
			}
		}
		r.svc.SeedEntries(entries)
	})
}

// 「全部符合筛选」冻结整个筛选结果并全选：80 条都进了多选，批量条数的是这个数。
func TestSelectAllMatchingSelectsEveryEntryOfTheFilter(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.click(gfx.RoleButton, locales.LibrarySelectAllMatching())
		if got := len(r.view().List.Selection); got != 80 {
			t.Fatalf("应选中符合筛选的 80 条，实际 %d", got)
		}
		if !r.hasText(locales.LibrarySelectedCount(80)) {
			t.Fatalf("批量条应写 80 条；画面文字 %q", r.session.Texts())
		}
		if got := r.svc.Freezes(); len(got) != 1 || got[0].Scope != services.LibraryScopeAll {
			t.Fatalf("应按当前筛选冻结一次，实际 %+v", got)
		}
	})
}

// ---- 条目菜单 ----

// 右键在光标处开条目菜单：收藏、置顶、AI 交接稿、归档、移到回收站，表格多一个「编辑标签」。
func TestRightClickOpensTheEntryMenuAtThePointer(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		e := r.view().List.Entries[3] // 排在前两条置顶之后：既没置顶也没收藏
		x, y := r.rightClickEntry(e)
		for _, label := range []string{
			locales.LibraryFavorite(), locales.LibraryPin(), locales.LibraryAiHandoff(), locales.LibraryArchive(), locales.LibraryMoveToTrash(),
		} {
			if !r.has(gfx.RoleMenuItem, label) {
				t.Fatalf("条目菜单缺「%s」", label)
			}
		}
		if got := r.has(gfx.RoleMenuItem, locales.LibraryEditTags()); got != r.inTable() {
			t.Fatalf("「编辑标签」只在表格的菜单里：inTable=%v 有=%v", r.inTable(), got)
		}
		first := r.mustNode(gfx.RoleMenuItem, locales.LibraryFavorite())
		if first.Bounds.X < x || first.Bounds.X > x+12 || first.Bounds.Y < y || first.Bounds.Y > y+12 {
			t.Fatalf("菜单应开在光标 (%.0f, %.0f) 处，第一项在 %+v", x, y, first.Bounds)
		}
		if got := r.view().List.Selection; len(got) != 0 {
			t.Fatalf("右键不改选择，实际多选 %v", got)
		}
	})
}

// 菜单项落到服务：收藏与置顶翻转当前值，归档改状态，移到回收站发批次并给撤销。
func TestEntryMenuActionsReachTheService(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		e := r.view().List.Entries[3] // 排在前两条置顶之后：既没置顶也没收藏
		r.rightClickEntry(e)
		r.click(gfx.RoleMenuItem, locales.LibraryFavorite())
		calls := r.svc.UpdateCalls()
		if len(calls) != 1 || calls[0].ID != e.ID || calls[0].Patch.IsFavorite == nil || !*calls[0].Patch.IsFavorite {
			t.Fatalf("收藏应对这一条发 isFavorite=true，实际 %+v", calls)
		}
		if r.has(gfx.RoleMenuItem, locales.LibraryPin()) {
			t.Fatal("选中菜单项后菜单应收起")
		}

		r.rightClickEntry(e)
		if !r.has(gfx.RoleMenuItem, locales.LibraryUnfavorite()) {
			t.Fatal("收藏之后再开菜单应是「取消收藏」")
		}
		r.click(gfx.RoleMenuItem, locales.LibraryPin())
		calls = r.svc.UpdateCalls()
		if len(calls) != 2 || calls[1].Patch.IsPinned == nil || !*calls[1].Patch.IsPinned {
			t.Fatalf("置顶应对这一条发 isPinned=true，实际 %+v", calls)
		}
		r.rightClickEntry(e)
		if !r.has(gfx.RoleMenuItem, locales.LibraryUnpin()) || r.has(gfx.RoleMenuItem, locales.LibraryPin()) {
			t.Fatalf("置顶之后再开菜单应是「取消置顶」；菜单项 %q", menuLabels(r))
		}
		r.keys("Escape")

		e = r.view().List.Entries[4]
		r.rightClickEntry(e)
		r.click(gfx.RoleMenuItem, locales.LibraryArchive())
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchStatus || b.Command.Status != "archived" || !slices.Equal(b.IDs, []string{e.ID}) {
			t.Fatalf("归档应对这一条改状态，实际 %+v %v", b.Command, b.IDs)
		}

		e = r.view().List.Entries[6]
		r.rightClickEntry(e)
		r.click(gfx.RoleMenuItem, locales.LibraryMoveToTrash())
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchTrash || !slices.Equal(b.IDs, []string{e.ID}) {
			t.Fatalf("移到回收站应对这一条发批次，实际 %+v %v", b.Command, b.IDs)
		}
		if !r.hasText(locales.LibraryMovedToTrash(1)) {
			t.Fatalf("应弹「已移到回收站」；画面文字 %q", r.session.Texts())
		}
	})
}

// 右键落在多选组上，菜单作用于整组：只有归档与移到回收站，文案带条数；落在没选中的条目上仍是单条菜单。
func TestEntryMenuOnTheSelectionActsOnTheWholeGroup(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		ids := r.ids(1, 3)
		r.selectIDs(ids)
		r.rightClickEntry(r.view().List.Entries[2])
		if !r.has(gfx.RoleMenuItem, locales.LibraryBulkArchive(3)) || !r.has(gfx.RoleMenuItem, locales.LibraryBulkMoveToTrash(3)) {
			t.Fatalf("多选组上应是带条数的整组菜单；菜单项 %q", menuLabels(r))
		}
		if r.has(gfx.RoleMenuItem, locales.LibraryFavorite()) {
			t.Fatal("整组菜单里没有收藏")
		}
		r.click(gfx.RoleMenuItem, locales.LibraryBulkArchive(3))
		if b := r.lastBatch(); b.Command.Status != "archived" || !slices.Equal(b.IDs, ids) {
			t.Fatalf("整组归档应带上选中的全部条目，实际 %+v %v", b.Command, b.IDs)
		}

		ids = r.ids(1, 3) // 归档之后这三条离开了「全部」，重新选本页现有的三条
		r.selectIDs(ids)
		r.rightClickEntry(r.view().List.Entries[2])
		r.click(gfx.RoleMenuItem, locales.LibraryBulkMoveToTrash(3))
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchTrash || !slices.Equal(b.IDs, ids) {
			t.Fatalf("整组移到回收站应带上选中的全部条目，实际 %+v %v", b.Command, b.IDs)
		}
		if !r.hasText(locales.LibraryMovedToTrash(3)) {
			t.Fatalf("整组移到回收站应弹「已移到回收站（3 项）」；画面文字 %q", r.session.Texts())
		}

		ids = r.ids(1, 3)
		r.selectIDs(ids)
		r.rightClickEntry(r.view().List.Entries[7])
		if !r.has(gfx.RoleMenuItem, locales.LibraryFavorite()) || r.has(gfx.RoleMenuItem, locales.LibraryBulkArchive(3)) {
			t.Fatalf("没选中的条目上应是单条菜单；菜单项 %q", menuLabels(r))
		}

		// 多选里只有一条时它不算「一组」：右键它仍是单条菜单。
		r.keys("Escape")
		only := r.view().List.Entries[3]
		r.selectIDs([]string{only.ID})
		r.rightClickEntry(only)
		if !r.has(gfx.RoleMenuItem, locales.LibraryFavorite()) || r.has(gfx.RoleMenuItem, locales.LibraryBulkArchive(1)) {
			t.Fatalf("只选了一条时右键它应是单条菜单；菜单项 %q", menuLabels(r))
		}
	})
}

// 回收站里右键多选组：整组的「恢复」与「彻底删除」，文案带条数；彻底删除先确认。
func TestEntryMenuOnTheSelectionInTheTrashActsOnTheWholeGroup(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		ids := r.ids(0, 2)
		r.selectIDs(ids)
		r.rightClickEntry(r.view().List.Entries[1])
		if !r.has(gfx.RoleMenuItem, locales.LibraryBulkRestore(3)) || !r.has(gfx.RoleMenuItem, locales.LibraryBulkDeleteForever(3)) {
			t.Fatalf("回收站里多选组上应是带条数的整组菜单；菜单项 %q", menuLabels(r))
		}
		r.click(gfx.RoleMenuItem, locales.LibraryBulkDeleteForever(3))
		if !r.has(gfx.RoleHeading, locales.LibraryDeleteForever()) || !r.hasText(locales.LibraryDeleteForeverConfirmMany(3)) {
			t.Fatalf("整组彻底删除应先确认（3 条）；画面文字 %q", r.session.Texts())
		}
		if r.batchCount() != 0 {
			t.Fatal("确认之前不该请求后端")
		}
		r.click(gfx.RoleButton, locales.CommonCancel())
		r.selectIDs(ids)
		r.rightClickEntry(r.view().List.Entries[1])
		r.click(gfx.RoleMenuItem, locales.LibraryBulkRestore(3))
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchRestore || !slices.Equal(b.IDs, ids) {
			t.Fatalf("整组恢复应带上选中的全部条目，实际 %+v %v", b.Command, b.IDs)
		}
	})
}

// 归档范围里条目菜单是「取消归档」：把状态改回 active。
func TestEntryMenuInTheArchiveOffersUnarchive(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.store.SetScope(context.Background(), services.LibraryScopeArchived)
		r.frames(3)
		e := r.view().List.Entries[0]
		r.rightClickEntry(e)
		if r.has(gfx.RoleMenuItem, locales.LibraryArchive()) {
			t.Fatalf("已归档的条目不该再给「归档」；菜单项 %q", menuLabels(r))
		}
		r.click(gfx.RoleMenuItem, locales.LibraryUnarchive())
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchStatus || b.Command.Status != "active" || !slices.Equal(b.IDs, []string{e.ID}) {
			t.Fatalf("取消归档应把这一条改回 active，实际 %+v %v", b.Command, b.IDs)
		}
	})
}

// menuLabels 是画面上全部菜单项的名字（失败时帮助定位）。
func menuLabels(r *rig) []string {
	var out []string
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleMenuItem {
			out = append(out, n.Label)
		}
	}
	return out
}

// 回收站里的条目菜单只有「恢复」与「彻底删除」；彻底删除先要确认，不能直接执行。
func TestEntryMenuInTheTrashOffersRestoreAndAsksBeforeDeletingForever(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		e := r.view().List.Entries[0]
		r.rightClickEntry(e)
		if r.has(gfx.RoleMenuItem, locales.LibraryFavorite()) || r.has(gfx.RoleMenuItem, locales.LibraryArchive()) {
			t.Fatalf("回收站里的菜单只有恢复与彻底删除；菜单项 %q", menuLabels(r))
		}
		r.click(gfx.RoleMenuItem, locales.LibraryRestore())
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchRestore || !slices.Equal(b.IDs, []string{e.ID}) {
			t.Fatalf("恢复应对这一条发恢复批次，实际 %+v %v", b.Command, b.IDs)
		}

		e = r.view().List.Entries[0]
		before := len(r.svc.Batches())
		r.rightClickEntry(e)
		r.click(gfx.RoleMenuItem, locales.LibraryDeleteForever())
		if !r.has(gfx.RoleHeading, locales.LibraryDeleteForever()) {
			t.Fatal("彻底删除应先弹确认框")
		}
		if len(r.svc.Batches()) != before {
			t.Fatal("确认之前不该请求后端")
		}
	})
}

// 表格行尾的「更多操作」在按钮左下角打开同一份条目菜单（含「编辑标签」）。
func TestMoreButtonOpensTheEntryMenuBelowTheButton(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	more := r.mustNode(gfx.RoleButton, locales.LibraryMoreActions())
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && n.Label == locales.LibraryMoreActions() && n.Bounds.X >= 240 {
			more = n
			break
		}
	}
	x, y := centerOf(more)
	r.clickAt(x, y)
	if !r.has(gfx.RoleMenuItem, locales.LibraryEditTags()) {
		t.Fatalf("「更多操作」的菜单应带「编辑标签」；菜单项 %q", menuLabels(r))
	}
	first := r.mustNode(gfx.RoleMenuItem, locales.LibraryUnfavorite()) // 第一行是置顶又收藏的那一条
	bottom := more.Bounds.Y + more.Bounds.Height
	// 锚点是按钮的左下角；按钮贴着窗口右缘，框架把弹层夹进窗口，所以画出来的菜单会比锚点靠左。
	if at := r.screen.bulk.entry.at; at.X != more.Bounds.X || at.Y != bottom {
		t.Fatalf("菜单锚点应是按钮左下角 (%.0f, %.0f)，实际 %+v", more.Bounds.X, bottom, at)
	}
	menu := r.mustNode(gfx.RoleMenu, "")
	if first.Bounds.Y < bottom || first.Bounds.Y > bottom+12 || menu.Bounds.X+menu.Bounds.Width > 1280 {
		t.Fatalf("菜单应在按钮下方、整个落在窗口里，菜单 %+v 第一项 %+v", menu.Bounds, first.Bounds)
	}
}

// AI 交接稿的子菜单：复制精简版把精简版写进剪贴板并提示，复制完整版写完整版；另存为走导出。
func TestAiHandoffSubmenuCopiesAndSavesTheEntry(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		e := r.view().List.Entries[3]
		r.rightClickEntry(e)
		r.hover(gfx.RoleMenuItem, locales.LibraryAiHandoff())
		r.click(gfx.RoleMenuItem, locales.LibraryAiHandoffCopyBrief())
		want := "handoff:" + e.ID + ":brief"
		if !slices.Equal(r.copied, []string{want}) {
			t.Fatalf("剪贴板应是精简版 %q，实际 %q", want, r.copied)
		}
		if !r.hasText(locales.LibraryAiHandoffCopied(withThousands(len(want)))) {
			t.Fatalf("应提示已复制；画面文字 %q", r.session.Texts())
		}

		r.rightClickEntry(e)
		r.hover(gfx.RoleMenuItem, locales.LibraryAiHandoff())
		r.click(gfx.RoleMenuItem, locales.LibraryAiHandoffCopyFull())
		if full := "handoff:" + e.ID + ":full"; len(r.copied) != 2 || r.copied[1] != full {
			t.Fatalf("复制完整版应把完整版写进剪贴板 %q，实际 %q", full, r.copied)
		}

		r.rightClickEntry(e)
		r.hover(gfx.RoleMenuItem, locales.LibraryAiHandoff())
		r.click(gfx.RoleMenuItem, locales.LibraryAiHandoffSave())
		if got := r.svc.Exports(); len(got) != 1 || got[0].Text != "handoff:"+e.ID+":full" {
			t.Fatalf("另存为应导出完整版，实际 %+v", got)
		}
	})
}

// 菜单开着时切走工作区再回来：菜单是界面状态，随工作区卸载一起丢掉。
func TestEntryMenuAndConfirmAreDroppedWhenTheWorkspaceUnmounts(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	e := r.view().List.Entries[3]
	r.rightClickEntry(e)
	if !r.has(gfx.RoleMenuItem, locales.LibraryFavorite()) {
		t.Fatal("菜单应已打开")
	}
	r.wsHidden = true
	r.frames(3)
	r.wsHidden = false
	r.frames(3)
	if r.has(gfx.RoleMenuItem, locales.LibraryFavorite()) {
		t.Fatal("工作区卸载再挂载后，菜单应已丢掉")
	}

	r.goTrash()
	r.selectIDs(r.ids(0, 0))
	r.click(gfx.RoleButton, locales.LibraryDeleteForever())
	if !r.has(gfx.RoleHeading, locales.LibraryDeleteForever()) {
		t.Fatal("确认框应已打开")
	}
	r.wsHidden = true
	r.frames(3)
	r.wsHidden = false
	r.frames(3)
	if r.has(gfx.RoleHeading, locales.LibraryDeleteForever()) {
		t.Fatal("工作区卸载再挂载后，确认框应已丢掉")
	}
}

// 表格行里的收藏钮翻转收藏；回收站里行尾是「恢复」与「彻底删除」，后者同样先确认。
func TestTableRowActionsReachTheService(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	e := r.view().List.Entries[1] // item-000050：置顶、没收藏，第一枚「收藏」钮是它的
	r.click(gfx.RoleButton, locales.LibraryFavorite())
	calls := r.svc.UpdateCalls()
	if len(calls) != 1 || calls[0].ID != e.ID || calls[0].Patch.IsFavorite == nil || !*calls[0].Patch.IsFavorite {
		t.Fatalf("行里的收藏钮应对这一条发 isFavorite=true，实际 %+v", calls)
	}
	if !r.view().List.Entries[1].IsFavorite {
		t.Fatal("服务回复之后这一行应显示为已收藏")
	}

	r.goTrash()
	first := r.view().List.Entries[0]
	r.click(gfx.RoleButton, locales.LibraryRestore())
	if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchRestore || !slices.Equal(b.IDs, []string{first.ID}) {
		t.Fatalf("行里的「恢复」应对这一条发恢复批次，实际 %+v %v", b.Command, b.IDs)
	}

	first = r.view().List.Entries[0]
	before := len(r.svc.Batches())
	r.click(gfx.RoleButton, locales.LibraryDeleteForever())
	if !r.has(gfx.RoleHeading, locales.LibraryDeleteForever()) || len(r.svc.Batches()) != before {
		t.Fatal("行里的「彻底删除」应先弹确认框，不直接请求后端")
	}
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchDelete || !slices.Equal(b.IDs, []string{first.ID}) {
		t.Fatalf("确认后应对这一条发彻底删除，实际 %+v %v", b.Command, b.IDs)
	}
}

// 批量条取代工具条时，工具条自己的菜单随之卸载：选中后排序菜单已关，清掉选择换回工具条时它仍是关的。
func TestSelectingEntriesClosesTheToolbarSortMenu(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	sortButton := func() (x, y float32) {
		t.Helper()
		for _, n := range r.session.Access() {
			if n.Role == gfx.RoleCombobox && strings.HasPrefix(n.Label, locales.LibrarySortBy()+":") {
				return centerOf(n)
			}
		}
		t.Fatal("找不到排序钮")
		return 0, 0
	}
	x, y := sortButton()
	r.clickAt(x, y)
	if len(menuLabels(r)) == 0 {
		t.Fatal("点排序钮应打开排序菜单")
	}
	r.selectIDs(r.ids(2, 3))
	if len(menuLabels(r)) != 0 {
		t.Fatalf("批量条取代工具条后，排序菜单应已关闭；菜单项 %q", menuLabels(r))
	}
	r.keys("Escape")
	if len(menuLabels(r)) != 0 {
		t.Fatalf("换回工具条时排序菜单仍应是关的；菜单项 %q", menuLabels(r))
	}
	sortButton()
}

// 批量条被换下时，它开着的「移动到合集」菜单随之丢掉（渲染层里菜单是批量条自己的状态，卸载即销毁）：
// 菜单开着、多选在后台被清掉（比如另一批命令把这几条移走），再选回来，菜单不会自己回来。
func TestUnmountingTheBulkBarDropsItsOpenMoveMenu(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		const collection = "工作" // 夹具里的合集；菜单里是菜单项，侧栏里是按钮，两者角色不同
		r.selectIDs(r.ids(0, 1))
		r.click(gfx.RoleButton, locales.LibraryBulkMoveTitle())
		if !r.has(gfx.RoleMenuItem, collection) {
			t.Fatalf("前提：应打开「移动到合集」菜单；菜单项 %q", menuLabels(r))
		}
		r.store.ClearSelection()
		r.frames(3)
		r.selectIDs(r.ids(2, 3))
		if r.has(gfx.RoleMenuItem, collection) {
			t.Fatalf("批量条卸载之后菜单应已丢掉，重新选中不该让它回来；菜单项 %q", menuLabels(r))
		}
	})
}

// 「焦点交给工具条」的交接是工具条自己的状态：交接还没落地（要晚一帧）工具条就被批量条换下时，交接随工具条一起作废；
// 清掉多选换回工具条，焦点不会被一个过期的交接抢到工具条上。
func TestAPendingToolbarFocusHandoffDoesNotSurviveTheToolbarUnmount(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.screen.pane.toolbarFocus = toolbarFocusWanted // 组合筛选入口刚因筛选取消而卸载，工具条要接焦点
	r.selectIDs(r.ids(0, 1))
	r.store.ClearSelection()
	r.frames(3)
	if r.screen.pane.toolbarFocus != toolbarFocusNone {
		t.Fatalf("换回工具条之后不该还有交接在等，实际状态 %v", r.screen.pane.toolbarFocus)
	}
	if label, _, ok := r.focusedLabel(); ok {
		t.Fatalf("过期的交接不该把焦点抢到工具条上，焦点在 %q", label)
	}
}

// 模块重新进入（Enter）时，菜单、确认框与标签浮层都复位。
func TestEnteringTheModuleDropsTheBulkState(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.goTrash()
	r.selectIDs(r.ids(0, 0))
	r.click(gfx.RoleButton, locales.LibraryDeleteForever())
	if !r.has(gfx.RoleHeading, locales.LibraryDeleteForever()) {
		t.Fatal("确认框应已打开")
	}
	r.screen.Enter()
	r.frames(3)
	if r.has(gfx.RoleHeading, locales.LibraryDeleteForever()) {
		t.Fatal("重新进入模块后确认框应已丢掉")
	}
}
