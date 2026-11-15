package aiflow

// 本文问答的 golden 对拍：夹具来自 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-article-qa.test.ts
// （真实 TS 的 askArticle + 脚本化的 window.api + 假的 AI 传输）。
//
// 重放方式：按同一份 IPC 脚本构造假的 ArticleStore，逐频道排队应答；应答前可触发 during 钩子，
// 确定性地复现取消时序。比较的是 IPC 调用序列、模型请求、用量、日志、补丁序列、结果与错误。
//
// 补丁快照与 TS 一致：顶层的「清掉步骤提示」记为 "$undefined"，长文本（回答、来源正文、证据正文）只记指纹。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ---- golden 形状 ----

type articleHook struct {
	Run string `json:"run"`
}

type articleIPCStepRaw struct {
	Result json.RawMessage `json:"result"`
	Throws *string         `json:"throws"`
	During *articleHook    `json:"during"`
}

type articleStep struct {
	result json.RawMessage
	throws *string
	during *articleHook
}

type articleInputRaw struct {
	Target     ArticleTarget    `json:"target"`
	Question   string           `json:"question"`
	History    []ArticleMessage `json:"history"`
	WebEnabled bool             `json:"webEnabled"`
	RequestID  string           `json:"requestId"`
	Context    *ArticleContext  `json:"context"`
}

type articleAbortOnPatch struct {
	Step   *string `json:"step"`
	Answer bool    `json:"answer"`
}

// ---- 假的存储 ----

type fakeArticleStore struct {
	t      *testing.T
	cancel context.CancelFunc

	mu        sync.Mutex
	queues    map[string][]articleStep
	calls     []map[string]any
	cancels   int
	searching bool
}

func (s *fakeArticleStore) step(channel string, record map[string]any) (articleStep, error) {
	s.mu.Lock()
	s.calls = append(s.calls, record)
	if channel == "search" {
		s.searching = true
	}
	var step articleStep
	found := false
	if queue := s.queues[channel]; len(queue) > 0 {
		step, s.queues[channel], found = queue[0], queue[1:], true
	}
	s.mu.Unlock()

	if channel == "search" {
		defer func() {
			s.mu.Lock()
			s.searching = false
			s.mu.Unlock()
		}()
	}
	if !found {
		return articleStep{}, fmt.Errorf("IPC 脚本已用尽：%s", channel)
	}
	if step.during != nil {
		s.runHook(*step.during)
	}
	if step.throws != nil {
		return articleStep{}, errors.New(*step.throws)
	}
	return step, nil
}

