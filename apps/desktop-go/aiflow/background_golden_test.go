package aiflow

// 后台任务执行器的 TS 对拍：重放 aiflow-golden-background.test.ts 导出的 background-client.json。
// 那份脚本在 jsdom 里渲染真实的 useBackgroundJobClient（假的 window.api.backgroundJob、假的 Wiki 编译与语义索引、
// 虚拟时钟），记录每一步里调度器与日志收到的调用；这里用同一份脚本驱动 BackgroundClient，逐步比较。
//
// 时间与并发：续租计时器由假的 ticker 驱动，「过 N 个 30 秒」就是对每个仍在续租的任务按创建顺序依次
// 触发一次 tick，并等这次续租到达调度器之后再触发下一个——两个任务的续租在 TS 里按创建顺序交错，
// Go 里它们是两个 goroutine，不这样串行化顺序就不确定。每一步结束后还要等任务 goroutine 完全退出
// （续租已停），下一步才开始，免得上一个任务的收尾与下一步的 tick 抢跑。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- golden 结构 ----

type bgGoldenBehavior struct {
	Result map[string]any `json:"result"`
	Throws *string        `json:"throws"`
	Block  bool           `json:"block"`
}

type bgGoldenJob struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type bgGoldenStep struct {
	Op       string         `json:"op"`
	Settings map[string]any `json:"settings"`
	Job      *bgGoldenJob   `json:"job"`
	Ticks    int            `json:"ticks"`
	Runner   string         `json:"runner"`
	Wiki     *bool          `json:"wiki"`
	Semantic *bool          `json:"semantic"`
}

type bgGoldenExpectStep struct {
	Calls []map[string]any `json:"calls"`
	// 缺省（长度 0）= 这一步没有回执；非指针的 RawMessage 才能把 JSON null 保留成 "null"（手动索引被守卫挡下时的回执）
	Notice json.RawMessage `json:"notice"`
}

type bgGoldenCase struct {
	Name     string             `json:"name"`
	Settings map[string]any     `json:"settings"`
	Wiki     []bgGoldenBehavior `json:"wiki"`
	Semantic []bgGoldenBehavior `json:"semantic"`
	Complete []json.RawMessage  `json:"complete"`
	Steps    []bgGoldenStep     `json:"steps"`
	Expect   struct {
		Steps        []bgGoldenExpectStep `json:"steps"`
		BlockedAtEnd struct {
			Wiki     int `json:"wiki"`
			Semantic int `json:"semantic"`
		} `json:"blockedAtEnd"`
	} `json:"expect"`
}

type bgGolden struct {
	HeartbeatMs int64          `json:"heartbeatMs"`
	Cases       []bgGoldenCase `json:"cases"`
}

// ---- 调用记录 ----

type bgRecorder struct {
	mu    sync.Mutex
	calls []map[string]any
	done  int // 已退出的任务 goroutine 数
}

func (r *bgRecorder) add(call map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *bgRecorder) jobDone() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done++
}

func (r *bgRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *bgRecorder) doneCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

func (r *bgRecorder) slice(from, to int) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if to > len(r.calls) {
		to = len(r.calls)
	}
	return append([]map[string]any{}, r.calls[from:to]...)
}

// handleWithin 调用 Handle 并要求它立即返回：调度器在持有自己的锁时交付任务，Handle 一旦阻塞就会死锁。
func handleWithin(t *testing.T, client *BackgroundClient, job BackgroundJob) {
	t.Helper()
	returned := make(chan struct{})
	go func() { client.Handle(job); close(returned) }()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatalf("Handle 没有立即返回（任务 %q）：调度器持锁交付任务时会死锁", job.ID)
	}
}

