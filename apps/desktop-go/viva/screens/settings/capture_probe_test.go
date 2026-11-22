package settings

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/viva/kit"
)

type probeLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *probeLog) AppError(scope, action, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, action+": "+message)
}

func (l *probeLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.msgs)
}

// settle 等后台探测跑完并把结果应用到 probe（模拟界面帧里的 drain）。
func settle[T any](t *testing.T, w *worker, p *probe[T]) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for p.refreshing {
		w.drain()
		if time.Now().After(deadline) {
			t.Fatal("等待探测完成超时")
		}
		time.Sleep(time.Millisecond)
	}
}

func newTestProbe(cache *EngineCache, log *probeLog, fetch func(ctx context.Context, force bool) (string, error)) (*probe[string], *worker) {
	w := &worker{}
	return &probe[string]{cache: cache, w: w, wake: func() {}, guard: kit.Guard{Log: log}, action: "detect", fetch: fetch}, w
}

func TestProbeStaleWhileRevalidate(t *testing.T) {
	cache := &EngineCache{}
	cache.set("ytdlp:", "old")
	log := &probeLog{}
	release := make(chan struct{})
	p, w := newTestProbe(cache, log, func(ctx context.Context, force bool) (string, error) {
		<-release
		return "new", nil
	})
	defer w.close()

	p.ensure("ytdlp:")
	// 刷新期间先给缓存里的旧值，不回到「检测中」。
	if p.status == nil || *p.status != "old" || !p.refreshing {
		t.Fatalf("应先展示旧值并在后台刷新：status=%v refreshing=%v", p.status, p.refreshing)
	}
	close(release)
	settle(t, w, p)
	if *p.status != "new" {
		t.Errorf("刷新完成后应换成新值，得到 %q", *p.status)
	}
	if v, _ := cache.get("ytdlp:"); v != "new" {
		t.Errorf("缓存应更新为新值，得到 %v", v)
	}
	// 同一个键重复 ensure 不再探测。
	calls := 0
	p.fetch = func(context.Context, bool) (string, error) { calls++; return "x", nil }
	p.ensure("ytdlp:")
	if p.refreshing || calls != 0 {
		t.Error("同一个键不应重复探测")
	}
}

func TestProbeFailureKeepsStaleValueAndOnlyLogs(t *testing.T) {
	cache := &EngineCache{}
	cache.set("ffmpeg:", "old")
	log := &probeLog{}
	p, w := newTestProbe(cache, log, func(context.Context, bool) (string, error) {
		return "", errors.New("spawn failed")
	})
	defer w.close()

	p.ensure("ffmpeg:")
	settle(t, w, p)
	if p.err != "spawn failed" || p.status == nil || *p.status != "old" {
		t.Errorf("失败应保留旧值并记录原因：err=%q status=%v", p.err, p.status)
	}
	if log.count() != 1 {
		t.Errorf("失败应留痕一次（不弹提示），得到 %d 条", log.count())
	}
	if v, _ := cache.get("ffmpeg:"); v != "old" {
		t.Error("失败不应覆盖缓存")
	}
	// 重试成功：错误清除。
	p.fetch = func(context.Context, bool) (string, error) { return "fresh", nil }
	p.refresh(true)
	settle(t, w, p)
	if p.err != "" || *p.status != "fresh" {
		t.Errorf("重试成功应清除错误：err=%q status=%v", p.err, p.status)
	}
}

func TestProbeKeyChangeDiscardsStaleResult(t *testing.T) {
	cache := &EngineCache{}
	log := &probeLog{}
	release := make(chan struct{})
	p, w := newTestProbe(cache, log, func(ctx context.Context, force bool) (string, error) {
		<-release
		return "result-for-first", nil
	})
	defer w.close()

	p.ensure("ytdlp:A")
	// 探测期间换了自定义路径：旧路径的结果不能当成新路径的状态。
	p.fetch = func(context.Context, bool) (string, error) { return "result-for-second", nil }
	p.ensure("ytdlp:B")
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for p.status == nil && time.Now().Before(deadline) {
		w.drain()
		time.Sleep(time.Millisecond)
	}
	w.drain()
	if p.status == nil || *p.status != "result-for-second" {
		t.Errorf("应只采用新键的结果，得到 %v", p.status)
	}
}

func TestEngineCacheNilAndZeroValue(t *testing.T) {
	var nilCache *EngineCache
	nilCache.set("k", "v") // 不 panic
	if _, ok := nilCache.get("k"); ok {
		t.Error("nil 缓存不应有值")
	}
	var c EngineCache
	if _, ok := c.get("k"); ok {
		t.Error("零值缓存应为空")
	}
	c.set("k", 1)
	if v, ok := c.get("k"); !ok || v != 1 {
		t.Errorf("零值缓存应可用，得到 %v %v", v, ok)
	}
}
