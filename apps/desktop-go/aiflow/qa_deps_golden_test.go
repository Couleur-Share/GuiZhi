package aiflow

// 全局问答依赖组装（NewQADeps）的 golden 对拍：夹具来自
// apps/desktop/tests/unit/knowledge-ai/aiflow-golden-qa-deps.test.ts（真实 createQaDeps + 假 window.api + 假传输）。
// 这一层只做接线：readItem 的筛选与回落、wiki 调用透传、chat 走 qa 场景、检索走混合检索且同一句提示只回调一次。

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type qaSemBehavior struct {
	Hits   []goldenSemanticHit `json:"hits"`
	Throws *string             `json:"throws"`
	Hang   bool                `json:"hang"`
}

type qaSemSpec struct {
	qaSemBehavior
	Sequence []qaSemBehavior `json:"sequence"`
}

// hasHang 说明这个用例里有语义检索挂起（要靠超时收场，需要缩短超时）。
func (s qaSemSpec) hasHang() bool {
	if s.Hang {
		return true
	}
	for _, behavior := range s.Sequence {
		if behavior.Hang {
			return true
		}
	}
	return false
}

// qaDepsSource 在 fakeHybridSource 的关键词检索之上，让语义检索支持「按调用次序给出不同行为」。
type qaDepsSource struct {
	*fakeHybridSource
	spec qaSemSpec
}

