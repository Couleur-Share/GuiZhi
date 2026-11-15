package aiflow

// Wiki 全文编译驱动器的 golden 对拍：模型身份、预览、控制、逐块驱动与后台一轮。
// 夹具来自 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-wiki-driver.test.ts
// （真实 TS 实现 + 脚本化的 window.api.wiki + 假的 AI 传输）。
//
// 重放方式：按同一份 IPC 脚本构造假的 WikiStore，逐频道排队应答；应答前可触发 during 钩子，
// 确定性地复现并发时序（嵌套再跑一遍、经 control 取消、中止信号、中途换模型）。
// 比较的是 IPC 调用序列、模型请求、用量、进度回调、事件、结果与错误；
// 其中恰好等于模型身份串的字符串在输出里统一记为 "$identity"（golden 里的约定）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

const wikiIdentityPlaceholder = "$identity"

// ---- golden 形状 ----

type wikiHook struct {
	Run      string         `json:"run"`
	ID       string         `json:"id"`
	Settings map[string]any `json:"settings"`
}

type wikiIPCStepRaw struct {
	Result any       `json:"result"`
	Throws *string   `json:"throws"`
	During *wikiHook `json:"during"`
}

type wikiCallSpec struct {
	Kind   string         `json:"kind"`
	IDs    []string       `json:"ids"`
	ID     string         `json:"id"`
	Status string         `json:"status"`
	Job    map[string]any `json:"job"`
}

// wikiStep 是展开占位之后的一步 IPC 应答。
type wikiStep struct {
	result json.RawMessage
	throws *string
	during *wikiHook
}

// ---- 占位展开与身份遮蔽 ----

