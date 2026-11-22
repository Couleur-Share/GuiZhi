package settings

import (
	"context"
	"errors"
	"sync"

	"guizhi-mygo-shell/viva/kit"
)

// errCaptureUnavailable 是外壳没有接上采集工具服务时的失败原因（进「查看详情」与日志，不进界面文案）。
var errCaptureUnavailable = errors.New("the capture tools service is not available in this build")

// EngineCache 记住各引擎上次探测的结果（渲染层 use-engine-status 的模块级缓存）：回到「采集与转写」页时先展示旧值、
// 再在后台静默刷新（陈旧优先），不必每次都从「检测中」转圈起步。键是 `引擎:自定义路径`——换了探测目标就不该沿用上一个目标的结果。
// 由外壳持有，生命周期同设置页；零值可用，并发安全。
type EngineCache struct {
	mu sync.Mutex
	m  map[string]any
}

func (e *EngineCache) get(key string) (any, bool) {
	if e == nil {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.m[key]
	return v, ok
}

func (e *EngineCache) set(key string, v any) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.m == nil {
		e.m = map[string]any{}
	}
	e.m[key] = v
}

// probe 是一个引擎的状态探测（渲染层 useEngineStatus）：已知状态（可能来自缓存）、是否在刷新、失败原因。
type probe[T any] struct {
	key        string
	status     *T // nil：从未探测成功过
	refreshing bool
	err        string
	started    bool

	cache  *EngineCache
	w      *worker
	wake   func()
	guard  kit.Guard
	action string
	fetch  func(ctx context.Context, force bool) (T, error)
}

// ensure 在每帧开头调用：首次或探测目标（key）变了就切到缓存值并后台刷新一次。
func (p *probe[T]) ensure(key string) {
	if p.started && p.key == key {
		return
	}
	p.started, p.key = true, key
	p.status, p.err = nil, ""
	if v, ok := p.cache.get(key); ok {
		if s, ok := v.(T); ok {
			p.status = &s
		}
	}
	p.refresh(false)
}

// refresh 在后台重新探测；force 为真时绕过外壳那一层带 TTL 的探测缓存。失败只进日志、在行上显示「检测失败」，不弹提示（用户没有做操作）。
func (p *probe[T]) refresh(force bool) {
	p.refreshing = true
	key, fetch, cache, g, action := p.key, p.fetch, p.cache, p.guard, p.action
	p.w.load(p.wake, func(ctx context.Context) func() {
		var (
			v   T
			err = errCaptureUnavailable
		)
		if fetch != nil {
			v, err = fetch(ctx, force)
		}
		if err == nil {
			cache.set(key, v)
		} else if ctx.Err() == nil {
			g.Background(action, err)
		}
		return func() {
			if p.key != key {
				return // 探测期间换了目标：旧目标的结果作废
			}
			p.refreshing = false
			if err != nil {
				p.err = err.Error()
				return
			}
			p.status, p.err = &v, ""
		}
	})
}
