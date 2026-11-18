package servicestest

import (
	"slices"
	"sync"
	"time"
)

// ManualClock 是手动推进的计时器：Advance 同步触发到期的回调。
// 给「撤销窗口」「去抖保存」这类靠定时器收尾的状态机做确定性测试：Store 的 After 字段注入它的 After 方法。
type ManualClock struct {
	mu     sync.Mutex
	now    time.Duration
	next   int
	timers map[int]*manualTimer
}

type manualTimer struct {
	at time.Duration
	fn func()
}

// NewManualClock 返回一个时间停在零点、没有计时器的时钟。
func NewManualClock() *ManualClock { return &ManualClock{timers: map[int]*manualTimer{}} }

// After 注册一个 d 之后触发的回调；返回的函数取消它（已触发或已取消时什么都不做）。
func (c *ManualClock) After(d time.Duration, fn func()) (stop func()) {
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

// Advance 把时间拨快 d，按注册顺序同步触发这期间到期的回调（回调在调用方的 goroutine 里执行）。
func (c *ManualClock) Advance(d time.Duration) {
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

// Pending 返回尚未触发也没被取消的计时器数。
func (c *ManualClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}
