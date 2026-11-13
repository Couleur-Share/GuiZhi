package main

// 主题阅读生成管线宿主：在 goja 里运行 TS 原文件打包产物 themed_pipeline.js
// （v3-pipeline / design / v3-research / v3-runtime-repair，见 apps/desktop-go/jsgen/build-pipeline.mts）。
//
// 为什么内嵌 JS 而不手写移植：管线含大量提示词、分块、截断重试、章节复用与校验分支，
// 与 Electron 版逐字一致是行为等价的唯一可验证办法（差分基线直接回放同一份脚本化模型）。
// 模型请求、搜索、网页抓取、用量记录、版本保存全部留在 Go：JS 只通过宿主桥调用。
//
// 并发模型：一个 goroutine 独占 goja 运行时（事件循环），宿主异步调用在各自的 goroutine 完成后
// 把 resolve / reject 投回循环线程；同步钩子（保存版本、记录阶段）直接在循环线程里执行。
// 同一时刻只运行一次管线（runMu），与 Electron 版「队列逐个执行」一致。

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

//go:embed themed_pipeline.js
var themedPipelineJS string

// pipelineHooks 是 ReadingV3Hooks 的 Go 形态；返回的 error 会作为 JS 异常抛进管线。
type pipelineHooks struct {
	Checkpoint     func(versionJSON string) error
	Stage          func(stage string, done, total int) error
	Request        func(kind string) error
	DesignProgress func(attempt, chars int) error
}

// pipelineServices 是管线可调用的宿主能力。
type pipelineServices struct {
	Chat    aiChatFunc
	Search  func(ctx context.Context, query, scenario string) ([]readingSearchResult, error)
	Capture func(ctx context.Context, req webCaptureRequest) (webCaptureResult, error)
	Usage   func(scenario, model string, prompt, completion *float64, failed bool)
	// Verify 在隔离环境运行已生成的页面，返回故障说明（空串 = 通过）。
	Verify func(ctx context.Context, versionJSON string) (string, error)
	// Ordered 仅供对拍测试：宿主调用严格按 JS 发起顺序串行执行、按同一顺序投回结果。
	// 管线用 readingPool 并发抓取来源，一个来源限流会中止其余来源，结果取决于「谁先完成」。
	// TS 版里测试替身是立即完成的 Promise，完成顺序由微任务队列决定、天然确定；Go 每个调用在独立 goroutine，
	// 完成顺序随机。生产环境谁先返回本来就取决于网络，不需要也不应该串行；测试开启它才能与 TS 基线逐项对拍。
	Ordered bool
}

type pipelineError struct {
	Message string
	Aborted bool
}

func (e *pipelineError) Error() string { return e.Message }

type pipelineRun struct {
	ctx      context.Context
	hooks    pipelineHooks
	services pipelineServices
	mu       sync.Mutex
	cancels  map[int64]context.CancelFunc
	hookErr  error
	seq      atomic.Int64 // 异步宿主调用的序号发生器，见 pipelineCallSeq
	lastDone chan struct{} // Ordered 模式下上一个调用的完成信号（mu 保护）
}

type pipelineCallSeqKey struct{}

// pipelineCallSeq 返回异步宿主调用的序号（按 JS 发起顺序，从 1 起；非管线调用返回 0）。
// 每个异步宿主调用在独立 goroutine 里执行，Promise.all 同时发起的几次调用谁先进入服务函数由调度器决定；
// TS 版单线程，服务被调用的先后天然等于 JS 发起的先后。需要还原这一先后的调用方（测试记录、诊断日志）按序号排序即可。
func pipelineCallSeq(ctx context.Context) int64 {
	n, _ := ctx.Value(pipelineCallSeqKey{}).(int64)
	return n
}

type pipelineEngine struct {
	tasks chan func()
	rt    *goja.Runtime
	gzp   *goja.Object
	runMu sync.Mutex
	runID atomic.Int64
	cur   *pipelineRun // 仅在持有 runMu 的 Run 期间有效
}

