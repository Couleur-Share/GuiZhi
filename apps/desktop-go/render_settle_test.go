package main

import (
	"context"
	"testing"
	"time"
)

// fakeSettleEnv：用假时钟驱动沉降等待，pause 只推进时钟。
type fakeSettleEnv struct {
	clock   time.Time
	text    func(at time.Duration) int     // 某时刻的可见文字长度
	domAge  func(at time.Duration) float64 // 某时刻距最近 DOM 变化的毫秒数
	netAge  func(at time.Duration) time.Duration
	started time.Time
	scrolls int
	blind   bool
}

func newFakeSettleEnv() *fakeSettleEnv {
	t0 := time.Unix(0, 0)
	return &fakeSettleEnv{clock: t0, started: t0,
		text:   func(time.Duration) int { return 500 },
		domAge: func(time.Duration) float64 { return 1000 },
		netAge: func(time.Duration) time.Duration { return time.Second }}
}
func (e *fakeSettleEnv) at() time.Duration { return e.clock.Sub(e.started) }
func (e *fakeSettleEnv) now() time.Time    { return e.clock }
func (e *fakeSettleEnv) pause(ctx context.Context, d time.Duration) error {
	e.clock = e.clock.Add(d)
	return ctx.Err()
}
func (e *fakeSettleEnv) scroll(context.Context) error          { e.scrolls++; return nil }
func (e *fakeSettleEnv) networkQuiet() time.Duration           { return e.netAge(e.at()) }
func (e *fakeSettleEnv) install(context.Context) (bool, error) { return !e.blind, nil }
func (e *fakeSettleEnv) sample(context.Context) (*settleSample, error) {
	return &settleSample{DomQuietMs: e.domAge(e.at()), TextLength: e.text(e.at())}, nil
}

func TestSettleQuietShellCapAndBlind(t *testing.T) {
	ctx := context.Background()
	// 静止的正常页：文字稳定 300 ms 后提前结束
	e := newFakeSettleEnv()
	r, err := settleRenderedPage(ctx, e, false)
	if err != nil || r.Reason != "quiet" || r.InitialMs >= 1200 || e.scrolls != 3 {
		t.Errorf("静止页: %+v %v scrolls=%d", r, err, e.scrolls)
	}
	// 空壳页（文字不足 200）：等满首段
	e = newFakeSettleEnv()
	e.text = func(time.Duration) int { return 10 }
	if r, _ = settleRenderedPage(ctx, e, false); r.Reason != "shell" || r.InitialMs < 1200 {
		t.Errorf("空壳页: %+v", r)
	}
	// 一直有 DOM 变化：到上限
	e = newFakeSettleEnv()
	e.domAge = func(time.Duration) float64 { return 50 }
	if r, _ = settleRenderedPage(ctx, e, false); r.Reason != "cap" {
		t.Errorf("持续变化: %+v", r)
	}
	// 有请求在途：到上限
	e = newFakeSettleEnv()
	e.netAge = func(time.Duration) time.Duration { return 0 }
	if r, _ = settleRenderedPage(ctx, e, false); r.Reason != "cap" {
		t.Errorf("请求在途: %+v", r)
	}
	// 无法观察 / 固定模式：退回固定等待
	e = newFakeSettleEnv()
	e.blind = true
	if r, _ = settleRenderedPage(ctx, e, false); r.Reason != "blind" || e.at() < settleInitialCap+3*settleStepCap {
		t.Errorf("盲等: %+v at=%s", r, e.at())
	}
	e = newFakeSettleEnv()
	if r, _ = settleRenderedPage(ctx, e, true); r.Reason != "fixed" || e.at() != settleInitialCap+3*settleStepCap {
		t.Errorf("固定: %+v at=%s", r, e.at())
	}
	// 取消向上传播
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := settleRenderedPage(cctx, newFakeSettleEnv(), false); err == nil {
		t.Errorf("取消应报错")
	}
}
