// Package servicestest 提供 native/services 各接口的假实现，供界面测试使用：
// 记录调用、可注入失败、可让响应卡住，并能手动触发变化事件。
//
// Package servicestest holds fakes of the native/services interfaces for UI tests.
package servicestest

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// Imports 是 services.Imports 的假实现。零值可用。
type Imports struct {
	mu       sync.Mutex
	page     services.ImportPage
	err      error
	gate     chan struct{}
	queries  []services.ImportQuery
	onChange func()
	onTask   func(services.ImportTask)

	// 变更类能力的状态（见 SetQueueState、FailOn 等）。
	state      services.ImportQueueState
	failures   map[string]error
	calls      []string
	retryOpts  services.ImportRetryOptions
	enqueued   []services.EnqueueImportInput
	missing    map[string]bool
	clearCount int
	files      []string

	// 条目事实（ItemFacts）是只读查询，不进 calls，免得干扰对变更类调用的断言。
	itemFacts    map[string]services.ImportItemFacts
	itemFactsErr error
	factsLoaded  []string
}

var _ services.Imports = (*Imports)(nil)

// Set 设置之后 List 返回的结果。
func (f *Imports) Set(page services.ImportPage, err error) {
	f.mu.Lock()
	f.page, f.err = page, err
	f.mu.Unlock()
}

// Hold 让之后的 List 卡住，直到返回的函数被调用；用来构造「旧响应晚于新响应」的竞态。
func (f *Imports) Hold() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.gate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// Unhold 取消卡住设置（已卡住的调用不受影响，仍等它的 release）。
func (f *Imports) Unhold() {
	f.mu.Lock()
	f.gate = nil
	f.mu.Unlock()
}

// Queries 返回迄今收到的全部查询。
func (f *Imports) Queries() []services.ImportQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.ImportQuery(nil), f.queries...)
}

// Fire 触发一次变化事件；返回是否有订阅者。
func (f *Imports) Fire() bool {
	f.mu.Lock()
	fn := f.onChange
	f.mu.Unlock()
	if fn == nil {
		return false
	}
	fn()
	return true
}

// Subscribed 报告当前是否有订阅者。
func (f *Imports) Subscribed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.onChange != nil
}

func (f *Imports) List(_ context.Context, q services.ImportQuery) (services.ImportPage, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	page, err, gate := f.page, f.err, f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return page, err
}

func (f *Imports) OnChange(fn func()) func() {
	f.mu.Lock()
	f.onChange = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.onChange = nil
		f.mu.Unlock()
	}
}

// OnTaskChange 登记任务变化回调；Emit 触发它。
func (f *Imports) OnTaskChange(fn func(services.ImportTask)) func() {
	f.mu.Lock()
	f.onTask = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.onTask = nil
		f.mu.Unlock()
	}
}

// Emit 模拟后端推送一次「任务变了」：同时触发 OnTaskChange 与 OnChange 的回调。
func (f *Imports) Emit(task services.ImportTask) {
	f.mu.Lock()
	onTask, onChange := f.onTask, f.onChange
	f.mu.Unlock()
	if onTask != nil {
		onTask(task)
	}
	if onChange != nil {
		onChange()
	}
}

// ---- 变更类能力的假实现：记录调用、可注入失败 ----

// SetQueueState 设置 QueueState 与暂停/继续之后返回的调度器状态。
func (f *Imports) SetQueueState(s services.ImportQueueState) {
	f.mu.Lock()
	f.state = s
	f.mu.Unlock()
}

// FailOn 让指定操作失败：op 是 state、pause、cancel、retry、remove、preview、clear、enqueue、select 之一，
// 也可以写成 op:id（只让针对该任务的调用失败，例如 "cancel:t2"）。err 为 nil 取消注入。
func (f *Imports) FailOn(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures == nil {
		f.failures = map[string]error{}
	}
	if err == nil {
		delete(f.failures, op)
		return
	}
	f.failures[op] = err
}

// SetClearCount 设置清理预览与实际清理返回的条数。
func (f *Imports) SetClearCount(n int) {
	f.mu.Lock()
	f.clearCount = n
	f.mu.Unlock()
}