var (
	pipelineOnce sync.Once
	pipelineInst *pipelineEngine
	pipelineErr  error
)

func getPipelineEngine() (*pipelineEngine, error) {
	pipelineOnce.Do(func() { pipelineInst, pipelineErr = newPipelineEngine() })
	return pipelineInst, pipelineErr
}

func (e *pipelineEngine) post(f func()) { e.tasks <- f }

// jsError 构造 JS Error 对象（message 与 Go error 一致，保证管线里的 /429|限流/ 之类匹配照常工作）。
func (e *pipelineEngine) jsError(msg string) *goja.Object {
	obj, err := e.rt.New(e.rt.Get("Error"), e.rt.ToValue(msg))
	if err != nil {
		return e.rt.NewGoError(errors.New(msg))
	}
	return obj
}

func newPipelineEngine() (*pipelineEngine, error) {
	e := &pipelineEngine{tasks: make(chan func(), 256), rt: goja.New()}
	rt := e.rt
	set := func(name string, v any) {
		if err := rt.Set(name, v); err != nil {
			panic(err)
		}
	}
	// 与渲染引擎相同的最小环境：node 内置模块返回空对象，linkedom 的可选依赖 canvas 按 Node 行为抛 MODULE_NOT_FOUND。
	set("require", func(call goja.FunctionCall) goja.Value {
		if name := call.Argument(0).String(); name == "canvas" {
			panic(rt.NewGoError(fmt.Errorf("Cannot find module '%s'", name)))
		}
		return rt.NewObject()
	})
	set("atob", func(s string) (string, error) { return jsAtob(s) })
	set("btoa", func(s string) (string, error) { return jsBtoa(s) })
	set("__gzHash", jsHash)
	set("__gzCompile", jsCompileCheck)
	set("__gzURLParse", jsURLParse)
	set("__gzLibrary", readingLibrary)
	set("__gzSync", e.hostSync)
	set("__gzAsync", e.hostAsync)
	set("__gzCancel", func(id int64) {
		if r := e.cur; r != nil {
			r.mu.Lock()
			if c := r.cancels[id]; c != nil {
				c()
			}
			r.mu.Unlock()
		}
	})
	if _, err := rt.RunString(themedPipelineJS); err != nil {
		return nil, fmt.Errorf("加载主题阅读生成管线失败：%w", err)
	}
	gzp, ok := rt.Get("GZP").(*goja.Object)
	if !ok {
		return nil, errors.New("主题阅读生成管线缺少入口对象")
	}
	e.gzp = gzp
	go func() {
		for f := range e.tasks {
			f()
		}
	}()
	return e, nil
}

// hostSync 处理同步钩子；钩子返回错误时抛成 JS 异常，管线按原样失败。
func (e *pipelineEngine) hostSync(kind, args string) string {
	run := e.cur
	if run == nil {
		panic(e.jsError("管线未在运行"))
	}
	var err error
	switch kind {
	case "checkpoint":
		var p struct{ Version string }
		_ = json.Unmarshal([]byte(args), &p)
		err = run.hooks.Checkpoint(p.Version)
	case "stage":
		var p struct {
			Stage       string
			Done, Total int
		}
		_ = json.Unmarshal([]byte(args), &p)
		err = run.hooks.Stage(p.Stage, p.Done, p.Total)
	case "request":
		var p struct{ Kind string }
		_ = json.Unmarshal([]byte(args), &p)
		err = run.hooks.Request(p.Kind)
	case "designProgress":
		var p struct{ Attempt, Chars int }
		_ = json.Unmarshal([]byte(args), &p)
		if run.hooks.DesignProgress != nil {
			err = run.hooks.DesignProgress(p.Attempt, p.Chars)
		}
	case "usage":
		var p struct {
			Scenario, Model  string
			PromptTokens     *float64
			CompletionTokens *float64
			Failed           bool
		}
		_ = json.Unmarshal([]byte(args), &p)
		if run.services.Usage != nil {
			run.services.Usage(p.Scenario, p.Model, p.PromptTokens, p.CompletionTokens, p.Failed)
		}
	default:
		err = fmt.Errorf("未知宿主同步调用：%s", kind)
	}
	if err != nil {
		panic(e.jsError(err.Error()))
	}
	return ""
}

