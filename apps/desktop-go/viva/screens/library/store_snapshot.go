package library

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 公众号网页快照（渲染层 WebSnapshotPane）背后的异步状态：读取快照、补采（入队后轮询导入任务）、取消补采、导出 HTML。
//
// 渲染层里这些是 WebSnapshotPane 的组件状态，面板随条目重建（ContentPanel 里 key={item.id}），所以这里同样挂在详情上，
// 换条目、清详情、Store.Close 时整体作废：snapshotState.gen 加一，旧一代在途的读取与轮询回来发现代号变了就整个丢弃、不再弹提示，
// 轮询的定时器也随之停掉（补采任务本身已经入了队，会在后台跑完，只是这一代界面不再等它）。
// 读取另有序号 loadSeq：同一代里重新读取（换版本、重试、补采完成）会让更早的读取作废，乱序返回的旧结果不能盖掉新的。
//
// 排版模式（原文 / 标准）、宽度档、目录开合都只属于界面，在 Screen 的面板状态里（detailsnapshot.go）。

// snapshotPollInterval 是补采进度的轮询间隔（渲染层 setInterval 1000）。
const snapshotPollInterval = time.Second

// SnapshotView 是网页快照面板背后的数据状态；ItemID、VersionID 是最近一次读取请求的条目与版本（空版本表示当前版本）。
type SnapshotView struct {
	ItemID    string
	VersionID string
	// Loading 是读取在途：重新读取一开始就清掉旧的 View（渲染层 setView(null)），界面显示「正在读取网页快照…」。
	Loading bool
	// View 是读到的快照；没有读到（读取中、失败、条目还没有快照数据）时为 nil。Store 每次读取换一个新对象，界面线程拿到的不会再变。
	// 没有保存过快照时 View 非 nil、View.Version 为 nil（界面退回标准排版并提示补采）。
	View *services.LibraryWebSnapshotView
	// Error 是读取失败的原因（含快照损坏），空表示没有失败；失败时 View 为 nil，界面在标准排版之上给红字与「重试读取」。
	// 补采轮询读不到任务状态时也落在这里（渲染层 setError(String(e))）。
	Error string
	// SupplementBusy 是补采在途（入队中或在等任务结束）；SupplementTasks 是已入队、正在等的导入任务 ID（取消补采用）。
	SupplementBusy  bool
	SupplementTasks []string
}

// snapshotState 是网页快照的内部状态（detailState 里的一份）。
type snapshotState struct {
	// gen 是挂载代号，loadSeq 是同一代里读取请求的序号。
	gen, loadSeq int
	// stop 取消下一次轮询的定时器；没有排定时为 nil。
	stop func()
	// exporting 是一次导出在途（目录选择框开着）：挡住重复触发，它不随代号重置——离开条目后那个框仍然开着。
	exporting bool
}

// resetSnapshotLocked 让上一代作废、停掉轮询并清空快照（换条目、清详情）。mu 由调用方持有。
func (s *Store) resetSnapshotLocked() {
	s.stopSnapshotLocked()
	s.view.Detail.Snapshot = SnapshotView{}
}

// stopSnapshotLocked 让上一代作废并停掉轮询，不动快照数据（Close）。mu 由调用方持有。
func (s *Store) stopSnapshotLocked() {
	st := &s.detail.snapshot
	st.gen++
	st.loadSeq++
	if st.stop != nil {
		st.stop()
		st.stop = nil
	}
}

// describeSnapshotError 取读取失败的原因：空的换成「读取网页快照失败」（渲染层 `result.error || translate("library.snapshotLoadFailed")`）。
func describeSnapshotError(msg string) string {
	if msg = strings.TrimSpace(msg); msg != "" {
		return msg
	}
	return locales.LibrarySnapshotLoadFailed()
}

// ---- 读取 ----

// LoadWebSnapshot 读取条目某个版本的网页快照（空版本是当前版本；WebSnapshotPane 随条目、版本与正文变化的 effect）。不阻塞：读取放到后台。
//
// 条目不是详情里正在显示的那一条时什么也不做。读取一开始就清掉旧的快照与错误、置为读取中；补采的忙标记不动（补采还在等任务）。
// 读取失败（含快照损坏）记下原因，界面在标准排版之上显示它与「重试读取」。
func (s *Store) LoadWebSnapshot(ctx context.Context, itemID, versionID string) {
	s.mu.Lock()
	if !s.showsLocked(itemID) {
		s.mu.Unlock()
		return
	}
	gen, seq := s.beginSnapshotLoadLocked(itemID, versionID)
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.readWebSnapshot(ctx, itemID, versionID, gen, seq) })
}

