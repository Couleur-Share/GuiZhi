package aiflow

// 后台任务执行器里 golden 管不到的部分：关闭、panic、回报与续租出错、计时器默认值与真实计时器、并发。
// TS 版没有对应物（浏览器侧客户端随页面存亡，没有关闭，也没有 goroutine）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func (w *warnRecorder) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.Messages...)
}

// bgRig 把假的调度器、下游与计时器装配成一个执行器，供 Go 专有测试使用。
type bgRig struct {
	rec     *bgRecorder
	sched   *bgScheduler
	wiki    *bgDownstream
	sem     *bgDownstream
	tickers *bgFakeTickers
	warns   *warnRecorder
	client  *BackgroundClient
}

func newBgRig(t *testing.T, wiki, semantic []bgGoldenBehavior) *bgRig {
	t.Helper()
	rec := &bgRecorder{}
	rig := &bgRig{
		rec:     rec,
		sched:   &bgScheduler{rec: rec},
		wiki:    &bgDownstream{rec: rec, callName: "compileReadyWiki", queue: wiki},
		sem:     &bgDownstream{rec: rec, callName: "runSemanticIndexing", queue: semantic},
		tickers: &bgFakeTickers{t: t, interval: DefaultHeartbeatInterval},
		warns:   &warnRecorder{},
	}
	rig.client = &BackgroundClient{
		Scheduler: rig.sched,
		Wiki:      bgFakeWiki{rig.wiki},
		Semantic:  bgFakeSemantic{rig.sem},
		Log:       rig.warns,
		LogError: func(scope, action, message string) {
			rec.add(map[string]any{"call": "appError", "scope": scope, "action": action, "message": message})
		},
		newTicker: rig.tickers.newTicker,
		jobDone:   rec.jobDone,
	}
	// 收尾尽力而为：测试因执行器被改坏而失败时，Close 可能永远等不到任务退出，不能让收尾把进程挂住
	t.Cleanup(func() {
		returned := make(chan struct{})
		go func() { rig.client.Close(); close(returned) }()
		select {
		case <-returned:
		case <-time.After(3 * time.Second):
			t.Log("收尾时 Close 超时，放弃等待")
		}
	})
	return rig
}

func (r *bgRig) callNames() []string {
	var names []string
	for _, call := range r.rec.slice(0, r.rec.count()) {
		name, _ := call["call"].(string)
		names = append(names, name)
	}
	return names
}

func (r *bgRig) countOf(name string) int {
	n := 0
	for _, got := range r.callNames() {
		if got == name {
			n++
		}
	}
	return n
}

// quiet 等一小会儿，让多出来的调用有机会现身。
func quiet() { time.Sleep(30 * time.Millisecond) }

func blockedBehavior() []bgGoldenBehavior {
	return []bgGoldenBehavior{{Block: true}}
}

func strPtr(s string) *string { return &s }

// semanticFunc / wikiFunc 用函数当下游，便于写出「忽略取消、照常返回」这类脚本之外的行为。
type semanticFunc func(ctx context.Context, store SemanticIndexStore, onProgress func(int)) (SemanticIndexRunResult, error)

func (f semanticFunc) RunSemanticIndexing(ctx context.Context, store SemanticIndexStore, onProgress func(int)) (SemanticIndexRunResult, error) {
	return f(ctx, store, onProgress)
}

type wikiFunc func(ctx context.Context) (WikiCompileRoundResult, error)

func (f wikiFunc) CompileReady(ctx context.Context, _ func(string, int, int)) (WikiCompileRoundResult, error) {
	return f(ctx)
}

// stubSemanticStore 是什么都不做的索引存储，只用来验证它被原样传给下游。
type stubSemanticStore struct{}

func (*stubSemanticStore) ListPending(context.Context, string, int) ([]PendingSemanticItem, error) {
	return nil, nil
}

func (*stubSemanticStore) ApplyEmbeddings(context.Context, ApplySemanticEmbeddingsInput) (bool, error) {
	return true, nil
}

// ---- 关闭 ----

