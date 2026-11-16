package imports

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Filter 是侧栏筛选组的取值（渲染层 ImportFilter）。
//
// active 合并 pending 与 processing；degraded 是 completed 的**子集**而不是另一档状态——那些任务确实入了库，
// 只是内容有缺失，所以两档会重复计数。
type Filter string

const (
	FilterAll       Filter = "all"
	FilterActive    Filter = "active"
	FilterCompleted Filter = "completed"
	FilterDegraded  Filter = "degraded"
	FilterDuplicate Filter = "duplicate"
	FilterFailed    Filter = "failed"
	FilterCanceled  Filter = "canceled"
)

// 来源筛选的取值。
const (
	OriginAll     = "all"
	OriginDesktop = "desktop"
	OriginMobile  = "mobile"
)

// Counts 是侧栏各档的计数。degraded 与 completed 有意重复计数，所以各档之和不等于 all。
type Counts struct {
	All, Active, Completed, Degraded, Duplicate, Failed, Canceled int
}

// Of 返回某一档的计数。
func (c Counts) Of(f Filter) int {
	switch f {
	case FilterActive:
		return c.Active
	case FilterCompleted:
		return c.Completed
	case FilterDegraded:
		return c.Degraded
	case FilterDuplicate:
		return c.Duplicate
	case FilterFailed:
		return c.Failed
	case FilterCanceled:
		return c.Canceled
	}
	return c.All
}

// IsActive 报告任务是否在队列里（排队或处理中）。
func IsActive(task services.ImportTask) bool {
	return task.Status == services.ImportStatusPending || task.Status == services.ImportStatusProcessing
}

// IsDegraded 报告任务是否入库了但内容有缺失（转写失败等）；列表上不能只给一枚绿色的「已完成」。
func IsDegraded(task services.ImportTask) bool {
	return task.Status == services.ImportStatusCompleted && task.Warning != ""
}

// CountByFilter 统计各档任务数（渲染层 countByFilter）。
func CountByFilter(tasks []services.ImportTask) Counts {
	c := Counts{All: len(tasks)}
	for _, task := range tasks {
		switch {
		case IsActive(task):
			c.Active++
		case task.Status == services.ImportStatusCompleted:
			c.Completed++
		case task.Status == services.ImportStatusDuplicate:
			c.Duplicate++
		case task.Status == services.ImportStatusFailed:
			c.Failed++
		case task.Status == services.ImportStatusCanceled:
			c.Canceled++
		}
		// 有意与 completed 重复计数：降级的任务同样是完成了的
		if IsDegraded(task) {
			c.Degraded++
		}
	}
	return c
}

func matchesFilter(task services.ImportTask, f Filter) bool {
	switch f {
	case FilterAll:
		return true
	case FilterActive:
		return IsActive(task)
	case FilterDegraded:
		return IsDegraded(task)
	}
	return task.Status == string(f)
}

// FilterTasks 求列表的可见范围。搜索同时匹配显示名、原始输入、错误与警告——采集成功后显示名会换成真实标题，
// 只匹配显示名的话，用手里的链接反而搜不到那条任务。origin 缺省（旧任务）按桌面端算。
func FilterTasks(tasks []services.ImportTask, f Filter, query, origin string) []services.ImportTask {
	keyword := strings.ToLower(strings.TrimSpace(query))
	out := make([]services.ImportTask, 0, len(tasks))
	for _, task := range tasks {
		if origin != "" && origin != OriginAll {
			taskOrigin := task.Origin
			if taskOrigin == "" {
				taskOrigin = OriginDesktop
			}
			if taskOrigin != origin {
				continue
			}
		}
		if !matchesFilter(task, f) {
			continue
		}
		if keyword != "" &&
			!strings.Contains(strings.ToLower(task.DisplayName), keyword) &&
			!strings.Contains(strings.ToLower(task.SourceInput), keyword) &&
			!strings.Contains(strings.ToLower(task.Error), keyword) &&
			!strings.Contains(strings.ToLower(task.Warning), keyword) {
			continue
		}
		out = append(out, task)
	}
	return out
}

// pageSize 是首页与翻页的每页条数。
const pageSize = 50

