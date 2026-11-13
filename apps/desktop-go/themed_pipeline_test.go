package main

// 主题阅读生成管线差分：把 golden-themed-pipeline.mts 的脚本规则喂给 goja 里的管线打包产物，
// 逐项对拍模型请求、钩子序列（含版本快照）、用量、搜索 / 抓取调用与最终版本。需要 GUIZHI_THEMED_PIPE_GOLDEN_DIR。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var rePipeMaskKey = regexp.MustCompile(`At$|updated`)

// pipeMask 与 TS 基线的 mask 一致：时间戳字段（大于 1e11 的数字）抹成 "NOW"。
func pipeMask(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = pipeMask(x[i])
		}
	case map[string]any:
		for k, e := range x {
			if f, ok := e.(float64); ok && f > 1e11 && rePipeMaskKey.MatchString(k) {
				x[k] = "NOW"
			} else {
				x[k] = pipeMask(e)
			}
		}
	}
	return v
}

type pipeRule struct {
	System       string `json:"system"`
	UserIncludes string `json:"userIncludes"`
	Times        *int   `json:"times"`
	Reply        struct {
		Content        string                                            `json:"content"`
		Deltas         []string                                          `json:"deltas"`
		FinishReason   string                                            `json:"finishReason"`
		Usage          *struct{ PromptTokens, CompletionTokens float64 } `json:"usage"`
		Throw          string                                            `json:"throw"`
		Abort          bool                                              `json:"abort"`
		EchoCandidates bool                                              `json:"echoCandidates"`
		Block          bool                                              `json:"block"`
	} `json:"reply"`
}

type pipeRun struct {
	Rules  []pipeRule `json:"rules"`
	Search struct {
		Default []readingSearchResult            `json:"default"`
		ByQuery map[string][]readingSearchResult `json:"byQuery"`
		ThrowOn map[string]string                `json:"throwOn"`
	} `json:"search"`
	Capture struct {
		Default map[string]any            `json:"default"`
		ByURL   map[string]map[string]any `json:"byUrl"`
		ThrowOn map[string]string         `json:"throwOn"`
	} `json:"capture"`
	Op     string    `json:"op"`
	Verify []*string `json:"verify"`
}

func pipeDecode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func pipeObj(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("版本 JSON 无效：%v", err)
	}
	return v
}