func TestBackgroundCloseCancelsRunningJobWithoutReporting(t *testing.T) {
	for _, kind := range []string{JobKindWikiCompile, JobKindSemanticIndex} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			rig := newBgRig(t, blockedBehavior(), blockedBehavior())
			rig.client.Handle(BackgroundJob{ID: "job-1", Kind: kind})
			running := "compileReadyWiki"
			if kind == JobKindSemanticIndex {
				running = "runSemanticIndexing"
			}
			waitUntil(t, "下游开始执行", func() bool { return rig.countOf(running) == 1 })

			closed := make(chan struct{})
			go func() { rig.client.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(3 * time.Second):
				t.Fatal("Close 没有取消在途任务，等了 3 秒仍未返回")
			}
			// Close 返回时任务 goroutine 必须已经退出（续租也已停止），不能靠事后等待
			if got := rig.rec.doneCount(); got != 1 {
				t.Fatalf("Close 返回时应已等到任务 goroutine 退出，已退出 %d 个", got)
			}

			quiet()
			for _, name := range rig.callNames() {
				if name == "complete" || name == "fail" || name == "appError" {
					t.Fatalf("被取消的任务不应回报或写错误日志，却出现了 %s：%v", name, rig.callNames())
				}
			}
			if warns := rig.warns.snapshot(); len(warns) != 0 {
				t.Fatalf("被取消不是失败，不应写诊断日志：%v", warns)
			}
			if got := rig.tickers.active(); len(got) != 0 {
				t.Fatalf("Close 之后续租计时器应全部停止，还剩 %d 个", len(got))
			}

			// 关闭之后再来的任务一律忽略；重复 Close 无害
			before := rig.rec.count()
			rig.client.Handle(BackgroundJob{ID: "job-2", Kind: kind})
			rig.client.Close()
			quiet()
			if rig.rec.count() != before {
				t.Fatalf("关闭之后的任务不应被执行：%v", rig.callNames())
			}
		})
	}
}

func TestBackgroundCloseWithoutAnyJob(t *testing.T) {
	var client BackgroundClient
	client.Close()
	client.Close()
	client.Handle(BackgroundJob{ID: "job", Kind: JobKindWikiCompile}) // 关闭后忽略，不应 panic
}

// 关闭打断了一轮之后，下游不论报错、无错返回部分结果（Runner 的语义索引遇到取消就是这样），还是
// 干脆不理会取消、照常跑完，都不回报：不能把没跑完的一轮报成完成，也不能把被打断报成失败。
func TestBackgroundNoReportWhenShutdownInterruptsRun(t *testing.T) {
	type outcome struct {
		name   string
		result SemanticIndexRunResult
		err    error
	}
	outcomes := []outcome{
		{"无错返回部分结果", SemanticIndexRunResult{Indexed: 2}, nil},
		{"无错返回空结果", SemanticIndexRunResult{}, nil},
		{"返回取消错误", SemanticIndexRunResult{}, context.Canceled},
		{"报告部分失败", SemanticIndexRunResult{Indexed: 1, Failed: 1, LastError: "x"}, nil},
	}
	for _, kind := range []string{JobKindWikiCompile, JobKindSemanticIndex} {
		for _, o := range outcomes {
			kind, o := kind, o
			t.Run(kind+"/"+o.name, func(t *testing.T) {
				rig := newBgRig(t, nil, nil)
				started, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				releaseNow := func() { releaseOnce.Do(func() { close(release) }) }
				t.Cleanup(releaseNow) // 测试提前失败时也要放行，免得收尾挂住
				enter := func() {
					close(started)
					<-release // 不理会取消，等测试放行
				}
				rig.client.Semantic = semanticFunc(func(context.Context, SemanticIndexStore, func(int)) (SemanticIndexRunResult, error) {
					enter()
					return o.result, o.err
				})
				rig.client.Wiki = wikiFunc(func(context.Context) (WikiCompileRoundResult, error) {
					enter()
					return WikiCompileRoundResult{}, o.err
				})

				rig.client.Handle(BackgroundJob{ID: "job-1", Kind: kind})
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("下游没有开始执行")
				}
				closed := make(chan struct{})
				go func() { rig.client.Close(); close(closed) }()
				waitUntil(t, "Close 已取消上下文", func() bool { return rig.client.ctx.Err() != nil })
				releaseNow()
				select {
				case <-closed:
				case <-time.After(3 * time.Second):
					t.Fatal("Close 没有在任务结束后返回")
				}

				for _, name := range rig.callNames() {
					if name == "complete" || name == "fail" {
						t.Fatalf("关闭打断的任务不应回报，却出现了 %s：%v", name, rig.callNames())
					}
				}
			})
		}
	}
}

