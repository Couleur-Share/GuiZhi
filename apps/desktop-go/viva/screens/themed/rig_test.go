package themed

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

// recLog 记录写进日志的失败。
type recLog struct {
	mu   sync.Mutex
	logs []string
}

func (r *recLog) AppError(_, action, message string) {
	r.mu.Lock()
	r.logs = append(r.logs, action+"|"+message)
	r.mu.Unlock()
}

func (r *recLog) entries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.logs)
}

// timers 收集 After 排程的回调，由测试手动触发（预览节流要的确定性）。
type timers struct {
	mu     sync.Mutex
	delays []time.Duration
	fns    []func()
}

func (t *timers) after(d time.Duration, fn func()) {
	t.mu.Lock()
	t.delays = append(t.delays, d)
	t.fns = append(t.fns, fn)
	t.mu.Unlock()
}

func (t *timers) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.fns)
}

func (t *timers) delay(i int) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.delays[i]
}

func (t *timers) fire(i int) {
	t.mu.Lock()
	fn := t.fns[i]
	t.mu.Unlock()
	fn()
}

// rig 把假服务、提示通道与日志接在各个 Store 上。Spawn 同步，保持确定性。
type rig struct {
	t      *testing.T
	svc    *servicestest.ThemedFake
	notify *kit.Notifier
	log    *recLog
	guard  kit.Guard
	wakes  atomic.Int32
	timers *timers
	clock  atomic.Int64 // 毫秒

	reading *ReadingStore
}

func runNow(fn func()) { fn() }

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, svc: &servicestest.ThemedFake{}, notify: kit.NewNotifier(nil), log: &recLog{}, timers: &timers{}}
	r.guard = kit.Guard{Notify: r.notify, Log: r.log}
	r.reading = NewReadingStore(r.svc, r.guard, func() { r.wakes.Add(1) })
	r.reading.Spawn = runNow
	r.reading.After = r.timers.after
	r.reading.Now = func() time.Time { return time.UnixMilli(r.clock.Load()) }
	r.reading.NewInstanceID = func() string { return "inst" }
	t.Cleanup(r.reading.Detach)
	return r
}

func (r *rig) view() ReadingView { return r.reading.View() }

func (r *rig) toasts() []kit.QueuedToast { return r.notify.Queued() }

func (r *rig) lastToast() kit.QueuedToast {
	r.t.Helper()
	all := r.toasts()
	if len(all) == 0 {
		r.t.Fatal("期望有一条提示，实际没有")
	}
	return all[len(all)-1]
}

var (
	reqA = services.ThemedRequest{ItemID: "item-a", SourceKind: services.ThemedKindBody}
	reqB = services.ThemedRequest{ItemID: "item-b", SourceKind: services.ThemedKindBody}
)

// open 选中内容并等读取完成（Spawn 同步，Open 返回时读取已结束）。
func (r *rig) open(req services.ThemedRequest) {
	r.t.Helper()
	r.reading.Open(req.ItemID, req.SourceKind, "key-"+req.ItemID, true)
}

func ptr[T any](v T) *T { return &v }