// beginSnapshotLoadLocked 把快照置为「正在读取」并返回这次读取的代号与序号。mu 由调用方持有。
func (s *Store) beginSnapshotLoadLocked(itemID, versionID string) (gen, seq int) {
	st := &s.detail.snapshot
	st.loadSeq++
	sv := &s.view.Detail.Snapshot
	sv.ItemID, sv.VersionID, sv.Loading, sv.View, sv.Error = itemID, versionID, true, nil, ""
	return st.gen, st.loadSeq
}

func (s *Store) readWebSnapshot(ctx context.Context, itemID, versionID string, gen, seq int) {
	view, err := s.svc.WebSnapshot(ctx, itemID, versionID)
	s.mu.Lock()
	current := gen == s.detail.snapshot.gen && seq == s.detail.snapshot.loadSeq
	if current {
		sv := &s.view.Detail.Snapshot
		sv.Loading = false
		switch {
		case err != nil:
			sv.Error = describeSnapshotError(err.Error())
		case view.Error != "":
			sv.Error = describeSnapshotError(view.Error)
		default:
			sv.View = &view
		}
	}
	s.mu.Unlock()
	switch {
	case err != nil:
		s.guard.Background("library.webSnapshot", err)
	case view.Error != "":
		s.guard.Background("library.webSnapshot", errors.New(view.Error))
	}
	if current {
		s.wake()
	}
}

// ---- 补采 ----

// SupplementWechatSnapshot 把详情里的公众号文章加入补采队列并等它结束（WebSnapshotPane 的 supplement）。不阻塞。
//
// 条目不是详情里正在显示的那一条、补采已在途时什么也不做（界面的钮在忙时也是禁用的）。入队失败弹带原因的错误提示并放下忙标记；
// 入队成功后每秒读一次任务状态，全部结束就放下忙标记、重新读取快照，并按结果提示「网页快照版本已保存」或「网页补采未完成」（任务的原因放进可复制的详情）。
func (s *Store) SupplementWechatSnapshot(ctx context.Context, itemID string) {
	s.mu.Lock()
	sv := &s.view.Detail.Snapshot
	if !s.showsLocked(itemID) || sv.SupplementBusy {
		s.mu.Unlock()
		return
	}
	gen := s.detail.snapshot.gen
	sv.SupplementBusy, sv.SupplementTasks = true, nil
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.runSupplement(ctx, itemID, gen) })
}

func (s *Store) runSupplement(ctx context.Context, itemID string, gen int) {
	var ids []string
	ok := s.guard.Run(locales.LibrarySnapshotSupplementWechat(), func() error {
		var err error
		if ids, err = s.svc.SupplementWechatTasks(ctx, []string{itemID}); err != nil {
			return err
		}
		if len(ids) == 0 { // 没有任务可等：渲染层此时忙标记会一直亮着，这里当作失败，让用户能重试
			return errors.New(locales.CommonUnknownError())
		}
		return nil
	})
	s.mu.Lock()
	if gen != s.detail.snapshot.gen {
		s.mu.Unlock()
		return
	}
	sv := &s.view.Detail.Snapshot
	if !ok {
		sv.SupplementBusy, sv.SupplementTasks = false, nil
		s.mu.Unlock()
		s.wake()
		return
	}
	sv.SupplementTasks = slices.Clone(ids)
	s.schedulePollLocked(ctx, itemID, gen, ids)
	s.mu.Unlock()
	s.wake()
}

// schedulePollLocked 排下一次状态读取。读取结束后才排再下一次（不会重叠）。mu 由调用方持有。
func (s *Store) schedulePollLocked(ctx context.Context, itemID string, gen int, ids []string) {
	s.detail.snapshot.stop = s.After(snapshotPollInterval, func() { s.pollSupplement(ctx, itemID, gen, ids) })
}

