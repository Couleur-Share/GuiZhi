package main

// 对应 packages/db/src/background-job.ts、main/services/background-jobs.ts 与 ipc/background-job.ipc.ts。
// 任务先落库再执行，租约过期后可被重新领取；需要浏览器环境的任务（wiki 编译、语义索引）只把
// 领取结果推给渲染进程，完成/失败/心跳仍回到这里校验租约持有者。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

const (
	jobTickInterval      = 30 * time.Second
	jobLeaseDurationMs   = 90_000
	rendererIntervalMin  = 5
	chBackgroundJobAvail = "backgroundJob:available"
	jobCols              = "id, kind, scope_id, state, payload_json, interval_minutes, next_run_at, attempt, lease_owner, lease_until, last_error, last_success_at, created_at, updated_at"
	maxInt64Literal      = "9223372036854775807"
	jobOwnerIDPrefixMain = "main-"
)

var jobRetryDelaysMs = []int64{15 * 60_000, 60 * 60_000, 6 * 60 * 60_000, 24 * 60 * 60_000}
var rendererJobKinds = []string{"wiki-compile", "semantic-index"}

// jobRetryDelay 对应 retryDelay(attempt)。
func jobRetryDelay(attempt int64) int64 {
	i := attempt
	if i < 0 {
		i = 0
	}
	if i > int64(len(jobRetryDelaysMs)-1) {
		i = int64(len(jobRetryDelaysMs) - 1)
	}
	return jobRetryDelaysMs[i]
}

// jobScheduleInput 对应 BackgroundJobScheduleInput。
type jobScheduleInput struct {
	Kind            string
	ScopeID         string
	PayloadJSON     string   // 已序列化的 payload；空表示 {}
	IntervalMinutes *float64 // nil → NULL
	NextRunAt       *int64   // nil → 现在
	Disabled        bool     // 对应 enabled === false
}

// jobHandlerResult 对应 BackgroundJobHandlerResult。
type jobHandlerResult struct {
	Pause     bool
	Message   string
	NextRunAt *int64
}

type jobHandler func(ctx context.Context, job map[string]any) (*jobHandlerResult, error)

// BackgroundJobRuntime 是主进程持有的统一后台任务调度器。
type BackgroundJobRuntime struct {
	l        *Library
	owner    string
	now      func() int64
	leaseMs  int64
	tickIvl  time.Duration
	sendJob  func(job map[string]any)
	mu       sync.Mutex // 保护下面的调度状态；不在持有它时等待数据库之外的东西
	handlers map[string]jobHandler
	order    []string // 注册顺序，对应 JS Map 的迭代顺序
	running  map[string]bool
	rendJob  string
	stopCh   chan struct{}
}

func newBackgroundJobRuntime(l *Library, send func(job map[string]any)) *BackgroundJobRuntime {
	return &BackgroundJobRuntime{
		l: l, owner: fmt.Sprintf("%s%d", jobOwnerIDPrefixMain, time.Now().UnixNano()), now: nowMs,
		leaseMs: jobLeaseDurationMs, tickIvl: jobTickInterval, sendJob: send,
		handlers: map[string]jobHandler{}, running: map[string]bool{},
	}
}

// ---- 数据库层（对应 BackgroundJobDB） ----

func toJob(r []any) map[string]any {
	payload := json.RawMessage("{}")
	if s, _ := r[4].(string); s != "" {
		if v, err := jsParse(s); err == nil && jsIsObject(v) {
			if _, isArr := v.(jsArr); !isArr {
				if out, ok := jsStringify(v); ok {
					payload = json.RawMessage(out)
				}
			}
		}
	}
	return map[string]any{
		"id": r[0], "kind": r[1], "scopeId": r[2], "state": r[3], "payload": payload,
		"intervalMinutes": r[5], "nextRunAt": r[6], "attempt": r[7], "leaseOwner": r[8], "leaseUntil": r[9],
		"lastError": r[10], "lastSuccessAt": r[11], "createdAt": r[12], "updatedAt": r[13],
	}
}

func (rt *BackgroundJobRuntime) dbList(ctx context.Context) ([]map[string]any, error) {
	rows, err := rt.l.db.Query(ctx, "SELECT "+jobCols+" FROM background_jobs ORDER BY COALESCE(next_run_at, "+maxInt64Literal+"), created_at")
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, toJob(r))
	}
	return out, nil
}

