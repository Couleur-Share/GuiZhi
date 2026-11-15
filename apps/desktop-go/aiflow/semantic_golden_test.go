package aiflow

// 语义域的 golden 对拍：分块、RRF 融合、索引循环、查询向量缓存、混合检索。
// 夹具来自 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-semantic.test.ts（真实 TS 实现 + 假传输 / 假 window.api 导出）。
//
// 取消类错误只比 aborted、不比文案（TS 里 signal.throwIfAborted() 抛的 reason 在 Node 与 Chromium 里文案不同）。
// 并发语义（合并同一次请求、一方取消不影响其他等待者）无法在 TS 里确定性复现，见 queryvec_test.go。

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- 公用 ----

// flowOutcome 汇总一次流程的外部可见行为，与 TS 的 FlowOutcome 同形；取消类错误的文案置空（只比 aborted）。
func flowOutcome(fake *fakeTransport, warns *warnRecorder, usage *usageRecorder, result any, err error) map[string]any {
	var errInfo any
	if err != nil {
		message := err.Error()
		if IsAborted(err) {
			message = ""
		}
		info := map[string]any{"message": message, "aborted": IsAborted(err)}
		if IsNotConfigured(err) {
			info["notConfigured"] = true
		}
		errInfo = info
		result = nil
	}
	return map[string]any{
		"requests":     compactRequests(fake.Requests),
		"unusedScript": len(fake.steps),
		"usage":        nonNil(usage.Entries),
		"warnings":     nonNil(warns.Messages),
		"result":       result,
		"error":        errInfo,
	}
}

// newSemanticRunner 按设置与脚本搭一个 Runner，返回它与各记录器。
func newSemanticRunner(settings map[string]any, script []scriptStep, cancel context.CancelFunc) (*Runner, *fakeTransport, *warnRecorder, *usageRecorder) {
	fake := newFakeTransport(script, cancel)
	warns := &warnRecorder{}
	usage := &usageRecorder{}
	runner := &Runner{
		Client: &Client{Transport: fake, Log: warns},
		Config: staticConfig{NormalizeAISettings(settings)},
		Usage:  usage,
		Log:    warns,
	}
	return runner, fake, warns, usage
}

// goldenHit 是 golden 里的一条命中（QaSearchHit）。
type goldenHit struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Snippet       string    `json:"snippet"`
	MatchText     *string   `json:"matchText"`
	ReviewStatus  string    `json:"reviewStatus"`
	ReviewReasons *[]string `json:"reviewReasons"`
}

func (h goldenHit) toSearchHit() SearchHit {
	hit := SearchHit{ID: h.ID, Title: h.Title, Snippet: h.Snippet, ReviewStatus: h.ReviewStatus}
	if h.MatchText != nil {
		hit.MatchText = *h.MatchText
	}
	if h.ReviewReasons != nil {
		hit.ReviewReasons = append([]string{}, *h.ReviewReasons...)
	}
	return hit
}

func toSearchHits(list []goldenHit) []SearchHit {
	out := make([]SearchHit, len(list))
	for i, h := range list {
		out[i] = h.toSearchHit()
	}
	return out
}

// searchHitsJSON 把命中转成 golden 的形状：可选字段有值才出现（TS 里是 undefined 就不序列化）。
func searchHitsJSON(hits []SearchHit) []map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		m := map[string]any{"id": h.ID, "title": h.Title, "snippet": h.Snippet}
		if h.MatchText != "" {
			m["matchText"] = h.MatchText
		}
		if h.ReviewStatus != "" {
			m["reviewStatus"] = h.ReviewStatus
		}
		if h.ReviewReasons != nil {
			m["reviewReasons"] = h.ReviewReasons
		}
		out = append(out, m)
	}
	return out
}

// ---- 分块与融合 ----

