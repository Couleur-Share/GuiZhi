package aiflow

// 本文问答的补充测试：golden 覆盖不到的、或有意与 TS 不同的地方。
//   - 搜索词不是合法 JSON：TS 的错误文案是 V8 的 SyntaxError 原文，不入 golden，这里只核对行为；
//   - 入参的上下文不被改写（TS 靠 structuredClone）；
//   - 存储侧调用不跟随 ctx 取消（IPC 本来就不接收 signal）；
//   - 联网搜索阻塞时，ctx 取消经 CancelSearch 解除阻塞；
//   - 补丁里的快照互相独立：TS 的 context.sources 与 sources 是同一个数组，联网资料会被追加进已保存的 context，
//     重试时又被当成本文资料带回；Go 版有意不复刻这个别名。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

var articleTestSettings = map[string]any{
	"aiModels": []any{map[string]any{
		"id": "main", "provider": "openai", "apiProtocol": "openai", "apiKey": "sk-main",
		"apiUrl": "https://api.openai.com/v1", "model": "model-main", "isDefault": true,
		"capabilities": map[string]any{"chat": true},
	}},
	"modelRouteDefaults": map[string]any{"mainText": "main", "fastText": "main"},
}

func newArticleRunner(script []scriptStep) (*Runner, *fakeTransport) {
	fake := newFakeTransport(script, nil)
	warns := &warnRecorder{}
	return &Runner{
		Client: &Client{Transport: fake, Log: warns},
		Config: staticConfig{NormalizeAISettings(articleTestSettings)},
		Usage:  &usageRecorder{},
		Log:    warns,
	}, fake
}

func plannerReply(content string) scriptStep {
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}, "finish_reason": "stop"}}})
	return scriptStep{Body: string(body)}
}

func answerReply(text string) scriptStep {
	chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": "stop"}}})
	return scriptStep{Chunks: []string{"data: " + string(chunk) + "\n"}}
}

// scriptedStore 用 JSON 文本排队应答，省去构造 golden 形状。
func scriptedStore(t *testing.T, cancel context.CancelFunc, queues map[string][]string) *fakeArticleStore {
	t.Helper()
	store := &fakeArticleStore{t: t, cancel: cancel, queues: map[string][]articleStep{}, calls: []map[string]any{}}
	for channel, results := range queues {
		for _, result := range results {
			store.queues[channel] = append(store.queues[channel], articleStep{result: json.RawMessage(result)})
		}
	}
	return store
}

const articleContextJSON = `{"success":true,"context":{"target":{"itemId":"item-1","view":"body"},"title":"T","fingerprint":"f",` +
	`"sources":[{"ordinal":1,"kind":"article","title":"T","text":"见 https://example.com/a?token=abc&x=1","target":{"itemId":"item-1","view":"body"}}],"clipped":false}}`

func TestArticlePlannerInvalidJSONBecomesWarning(t *testing.T) {
	runner, fake := newArticleRunner([]scriptStep{plannerReply("[搜索词一, 搜索词二]"), answerReply("结论[1]")})
	store := scriptedStore(t, func() {}, map[string][]string{"context": {articleContextJSON}, "get": {`{"id":"item-1"}`}})
	asker := &ArticleAsker{Runner: runner, Store: store, Now: func() int64 { return 1 }}

	var patches []ArticlePatch
	err := asker.Ask(context.Background(), AskArticleInput{
		Target: ArticleTarget{ItemID: "item-1", View: "body"}, Question: "问题", WebEnabled: true, RequestID: "r1",
	}, func(p ArticlePatch) { patches = append(patches, p) })
	if err != nil {
		t.Fatalf("规划失败应记成警告并继续回答，实际返回错误：%v", err)
	}
	if len(fake.Requests) != 2 {
		t.Fatalf("应有规划与回答两次请求，实际 %d 次", len(fake.Requests))
	}
	if store.counts("search") != 0 {
		t.Errorf("没有有效搜索词时不应调用搜索")
	}

	var warnings []string
	var failed, done bool
	for _, p := range patches {
		if p.Warnings != nil {
			warnings = p.Warnings
		}
		failed = failed || (p.WebStatus != nil && *p.WebStatus == "failed")
		done = done || (p.Status != nil && *p.Status == "done")
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "未完成联网查证：搜索词不是有效的 JSON") {
		t.Errorf("警告不符合预期：%q", warnings)
	}
	if !failed || !done {
		t.Errorf("应记联网失败并完成回答：failed=%v done=%v", failed, done)
	}
}

