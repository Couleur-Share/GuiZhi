package library

import (
	"strconv"
	"sync/atomic"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 表格视图的详情浮层（渲染层 ItemDetailModal 与 ItemTableView 里挂它的一处，台账 B-043 / B-052）。
//
// 表格占满工作区后没有常驻详情栏：点行（或在选中行上按 Enter）先 BeginReview（冻结当前筛选的结果作整理顺序、选中这一条、读详情），
// 成功才打开浮层；浮层是 size="fullscreen" 的 Modal，里面是整理导航条加同一套详情。关闭先把未落盘的内容存好，存成才关。

// detailModal 是详情浮层的界面状态。
type detailModal struct {
	state kit.ModalState
	open  bool
	// openReq 与 closeReq 由后台动作置位（BeginReview 成功、落盘成功），界面线程在帧首取走：打开与关闭都不在界面线程里决定。
	openReq, closeReq atomic.Bool
}

// openDetail 打开一条的详情浮层（渲染层 openDetail：beginReview(id) 成功才 setDetailOpen(true)）。
func (s *Screen) openDetail(id string) {
	if s.HideDetail { // 对照场景：详情不画，行与 Enter 只选中（M2-4 之前的行为）
		s.Store.SelectItem(id)
		return
	}
	m := &s.detail.modal
	s.Store.Spawn(func() {
		if s.Store.BeginReview(s.Ctx, id) {
			m.openReq.Store(true)
			s.Store.wake() // 状态改动时 Store 已经唤醒过一次，那一帧还看不到这个标志：再唤醒一次
		}
	})
}

// closeDetail 关闭详情浮层（渲染层 handleClose：flushPendingSave 成功才 onClose）；落盘失败浮层留着，失败由头部状态说出来。
func (s *Screen) closeDetail() {
	m := &s.detail.modal
	s.Store.Spawn(func() {
		if s.Store.FlushPendingSave(s.Ctx) {
			m.closeReq.Store(true)
			s.Store.wake()
		}
	})
}

// settleDetailModal 在帧首取走后台动作的结果。
func (s *Screen) settleDetailModal() {
	m := &s.detail.modal
	if m.openReq.Swap(false) {
		m.open = true
	}
	if m.closeReq.Swap(false) {
		m.open = false
	}
}

// DetailModalOpen 报告详情浮层是否开着（窗口键盘钩子用：浮层里列表的键盘整体停用）。
func (s *Screen) DetailModalOpen() bool { return s.detail.modal.open }

// detailModalView 声明详情浮层。isReady：没有选中的条目就不开（渲染层 Boolean(selectedId)），避免闪一下上一条的内容。
func (s *Screen) detailModalView(c ui.C, v View) {
	l := s.Look
	m := &s.detail.modal
	s.settleDetailModal()
	open := m.open && v.List.SelectedID != ""
	closed := l.Modal(c.Key("detail-modal"), &m.state, kit.ModalSpec{
		Open: open, Size: kit.ModalFullscreen, DialogLabel: locales.CommonDialog(), CloseLabel: locales.CommonClose(), RawBody: true,
		Body: func(c ui.C) {
			s.reviewBar(c, v)
			// min-h-0 flex-1：浮层的内容区是 flex-col，详情填满剩下的高度。
			ui.Column(c.Key("detail")).FillWidth().Grow(1).MinHeight(0).Children(func() {
				s.itemDetail(c, v, s.closeDetail)
			})
		},
	})
	if closed {
		s.closeDetail()
	}
}

// reviewBar 是整理导航条（渲染层 ReviewNavigation）：flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-4 py-2，
// 左边是「第几条 / 共几条」，右边是上一条、下一条与「保存并下一条」；没有在整理（顺序为空）时整条不存在。
func (s *Screen) reviewBar(c ui.C, v View) {
	l := s.Look
	t := l.T
	r := v.Review
	if len(r.IDs) == 0 {
		return
	}
	index := r.Index(v.List.SelectedID)
	busy := v.Batch.Busy
	label := strconv.Itoa(index+1) + " / " + strconv.Itoa(len(r.IDs))
	if len(r.Skipped) > 0 {
		label += " · " + locales.LibraryWorkflowSkipped(len(r.Skipped))
	}
	atFirst, atLast := index <= 0, index >= len(r.IDs)-1
	ui.Flow(c.Key("review-bar")).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(2), l.Space(4), l.Space(2)+1, l.Space(4)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			// mr-auto：自动外边距吃掉这一行的剩余宽度，把按钮推到行尾；这里让文字盒子增长，效果相同。
			l.Text(c.Key("count"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground).Grow(1)
			nav := func(key, name string, disabled bool, style kit.ButtonStyle, dir int) {
				spec := kit.ButtonSpec{Label: name, Disabled: disabled, Hint: ""}
				if dir != 0 && key != "save-next" {
					spec.Hint = "Alt + →"
					if dir < 0 {
						spec.Hint = "Alt + ←"
					}
				}
				if l.Button(c.Key(key), spec, style).Clicked() {
					d := dir
					s.Store.Spawn(func() { s.Store.ReviewNavigate(s.Ctx, d) })
				}
			}
			nav("prev", locales.LibraryWorkflowPrevious(), busy || atFirst, l.UIGhostButton(kit.UIButtonSM), -1)
			nav("next", locales.LibraryWorkflowNext(), busy || atLast, l.UIGhostButton(kit.UIButtonSM), 1)
			nav("save-next", locales.LibraryWorkflowSaveNext(), busy || atLast, l.UISecondaryButton(kit.UIButtonSM), 1)
		})
}