// removalUndoWindow 是撤销提示条的停留时长；撤销窗口与它一致。
const removalUndoWindow = 8 * time.Second

// countsRefreshDelay 合并密集推送：只更新计数，不把用户已经翻开的历史重置回第一页。
const countsRefreshDelay = 250 * time.Millisecond

// View 是界面每帧读取的快照：切片都是写时复制的，拿到之后不会被后台改动。
type View struct {
	Tasks       []services.ImportTask
	Visible     []services.ImportTask // 按当前筛选、搜索与来源过滤后的任务
	Selected    []string              // 选中的任务 id，按选中顺序（最后一个是 Shift 点选的锚点）
	HasLoaded   bool
	LoadError   string
	ActiveCount int // 进行中任务数（rail 角标）：运行中加排队
	Total       int
	Counts      Counts
	NextCursor  string
	LoadingMore bool
	Queue       services.ImportQueueState
	Origin      string
	Filter      Filter
	Query       string
}

// IsSelected 报告任务是否在选中集合里。
func (v View) IsSelected(id string) bool {
	for _, s := range v.Selected {
		if s == id {
			return true
		}
	}
	return false
}

// ScheduledRemoval 是一次待提交的移除：撤销窗口内任务只是从界面隐藏。
type ScheduledRemoval struct {
	Count int
	// Undo 在窗口内撤销：任务原样回到列表。
	Undo func()
	// Commit 立即提交；窗口结束时自动调用。
	Commit func()
}

// Store 是采集队列的状态（渲染层 import.store.ts）：列表、筛选、选择、批量操作与后台推送。
//
// 方法都是同步的、可从任意 goroutine 调用；界面线程只读 View。变化后调用 invalidate 请求重绘。
type Store struct {
	svc        services.Imports
	guard      kit.Guard
	invalidate func()

	// After 注册一个延时回调并返回取消函数；默认是真实定时器，测试注入可手动触发的假实现。
	After func(d time.Duration, fn func()) (stop func())
	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())
	// OnCompleted 在某个任务变成已完成时调用（刷新知识库视图、标签与语义索引）；可空。
	OnCompleted func(services.ImportTask)
	// RefreshInbox 在任务变成已完成、失败或重复，或撤销窗口里的移除提交之后调用（刷新处理中心）；可空。
	RefreshInbox func()

	mu   sync.Mutex
	view View

	// 查询代次同时保护首页和翻页，避免快速切换来源时旧结果覆盖新筛选。
	listRev, fetchRev int
	// 推送可能先于列表请求回执到达；旧快照不能把终态覆盖回处理中。
	taskRev int

	// 撤销窗口内已从界面隐藏、尚未提交给后端的移除。刷新、翻页与后端推送都必须绕开它，否则被删的行会在窗口内闪回来。
	removed map[string]services.ImportTask
	// 尚未提交的移除；退出页面前统一提交，避免关窗口把「已删除」的行又带回来。
	pendingCommits map[int]func()
	nextCommit     int

	countsTimer func() // 非 nil 表示计数刷新已排队
}

// NewStore 创建一个队列状态。svc 不能为空。
func NewStore(svc services.Imports, guard kit.Guard, invalidate func()) *Store {
	s := &Store{
		svc:        svc,
		guard:      guard,
		invalidate: invalidate,
		After: func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, fn)
			return func() { t.Stop() }
		},
		Spawn:          func(fn func()) { go fn() },
		removed:        map[string]services.ImportTask{},
		pendingCommits: map[int]func(){},
	}
	s.view = View{
		Origin: OriginAll,
		Filter: FilterAll,
		Queue:  services.ImportQueueState{Concurrency: 2},
	}
	return s
}

// View 返回当前快照。
func (s *Store) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

func (s *Store) wake() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

// refreshVisibleLocked 在任务、筛选、搜索或来源变化后重算可见列表。
func (s *Store) refreshVisibleLocked() {
	s.view.Visible = FilterTasks(s.view.Tasks, s.view.Filter, s.view.Query, s.view.Origin)
}

// ---- 筛选、搜索与来源 ----