func TestSemanticChunkAndMergeGolden(t *testing.T) {
	var golden struct {
		Constants struct {
			ChunkSize    int `json:"chunkSize"`
			ChunkOverlap int `json:"chunkOverlap"`
			MaxChunks    int `json:"maxChunks"`
		} `json:"constants"`
		Chunks []struct {
			Name       string    `json:"name"`
			Title      textSpec  `json:"title"`
			Content    textSpec  `json:"content"`
			Transcript *textSpec `json:"transcript"`
			Inputs     struct {
				Title      textDigest  `json:"title"`
				Content    textDigest  `json:"content"`
				Transcript *textDigest `json:"transcript"`
			} `json:"inputs"`
			Count   int   `json:"count"`
			Lengths []int `json:"lengths"`
			Chunks  []any `json:"chunks"`
		} `json:"chunks"`
		Merge []struct {
			Name     string      `json:"name"`
			FTS      []goldenHit `json:"fts"`
			Semantic []goldenHit `json:"semantic"`
			Limit    int         `json:"limit"`
			Result   []any       `json:"result"`
		} `json:"merge"`
	}
	loadGolden(t, "semantic", &golden)
	if len(golden.Chunks) < 15 || len(golden.Merge) < 12 {
		t.Fatalf("golden 用例数异常：chunks=%d merge=%d", len(golden.Chunks), len(golden.Merge))
	}
	if golden.Constants.ChunkSize != SemanticChunkSize || golden.Constants.ChunkOverlap != SemanticChunkOverlap || golden.Constants.MaxChunks != SemanticMaxChunks {
		t.Fatalf("分块常量与 TS 不一致：TS=%+v Go=%d/%d/%d", golden.Constants, SemanticChunkSize, SemanticChunkOverlap, SemanticMaxChunks)
	}

	for _, c := range golden.Chunks {
		t.Run("分块/"+c.Name, func(t *testing.T) {
			title, content := c.Title.String(), c.Content.String()
			checkInput(t, title, c.Inputs.Title)
			checkInput(t, content, c.Inputs.Content)
			var transcript *string
			if c.Transcript != nil {
				text := c.Transcript.String()
				if c.Inputs.Transcript == nil {
					t.Fatal("golden 缺少转写稿的指纹")
				}
				checkInput(t, text, *c.Inputs.Transcript)
				transcript = &text
			}
			chunks := BuildEmbeddingChunks(title, content, transcript)
			lengths := make([]int, len(chunks))
			compact := make([]any, len(chunks))
			for i, chunk := range chunks {
				lengths[i] = u16Len(chunk)
				compact[i] = compactText(chunk)
			}
			assertSame(t, "块数", c.Count, len(chunks))
			assertSame(t, "各块长度", nonNil(c.Lengths), nonNil(lengths))
			assertSame(t, "各块内容", nonNil(c.Chunks), nonNil(compact))
		})
	}

	for _, c := range golden.Merge {
		t.Run("融合/"+c.Name, func(t *testing.T) {
			got := MergeHybridResults(toSearchHits(c.FTS), toSearchHits(c.Semantic), c.Limit)
			assertSame(t, "融合结果", nonNil(c.Result), searchHitsJSON(got))
		})
	}
}

// ---- 索引循环 ----

type goldenIndexItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Content     textSpec  `json:"content"`
	Transcript  *textSpec `json:"transcript"`
	ContentHash string    `json:"contentHash"`
}

// goldenBatch 是一批待索引条目，或一次 listPending 失败 {"throws": "…"}。
type goldenBatch struct {
	Items  []goldenIndexItem
	Throws *string
}

func (b *goldenBatch) UnmarshalJSON(data []byte) error {
	var items []goldenIndexItem
	if err := json.Unmarshal(data, &items); err == nil {
		b.Items = items
		return nil
	}
	var failure struct {
		Throws *string `json:"throws"`
	}
	if err := json.Unmarshal(data, &failure); err != nil || failure.Throws == nil {
		return errors.New("无法解析批次：既不是条目数组也不是 {throws}")
	}
	b.Throws = failure.Throws
	return nil
}

// fakeIndexStore 按脚本回放 listPending / applyEmbeddings 并记录调用。
type fakeIndexStore struct {
	mu         sync.Mutex
	batches    []goldenBatch
	applies    map[string]json.RawMessage
	listCalls  []map[string]any
	applyCalls []map[string]any
}

func (s *fakeIndexStore) ListPending(_ context.Context, model string, limit int) ([]PendingSemanticItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls = append(s.listCalls, map[string]any{"model": model, "limit": limit})
	if len(s.batches) == 0 {
		return []PendingSemanticItem{}, nil
	}
	batch := s.batches[0]
	s.batches = s.batches[1:]
	if batch.Throws != nil {
		return nil, errors.New(*batch.Throws)
	}
	out := make([]PendingSemanticItem, len(batch.Items))
	for i, item := range batch.Items {
		out[i] = PendingSemanticItem{ID: item.ID, Title: item.Title, Content: item.Content.String(), ContentHash: item.ContentHash}
		if item.Transcript != nil {
			text := item.Transcript.String()
			out[i].Transcript = &text
		}
	}
	return out, nil
}

