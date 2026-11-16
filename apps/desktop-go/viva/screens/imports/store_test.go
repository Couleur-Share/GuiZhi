package imports

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

// 本文件对应渲染层 tests/unit/components/import-remove-undo.test.ts、import-origin-store.test.ts
// 与 tests/unit/import/import-task-meta.test.ts 里的「列表筛选」用例：状态机的代次保护、推送与翻页的竞态、撤销窗口。

// ---- 测试替身 ----

// scripted 是可编排每次 List / QueueState 返回的假服务：其余能力沿用 servicestest.Imports。
type scripted struct {
	*servicestest.Imports
	mu    sync.Mutex
	list  func(q services.ImportQuery) (services.ImportPage, error)
	queue func() (services.ImportQueueState, error)
	calls []services.ImportQuery
}

func (f *scripted) List(_ context.Context, q services.ImportQuery) (services.ImportPage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, q)
	fn := f.list
	f.mu.Unlock()
	return fn(q)
}

func (f *scripted) QueueState(context.Context) (services.ImportQueueState, error) {
	f.mu.Lock()
	fn := f.queue
	f.mu.Unlock()
	return fn()
}

func (f *scripted) listCalls() []services.ImportQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.ImportQuery(nil), f.calls...)
}

// manualClock 是手动推进的计时器：Advance 同步触发到期的回调。
type manualClock struct {
	mu     sync.Mutex
	now    time.Duration
	next   int
	timers map[int]*manualTimer
}

type manualTimer struct {
	at time.Duration
	fn func()
}

func newManualClock() *manualClock { return &manualClock{timers: map[int]*manualTimer{}} }

func (c *manualClock) After(d time.Duration, fn func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.next
	c.next++
	c.timers[id] = &manualTimer{at: c.now + d, fn: fn}
	return func() {
		c.mu.Lock()
		delete(c.timers, id)
		c.mu.Unlock()
	}
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var due []int
	for id, t := range c.timers {
		if t.at <= c.now {
			due = append(due, id)
		}
	}
	slices.Sort(due)
	fns := make([]func(), 0, len(due))
	for _, id := range due {
		fns = append(fns, c.timers[id].fn)
		delete(c.timers, id)
	}
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// 返回每次调用都从头计算的页：failedTasks 用「失败」状态，计数与条数一致。
func failedPage(entries ...services.ImportTask) services.ImportPage {
	return services.ImportPage{
		Entries: entries, Total: int64(len(entries)),
		Counts: map[string]int64{"failed": int64(len(entries))},
	}
}

func idle() (services.ImportQueueState, error) {
	return services.ImportQueueState{Concurrency: 2}, nil
}

func task(id, origin string) services.ImportTask {
	return services.ImportTask{
		ID: id, Origin: origin, SourceKind: "text", SourceInput: id, DisplayName: id,
		Status: services.ImportStatusFailed, Error: "网络错误", CreatedAt: 1, UpdatedAt: 1,
	}
}

// recLog 记录写进日志的失败（Guard 在弹提示的同时写日志，测试以日志断言失败汇总）。
type recLog struct {
	mu   sync.Mutex
	logs []string
}

func (r *recLog) AppError(scope, action, message string) {
	r.mu.Lock()
	r.logs = append(r.logs, action+"|"+message)
	r.mu.Unlock()
}

func (r *recLog) entries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.logs...)
}

type harness struct {
	t      *testing.T
	svc    *scripted
	store  *Store
	clock  *manualClock
	log    *recLog
	wakes  int
	wakeMu sync.Mutex
}

func newHarness(t *testing.T, list func(q services.ImportQuery) (services.ImportPage, error)) *harness {
	t.Helper()
	h := &harness{t: t, svc: &scripted{Imports: &servicestest.Imports{}, list: list, queue: idle}, clock: newManualClock()}
	h.log = &recLog{}
	h.store = NewStore(h.svc, kit.Guard{Notify: kit.NewNotifier(nil), Log: h.log}, func() { h.wakeMu.Lock(); h.wakes++; h.wakeMu.Unlock() })
	h.store.After = h.clock.After
	h.store.Spawn = func(fn func()) { fn() } // 默认同步；并发用例自己改回 goroutine
	return h
}

