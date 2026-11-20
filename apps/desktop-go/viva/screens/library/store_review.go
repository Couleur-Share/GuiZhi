package library

import (
	"context"
	"errors"
	"slices"

	"guizhi-mygo-shell/viva/locales"
)

// 连续整理（渲染层 library-workflow.store 的 beginReview、navigate 与 reviewIds、skipped；台账 B-052）。
//
// 它只能从表格视图的详情浮层到达：点行（或按 Enter）打开浮层之前先 BeginReview——冻结当前筛选的整个结果作为整理顺序、
// 选中这一条并读取详情；浮层里的「上一条 / 下一条 / 保存并下一条」与 Alt+← / Alt+→ 走 ReviewNavigate。
// 与批量执行共用同一个「忙」标志（渲染层 busy 是一个）：整理切换期间不能发起批量，批量期间也不能切换。

// ReviewState 是连续整理的顺序与进度。
type ReviewState struct {
	// IDs 是整理顺序：打开详情那一刻冻结的筛选结果（含回收站范围，不分页）；空表示没有在整理。
	// Store 整体替换它，界面线程拿到的快照不会再变。
	IDs []string
	// Skipped 是导航时跳过的条目（已被删除或读不到），去重、按先后。
	Skipped []string
}

// Index 是 id 在整理顺序里的位置；不在其中（含没有选中）为 -1。
func (r ReviewState) Index(id string) int { return slices.Index(r.IDs, id) }

// 连续整理的失败原因（渲染层写死的中文，不进 locales；errors.New 的参数免检）。
var (
	errReviewFilterChanged = errors.New("筛选已变化，请重新打开")
	errReviewSelectBlocked = errors.New("请先处理当前保存或加载错误")
	errReviewSwitchFailed  = errors.New("资料读取失败，请重试")
	errReviewNotSaved      = errors.New("保存未完成")
)

// tryBusy 抢「忙」标志：已经忙着就返回 false、什么也不改（渲染层 `if (get().busy) return false; set({ busy: true })`）。
func (s *Store) tryBusy() bool {
	s.mu.Lock()
	if s.view.Batch.Busy {
		s.mu.Unlock()
		return false
	}
	s.view.Batch.Busy = true
	s.mu.Unlock()
	s.wake()
	return true
}

// clearBusy 放下「忙」标志。
func (s *Store) clearBusy() {
	s.mu.Lock()
	s.view.Batch.Busy = false
	s.mu.Unlock()
	s.wake()
}

// BeginReview 打开连续整理（渲染层 beginReview）：冻结当前筛选的整个结果作为整理顺序，选中 id 并读取详情。
// 返回是否成功（成功后调用方才打开详情浮层）；正忙时返回 false 且不提示；失败走变更失败通道。
//
//   - 冻结期间筛选变了：冻结的是旧筛选的结果，不能当作新筛选的整理顺序；
//   - 选中失败（上一条的草稿落不了盘，或这一条读不出来）：不开浮层；
//   - 点的这一条不在冻结的结果里（例如刚被别处改出了筛选）：排到顺序的最前面，照样能整理它。
func (s *Store) BeginReview(ctx context.Context, id string) bool {
	if !s.tryBusy() {
		return false
	}
	defer s.clearBusy()
	return s.guard.Run(locales.LibraryActionOpenWorkflow(), func() error {
		s.mu.Lock()
		query := s.freezeQueryLocked()
		s.mu.Unlock()
		ids, err := s.svc.Freeze(ctx, query)
		if err != nil {
			return err
		}
		s.mu.Lock()
		changed := query != s.freezeQueryLocked()
		s.mu.Unlock()
		if changed {
			return errReviewFilterChanged
		}
		if !s.SelectItemWait(ctx, id) {
			return errReviewSelectBlocked
		}
		order := ids
		if !slices.Contains(ids, id) {
			order = append([]string{id}, ids...)
		}
		s.mu.Lock()
		s.view.Review = ReviewState{IDs: order}
		s.mu.Unlock()
		s.wake()
		return nil
	})
}

// ReviewNavigate 在整理顺序里切到上一条（direction 小于 0）或下一条（渲染层 navigate）。
// 先把当前条的草稿落盘（落不了就带着保存失败的原因报错、不切换）；再沿顺序找第一个没被删除的条目，
// 已删除或读不到的记入「已跳过」；走到头给「已到本次整理范围的边界」。返回是否成功；正忙时返回 false 且不提示。
func (s *Store) ReviewNavigate(ctx context.Context, direction int) bool {
	if !s.tryBusy() {
		return false
	}
	defer s.clearBusy()
	return s.guard.Run(locales.LibraryActionSwitchWorkflowItem(), func() error {
		if !s.FlushPendingSave(ctx) {
			s.mu.Lock()
			reason := s.view.Detail.SaveError
			s.mu.Unlock()
			if reason == "" {
				return errReviewNotSaved
			}
			return errors.New(reason)
		}
		s.mu.Lock()
		order, selected := s.view.Review.IDs, s.view.List.SelectedID
		s.mu.Unlock()
		step := 1
		if direction < 0 {
			step = -1
		}
		for i := slices.Index(order, selected) + step; i >= 0 && i < len(order); i += step {
			id := order[i]
			item, found, err := s.svc.GetItem(ctx, id)
			if err != nil {
				return err
			}
			if !found || item.DeletedAt != 0 {
				s.skipReview(id)
				continue
			}
			if !s.SelectItemWait(ctx, id) {
				return errReviewSwitchFailed
			}
			return nil
		}
		s.setNotice(locales.LibraryWorkflowBoundary())
		return nil
	})
}

// skipReview 把一条记入「已跳过」（去重、按先后；整体换一份新切片）。
func (s *Store) skipReview(id string) {
	s.mu.Lock()
	if !slices.Contains(s.view.Review.Skipped, id) {
		s.view.Review.Skipped = append(slices.Clone(s.view.Review.Skipped), id)
	}
	s.mu.Unlock()
	s.wake()
}