func (s *fakeIndexStore) ApplyEmbeddings(_ context.Context, in ApplySemanticEmbeddingsInput) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chunks := make([]map[string]any, len(in.Chunks))
	for i, c := range in.Chunks {
		chunks[i] = map[string]any{"text": compactText(c.Text), "vector": c.Vector}
	}
	s.applyCalls = append(s.applyCalls, map[string]any{
		"itemId": in.ItemID, "contentHash": in.ContentHash, "model": in.Model, "dims": in.Dims, "chunks": chunks,
	})
	raw, ok := s.applies[in.ItemID]
	if !ok {
		return true, nil
	}
	var failure struct {
		Throws *string `json:"throws"`
	}
	if string(raw) == "false" {
		return false, nil
	}
	if err := json.Unmarshal(raw, &failure); err != nil || failure.Throws == nil {
		return false, errors.New("golden 里的落库行为无法解析：" + string(raw))
	}
	return false, errors.New(*failure.Throws)
}

func TestSemanticIndexGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name              string                     `json:"name"`
			Settings          map[string]any             `json:"settings"`
			Batches           []goldenBatch              `json:"batches"`
			Applies           map[string]json.RawMessage `json:"applies"`
			Abort             *string                    `json:"abort"`
			AbortAfterIndexed *int                       `json:"abortAfterIndexed"`
			Script            []scriptStep               `json:"script"`
			Inputs            []struct {
				ID         string      `json:"id"`
				Content    textDigest  `json:"content"`
				Transcript *textDigest `json:"transcript"`
			} `json:"inputs"`
			Expect map[string]any `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "semantic-index", &golden)
	if len(golden.Cases) < 18 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			// 先核对重建出的输入与 TS 的指纹一致
			position := 0
			for _, batch := range c.Batches {
				for _, item := range batch.Items {
					want := c.Inputs[position]
					position++
					checkInput(t, item.Content.String(), want.Content)
					if item.Transcript != nil && want.Transcript != nil {
						checkInput(t, item.Transcript.String(), *want.Transcript)
					}
				}
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.Abort != nil && *c.Abort == "before" {
				cancel()
			}
			runner, fake, warns, usage := newSemanticRunner(c.Settings, c.Script, cancel)
			store := &fakeIndexStore{batches: append([]goldenBatch(nil), c.Batches...), applies: c.Applies}
			progress := []int{}
			res, err := runner.RunSemanticIndexing(ctx, store, func(indexed int) {
				progress = append(progress, indexed)
				if c.AbortAfterIndexed != nil && indexed == *c.AbortAfterIndexed {
					cancel()
				}
			})
			var result any
			if err == nil {
				m := map[string]any{"skipped": res.Skipped, "indexed": res.Indexed, "failed": res.Failed}
				if res.LastError != "" {
					m["lastError"] = res.LastError
				}
				result = m
			}
			got := flowOutcome(fake, warns, usage, result, err)
			got["listCalls"] = nonNil(store.listCalls)
			got["applyCalls"] = nonNil(store.applyCalls)
			got["progress"] = nonNil(progress)
			assertOutcome(t, c.Expect, got)
		})
	}
}

// ---- 查询向量 ----

type qvStep struct {
	Op      string   `json:"op"`
	ID      string   `json:"id"`
	Config  string   `json:"config"`
	Query   string   `json:"query"`
	Abort   string   `json:"abort"`
	Queries []string `json:"queries"`
	Ms      int64    `json:"ms"`
}

// qvResult 把一次 Get 的结果转成 golden 的形状；取消类错误文案置空。
func qvResult(vector []float64, err error) map[string]any {
	if err != nil {
		message := err.Error()
		if IsAborted(err) {
			message = ""
		}
		return map[string]any{"ok": false, "error": map[string]any{"message": message, "aborted": IsAborted(err)}}
	}
	return map[string]any{"ok": true, "vector": vector}
}

func TestQueryVectorsGolden(t *testing.T) {
	var golden struct {
		Configs map[string]goldenConfig `json:"configs"`
		Cases   []struct {
			Name              string         `json:"name"`
			Script            []scriptStep   `json:"script"`
			Steps             []qvStep       `json:"steps"`
			UnorderedRequests bool           `json:"unorderedRequests"`
			Expect            map[string]any `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "query-vectors", &golden)
	if len(golden.Cases) < 12 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			// 时钟会被后台协程（写缓存）与测试协程同时读，用原子量避免数据竞争
			var clockMs atomic.Int64
			clockMs.Store(1_700_000_000_000)
			qv := &QueryVectors{Now: func() time.Time { return time.UnixMilli(clockMs.Load()) }}
			runner, fake, _, usage := newSemanticRunner(nil, c.Script, nil)
			stepResults := []any{}
			requestCounts := []int{}

			for _, step := range c.Steps {
				switch step.Op {
				case "advance":
					clockMs.Add(step.Ms)
					continue
				case "reset":
					qv.Reset()
					continue
				}
				cfg := golden.Configs[step.Config].toAIConfig()
				if step.Config == "" {
					cfg = golden.Configs["A"].toAIConfig()
				}

				if step.Op == "concurrent" {
					results := make([]any, len(step.Queries))
					var wg sync.WaitGroup
					for i, query := range step.Queries {
						wg.Add(1)
						go func(i int, query string) {
							defer wg.Done()
							vector, err := qv.Get(context.Background(), runner, cfg, query)
							results[i] = qvResult(vector, err)
						}(i, query)
					}
					wg.Wait()
					stepResults = append(stepResults, map[string]any{"id": step.ID, "results": results})
					requestCounts = append(requestCounts, len(fake.Requests))
					continue
				}

				// 每步一个新的取消控制器：abortDuring 的那一步取消它，不影响后面的步骤
				ctx, cancel := context.WithCancel(context.Background())
				fake.mu.Lock()
				fake.cancel = cancel
				fake.mu.Unlock()
				if step.Abort == "before" {
					cancel()
				}
				callCtx := context.Background()
				if step.Abort != "" {
					callCtx = ctx
				}
				vector, err := qv.Get(callCtx, runner, cfg, step.Query)
				cancel()
				result := qvResult(vector, err)
				result["id"] = step.ID
				stepResults = append(stepResults, result)
				requestCounts = append(requestCounts, len(fake.Requests))
			}

			requests := make([]map[string]any, 0, len(fake.Requests))
			for _, r := range fake.Requests {
				m := map[string]any{"url": r.URL, "headers": r.Headers}
				if r.Body != nil {
					m["body"] = *r.Body
				}
				if r.TimeoutMs != nil {
					m["timeoutMs"] = *r.TimeoutMs
				}
				requests = append(requests, m)
			}
			if c.UnorderedRequests {
				sort.SliceStable(requests, func(i, j int) bool { return requests[i]["body"].(string) < requests[j]["body"].(string) })
			}
			got := map[string]any{
				"steps":         stepResults,
				"requestCounts": requestCounts,
				"requests":      requests,
				"unusedScript":  len(fake.steps),
				"usage":         nonNil(usage.Entries),
			}
			assertOutcome(t, c.Expect, got)
		})
	}
}