func ids(tasks []services.ImportTask) []string {
	out := make([]string, len(tasks))
	for i, task := range tasks {
		out[i] = task.ID
	}
	return out
}

func equalStrings(a, b []string) bool { return slices.Equal(a, b) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

// ---- 列表筛选（import-task-meta.test.ts「列表筛选」） ----

func TestFilterAndCountMatchRenderer(t *testing.T) {
	mk := func(id, status string, patch func(*services.ImportTask)) services.ImportTask {
		task := services.ImportTask{ID: id, DisplayName: id, Status: status, SourceKind: "url", SourceInput: "https://example.com/" + id}
		if patch != nil {
			patch(&task)
		}
		return task
	}
	tasks := []services.ImportTask{
		mk("a", "processing", func(x *services.ImportTask) { x.DisplayName = "正在采集" }),
		mk("b", "pending", func(x *services.ImportTask) { x.DisplayName = "排队中" }),
		mk("c", "completed", func(x *services.ImportTask) { x.DisplayName = "SQLite 队列" }),
		mk("d", "failed", func(x *services.ImportTask) { x.DisplayName = "失败的" }),
		mk("e", "duplicate", func(x *services.ImportTask) { x.DisplayName = "重复的" }),
		mk("f", "canceled", func(x *services.ImportTask) { x.DisplayName = "取消的" }),
		mk("g", "completed", func(x *services.ImportTask) {
			x.DisplayName, x.Warning = "采到了但没有文字稿", "文字稿生成失败：本地转写服务启动失败"
		}),
	}
	if got := ids(FilterTasks(tasks, FilterActive, "", "")); !equalStrings(got, []string{"a", "b"}) {
		t.Errorf("active 合并 pending 与 processing：%v", got)
	}
	if got := ids(FilterTasks(tasks, FilterFailed, "", "")); !equalStrings(got, []string{"d"}) {
		t.Errorf("failed：%v", got)
	}
	if got := ids(FilterTasks(tasks, FilterCanceled, "", "")); !equalStrings(got, []string{"f"}) {
		t.Errorf("canceled：%v", got)
	}
	if got := len(FilterTasks(tasks, FilterAll, "", "")); got != 7 {
		t.Errorf("all 应有 7 条：%d", got)
	}
	if got := ids(FilterTasks(tasks, FilterDegraded, "", "")); !equalStrings(got, []string{"g"}) {
		t.Errorf("degraded 是 completed 的子集：%v", got)
	}
	if got := ids(FilterTasks(tasks, FilterCompleted, "", "")); !equalStrings(got, []string{"c", "g"}) {
		t.Errorf("completed 含降级那条：%v", got)
	}
	// 搜索同时匹配显示名与原始链接
	withLink := []services.ImportTask{mk("g", "completed", func(x *services.ImportTask) {
		x.DisplayName, x.SourceInput = "为什么 SQLite 不适合做队列", "https://www.v2ex.com/t/1223399"
	})}
	if len(FilterTasks(withLink, FilterAll, "1223399", "")) != 1 || len(FilterTasks(withLink, FilterAll, "sqlite", "")) != 1 ||
		len(FilterTasks(withLink, FilterAll, "抖音", "")) != 0 {
		t.Error("搜索应同时匹配显示名与原始链接，且不区分大小写")
	}
	want := Counts{All: 7, Active: 2, Completed: 2, Degraded: 1, Duplicate: 1, Failed: 1, Canceled: 1}
	if got := CountByFilter(tasks); got != want {
		t.Errorf("计数与筛选口径一致（降级同时计进 completed 与 degraded）：\n got %+v\nwant %+v", got, want)
	}
}

func TestOriginStatusAndErrorSearchCombine(t *testing.T) {
	tasks := []services.ImportTask{task("手机", OriginMobile), task("桌面", OriginDesktop), task("旧任务", "")}
	if got := ids(FilterTasks(tasks, FilterFailed, "网络", OriginMobile)); !equalStrings(got, []string{"手机"}) {
		t.Errorf("来源、状态和错误搜索组合：%v", got)
	}
	// 没有 origin 的旧任务按桌面端算
	if got := ids(FilterTasks(tasks, FilterAll, "", OriginDesktop)); !equalStrings(got, []string{"桌面", "旧任务"}) {
		t.Errorf("缺省来源按桌面端：%v", got)
	}
}

// ---- 来源筛选状态与异步边界（import-origin-store.test.ts） ----

func TestSelectVisibleOnlyPicksVisibleTasks(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) {
		return failedPage(task("手机", OriginMobile), task("桌面", OriginDesktop)), nil
	})
	ctx := context.Background()
	h.store.Fetch(ctx)
	h.store.SetOrigin(ctx, OriginMobile)
	h.store.SetFilter(ctx, FilterFailed)
	h.store.SetQuery("网络")
	h.store.mu.Lock()
	h.store.view.Tasks = []services.ImportTask{task("手机", OriginMobile), task("桌面", OriginDesktop)}
	h.store.refreshVisibleLocked()
	h.store.mu.Unlock()
	h.store.SelectVisible()
	if got := h.store.View().Selected; !equalStrings(got, []string{"手机"}) {
		t.Errorf("只选择当前可见任务：%v", got)
	}
}