// SetOrigin 切换来源筛选（立即生效）并在后台重新读取列表。
func (s *Store) SetOrigin(ctx context.Context, origin string) {
	s.mu.Lock()
	s.listRev++
	s.view.Origin, s.view.Selected, s.view.NextCursor, s.view.LoadingMore = origin, nil, "", false
	s.refreshVisibleLocked()
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.Fetch(ctx) })
}

// SetFilter 切换状态筛选（立即生效）并在后台重新读取列表。
func (s *Store) SetFilter(ctx context.Context, f Filter) {
	s.mu.Lock()
	s.listRev++
	s.view.Filter, s.view.Selected, s.view.NextCursor, s.view.LoadingMore = f, nil, "", false
	s.refreshVisibleLocked()
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.Fetch(ctx) })
}

// SetQuery 只改搜索词：重新读取由界面防抖后触发（渲染层 150 ms）。
func (s *Store) SetQuery(query string) {
	s.mu.Lock()
	s.listRev++
	s.view.Query, s.view.Selected, s.view.NextCursor, s.view.LoadingMore = query, nil, "", false
	s.refreshVisibleLocked()
	s.mu.Unlock()
	s.wake()
}

// ---- 选择 ----

// ToggleSelection 切换一项的选中状态。
func (s *Store) ToggleSelection(id string) {
	s.mu.Lock()
	next := make([]string, 0, len(s.view.Selected)+1)
	found := false
	for _, candidate := range s.view.Selected {
		if candidate == id {
			found = true
			continue
		}
		next = append(next, candidate)
	}
	if !found {
		next = append(next, id)
	}
	s.view.Selected = next
	s.mu.Unlock()
	s.wake()
}

// RangeSelectTo 是 Shift 点选：从上一个选中项到 id 之间的可见任务全选；锚点或目标不在可见列表里时只选 id。
func (s *Store) RangeSelectTo(id string) {
	s.mu.Lock()
	visible := s.view.Visible
	anchorID := ""
	if n := len(s.view.Selected); n > 0 {
		anchorID = s.view.Selected[n-1]
	}
	target, anchor := indexOfTask(visible, id), indexOfTask(visible, anchorID)
	if target < 0 || anchor < 0 {
		s.view.Selected = []string{id}
	} else {
		from, to := min(anchor, target), max(anchor, target)
		merged := append([]string(nil), s.view.Selected...)
		seen := make(map[string]bool, len(merged))
		for _, sel := range merged {
			seen[sel] = true
		}
		for _, task := range visible[from : to+1] {
			if !seen[task.ID] {
				seen[task.ID] = true
				merged = append(merged, task.ID)
			}
		}
		s.view.Selected = merged
	}
	s.mu.Unlock()
	s.wake()
}

func indexOfTask(tasks []services.ImportTask, id string) int {
	for i, task := range tasks {
		if task.ID == id {
			return i
		}
	}
	return -1
}

// SelectVisible 全选当前可见的任务。
func (s *Store) SelectVisible() {
	s.mu.Lock()
	ids := make([]string, len(s.view.Visible))
	for i, task := range s.view.Visible {
		ids[i] = task.ID
	}
	s.view.Selected = ids
	s.mu.Unlock()
	s.wake()
}

// ClearSelection 取消全部选择。
func (s *Store) ClearSelection() {
	s.mu.Lock()
	s.view.Selected = nil
	s.mu.Unlock()
	s.wake()
}

// ---- 读取 ----

func mergeActiveFirst(active, entries []services.ImportTask) []services.ImportTask {
	seen := make(map[string]bool, len(active)+len(entries))
	out := make([]services.ImportTask, 0, len(active)+len(entries))
	for _, list := range [][]services.ImportTask{active, entries} {
		for _, task := range list {
			if !seen[task.ID] {
				seen[task.ID] = true
				out = append(out, task)
			}
		}
	}
	return out
}

func (s *Store) hidePendingRemovalsLocked(tasks []services.ImportTask) []services.ImportTask {
	if len(s.removed) == 0 {
		return tasks
	}
	out := make([]services.ImportTask, 0, len(tasks))
	for _, task := range tasks {
		if _, hidden := s.removed[task.ID]; !hidden {
			out = append(out, task)
		}
	}
	return out
}