// dbGet 读取单条任务；调用方自行决定是否持有 writeMu。
func (rt *BackgroundJobRuntime) dbGet(ctx context.Context, id string) (map[string]any, error) {
	rows, err := rt.l.db.Query(ctx, "SELECT "+jobCols+" FROM background_jobs WHERE id = ?", id)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	return toJob(rows.Values[0]), nil
}

func (rt *BackgroundJobRuntime) dbUpsert(ctx context.Context, id string, in jobScheduleInput, now int64) (map[string]any, error) {
	rt.l.writeMu.Lock()
	defer rt.l.writeMu.Unlock()
	enabled := !in.Disabled
	state := "paused"
	var nextRunAt any
	if enabled {
		state = "scheduled"
		if in.NextRunAt != nil {
			nextRunAt = *in.NextRunAt
		} else {
			nextRunAt = now
		}
	}
	payload := in.PayloadJSON
	if payload == "" {
		payload = "{}"
	}
	var interval any
	if in.IntervalMinutes != nil {
		interval = jsNumberOrInt(*in.IntervalMinutes)
	}
	e := b2i(enabled)
	_, err := rt.l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: `INSERT INTO background_jobs
        (id, kind, scope_id, state, payload_json, interval_minutes, next_run_at,
         attempt, lease_owner, lease_until, last_error, last_success_at, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, 0, NULL, NULL, NULL, NULL, ?, ?)
       ON CONFLICT(kind, scope_id) DO UPDATE SET
         state = CASE WHEN background_jobs.state = 'running' AND ? = 1
                      THEN background_jobs.state ELSE excluded.state END,
         payload_json = excluded.payload_json,
         interval_minutes = excluded.interval_minutes,
         next_run_at = CASE WHEN background_jobs.state = 'running' AND ? = 1
                            THEN background_jobs.next_run_at ELSE excluded.next_run_at END,
         lease_owner = CASE WHEN ? = 1 THEN background_jobs.lease_owner ELSE NULL END,
         lease_until = CASE WHEN ? = 1 THEN background_jobs.lease_until ELSE NULL END,
         updated_at = excluded.updated_at`,
		Args: []any{id, in.Kind, in.ScopeID, state, payload, interval, nextRunAt, now, now, e, e, e, e},
	}})
	if err != nil {
		return nil, err
	}
	rows, err := rt.l.db.Query(ctx, "SELECT "+jobCols+" FROM background_jobs WHERE kind = ? AND scope_id = ?", in.Kind, in.ScopeID)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	return toJob(rows.Values[0]), nil
}

// jsNumberOrInt：整数以 int64 绑定，其余按 double（与 node-sqlite3-wasm 单参数绑定一致）。
func jsNumberOrInt(f float64) any {
	if f == float64(int64(f)) {
		return int64(f)
	}
	return f
}