func TestQuickOriginSwitchIsNotOverwrittenByStaleRequest(t *testing.T) {
	oldGate, freshGate := make(chan services.ImportPage, 1), make(chan services.ImportPage, 1)
	var mu sync.Mutex
	call := 0
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) {
		mu.Lock()
		call++
		n := call
		mu.Unlock()
		if n == 1 {
			return <-oldGate, nil
		}
		return <-freshGate, nil
	})
	h.svc.queue = func() (services.ImportQueueState, error) {
		return services.ImportQueueState{RunningCount: 2, PendingCount: 1, Concurrency: 2}, nil
	}
	h.store.Spawn = func(fn func()) { go fn() }
	ctx := context.Background()

	firstDone := make(chan struct{})
	go func() { h.store.Fetch(ctx); close(firstDone) }()
	waitFor(t, "第一次读取已发出", func() bool { return len(h.svc.listCalls()) == 1 })
	h.store.mu.Lock()
	h.store.view.Selected = []string{"旧任务"}
	h.store.mu.Unlock()
	h.store.SetOrigin(ctx, OriginMobile)
	waitFor(t, "切换来源后的读取已发出", func() bool { return len(h.svc.listCalls()) == 2 })

	freshGate <- failedPage(task("手机", OriginMobile))
	waitFor(t, "新来源的结果已落下", func() bool { v := h.store.View(); return len(v.Tasks) == 1 && v.Tasks[0].ID == "手机" })
	oldGate <- failedPage(task("桌面", OriginDesktop))
	<-firstDone

	v := h.store.View()
	if got := ids(v.Tasks); !equalStrings(got, []string{"手机"}) {
		t.Errorf("旧请求不能覆盖新来源：%v", got)
	}
	if len(v.Selected) != 0 {
		t.Errorf("切换来源应清空选择：%v", v.Selected)
	}
	if v.ActiveCount != 3 {
		t.Errorf("角标取运行中加排队：%d", v.ActiveCount)
	}
	if last := h.svc.listCalls(); last[len(last)-1].Origin != OriginMobile {
		t.Errorf("最后一次读取应带新来源：%+v", last[len(last)-1])
	}
}

func TestStaleLoadMoreDoesNotMixIntoNewOrigin(t *testing.T) {
	more := make(chan services.ImportPage, 1)
	var mu sync.Mutex
	call := 0
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) {
		mu.Lock()
		call++
		n := call
		mu.Unlock()
		if n == 1 {
			return <-more, nil
		}
		return failedPage(task("手机", OriginMobile)), nil
	})
	h.store.Spawn = func(fn func()) { go fn() }
	ctx := context.Background()
	h.store.mu.Lock()
	h.store.view.Tasks = []services.ImportTask{task("桌面", OriginDesktop)}
	h.store.view.Origin, h.store.view.NextCursor = OriginDesktop, "older"
	h.store.refreshVisibleLocked()
	h.store.mu.Unlock()

	loadingDone := make(chan struct{})
	go func() { h.store.LoadMore(ctx); close(loadingDone) }()
	waitFor(t, "翻页读取已发出", func() bool { return len(h.svc.listCalls()) == 1 })
	h.store.SetOrigin(ctx, OriginMobile)
	waitFor(t, "新来源的结果已落下", func() bool { v := h.store.View(); return len(v.Tasks) == 1 && v.Tasks[0].ID == "手机" })
	more <- failedPage(task("更早桌面", OriginDesktop))
	<-loadingDone

	v := h.store.View()
	if got := ids(v.Tasks); !equalStrings(got, []string{"手机"}) {
		t.Errorf("旧来源翻页返回时不能混入新来源列表：%v", got)
	}
	if v.LoadingMore {
		t.Error("翻页状态应复位")
	}
}

