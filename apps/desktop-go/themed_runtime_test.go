package main

// 主题阅读运行时差分：回放 golden-themed-runtime.mts 的脚本（真实 runtime.ts + 种子库 seed.db + ai-models.json），
// 逐步对拍返回值、进度通知序列、任务表、各页面版本与图片目录。模型 / 搜索 / 生图用与 TS 同一份脚本规则回放。
// 需要 GUIZHI_THEMED_RT_GOLDEN_DIR。用量记账走独立的 AI 用量基线，这里不比。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

var reRTMaskKey = regexp.MustCompile(`At$|updated`)

// rtClean 对应 TS 的 clean(mask(JSON roundtrip))：时间戳抹成 NOW，timings 的毫秒耗时抹成 0。
func rtClean(v any, key string) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = rtClean(x[i], key)
		}
	case map[string]any:
		for k, e := range x {
			if m, ok := e.(map[string]any); ok && k == "timings" {
				for s := range m {
					m[s] = float64(0)
				}
				continue
			}
			x[k] = rtClean(e, k)
		}
	case float64:
		if x > 1e11 && reRTMaskKey.MatchString(key) {
			return "NOW"
		}
	}
	return v
}

func rtJSON(t *testing.T, v any) any {
	t.Helper()
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return rtClean(out, "")
}

type rtStep struct {
	Name    string          `json:"name"`
	Op      string          `json:"op"`
	ItemID  string          `json:"itemId"`
	Kind    string          `json:"kind"`
	Options json.RawMessage `json:"options"`
	Raw     json.RawMessage `json:"raw"`
	Rules   []pipeRule      `json:"rules"`
	Search  struct {
		Default []readingSearchResult            `json:"default"`
		ByQuery map[string][]readingSearchResult `json:"byQuery"`
		ThrowOn map[string]string                `json:"throwOn"`
	} `json:"search"`
	Images []struct {
		PNG   string `json:"png"`
		Throw string `json:"throw"`
	} `json:"images"`
	AssetPick string `json:"assetPick"`
	WaitFor   string `json:"waitFor"`
}

func rtPNG(tag string, extra int) []byte {
	out := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, []byte(tag)...)
	for i := 0; i < extra; i++ {
		out = append(out, 7)
	}
	return out
}