// SetFiles 设置文件选择框返回的路径。
func (f *Imports) SetFiles(paths []string) {
	f.mu.Lock()
	f.files = paths
	f.mu.Unlock()
}

// SetMissing 让针对这些任务的重试返回「任务已不存在」。
func (f *Imports) SetMissing(ids ...string) {
	f.mu.Lock()
	f.missing = map[string]bool{}
	for _, id := range ids {
		f.missing[id] = true
	}
	f.mu.Unlock()
}

// Calls 返回变更类调用的记录，每项形如 "cancel:t1"、"retry:t1"、"pause"、"clear:all"。
func (f *Imports) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// RetryOptions 返回最近一次重试收到的参数。
func (f *Imports) RetryOptions() services.ImportRetryOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.retryOpts
}

// Enqueued 返回迄今入队的全部输入。
func (f *Imports) Enqueued() []services.EnqueueImportInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.EnqueueImportInput(nil), f.enqueued...)
}

// record 记下一次调用并返回应当注入的失败（先查带 id 的键，再查整个操作）。
func (f *Imports) record(op, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := op
	if id != "" {
		entry = op + ":" + id
	}
	f.calls = append(f.calls, entry)
	if err := f.failures[entry]; err != nil {
		return err
	}
	return f.failures[op]
}

func (f *Imports) QueueState(context.Context) (services.ImportQueueState, error) {
	if err := f.record("state", ""); err != nil {
		return services.ImportQueueState{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *Imports) SetPaused(_ context.Context, paused bool) (services.ImportQueueState, error) {
	op := "resume"
	if paused {
		op = "pause"
	}
	if err := f.record(op, ""); err != nil {
		return services.ImportQueueState{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state.Paused = paused
	return f.state, nil
}

func (f *Imports) Cancel(_ context.Context, id string) error { return f.record("cancel", id) }

func (f *Imports) Retry(_ context.Context, id string, opts services.ImportRetryOptions) (bool, error) {
	if err := f.record("retry", id); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryOpts = opts
	return !f.missing[id], nil
}

func (f *Imports) Remove(_ context.Context, id string) error { return f.record("remove", id) }

func (f *Imports) PreviewClearTerminal(_ context.Context, q services.ImportClearQuery) (int, error) {
	if err := f.record("preview", q.Scope); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clearCount, nil
}

func (f *Imports) ClearTerminal(_ context.Context, q services.ImportClearQuery) (int, error) {
	if err := f.record("clear", q.Scope); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clearCount, nil
}

func (f *Imports) Enqueue(_ context.Context, inputs []services.EnqueueImportInput) ([]services.ImportTask, error) {
	if err := f.record("enqueue", ""); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueued = append(f.enqueued, inputs...)
	return nil, nil
}

func (f *Imports) SelectFiles(context.Context) ([]string, error) {
	if err := f.record("select", ""); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.files...), nil
}

// SetItemFacts 设置 ItemFacts 能读到的条目（整体替换）；没登记的条目算「不存在」。
func (f *Imports) SetItemFacts(items ...services.ImportItemFacts) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.itemFacts = make(map[string]services.ImportItemFacts, len(items))
	for _, item := range items {
		f.itemFacts[item.ID] = item
	}
}

// SetItemFactsError 让之后的 ItemFacts 失败；err 为 nil 取消注入。
func (f *Imports) SetItemFactsError(err error) {
	f.mu.Lock()
	f.itemFactsErr = err
	f.mu.Unlock()
}

// ItemFactsLoaded 返回 ItemFacts 收到的条目 id，按调用顺序。
func (f *Imports) ItemFactsLoaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.factsLoaded...)
}

func (f *Imports) ItemFacts(_ context.Context, id string) (services.ImportItemFacts, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.factsLoaded = append(f.factsLoaded, id)
	if f.itemFactsErr != nil {
		return services.ImportItemFacts{}, false, f.itemFactsErr
	}
	facts, ok := f.itemFacts[id]
	return facts, ok, nil
}