func TestStaleLoadMoreCannotResurrectFinishedTask(t *testing.T) {
	processing := task("快速失败", OriginDesktop)
	processing.Status, processing.Stage = services.ImportStatusProcessing, "fetching"
	failed := processing
	failed.Status, failed.Stage, failed.UpdatedAt = services.ImportStatusFailed, "", 2

	more := make(chan services.ImportPage, 1)
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return <-more, nil })
	ctx := context.Background()
	h.store.mu.Lock()
	h.store.view.Tasks, h.store.view.NextCursor = []services.ImportTask{processing}, "older"
	h.store.refreshVisibleLocked()
	h.store.mu.Unlock()
	stop := h.store.Subscribe(ctx)
	defer stop()

	done := make(chan struct{})
	go func() { h.store.LoadMore(ctx); close(done) }()
	waitFor(t, "翻页读取已发出", func() bool { return len(h.svc.listCalls()) == 1 })
	h.svc.Emit(failed)
	older := task("历史", OriginDesktop)
	page := failedPage(older)
	page.Active = []services.ImportTask{processing}
	more <- page
	<-done

	v := h.store.View()
	if v.Tasks[0].Status != services.ImportStatusFailed {
		t.Errorf("翻页旧快照不能把收到失败通知的任务复活为处理中：%+v", v.Tasks[0])
	}
	if v.LoadingMore || v.NextCursor != "older" {
		t.Errorf("翻页状态应复位且游标保留：%+v", v)
	}
}

func TestPushIsNotOverwrittenByLateReceipt(t *testing.T) {
	for _, boundary := range []string{"list", "queue"} {
		t.Run(boundary, func(t *testing.T) {
			processing := task("快速失败", OriginDesktop)
			processing.Status, processing.Stage = services.ImportStatusProcessing, "web-preparing"
			failed := processing
			failed.Status, failed.Stage, failed.UpdatedAt = services.ImportStatusFailed, "", 2
			oldPage := failedPage(processing)
			oldPage.Counts = map[string]int64{"processing": 1}
			oldPage.Active = []services.ImportTask{processing}

			gate := make(chan struct{})
			var mu sync.Mutex
			listCall, queueCall := 0, 0
			h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) {
				mu.Lock()
				listCall++
				n := listCall
				mu.Unlock()
				if n == 1 {
					if boundary == "list" {
						<-gate
					}
					return oldPage, nil
				}
				return failedPage(failed), nil
			})
			h.svc.queue = func() (services.ImportQueueState, error) {
				mu.Lock()
				queueCall++
				n := queueCall
				mu.Unlock()
				if n == 1 && boundary == "queue" {
					<-gate
				}
				return services.ImportQueueState{Concurrency: 2}, nil
			}
			ctx := context.Background()
			stop := h.store.Subscribe(ctx)
			defer stop()

			done := make(chan struct{})
			go func() { h.store.Fetch(ctx); close(done) }()
			if boundary == "queue" {
				waitFor(t, "队列状态读取已发出", func() bool { mu.Lock(); defer mu.Unlock(); return queueCall >= 1 })
			} else {
				waitFor(t, "列表读取已发出", func() bool { return len(h.svc.listCalls()) == 1 })
			}
			h.svc.Emit(failed)
			close(gate)
			<-done

			v := h.store.View()
			if v.Tasks[0].Status != services.ImportStatusFailed {
				t.Errorf("快速失败的推送不被迟到的 %s 回执覆盖：%+v", boundary, v.Tasks[0])
			}
			if v.Counts.Active != 0 || !v.HasLoaded {
				t.Errorf("计数与加载态：%+v", v)
			}
		})
	}
}