// replaceIdentityPlaceholder 把字符串 "$identity" 换成当前模型身份串（通用 JSON 值上的深度替换）。
func replaceIdentityPlaceholder(v any, identity string) any {
	switch x := v.(type) {
	case string:
		if x == wikiIdentityPlaceholder {
			return identity
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, entry := range x {
			out[i] = replaceIdentityPlaceholder(entry, identity)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, entry := range x {
			out[key] = replaceIdentityPlaceholder(entry, identity)
		}
		return out
	}
	return v
}

// maskIdentity 与 TS 的 maskIdentity 一致：恰好等于身份串的字符串换回 "$identity"。身份为空时不替换（否则会误伤空串）。
func maskIdentity(v any, identity string) any {
	if identity == "" {
		return v
	}
	switch x := v.(type) {
	case string:
		if x == identity {
			return wikiIdentityPlaceholder
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, entry := range x {
			out[i] = maskIdentity(entry, identity)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, entry := range x {
			out[key] = maskIdentity(entry, identity)
		}
		return out
	}
	return v
}

// toGeneric 经 JSON 往返得到通用结构；这些类型都是可序列化的，失败说明测试本身写错了。
func toGeneric(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("序列化失败：%v", err))
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(fmt.Sprintf("反序列化失败：%v", err))
	}
	return out
}

// compactWikiInput 对应 TS 的 compactCall：长文本（分块正文、贡献的标题/摘要/正文）只记指纹。
func compactWikiInput(input map[string]any) map[string]any {
	if work, ok := input["work"].(map[string]any); ok {
		if block, ok := work["block"].(map[string]any); ok {
			if text, ok := block["text"].(string); ok {
				block["text"] = compactText(text)
			}
		}
	}
	if list, ok := input["contributions"].([]any); ok {
		for _, entry := range list {
			contribution, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, field := range []string{"title", "summary", "body"} {
				if text, ok := contribution[field].(string); ok {
					contribution[field] = compactText(text)
				}
			}
		}
	}
	return input
}

// ---- 假的存储 ----

type fakeWikiStore struct {
	t *testing.T

	mu     sync.Mutex
	queues map[string][]wikiStep
	calls  []map[string]any
	counts map[string]int

	// runHook 在应答前执行 during 钩子；不持锁，钩子里可以再调驱动器。
	runHook func(wikiHook)
}

func (s *fakeWikiStore) step(channel string, record map[string]any) (wikiStep, error) {
	s.mu.Lock()
	s.calls = append(s.calls, record)
	s.counts[channel]++
	var step wikiStep
	found := false
	if queue := s.queues[channel]; len(queue) > 0 {
		step, s.queues[channel], found = queue[0], queue[1:], true
	}
	s.mu.Unlock()

	if !found {
		return wikiStep{}, fmt.Errorf("IPC 脚本已用尽：%s", channel)
	}
	if step.during != nil {
		s.runHook(*step.during)
	}
	if step.throws != nil {
		return wikiStep{}, errors.New(*step.throws)
	}
	return step, nil
}

func (s *fakeWikiStore) callCount(channel string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[channel]
}

func (s *fakeWikiStore) compiler(channel string, input map[string]any) (WikiResult, error) {
	record := map[string]any{"channel": "compiler", "input": compactWikiInput(toGeneric(input).(map[string]any))}
	step, err := s.step(channel, record)
	if err != nil {
		return WikiResult{}, err
	}
	var decoded struct {
		OK      bool                `json:"ok"`
		Error   string              `json:"error"`
		Preview *WikiCompilePreview `json:"preview"`
		Job     *WikiCompileJob     `json:"job"`
		Work    *WikiCompileWork    `json:"work"`
	}
	if len(step.result) > 0 {
		if err := json.Unmarshal(step.result, &decoded); err != nil {
			s.t.Errorf("IPC 脚本的应答无法解析（%s）：%v", channel, err)
		}
	}
	return WikiResult{OK: decoded.OK, Error: decoded.Error, Preview: decoded.Preview, Job: decoded.Job, Work: decoded.Work}, nil
}

func (s *fakeWikiStore) Preview(_ context.Context, model string, ids []string) (WikiResult, error) {
	input := map[string]any{"action": "preview", "model": model}
	if ids != nil {
		input["ids"] = ids
	}
	return s.compiler("preview", input)
}

func (s *fakeWikiStore) Start(_ context.Context, model string, selected []WikiCandidate) (WikiResult, error) {
	return s.compiler("start", map[string]any{"action": "start", "model": model, "selected": selected})
}

func (s *fakeWikiStore) Next(_ context.Context, jobID string) (WikiResult, error) {
	return s.compiler("next", map[string]any{"action": "next", "id": jobID})
}

func (s *fakeWikiStore) Finish(_ context.Context, work WikiCompileWork, contributions []WikiContribution, errMsg *string) (WikiResult, error) {
	input := map[string]any{"action": "finish", "work": work}
	if contributions != nil {
		input["contributions"] = contributions
	}
	if errMsg != nil {
		input["error"] = *errMsg
	}
	return s.compiler("finish", input)
}

func (s *fakeWikiStore) Control(_ context.Context, jobID, status string) (WikiResult, error) {
	return s.compiler("control", map[string]any{"action": "control", "id": jobID, "status": status})
}

func (s *fakeWikiStore) Catalog(_ context.Context) ([]WikiCatalogEntry, error) {
	step, err := s.step("catalog", map[string]any{"channel": "catalog"})
	if err != nil {
		return nil, err
	}
	var entries []WikiCatalogEntry
	if len(step.result) > 0 {
		if err := json.Unmarshal(step.result, &entries); err != nil {
			s.t.Errorf("IPC 脚本的目录应答无法解析：%v", err)
		}
	}
	return entries, nil
}

// ---- 可变的配置来源（流程里换模型） ----

type mutableConfig struct {
	mu       sync.Mutex
	settings AISettings
}

func (m *mutableConfig) AISettings() AISettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

func (m *mutableConfig) set(settings AISettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = settings
}

// ---- 重放 ----

// settleHook 对应 TS 的 settle：钩子里调用的结果（成功记 value，失败记 error）。
func settleHook(name string, value any, err error) map[string]any {
	if err != nil {
		return map[string]any{"hook": name, "error": flowErrorInfo(err, false)}
	}
	return map[string]any{"hook": name, "value": value}
}

func jobOrNil(job *WikiCompileJob) any {
	if job == nil {
		return nil
	}
	return *job
}

func TestWikiDriverGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string                      `json:"name"`
			Settings map[string]any              `json:"settings"`
			Abort    *string                     `json:"abort"`
			Script   []scriptStep                `json:"script"`
			Call     wikiCallSpec                `json:"call"`
			IPC      map[string][]wikiIPCStepRaw `json:"ipc"`
			Expect   map[string]any              `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "wiki-driver", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			config := &mutableConfig{settings: NormalizeAISettings(c.Settings)}

			var (
				identity string
				events   int
				evMu     sync.Mutex
				nested   = []any{}
				progress = []any{}
				store    *fakeWikiStore
			)

			got := runFlowWith(t, config, c.Script, c.Abort, false,
				func(ctx context.Context, cancel context.CancelFunc, r *Runner) (any, error) {
					driver := &WikiDriver{
						Runner: r,
						OnContributionsUpdated: func() {
							evMu.Lock()
							events++
							evMu.Unlock()
						},
					}
					identity = driver.ModelIdentity()

					// IPC 脚本：展开 $text 与 $identity，按频道排队
					queues := map[string][]wikiStep{}
					for channel, steps := range c.IPC {
						for _, raw := range steps {
							step := wikiStep{throws: raw.Throws, during: raw.During}
							if raw.Result != nil {
								var digests []textDigest
								resolved := replaceIdentityPlaceholder(expandTexts(t, raw.Result, &digests), identity)
								encoded, err := json.Marshal(resolved)
								if err != nil {
									t.Fatalf("IPC 应答无法序列化：%v", err)
								}
								step.result = encoded
							}
							queues[channel] = append(queues[channel], step)
						}
					}

					resolveJob := func(source map[string]any) WikiCompileJob {
						var digests []textDigest
						resolved := replaceIdentityPlaceholder(expandTexts(t, source, &digests), identity)
						encoded, err := json.Marshal(resolved)
						if err != nil {
							t.Fatalf("任务无法序列化：%v", err)
						}
						var job WikiCompileJob
						if err := json.Unmarshal(encoded, &job); err != nil {
							t.Fatalf("任务无法解析：%v", err)
						}
						return job
					}

					store = &fakeWikiStore{t: t, queues: queues, calls: []map[string]any{}, counts: map[string]int{}}
					driver.Store = store

					defaultJob := map[string]any{
						"id": "job-1", "model": wikiIdentityPlaceholder, "status": "running",
						"total": 3, "completed": 0, "failed": 0, "createdAt": 1_700_000_000_000,
					}
					store.runHook = func(hook wikiHook) {
						switch hook.Run {
						case "runJobAgain":
							job, err := driver.RunJob(context.Background(), resolveJob(defaultJob), nil)
							nested = append(nested, settleHook("runJobAgain", job, err))
						case "cancelViaControl":
							job, err := driver.Control(context.Background(), hook.ID, "cancelled")
							nested = append(nested, settleHook("cancelViaControl", jobOrNil(job), err))
						case "compileReadyAgain":
							round, err := driver.CompileReady(context.Background(), nil)
							nested = append(nested, settleHook("compileReadyAgain", round, err))
						case "abortSignal":
							// TS 里 abort 事件同步送达：取消通知存储的调用在钩子返回前就已发出。
							// Go 里取消经由另一个协程转发，这里等那次 control 调用落账后再返回，两边的调用顺序才一致。
							before := store.callCount("control")
							cancel()
							deadline := time.Now().Add(2 * time.Second)
							for store.callCount("control") == before && time.Now().Before(deadline) {
								time.Sleep(time.Millisecond)
							}
							nested = append(nested, map[string]any{"hook": "abortSignal"})
						case "swapSettings":
							config.set(NormalizeAISettings(hook.Settings))
							nested = append(nested, map[string]any{"hook": "swapSettings"})
						default:
							t.Errorf("未知的钩子：%q", hook.Run)
						}
					}

					switch c.Call.Kind {
					case "identity":
						return identity, nil
					case "preview":
						return driver.Preview(ctx, c.Call.IDs)
					case "control":
						job, err := driver.Control(ctx, c.Call.ID, c.Call.Status)
						if err != nil {
							return nil, err
						}
						return jobOrNil(job), nil
					case "runJob":
						return driver.RunJob(ctx, resolveJob(c.Call.Job), func(job WikiCompileJob) {
							progress = append(progress, toGeneric(job))
						})
					case "compileReady":
						return driver.CompileReady(ctx, func(title string, current, total int) {
							progress = append(progress, map[string]any{"title": title, "current": current, "total": total})
						})
					}
					t.Fatalf("未知的调用类型：%q", c.Call.Kind)
					return nil, nil
				})

			unusedIPC := map[string]any{}
			store.mu.Lock()
			for channel, queue := range store.queues {
				if len(queue) > 0 {
					unusedIPC[channel] = len(queue)
				}
			}
			calls := toGeneric(store.calls)
			store.mu.Unlock()

			mask := func(v any) any { return maskIdentity(toGeneric(v), identity) }
			evMu.Lock()
			eventCount := events
			evMu.Unlock()

			got["identity"] = identity
			got["result"] = mask(got["result"])
			got["calls"] = maskIdentity(calls, identity)
			got["nested"] = mask(nested)
			got["progress"] = mask(progress)
			got["events"] = eventCount
			got["unusedIpc"] = unusedIPC

			assertOutcome(t, c.Expect, got)
		})
	}
}