func (s *qaDepsSource) SemanticSearch(ctx context.Context, model string, vector []float64, limit int) ([]SemanticSearchHit, error) {
	s.mu.Lock()
	s.semanticCalls = append(s.semanticCalls, map[string]any{"model": model, "vector": vector, "limit": limit})
	index := len(s.semanticCalls) - 1
	s.mu.Unlock()

	behavior := s.spec.qaSemBehavior
	if len(s.spec.Sequence) > 0 {
		behavior = qaSemBehavior{}
		if index < len(s.spec.Sequence) {
			behavior = s.spec.Sequence[index]
		}
	}
	switch {
	case behavior.Throws != nil:
		return nil, errors.New(*behavior.Throws)
	case behavior.Hang:
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out := make([]SemanticSearchHit, len(behavior.Hits))
	for i, h := range behavior.Hits {
		out[i] = SemanticSearchHit{ReviewStatus: h.ReviewStatus, ItemID: h.ItemID, Title: h.Title, Snippet: h.Snippet, Score: h.Score}
		if h.ReviewReasons != nil {
			out[i].ReviewReasons = append([]string{}, *h.ReviewReasons...)
		}
	}
	return out, nil
}

// qaDepsStore 回放 knowledge.get 与 wiki.* 的脚本行为并记录调用。
type qaDepsStore struct {
	t         *testing.T
	get       any
	wiki      any
	getCalls  []map[string]any
	wikiCalls []map[string]any
}

func qaFail(v any) error {
	if m, ok := v.(map[string]any); ok {
		if throws, has := m["throws"]; has {
			return errors.New(throws.(string))
		}
	}
	return nil
}

func (s *qaDepsStore) GetItem(_ context.Context, id string) (*QAKnowledgeItem, error) {
	s.getCalls = append(s.getCalls, map[string]any{"id": id})
	if err := qaFail(s.get); err != nil {
		return nil, err
	}
	m, ok := s.get.(map[string]any)
	if !ok { // null、缺省与 "$undefined" 都是「不存在」
		return nil, nil
	}
	item := &QAKnowledgeItem{Title: m["title"].(string), Content: m["content"].(string)}
	if v, ok := m["deletedAt"].(float64); ok {
		n := int64(v)
		item.DeletedAt = &n
	}
	if v, ok := m["transcript"].(string); ok {
		item.Transcript = &v
	}
	if v, ok := m["updatedAt"].(float64); ok {
		n := int64(v)
		item.UpdatedAt = &n
	}
	if v, ok := m["reviewStatus"].(string); ok {
		item.ReviewStatus = v
	}
	if v, ok := m["reviewReasons"].([]any); ok {
		item.ReviewReasons = []string{}
		for _, reason := range v {
			item.ReviewReasons = append(item.ReviewReasons, reason.(string))
		}
	}
	return item, nil
}

func (s *qaDepsStore) WikiCatalog(context.Context) ([]WikiCatalogEntry, error) {
	s.wikiCalls = append(s.wikiCalls, map[string]any{"call": "catalog"})
	if err := qaFail(s.wiki); err != nil {
		return nil, err
	}
	return remarshal[[]WikiCatalogEntry](s.t, s.wiki), nil
}

func (s *qaDepsStore) WikiSearch(_ context.Context, query string, limit int) ([]WikiSearchHit, error) {
	s.wikiCalls = append(s.wikiCalls, map[string]any{"call": "search", "query": query, "limit": limit})
	if err := qaFail(s.wiki); err != nil {
		return nil, err
	}
	return remarshal[[]WikiSearchHit](s.t, s.wiki), nil
}

func (s *qaDepsStore) WikiPage(_ context.Context, id string) (*WikiPageDetail, error) {
	s.wikiCalls = append(s.wikiCalls, map[string]any{"call": "getPage", "id": id})
	if err := qaFail(s.wiki); err != nil {
		return nil, err
	}
	if s.wiki == nil {
		return nil, nil
	}
	detail := remarshal[WikiPageDetail](s.t, s.wiki)
	return &detail, nil
}

// qaItemView 对应 TS 测试里的 normalizeItem：null 与缺省等价（Go 的 *string 分不出两者，引擎也不区分）。
func qaItemView(item *QAItemContent) any {
	if item == nil {
		return nil
	}
	view := map[string]any{"title": item.Title, "content": item.Content}
	if item.ReviewStatus != "" {
		view["reviewStatus"] = item.ReviewStatus
	}
	if item.ReviewReasons != nil {
		view["reviewReasons"] = item.ReviewReasons
	}
	if item.UpdatedAt != nil {
		view["updatedAt"] = *item.UpdatedAt
	}
	if item.Transcript != nil {
		view["transcript"] = *item.Transcript
	}
	return view
}

func TestQADepsGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string         `json:"name"`
			Settings map[string]any `json:"settings"`
			Script   []scriptStep   `json:"script"`
			Abort    *string        `json:"abort"`
			Op       string         `json:"op"`
			ID       string         `json:"id"`
			Get      any            `json:"get"`
			Wiki     *struct {
				Call   string `json:"call"`
				Query  string `json:"query"`
				Limit  int    `json:"limit"`
				ID     string `json:"id"`
				Result any    `json:"result"`
			} `json:"wiki"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Options struct {
				Temperature *float64 `json:"temperature"`
				MaxTokens   int      `json:"maxTokens"`
				Stream      bool     `json:"stream"`
			} `json:"options"`
			Queries   []string        `json:"queries"`
			Limit     int             `json:"limit"`
			FTS       json.RawMessage `json:"fts"`
			Semantic  json.RawMessage `json:"semantic"`
			FreshDeps bool            `json:"freshDeps"`
			Expect    map[string]any  `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "qa-deps", &golden)
	if len(golden.Cases) < 30 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ctxSource := &fakeHybridSource{}
			if len(c.FTS) > 0 {
				if err := json.Unmarshal(c.FTS, &ctxSource.fts); err != nil {
					t.Fatalf("解析 fts：%v", err)
				}
			}
			source := &qaDepsSource{fakeHybridSource: ctxSource}
			if len(c.Semantic) > 0 {
				if err := json.Unmarshal(c.Semantic, &source.spec); err != nil {
					t.Fatalf("解析 semantic：%v", err)
				}
			}
			store := &qaDepsStore{t: t, get: c.Get}
			if c.Wiki != nil {
				store.wiki = c.Wiki.Result
			}
			if text, ok := c.Get.(string); ok && text == "$undefined" {
				store.get = nil
			}

			// 挂起的用例靠超时收场（TS 侧把 3 秒缩成了 150 毫秒）；其余给足时间，避免负载高时误触发
			timeout := 5 * time.Second
			if source.spec.hasHang() {
				timeout = 150 * time.Millisecond
			}
			var mu sync.Mutex
			onWarnings := []string{}
			deltas := []string{}

			got := runFlowWith(t, staticConfig{NormalizeAISettings(c.Settings)}, c.Script, c.Abort, false,
				func(ctx context.Context, _ context.CancelFunc, r *Runner) (any, error) {
					hybrid := &Hybrid{Runner: r, Source: source, Vectors: &QueryVectors{}, SemanticTimeout: timeout}
					newDeps := func() QADeps {
						return NewQADeps(r, hybrid, store, func(message string) {
							mu.Lock()
							defer mu.Unlock()
							onWarnings = append(onWarnings, message)
						}, nil)
					}
					deps := newDeps()
					switch c.Op {
					case "readItem":
						item, err := deps.ReadItem(ctx, c.ID)
						if err != nil {
							return nil, err
						}
						return qaItemView(item), nil
					case "wiki":
						switch c.Wiki.Call {
						case "catalog":
							return deps.WikiCatalog(ctx)
						case "search":
							return deps.SearchWikiPages(ctx, c.Wiki.Query, c.Wiki.Limit)
						default:
							detail, err := deps.ReadWikiPage(ctx, c.Wiki.ID)
							if err != nil || detail == nil {
								return nil, err
							}
							return detail, nil
						}
					case "chat":
						messages := make([]ChatMessage, len(c.Messages))
						for i, m := range c.Messages {
							messages[i] = ChatMessage{Role: m.Role, Content: m.Content}
						}
						opts := ScenarioChatOptions{Temperature: c.Options.Temperature, MaxTokens: c.Options.MaxTokens}
						if c.Options.Stream {
							opts.OnDelta = func(chunk string) { deltas = append(deltas, chunk) }
						}
						res, err := deps.Chat(ctx, messages, opts)
						if err != nil {
							return nil, err
						}
						out := map[string]any{"content": res.Content, "model": res.Model}
						if res.FinishReason != "" {
							out["finishReason"] = res.FinishReason
						}
						return out, nil
					case "search":
						results := []map[string]any{}
						for _, query := range c.Queries {
							target := deps
							if c.FreshDeps {
								target = newDeps()
							}
							hits, err := target.SearchItems(ctx, query, c.Limit)
							if err != nil {
								results = append(results, map[string]any{"ok": false, "error": flowErrorInfo(err, false)})
								continue
							}
							results = append(results, map[string]any{"ok": true, "hits": searchHitsJSON(hits)})
						}
						return results, nil
					}
					t.Fatalf("未知的 op：%q", c.Op)
					return nil, nil
				})

			ftsCalls, semanticCalls := source.snapshot()
			mu.Lock()
			got["onWarnings"] = append([]string{}, onWarnings...)
			mu.Unlock()
			got["getCalls"] = nonNil(store.getCalls)
			got["wikiCalls"] = nonNil(store.wikiCalls)
			got["ftsCalls"] = ftsCalls
			got["semanticCalls"] = semanticCalls
			got["deltas"] = nonNil(deltas)
			assertOutcome(t, c.Expect, got)
		})
	}
}