// closeWithin 调用 Close 并要求它在限时内返回：Close 必须能取消在途任务并等它们退出。
func closeWithin(t *testing.T, client *BackgroundClient) {
	t.Helper()
	returned := make(chan struct{})
	go func() { client.Close(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("Close 没有取消在途任务，等了 3 秒仍未返回")
	}
}

// waitUntil 轮询等条件成立，超时即失败（执行器漏掉一次调用时不至于让测试挂住）。
func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// ---- 假的调度器 ----

type bgCompleteOutcome struct {
	lost    bool
	rejects *string
}

type bgScheduler struct {
	rec      *bgRecorder
	mu       sync.Mutex
	complete []bgCompleteOutcome
}

func (s *bgScheduler) Sync(_ context.Context, wikiEnabled, semanticEnabled bool) error {
	s.rec.add(map[string]any{"call": "syncRenderer", "wikiEnabled": wikiEnabled, "semanticEnabled": semanticEnabled})
	return nil
}

func (s *bgScheduler) Renew(_ context.Context, id string) (bool, error) {
	s.rec.add(map[string]any{"call": "renew", "id": id})
	return true, nil
}

func (s *bgScheduler) Complete(_ context.Context, id string) (bool, error) {
	s.rec.add(map[string]any{"call": "complete", "id": id})
	s.mu.Lock()
	var outcome bgCompleteOutcome
	if len(s.complete) > 0 {
		outcome, s.complete = s.complete[0], s.complete[1:]
	}
	s.mu.Unlock()
	if outcome.rejects != nil {
		return false, errors.New(*outcome.rejects)
	}
	return !outcome.lost, nil
}

func (s *bgScheduler) Fail(_ context.Context, id, message string) (bool, error) {
	s.rec.add(map[string]any{"call": "fail", "id": id, "error": message})
	return true, nil
}

func parseBgComplete(t *testing.T, raw []json.RawMessage) []bgCompleteOutcome {
	t.Helper()
	out := make([]bgCompleteOutcome, 0, len(raw))
	for _, item := range raw {
		var text string
		if json.Unmarshal(item, &text) == nil {
			switch text {
			case "ok":
				out = append(out, bgCompleteOutcome{})
			case "lost":
				out = append(out, bgCompleteOutcome{lost: true})
			default:
				t.Fatalf("未知的 complete 结果 %q", text)
			}
			continue
		}
		var obj struct {
			Rejects string `json:"rejects"`
		}
		if err := json.Unmarshal(item, &obj); err != nil {
			t.Fatalf("无法解析 complete 结果 %s：%v", item, err)
		}
		out = append(out, bgCompleteOutcome{rejects: &obj.Rejects})
	}
	return out
}

// ---- 假的下游（Wiki 编译 / 语义索引） ----

type bgPendingCall struct {
	behavior bgGoldenBehavior
	release  chan struct{}
}

// bgDownstream 按次序吐出脚本里的行为；block 的调用卡到 release 或上下文取消。
type bgDownstream struct {
	rec      *bgRecorder
	callName string
	mu       sync.Mutex
	queue    []bgGoldenBehavior
	pending  []*bgPendingCall
}

func (d *bgDownstream) next(ctx context.Context) (bgGoldenBehavior, error) {
	d.rec.add(map[string]any{"call": d.callName})
	d.mu.Lock()
	var behavior bgGoldenBehavior
	if len(d.queue) > 0 {
		behavior, d.queue = d.queue[0], d.queue[1:]
	}
	if !behavior.Block {
		d.mu.Unlock()
		return behavior, nil
	}
	call := &bgPendingCall{behavior: behavior, release: make(chan struct{})}
	d.pending = append(d.pending, call)
	d.mu.Unlock()
	select {
	case <-call.release:
		return behavior, nil
	case <-ctx.Done():
		return behavior, ctx.Err()
	}
}

func (d *bgDownstream) releaseOldest() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.pending) == 0 {
		return false
	}
	call := d.pending[0]
	d.pending = d.pending[1:]
	close(call.release)
	return true
}

func (d *bgDownstream) blocked() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}

type bgFakeWiki struct{ down *bgDownstream }

func (w bgFakeWiki) CompileReady(ctx context.Context, _ func(string, int, int)) (WikiCompileRoundResult, error) {
	behavior, err := w.down.next(ctx)
	if err != nil {
		return WikiCompileRoundResult{}, err
	}
	if behavior.Throws != nil {
		return WikiCompileRoundResult{}, errors.New(*behavior.Throws)
	}
	var result WikiCompileRoundResult
	if behavior.Result != nil {
		raw, _ := json.Marshal(behavior.Result)
		if err := json.Unmarshal(raw, &result); err != nil {
			return WikiCompileRoundResult{}, fmt.Errorf("脚本里的编译结果无法解析：%w", err)
		}
	}
	return result, nil
}

type bgFakeSemantic struct{ down *bgDownstream }

func (s bgFakeSemantic) RunSemanticIndexing(ctx context.Context, _ SemanticIndexStore, _ func(int)) (SemanticIndexRunResult, error) {
	behavior, err := s.down.next(ctx)
	if err != nil {
		return SemanticIndexRunResult{}, err
	}
	if behavior.Throws != nil {
		return SemanticIndexRunResult{}, errors.New(*behavior.Throws)
	}
	var result SemanticIndexRunResult
	result.Skipped, _ = behavior.Result["skipped"].(bool)
	if v, ok := behavior.Result["indexed"].(float64); ok {
		result.Indexed = int(v)
	}
	if v, ok := behavior.Result["failed"].(float64); ok {
		result.Failed = int(v)
	}
	result.LastError, _ = behavior.Result["lastError"].(string)
	return result, nil
}