func TestThemedRuntimeDifferential(t *testing.T) {
	dir := os.Getenv("GUIZHI_THEMED_RT_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_THEMED_RT_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-themed-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Recs     []json.RawMessage `json:"recs"`
		Steps    []rtStep          `json:"steps"`
		AIConfig string            `json:"aiConfig"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}

	user := t.TempDir()
	l := &Library{paths: Paths{UserData: user}, configDir: filepath.Join(user, "config")}
	if err := os.MkdirAll(l.configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(l.configDir, "ai-models.json")
	if err := os.WriteFile(cfgPath, []byte(g.AIConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.paths.ImagesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile(filepath.Join(dir, "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(user, "knowledge.db")
	if err := os.WriteFile(work, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), work, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	l.db = db

	var uuidN int
	var uuidMu sync.Mutex
	themedUUIDHook = func() string {
		uuidMu.Lock()
		defer uuidMu.Unlock()
		uuidN++
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", uuidN)
	}
	t.Cleanup(func() { themedUUIDHook = nil })

	var evMu sync.Mutex
	var events []any
	l.setPush(func(ch string, payload any) {
		if ch != chThemedProgress {
			return
		}
		evMu.Lock()
		defer evMu.Unlock()
		events = append(events, rtJSON(t, payload))
	})
	b := &Bridge{handlers: map[string]handler{}}
	l.registerThemedReading(b)
	rt := l.themedRT
	t.Cleanup(func() { rt.shutdown(context.Background()) })
	ctx := context.Background()

	call := func(ch string, args ...any) (any, string) {
		var raws []json.RawMessage
		for _, a := range args {
			bs, _ := json.Marshal(a)
			raws = append(raws, bs)
		}
		out, err := b.handlers[ch](ctx, raws)
		if err != nil {
			return nil, err.Error()
		}
		m := rtJSON(t, out).(map[string]any)
		if m["success"] == false {
			e, _ := m["error"].(string)
			return nil, e
		}
		return m["task"], ""
	}
	idle := func() bool {
		tasks, _ := rt.pages.listTasks(ctx)
		for _, tk := range tasks {
			if s := themedStr(tk, "state"); s == "queued" || s == "running" {
				return false
			}
		}
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return !rt.pumping
	}
	wait := func(label string, cond func() bool) {
		t0 := time.Now()
		for !cond() {
			if time.Since(t0) > 30*time.Second {
				t.Fatalf("等待超时：%s", label)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	latestTask := func(item, kind string) *jsObj {
		tasks, _ := rt.pages.listTasks(ctx)
		var best *jsObj
		for _, tk := range tasks {
			if themedStr(tk, "itemId") == item && themedStr(tk, "sourceKind") == kind && (best == nil || themedNum(tk.get("createdAt")) > themedNum(best.get("createdAt"))) {
				best = tk
			}
		}
		return best
	}

	var (
		stepMu   sync.Mutex
		step     rtStep
		calls    []any
		images   []any
		searches []any
		used     map[int]int
		queue    int
		blocked  bool
	)
	l.chatFn = func(c context.Context, cfg aiClientConfig, msgs []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
		stepMu.Lock()
		var sys, userMsg string
		for _, m := range msgs {
			if m.Role == "system" && sys == "" {
				sys = m.Content
			}
			if m.Role == "user" && userMsg == "" {
				userMsg = m.Content
			}
		}
		head := func(s string, n int) string {
			r := []rune(s)
			if len(r) > n {
				r = r[:n]
			}
			return string(r)
		}
		o := map[string]any{}
		if opts.Temperature != nil {
			o["temperature"] = *opts.Temperature
		}
		if opts.MaxTokens != 0 {
			o["maxTokens"] = opts.MaxTokens
		}
		if opts.Timeout != 0 {
			o["timeoutMs"] = float64(opts.Timeout / time.Millisecond)
		}
		if opts.Stream {
			o["stream"] = true
		}
		calls = append(calls, map[string]any{"config": map[string]any{"provider": cfg.Provider, "apiProtocol": cfg.APIProtocol, "apiKey": cfg.APIKey, "apiUrl": cfg.APIURL, "model": cfg.Model}, "sys": head(sys, 20), "user": head(userMsg, 60), "options": o})
		var rule *pipeRule
		for idx := range step.Rules {
			r := &step.Rules[idx]
			if r.System == "editor" && !strings.HasPrefix(sys, "你是专题文章编辑") || r.System == "design" && !strings.HasPrefix(sys, "你是阅读页设计师") {
				continue
			}
			if r.UserIncludes != "" && !strings.Contains(userMsg, r.UserIncludes) {
				continue
			}
			if r.Times != nil && used[idx] >= *r.Times {
				continue
			}
			used[idx]++
			rule = r
			break
		}
		stepMu.Unlock()
		if rule == nil {
			return aiChatResult{}, errors.New("脚本规则已用尽")
		}
		rp := rule.Reply
		if rp.Block {
			stepMu.Lock()
			blocked = true
			stepMu.Unlock()
			<-c.Done()
			return aiChatResult{}, context.Cause(c)
		}
		for _, d := range rp.Deltas {
			if opts.OnDelta != nil {
				opts.OnDelta(d)
			}
			if opts.OnProgress != nil {
				opts.OnProgress(len([]rune(d)))
			}
		}
		if rp.Throw != "" {
			return aiChatResult{}, errors.New(rp.Throw)
		}
		res := aiChatResult{Content: rp.Content, FinishReason: rp.FinishReason}
		if rp.Usage != nil {
			res.Usage = &chatTokenUsage{PromptTokens: rp.Usage.PromptTokens, CompletionTokens: rp.Usage.CompletionTokens}
		}
		return res, nil
	}
	l.searchFn = func(c context.Context, query, scenario string) ([]readingSearchResult, error) {
		stepMu.Lock()
		defer stepMu.Unlock()
		searches = append(searches, query)
		for k, msg := range step.Search.ThrowOn {
			if strings.Contains(query, k) {
				return nil, errors.New(msg)
			}
		}
		if v, ok := step.Search.ByQuery[query]; ok {
			return v, nil
		}
		return step.Search.Default, nil
	}
	l.imageFn = func(c context.Context, prompt, aspect string, cfg aiClientConfig, o imageGenOptions) (generatedImage, error) {
		stepMu.Lock()
		defer stepMu.Unlock()
		delays := []any{}
		for _, d := range o.RetryDelays {
			delays = append(delays, float64(d/time.Millisecond))
		}
		images = append(images, map[string]any{"prompt": prompt, "aspect": aspect, "model": cfg.Model, "scenario": o.Scenario, "timeoutMs": float64(o.Timeout / time.Millisecond), "retryDelays": delays})
		if o.OnRequest != nil {
			if err := o.OnRequest(); err != nil {
				return generatedImage{}, err
			}
		}
		if queue >= len(step.Images) {
			return generatedImage{}, errors.New("脚本里没有更多图片")
		}
		spec := step.Images[queue]
		queue++
		if spec.Throw != "" {
			return generatedImage{}, errors.New(spec.Throw)
		}
		tag := spec.PNG
		if tag == "" {
			tag = "x"
		}
		return generatedImage{Data: rtPNG(tag, 24), Extension: ".png"}, nil
	}

	dump := func() map[string]any {
		out := map[string]any{}
		for _, id := range []string{"a", "b", "c", "empty", "gone"} {
			for _, kind := range []string{"body", "summary"} {
				for _, role := range []string{"current", "previous"} {
					page, err := rt.pages.get(ctx, id, kind, role)
					if err != nil {
						t.Fatal(err)
					}
					if page != nil {
						out[id+"/"+kind+"/"+role] = rtJSON(t, page)
					}
				}
			}
		}
		rows, err := l.db.Query(ctx, "SELECT item_id,source_kind,role FROM themed_reading_versions ORDER BY item_id,source_kind,role,created_at,id")
		if err != nil {
			t.Fatal(err)
		}
		list := []any{}
		for _, r := range rows.Values {
			list = append(list, map[string]any{"item": r[0], "kind": r[1], "role": r[2]})
		}
		out["versionRows"] = list
		return out
	}

	for i, s := range g.Steps {
		s := s
		stepMu.Lock()
		step, calls, images, searches, used, queue = s, nil, nil, nil, map[int]int{}, 0
		stepMu.Unlock()
		evMu.Lock()
		events = nil
		evMu.Unlock()
		var result any
		var errMsg string
		switch s.Op {
		case "generate":
			var arg any
			if len(s.Raw) > 0 {
				_ = json.Unmarshal(s.Raw, &arg)
			} else {
				var opts any
				_ = json.Unmarshal(s.Options, &opts)
				arg = map[string]any{"itemId": s.ItemID, "sourceKind": s.Kind, "options": opts}
			}
			result, errMsg = call("themedReading:generate", arg)
			if errMsg == "" {
				if s.WaitFor == "blocked" {
					wait(s.Name, func() bool { stepMu.Lock(); defer stepMu.Unlock(); return blocked })
				} else {
					wait(s.Name, idle)
				}
			}
		case "cancel":
			if tk := latestTask(s.ItemID, s.Kind); tk != nil {
				result, errMsg = call("themedReading:cancel", themedStr(tk, "id"))
			}
			stepMu.Lock()
			blocked = false
			stepMu.Unlock()
			wait(s.Name, idle)
		case "resume", "continueOffline":
			id := "no-such-task"
			if tk := latestTask(s.ItemID, s.Kind); tk != nil {
				id = themedStr(tk, "id")
			}
			ch := "themedReading:resume"
			if s.Op == "continueOffline" {
				ch = "themedReading:continueOffline"
			}
			result, errMsg = call(ch, id)
			wait(s.Name, idle)
		case "regenerateAsset":
			var arg map[string]any
			if len(s.Raw) > 0 {
				_ = json.Unmarshal(s.Raw, &arg)
			} else {
				arg = map[string]any{"itemId": s.ItemID, "sourceKind": s.Kind}
			}
			if s.AssetPick == "generated" {
				if page, _ := rt.pages.get(ctx, s.ItemID, s.Kind, ""); page != nil {
					for _, a := range themedArr(page.get("assets")) {
						if themedStr(a, "role") == "generated" {
							arg["assetId"] = themedStr(a, "id")
							break
						}
					}
				}
			}
			result, errMsg = call("themedReading:regenerateAsset", arg)
			wait(s.Name, idle)
		case "removeConfig":
			if err := os.Remove(cfgPath); err != nil {
				t.Fatal(err)
			}
		}

		tasks, _ := rt.pages.listTasks(ctx)
		sort.SliceStable(tasks, func(a, c int) bool {
			ca, cc := themedNum(tasks[a].get("createdAt")), themedNum(tasks[c].get("createdAt"))
			if ca != cc {
				return ca < cc
			}
			return themedStr(tasks[a], "id") < themedStr(tasks[c], "id")
		})
		entries, _ := os.ReadDir(l.paths.ImagesDir())
		files := []any{}
		for _, e := range entries {
			files = append(files, e.Name())
		}
		sort.Slice(files, func(a, c int) bool { return files[a].(string) < files[c].(string) })
		stepMu.Lock()
		evMu.Lock()
		got := map[string]any{
			"name": s.Name, "result": result, "events": nilToEmpty(events), "calls": nilToEmpty(calls),
			"images": nilToEmpty(images), "searches": nilToEmpty(searches),
			"tasks": rtJSON(t, tasks), "pages": dump(), "files": files,
		}
		if errMsg == "" {
			got["error"] = nil
		} else {
			got["error"] = errMsg
		}
		evMu.Unlock()
		stepMu.Unlock()
		b2, _ := json.Marshal(got)
		var gotAny any
		_ = json.Unmarshal(b2, &gotAny)
		var wantAny map[string]any
		if err := json.Unmarshal(g.Recs[i], &wantAny); err != nil {
			t.Fatal(err)
		}
		delete(wantAny, "usage") // 用量记账走独立的 AI 用量基线
		t.Run(s.Name, func(t *testing.T) { fgCompare(t, wantAny, gotAny) })
	}
}