// ---- 混合检索 ----

type goldenFTSEntry struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Snippet       *string   `json:"snippet"`
	ReviewStatus  string    `json:"reviewStatus"`
	ReviewReasons *[]string `json:"reviewReasons"`
}

type goldenSemanticHit struct {
	ItemID        string    `json:"itemId"`
	Title         string    `json:"title"`
	Snippet       string    `json:"snippet"`
	Score         float64   `json:"score"`
	ReviewStatus  string    `json:"reviewStatus"`
	ReviewReasons *[]string `json:"reviewReasons"`
}

// fakeHybridSource 回放 FTS 与语义检索的脚本行为并记录调用。
type fakeHybridSource struct {
	fts struct {
		Entries []goldenFTSEntry `json:"entries"`
		Throws  *string          `json:"throws"`
	}
	sem struct {
		Hits          []goldenSemanticHit `json:"hits"`
		Throws        *string             `json:"throws"`
		Hang          bool                `json:"hang"`
		AbortThenHang bool                `json:"abortThenHang"`
	}
	cancel context.CancelFunc

	mu            sync.Mutex
	ftsCalls      []map[string]any
	semanticCalls []map[string]any
}

// snapshot 在锁内取走调用记录：超时或取消后语义这一路的协程可能还没退出，读取必须和它的写入同步。
func (s *fakeHybridSource) snapshot() (fts, semantic []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any{}, s.ftsCalls...), append([]map[string]any{}, s.semanticCalls...)
}