func TestOtherOriginEventsDoNotMixAndCountRefreshKeepsHistory(t *testing.T) {
	older, first := task("历史手机任务", OriginMobile), task("最新手机任务", OriginMobile)
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(first), nil })
	ctx := context.Background()
	h.store.mu.Lock()
	h.store.view.Tasks, h.store.view.Origin, h.store.view.NextCursor = []services.ImportTask{first, older}, OriginMobile, "older-page"
	h.store.refreshVisibleLocked()
	h.store.mu.Unlock()
	stop := h.store.Subscribe(ctx)
	defer stop()

	other := task("桌面新任务", OriginDesktop)
	other.Status = services.ImportStatusProcessing
	h.svc.Emit(other)
	h.clock.Advance(250 * time.Millisecond)

	if n := len(h.svc.listCalls()); n != 1 {
		t.Fatalf("推送后应合并成一次计数刷新：%d", n)
	}
	v := h.store.View()
	if got := ids(v.Tasks); !equalStrings(got, []string{first.ID, older.ID}) {
		t.Errorf("其他来源的实时事件不混入列表，刷新计数不丢失已加载历史：%v", got)
	}
	if v.NextCursor != "older-page" {
		t.Errorf("游标应保留：%q", v.NextCursor)
	}
}

func TestDensePushesCoalesceIntoOneCountsRefresh(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	ctx := context.Background()
	stop := h.store.Subscribe(ctx)
	defer stop()
	for i := 0; i < 20; i++ {
		h.svc.Emit(task("t"+string(rune('a'+i)), OriginDesktop))
	}
	h.clock.Advance(249 * time.Millisecond)
	if n := len(h.svc.listCalls()); n != 0 {
		t.Fatalf("250 ms 内不应刷新：%d", n)
	}
	h.clock.Advance(1 * time.Millisecond)
	if n := len(h.svc.listCalls()); n != 1 {
		t.Fatalf("密集推送只应触发一次刷新：%d", n)
	}
}

func TestPushPrependsNewMatchingTaskAndReplacesExisting(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	ctx := context.Background()
	h.store.mu.Lock()
	h.store.view.Tasks = []services.ImportTask{task("甲", OriginDesktop)}
	h.store.refreshVisibleLocked()
	h.store.mu.Unlock()
	stop := h.store.Subscribe(ctx)
	defer stop()

	h.svc.Emit(task("乙", OriginDesktop))
	updated := task("甲", OriginDesktop)
	updated.Error = "新的原因"
	h.svc.Emit(updated)

	v := h.store.View()
	if got := ids(v.Tasks); !equalStrings(got, []string{"乙", "甲"}) {
		t.Errorf("新任务排在最前，已有任务原位更新：%v", got)
	}
	if v.Tasks[1].Error != "新的原因" {
		t.Errorf("已有任务应被新快照替换：%+v", v.Tasks[1])
	}
}

func TestLoadErrorIsNotRenderedAsEmpty(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) {
		return services.ImportPage{}, errors.New("数据库打不开")
	})
	h.store.Fetch(context.Background())
	v := h.store.View()
	if v.LoadError != "数据库打不开" || !v.HasLoaded {
		t.Errorf("读取失败应留下原因并结束加载态：%+v", v)
	}
	// 重试成功后错误清除
	h.svc.mu.Lock()
	h.svc.list = func(services.ImportQuery) (services.ImportPage, error) {
		return failedPage(task("甲", OriginDesktop)), nil
	}
	h.svc.mu.Unlock()
	h.store.Fetch(context.Background())
	if v := h.store.View(); v.LoadError != "" || len(v.Tasks) != 1 {
		t.Errorf("重试成功后应清除错误：%+v", v)
	}
}

func TestQueueStateFailureCountsAsLoadError(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	h.svc.queue = func() (services.ImportQueueState, error) {
		return services.ImportQueueState{}, errors.New("调度器无响应")
	}
	h.store.Fetch(context.Background())
	if v := h.store.View(); v.LoadError != "调度器无响应" {
		t.Errorf("队列状态读取失败与列表读取失败同样进入错误态：%+v", v)
	}
}

