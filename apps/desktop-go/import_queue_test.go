package main

// 导入队列差分测试的 Go 侧运行环境：与 make-golden.mts 的 importHandlers 一一对应。
// 抽取被换成按输入文本决定行为的固定替身，其余（校验、排队、去重、入库、事件、清理）走真实实现。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

type impTestEnv struct {
	l      *Library
	b      *Bridge
	mu     sync.Mutex
	holds  map[string]chan struct{}
	events []map[string]any
}

func newImpTestEnv(t *testing.T, l *Library, b *Bridge) *impTestEnv {
	e := &impTestEnv{l: l, b: b, holds: map[string]chan struct{}{}}
	l.extractOverride = e.extract
	l.setPush(func(channel string, payload any) {
		if channel != chImportChanged {
			return
		}
		snap := toGeneric(t, payload).(map[string]any)
		e.mu.Lock()
		e.events = append(e.events, snap)
		e.mu.Unlock()
	})
	l.registerImport(b)
	l.registerImportList(b)
	return e
}

func toGeneric(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// scrubStageStats 把 stageStats 数组换成阶段名列表（耗时是观测值），对应 TS 的 scrubTasks。
func scrubStageStats(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = scrubStageStats(x[i])
		}
		return x
	case map[string]any:
		for k, val := range x {
			if arr, ok := val.([]any); ok && k == "stageStats" {
				names := make([]any, len(arr))
				for i, s := range arr {
					if m, ok := s.(map[string]any); ok {
						names[i] = m["stage"]
					}
				}
				x[k] = names
				continue
			}
			x[k] = scrubStageStats(val)
		}
		return x
	}
	return v
}

func (e *impTestEnv) extract(ctx context.Context, task map[string]any, onStage func(string)) (*extractedContent, error) {
	input := task["sourceInput"].(string)
	kind, _ := task["sourceKind"].(string)
	if strings.HasPrefix(input, "hold:") {
		ch := make(chan struct{})
		e.mu.Lock()
		e.holds[input] = ch
		e.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			e.mu.Lock()
			if e.holds[input] == ch {
				delete(e.holds, input)
			}
			e.mu.Unlock()
			return nil, errors.New("aborted")
		}
	}
	if strings.HasPrefix(input, "stage:") {
		onStage("web-preparing")
		onStage("web-verifying")
	}
	if strings.HasPrefix(input, "fail:") {
		return nil, errors.New(strings.TrimPrefix(input, "fail:"))
	}
	if strings.Contains(input, "/captcha") {
		return nil, errors.New("网页要求完成验证码")
	}
	if strings.Contains(input, "/boom") {
		return nil, errors.New("抓取失败 boom")
	}
	c := &extractedContent{Title: "标题 " + input, ItemType: "note"}
	switch {
	case strings.HasPrefix(input, "same:"):
		c.Title, c.Content = "共同标题", "共同内容正文"
	case strings.HasPrefix(input, "short:"):
		c.Content = "短"
	default:
		c.Content = strings.Repeat(input+" 的正文内容，足够长以通过复核。", 3)
	}
	if strings.HasPrefix(input, "img:") {
		c.ItemType = "image"
	}
	if kind == "url" {
		c.SourceURI = &input
	}
	if strings.HasPrefix(input, "tr:") {
		tr := "转写 " + input
		c.Transcript = &tr
	}
	if strings.HasPrefix(input, "degrade:") {
		c.DegradedReason = strings.TrimPrefix(input, "degrade:")
	}
	if strings.HasPrefix(input, "warn:") {
		c.WarningReason = strings.TrimPrefix(input, "warn:")
	}
	return c, nil
}