func TestThemedPipelineDifferential(t *testing.T) {
	dir := os.Getenv("GUIZHI_THEMED_PIPE_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_THEMED_PIPE_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-themed-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Recs []forumRec `json:"recs"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	eng, err := getPipelineEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Recs {
		r := r
		t.Run(r.Name, func(t *testing.T) {
			var in struct {
				Version json.RawMessage `json:"version"`
				Config  aiConfigJSON    `json:"config"`
				Runs    []json.RawMessage
			}
			mustJSON(t, r.Input, &in)
			want := pipeDecode[[]any](t, r.Output)
			version := string(in.Version)
			cfgJSON, _ := json.Marshal(in.Config)
			for i, rawRun := range in.Runs {
				run := pipeDecode[pipeRun](t, rawRun)
				var events, calls, usage []any
				searches, captures := []any{}, []any{}
				lastCheckpoint := ""
				ctx, cancel := context.WithCancelCause(context.Background())
				used := map[int]int{}
				chat := func(c context.Context, cfg aiClientConfig, msgs []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
					var sys, user string
					mm := []any{}
					for _, m := range msgs {
						mm = append(mm, map[string]any{"role": m.Role, "content": m.Content})
						if m.Role == "system" && sys == "" {
							sys = m.Content
						}
						if m.Role == "user" && user == "" {
							user = m.Content
						}
					}
					o := map[string]any{"progress": opts.OnProgress != nil, "delta": opts.OnDelta != nil}
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
					calls = append(calls, map[string]any{"config": map[string]any{"provider": cfg.Provider, "apiProtocol": cfg.APIProtocol, "apiKey": cfg.APIKey, "apiUrl": cfg.APIURL, "model": cfg.Model}, "messages": mm, "options": o})
					for idx, rule := range run.Rules {
						if rule.System == "editor" && !strings.HasPrefix(sys, "你是专题文章编辑") || rule.System == "design" && !strings.HasPrefix(sys, "你是阅读页设计师") {
							continue
						}
						if rule.UserIncludes != "" && !strings.Contains(user, rule.UserIncludes) {
							continue
						}
						if rule.Times != nil && used[idx] >= *rule.Times {
							continue
						}
						used[idx]++
						rp := rule.Reply
						if rp.Abort {
							cancel(errors.New("This operation was aborted"))
							time.Sleep(80 * time.Millisecond) // 等中止信号先于增量送达管线（TS 里 abort 是同步的）
						}
						content := rp.Content
						if rp.EchoCandidates {
							var u struct{ Candidates []struct{ URL string } }
							_ = json.Unmarshal([]byte(user), &u)
							urls := []string{}
							for _, cd := range u.Candidates {
								urls = append(urls, cd.URL)
							}
							b, _ := json.Marshal(map[string]any{"urls": urls})
							content = string(b)
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
						res := aiChatResult{Content: content, FinishReason: rp.FinishReason}
						if rp.Usage != nil {
							res.Usage = &chatTokenUsage{PromptTokens: rp.Usage.PromptTokens, CompletionTokens: rp.Usage.CompletionTokens}
						}
						return res, nil
					}
					return aiChatResult{}, errors.New("脚本规则已用尽")
				}
				vi := 0
				svc := pipelineServices{
					Chat: chat,
					Usage: func(scenario, model string, p, c *float64, failed bool) {
						u := map[string]any{"scenario": scenario, "model": model}
						if p != nil {
							u["promptTokens"] = *p
						}
						if c != nil {
							u["completionTokens"] = *c
						}
						if failed {
							u["failed"] = true
						}
						usage = append(usage, u)
					},
					Search: func(c context.Context, query, scenario string) ([]readingSearchResult, error) {
						searches = append(searches, query)
						for k, msg := range run.Search.ThrowOn {
							if strings.Contains(query, k) {
								return nil, errors.New(msg)
							}
						}
						if v, ok := run.Search.ByQuery[query]; ok {
							return v, nil
						}
						return run.Search.Default, nil
					},
					Capture: func(c context.Context, req webCaptureRequest) (webCaptureResult, error) {
						captures = append(captures, map[string]any{"url": req.URL, "purpose": req.Purpose, "taskId": req.TaskID})
						spec := run.Capture.Default
						if v, ok := run.Capture.ByURL[req.URL]; ok {
							spec = v
						}
						for k, msg := range run.Capture.ThrowOn {
							if strings.Contains(req.URL, k) {
								return webCaptureResult{}, errors.New(msg)
							}
						}
						if m, ok := spec["throw"].(string); ok {
							return webCaptureResult{}, errors.New(m)
						}
						if spec == nil {
							return webCaptureResult{Complete: true, Markdown: strings.Repeat("可核对的来源正文与明确适用条件。", 25)}, nil
						}
						md, _ := spec["markdown"].(string)
						return webCaptureResult{Complete: spec["complete"] == true, Markdown: md}, nil
					},
					Verify: func(c context.Context, versionJSON string) (string, error) {
						var out *string
						if vi < len(run.Verify) {
							out = run.Verify[vi]
						}
						vi++
						if out == nil {
							return "", nil
						}
						return *out, nil
					},
				}
				hooks := pipelineHooks{
					Checkpoint: func(v string) error {
						lastCheckpoint = v
						events = append(events, map[string]any{"k": "checkpoint", "v": pipeMask(pipeObj(t, v))})
						return nil
					},
					Stage: func(stage string, done, total int) error {
						events = append(events, map[string]any{"k": "stage", "stage": stage, "done": float64(done), "total": float64(total)})
						return nil
					},
					Request: func(kind string) error {
						events = append(events, map[string]any{"k": "request", "kind": kind})
						return nil
					},
					DesignProgress: func(a, c int) error {
						events = append(events, map[string]any{"k": "designProgress", "attempt": float64(a), "chars": float64(c)})
						return nil
					},
				}
				op := "design"
				if run.Op != "" {
					op = run.Op
				}
				args, _ := json.Marshal(map[string]any{"version": json.RawMessage(version), "config": json.RawMessage(cfgJSON)})
				result, runErr := eng.Run(ctx, op, string(args), hooks, svc)
				cancel(nil)
				got := map[string]any{"calls": nilToEmpty(calls), "events": nilToEmpty(events), "usage": nilToEmpty(usage), "searches": searches, "captures": captures}
				if runErr != nil {
					got["error"] = runErr.Error()
					got["result"] = nil
					if lastCheckpoint != "" {
						version = lastCheckpoint
					}
				} else {
					got["error"] = nil
					got["result"] = pipeMask(pipeObj(t, result))
					version = result
				}
				b, _ := json.Marshal(got)
				var gotAny any
				_ = json.Unmarshal(b, &gotAny)
				if i >= len(want) {
					t.Fatalf("多出一轮 %d", i)
				}
				t.Run(strings.Join([]string{"run", string(rune('0' + i))}, ""), func(t *testing.T) {
					// V8 与 goja 的语法错误措辞不同（已知差异），其余逐项比对
					fgCompare(t, scrubSyntaxMessages(want[i]), scrubSyntaxMessages(gotAny))
				})
			}
		})
	}
}

func nilToEmpty(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

type aiConfigJSON struct {
	Provider    string `json:"provider"`
	APIProtocol string `json:"apiProtocol"`
	APIKey      string `json:"apiKey"`
	APIURL      string `json:"apiUrl"`
	Model       string `json:"model"`
}