func TestClearTerminalUsesPreviewScopeSnapshot(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	query := services.ImportClearQuery{Scope: "filtered", Origin: OriginMobile, Status: "failed", Query: "网络"}
	h.svc.SetClearCount(1)
	ctx := context.Background()
	if n, err := h.store.PreviewClearTerminal(ctx, query); err != nil || n != 1 {
		t.Fatalf("预览：%d %v", n, err)
	}
	h.store.SetOrigin(ctx, OriginDesktop) // 预览之后用户又换了来源筛选
	if n := h.store.ClearTerminal(ctx, query); n != 1 {
		t.Errorf("清理条数：%d", n)
	}
	if got := h.svc.Calls(); !slices.Contains(got, "clear:filtered") {
		t.Errorf("确认清理使用预览时的范围快照：%v", got)
	}
}

func TestClearTerminalFailureReportsAndReturnsZero(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	h.svc.FailOn("clear", errors.New("磁盘已满"))
	if n := h.store.ClearTerminal(context.Background(), services.ImportClearQuery{Scope: "all"}); n != 0 {
		t.Errorf("失败应返回 0：%d", n)
	}
	if got := h.log.entries(); len(got) != 1 {
		t.Errorf("失败应留下一条错误记录（提示与日志同源）：%v", got)
	}
}

// ---- 选择 ----

func TestRangeSelectAndToggle(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	all := []services.ImportTask{task("a", ""), task("b", ""), task("c", ""), task("d", "")}
	h.store.mu.Lock()
	h.store.view.Tasks = all
	h.store.refreshVisibleLocked()
	h.store.mu.Unlock()

	h.store.ToggleSelection("b")
	h.store.RangeSelectTo("d")
	if got := h.store.View().Selected; !equalStrings(got, []string{"b", "c", "d"}) {
		t.Errorf("Shift 点选从锚点到目标全选：%v", got)
	}
	h.store.ToggleSelection("c")
	if got := h.store.View().Selected; !equalStrings(got, []string{"b", "d"}) {
		t.Errorf("再点一次取消：%v", got)
	}
	h.store.ClearSelection()
	h.store.RangeSelectTo("c") // 没有锚点：只选目标
	if got := h.store.View().Selected; !equalStrings(got, []string{"c"}) {
		t.Errorf("没有锚点时只选目标：%v", got)
	}
}

// ---- 删除：先隐藏、窗口内可撤销、之后才提交（import-remove-undo.test.ts） ----

type removeFixture struct {
	*harness
	remaining []services.ImportTask // 服务端此刻返回的列表
}

func newRemoveFixture(t *testing.T) *removeFixture {
	f := &removeFixture{remaining: []services.ImportTask{task("甲", OriginDesktop), task("乙", OriginDesktop), task("丙", OriginDesktop)}}
	f.harness = newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(f.remaining...), nil })
	f.store.mu.Lock()
	f.store.view.Tasks = append([]services.ImportTask(nil), f.remaining...)
	f.store.view.Total = 3
	f.store.view.Counts = Counts{All: 3, Failed: 3}
	f.store.view.Selected = []string{"甲", "乙"}
	f.store.refreshVisibleLocked()
	f.store.mu.Unlock()
	return f
}

func TestRemoveHidesImmediatelyAndDoesNotCallBackendInWindow(t *testing.T) {
	f := newRemoveFixture(t)
	removal := f.store.ScheduleRemove(context.Background(), []string{"甲", "乙"}, 0)
	if removal == nil || removal.Count != 2 {
		t.Fatalf("应登记 2 条移除：%+v", removal)
	}
	v := f.store.View()
	if got := ids(v.Tasks); !equalStrings(got, []string{"丙"}) {
		t.Errorf("立即从列表消失：%v", got)
	}
	if len(v.Selected) != 0 || v.Total != 1 || v.Counts.Failed != 1 {
		t.Errorf("选中集合与计数应同步扣除：%+v", v)
	}
	if got := f.svc.Calls(); slices.Contains(got, "remove:甲") {
		t.Errorf("窗口内不调用后端：%v", got)
	}
	removal.Undo()
}

func TestRemoveUndoRestoresAndNeverCommits(t *testing.T) {
	f := newRemoveFixture(t)
	removal := f.store.ScheduleRemove(context.Background(), []string{"甲"}, 0)
	removal.Undo()
	f.clock.Advance(10 * time.Second)
	for _, call := range f.svc.Calls() {
		if call == "remove:甲" {
			t.Fatalf("撤销后从未提交过删除：%v", f.svc.Calls())
		}
	}
	if got := ids(f.store.View().Tasks); !equalStrings(got, []string{"甲", "乙", "丙"}) {
		t.Errorf("撤销后原样回到列表：%v", got)
	}
}

