package main

// aiflow 后台任务执行器与主包真实调度器（BackgroundJobRuntime）的联调：
// 同一份 SQLite 库副本、同一套租约与退避逻辑，只把下游的 Wiki 编译与语义索引换成脚本化的假实现、
// 把时钟换成可控的假时钟。验证 aiflow 包内的假调度器看不出的三件事：
//   - 接线：同步开关 → 任务排期 → 领取 → 执行 → 完成 / 失败回报，库里的状态与重试退避符合预期；
//   - 死锁：调度器在持有自己的锁时交付任务，执行器的 Handle 必须不阻塞，完成回报才能回到调度器拿同一把锁；
//   - 租约：续租把租约往后推；执行器关闭后不回报，租约过期后调度器重新领取。
//
// aiflowJobScheduler 是原生壳接入时要写的适配器的最小形态，目前只放在测试里：aiflow 还没有被主包 import，
// 在原生界面接入之前，不让它进入外壳的二进制。

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/aiflow"
)

// aiflowJobScheduler 把 BackgroundJobRuntime 接成 aiflow.JobScheduler。
// 失败回报不暂停任务（pause=false），由调度器按退避重试，与渲染层客户端一致。
type aiflowJobScheduler struct{ rt *BackgroundJobRuntime }

func (s aiflowJobScheduler) Sync(ctx context.Context, wikiEnabled, semanticEnabled bool) error {
	_, err := s.rt.SyncRendererJobs(ctx, wikiEnabled, semanticEnabled)
	return err
}

func (s aiflowJobScheduler) Renew(ctx context.Context, id string) (bool, error) {
	return s.rt.RenewRendererJob(ctx, id)
}

func (s aiflowJobScheduler) Complete(ctx context.Context, id string) (bool, error) {
	return s.rt.CompleteRendererJob(ctx, id)
}

func (s aiflowJobScheduler) Fail(ctx context.Context, id, message string) (bool, error) {
	return s.rt.FailRendererJob(ctx, id, message, false)
}

// afStaticConfig 是固定的 AI 设置。
type afStaticConfig struct{ settings aiflow.AISettings }

func (c afStaticConfig) AISettings() aiflow.AISettings { return c.settings }

// afRounds 是脚本化的下游：第 n 次调用执行第 n 个脚本，超出脚本的调用返回空结果；每次调用开始时在 started 上记一笔。
type afRounds struct {
	mu      sync.Mutex
	calls   int
	wiki    []func(ctx context.Context) (aiflow.WikiCompileRoundResult, error)
	sem     []func(ctx context.Context) (aiflow.SemanticIndexRunResult, error)
	started chan struct{}
}

func newAFRounds() *afRounds { return &afRounds{started: make(chan struct{}, 64)} }

func (r *afRounds) next() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.calls
	r.calls++
	return n
}

func (r *afRounds) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *afRounds) CompileReady(ctx context.Context, _ func(string, int, int)) (aiflow.WikiCompileRoundResult, error) {
	n := r.next()
	r.started <- struct{}{}
	if n < len(r.wiki) && r.wiki[n] != nil {
		return r.wiki[n](ctx)
	}
	return aiflow.WikiCompileRoundResult{}, nil
}

func (r *afRounds) RunSemanticIndexing(ctx context.Context, _ aiflow.SemanticIndexStore, _ func(int)) (aiflow.SemanticIndexRunResult, error) {
	n := r.next()
	r.started <- struct{}{}
	if n < len(r.sem) && r.sem[n] != nil {
		return r.sem[n](ctx)
	}
	return aiflow.SemanticIndexRunResult{}, nil
}

const (
	afT0       = int64(1_700_000_000_000)
	afMinute   = int64(60_000)
	afLeaseMs  = int64(jobLeaseDurationMs)
	afWikiID   = "renderer-wiki-compile"
	afSemantic = "renderer-semantic-index"
)