func (s *fakeArticleStore) runHook(hook articleHook) {
	if hook.Run != "abortSignal" {
		s.t.Errorf("未知的钩子：%q", hook.Run)
		return
	}
	// TS 里 abort 事件同步送达：搜索进行中中止时，通知存储取消搜索的调用在钩子返回前就已发出。
	// Go 里取消经由另一个协程转发，这里等那次 cancelSearch 落账后再返回，两边的调用顺序才一致。
	s.mu.Lock()
	before, waitForCancel := s.cancels, s.searching
	s.mu.Unlock()
	s.cancel()
	if !waitForCancel {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		done := s.cancels > before
		s.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func decodeStepResult(t *testing.T, channel string, raw json.RawMessage, into any) {
	t.Helper()
	if len(raw) == 0 {
		return
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Errorf("IPC 脚本的应答无法解析（%s）：%v", channel, err)
	}
}

func (s *fakeArticleStore) GetItem(_ context.Context, itemID string) (*ArticleItem, error) {
	step, err := s.step("get", map[string]any{"channel": "knowledge.get", "id": itemID})
	if err != nil {
		return nil, err
	}
	if len(step.result) == 0 || string(step.result) == "null" {
		return nil, nil
	}
	var decoded struct {
		DeletedAt     *int64   `json:"deletedAt"`
		ReviewStatus  string   `json:"reviewStatus"`
		ReviewReasons []string `json:"reviewReasons"`
	}
	decodeStepResult(s.t, "get", step.result, &decoded)
	return &ArticleItem{DeletedAt: decoded.DeletedAt, ReviewStatus: decoded.ReviewStatus, ReviewReasons: decoded.ReviewReasons}, nil
}

func (s *fakeArticleStore) Context(_ context.Context, target ArticleTarget, question string) (ArticleContextResult, error) {
	step, err := s.step("context", map[string]any{
		"channel": "articleAsk.context",
		"input":   map[string]any{"target": toGeneric(target), "question": question},
	})
	if err != nil {
		return ArticleContextResult{}, err
	}
	var decoded struct {
		Success bool            `json:"success"`
		Context *ArticleContext `json:"context"`
		Error   string          `json:"error"`
	}
	decodeStepResult(s.t, "context", step.result, &decoded)
	return ArticleContextResult{Success: decoded.Success, Context: decoded.Context, Error: decoded.Error}, nil
}

func (s *fakeArticleStore) Search(_ context.Context, requestID string, queries []string) (ArticleSearchResult, error) {
	step, err := s.step("search", map[string]any{
		"channel": "articleAsk.search",
		"input":   map[string]any{"requestId": requestID, "queries": queries},
	})
	if err != nil {
		return ArticleSearchResult{}, err
	}
	var decoded struct {
		Success  bool            `json:"success"`
		Sources  []ArticleSource `json:"sources"`
		Warnings []string        `json:"warnings"`
		Error    string          `json:"error"`
	}
	decodeStepResult(s.t, "search", step.result, &decoded)
	return ArticleSearchResult{Success: decoded.Success, Sources: decoded.Sources, Warnings: decoded.Warnings, Error: decoded.Error}, nil
}

func (s *fakeArticleStore) CancelSearch(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, map[string]any{"channel": "articleAsk.cancelSearch", "requestId": requestID})
	s.cancels++
}

// ---- 补丁快照 ----

// compactSourceList 把来源列表（通用 JSON）里的正文与证据正文换成指纹。
func compactSourceList(v any) {
	list, _ := v.([]any)
	for _, entry := range list {
		source, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := source["text"].(string); ok {
			source["text"] = compactText(text)
		}
		if evidence, ok := source["evidence"].(map[string]any); ok {
			if text, ok := evidence["text"].(string); ok {
				evidence["text"] = compactText(text)
			}
		}
	}
}

// articlePatchSnapshot 对应 TS 的 snapshotPatch。
func articlePatchSnapshot(p ArticlePatch) map[string]any {
	out := map[string]any{}
	switch {
	case p.ClearStep:
		out["step"] = "$undefined"
	case p.Step != nil:
		out["step"] = *p.Step
	}
	if p.Context != nil {
		articleCtx, _ := toGeneric(*p.Context).(map[string]any)
		compactSourceList(articleCtx["sources"])
		out["context"] = articleCtx
	}
	if p.Sources != nil {
		list := toGeneric(p.Sources)
		compactSourceList(list)
		out["sources"] = list
	}
	if p.Warnings != nil {
		out["warnings"] = toGeneric(p.Warnings)
	}
	if p.WebStatus != nil {
		out["webStatus"] = *p.WebStatus
	}
	if p.Answer != nil {
		out["answer"] = compactText(*p.Answer)
	}
	if p.Status != nil {
		out["status"] = *p.Status
	}
	if p.Model != nil {
		out["model"] = *p.Model
	}
	if p.Truncated != nil {
		out["truncated"] = *p.Truncated
	}
	return out
}

// ---- 重放 ----

func TestArticleQAGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name         string                         `json:"name"`
			Settings     map[string]any                 `json:"settings"`
			Abort        *string                        `json:"abort"`
			Script       []scriptStep                   `json:"script"`
			Input        any                            `json:"input"`
			IPC          map[string][]articleIPCStepRaw `json:"ipc"`
			Now          int64                          `json:"now"`
			AbortOnPatch *articleAbortOnPatch           `json:"abortOnPatch"`
			Expect       map[string]any                 `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "article-qa", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var (
				store   *fakeArticleStore
				patches = []any{}
			)
			got := runFlowWith(t, staticConfig{NormalizeAISettings(c.Settings)}, c.Script, c.Abort, false,
				func(ctx context.Context, cancel context.CancelFunc, r *Runner) (any, error) {
					// 用例输入与 IPC 应答里的 {"$text": 规格} 先展开
					var digests []textDigest
					rawInput, err := json.Marshal(expandTexts(t, c.Input, &digests))
					if err != nil {
						t.Fatalf("输入无法序列化：%v", err)
					}
					var raw articleInputRaw
					if err := json.Unmarshal(rawInput, &raw); err != nil {
						t.Fatalf("输入无法解析：%v", err)
					}
					if raw.RequestID == "" {
						raw.RequestID = "req-article-1"
					}

					queues := map[string][]articleStep{}
					for channel, steps := range c.IPC {
						for _, step := range steps {
							queued := articleStep{throws: step.Throws, during: step.During}
							if len(step.Result) > 0 {
								var value any
								if err := json.Unmarshal(step.Result, &value); err != nil {
									t.Fatalf("IPC 应答无法解析：%v", err)
								}
								encoded, err := json.Marshal(expandTexts(t, value, &digests))
								if err != nil {
									t.Fatalf("IPC 应答无法序列化：%v", err)
								}
								queued.result = encoded
							}
							queues[channel] = append(queues[channel], queued)
						}
					}

					store = &fakeArticleStore{t: t, cancel: cancel, queues: queues, calls: []map[string]any{}}
					asker := &ArticleAsker{Runner: r, Store: store, Now: func() int64 { return c.Now }}
					hook := c.AbortOnPatch

					err = asker.Ask(ctx, AskArticleInput{
						Target: raw.Target, Question: raw.Question, History: raw.History,
						WebEnabled: raw.WebEnabled, RequestID: raw.RequestID, Context: raw.Context,
					}, func(p ArticlePatch) {
						patches = append(patches, articlePatchSnapshot(p))
						if hook != nil && ((hook.Step != nil && p.Step != nil && *p.Step == *hook.Step) || (hook.Answer && p.Answer != nil)) {
							cancel()
						}
					})
					return nil, err
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

			got["patches"] = patches
			got["calls"] = calls
			got["unusedIpc"] = unusedIPC
			assertOutcome(t, c.Expect, got)
		})
	}
}