func TestRemoveCommitsAfterWindowOnlySelectedTasks(t *testing.T) {
	f := newRemoveFixture(t)
	f.store.ScheduleRemove(context.Background(), []string{"甲"}, 0)
	f.clock.Advance(8 * time.Second)
	var removes []string
	for _, call := range f.svc.Calls() {
		if len(call) > 7 && call[:7] == "remove:" {
			removes = append(removes, call)
		}
	}
	if !equalStrings(removes, []string{"remove:甲"}) {
		t.Errorf("窗口结束自动提交，只删被选中的任务：%v", removes)
	}
}

func TestRemovedRowsDoNotFlashBackOnRefreshOrPush(t *testing.T) {
	f := newRemoveFixture(t)
	ctx := context.Background()
	removal := f.store.ScheduleRemove(ctx, []string{"甲"}, 0)
	stop := f.store.Subscribe(ctx)
	defer stop()
	defer removal.Undo()

	f.store.Fetch(ctx)
	v := f.store.View()
	if got := ids(v.Tasks); !equalStrings(got, []string{"乙", "丙"}) {
		t.Errorf("窗口内刷新列表，被删的行不会闪回：%v", got)
	}
	// 服务端计数仍含「甲」，界面需要扣掉
	if v.Counts.Failed != 2 {
		t.Errorf("计数应扣掉隐藏项：%+v", v.Counts)
	}
	updated := task("甲", OriginDesktop)
	updated.UpdatedAt = 2
	f.svc.Emit(updated)
	if got := ids(f.store.View().Tasks); !equalStrings(got, []string{"乙", "丙"}) {
		t.Errorf("窗口内收到推送，被删的行也不会闪回：%v", got)
	}
}

func TestRemoveCommitAccountsPerTask(t *testing.T) {
	f := newRemoveFixture(t)
	f.svc.FailOn("remove:乙", errors.New("数据库忙"))
	f.remaining = []services.ImportTask{task("乙", OriginDesktop), task("丙", OriginDesktop)} // 提交后服务端只剩失败的那条
	removal := f.store.ScheduleRemove(context.Background(), []string{"甲", "乙"}, 0)
	removal.Commit()

	var removes int
	for _, call := range f.svc.Calls() {
		if call == "remove:甲" || call == "remove:乙" {
			removes++
		}
	}
	if removes != 2 {
		t.Errorf("两条都应尝试删除，一条失败不中断另一条：%v", f.svc.Calls())
	}
	if got := ids(f.store.View().Tasks); !equalStrings(got, []string{"乙", "丙"}) {
		t.Errorf("失败的任务回到列表，成功的不再出现：%v", got)
	}
	if got := f.log.entries(); len(got) != 1 || !strings.Contains(got[0], "乙") || !strings.Contains(got[0], "数据库忙") {
		t.Errorf("失败应汇总成一条带逐条原因的记录：%v", got)
	}
}

func TestRunningTasksCannotBeRemovedNorRegisteredTwice(t *testing.T) {
	f := newRemoveFixture(t)
	running := task("运行中", OriginDesktop)
	running.Status = services.ImportStatusProcessing
	f.store.mu.Lock()
	f.store.view.Tasks = append([]services.ImportTask{running}, f.store.view.Tasks...)
	f.store.refreshVisibleLocked()
	f.store.mu.Unlock()

	if f.store.ScheduleRemove(context.Background(), []string{"运行中"}, 0) != nil {
		t.Error("运行中的任务不能被删")
	}
	first := f.store.ScheduleRemove(context.Background(), []string{"甲"}, 0)
	if first == nil || first.Count != 1 {
		t.Fatalf("第一次应登记 1 条：%+v", first)
	}
	if f.store.ScheduleRemove(context.Background(), []string{"甲"}, 0) != nil {
		t.Error("不会重复登记同一条")
	}
	first.Undo()
}