// ---- 回报与续租 ----

// gatedScheduler 在最终回报（完成或失败）上卡住，用来观察回报进行期间的续租与上下文。
type gatedScheduler struct {
	*bgScheduler
	mode    string // "complete" 或 "fail"：在哪一种最终回报上卡住
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once // 保护 gate 只关一次

	mu        sync.Mutex
	reportCtx []bool // 卡住放行之后，回报收到的上下文是否已被取消
	renewCtx  []bool // 续租调用发出时，上下文是否已被取消
}

// newGatedScheduler 建调度器并登记收尾放行：测试失败提前返回时，卡住的回报也要能结束，免得收尾挂住。
func newGatedScheduler(t *testing.T, base *bgScheduler, mode string) *gatedScheduler {
	t.Helper()
	s := &gatedScheduler{bgScheduler: base, mode: mode, entered: make(chan struct{}, 1), gate: make(chan struct{})}
	t.Cleanup(s.open)
	return s
}

func (s *gatedScheduler) open() { s.once.Do(func() { close(s.gate) }) }

func (s *gatedScheduler) hold(ctx context.Context, mode string) {
	if s.mode != mode {
		return
	}
	s.entered <- struct{}{}
	<-s.gate
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reportCtx = append(s.reportCtx, ctx.Err() != nil)
}

func (s *gatedScheduler) Complete(ctx context.Context, id string) (bool, error) {
	ok, err := s.bgScheduler.Complete(ctx, id)
	s.hold(ctx, "complete")
	return ok, err
}

func (s *gatedScheduler) Fail(ctx context.Context, id, message string) (bool, error) {
	ok, err := s.bgScheduler.Fail(ctx, id, message)
	s.hold(ctx, "fail")
	return ok, err
}

func (s *gatedScheduler) Renew(ctx context.Context, id string) (bool, error) {
	s.mu.Lock()
	s.renewCtx = append(s.renewCtx, ctx.Err() != nil)
	s.mu.Unlock()
	return s.bgScheduler.Renew(ctx, id)
}

func (s *gatedScheduler) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-s.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("最终回报没有发出")
	}
}

// 续租直到最终回报结束才停止：回报本身可能很慢，租约不能在这期间过期。
func TestBackgroundHeartbeatContinuesDuringFinalReport(t *testing.T) {
	for _, mode := range []string{"complete", "fail"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			var wiki []bgGoldenBehavior
			if mode == "fail" {
				wiki = []bgGoldenBehavior{{Throws: strPtr("编译失败")}}
			}
			rig := newBgRig(t, wiki, nil)
			sched := newGatedScheduler(t, rig.sched, mode)
			rig.client.Scheduler = sched

			rig.client.Handle(BackgroundJob{ID: "job-1", Kind: JobKindWikiCompile})
			sched.waitEntered(t)

			tickers := rig.tickers.active()
			if len(tickers) != 1 {
				t.Fatalf("最终回报期间续租计时器应仍在运行，实际 %d 个", len(tickers))
			}
			tickers[0].fire()
			waitUntil(t, "最终回报期间的续租", func() bool { return rig.countOf("renew") == 1 })

			sched.open()
			waitUntil(t, "任务 goroutine 退出", func() bool { return rig.rec.doneCount() == 1 })
			if got := rig.tickers.active(); len(got) != 0 {
				t.Fatalf("回报结束之后续租计时器应已停止，还剩 %d 个", len(got))
			}
		})
	}
}

