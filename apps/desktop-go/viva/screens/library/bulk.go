package library

import (
	"slices"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 批量与条目操作的界面一侧（渲染层 ItemBulkBar、item-menus.tsx 的 useItemMenus 与 ItemConfirmDialog、LibraryBatchResults、
// BulkTagPopover、TagPickerPopover 里属于界面的部分）。语义与逐条记账都在 Store（store_batch.go、store_items.go）；
// 这里只有菜单、浮层与确认框的开合，以及把点击接到 Store 上。
//
// Screen 的方法都在界面线程里调用，不能被网络或磁盘卡住：所有会碰后端的动作经 Store.Spawn 放到后台，
// 后台的结果靠 Notifier 与 Store 的快照回到界面（需要界面状态的那一个——确认框的结果——靠 settleBulk 在帧首取回）。

// anchoredMenu 是一个在某点打开的菜单：状态、开关与锚点（渲染层各处的 `{ x, y }` 状态）。
type anchoredMenu struct {
	st   kit.MenuState
	open bool
	at   gfx.Point
}

func (m *anchoredMenu) show(at gfx.Point) { m.open, m.at = true, at }
func (m *anchoredMenu) close()            { m.open = false }

// entryMenu 是条目菜单（右键或「更多」打开）：渲染层 menuState 里除坐标还有右键那一刻的条目，菜单项按这份快照生成
// （菜单开着时条目被改了，菜单文字也不跟着变）。
type entryMenu struct {
	anchoredMenu
	entry services.LibraryEntry
	// tags 为真时菜单里有「编辑标签」：只有表格视图传了 onEditTags——卡片视图右侧常驻详情栏，标签行的 + 一直可见。
	tags bool
}

// confirmKind 是需要二次确认的不可逆动作（渲染层 ItemConfirmState 的 kind）。
type confirmKind uint8

const (
	confirmDeleteForever confirmKind = iota + 1
	confirmEmptyTrash
)

// itemConfirm 是「彻底删除 / 清空回收站」确认框（渲染层 ItemConfirmDialog 与 ItemConfirmState）。
type itemConfirm struct {
	open bool
	kind confirmKind
	ids  []string
	// clear 是「同时清除历史问答中的相关证据」，每次打开都复位（渲染层 useEffect(() => setClearEvidence(false), [state])）。
	clear bool
	// busy 在后台执行期间为真：确认钮转圈、复选框禁用，不能重复提交。
	busy bool
	st   kit.ConfirmState
}

// bulkState 是批量与条目操作的界面状态。Enter 时整体复位（渲染层这些状态都是组件状态，离开模块就卸载）。
type bulkState struct {
	confirm itemConfirm
	entry   entryMenu
	// move 是批量条上「移动到合集」的菜单；bulkTag 是批量打标签的浮层（它们属于批量条：条一卸载就一起消失）。
	move    anchoredMenu
	bulkTag tagPopup
	// picker 是单条的标签选择浮层（表格视图右键菜单的「编辑标签」）。
	picker tagPicker
	// details 是结果条里「逐条结果」是否展开（<details> 的 open 状态）；条收起再出现时回到折叠。
	details bool
}

// selection 是此刻多选集合的副本：动作在后台执行，期间选择可能变。
func selection(v View) []string { return slices.Clone(v.List.Selection) }

// pointerAnchor 是批量条上按钮打开菜单或浮层的位置：鼠标点是光标处（渲染层直接取 event.clientX / clientY），
// 键盘（Enter 或空格）点是原点——浏览器合成的点击事件没有坐标，菜单与浮层再夹进视口（渲染层批量条没有像侧栏
// `menuAnchor` 那样为键盘另算位置）。
func (s *Screen) pointerAnchor(canvas *gfx.Canvas, id gfx.ID) gfx.Point {
	if canvas.ClickedByKey(id) {
		return gfx.Point{}
	}
	return s.menu.up
}

// rowHit 登记一个属于条目 e 的命中盒（渲染层的 onContextMenu 挂在整行或整张卡片上，里面的按钮冒泡上来）：
// 悬停归到该条目的行组，右键在光标处打开条目菜单；tags 为真时菜单带「编辑标签」（表格视图）。
func (s *Screen) rowHit(canvas *gfx.Canvas, e services.LibraryEntry, id gfx.ID, tags bool) {
	s.cardGroups.Add(e.ID, id)
	s.watchContext(canvas, func(at gfx.Point) { s.openEntryMenu(at, e, tags) }, id)
}

// openEntryMenu 在 at 处打开条目菜单；tags 为真时带「编辑标签」。
func (s *Screen) openEntryMenu(at gfx.Point, e services.LibraryEntry, tags bool) {
	m := &s.bulk.entry
	m.entry, m.tags = e, tags
	m.show(at)
}

// openConfirm 打开确认框（渲染层 setConfirmState）；ids 是要彻底删除的条目（清空回收站不用）。
// 「同时清除证据」每次都复位；忙碌标记不动——渲染层的 busy 是确认框组件自己的状态，上一次动作还没结束时，
// 关掉再打开的框仍是忙碌的。
func (s *Screen) openConfirm(kind confirmKind, ids []string) {
	cf := &s.bulk.confirm
	cf.open, cf.kind, cf.ids, cf.clear = true, kind, slices.Clone(ids), false
}

// resetBulk 丢掉批量与条目操作的全部界面状态（工作区卸载或模块重新进入）。确认框的动作还在后台跑时，
// 它的结果作废：之后打开的新确认框不能被上一轮的结果关掉或解除忙碌。
func (s *Screen) resetBulk() {
	if s.bulk.confirm.busy {
		s.confirmGen++
	}
	s.bulk = bulkState{}
}

// confirmOutcome 是确认框后台动作的结果；gen 是发起时的代次，对不上就是上一轮的、不能用。
type confirmOutcome struct {
	gen uint64
	ok  bool
}

// settleBulk 在帧首取回确认框后台任务的结果：只有成功才关框，失败留着框、解除忙碌，可以再试
// （渲染层 confirm()：`if (ok) onClose()`，finally 里 setBusy(false)）。
func (s *Screen) settleBulk() {
	s.mu.Lock()
	done := s.confirmDone
	s.confirmDone = nil
	s.mu.Unlock()
	if done == nil || done.gen != s.confirmGen {
		return
	}
	cf := &s.bulk.confirm
	cf.busy = false
	if done.ok {
		cf.open = false
	}
}

// runConfirm 执行确认框里的动作（渲染层 confirm()）：忙碌中不重复提交。
func (s *Screen) runConfirm() {
	cf := &s.bulk.confirm
	if !cf.open || cf.busy {
		return
	}
	cf.busy = true
	kind, ids, clearEvidence, gen := cf.kind, slices.Clone(cf.ids), cf.clear, s.confirmGen
	s.Store.Spawn(func() {
		var ok bool
		if kind == confirmDeleteForever {
			ok = s.Store.DeleteForever(s.Ctx, ids, clearEvidence)
		} else {
			ok = s.Store.EmptyTrash(s.Ctx, clearEvidence)
		}
		s.mu.Lock()
		s.confirmDone = &confirmOutcome{gen: gen, ok: ok}
		s.mu.Unlock()
		s.Store.wake()
	})
}

// ---- 动作（都在后台执行；失败的提示由 Store 走变更失败通道）----

// trashWithUndo 把条目移到回收站，成功才给一个撤销窗口（渲染层 trashWithUndo）：删不删得掉由 Store 说了算，
// 删失败时错误提示已经发出，这里不能再补一句「已移到回收站」。
func (s *Screen) trashWithUndo(ids []string) {
	if len(ids) == 0 {
		return
	}
	ids = slices.Clone(ids)
	s.Store.Spawn(func() {
		if !s.Store.MoveToTrash(s.Ctx, ids) {
			return
		}
		if s.Guard.Notify != nil {
			s.Guard.Notify.Undo(locales.LibraryMovedToTrash(len(ids)), locales.CommonUndo(), func() { s.restore(ids) })
		}
	})
}

// restore 把回收站里的条目恢复。
func (s *Screen) restore(ids []string) {
	ids = slices.Clone(ids)
	s.Store.Spawn(func() { s.Store.RestoreItems(s.Ctx, ids) })
}

// setStatus 改状态（归档、取消归档）。
func (s *Screen) setStatus(ids []string, status string) {
	ids = slices.Clone(ids)
	s.Store.Spawn(func() { s.Store.SetStatus(s.Ctx, ids, status) })
}

// bulkUpdate 批量改字段（收藏、追加标签）。
func (s *Screen) bulkUpdate(ids []string, patch services.LibraryEntryPatch) {
	ids = slices.Clone(ids)
	s.Store.Spawn(func() { s.Store.BulkUpdate(s.Ctx, ids, patch) })
}

// moveToCollection 把条目移到合集；collectionID 为空表示移出合集。
func (s *Screen) moveToCollection(ids []string, collectionID string) {
	ids = slices.Clone(ids)
	s.Store.Spawn(func() { s.Store.BulkMoveToCollection(s.Ctx, ids, collectionID) })
}

func (s *Screen) toggleFavorite(id string) {
	s.Store.Spawn(func() { s.Store.ToggleFavorite(s.Ctx, id) })
}

func (s *Screen) togglePinned(id string) {
	s.Store.Spawn(func() { s.Store.TogglePinned(s.Ctx, id) })
}

func (s *Screen) setItemTags(id string, names []string) {
	names = slices.Clone(names)
	s.Store.Spawn(func() { s.Store.SetItemTags(s.Ctx, id, names) })
}

func (s *Screen) supplementWechat(ids []string) {
	ids = slices.Clone(ids)
	s.Store.Spawn(func() { s.Store.SupplementWechat(s.Ctx, ids) })
}

func (s *Screen) copyHandoff(id string, full bool) {
	s.Store.Spawn(func() { s.Store.CopyAiHandoff(s.Ctx, id, full) })
}

func (s *Screen) saveHandoff(id string) {
	s.Store.Spawn(func() { s.Store.SaveAiHandoff(s.Ctx, id) })
}

// selectAllMatching 冻结整个筛选结果并全选（渲染层 library-workflow.store 的 selectAllMatching）。
func (s *Screen) selectAllMatching() {
	s.Store.Spawn(func() { s.Store.SelectAllMatching(s.Ctx) })
}

// deleteKey 是 Delete 键（渲染层 use-item-keyboard 的 onDelete 与两个视图的 handleKeyboardDelete）：
// 有多选删多选，否则删当前条目；回收站里是彻底删除，要确认；其余范围直接移到回收站并给撤销窗口。
// 返回键有没有被接走（没有可删的条目时放行）。
func (s *Screen) deleteKey(v View) bool {
	ids := selection(v)
	if len(ids) == 0 && v.List.SelectedID != "" {
		ids = []string{v.List.SelectedID}
	}
	if len(ids) == 0 {
		return false
	}
	if v.Filters.Scope == services.LibraryScopeTrash {
		s.openConfirm(confirmDeleteForever, ids)
		return true
	}
	s.trashWithUndo(ids)
	return true
}