// pollSupplement 读一次补采任务的状态：全部结束就收尾，否则排下一次；读不到状态就停止等待并把原因记在快照的错误里。
// 已不存在的任务（被清理）一定早已结束，按已结束处理；原因是空的，不算失败。
func (s *Store) pollSupplement(ctx context.Context, itemID string, gen int, ids []string) {
	s.mu.Lock()
	if gen != s.detail.snapshot.gen {
		s.mu.Unlock()
		return
	}
	s.detail.snapshot.stop = nil
	s.mu.Unlock()

	all, err := s.svc.ImportTaskStatuses(ctx, ids)

	s.mu.Lock()
	if gen != s.detail.snapshot.gen {
		s.mu.Unlock()
		return
	}
	sv := &s.view.Detail.Snapshot
	if err != nil {
		sv.SupplementBusy, sv.SupplementTasks = false, nil
		sv.Error = describeSnapshotError(err.Error())
		s.mu.Unlock()
		s.guard.Background("library.snapshotSupplementPoll", err)
		s.wake()
		return
	}
	matched := make([]services.LibraryImportTaskStatus, 0, len(ids))
	for _, task := range all {
		if slices.Contains(ids, task.ID) {
			matched = append(matched, task)
		}
	}
	if slices.ContainsFunc(matched, func(t services.LibraryImportTaskStatus) bool { return !t.Finished() }) {
		s.schedulePollLocked(ctx, itemID, gen, ids)
		s.mu.Unlock()
		return
	}
	sv.SupplementBusy, sv.SupplementTasks = false, nil
	versionID := ""
	if sv.ItemID == itemID {
		versionID = sv.VersionID
	}
	rgen, rseq := s.beginSnapshotLoadLocked(itemID, versionID)
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.readWebSnapshot(ctx, itemID, versionID, rgen, rseq) })

	failed := false
	var reasons []string
	for _, task := range matched {
		if task.Status != services.ImportStatusCompleted {
			failed = true
		}
		if reason := firstNonEmpty(task.Error, task.Warning); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	detail := strings.Join(reasons, "\n")
	if failed {
		s.toastError(locales.LibrarySnapshotSupplementIncomplete(), detail)
		return
	}
	s.toastSuccessDetail(locales.LibrarySnapshotVersionSaved(), detail)
}

// toastSuccessDetail 弹成功提示；有详情（任务的警告）时放进可复制的详情。
func (s *Store) toastSuccessDetail(message, detail string) {
	if s.guard.Notify == nil {
		return
	}
	var opts []kit.ToastOption
	if detail != "" {
		opts = append(opts, kit.WithDetail(detail))
	}
	s.guard.Notify.Success(message, opts...)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// CancelWechatSupplement 取消正在等的补采任务（WebSnapshotPane 更多菜单的「取消补采」）。不阻塞。
// 没有在途的补采、任务还没入队时什么也不做；任务已结束或取消不了弹错误提示（后面的任务不再取消，渲染层同）。
// 取消之后的收尾（忙标记、提示、重新读取）仍由轮询发现任务已结束时统一做。
func (s *Store) CancelWechatSupplement(ctx context.Context) {
	s.mu.Lock()
	sv := s.view.Detail.Snapshot
	tasks := slices.Clone(sv.SupplementTasks)
	s.mu.Unlock()
	if !sv.SupplementBusy || len(tasks) == 0 {
		return
	}
	s.Spawn(func() {
		s.guard.Run(locales.LibrarySnapshotCancelSupplement(), func() error {
			for _, id := range tasks {
				canceled, err := s.svc.CancelImportTask(ctx, id)
				if err != nil {
					return err
				}
				if !canceled {
					return errors.New(locales.NativeLibrarySnapshotTaskEnded())
				}
			}
			return nil
		})
	})
}

// ---- 导出 ----

// ExportWebSnapshotHTML 把条目某个版本的快照导出为 HTML 目录（WebSnapshotPane 的 exportHtml；会弹目录选择框）。不阻塞。
// 用户没选目录不算失败、不提示；成功时按快照是否有缺失资源提示「已导出，部分资源有缺失」（警告）或「网页快照 HTML 已导出」；失败弹带原因的错误提示。
// 上一次导出还开着选择框时什么也不做。
func (s *Store) ExportWebSnapshotHTML(ctx context.Context, itemID, versionID string) {
	s.mu.Lock()
	if s.detail.snapshot.exporting {
		s.mu.Unlock()
		return
	}
	s.detail.snapshot.exporting = true
	s.mu.Unlock()
	s.Spawn(func() {
		var res services.LibraryWebSnapshotExportResult
		ok := s.guard.Run(locales.LibrarySnapshotExportHtml(), func() error {
			var err error
			res, err = s.svc.ExportWebSnapshotHTML(ctx, itemID, versionID)
			return err
		})
		s.mu.Lock()
		s.detail.snapshot.exporting = false
		s.mu.Unlock()
		switch {
		case !ok || res.Canceled:
		case res.Incomplete:
			s.toastWarning(locales.LibrarySnapshotExportedIncomplete(), "")
		default:
			s.toastSuccess(locales.LibrarySnapshotExported())
		}
	})
}
