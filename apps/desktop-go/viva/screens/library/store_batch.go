package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 批量操作的状态与执行（渲染层 library-workflow.store 里除「连续整理导航」之外的部分）。
//
// 连续整理导航（beginReview、navigate、ReviewNavigation 条）只能从详情浮层到达，随 M2-4（台账 B-052）。
// 单条与批量走同一条路：移到回收站、恢复、彻底删除、归档、改字段都是「对若干条执行一个命令」，每条独立记账，
// 结果合成状态条里的一句话与逐条清单；失败的条目留在选择里，可以只重试它们。

// batchChunk 是一次发给后端的条数上限（渲染层按 50 一块）。
const batchChunk = 50

// BatchState 是批量状态条背后的状态（渲染层 library-workflow.store 的 notice、results、command、busy）。
type BatchState struct {
	// Notice 是状态条里的一句话：冻结了多少项、成功与失败的汇总、筛选变化清空了选择。
	Notice string
	// Results 是逐条结果；Command 是产生它们的命令（重试要用）。
	Results []services.LibraryBatchResult
	Command *services.LibraryBatchCommand
	// Busy 在批量执行中为真：期间不能再发起批量，状态条显示「处理中，已核对 N 项」。
	Busy bool
}

// Visible 报告状态条要不要显示（有一句话或有逐条结果）。
func (b BatchState) Visible() bool { return b.Notice != "" || len(b.Results) > 0 }

// Failed 是失败的条目 ID，按结果的先后。
func (b BatchState) Failed() []string { return failedIDs(b.Results) }

func failedIDs(results []services.LibraryBatchResult) []string {
	var ids []string
	for _, r := range results {
		if !r.OK {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func allOK(results []services.LibraryBatchResult) bool {
	for _, r := range results {
		if !r.OK {
			return false
		}
	}
	return true
}

// uniqueIDs 去重并保持先后（渲染层 [...new Set(ids)]）。
func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// randomID 是批次的回执键（渲染层 crypto.randomUUID）：同一批次的重试靠它复用已提交的结果。
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // 系统随机源不可用时无法继续
	}
	return hex.EncodeToString(b[:])
}

// freezeQueryLocked 是冻结与「筛选有没有变」用的查询（渲染层 currentQuery）：筛选、去空白的搜索词与排序，不含分页。mu 由调用方持有。
func (s *Store) freezeQueryLocked() services.LibraryListQuery {
	f, l := s.view.Filters, s.view.List
	return services.LibraryListQuery{
		Scope: f.Scope, CollectionID: f.CollectionID, TagID: f.TagID, Platform: f.Platform,
		Search: l.searchTerm(), SortBy: l.SortBy, SortOrder: l.SortOrder,
	}
}

func (s *Store) setNotice(text string) {
	s.mu.Lock()
	s.view.Batch.Notice = text
	s.mu.Unlock()
	s.wake()
}

// DismissBatch 收起状态条（渲染层 dismiss）：一句话与逐条结果都清掉。
func (s *Store) DismissBatch() {
	s.mu.Lock()
	s.view.Batch.Notice, s.view.Batch.Results = "", nil
	s.mu.Unlock()
	s.wake()
}

// errSelectFilterChanged 是冻结期间筛选变了的失败原因：冻结的是旧筛选的结果，不能当作新筛选的选择。
var errSelectFilterChanged = errors.New("筛选已变化，请重新选择")

// SelectAllMatching 冻结整个筛选结果并全选（渲染层 selectAllMatching）：之后新增的条目不在这次选择里。
// 返回是否成功；失败已经提示。
func (s *Store) SelectAllMatching(ctx context.Context) bool {
	return s.guard.Run(locales.LibraryActionSelectAllMatching(), func() error {
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
			return errSelectFilterChanged
		}
		s.SetSelection(ids)
		s.setNotice(locales.LibraryWorkflowFrozen(len(ids)))
		return nil
	})
}

// RetryFailed 只重试上一批里失败的条目（渲染层 retryFailed）：同一个批次、同一个命令，已提交的不会重做。
func (s *Store) RetryFailed(ctx context.Context) bool {
	s.mu.Lock()
	cmd, failed := s.view.Batch.Command, failedIDs(s.view.Batch.Results)
	s.mu.Unlock()
	if cmd == nil {
		return false
	}
	return s.execute(ctx, failed, *cmd, true)
}