func removedTasks(removed map[string]services.ImportTask) []services.ImportTask {
	out := make([]services.ImportTask, 0, len(removed))
	for _, task := range removed {
		out = append(out, task)
	}
	return out
}

// countsFromPage 把后端的各状态计数换成侧栏各档计数，并扣掉撤销窗口内隐藏的任务。
// degradedFromServer 为假时降级数是在已加载任务里数的，已经排除了隐藏项，不必再扣。
func (s *Store) countsFromPageLocked(page services.ImportPage, loaded []services.ImportTask) Counts {
	var all int
	for _, n := range page.Counts {
		all += int(n)
	}
	c := Counts{
		All:       all,
		Active:    int(page.Counts["pending"] + page.Counts["processing"]),
		Completed: int(page.Counts["completed"]),
		Degraded:  int(page.DegradedCount),
		Duplicate: int(page.Counts["duplicate"]),
		Failed:    int(page.Counts["failed"]),
		Canceled:  int(page.Counts["canceled"]),
	}
	if len(s.removed) == 0 {
		return c
	}
	return subtractCounts(c, removedTasks(s.removed))
}

// subtractCounts 从各档计数里扣掉一批任务（仅用于撤销窗口内的乐观更新）。
func subtractCounts(c Counts, removed []services.ImportTask) Counts {
	h := CountByFilter(removed)
	return Counts{
		All:       max(0, c.All-h.All),
		Active:    max(0, c.Active-h.Active),
		Completed: max(0, c.Completed-h.Completed),
		Degraded:  max(0, c.Degraded-h.Degraded),
		Duplicate: max(0, c.Duplicate-h.Duplicate),
		Failed:    max(0, c.Failed-h.Failed),
		Canceled:  max(0, c.Canceled-h.Canceled),
	}
}

// hiddenInViewLocked 是撤销窗口内被隐藏、且属于当前筛选范围的任务数（用于修正列表总数）。
func (s *Store) hiddenInViewLocked() int {
	return len(FilterTasks(removedTasks(s.removed), s.view.Filter, s.view.Query, s.view.Origin))
}

// describeLoadError 对应渲染层 describeLoadError：错误原文，没有时给「未知错误」。
func describeLoadError(err error) string {
	if msg := strings.TrimSpace(err.Error()); msg != "" {
		return msg
	}
	return locales.CommonUnknownError()
}

// Fetch 读取首页与调度器状态。加载类失败不弹提示（用户可能只是切了个页签，不该被打断），
// 但必须记在 LoadError 里让列表区渲染成错误态——否则「读不出来」会被画成「什么都没有」。
func (s *Store) Fetch(ctx context.Context) {
	for {
		s.mu.Lock()
		revision := s.listRev
		s.fetchRev++
		request, changes := s.fetchRev, s.taskRev
		q := services.ImportQuery{Origin: s.view.Origin, Status: string(s.view.Filter), Query: s.view.Query, PageSize: pageSize}
		s.view.LoadError = ""
		s.mu.Unlock()
		s.wake()

		page, err := s.svc.List(ctx, q)
		var queue services.ImportQueueState
		if err == nil {
			queue, err = s.svc.QueueState(ctx)
		}

		s.mu.Lock()
		current := revision == s.listRev && request == s.fetchRev
		if !current {
			s.mu.Unlock()
			return
		}
		if err != nil {
			s.view.LoadError = describeLoadError(err)
			s.view.HasLoaded = true
			s.mu.Unlock()
			s.wake()
			return
		}
		if changes != s.taskRev {
			// 请求期间又有推送到达：这份快照可能已经过时，重读一次（继续下一轮）。
			s.mu.Unlock()
			continue
		}
		tasks := s.hidePendingRemovalsLocked(mergeActiveFirst(page.Active, page.Entries))
		alive := make(map[string]bool, len(tasks))
		for _, task := range tasks {
			alive[task.ID] = true
		}
		var selected []string
		for _, id := range s.view.Selected {
			// 清理或删除之后选中集合里可能留着已经不存在的 id
			if alive[id] {
				selected = append(selected, id)
			}
		}
		s.view.Tasks, s.view.LoadingMore = tasks, false
		s.view.ActiveCount = queue.RunningCount + queue.PendingCount
		s.view.Total = max(0, int(page.Total)-s.hiddenInViewLocked())
		s.view.Counts = s.countsFromPageLocked(page, tasks)
		s.view.NextCursor, s.view.Queue, s.view.Selected = page.NextCursor, queue, selected
		s.view.HasLoaded = true
		s.refreshVisibleLocked()
		s.mu.Unlock()
		s.wake()
		return
	}
}