func (s *fakeHybridSource) SearchRecall(_ context.Context, query string, limit int) ([]KnowledgeListEntry, error) {
	s.mu.Lock()
	s.ftsCalls = append(s.ftsCalls, map[string]any{
		"scope": "all", "search": query, "searchMode": "recall", "includeArchived": true, "limit": limit,
	})
	s.mu.Unlock()
	if s.fts.Throws != nil {
		return nil, errors.New(*s.fts.Throws)
	}
	out := make([]KnowledgeListEntry, len(s.fts.Entries))
	for i, e := range s.fts.Entries {
		out[i] = KnowledgeListEntry{ID: e.ID, ReviewStatus: e.ReviewStatus, Title: e.Title, Snippet: e.Snippet}
		if e.ReviewReasons != nil {
			out[i].ReviewReasons = append([]string{}, *e.ReviewReasons...)
		}
	}
	return out, nil
}

func (s *fakeHybridSource) SemanticSearch(ctx context.Context, model string, vector []float64, limit int) ([]SemanticSearchHit, error) {
	s.mu.Lock()
	s.semanticCalls = append(s.semanticCalls, map[string]any{"model": model, "vector": vector, "limit": limit})
	s.mu.Unlock()
	switch {
	case s.sem.Throws != nil:
		return nil, errors.New(*s.sem.Throws)
	case s.sem.Hang:
		<-ctx.Done()
		return nil, ctx.Err()
	case s.sem.AbortThenHang:
		s.cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out := make([]SemanticSearchHit, len(s.sem.Hits))
	for i, h := range s.sem.Hits {
		out[i] = SemanticSearchHit{ReviewStatus: h.ReviewStatus, ItemID: h.ItemID, Title: h.Title, Snippet: h.Snippet, Score: h.Score}
		if h.ReviewReasons != nil {
			out[i].ReviewReasons = append([]string{}, *h.ReviewReasons...)
		}
	}
	return out, nil
}

func TestHybridSearchGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string          `json:"name"`
			Settings map[string]any  `json:"settings"`
			Query    string          `json:"query"`
			Limit    int             `json:"limit"`
			FTS      json.RawMessage `json:"fts"`
			Semantic json.RawMessage `json:"semantic"`
			Abort    *string         `json:"abort"`
			Script   []scriptStep    `json:"script"`
			Expect   map[string]any  `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "hybrid", &golden)
	if len(golden.Cases) < 12 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.Abort != nil && *c.Abort == "before" {
				cancel()
			}
			runner, fake, warns, usage := newSemanticRunner(c.Settings, c.Script, cancel)
			source := &fakeHybridSource{cancel: cancel}
			if err := json.Unmarshal(c.FTS, &source.fts); err != nil {
				t.Fatalf("解析 fts：%v", err)
			}
			if err := json.Unmarshal(c.Semantic, &source.sem); err != nil {
				t.Fatalf("解析 semantic：%v", err)
			}
			// 挂起的用例靠超时收场（TS 侧把 3 秒缩成了 150 毫秒）；其余用例给足时间，避免负载高时误触发
			timeout := 5 * time.Second
			if source.sem.Hang {
				timeout = 150 * time.Millisecond
			}
			h := &Hybrid{Runner: runner, Source: source, Vectors: &QueryVectors{}, SemanticTimeout: timeout}

			var warnMu sync.Mutex
			onWarnings := []string{}
			hits, err := h.HybridSearchItems(ctx, c.Query, c.Limit, func(message string) {
				warnMu.Lock()
				defer warnMu.Unlock()
				onWarnings = append(onWarnings, message)
			})
			var result any
			if err == nil {
				result = searchHitsJSON(hits)
			}
			got := flowOutcome(fake, warns, usage, result, err)
			ftsCalls, semanticCalls := source.snapshot()
			got["ftsCalls"] = ftsCalls
			got["semanticCalls"] = semanticCalls
			warnMu.Lock()
			got["onWarnings"] = append([]string{}, onWarnings...)
			warnMu.Unlock()
			assertOutcome(t, c.Expect, got)
		})
	}
}