// hostAsync 处理异步宿主调用：在独立 goroutine 执行，结果投回循环线程。
func (e *pipelineEngine) hostAsync(kind, args string, id int64) goja.Value {
	run := e.cur
	promise, resolve, reject := e.rt.NewPromise()
	if run == nil {
		_ = reject(e.jsError("管线未在运行"))
		return e.rt.ToValue(promise)
	}
	cctx, cancel := context.WithCancel(run.ctx)
	// 序号必须在循环线程（此处，JS 调用的当下）取，不能放进 goroutine 里取。
	cctx = context.WithValue(cctx, pipelineCallSeqKey{}, run.seq.Add(1))
	run.mu.Lock()
	run.cancels[id] = cancel
	// Ordered（仅测试）：本调用等上一个调用的结果投回后才开始，自己的结果投回后放行下一个。
	var prev <-chan struct{}
	var mine chan struct{}
	if run.services.Ordered {
		mine = make(chan struct{})
		prev, run.lastDone = run.lastDone, mine
	}
	run.mu.Unlock()
	go func() {
		if prev != nil {
			<-prev
		}
		out, err := e.serve(cctx, run, kind, args, id)
		cancel()
		e.post(func() {
			if mine != nil {
				defer close(mine)
			}
			run.mu.Lock()
			delete(run.cancels, id)
			run.mu.Unlock()
			if err != nil {
				_ = reject(e.jsError(err.Error()))
			} else {
				_ = resolve(out)
			}
		})
	}()
	return e.rt.ToValue(promise)
}

func (e *pipelineEngine) callback(id int64, kind string, value any) {
	e.post(func() {
		fn, ok := goja.AssertFunction(e.gzp.Get("callback"))
		if !ok {
			return
		}
		_, _ = fn(e.gzp, e.rt.ToValue(id), e.rt.ToValue(kind), e.rt.ToValue(value))
	})
}

func (e *pipelineEngine) serve(ctx context.Context, run *pipelineRun, kind, args string, id int64) (string, error) {
	svc := run.services
	switch kind {
	case "chat":
		var p struct {
			Config struct {
				Provider, ApiProtocol, ApiKey, ApiUrl, Model string
			}
			Messages []struct{ Role, Content string }
			Options  struct {
				Temperature    *float64
				MaxTokens      int
				ResponseFormat string
				TimeoutMs      float64
				Stream         bool
				Progress       bool
				Delta          bool
			}
		}
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			return "", err
		}
		cfg := aiClientConfig{Provider: p.Config.Provider, APIProtocol: p.Config.ApiProtocol, APIKey: p.Config.ApiKey, APIURL: p.Config.ApiUrl, Model: p.Config.Model}
		msgs := make([]aiChatMessage, len(p.Messages))
		for i, m := range p.Messages {
			msgs[i] = aiChatMessage{Role: m.Role, Content: m.Content}
		}
		opts := aiChatOptions{Temperature: p.Options.Temperature, MaxTokens: p.Options.MaxTokens, ResponseFormat: p.Options.ResponseFormat,
			Timeout: time.Duration(p.Options.TimeoutMs * float64(time.Millisecond)), Stream: p.Options.Stream}
		if p.Options.Progress {
			opts.OnProgress = func(n int) { e.callback(id, "progress", n) }
		}
		if p.Options.Delta {
			opts.OnDelta = func(s string) { e.callback(id, "delta", s) }
		}
		res, err := svc.Chat(ctx, cfg, msgs, opts)
		if err != nil {
			return "", err
		}
		out := map[string]any{"content": res.Content}
		if res.FinishReason != "" {
			out["finishReason"] = res.FinishReason
		}
		if res.Usage != nil {
			out["usage"] = map[string]any{"promptTokens": res.Usage.PromptTokens, "completionTokens": res.Usage.CompletionTokens}
		}
		b, err := json.Marshal(out)
		return string(b), err
	case "search":
		var p struct{ Query, Scenario string }
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			return "", err
		}
		list, err := svc.Search(ctx, p.Query, p.Scenario)
		if err != nil {
			return "", err
		}
		if list == nil {
			list = []readingSearchResult{}
		}
		b, err := json.Marshal(list)
		return string(b), err
	case "capture":
		var p struct{ URL, Purpose, TaskID string }
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			return "", err
		}
		res, err := svc.Capture(ctx, webCaptureRequest{URL: p.URL, Purpose: p.Purpose, TaskID: p.TaskID})
		if err != nil {
			return "", err
		}
		if res.release != nil {
			defer res.release()
		}
		b, err := json.Marshal(res)
		return string(b), err
	case "verify":
		var p struct{ Version string }
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			return "", err
		}
		if svc.Verify == nil {
			return "null", nil
		}
		msg, err := svc.Verify(ctx, p.Version)
		if err != nil {
			return "", err
		}
		if msg == "" {
			return "null", nil
		}
		b, _ := json.Marshal(msg)
		return string(b), nil
	}
	return "", fmt.Errorf("未知宿主异步调用：%s", kind)
}

