package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// jobTestEnv 对应 make-golden.mts 的 jobHandlers：时钟、租约持有者、推送与 handler 调用记录都由场景控制。
type jobTestEnv struct {
	rt      *BackgroundJobRuntime
	mu      sync.Mutex
	clock   int64
	sent    []map[string]any
	handled []map[string]any
}

func newJobTestEnv(l *Library) *jobTestEnv {
	e := &jobTestEnv{clock: 1_700_000_000_000}
	rt := newBackgroundJobRuntime(l, func(job map[string]any) {
		e.mu.Lock()
		e.sent = append(e.sent, job)
		e.mu.Unlock()
	})
	rt.owner = "OWNER"
	rt.now = func() int64 {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.clock
	}
	e.rt = rt
	l.jobs = rt
	return e
}

func (e *jobTestEnv) drain() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]any{"sent": append([]map[string]any{}, e.sent...), "handled": append([]map[string]any{}, e.handled...)}
	e.sent, e.handled = nil, nil
	return out
}

func (e *jobTestEnv) run(ctx context.Context, name string, args []json.RawMessage) (any, error) {
	arg := func(i int) any { return jsArg(args, i) }
	switch name {
	case "clock":
		e.mu.Lock()
		e.clock = int64(jsToNumber(arg(0)))
		e.mu.Unlock()
		return nil, nil
	case "register":
		kind, _ := arg(0).(string)
		behavior, _ := arg(1).(string)
		mode, rest, _ := strings.Cut(behavior, ":")
		return nil, e.rt.RegisterHandler(kind, func(_ context.Context, job map[string]any) (*jobHandlerResult, error) {
			e.mu.Lock()
			e.handled = append(e.handled, job)
			e.mu.Unlock()
			switch mode {
			case "throw":
				if !strings.Contains(behavior, ":") {
					rest = "boom"
				}
				return nil, errors.New(rest)
			case "pause":
				return &jobHandlerResult{Pause: true, Message: rest}, nil
			case "next":
				n, _ := strconv.ParseInt(rest, 10, 64)
				return &jobHandlerResult{NextRunAt: &n}, nil
			}
			return nil, nil
		})
	case "schedule":
		in := arg(1)
		si := jobScheduleInput{Kind: jsToString(jsGet(in, "kind")), ScopeID: ""}
		if s, ok := jsGet(in, "scopeId").(string); ok {
			si.ScopeID = s
		}
		if p := jsGet(in, "payload"); !isNullish(p) {
			si.PayloadJSON, _ = jsStringify(p)
		}
		if v := jsGet(in, "intervalMinutes"); !isNullish(v) {
			f := jsToNumber(v)
			si.IntervalMinutes = &f
		}
		if v := jsGet(in, "nextRunAt"); !isNullish(v) {
			n := int64(jsToNumber(v))
			si.NextRunAt = &n
		}
		si.Disabled = jsStrictEqual(jsGet(in, "enabled"), false)
		return e.rt.Schedule(ctx, jsToString(arg(0)), si)
	case "unschedule":
		return e.rt.Unschedule(ctx, jsToString(arg(0)), jsToString(arg(1)))
	case "tick":
		e.rt.tick()
		return nil, nil
	case "drain":
		return e.drain(), nil
	case "settle":
		// 等所有主进程任务跑完（含完成后连带领取的下一批）
		quiet := 0
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			e.rt.mu.Lock()
			n := len(e.rt.running)
			e.rt.mu.Unlock()
			if n == 0 {
				if quiet++; quiet >= 3 {
					return e.drain(), nil
				}
			} else {
				quiet = 0
			}
			time.Sleep(10 * time.Millisecond)
		}
		return nil, fmt.Errorf("settle 超时")
	}
	return nil, fmt.Errorf("未知 job 操作 %s", name)
}
