package aiflow

// 查询向量缓存与混合检索的并发语义。TS 侧同类用例在 apps/desktop/tests/unit/knowledge-ai/heavy-user-search.test.ts，
// 它们依赖摘要计算的完成顺序，没法放进 golden；这里用可阻塞的假传输确定性地验证。

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// blockingTransport 让每次 embeddings 请求阻塞到 release 被关闭，或请求自己的 ctx 被取消。
type blockingTransport struct {
	mu      sync.Mutex
	ctxs    []context.Context
	started chan struct{}
	release chan struct{}
	body    string
}

func newBlockingTransport() *blockingTransport {
	return &blockingTransport{
		started: make(chan struct{}, 16),
		release: make(chan struct{}),
		body:    `{"data":[{"index":0,"embedding":[3,4]}]}`,
	}
}

func (b *blockingTransport) Request(ctx context.Context, _ TransportRequest) (TransportResponse, error) {
	b.mu.Lock()
	b.ctxs = append(b.ctxs, ctx)
	b.mu.Unlock()
	b.started <- struct{}{}
	select {
	case <-b.release:
		return TransportResponse{OK: true, Status: 200, Body: b.body, Headers: map[string]string{}}, nil
	case <-ctx.Done():
		// 与代理一致：取消在飞行中时返回 error 响应而不是抛错
		reason := "已取消"
		return TransportResponse{OK: false, Status: 0, Error: &reason, Headers: map[string]string{}}, nil
	}
}

func (b *blockingTransport) RequestStream(context.Context, TransportRequest, StreamHandler) (TransportResponse, error) {
	return TransportResponse{}, errors.New("blockingTransport 不支持流式")
}

func (b *blockingTransport) requestCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.ctxs)
}

func (b *blockingTransport) requestCtx(t *testing.T, index int) context.Context {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if index >= len(b.ctxs) {
		t.Fatalf("只发了 %d 次请求，没有第 %d 次", len(b.ctxs), index+1)
	}
	return b.ctxs[index]
}

var qvTestConfig = AIConfig{ID: "embed", Provider: "custom", APIProtocol: "openai", APIKey: "sk-embed", APIURL: "https://embed.example.com/v1", Model: "model-embed"}

func newBlockingRunner(tr Transport) *Runner {
	return &Runner{Client: &Client{Transport: tr}, Config: staticConfig{NormalizeAISettings(embeddingSettings())}}
}

// embeddingSettings 是只配了一个 embedding 模型的设置（与 TS golden 的 WITH_EMBEDDING 同形）。
func embeddingSettings() map[string]any {
	return map[string]any{
		"aiModels": []any{map[string]any{
			"id": "embed", "provider": "custom", "apiProtocol": "openai", "apiKey": "sk-embed",
			"apiUrl": "https://embed.example.com/v1", "model": "model-embed",
			"capabilities": map[string]any{"chat": false, "embedding": true},
		}},
		"modelRouteDefaults": map[string]any{"embedding": "embed"},
	}
}

// waitUsers 等到该键的在途请求有 want 个等待者。
func waitUsers(t *testing.T, q *QueryVectors, query string, want int) {
	t.Helper()
	key := queryVectorKey(qvTestConfig, query)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		users := -1
		if entry := q.pending[key]; entry != nil {
			users = entry.users
		}
		q.mu.Unlock()
		if users == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待者数没有在 3 秒内变成 %d", want)
}

func waitStarted(t *testing.T, tr *blockingTransport, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-tr.started:
		case <-time.After(3 * time.Second):
			t.Fatalf("第 %d 次请求没有在 3 秒内发出", i+1)
		}
	}
}

func isDone(ctx context.Context, within time.Duration) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(within):
		return false
	}
}

type getResult struct {
	vector []float64
	err    error
}

func asyncGet(q *QueryVectors, ctx context.Context, r *Runner, query string) <-chan getResult {
	ch := make(chan getResult, 1)
	go func() {
		vector, err := q.Get(ctx, r, qvTestConfig, query)
		ch <- getResult{vector, err}
	}()
	return ch
}

func recv(t *testing.T, ch <-chan getResult) getResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("调用没有在 3 秒内返回")
		return getResult{}
	}
}

func TestQueryVectorsOneCallerCancelDoesNotAffectOthers(t *testing.T) {
	tr := newBlockingTransport()
	runner := newBlockingRunner(tr)
	q := &QueryVectors{}

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	a := asyncGet(q, ctxA, runner, "同一问题")
	b := asyncGet(q, context.Background(), runner, "同一问题")
	waitStarted(t, tr, 1)
	waitUsers(t, q, "同一问题", 2)

	cancelA()
	if got := recv(t, a); !IsAborted(got.err) {
		t.Fatalf("取消的调用方应返回取消错误，实际：%v", got.err)
	}
	if isDone(tr.requestCtx(t, 0), 50*time.Millisecond) {
		t.Fatal("还有等待者时不应取消共享的底层请求")
	}

	close(tr.release)
	got := recv(t, b)
	if got.err != nil || len(got.vector) != 2 || got.vector[0] != 0.6 || got.vector[1] != 0.8 {
		t.Fatalf("另一位等待者应拿到共享结果 [0.6 0.8]，实际：%v %v", got.vector, got.err)
	}

	// 成功的结果入缓存：再取一次不再发请求
	again, err := q.Get(context.Background(), runner, qvTestConfig, "同一问题")
	if err != nil || len(again) != 2 {
		t.Fatalf("缓存命中应成功：%v %v", again, err)
	}
	if n := tr.requestCount(); n != 1 {
		t.Fatalf("共发了 %d 次请求，应为 1", n)
	}
}

