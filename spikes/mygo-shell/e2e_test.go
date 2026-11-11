package main

// 端到端：网页采集 → 正文提取 → 入库 → 全文检索 → 本文问答取上下文 → AI 流式问答。
// 与应用启动同一条装配路径（initDatabaseFile、registerImport、registerList、registerArticleAsk、aiProxy），
// 经 Bridge 的频道处理器调用，和渲染进程发起的调用一致；网页与模型服务都是本地 httptest，不访问真实网络。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

func e2eInvoke(t *testing.T, b *Bridge, channel string, args ...any) any {
	t.Helper()
	h, ok := b.handlers[channel]
	if !ok {
		t.Fatalf("频道未登记: %s", channel)
	}
	raw := make([]json.RawMessage, len(args))
	for i, a := range args {
		bs, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		raw[i] = bs
	}
	v, err := h(context.Background(), raw)
	if err != nil {
		t.Fatalf("%s: %v", channel, err)
	}
	// 统一成通用 JSON 结构，便于断言
	bs, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(bs, &out)
	return out
}

func TestEndToEndCaptureSearchAndStreamingAsk(t *testing.T) {
	ctx := context.Background()
	userData := t.TempDir()
	paths := Paths{UserData: userData}
	if err := os.MkdirAll(paths.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := paths.DatabasePath()
	lease, err := initDatabaseFile(ctx, dbPath, dbInitOptions{BackupsDir: paths.BackupsDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	db, err := sqlite.Open(ctx, dbPath, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 1) 站点：一篇静态文章（正文含独有词「量子退火」）
	const article = `<html><head><title>量子退火入门</title></head><body><main><article>
<h1>量子退火入门</h1><p>作者：李四</p>
<p>量子退火是一种利用量子涨落寻找组合优化问题全局最优解的方法，它从一个容易制备的基态出发，缓慢改变哈密顿量，使系统停留在最低能量态。</p>
<p>与模拟退火不同，量子退火可以借助量子隧穿穿过能量势垒，而不必翻越它，因此在崎岖的能量地形上可能更快收敛到全局最优解。</p>
<p>典型应用包括旅行商问题、投资组合优化和蛋白质折叠，归知把这类资料收进本地知识库后，可以直接对着正文提问。</p>
</article></main><footer>页脚</footer></body></html>`
	site := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, article)
	}))
	articleURL := "http://site.test:" + site.port() + "/quantum"

	// 2) 模型服务：OpenAI 兼容的 SSE，记录收到的请求体
	var (
		modelMu   sync.Mutex
		modelBody string
	)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		modelMu.Lock()
		modelBody = string(raw)
		modelMu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, part := range []string{"根据资料，", "量子退火借助", "量子隧穿", "穿过势垒。"} {
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": part}}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			fl.Flush()
			time.Sleep(20 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer model.Close()
	configDir := paths.ConfigDir()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"kind":"guizhi-ai-config","version":1,"updatedAt":"2026-01-01T00:00:00.000Z","providers":[{"id":"mock","name":"mock","provider":"openai","apiUrl":%q}],"models":[],"routes":{}}`, model.URL+"/v1")
	if err := os.WriteFile(filepath.Join(configDir, "ai-models.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// 3) 装配（与 main.go 一致的子集）
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	l := &Library{db: db, dbPath: dbPath, configDir: configDir, paths: paths, webFetcher: site.f}
	l.setPush(func(string, any) {})
	l.registerList(b)
	l.registerImport(b)
	l.registerArticleAsk(b)
	var (
		chunkMu sync.Mutex
		chunks  []string
	)
	proxy := newAIProxy(l.configuredAIHosts)
	proxy.emit = func(channel string, p aiStreamPayload) {
		if channel == chAIHTTPStreamChunk {
			chunkMu.Lock()
			chunks = append(chunks, p.Chunk)
			chunkMu.Unlock()
		}
	}
	proxy.register(b)

	// 4) 采集：入队 → 等任务结束
	e2eInvoke(t, b, "import:enqueue", []any{map[string]any{"kind": "url", "input": articleURL}})
	deadline := time.Now().Add(60 * time.Second)
	var state map[string]any
	for time.Now().Before(deadline) {
		state, _ = e2eInvoke(t, b, "import:queueState").(map[string]any)
		running, _ := state["runningCount"].(float64)
		pending, _ := state["pendingCount"].(float64)
		if running == 0 && pending == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 5) 全文检索：用正文里的独有词搜到刚入库的条目
	list, _ := e2eInvoke(t, b, "knowledge:list", map[string]any{"search": "量子退火"}).(map[string]any)
	items, _ := list["entries"].([]any)
	if len(items) != 1 {
		t.Fatalf("全文检索应命中 1 条，实际 %d（队列状态 %v，列表 %v）", len(items), state, list)
	}
	item, _ := items[0].(map[string]any)
	itemID, _ := item["id"].(string)
	if itemID == "" || !strings.Contains(fmt.Sprint(item["title"]), "量子退火") {
		t.Fatalf("入库条目不符: %v", item)
	}
	// 没有出现在无关词的检索里
	if other, _ := e2eInvoke(t, b, "knowledge:list", map[string]any{"search": "区块链"}).(map[string]any); len(other["entries"].([]any)) != 0 {
		t.Errorf("无关词不应命中")
	}

	// 6) 本文问答：按条目取正文上下文（含来源指纹与分块）
	ask := e2eInvoke(t, b, chArticleAskContext, map[string]any{"target": map[string]any{"itemId": itemID, "view": "body"}, "question": "量子退火为什么能避开局部最优？"}).(map[string]any)
	if ok, _ := ask["success"].(bool); !ok {
		t.Fatalf("取上下文失败: %v", ask)
	}
	ctxText := fmt.Sprint(ask["context"])
	if !strings.Contains(ctxText, "量子隧穿") && !strings.Contains(fmt.Sprint(ask), "量子隧穿") {
		t.Fatalf("上下文里没有文章正文: %v", ask)
	}

	// 7) AI 流式问答：把上下文放进提示词，经代理流式取回，逐块到达
	prompt := "请只依据下面的资料回答。\n资料：" + fmt.Sprint(ask["context"]) + "\n问题：量子退火为什么能避开局部最优？"
	body, _ := json.Marshal(map[string]any{"model": "mock", "stream": true, "messages": []any{map[string]any{"role": "user", "content": prompt}}})
	bodyStr := string(body)
	resp := e2eInvoke(t, b, chAIHTTPStream, map[string]any{
		"requestId": "e2e-1", "method": "POST", "url": model.URL + "/v1/chat/completions",
		"headers": map[string]any{"Content-Type": "application/json"}, "body": bodyStr,
	}).(map[string]any)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("流式请求失败: %v", resp)
	}
	chunkMu.Lock()
	got := strings.Join(chunks, "")
	n := len(chunks)
	chunkMu.Unlock()
	if n == 0 || !strings.Contains(got, "量子隧穿") || !strings.Contains(got, "穿过势垒") {
		t.Fatalf("没有收到流式回答：%d 块 %q", n, got)
	}
	modelMu.Lock()
	seen := modelBody
	modelMu.Unlock()
	if !strings.Contains(seen, "量子涨落") && !strings.Contains(seen, "量子隧穿") {
		t.Errorf("模型服务收到的提示词里没有文章内容：%.300s", seen)
	}
	t.Logf("端到端通过：条目 %s，流式 %d 块 %q", itemID, n, got)
}