// 已经发出的回报与续租不随关闭取消：IPC 一旦发出就要完成，否则「落库一半」的状态只能靠租约过期收场。
func TestBackgroundInFlightReportSurvivesShutdown(t *testing.T) {
	rig := newBgRig(t, nil, nil)
	sched := newGatedScheduler(t, rig.sched, "complete")
	rig.client.Scheduler = sched

	rig.client.Handle(BackgroundJob{ID: "job-1", Kind: JobKindWikiCompile})
	sched.waitEntered(t) // 下游已跑完，完成回报正在途中

	closed := make(chan struct{})
	go func() { rig.client.Close(); close(closed) }()
	waitUntil(t, "Close 已取消上下文", func() bool { return rig.client.ctx.Err() != nil })

	// 取消之后，心跳照常续租，回报也照常完成
	tickers := rig.tickers.active()
	if len(tickers) != 1 {
		t.Fatalf("回报进行中续租计时器应仍在运行，实际 %d 个", len(tickers))
	}
	tickers[0].fire()
	waitUntil(t, "取消之后的续租", func() bool { return rig.countOf("renew") == 1 })
	sched.open()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close 没有在回报结束后返回")
	}

	sched.mu.Lock()
	defer sched.mu.Unlock()
	if len(sched.reportCtx) != 1 || sched.reportCtx[0] {
		t.Fatalf("完成回报的上下文不应被取消，实际 %v", sched.reportCtx)
	}
	if len(sched.renewCtx) != 1 || sched.renewCtx[0] {
		t.Fatalf("续租的上下文不应被取消，实际 %v", sched.renewCtx)
	}
	if got := rig.countOf("fail"); got != 0 {
		t.Fatalf("不应回报失败：%v", rig.callNames())
	}
}

// reportErrorScheduler 让续租与失败回报返回错误。
type reportErrorScheduler struct {
	*bgScheduler
	renewErr error
	failErr  error
}

func (s *reportErrorScheduler) Renew(ctx context.Context, id string) (bool, error) {
	if _, err := s.bgScheduler.Renew(ctx, id); err != nil {
		return false, err
	}
	return false, s.renewErr
}

func (s *reportErrorScheduler) Fail(ctx context.Context, id, message string) (bool, error) {
	if _, err := s.bgScheduler.Fail(ctx, id, message); err != nil {
		return false, err
	}
	return false, s.failErr
}

func TestBackgroundRenewAndFailErrorsAreLoggedNotFatal(t *testing.T) {
	rig := newBgRig(t, []bgGoldenBehavior{{Block: true, Throws: strPtr("编译失败")}, {}}, nil)
	rig.client.Scheduler = &reportErrorScheduler{bgScheduler: rig.sched, renewErr: errors.New("IPC 断开"), failErr: errors.New("回报被拒")}

	rig.client.Handle(BackgroundJob{ID: "job-1", Kind: JobKindWikiCompile})
	waitUntil(t, "下游开始执行", func() bool { return rig.countOf("compileReadyWiki") == 1 })

	// 续租出错：记诊断日志，心跳不停，下个间隔继续续租
	for round := 1; round <= 2; round++ {
		tickers := rig.tickers.active()
		if len(tickers) != 1 {
			t.Fatalf("应有一个续租计时器，实际 %d 个", len(tickers))
		}
		tickers[0].fire()
		waitUntil(t, fmt.Sprintf("第 %d 次续租", round), func() bool { return rig.countOf("renew") == round })
	}
	waitUntil(t, "续租错误写入日志", func() bool {
		n := 0
		for _, msg := range rig.warns.snapshot() {
			if strings.Contains(msg, "job-1") && strings.Contains(msg, "IPC 断开") {
				n++
			}
		}
		return n == 2
	})

	// 下游抛错后回报失败，回报本身也出错：同样只记日志，执行器继续可用
	rig.wiki.releaseOldest()
	waitUntil(t, "回报失败", func() bool { return rig.countOf("fail") == 1 })
	waitUntil(t, "任务 goroutine 退出", func() bool { return rig.rec.doneCount() == 1 })
	found := false
	for _, msg := range rig.warns.snapshot() {
		if strings.Contains(msg, "job-1") && strings.Contains(msg, "回报被拒") {
			found = true
		}
	}
	if !found {
		t.Fatalf("回报失败时的错误应写入日志：%v", rig.warns.snapshot())
	}

	rig.client.Handle(BackgroundJob{ID: "job-2", Kind: JobKindWikiCompile})
	waitUntil(t, "之后的任务照常完成", func() bool { return rig.countOf("complete") == 1 })
}