type afRig struct {
	t       *testing.T
	env     *jobTestEnv
	wiki    *afRounds
	sem     *afRounds
	logMu   sync.Mutex
	appLogs []string
}

// newAFRig 在冻结基准库的副本上建调度器；clock 固定在 afT0，由测试推进。
func newAFRig(t *testing.T) *afRig {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	l := openWriteCopy(t, dir, "knowledge.db")
	rig := &afRig{t: t, env: newJobTestEnv(l), wiki: newAFRounds(), sem: newAFRounds()}
	rig.setClock(afT0)
	return rig
}

func (r *afRig) setClock(ms int64) {
	r.env.mu.Lock()
	defer r.env.mu.Unlock()
	r.env.clock = ms
}

// newClient 建一个执行器并让调度器把渲染进程任务交给它。embedding 为 false 时语义索引不启用。
func (r *afRig) newClient(embedding bool, wiki aiflow.WikiCompiler, sem aiflow.SemanticIndexer) *aiflow.BackgroundClient {
	r.t.Helper()
	raw := map[string]any{}
	if embedding {
		raw["aiModels"] = []any{map[string]any{
			"id": "embed", "provider": "custom", "apiProtocol": "openai",
			"apiUrl": "https://embed.example.com/v1", "model": "text-embedding-3-small", "apiKey": "sk-test",
			"capabilities": map[string]any{"chat": false, "embedding": true}, // 没有 embedding 能力的模型不算已配置
		}}
		raw["modelRouteDefaults"] = map[string]any{"embedding": "embed"}
	}
	client := &aiflow.BackgroundClient{
		Scheduler: aiflowJobScheduler{r.env.rt},
		Switches: func() aiflow.BackgroundSwitches {
			return aiflow.BackgroundSwitches{BackgroundTasksEnabled: true, WikiCompileEnabled: true}
		},
		Config:            afStaticConfig{aiflow.NormalizeAISettings(raw)},
		Wiki:              wiki,
		Semantic:          sem,
		HeartbeatInterval: 10 * time.Millisecond,
		LogError: func(scope, action, message string) {
			r.logMu.Lock()
			defer r.logMu.Unlock()
			r.appLogs = append(r.appLogs, scope+"/"+action+"/"+message)
		},
	}
	r.env.rt.sendJob = func(job map[string]any) {
		client.Handle(aiflow.BackgroundJob{ID: job["id"].(string), Kind: job["kind"].(string)})
	}
	r.t.Cleanup(func() { r.closeClient(client) })
	return client
}

// closeClient 关闭执行器并限时等待：Close 要等在途任务退出，任务一旦卡在调度器上就永远回不来，
// 限时之后让测试带着明确的原因失败，而不是等 go test 的整体超时把现场淹没。
func (r *afRig) closeClient(client *aiflow.BackgroundClient) {
	r.t.Helper()
	closed := make(chan struct{})
	go func() { client.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		r.t.Error("执行器的 Close 5 秒没有返回：在途任务没有退出？")
	}
}

