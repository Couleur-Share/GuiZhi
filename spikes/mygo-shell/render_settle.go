package main

// 对应 services/web-capture/web-settle.ts 的沉降等待（逻辑一一移植，常量与判定一致）：
// 页面加载后观察网络、DOM、可见文字三个信号，全部静止才提前结束；各段上限与旧版固定等待相同，最坏情况不更慢。

import (
	"context"
	"time"
)

const (
	settleInitialCap     = 1200 * time.Millisecond
	settleStepCap        = 200 * time.Millisecond
	settleScrollSteps    = 3
	settlePoll           = 100 * time.Millisecond
	settleNetworkQuiet   = 300 * time.Millisecond
	settleDomQuiet       = 300.0 // 毫秒（页面侧给出）
	settleTextStable     = 300 * time.Millisecond
	settleMinTextChars   = 200
	settleStepFloor      = 100 * time.Millisecond
	settleStepPoll       = 50 * time.Millisecond
	settleStepNetQuiet   = 50 * time.Millisecond
	settleMaxInitialPoll = 12 + 2 // ceil(1200/100)+2：时钟停滞时也保证循环终止
	settleMaxStepPoll    = 4 + 2  // ceil(200/50)+2
)

type settleSample struct {
	DomQuietMs float64 `json:"domQuietMs"`
	TextLength int     `json:"textLength"`
	CostMs     float64 `json:"costMs"`
}

type settleEnv interface {
	now() time.Time
	pause(ctx context.Context, d time.Duration) error
	scroll(ctx context.Context) error
	// networkQuiet：有会改动 DOM 的请求在途时为 0，否则为距最近一次此类请求开始/结束的时长。
	networkQuiet() time.Duration
	// install 在页面里安装 DOM 观察，返回是否可用。
	install(ctx context.Context) (bool, error)
	// sample 读取页面状态；(nil, nil) 表示观察已失效（文档被替换），需要重新安装。
	sample(ctx context.Context) (*settleSample, error)
}

type settleResult struct {
	Reason    string // quiet | cap | shell | fixed | blind
	InitialMs int
}

// settleTolerate：取消与超时继续向上传播；只有页面侧失败才降级为默认值。
func settleTolerate[T any](ctx context.Context, work func() (T, error), fallback T) (T, error) {
	v, err := work()
	if err != nil {
		if ctx.Err() != nil {
			var zero T
			return zero, ctx.Err()
		}
		return fallback, nil
	}
	return v, nil
}

func settleInitialRest(env settleEnv, startedAt time.Time) time.Duration {
	left := settleInitialCap - env.now().Sub(startedAt)
	if left < 0 {
		return 0
	}
	return left
}

func settleWaitInitial(ctx context.Context, env settleEnv, startedAt time.Time) (reason string, observable bool, err error) {
	rest := func() time.Duration { return settleInitialRest(env, startedAt) }
	blind := func() (string, bool, error) {
		return "blind", false, env.pause(ctx, rest())
	}
	previousText := -1
	textChangedAt := startedAt
	text := 0
	for polls := 0; polls < settleMaxInitialPoll; polls++ {
		sample, sampleErr := env.sample(ctx)
		if sampleErr != nil {
			if ctx.Err() != nil {
				return "", false, ctx.Err()
			}
			return blind()
		}
		if sample == nil {
			// 文档被替换（页面脚本跳转）后观察器随之消失：重新安装，文字长度重新计时。
			previousText = -1
			ok, _ := settleTolerate(ctx, func() (bool, error) { return env.install(ctx) }, false)
			if ctx.Err() != nil {
				return "", false, ctx.Err()
			}
			if !ok {
				return blind()
			}
		} else {
			text = sample.TextLength
			if text != previousText {
				previousText = text
				textChangedAt = env.now()
			}
			// 可见文字与 DOM 一样，需要连续 settleTextStable 不变，才不会被间隔较长的变化漏掉。
			networkQuiet := env.networkQuiet()
			textQuiet := env.now().Sub(textChangedAt)
			if text >= settleMinTextChars && textQuiet >= settleTextStable && sample.DomQuietMs >= settleDomQuiet && networkQuiet >= settleNetworkQuiet {
				return "quiet", true, nil
			}
		}
		left := rest()
		if left <= 0 {
			break
		}
		wait := settlePoll
		if left < wait {
			wait = left
		}
		if err := env.pause(ctx, wait); err != nil {
			return "", false, err
		}
	}
	if text < settleMinTextChars {
		return "shell", true, nil
	}
	return "cap", true, nil
}

// settleWaitStep：一次滚动之后等到没有请求、DOM 也不再变化，最多 settleStepCap。返回是否仍可观察。
func settleWaitStep(ctx context.Context, env settleEnv) (bool, error) {
	startedAt := env.now()
	rest := func() time.Duration {
		left := settleStepCap - env.now().Sub(startedAt)
		if left < 0 {
			return 0
		}
		return left
	}
	if err := env.pause(ctx, settleStepFloor); err != nil {
		return false, err
	}
	for polls := 0; polls < settleMaxStepPoll; polls++ {
		if rest() <= 0 {
			return true, nil
		}
		if env.networkQuiet() >= settleStepNetQuiet {
			sample, err := env.sample(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				return false, env.pause(ctx, rest())
			}
			if sample != nil && sample.DomQuietMs >= float64(settleStepFloor/time.Millisecond) {
				return true, nil
			}
		}
		wait := settleStepPoll
		if left := rest(); left < wait {
			wait = left
		}
		if err := env.pause(ctx, wait); err != nil {
			return false, err
		}
	}
	return true, nil
}

// settleRenderedPage 对应 settleRenderedPage（quiet 模式；GUIZHI_WEB_SETTLE=fixed 的回退由 fixed 参数提供）。
func settleRenderedPage(ctx context.Context, env settleEnv, fixed bool) (settleResult, error) {
	startedAt := env.now()
	reason := "fixed"
	observable := false
	if !fixed {
		ok, err := settleTolerate(ctx, func() (bool, error) { return env.install(ctx) }, false)
		if err != nil {
			return settleResult{}, err
		}
		observable, reason = ok, "blind"
	}
	if observable {
		var err error
		if reason, observable, err = settleWaitInitial(ctx, env, startedAt); err != nil {
			return settleResult{}, err
		}
	} else if err := env.pause(ctx, settleInitialRest(env, startedAt)); err != nil {
		return settleResult{}, err
	}
	initialMs := int(env.now().Sub(startedAt) / time.Millisecond)
	for step := 0; step < settleScrollSteps; step++ {
		if err := env.scroll(ctx); err != nil && ctx.Err() != nil {
			return settleResult{}, err
		}
		if observable {
			var err error
			if observable, err = settleWaitStep(ctx, env); err != nil {
				return settleResult{}, err
			}
		} else if err := env.pause(ctx, settleStepCap); err != nil {
			return settleResult{}, err
		}
	}
	return settleResult{Reason: reason, InitialMs: initialMs}, nil
}