func (s *fakeArticleStore) counts(channel string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, call := range s.calls {
		if call["channel"] == channel || call["channel"] == "articleAsk."+channel || call["channel"] == "knowledge."+channel {
			n++
		}
	}
	return n
}

func TestArticleAskDoesNotMutateInputContext(t *testing.T) {
	runner, _ := newArticleRunner([]scriptStep{answerReply("结论[1]")})
	store := scriptedStore(t, func() {}, map[string][]string{"get": {`{"id":"item-1"}`, `{"id":"item-1"}`}})
	asker := &ArticleAsker{Runner: runner, Store: store, Now: func() int64 { return 1 }}

	var saved struct {
		Context ArticleContext `json:"context"`
	}
	if err := json.Unmarshal([]byte(articleContextJSON), &saved); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(saved.Context)

	err := asker.Ask(context.Background(), AskArticleInput{
		Target: saved.Context.Target, Question: "重试", RequestID: "r1", Context: &saved.Context,
	}, func(ArticlePatch) {})
	if err != nil {
		t.Fatalf("重试应成功：%v", err)
	}
	after, _ := json.Marshal(saved.Context)
	if string(before) != string(after) {
		t.Errorf("入参的上下文被改写\n之前：%s\n之后：%s", before, after)
	}
	if saved.Context.Sources[0].Evidence != nil || !strings.Contains(saved.Context.Sources[0].Text, "token=abc") {
		t.Errorf("入参的来源不应被补证据或脱敏：%+v", saved.Context.Sources[0])
	}
}

// ctxProbeStore 记录每次存储调用时拿到的 ctx 是否已取消。
type ctxProbeStore struct {
	mu       sync.Mutex
	canceled []bool
}

func (s *ctxProbeStore) note(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.canceled = append(s.canceled, ctx.Err() != nil)
}

func (s *ctxProbeStore) GetItem(ctx context.Context, _ string) (*ArticleItem, error) {
	s.note(ctx)
	return &ArticleItem{}, nil
}

func (s *ctxProbeStore) Context(ctx context.Context, _ ArticleTarget, _ string) (ArticleContextResult, error) {
	s.note(ctx)
	var decoded struct {
		Context *ArticleContext `json:"context"`
	}
	_ = json.Unmarshal([]byte(articleContextJSON), &decoded)
	return ArticleContextResult{Success: true, Context: decoded.Context}, nil
}

func (s *ctxProbeStore) Search(ctx context.Context, _ string, _ []string) (ArticleSearchResult, error) {
	s.note(ctx)
	return ArticleSearchResult{Success: true}, nil
}

func (s *ctxProbeStore) CancelSearch(string) {}

func TestArticleStoreCallsIgnoreCancellation(t *testing.T) {
	runner, _ := newArticleRunner(nil)
	store := &ctxProbeStore{}
	asker := &ArticleAsker{Runner: runner, Store: store, Now: func() int64 { return 1 }}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 调用前已取消：与 TS 一致，读上下文的 IPC 照常发出，随后在第一个检查点结束
	err := asker.Ask(ctx, AskArticleInput{Target: ArticleTarget{ItemID: "item-1", View: "body"}, Question: "q", RequestID: "r1"}, func(ArticlePatch) {})
	if !IsAborted(err) {
		t.Fatalf("应返回取消错误，实际：%v", err)
	}
	if len(store.canceled) != 1 || store.canceled[0] {
		t.Errorf("存储调用不应拿到已取消的 ctx：%v", store.canceled)
	}
}

// blockingSearchStore 的搜索会一直阻塞，直到收到 CancelSearch。
type blockingSearchStore struct {
	ctxProbeStore
	entered chan struct{}
	release chan struct{}
	mu2     sync.Mutex
	cancels int
}

func (s *blockingSearchStore) Search(context.Context, string, []string) (ArticleSearchResult, error) {
	close(s.entered)
	select {
	case <-s.release:
		return ArticleSearchResult{}, errAborted()
	case <-time.After(5 * time.Second):
		return ArticleSearchResult{Success: true}, nil
	}
}

func errAborted() error { return ErrAborted }

func (s *blockingSearchStore) CancelSearch(string) {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	s.cancels++
	if s.cancels == 1 {
		close(s.release)
	}
}