// LoadMore 翻下一页历史。失败走错误提示，列表保持原样。
func (s *Store) LoadMore(ctx context.Context) {
	s.mu.Lock()
	cursor := s.view.NextCursor
	if cursor == "" || s.view.LoadingMore {
		s.mu.Unlock()
		return
	}
	revision, request, changes := s.listRev, s.fetchRev, s.taskRev
	q := services.ImportQuery{Origin: s.view.Origin, Status: string(s.view.Filter), Query: s.view.Query, PageSize: pageSize, Cursor: cursor}
	s.view.LoadingMore = true
	s.mu.Unlock()
	s.wake()

	page, err := s.svc.List(ctx, q)

	s.mu.Lock()
	if revision != s.listRev || request != s.fetchRev {
		s.mu.Unlock()
		return
	}
	s.view.LoadingMore = false
	if err != nil {
		s.mu.Unlock()
		s.wake()
		s.guard.Report(locales.ImportsActionLoadMore(), err)
		return
	}
	if changes == s.taskRev {
		kept := make([]services.ImportTask, 0, len(s.view.Tasks)+len(page.Entries))
		for _, task := range s.view.Tasks {
			if !IsActive(task) {
				kept = append(kept, task)
			}
		}
		tasks := s.hidePendingRemovalsLocked(mergeActiveFirst(page.Active, append(kept, page.Entries...)))
		s.view.Tasks = tasks
		s.view.Counts = s.countsFromPageLocked(page, tasks)
		s.view.Total = max(0, int(page.Total)-s.hiddenInViewLocked())
		s.view.NextCursor = page.NextCursor
		s.refreshVisibleLocked()
	}
	s.mu.Unlock()
	s.wake()
}

// ---- 变更类操作 ----

// TogglePaused 暂停或继续队列。
func (s *Store) TogglePaused(ctx context.Context) {
	s.guard.Run(locales.ImportsActionToggleQueue(), func() error {
		queue, err := s.svc.SetPaused(ctx, !s.View().Queue.Paused)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.view.Queue = queue
		s.mu.Unlock()
		s.wake()
		return nil
	})
}

// Enqueue 入队一批输入并刷新列表；失败原样返回，由调用方决定怎么提示。
func (s *Store) Enqueue(ctx context.Context, inputs []services.EnqueueImportInput) ([]services.ImportTask, error) {
	created, err := s.svc.Enqueue(ctx, inputs)
	if err != nil {
		return nil, err
	}
	s.Fetch(ctx)
	return created, nil
}

// CancelTask 取消一个任务。
func (s *Store) CancelTask(ctx context.Context, id string) bool {
	return s.guard.Run(locales.ImportsActionCancel(), func() error {
		if err := s.svc.Cancel(ctx, id); err != nil {
			return err
		}
		s.Fetch(ctx)
		return nil
	})
}

// RetryTask 重试一个任务；任务已不存在时报错。
func (s *Store) RetryTask(ctx context.Context, id string, opts services.ImportRetryOptions) bool {
	return s.guard.Run(locales.ImportsActionRetry(), func() error {
		found, err := s.svc.Retry(ctx, id, opts)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("任务已不存在，请刷新列表")
		}
		s.Fetch(ctx)
		return nil
	})
}

// runBatch 逐条独立执行。
//
// 此前是一个裸 for 循环：选中 20 条点「重试失败」，第 3 条抛异常，后 17 条一声不响地不执行，用户还以为都重试了。
// 现在失败只记账、不中断，结束后一次性把逐条原因报出来。
func (s *Store) runBatch(ids []string, action string, run func(id string) error) {
	var failures []string
	for _, id := range ids {
		if err := run(id); err != nil {
			failures = append(failures, s.nameOf(id)+"\uff1a"+err.Error())
		}
	}
	if len(failures) > 0 {
		s.guard.Report(action, errors.New(strings.Join(failures, "\n")))
	}
}