// ---- 假的续租计时器 ----

type bgFakeTicker struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func (f *bgFakeTicker) C() <-chan time.Time { return f.ch }
func (f *bgFakeTicker) Stop()               { f.stopped.Store(true) }

func (f *bgFakeTicker) fire() {
	select {
	case f.ch <- time.Time{}:
	default:
	}
}

type bgFakeTickers struct {
	t        *testing.T
	interval time.Duration
	mu       sync.Mutex
	all      []*bgFakeTicker
}

func (f *bgFakeTickers) newTicker(d time.Duration) heartbeatTicker {
	if d != f.interval {
		f.t.Errorf("续租间隔应为 %v，实际 %v", f.interval, d)
	}
	ticker := &bgFakeTicker{ch: make(chan time.Time, 1)}
	f.mu.Lock()
	f.all = append(f.all, ticker)
	f.mu.Unlock()
	return ticker
}

// active 按创建顺序返回还没停止的计时器。
func (f *bgFakeTickers) active() []*bgFakeTicker {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*bgFakeTicker
	for _, ticker := range f.all {
		if !ticker.stopped.Load() {
			out = append(out, ticker)
		}
	}
	return out
}

// ---- 重放 ----

func TestBackgroundClientGolden(t *testing.T) {
	var golden bgGolden
	loadGolden(t, "background-client", &golden)
	if len(golden.Cases) < 75 {
		t.Fatalf("golden 用例过少：%d", len(golden.Cases))
	}
	if time.Duration(golden.HeartbeatMs)*time.Millisecond != DefaultHeartbeatInterval {
		t.Fatalf("TS 的续租间隔是 %d ms，与 DefaultHeartbeatInterval=%v 不一致", golden.HeartbeatMs, DefaultHeartbeatInterval)
	}
	for _, c := range golden.Cases {
		c := c
		t.Run(c.Name, func(t *testing.T) { replayBackgroundCase(t, c) })
	}
}

func bgBool(spec map[string]any, key string) bool {
	v, _ := spec[key].(bool)
	return v
}