func TestArticleSearchCancelUnblocksStore(t *testing.T) {
	runner, _ := newArticleRunner([]scriptStep{plannerReply(`["关键词"]`)})
	store := &blockingSearchStore{entered: make(chan struct{}), release: make(chan struct{})}
	asker := &ArticleAsker{Runner: runner, Store: store, Now: func() int64 { return 1 }}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-store.entered
		cancel()
	}()

	started := time.Now()
	err := asker.Ask(ctx, AskArticleInput{
		Target: ArticleTarget{ItemID: "item-1", View: "body"}, Question: "q", WebEnabled: true, RequestID: "r1",
	}, func(ArticlePatch) {})
	if !IsAborted(err) {
		t.Fatalf("应返回取消错误，实际：%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("取消没有及时解除阻塞：%v", elapsed)
	}
	store.mu2.Lock()
	defer store.mu2.Unlock()
	if store.cancels != 1 {
		t.Errorf("CancelSearch 应恰好调用一次，实际 %d 次", store.cancels)
	}
}

func TestArticlePatchSnapshotsAreIndependent(t *testing.T) {
	runner, _ := newArticleRunner([]scriptStep{plannerReply(`["关键词"]`), answerReply("结论[1]")})
	store := scriptedStore(t, func() {}, map[string][]string{
		"context": {articleContextJSON},
		"get":     {`{"id":"item-1"}`},
		"search":  {`{"success":true,"sources":[{"ordinal":1,"kind":"web","title":"W","text":"网页","url":"https://example.com/w"}]}`},
	})
	asker := &ArticleAsker{Runner: runner, Store: store, Now: func() int64 { return 1 }}

	var patches []ArticlePatch
	err := asker.Ask(context.Background(), AskArticleInput{
		Target: ArticleTarget{ItemID: "item-1", View: "body"}, Question: "q", WebEnabled: true, RequestID: "r1",
	}, func(p ArticlePatch) { patches = append(patches, p) })
	if err != nil {
		t.Fatal(err)
	}

	var contexts, finalSources int
	var contextSources int
	for _, p := range patches {
		if p.Context != nil {
			contexts++
			contextSources = len(p.Context.Sources)
		}
		if p.Sources != nil {
			finalSources = len(p.Sources)
		}
	}
	if contexts != 1 || contextSources != 1 {
		t.Errorf("context 补丁应只有一条且只含本文资料（联网资料不得追加进去）：条数 %d，资料 %d", contexts, contextSources)
	}
	if finalSources != 2 {
		t.Errorf("最终的 sources 应含本文与联网资料共 2 份，实际 %d", finalSources)
	}
}

func TestEvidenceSnapshotJSONTriState(t *testing.T) {
	base := EvidenceSnapshot{Version: 1, Kind: "item", Title: "t", Text: "x", Fingerprint: "f", CapturedAt: 1, ReviewStatus: "clear"}
	cases := []struct {
		name    string
		mutate  func(*EvidenceSnapshot)
		want    string // 期望出现的片段
		notWant string // 不应出现的片段
	}{
		{"未设置：省略 sourceItemIds，reviewReasons 写 []", func(*EvidenceSnapshot) {}, `"reviewReasons":[]`, `sourceItemIds`},
		{"空数组：写 []", func(e *EvidenceSnapshot) { e.SourceItemIDs = []string{} }, `"sourceItemIds":[]`, ""},
		{"有值", func(e *EvidenceSnapshot) { e.SourceItemIDs = []string{"a", "b"} }, `"sourceItemIds":["a","b"]`, ""},
	}
	for _, c := range cases {
		e := base
		c.mutate(&e)
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), c.want) {
			t.Errorf("%s：缺少 %s\n%s", c.name, c.want, raw)
		}
		if c.notWant != "" && strings.Contains(string(raw), c.notWant) {
			t.Errorf("%s：不应出现 %s\n%s", c.name, c.notWant, raw)
		}
	}

	var decoded EvidenceSnapshot
	if err := json.Unmarshal([]byte(`{"version":1,"kind":"item","sourceItemIds":[],"title":"","text":"","fingerprint":"","capturedAt":0,"reviewStatus":"clear","reviewReasons":[]}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SourceItemIDs == nil {
		t.Errorf("反序列化后空数组应保持非 nil，才能区分「未设置」")
	}
}

func TestCaptureEvidenceUsesSystemClockByDefault(t *testing.T) {
	before := time.Now().UnixMilli()
	evidence := CaptureEvidence(EvidenceInput{Kind: "web", Title: "t", Text: "x"}, nil)
	after := time.Now().UnixMilli()
	if evidence.CapturedAt < before || evidence.CapturedAt > after {
		t.Errorf("采集时间应取系统时钟：%d 不在 [%d, %d]", evidence.CapturedAt, before, after)
	}
	if evidence.ReviewStatus != "clear" || evidence.ReviewReasons == nil || len(evidence.Fingerprint) != 64 {
		t.Errorf("默认值不符合预期：%+v", evidence)
	}
}