func TestFlushRemovalsCommitsPendingWindows(t *testing.T) {
	f := newRemoveFixture(t)
	f.store.ScheduleRemove(context.Background(), []string{"甲"}, 0)
	f.store.FlushRemovals()
	if !slices.Contains(f.svc.Calls(), "remove:甲") {
		t.Errorf("窗口关闭前应提交尚在撤销窗口内的移除：%v", f.svc.Calls())
	}
	f.clock.Advance(10 * time.Second) // 定时器已取消，不会二次提交
	n := 0
	for _, call := range f.svc.Calls() {
		if call == "remove:甲" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("同一条只提交一次：%d", n)
	}
}

// ---- 批量与单项操作 ----

func TestBatchRetryReportsEachFailureAndKeepsGoing(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	h.svc.FailOn("retry:乙", errors.New("队列已满"))
	h.store.mu.Lock()
	h.store.view.Tasks = []services.ImportTask{task("甲", ""), task("乙", ""), task("丙", "")}
	h.store.view.Selected = []string{"甲", "乙", "丙"}
	h.store.mu.Unlock()

	h.store.RetryTasks(context.Background(), []string{"甲", "乙", "丙"})

	calls := h.svc.Calls()
	if !slices.Contains(calls, "retry:甲") || !slices.Contains(calls, "retry:丙") {
		t.Errorf("一条失败不应中断后面的：%v", calls)
	}
	if got := h.log.entries(); len(got) != 1 || !strings.Contains(got[0], "乙") || !strings.Contains(got[0], "队列已满") {
		t.Errorf("逐条原因汇总成一条记录：%v", got)
	}
	if len(h.store.View().Selected) != 0 {
		t.Error("批量操作结束后清空选择")
	}
}

func TestRetryReportsMissingTask(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	h.svc.SetMissing("甲")
	if h.store.RetryTask(context.Background(), "甲", services.ImportRetryOptions{}) {
		t.Error("任务已不存在时应返回 false")
	}
	if got := h.log.entries(); len(got) != 1 || !strings.Contains(got[0], "任务已不存在") {
		t.Errorf("应记录「任务已不存在」的失败：%v", got)
	}
	if !h.store.RetryTask(context.Background(), "乙", services.ImportRetryOptions{ForceDuplicate: true}) {
		t.Error("存在的任务应重试成功")
	}
	if !h.svc.RetryOptions().ForceDuplicate {
		t.Error("重试参数应原样传给后端")
	}
}

func TestTogglePausedFlipsQueueState(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	ctx := context.Background()
	h.store.TogglePaused(ctx)
	if !h.store.View().Queue.Paused {
		t.Error("第一次应暂停")
	}
	h.store.TogglePaused(ctx)
	if h.store.View().Queue.Paused {
		t.Error("第二次应继续")
	}
	if got := h.svc.Calls(); !slices.Contains(got, "pause") || !slices.Contains(got, "resume") {
		t.Errorf("应分别调用 pause 与 resume：%v", got)
	}
}

func TestEnqueueRefreshesAndPropagatesError(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(task("新", "")), nil })
	ctx := context.Background()
	if _, err := h.store.Enqueue(ctx, []services.EnqueueImportInput{{Kind: "file", Input: `D:\a.md`}}); err != nil {
		t.Fatal(err)
	}
	if got := ids(h.store.View().Tasks); !equalStrings(got, []string{"新"}) {
		t.Errorf("入队后应刷新列表：%v", got)
	}
	h.svc.FailOn("enqueue", errors.New("队列已关闭"))
	if _, err := h.store.Enqueue(ctx, nil); err == nil {
		t.Error("入队失败应把错误交还调用方")
	}
}

func TestCompletedPushTriggersHooks(t *testing.T) {
	h := newHarness(t, func(services.ImportQuery) (services.ImportPage, error) { return failedPage(), nil })
	var completed, inbox int
	h.store.OnCompleted = func(services.ImportTask) { completed++ }
	h.store.RefreshInbox = func() { inbox++ }
	stop := h.store.Subscribe(context.Background())
	defer stop()

	done := task("完成", "")
	done.Status = services.ImportStatusCompleted
	h.svc.Emit(done)
	running := task("进行", "")
	running.Status = services.ImportStatusProcessing
	h.svc.Emit(running)
	failed := task("失败", "")
	h.svc.Emit(failed)
	if completed != 1 || inbox != 2 {
		t.Errorf("完成触发两个钩子，失败只刷新处理中心，进行中不触发：completed=%d inbox=%d", completed, inbox)
	}
}