func replayBackgroundCase(t *testing.T, c bgGoldenCase) {
	if len(c.Expect.Steps) != len(c.Steps) {
		t.Fatalf("golden 的步骤数 %d 与期望数 %d 不一致", len(c.Steps), len(c.Expect.Steps))
	}
	rec := &bgRecorder{}
	scheduler := &bgScheduler{rec: rec, complete: parseBgComplete(t, c.Complete)}
	wikiDown := &bgDownstream{rec: rec, callName: "compileReadyWiki", queue: c.Wiki}
	semDown := &bgDownstream{rec: rec, callName: "runSemanticIndexing", queue: c.Semantic}
	tickers := &bgFakeTickers{t: t, interval: DefaultHeartbeatInterval}

	cfg := &mutableConfig{settings: NormalizeAISettings(c.Settings)}
	var switchMu sync.Mutex
	switches := BackgroundSwitches{
		BackgroundTasksEnabled: bgBool(c.Settings, "backgroundTasksEnabled"),
		WikiCompileEnabled:     bgBool(c.Settings, "wikiCompileEnabled"),
	}
	applySettings := func(spec map[string]any) {
		cfg.set(NormalizeAISettings(spec))
		switchMu.Lock()
		defer switchMu.Unlock()
		switches = BackgroundSwitches{
			BackgroundTasksEnabled: bgBool(spec, "backgroundTasksEnabled"),
			WikiCompileEnabled:     bgBool(spec, "wikiCompileEnabled"),
		}
	}

	client := &BackgroundClient{
		Scheduler: scheduler,
		Switches: func() BackgroundSwitches {
			switchMu.Lock()
			defer switchMu.Unlock()
			return switches
		},
		Config:   cfg,
		Wiki:     bgFakeWiki{wikiDown},
		Semantic: bgFakeSemantic{semDown},
		LogError: func(scope, action, message string) {
			rec.add(map[string]any{"call": "appError", "scope": scope, "action": action, "message": message})
		},
		newTicker: tickers.newTicker,
		jobDone:   rec.jobDone,
	}
	ctx := context.Background()

	mounted := false
	started := 0 // 被执行器接手的任务数（含被守卫跳过的）
	offset := 0  // 已对上的 golden 调用数
	for i, step := range c.Steps {
		want := c.Expect.Steps[i]
		var notice *SemanticRunNotice
		sawNotice := false

		switch step.Op {
		case "mount":
			if mounted {
				t.Fatalf("第 %d 步重复挂载", i+1)
			}
			mounted = true
			if err := client.SyncSettings(ctx); err != nil {
				t.Fatalf("第 %d 步（mount）同步设置失败：%v", i+1, err)
			}
		case "settings":
			applySettings(step.Settings)
			if mounted {
				if err := client.SyncSettings(ctx); err != nil {
					t.Fatalf("第 %d 步（settings）同步设置失败：%v", i+1, err)
				}
			}
		case "available":
			if mounted && (step.Job.Kind == JobKindWikiCompile || step.Job.Kind == JobKindSemanticIndex) {
				started++
			}
			handleWithin(t, client, BackgroundJob{ID: step.Job.ID, Kind: step.Job.Kind})
		case "advance":
			for k := 0; k < step.Ticks; k++ {
				for _, ticker := range tickers.active() {
					before := rec.count()
					ticker.fire()
					waitUntil(t, fmt.Sprintf("第 %d 步（advance）的一次续租", i+1), func() bool { return rec.count() > before })
				}
			}
		case "release":
			down := wikiDown
			if step.Runner == "semantic" {
				down = semDown
			}
			if !down.releaseOldest() {
				t.Fatalf("第 %d 步：没有卡住的 %s 调用可放行", i+1, step.Runner)
			}
		case "busy":
			client.mu.Lock()
			if step.Wiki != nil {
				client.compiling = *step.Wiki
			}
			if step.Semantic != nil {
				client.indexing = *step.Semantic
			}
			client.mu.Unlock()
		case "manualIndexing":
			notice = client.RunSemanticIndexing(ctx, nil)
			sawNotice = true
		case "unmount":
			closeWithin(t, client)
			mounted = false
		default:
			t.Fatalf("第 %d 步：未知操作 %q", i+1, step.Op)
		}

		// 等这一步的调用到齐，并等所有该结束的任务 goroutine 退出（仍卡住的除外）
		target := offset + len(want.Calls)
		waitUntil(t, fmt.Sprintf("第 %d 步（%s）的 %d 个调用", i+1, step.Op, len(want.Calls)), func() bool { return rec.count() >= target })
		waitUntil(t, fmt.Sprintf("第 %d 步（%s）之后任务 goroutine 收尾", i+1, step.Op), func() bool {
			return rec.doneCount() >= started-wikiDown.blocked()-semDown.blocked()
		})
		// 给多出来的调用一点现身的时间
		time.Sleep(time.Millisecond)

		label := fmt.Sprintf("第 %d 步（%s）的调用", i+1, step.Op)
		assertSame(t, label, want.Calls, rec.slice(offset, rec.count()))
		offset = rec.count()

		hasNotice := len(want.Notice) > 0
		switch {
		case !hasNotice && sawNotice:
			t.Errorf("第 %d 步（manualIndexing）：golden 里没有回执，Go 却返回了回执", i+1)
		case hasNotice && !sawNotice:
			t.Errorf("第 %d 步（%s）：golden 里有回执，这一步却没有手动索引", i+1, step.Op)
		case hasNotice:
			var wantNotice any
			if err := json.Unmarshal(want.Notice, &wantNotice); err != nil {
				t.Fatalf("第 %d 步：golden 的回执无法解析：%v", i+1, err)
			}
			assertSame(t, fmt.Sprintf("第 %d 步（manualIndexing）的回执", i+1), wantNotice, noticeJSON(notice))
		}
		if t.Failed() {
			return
		}
	}

	if got := wikiDown.blocked(); got != c.Expect.BlockedAtEnd.Wiki {
		t.Errorf("结束时卡住的 Wiki 调用应为 %d，实际 %d", c.Expect.BlockedAtEnd.Wiki, got)
	}
	if got := semDown.blocked(); got != c.Expect.BlockedAtEnd.Semantic {
		t.Errorf("结束时卡住的语义调用应为 %d，实际 %d", c.Expect.BlockedAtEnd.Semantic, got)
	}

	// 收尾：关闭执行器取消卡住的任务，被取消的任务不能再产生任何调用（含错误日志）
	closeWithin(t, client)
	if extra := rec.slice(offset, rec.count()); len(extra) > 0 {
		t.Errorf("Close 之后又出现了调用：%v", extra)
	}
}

// noticeJSON 把回执转成与 golden 同形的结构；message 为空串时省略（TS 侧同样处理）。
func noticeJSON(n *SemanticRunNotice) any {
	if n == nil {
		return nil
	}
	out := map[string]any{"kind": string(n.Kind), "indexed": n.Indexed, "failed": n.Failed}
	if n.Message != "" {
		out["message"] = n.Message
	}
	return out
}