// panicWiki 第一次调用时 panic，之后返回空结果。
type panicWiki struct {
	mu    sync.Mutex
	calls int
	value any
}

func (p *panicWiki) CompileReady(context.Context, func(string, int, int)) (WikiCompileRoundResult, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.mu.Unlock()
	if n == 1 {
		panic(p.value)
	}
	return WikiCompileRoundResult{}, nil
}

func TestBackgroundPanicBecomesFailAndReleasesGuard(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"字符串", "boom", "boom"},
		{"error", errors.New("下游出错"), "下游出错"},
		{"其他类型", 42, "42"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rig := newBgRig(t, nil, nil)
			rig.client.Wiki = &panicWiki{value: tc.value}

			rig.client.Handle(BackgroundJob{ID: "job-1", Kind: JobKindWikiCompile})
			waitUntil(t, "panic 之后回报失败", func() bool { return rig.countOf("fail") == 1 })
			waitUntil(t, "任务 goroutine 退出", func() bool { return rig.rec.doneCount() == 1 })
			calls := rig.rec.slice(0, rig.rec.count())
			last := calls[len(calls)-1]
			if last["call"] != "fail" || last["id"] != "job-1" || last["error"] != tc.want {
				t.Fatalf("应以 %q 回报失败，实际 %v", tc.want, calls)
			}

			// 守卫已复位：下一个任务照常执行并完成
			rig.client.Handle(BackgroundJob{ID: "job-2", Kind: JobKindWikiCompile})
			waitUntil(t, "第二个任务完成", func() bool { return rig.countOf("complete") == 1 })
		})
	}
}

func TestBackgroundHeartbeatIntervalDefaultsTo30Seconds(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{"零值取默认", 0, 30 * time.Second},
		{"负值取默认", -time.Second, 30 * time.Second},
		{"自定义", 7 * time.Second, 7 * time.Second},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rig := newBgRig(t, nil, nil)
			got := make(chan time.Duration, 1)
			rig.client.HeartbeatInterval = tc.interval
			rig.client.newTicker = func(d time.Duration) heartbeatTicker {
				got <- d
				return rig.tickers.newTicker(d)
			}
			rig.tickers.interval = tc.want
			rig.client.Handle(BackgroundJob{ID: "job", Kind: JobKindWikiCompile})
			select {
			case d := <-got:
				if d != tc.want {
					t.Fatalf("续租间隔应为 %v，实际 %v", tc.want, d)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("没有创建续租计时器")
			}
			waitUntil(t, "任务完成", func() bool { return rig.countOf("complete") == 1 })
		})
	}
}

// blockingRenewScheduler 的续租会卡住，用来制造「续租在途时停止心跳」。
type blockingRenewScheduler struct {
	*bgScheduler
	entered chan struct{}
	gate    chan struct{}
}

func (s *blockingRenewScheduler) Renew(ctx context.Context, id string) (bool, error) {
	s.entered <- struct{}{}
	<-s.gate
	return s.bgScheduler.Renew(ctx, id)
}

// 停止心跳要等在途的那次续租返回：stop 返回之后不能再有续租发出，Close 返回时也不能留下 goroutine。
func TestHeartbeatStopWaitsForInFlightRenew(t *testing.T) {
	rig := newBgRig(t, nil, nil)
	sched := &blockingRenewScheduler{bgScheduler: rig.sched, entered: make(chan struct{}, 1), gate: make(chan struct{})}
	var once sync.Once
	open := func() { once.Do(func() { close(sched.gate) }) }
	t.Cleanup(open)
	rig.client.Scheduler = sched
	ticker := &bgFakeTicker{ch: make(chan time.Time, 1)}
	rig.client.newTicker = func(time.Duration) heartbeatTicker { return ticker }

	stop := rig.client.startHeartbeat(context.Background(), "job-1")
	ticker.fire()
	select {
	case <-sched.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("续租没有发出")
	}

	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("stop 应等在途的续租返回，却提前返回了")
	case <-time.After(50 * time.Millisecond):
	}
	open()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("续租返回之后 stop 仍未返回")
	}
	if !ticker.stopped.Load() {
		t.Fatal("stop 应停止计时器")
	}
	if got := rig.countOf("renew"); got != 1 {
		t.Fatalf("应只有在途的那一次续租，实际 %d 次", got)
	}
}