// nameOf 取任务的显示名（失败汇总里用）：先在列表里找，再看撤销窗口里隐藏的，最后退回 id。
func (s *Store) nameOf(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := indexOfTask(s.view.Tasks, id); i >= 0 {
		return s.view.Tasks[i].DisplayName
	}
	if task, ok := s.removed[id]; ok {
		return task.DisplayName
	}
	return id
}

// CancelTasks 批量取消。
func (s *Store) CancelTasks(ctx context.Context, ids []string) {
	s.runBatch(ids, locales.ImportsActionCancel(), func(id string) error { return s.svc.Cancel(ctx, id) })
	s.ClearSelection()
	s.Fetch(ctx)
}

// RetryTasks 批量重试。
func (s *Store) RetryTasks(ctx context.Context, ids []string) {
	s.runBatch(ids, locales.ImportsActionRetry(), func(id string) error {
		_, err := s.svc.Retry(ctx, id, services.ImportRetryOptions{})
		return err
	})
	s.ClearSelection()
	s.Fetch(ctx)
}

// ScheduleRemove 先在界面隐藏，窗口结束才真正提交，窗口内可撤销。任务记录只是队列的流水，
// 不含已入库的条目，值得撤销但不值得为它加软删除字段。运行中的任务与已在窗口里的任务被忽略；没有可移除的返回 nil。
func (s *Store) ScheduleRemove(ctx context.Context, ids []string, delay time.Duration) *ScheduledRemoval {
	if delay <= 0 {
		delay = removalUndoWindow
	}
	targets := make(map[string]bool, len(ids))
	for _, id := range ids {
		targets[id] = true
	}

	s.mu.Lock()
	var snapshot []services.ImportTask
	for _, task := range s.view.Tasks {
		if _, hidden := s.removed[task.ID]; targets[task.ID] && !hidden && !IsActive(task) {
			snapshot = append(snapshot, task)
		}
	}
	if len(snapshot) == 0 {
		s.mu.Unlock()
		return nil
	}
	removeIDs := make([]string, len(snapshot))
	for i, task := range snapshot {
		removeIDs[i] = task.ID
		s.removed[task.ID] = task
	}
	s.view.Tasks = s.hidePendingRemovalsLocked(s.view.Tasks)
	s.view.Total = max(0, s.view.Total-len(snapshot))
	s.view.Counts = subtractCounts(s.view.Counts, snapshot)
	var selected []string
	for _, id := range s.view.Selected {
		if !targets[id] {
			selected = append(selected, id)
		}
	}
	s.view.Selected = selected
	s.refreshVisibleLocked()
	commitID := s.nextCommit
	s.nextCommit++
	s.mu.Unlock()
	s.wake()

	// settle 把这次移除标为已了结并取消定时器；已经了结过的返回 false（提交与撤销只有一个能生效）。
	var (
		settled  bool
		stopTick func()
	)
	settle := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if settled {
			return false
		}
		settled = true
		if stopTick != nil {
			stopTick()
		}
		return true
	}
	release := func() {
		s.mu.Lock()
		for _, id := range removeIDs {
			delete(s.removed, id)
		}
		delete(s.pendingCommits, commitID)
		s.mu.Unlock()
	}
	commit := func() {
		if !settle() {
			return
		}
		// 提交期间保持隐藏，避免提交中的刷新让被删的行闪回
		s.runBatch(removeIDs, locales.ImportsActionRemove(), func(id string) error { return s.svc.Remove(ctx, id) })
		release()
		// 提交失败的任务会随这次刷新回到列表，并已有逐条原因的提示
		s.Fetch(ctx)
		if s.RefreshInbox != nil {
			s.RefreshInbox()
		}
	}
	undo := func() {
		if !settle() {
			return
		}
		release()
		s.Fetch(ctx)
	}
	stop := s.After(delay, commit)
	s.mu.Lock()
	stopTick = stop
	s.pendingCommits[commitID] = commit
	s.mu.Unlock()
	return &ScheduledRemoval{Count: len(removeIDs), Undo: undo, Commit: commit}
}