func (rt *BackgroundJobRuntime) dbClaimDue(ctx context.Context, kinds []string, now int64) (map[string]any, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	rt.l.writeMu.Lock()
	defer rt.l.writeMu.Unlock()
	args := make([]any, 0, len(kinds)+2)
	for _, k := range kinds {
		args = append(args, k)
	}
	args = append(args, now, now)
	rows, err := rt.l.db.Query(ctx, `SELECT id FROM background_jobs
         WHERE kind IN (`+strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")+`)
           AND ((state IN ('scheduled','retry_wait') AND next_run_at <= ?)
             OR (state = 'running' AND lease_until <= ?))
         ORDER BY COALESCE(next_run_at, 0), created_at
         LIMIT 1`, args...)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	id := rows.Values[0][0].(string)
	res, err := rt.l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: `UPDATE background_jobs
         SET state = 'running', lease_owner = ?, lease_until = ?, updated_at = ?
         WHERE id = ? AND ((state IN ('scheduled','retry_wait') AND next_run_at <= ?)
           OR (state = 'running' AND lease_until <= ?))`,
		Args: []any{rt.owner, now + rt.leaseMs, now, id, now, now},
	}})
	if err != nil || len(res) == 0 || res[0].Changes != 1 {
		return nil, err
	}
	return rt.dbGet(ctx, id)
}

func (rt *BackgroundJobRuntime) changed(ctx context.Context, sql string, args ...any) (bool, error) {
	res, err := rt.l.db.Transaction(ctx, []sqlite.Statement{{SQL: sql, Args: args}})
	if err != nil {
		return false, err
	}
	return len(res) > 0 && res[0].Changes == 1, nil
}

func (rt *BackgroundJobRuntime) dbComplete(ctx context.Context, id string, now int64, nextOverride *int64) (bool, error) {
	rt.l.writeMu.Lock()
	defer rt.l.writeMu.Unlock()
	rows, err := rt.l.db.Query(ctx, "SELECT interval_minutes FROM background_jobs WHERE id = ? AND state = 'running' AND lease_owner = ?", id, rt.owner)
	if err != nil || len(rows.Values) == 0 {
		return false, err
	}
	interval := 0.0 // Number(null) = 0
	switch n := rows.Values[0][0].(type) {
	case int64:
		interval = float64(n)
	case float64:
		interval = n
	}
	recurring := interval > 0
	state := "succeeded"
	var next any
	if recurring {
		state = "scheduled"
		if nextOverride != nil {
			next = *nextOverride
		} else {
			next = now + int64(interval*60_000)
		}
	}
	return rt.changed(ctx, `UPDATE background_jobs SET state = ?, next_run_at = ?, attempt = 0,
       lease_owner = NULL, lease_until = NULL, last_error = NULL,
       last_success_at = ?, updated_at = ? WHERE id = ? AND lease_owner = ?`,
		state, next, now, now, id, rt.owner)
}

func (rt *BackgroundJobRuntime) dbRenew(ctx context.Context, id string, now int64) (bool, error) {
	rt.l.writeMu.Lock()
	defer rt.l.writeMu.Unlock()
	return rt.changed(ctx, `UPDATE background_jobs SET lease_until = ?, updated_at = ?
       WHERE id = ? AND state = 'running' AND lease_owner = ?`, now+rt.leaseMs, now, id, rt.owner)
}

// dbFail：delay 为 nil 表示暂停（不再重试）。
func (rt *BackgroundJobRuntime) dbFail(ctx context.Context, id, msg string, delay *int64, now int64) (bool, error) {
	rt.l.writeMu.Lock()
	defer rt.l.writeMu.Unlock()
	state := "paused"
	var next any
	if delay != nil {
		state = "retry_wait"
		next = now + *delay
	}
	return rt.changed(ctx, `UPDATE background_jobs SET state = ?, next_run_at = ?, attempt = attempt + 1,
       lease_owner = NULL, lease_until = NULL, last_error = ?, updated_at = ?
       WHERE id = ? AND state = 'running' AND lease_owner = ?`, state, next, msg, now, id, rt.owner)
}

func (rt *BackgroundJobRuntime) dbSetPaused(ctx context.Context, id string, paused bool, now int64) (bool, error) {
	rt.l.writeMu.Lock()
	defer rt.l.writeMu.Unlock()
	state := "scheduled"
	var next any = now
	if paused {
		state, next = "paused", nil
	}
	return rt.changed(ctx, `UPDATE background_jobs SET state = ?, next_run_at = ?, lease_owner = NULL,
       lease_until = NULL, updated_at = ? WHERE id = ?`, state, next, now, id)
}

// ---- 调度层（对应 BackgroundJobRuntime） ----

func (rt *BackgroundJobRuntime) Start() {
	rt.mu.Lock()
	if rt.stopCh != nil {
		rt.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	rt.stopCh = stop
	rt.mu.Unlock()
	go rt.tick()
	go func() {
		t := time.NewTicker(rt.tickIvl)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				rt.tick()
			case <-stop:
				return
			}
		}
	}()
}

func (rt *BackgroundJobRuntime) Stop() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.stopCh != nil {
		close(rt.stopCh)
		rt.stopCh = nil
	}
}

func (rt *BackgroundJobRuntime) RegisterHandler(kind string, h jobHandler) error {
	for _, k := range rendererJobKinds {
		if k == kind {
			return fmt.Errorf("%s 必须由 Renderer 执行，不能注册主进程 handler", kind)
		}
	}
	rt.mu.Lock()
	if _, exists := rt.handlers[kind]; !exists {
		rt.order = append(rt.order, kind)
	}
	rt.handlers[kind] = h
	rt.mu.Unlock()
	return nil
}

func (rt *BackgroundJobRuntime) Schedule(ctx context.Context, id string, in jobScheduleInput) (map[string]any, error) {
	job, err := rt.dbUpsert(ctx, id, in, rt.now())
	if err == nil {
		rt.tick()
	}
	return job, err
}

func (rt *BackgroundJobRuntime) Unschedule(ctx context.Context, kind, scopeID string) (bool, error) {
	rt.l.writeMu.Lock()
	defer rt.l.writeMu.Unlock()
	res, err := rt.l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: "DELETE FROM background_jobs WHERE kind = ? AND scope_id = ? AND state <> 'running'", Args: []any{kind, scopeID},
	}})
	return err == nil && len(res) > 0 && res[0].Changes > 0, err
}

func (rt *BackgroundJobRuntime) List(ctx context.Context) ([]map[string]any, error) {
	return rt.dbList(ctx)
}

func (rt *BackgroundJobRuntime) SyncRendererJobs(ctx context.Context, wikiEnabled, semanticEnabled bool) ([]map[string]any, error) {
	now := rt.now()
	current, err := rt.dbList(ctx)
	if err != nil {
		return nil, err
	}
	find := func(kind string) map[string]any {
		for _, j := range current {
			if j["kind"] == kind {
				return j
			}
		}
		return nil
	}
	keep := func(j map[string]any, fallback int64) *int64 {
		if j != nil && (j["state"] == "scheduled" || j["state"] == "retry_wait") {
			// nextRunAt 在 scheduled/retry_wait 时总有值；若为 NULL 则 JS 里 undefined ?? 现在 → 这里同样落到现在
			if v, ok := j["nextRunAt"].(int64); ok {
				return &v
			}
			return nil
		}
		return &fallback
	}
	interval := float64(rendererIntervalMin)
	if _, err := rt.dbUpsert(ctx, "renderer-wiki-compile", jobScheduleInput{
		Kind: "wiki-compile", IntervalMinutes: &interval, NextRunAt: keep(find("wiki-compile"), now+rendererIntervalMin*60_000), Disabled: !wikiEnabled,
	}, now); err != nil {
		return nil, err
	}
	if _, err := rt.dbUpsert(ctx, "renderer-semantic-index", jobScheduleInput{
		Kind: "semantic-index", IntervalMinutes: &interval, NextRunAt: keep(find("semantic-index"), now+60_000), Disabled: !semanticEnabled,
	}, now); err != nil {
		return nil, err
	}
	rt.tick()
	return rt.dbList(ctx)
}

func (rt *BackgroundJobRuntime) clearRenderer(id string) {
	rt.mu.Lock()
	if rt.rendJob == id {
		rt.rendJob = ""
	}
	rt.mu.Unlock()
}

func (rt *BackgroundJobRuntime) RenewRendererJob(ctx context.Context, id string) (bool, error) {
	return rt.dbRenew(ctx, id, rt.now())
}

func (rt *BackgroundJobRuntime) CompleteRendererJob(ctx context.Context, id string) (bool, error) {
	ok, err := rt.dbComplete(ctx, id, rt.now(), nil)
	rt.clearRenderer(id)
	rt.tick()
	return ok, err
}

func (rt *BackgroundJobRuntime) FailRendererJob(ctx context.Context, id, msg string, pause bool) (bool, error) {
	job, err := rt.dbGet(ctx, id)
	if err != nil || job == nil {
		return false, err
	}
	var delay *int64
	if !pause {
		d := jobRetryDelay(asInt(job["attempt"]))
		delay = &d
	}
	ok, err := rt.dbFail(ctx, id, msg, delay, rt.now())
	rt.clearRenderer(id)
	rt.tick()
	return ok, err
}

func (rt *BackgroundJobRuntime) SetPaused(ctx context.Context, id string, paused bool) (bool, error) {
	rt.clearRenderer(id)
	ok, err := rt.dbSetPaused(ctx, id, paused, rt.now())
	if !paused {
		rt.tick()
	}
	return ok, err
}

// tick 领取到期任务：主进程任务各自在 goroutine 里跑，渲染进程任务推给窗口。
func (rt *BackgroundJobRuntime) tick() {
	ctx := context.Background()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, kind := range rt.order {
		handler := rt.handlers[kind]
		if rt.running[kind] {
			continue
		}
		job, err := rt.dbClaimDue(ctx, []string{kind}, rt.now())
		if err != nil {
			log.Printf("[background-jobs] 领取 %s 失败：%v", kind, err)
			continue
		}
		if job == nil {
			continue
		}
		rt.running[kind] = true
		go rt.runMainJob(ctx, job, handler)
	}
	if rt.rendJob != "" {
		active, _ := rt.dbGet(ctx, rt.rendJob)
		if active == nil || active["state"] != "running" || active["leaseOwner"] != rt.owner || asInt(active["leaseUntil"]) <= rt.now() {
			rt.rendJob = ""
		}
	}
	if rt.rendJob == "" {
		job, err := rt.dbClaimDue(ctx, rendererJobKinds, rt.now())
		if err != nil {
			log.Printf("[background-jobs] 领取渲染进程任务失败：%v", err)
		} else if job != nil {
			rt.rendJob = job["id"].(string)
			if rt.sendJob != nil {
				rt.sendJob(job)
			}
		}
	}
}

func (rt *BackgroundJobRuntime) runMainJob(ctx context.Context, job map[string]any, handler jobHandler) {
	id, kind := job["id"].(string), job["kind"].(string)
	result, err := safeRunJob(ctx, handler, job)
	switch {
	case err != nil:
		attempt := asInt(job["attempt"])
		if cur, _ := rt.dbGet(ctx, id); cur != nil {
			attempt = asInt(cur["attempt"])
		}
		d := jobRetryDelay(attempt)
		_, _ = rt.dbFail(ctx, id, err.Error(), &d, rt.now())
	case result != nil && result.Pause:
		msg := result.Message
		if msg == "" {
			msg = "任务已暂停"
		}
		_, _ = rt.dbFail(ctx, id, msg, nil, rt.now())
	default:
		var next *int64
		if result != nil {
			next = result.NextRunAt
		}
		_, _ = rt.dbComplete(ctx, id, rt.now(), next)
	}
	rt.mu.Lock()
	delete(rt.running, kind)
	rt.mu.Unlock()
	rt.tick()
}

func safeRunJob(ctx context.Context, h jobHandler, job map[string]any) (res *jobHandlerResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return h(ctx, job)
}

// ---- IPC ----

func requireJobID(v any) (string, error) {
	s, ok := v.(string)
	if !ok || jsTrim(s) == "" {
		return "", errors.New("后台任务 id 不能为空")
	}
	return s, nil
}

func (l *Library) registerBackgroundJobs(b *Bridge) {
	rt := l.jobs
	b.handlers["backgroundJob:list"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		return rt.List(ctx)
	}
	b.handlers["backgroundJob:syncRenderer"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		in := jsArg(a, 0)
		wiki, ok1 := jsGet(in, "wikiEnabled").(bool)
		sem, ok2 := jsGet(in, "semanticEnabled").(bool)
		if !vTruthy(in) || !ok1 || !ok2 {
			return nil, errors.New("后台任务同步参数不合法")
		}
		return rt.SyncRendererJobs(ctx, wiki, sem)
	}
	idCall := func(name string, fn func(ctx context.Context, id string) (bool, error)) {
		b.handlers[name] = func(ctx context.Context, a []json.RawMessage) (any, error) {
			id, err := requireJobID(jsArg(a, 0))
			if err != nil {
				return nil, err
			}
			return fn(ctx, id)
		}
	}
	idCall("backgroundJob:renew", rt.RenewRendererJob)
	idCall("backgroundJob:complete", rt.CompleteRendererJob)
	idCall("backgroundJob:pause", func(ctx context.Context, id string) (bool, error) { return rt.SetPaused(ctx, id, true) })
	idCall("backgroundJob:resume", func(ctx context.Context, id string) (bool, error) { return rt.SetPaused(ctx, id, false) })
	b.handlers["backgroundJob:fail"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		in := jsArg(a, 0)
		msg, ok := jsGet(in, "error").(string)
		if !vTruthy(in) || !ok {
			return nil, errors.New("后台任务失败参数不合法")
		}
		id, err := requireJobID(jsGet(in, "id"))
		if err != nil {
			return nil, err
		}
		return rt.FailRendererJob(ctx, id, msg, jsStrictEqual(jsGet(in, "pause"), true))
	}
}

// pushMain 向主窗口推送一次性事件（对应 sendToMainWindow）；窗口未创建时静默丢弃，与 TS 一致。
func (l *Library) pushMain(channel string, payload any) {
	l.pushMu.RLock()
	f := l.push
	l.pushMu.RUnlock()
	if f != nil {
		f(channel, payload)
	}
}

func (l *Library) setPush(f func(channel string, payload any)) {
	l.pushMu.Lock()
	l.push = f
	l.pushMu.Unlock()
}