// settle 等到队列静止：所有在跑的任务要么结束，要么阻塞在 hold 上，且没有可起跑的待处理任务。
//
// 上限 60 秒且超时必须失败：CI 的 Windows 机器慢时整套写入对拍可达 65 秒，旧的 10 秒上限到期后静默返回，
// 随后快照到「还没跑完」的中间态，报出看似行为不一致、实为等待不足的误导性差异。
func (e *impTestEnv) settle(t *testing.T) {
	t.Helper()
	q := e.l.imports
	deadline := time.Now().Add(60 * time.Second)
	stable := 0
	var running, pending, held, conc int
	var paused bool
	for time.Now().Before(deadline) {
		q.mu.Lock()
		running, pending, paused, conc = len(q.running), len(q.pending), q.paused, q.concurrency
		q.mu.Unlock()
		e.mu.Lock()
		held = len(e.holds)
		e.mu.Unlock()
		if running == held && (pending == 0 || paused || running >= conc) {
			stable++
		} else {
			stable = 0
		}
		if stable >= 5 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("导入队列 60 秒内未静止：running=%d held=%d pending=%d paused=%v concurrency=%d", running, held, pending, paused, conc)
}

// resolveTaskArgs 把参数里的 {"__taskOf": 输入} / {"__itemOfTask": 输入} 换成对应任务 id / 其入库条目 id。
func (e *impTestEnv) resolveTaskArgs(t *testing.T, args []json.RawMessage) []json.RawMessage {
	t.Helper()
	lookup := func(col, input string) string {
		rows, err := e.l.db.Query(context.Background(), "SELECT "+col+" FROM import_tasks WHERE source_input = ? ORDER BY created_at DESC, rowid DESC", input)
		if err != nil || len(rows.Values) == 0 || rows.Values[0][0] == nil {
			if col == "id" {
				return "missing-task"
			}
			return "missing-item"
		}
		return rows.Values[0][0].(string)
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			if s, ok := x["__taskOf"].(string); ok {
				return lookup("id", s)
			}
			if s, ok := x["__itemOfTask"].(string); ok {
				return lookup("result_item_id", s)
			}
			for k := range x {
				x[k] = walk(x[k])
			}
		}
		return v
	}
	out := make([]json.RawMessage, len(args))
	for i, raw := range args {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			out[i] = raw
			continue
		}
		b, _ := json.Marshal(walk(v))
		out[i] = b
	}
	return out
}

func (e *impTestEnv) run(ctx context.Context, t *testing.T, fn string, args []json.RawMessage) (any, error) {
	t.Helper()
	switch fn {
	case "settle":
		e.settle(t)
		return nil, nil
	case "recover":
		return nil, e.l.imports.Recover(ctx)
	case "release":
		name, _ := jsArg(args, 0).(string)
		e.mu.Lock()
		if ch, ok := e.holds[name]; ok {
			delete(e.holds, name)
			close(ch)
		}
		e.mu.Unlock()
		return nil, nil
	case "seed":
		rows, _ := jsArg(args, 0).(jsArr)
		var stmts []sqlite.Statement
		for _, r := range rows {
			at := int64(1_700_000_000_000)
			if v, ok := jsGet(r, "at").(float64); ok {
				at = int64(v)
			}
			or := func(k string, d any) any {
				if v := jsGet(r, k); v != jsUndefined && v != nil {
					return v
				}
				return d
			}
			stmts = append(stmts, sqlite.Statement{
				SQL:  "INSERT INTO import_tasks (id, source_kind, source_input, display_name, status, stage, capture_strategy, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
				Args: []any{or("id", nil), or("kind", "text"), or("input", nil), or("input", nil), or("status", nil), or("stage", nil), or("strategy", "standard"), at, at},
			})
		}
		e.l.writeMu.Lock()
		defer e.l.writeMu.Unlock()
		_, err := e.l.db.Transaction(ctx, stmts)
		return nil, err
	case "events":
		e.mu.Lock()
		evs := e.events
		e.events = nil
		e.mu.Unlock()
		byTask := map[string]any{}
		for _, ev := range evs {
			key, _ := ev["sourceInput"].(string)
			list, _ := byTask[key].([]any)
			byTask[key] = append(list, scrubStageStats(ev))
		}
		return byTask, nil
	}
	channel := "import:" + fn
	if fn == "state" {
		channel = "import:queueState"
	}
	h, ok := e.b.handlers[channel]
	if !ok {
		t.Fatalf("频道未登记: %s", channel)
	}
	v, err := h(ctx, e.resolveTaskArgs(t, args))
	if err != nil {
		return nil, err
	}
	return scrubStageStats(toGeneric(t, v)), nil
}