// runChunk 执行一块并把回复对齐到请求的 ID（渲染层 execute 里 try 的部分）：整块失败时每一条都记同一个原因，
// 回复里缺的那条记「未返回此条结果」。原因取 String(error) 的写法，带「Error: 」前缀。
func (s *Store) runChunk(ctx context.Context, runID string, chunk []string, cmd services.LibraryBatchCommand) []services.LibraryBatchResult {
	reply, err := s.svc.ExecuteBatch(ctx, runID, chunk, cmd)
	out := make([]services.LibraryBatchResult, len(chunk))
	for i, id := range chunk {
		switch {
		case err != nil:
			out[i] = services.LibraryBatchResult{ID: id, Error: "Error: " + err.Error()}
		default:
			j := slices.IndexFunc(reply, func(r services.LibraryBatchResult) bool { return r.ID == id })
			if j < 0 {
				out[i] = services.LibraryBatchResult{ID: id, Error: locales.LibraryWorkflowNoResult()}
			} else {
				out[i] = reply[j]
			}
		}
	}
	return out
}

// execute 对一批条目执行同一个命令（渲染层 library-workflow.store 的 execute），返回是否全部成功。
//
//   - 同一时刻只跑一批：正忙时返回 false、什么也不做；
//   - 按 50 一块发给后端，每块的结果逐条并入状态条（重试时按 ID 替换原有的行）；
//   - 单条的移到回收站与彻底删除成功时不弹结果栏（前者有撤销提示、后者有确认框）；
//   - 批后选择只留失败的条目（筛选没变的话），再刷新列表与计数；选中的条目若已离开当前视图就取消选中；
//   - 整批的异常（不是单条失败）走变更失败通道，状态条仍反映已经核对的部分。
func (s *Store) execute(ctx context.Context, ids []string, cmd services.LibraryBatchCommand, retry bool) bool {
	s.mu.Lock()
	if s.view.Batch.Busy {
		s.mu.Unlock()
		return false
	}
	s.view.Batch.Busy = true
	s.mu.Unlock()
	s.wake()

	var outcome []services.LibraryBatchResult
	ok := s.guard.Run(locales.LibraryActionBatch(), func() error {
		targets := uniqueIDs(ids)
		single := len(targets) == 1 && !retry && (cmd.Kind == services.LibraryBatchTrash || cmd.Kind == services.LibraryBatchDelete)

		s.mu.Lock()
		query := s.freezeQueryLocked()
		if !retry {
			c := cmd
			s.view.Batch.Results, s.view.Batch.Notice, s.view.Batch.Command, s.batchRun = nil, "", &c, s.newID()
		}
		runID := s.batchRun
		s.mu.Unlock()
		s.wake()

		var collected []services.LibraryBatchResult
		for offset := 0; offset < len(targets); offset += batchChunk {
			chunk := targets[offset:min(offset+batchChunk, len(targets))]
			results := s.runChunk(ctx, runID, chunk, cmd)
			if cmd.Kind == services.LibraryBatchDelete && cmd.ClearEvidence && s.OnEvidenceCleared != nil {
				var cleared []string
				for _, r := range results {
					if r.OK {
						cleared = append(cleared, r.ID)
					}
				}
				s.OnEvidenceCleared(cleared)
			}
			if single {
				collected = append(collected, results...)
			} else {
				s.mu.Lock()
				kept := slices.DeleteFunc(slices.Clone(s.view.Batch.Results), func(r services.LibraryBatchResult) bool { return slices.Contains(chunk, r.ID) })
				s.view.Batch.Results = append(kept, results...)
				s.mu.Unlock()
			}
			s.wake()
		}

		s.mu.Lock()
		if single {
			outcome = collected
		} else {
			outcome = slices.Clone(s.view.Batch.Results)
		}
		unchanged := query == s.freezeQueryLocked()
		s.mu.Unlock()

		failed := failedIDs(outcome)
		if unchanged {
			s.SetSelection(failed)
		}
		s.RefreshAll(ctx)
		s.afterBatch(targets, cmd, outcome)
		if single && len(failed) == 0 {
			return nil
		}
		s.mu.Lock()
		s.view.Batch.Results = outcome
		s.view.Batch.Notice = locales.LibraryWorkflowSummary(len(outcome)-len(failed), len(failed))
		s.mu.Unlock()
		s.wake()
		return nil
	})

	s.mu.Lock()
	s.view.Batch.Busy = false
	s.mu.Unlock()
	s.wake()
	return ok && allOK(outcome)
}

// afterBatch 处理批后的选中项：删除、移到回收站、恢复成功之后条目已离开当前视图，重新选中只会得到「读取失败」，所以取消选中；
// 其余情形重新选中它（详情随 M2-4 迁入时在这里重读）。
func (s *Store) afterBatch(targets []string, cmd services.LibraryBatchCommand, outcome []services.LibraryBatchResult) {
	selected := s.View().List.SelectedID
	if selected == "" || !slices.Contains(targets, selected) {
		return
	}
	leaves := cmd.Kind == services.LibraryBatchDelete || cmd.Kind == services.LibraryBatchTrash || cmd.Kind == services.LibraryBatchRestore
	if leaves && slices.ContainsFunc(outcome, func(r services.LibraryBatchResult) bool { return r.ID == selected && r.OK }) {
		s.SelectItem("")
		return
	}
	s.SelectItem(selected)
}