func TestQueryVectorsLastWaiterCancelsRequestAndSkipsCache(t *testing.T) {
	tr := newBlockingTransport()
	runner := newBlockingRunner(tr)
	q := &QueryVectors{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := asyncGet(q, ctx, runner, "问题")
	waitStarted(t, tr, 1)
	waitUsers(t, q, "问题", 1)

	cancel()
	if got := recv(t, first); !IsAborted(got.err) {
		t.Fatalf("应返回取消错误，实际：%v", got.err)
	}
	if !isDone(tr.requestCtx(t, 0), 2*time.Second) {
		t.Fatal("最后一个等待者离开后，底层请求应被取消")
	}

	// 被取消的请求不入缓存：再取会重新发请求
	second := asyncGet(q, context.Background(), runner, "问题")
	waitStarted(t, tr, 1)
	close(tr.release)
	if got := recv(t, second); got.err != nil {
		t.Fatalf("重新请求应成功：%v", got.err)
	}
	if n := tr.requestCount(); n != 2 {
		t.Fatalf("共发了 %d 次请求，应为 2（取消的那次不入缓存）", n)
	}
}

func TestQueryVectorsResetAbortsInFlightRequests(t *testing.T) {
	tr := newBlockingTransport()
	runner := newBlockingRunner(tr)
	q := &QueryVectors{}

	waiter := asyncGet(q, context.Background(), runner, "问题")
	waitStarted(t, tr, 1)
	waitUsers(t, q, "问题", 1)

	q.Reset()
	got := recv(t, waiter)
	if !IsAborted(got.err) {
		t.Fatalf("Reset 应中止在途请求并让等待者收到取消错误，实际：%v %v", got.vector, got.err)
	}
	if !isDone(tr.requestCtx(t, 0), 2*time.Second) {
		t.Fatal("Reset 后底层请求应被取消")
	}
}

func TestQueryVectorsReturnsCopies(t *testing.T) {
	tr := newBlockingTransport()
	close(tr.release)
	runner := newBlockingRunner(tr)
	q := &QueryVectors{}

	first, err := q.Get(context.Background(), runner, qvTestConfig, "问题")
	if err != nil {
		t.Fatal(err)
	}
	first[0] = 99 // 调用方改了自己拿到的向量，不能污染缓存
	second, err := q.Get(context.Background(), runner, qvTestConfig, "问题")
	if err != nil {
		t.Fatal(err)
	}
	if second[0] != 0.6 {
		t.Fatalf("缓存被调用方的修改污染了：%v", second)
	}
}

type staticHybridSource struct {
	entries []KnowledgeListEntry
}

func (s staticHybridSource) SearchRecall(context.Context, string, int) ([]KnowledgeListEntry, error) {
	return s.entries, nil
}

func (s staticHybridSource) SemanticSearch(context.Context, string, []float64, int) ([]SemanticSearchHit, error) {
	return nil, errors.New("不应走到这里：embedding 请求一直没有返回")
}

func TestHybridSearchTimesOutOnHungEmbeddingAndCancelsIt(t *testing.T) {
	tr := newBlockingTransport() // 永不放行：模拟 embedding 接口挂起
	runner := newBlockingRunner(tr)
	snippet := "正文"
	h := &Hybrid{
		Runner:          runner,
		Source:          staticHybridSource{entries: []KnowledgeListEntry{{ID: "local", Title: "本地命中", Snippet: &snippet}}},
		Vectors:         &QueryVectors{},
		SemanticTimeout: 80 * time.Millisecond,
	}

	var mu sync.Mutex
	var warnings []string
	began := time.Now()
	hits, err := h.HybridSearchItems(context.Background(), "问题", 5, func(message string) {
		mu.Lock()
		defer mu.Unlock()
		warnings = append(warnings, message)
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(began); elapsed > 2*time.Second {
		t.Fatalf("挂起的 embedding 不应拖住检索：耗时 %v", elapsed)
	}
	if len(hits) != 1 || hits[0].ID != "local" {
		t.Fatalf("应退回 FTS 结果，实际：%+v", hits)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(warnings) != 1 || warnings[0] != "语义检索等待超过 3 秒，本次使用本地关键词结果。" {
		t.Fatalf("应有且仅有一条超时提示，实际：%q", warnings)
	}
	if !isDone(tr.requestCtx(t, 0), 2*time.Second) {
		t.Fatal("最后等待者（超时）离开后，底层 embedding 请求应被取消")
	}
}

func TestHybridSearchDoesNotWarnAfterFTSFailure(t *testing.T) {
	// FTS 先失败时整体返回该错误；被收尾的语义这一路不应再弹「暂不可用」，也不能在返回之后才回调
	tr := newBlockingTransport()
	runner := newBlockingRunner(tr)
	h := &Hybrid{Runner: runner, Source: failingFTS{}, Vectors: &QueryVectors{}, SemanticTimeout: time.Second}

	var mu sync.Mutex
	calls := 0
	_, err := h.HybridSearchItems(context.Background(), "问题", 5, func(string) {
		mu.Lock()
		defer mu.Unlock()
		calls++
	})
	if err == nil || err.Error() != "fts index corrupted" {
		t.Fatalf("应返回 FTS 的错误，实际：%v", err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatalf("FTS 失败后不应再有语义提示回调，实际 %d 次", calls)
	}
}

type failingFTS struct{}

func (failingFTS) SearchRecall(context.Context, string, int) ([]KnowledgeListEntry, error) {
	return nil, errors.New("fts index corrupted")
}

func (failingFTS) SemanticSearch(context.Context, string, []float64, int) ([]SemanticSearchHit, error) {
	return nil, errors.New("不应走到这里")
}