func TestBackgroundRealTickerRenewsUntilReportDone(t *testing.T) {
	rig := newBgRig(t, blockedBehavior(), nil)
	rig.client.newTicker = nil // 用真实计时器
	rig.client.HeartbeatInterval = 5 * time.Millisecond

	rig.client.Handle(BackgroundJob{ID: "job-1", Kind: JobKindWikiCompile})
	waitUntil(t, "至少续租两次", func() bool { return rig.countOf("renew") >= 2 })
	rig.wiki.releaseOldest()
	waitUntil(t, "任务完成", func() bool { return rig.countOf("complete") == 1 })
	waitUntil(t, "任务 goroutine 退出", func() bool { return rig.rec.doneCount() == 1 })

	after := rig.countOf("renew")
	quiet()
	if got := rig.countOf("renew"); got != after {
		t.Fatalf("回报结束之后不应再续租：%d → %d", after, got)
	}
	if got := rig.countOf("fail"); got != 0 {
		t.Fatalf("不应回报失败：%v", rig.callNames())
	}
}

// ---- 并发 ----

func TestBackgroundConcurrentHandleReportsEachJobOnce(t *testing.T) {
	const jobs = 60
	rig := newBgRig(t, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			kind := JobKindWikiCompile
			if i%2 == 1 {
				kind = JobKindSemanticIndex
			}
			rig.client.Handle(BackgroundJob{ID: fmt.Sprintf("job-%d", i), Kind: kind})
		}()
	}
	wg.Wait()
	waitUntil(t, "全部任务回报完成", func() bool { return rig.countOf("complete") == jobs })
	waitUntil(t, "任务 goroutine 全部退出", func() bool { return rig.rec.doneCount() == jobs })

	seen := map[string]int{}
	for _, call := range rig.rec.slice(0, rig.rec.count()) {
		if call["call"] == "complete" {
			seen[call["id"].(string)]++
		}
	}
	for i := 0; i < jobs; i++ {
		if id := fmt.Sprintf("job-%d", i); seen[id] != 1 {
			t.Errorf("任务 %s 应恰好回报一次完成，实际 %d 次", id, seen[id])
		}
	}
	if got := rig.countOf("fail"); got != 0 {
		t.Errorf("不应有失败回报：%v", rig.callNames())
	}
	rig.client.mu.Lock()
	compiling, indexing := rig.client.compiling, rig.client.indexing
	rig.client.mu.Unlock()
	if compiling || indexing {
		t.Errorf("全部结束后守卫应复位：compiling=%v indexing=%v", compiling, indexing)
	}
}

// ---- 同步设置 ----

func TestSyncSettingsWithoutSwitchesOrConfigDisablesBoth(t *testing.T) {
	rig := newBgRig(t, nil, nil)
	if err := rig.client.SyncSettings(context.Background()); err != nil {
		t.Fatalf("SyncSettings 失败：%v", err)
	}
	want := []map[string]any{{"call": "syncRenderer", "wikiEnabled": false, "semanticEnabled": false}}
	assertSame(t, "没有开关与配置时的同步", want, rig.rec.slice(0, rig.rec.count()))
}

// errSyncScheduler 让 Sync 返回错误，并记下收到的上下文取值。
type errSyncScheduler struct {
	*bgScheduler
	err error
	got chan any
}

type ctxKey struct{}

func (s *errSyncScheduler) Sync(ctx context.Context, wiki, semantic bool) error {
	s.got <- ctx.Value(ctxKey{})
	_ = s.bgScheduler.Sync(ctx, wiki, semantic)
	return s.err
}

func TestSyncSettingsPassesContextAndReturnsSchedulerError(t *testing.T) {
	rig := newBgRig(t, nil, nil)
	want := errors.New("调度器不可用")
	sched := &errSyncScheduler{bgScheduler: rig.sched, err: want, got: make(chan any, 1)}
	rig.client.Scheduler = sched

	got := rig.client.SyncSettings(context.WithValue(context.Background(), ctxKey{}, "marker"))
	if !errors.Is(got, want) {
		t.Fatalf("应原样返回调度器的错误，实际 %v", got)
	}
	if v := <-sched.got; v != "marker" {
		t.Fatalf("调度器应收到调用方的上下文，实际 %v", v)
	}
}