// FlushRemovals 立即提交所有尚在撤销窗口内的移除；窗口关闭前调用，避免关窗口把「已删除」的行又带回来。
func (s *Store) FlushRemovals() {
	s.mu.Lock()
	commits := make([]func(), 0, len(s.pendingCommits))
	for _, commit := range s.pendingCommits {
		commits = append(commits, commit)
	}
	s.mu.Unlock()
	for _, commit := range commits {
		commit()
	}
}

// PreviewClearTerminal 数出清理范围内会被删除的终态任务数。
func (s *Store) PreviewClearTerminal(ctx context.Context, q services.ImportClearQuery) (int, error) {
	return s.svc.PreviewClearTerminal(ctx, q)
}

// ClearTerminal 删除清理范围内的终态任务；失败时弹出错误提示并返回 0。
func (s *Store) ClearTerminal(ctx context.Context, q services.ImportClearQuery) int {
	count := 0
	ok := s.guard.Run(locales.ImportsClearConfirmTitle(), func() error {
		n, err := s.svc.ClearTerminal(ctx, q)
		if err != nil {
			return err
		}
		count = n
		s.ClearSelection()
		s.Fetch(ctx)
		return nil
	})
	if !ok {
		return 0
	}
	return count
}

// ---- 后台推送 ----

// Subscribe 订阅后端的任务变更（应用挂载时调用一次）；返回取消函数。
func (s *Store) Subscribe(ctx context.Context) (cancel func()) {
	stopTask := s.svc.OnTaskChange(func(task services.ImportTask) { s.handleChanged(ctx, task) })
	return func() {
		stopTask()
		s.mu.Lock()
		stop := s.countsTimer
		s.countsTimer = nil
		s.mu.Unlock()
		if stop != nil {
			stop()
		}
	}
}

func (s *Store) handleChanged(ctx context.Context, task services.ImportTask) {
	s.mu.Lock()
	s.taskRev++
	// 撤销窗口内的任务已从界面隐藏：只更新被保留的快照，不能把它推回列表
	if _, hidden := s.removed[task.ID]; hidden {
		s.removed[task.ID] = task
		s.mu.Unlock()
		return
	}
	if i := indexOfTask(s.view.Tasks, task.ID); i >= 0 {
		tasks := append([]services.ImportTask(nil), s.view.Tasks...)
		tasks[i] = task
		s.view.Tasks = tasks
	} else if len(FilterTasks([]services.ImportTask{task}, s.view.Filter, s.view.Query, s.view.Origin)) > 0 {
		s.view.Tasks = append([]services.ImportTask{task}, s.view.Tasks...)
	}
	s.refreshVisibleLocked()
	if s.countsTimer == nil {
		s.countsTimer = s.After(countsRefreshDelay, func() { s.refreshCounts(ctx) })
	}
	s.mu.Unlock()
	s.wake()

	if task.Status == services.ImportStatusCompleted && s.OnCompleted != nil {
		s.OnCompleted(task)
	}
	if (task.Status == services.ImportStatusCompleted || task.Status == services.ImportStatusFailed ||
		task.Status == services.ImportStatusDuplicate) && s.RefreshInbox != nil {
		s.RefreshInbox()
	}
}

// refreshCounts 合并密集推送后只更新计数与调度器状态，不把用户已经翻开的历史重置回第一页。
func (s *Store) refreshCounts(ctx context.Context) {
	s.mu.Lock()
	s.countsTimer = nil
	revision, request := s.listRev, s.fetchRev
	q := services.ImportQuery{Origin: s.view.Origin, Status: string(s.view.Filter), Query: s.view.Query, PageSize: 20}
	s.mu.Unlock()

	page, err := s.svc.List(ctx, q)
	var queue services.ImportQueueState
	if err == nil {
		queue, err = s.svc.QueueState(ctx)
	}

	s.mu.Lock()
	if revision != s.listRev || request != s.fetchRev {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.view.LoadError = describeLoadError(err)
	} else {
		s.view.Counts = s.countsFromPageLocked(page, s.view.Tasks)
		s.view.Total, s.view.Queue = int(page.Total), queue
		s.view.ActiveCount = queue.RunningCount + queue.PendingCount
	}
	s.mu.Unlock()
	s.wake()
}