// tick 让调度器领取一次到期任务。它在持有调度器的锁时把任务交给执行器的 Handle：Handle 一旦阻塞，
// tick 就回不来——限时等待，把死锁变成明确的失败而不是挂住整个测试。
func (r *afRig) tick() {
	r.t.Helper()
	returned := make(chan struct{})
	go func() { r.env.rt.tick(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		r.t.Fatal("调度器的 tick 没有返回：向执行器交付任务时死锁了？")
	}
}

func (r *afRig) job(id string) map[string]any {
	r.t.Helper()
	job, err := r.env.rt.dbGet(context.Background(), id)
	if err != nil || job == nil {
		r.t.Fatalf("读取任务 %s 失败：%v（%v）", id, err, job)
	}
	return job
}

func afWaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (r *afRig) appLogSnapshot() []string {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return append([]string{}, r.appLogs...)
}

// 从开关同步、排期、到期领取、执行到完成回报整条链路；两个任务先后到期，第一个完成回报时由调度器连带领取第二个
// ——完成回报与领取交付发生在同一把调度器锁的调用栈里，Handle 若阻塞就会死锁。
func TestAIFlowExecutorRunsRendererJobsThroughRealScheduler(t *testing.T) {
	rig := newAFRig(t)
	client := rig.newClient(true, rig.wiki, rig.sem)

	if err := client.SyncSettings(context.Background()); err != nil {
		t.Fatalf("同步开关失败：%v", err)
	}
	for id, wantNext := range map[string]int64{afSemantic: afT0 + afMinute, afWikiID: afT0 + 5*afMinute} {
		job := rig.job(id)
		if job["state"] != "scheduled" || asInt(job["nextRunAt"]) != wantNext {
			t.Fatalf("%s 应排在 %d、状态 scheduled，实际 %v / %v", id, wantNext, job["nextRunAt"], job["state"])
		}
	}

	done := afT0 + 6*afMinute
	rig.setClock(done)
	rig.tick()

	afWaitFor(t, "两个任务都执行并回报完成", func() bool {
		return rig.sem.callCount() == 1 && rig.wiki.callCount() == 1 &&
			rig.job(afSemantic)["state"] == "scheduled" && rig.job(afWikiID)["state"] == "scheduled"
	})
	for _, id := range []string{afSemantic, afWikiID} {
		job := rig.job(id)
		if asInt(job["lastSuccessAt"]) != done || asInt(job["attempt"]) != 0 || job["leaseOwner"] != nil || asInt(job["nextRunAt"]) != done+5*afMinute {
			t.Errorf("%s 完成回报后的状态不对：%v", id, job)
		}
	}
}

// 失败回报按退避重试：第一次 15 分钟，失败原因留在库里，成功后清零。
func TestAIFlowExecutorFailureBacksOffThenRecovers(t *testing.T) {
	rig := newAFRig(t)
	rig.wiki.wiki = []func(context.Context) (aiflow.WikiCompileRoundResult, error){
		func(context.Context) (aiflow.WikiCompileRoundResult, error) {
			return aiflow.WikiCompileRoundResult{}, errors.New("AI_NOT_CONFIGURED")
		},
	}
	client := rig.newClient(false, rig.wiki, rig.sem)
	if err := client.SyncSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rig.job(afSemantic)["state"]; got != "paused" {
		t.Fatalf("没有配置 embedding 时语义索引任务应为 paused，实际 %v", got)
	}

	first := afT0 + 6*afMinute
	rig.setClock(first)
	rig.tick()
	afWaitFor(t, "第一次失败回报", func() bool { return rig.job(afWikiID)["state"] == "retry_wait" })
	job := rig.job(afWikiID)
	if asInt(job["attempt"]) != 1 || job["lastError"] != "AI_NOT_CONFIGURED" || asInt(job["nextRunAt"]) != first+15*afMinute {
		t.Fatalf("失败回报后的状态不对：%v", job)
	}
	logs := rig.appLogSnapshot()
	if len(logs) != 1 || logs[0] != "wiki-compile/后台 Wiki 编译/AI_NOT_CONFIGURED" {
		t.Fatalf("失败应写一条应用日志，实际 %v", logs)
	}
	if rig.sem.callCount() != 0 {
		t.Fatalf("暂停的语义索引任务不应被执行")
	}

	retry := first + 15*afMinute
	rig.setClock(retry)
	rig.tick()
	afWaitFor(t, "重试成功", func() bool { return rig.job(afWikiID)["state"] == "scheduled" })
	job = rig.job(afWikiID)
	if asInt(job["attempt"]) != 0 || job["lastError"] != nil || asInt(job["lastSuccessAt"]) != retry {
		t.Fatalf("重试成功后应清零失败状态：%v", job)
	}
}

// 续租把租约往后推：假时钟走了 60 秒，下一次心跳就把 lease_until 推到「当前时刻 + 租约时长」。
func TestAIFlowExecutorHeartbeatRenewsLease(t *testing.T) {
	rig := newAFRig(t)
	release := make(chan struct{})
	rig.wiki.wiki = []func(context.Context) (aiflow.WikiCompileRoundResult, error){
		func(ctx context.Context) (aiflow.WikiCompileRoundResult, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return aiflow.WikiCompileRoundResult{}, nil
		},
	}
	client := rig.newClient(false, rig.wiki, rig.sem)
	if err := client.SyncSettings(context.Background()); err != nil {
		t.Fatal(err)
	}

	claimed := afT0 + 6*afMinute
	rig.setClock(claimed)
	rig.tick()
	afWaitFor(t, "任务开始执行", func() bool { return rig.wiki.callCount() == 1 })
	if job := rig.job(afWikiID); job["state"] != "running" || asInt(job["leaseUntil"]) != claimed+afLeaseMs {
		t.Fatalf("领取后应为 running、租约到 %d，实际 %v", claimed+afLeaseMs, job)
	}

	later := claimed + 60_000
	rig.setClock(later)
	afWaitFor(t, "心跳把租约推到新的时刻", func() bool { return asInt(rig.job(afWikiID)["leaseUntil"]) == later+afLeaseMs })

	close(release)
	afWaitFor(t, "执行完成并回报", func() bool { return rig.job(afWikiID)["state"] == "scheduled" })
	if got := asInt(rig.job(afWikiID)["lastSuccessAt"]); got != later {
		t.Fatalf("完成时刻应为 %d，实际 %d", later, got)
	}
}

// 执行器关闭不回报：任务留在 running，关闭之后也不再续租；租约过期后调度器重新领取，新的执行器接着做完。
func TestAIFlowExecutorCloseLeavesLeaseToExpireThenRestartRecovers(t *testing.T) {
	rig := newAFRig(t)
	rig.wiki.wiki = []func(context.Context) (aiflow.WikiCompileRoundResult, error){
		func(ctx context.Context) (aiflow.WikiCompileRoundResult, error) {
			<-ctx.Done() // 卡住，直到执行器关闭
			return aiflow.WikiCompileRoundResult{}, ctx.Err()
		},
	}
	first := rig.newClient(false, rig.wiki, rig.sem)
	if err := first.SyncSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	claimed := afT0 + 6*afMinute
	rig.setClock(claimed)
	rig.tick()
	afWaitFor(t, "任务开始执行", func() bool { return rig.wiki.callCount() == 1 })

	rig.closeClient(first)
	rig.setClock(claimed + 1_000) // 若关闭之后还在续租，租约会被推到这个时刻之后
	time.Sleep(60 * time.Millisecond)
	job := rig.job(afWikiID)
	if job["state"] != "running" || asInt(job["attempt"]) != 0 || job["lastError"] != nil || asInt(job["leaseUntil"]) != claimed+afLeaseMs {
		t.Fatalf("关闭后任务应原样留在 running、不记失败、不再续租：%v", job)
	}
	if logs := rig.appLogSnapshot(); len(logs) != 0 {
		t.Fatalf("被关闭打断不是编译失败，不应写应用日志：%v", logs)
	}

	// 租约过期：调度器重新领取，交给新的执行器
	rig.wiki.wiki = nil // 之后的调用都立即成功
	second := rig.newClient(false, rig.wiki, rig.sem)
	_ = second
	recovered := claimed + afLeaseMs + 1
	rig.setClock(recovered)
	rig.tick()
	afWaitFor(t, "新的执行器接着做完", func() bool {
		job := rig.job(afWikiID)
		return rig.wiki.callCount() == 2 && job["state"] == "scheduled" && asInt(job["lastSuccessAt"]) == recovered
	})
}