// Run 运行一次管线操作（op：design / repair），返回结束时的版本 JSON。
// ctx 取消 → JS 侧 AbortController.abort(原因)；管线随即抛出该原因（与 TS 的 controller.abort(reason) 一致）。
func (e *pipelineEngine) Run(ctx context.Context, op, argsJSON string, hooks pipelineHooks, svc pipelineServices) (string, error) {
	e.runMu.Lock()
	defer e.runMu.Unlock()
	run := &pipelineRun{ctx: ctx, hooks: hooks, services: svc, cancels: map[int64]context.CancelFunc{}}
	e.cur = run
	defer func() { e.cur = nil }()
	id := e.runID.Add(1)
	type outcome struct {
		result string
		err    error
	}
	done := make(chan outcome, 1)
	e.post(func() {
		start, ok := goja.AssertFunction(e.gzp.Get("start"))
		if !ok {
			done <- outcome{err: errors.New("管线缺少 start")}
			return
		}
		cb := func(call goja.FunctionCall) goja.Value {
			if msg := call.Argument(1); msg != nil && !goja.IsNull(msg) && !goja.IsUndefined(msg) {
				var p struct {
					Message string
					Aborted bool
				}
				_ = json.Unmarshal([]byte(msg.String()), &p)
				done <- outcome{err: &pipelineError{Message: p.Message, Aborted: p.Aborted}}
			} else {
				done <- outcome{result: call.Argument(0).String()}
			}
			return goja.Undefined()
		}
		if _, err := start(e.gzp, e.rt.ToValue(id), e.rt.ToValue(op), e.rt.ToValue(argsJSON), e.rt.ToValue(cb)); err != nil {
			done <- outcome{err: err}
		}
	})
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			reason := "已取消"
			if c := context.Cause(ctx); c != nil {
				reason = c.Error()
			}
			e.post(func() {
				if fn, ok := goja.AssertFunction(e.gzp.Get("abort")); ok {
					_, _ = fn(e.gzp, e.rt.ToValue(id), e.rt.ToValue(reason))
				}
			})
		case <-stop:
		}
	}()
	out := <-done
	// 管线结束后取消仍在途的宿主调用（例如被放弃的并行请求）。
	run.mu.Lock()
	for _, c := range run.cancels {
		c()
	}
	run.mu.Unlock()
	return out.result, out.err
}

func isPipelineAborted(err error) bool {
	var pe *pipelineError
	return errors.As(err, &pe) && pe.Aborted
}

func describePipelineError(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
