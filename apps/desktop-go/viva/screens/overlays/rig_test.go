package overlays

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// timers 收集 After 排程的回调，由测试手动触发。
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

func runNow(fn func()) { fn() }

// rig 提供各 Store 共用的提示通道、日志、计时器与时钟。Spawn 默认同步，保持确定性。
type rig struct {
	t      *testing.T
	notify *kit.Notifier
	log    *recLog
	guard  kit.Guard
	wakes  atomic.Int32
	timers *timers
	clock  atomic.Int64 // 毫秒
	wg     sync.WaitGroup
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, notify: kit.NewNotifier(nil), log: &recLog{}, timers: &timers{}}
	r.guard = kit.Guard{Notify: r.notify, Log: r.log}
	r.clock.Store(1_700_000_000_000)
	return r
}

func (r *rig) invalidate() { r.wakes.Add(1) }

// env 是同步执行的运行环境。
func (r *rig) env() Env {
	return Env{Spawn: runNow, After: r.timers.after, Now: func() time.Time { return time.UnixMilli(r.clock.Load()) }}
}

// asyncEnv 是真正开协程的运行环境（卡住的假服务调用不会堵死测试线程）；用 r.wait 等它们结束。
func (r *rig) asyncEnv() Env {
	e := r.env()
	e.Spawn = func(fn func()) {
		r.wg.Add(1)
		go func() { defer r.wg.Done(); fn() }()
	}
	return e
}

func (r *rig) wait() { r.t.Helper(); r.wg.Wait() }

func (r *rig) advance(d time.Duration) { r.clock.Add(d.Milliseconds()) }

func (r *rig) toasts() []kit.QueuedToast { return r.notify.Queued() }

func (r *rig) lastToast() kit.QueuedToast {
	r.t.Helper()
	all := r.toasts()
	if len(all) == 0 {
		r.t.Fatal("期望有一条提示，实际没有")
	}
	return all[len(all)-1]
}