// ---- 手动入口（RunWikiCompile / RunSemanticIndexing）的细节 ----

func TestRunSemanticIndexingForwardsStoreAndProgress(t *testing.T) {
	store := &stubSemanticStore{}
	var gotStore SemanticIndexStore
	rig := newBgRig(t, nil, nil)
	rig.client.Store = store
	rig.client.Semantic = semanticFunc(func(_ context.Context, s SemanticIndexStore, onProgress func(int)) (SemanticIndexRunResult, error) {
		gotStore = s
		onProgress(2)
		onProgress(5)
		return SemanticIndexRunResult{Indexed: 5}, nil
	})

	var progress []int
	notice := rig.client.RunSemanticIndexing(context.Background(), func(n int) { progress = append(progress, n) })
	if gotStore != SemanticIndexStore(store) {
		t.Fatalf("应把 Store 原样传给下游，实际 %v", gotStore)
	}
	assertSame(t, "进度回调", []int{2, 5}, progress)
	assertSame(t, "回执", map[string]any{"kind": "done", "indexed": 5, "failed": 0}, noticeJSON(notice))
}

func TestRunSemanticIndexingLogsFailureUnlessCancelled(t *testing.T) {
	failing := semanticFunc(func(context.Context, SemanticIndexStore, func(int)) (SemanticIndexRunResult, error) {
		return SemanticIndexRunResult{}, errors.New("下游出错")
	})

	t.Run("未取消时写诊断日志", func(t *testing.T) {
		rig := newBgRig(t, nil, nil)
		rig.client.Semantic = failing
		notice := rig.client.RunSemanticIndexing(context.Background(), nil)
		assertSame(t, "回执", map[string]any{"kind": "failed", "indexed": 0, "failed": 0, "message": "下游出错"}, noticeJSON(notice))
		warns := rig.warns.snapshot()
		if len(warns) != 1 || !strings.Contains(warns[0], "下游出错") {
			t.Fatalf("应写一条带原因的诊断日志，实际 %v", warns)
		}
	})
	t.Run("已取消时不写", func(t *testing.T) {
		rig := newBgRig(t, nil, nil)
		rig.client.Semantic = failing
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		notice := rig.client.RunSemanticIndexing(ctx, nil)
		if notice == nil || notice.Kind != NoticeFailed {
			t.Fatalf("被取消时仍应返回 failed 回执，实际 %v", notice)
		}
		if warns := rig.warns.snapshot(); len(warns) != 0 {
			t.Fatalf("被取消不是索引失败，不应写诊断日志：%v", warns)
		}
	})
}

func TestRunWikiCompileLogsFailureUnlessCancelled(t *testing.T) {
	failing := wikiFunc(func(context.Context) (WikiCompileRoundResult, error) {
		return WikiCompileRoundResult{}, errors.New("编译出错")
	})

	t.Run("未取消时写应用日志并返回错误", func(t *testing.T) {
		rig := newBgRig(t, nil, nil)
		rig.client.Wiki = failing
		err := rig.client.RunWikiCompile(context.Background())
		if err == nil || err.Error() != "编译出错" {
			t.Fatalf("应原样返回下游的错误，实际 %v", err)
		}
		want := []map[string]any{{"call": "appError", "scope": "wiki-compile", "action": "后台 Wiki 编译", "message": "编译出错"}}
		assertSame(t, "应用日志", want, rig.rec.slice(0, rig.rec.count()))
	})
	t.Run("已取消时不写但仍返回错误", func(t *testing.T) {
		rig := newBgRig(t, nil, nil)
		rig.client.Wiki = failing
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := rig.client.RunWikiCompile(ctx); err == nil {
			t.Fatal("被取消的编译仍应把下游的错误返回给调用方")
		}
		if got := rig.countOf("appError"); got != 0 {
			t.Fatalf("被取消不是编译失败，不应写应用日志：%v", rig.callNames())
		}
	})
}
