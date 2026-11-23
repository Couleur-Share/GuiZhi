package themed

// 主题页任务列表（ThemedReadingTasks.tsx）：处理中心里按需生成任务的清单，实时跟进进度，可停止 / 继续 / 打开。

import (
	"context"
	"sort"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// TaskFilter 是列表的筛选（渲染层没有，这里只提供逻辑，界面可选用）。
type TaskFilter int

const (
	FilterAll        TaskFilter = iota
	FilterActive                // 排队或执行中
	FilterUnfinished            // 可继续的未完成任务（失败、取消、中断、部分完成）
)

// Match 报告任务是否通过筛选。
func (f TaskFilter) Match(t services.ThemedTask) bool {
	switch f {
	case FilterActive:
		return t.Active()
	case FilterUnfinished:
		return t.Resumable()
	}
	return true
}

// FilterTasks 返回通过筛选的任务（保持顺序，不改原切片）。
func FilterTasks(tasks []services.ThemedTask, f TaskFilter) []services.ThemedTask {
	out := make([]services.ThemedTask, 0, len(tasks))
	for _, t := range tasks {
		if f.Match(t) {
			out = append(out, t)
		}
	}
	return out
}

// TaskListView 是任务列表的快照；Tasks 与 Busy 都是整体替换的，拿到后不会被改动。
type TaskListView struct {
	// Tasks 按最近更新倒序。
	Tasks []services.ThemedTask
	// Loading 为首次读取尚未完成；Error 非空时必须渲染成可重试的错误态（再次 Reload），不能当作空态。
	Loading bool
	Error   string
	// Busy 是正在执行停止 / 继续的任务标识。
	Busy map[string]bool
}

// IsBusy 报告任务是否有操作在进行（对应的按钮禁用）。
func (v TaskListView) IsBusy(id string) bool { return v.Busy[id] }

// Empty 报告是否应显示「还没有任务」：已读完、没出错、没有任务。
func (v TaskListView) Empty() bool { return !v.Loading && v.Error == "" && len(v.Tasks) == 0 }

// TaskList 管理任务列表。方法可从任意 goroutine 调用。
type TaskList struct {
	svc        services.ThemedReading
	guard      kit.Guard
	invalidate func()

	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context
	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行。
	Spawn func(fn func())
	// OnOpen 在用户点「查看主题页」时调用，界面据此跳到对应文章的 AI 阅读（记住标签、切到知识库）。可空。
	OnOpen func(services.ThemedTask)

	mu     sync.Mutex
	view   TaskListView
	unsub  func()
	loadID int
	// seen 收集读取期间推送到达的任务：推送可能比读取结果更新，读完后要并回去。
	seen map[string]services.ThemedTask
}

// NewTaskList 创建任务列表（首次读取前处于 Loading）。
func NewTaskList(svc services.ThemedReading, guard kit.Guard, invalidate func()) *TaskList {
	return &TaskList{svc: svc, guard: guard, invalidate: invalidate, view: TaskListView{Loading: true},
		Spawn: func(fn func()) { go fn() }}
}

// View 返回当前快照。
func (l *TaskList) View() TaskListView {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.view
}

func (l *TaskList) wake() {
	if l.invalidate != nil {
		l.invalidate()
	}
}

func (l *TaskList) ctx() context.Context {
	if l.Ctx != nil {
		return l.Ctx
	}
	return context.Background()
}

// Attach 订阅进度并后台读取一次；重复调用只会再读取，不会重复订阅。
func (l *TaskList) Attach() {
	l.mu.Lock()
	subscribe := l.unsub == nil
	if subscribe {
		l.unsub = func() {} // 占位：订阅期间推送可能立刻到达
	}
	l.mu.Unlock()
	if subscribe {
		cancel := l.svc.OnProgress(l.onProgress)
		l.mu.Lock()
		l.unsub = cancel
		l.mu.Unlock()
	}
	spawn := l.Spawn
	if spawn == nil {
		spawn = func(fn func()) { go fn() }
	}
	spawn(l.Reload)
}

// Detach 退订并作废在途的读取。
func (l *TaskList) Detach() {
	l.mu.Lock()
	cancel := l.unsub
	l.unsub = nil
	l.loadID++
	l.seen = nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// merge 把一个任务并入列表（同标识替换，否则插入），并按最近更新倒序排好。返回新切片，不改旧的。
func merge(tasks []services.ThemedTask, t services.ThemedTask) []services.ThemedTask {
	out := make([]services.ThemedTask, 0, len(tasks)+1)
	out = append(out, t)
	for _, e := range tasks {
		if e.ID != t.ID {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

func (l *TaskList) onProgress(t services.ThemedTask) {
	if t.ID == "" {
		return
	}
	l.mu.Lock()
	if l.unsub == nil {
		l.mu.Unlock()
		return // 已退订
	}
	l.view.Tasks = merge(l.view.Tasks, t)
	if l.seen != nil {
		l.seen[t.ID] = t
	}
	l.mu.Unlock()
	l.wake()
}

// Reload 重读任务列表。失败设置 Error（带重试：再次 Reload），已有的列表保留但界面显示错误态。
func (l *TaskList) Reload() {
	l.mu.Lock()
	l.loadID++
	id := l.loadID
	l.seen = map[string]services.ThemedTask{}
	l.view.Error = ""
	l.mu.Unlock()
	l.wake()

	tasks, err := l.svc.ListTasks(l.ctx())

	l.mu.Lock()
	if id != l.loadID {
		l.mu.Unlock()
		return
	}
	seen := l.seen
	l.seen = nil
	l.view.Loading = false
	if err != nil {
		l.view.Error = err.Error()
		l.mu.Unlock()
		l.guard.Background("themedReading.listTasks", err)
		l.wake()
		return
	}
	list := append([]services.ThemedTask(nil), tasks...)
	for _, t := range seen {
		// 读取期间推送来的任务若不比读到的旧，就以推送为准（它可能是读取之后才发生的变化）。
		for i, e := range list {
			if e.ID == t.ID && t.UpdatedAt >= e.UpdatedAt {
				list[i] = t
			}
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt > list[j].UpdatedAt })
	l.view.Tasks = list
	l.mu.Unlock()
	l.wake()
}

func (l *TaskList) setBusy(id string, on bool) {
	l.mu.Lock()
	next := make(map[string]bool, len(l.view.Busy)+1)
	for k := range l.view.Busy {
		next[k] = true
	}
	if on {
		next[id] = true
	} else {
		delete(next, id)
	}
	l.view.Busy = next
	l.mu.Unlock()
	l.wake()
}

// action 对一个任务执行停止 / 继续：同一任务同一时间只允许一个操作；成功后并回返回的任务并重读列表。
func (l *TaskList) action(id, action string, call func(context.Context, string) (services.ThemedTask, error)) bool {
	l.mu.Lock()
	if id == "" || l.view.Busy[id] {
		l.mu.Unlock()
		return false
	}
	l.mu.Unlock()
	l.setBusy(id, true)
	defer l.setBusy(id, false)

	task, err := call(l.ctx(), id)
	if err != nil {
		l.guard.Fail(locales.ThemedReadingTaskFailed(), action, err)
		return false
	}
	// 继续「已发布页面里失败的素材」会返回新任务；先并入，再以重读结果为准。
	l.mu.Lock()
	if task.ID != "" {
		l.view.Tasks = merge(l.view.Tasks, task)
	}
	l.mu.Unlock()
	l.wake()
	l.Reload()
	return true
}

// Cancel 停止一个任务。
func (l *TaskList) Cancel(id string) bool { return l.action(id, "themedReading.cancel", l.svc.Cancel) }

// Resume 继续一个任务（旧格式任务不能继续：不发请求）。
func (l *TaskList) Resume(id string) bool {
	l.mu.Lock()
	var t *services.ThemedTask
	for i := range l.view.Tasks {
		if l.view.Tasks[i].ID == id {
			t = &l.view.Tasks[i]
		}
	}
	allowed := t != nil && t.Resumable()
	l.mu.Unlock()
	if !allowed {
		return false
	}
	return l.action(id, "themedReading.resume", l.svc.Resume)
}

// Open 通知界面打开某个任务对应的主题页。
func (l *TaskList) Open(id string) bool {
	l.mu.Lock()
	var found *services.ThemedTask
	for i := range l.view.Tasks {
		if l.view.Tasks[i].ID == id {
			t := l.view.Tasks[i]
			found = &t
		}
	}
	l.mu.Unlock()
	if found == nil || l.OnOpen == nil {
		return false
	}
	l.OnOpen(*found)
	return true
}
