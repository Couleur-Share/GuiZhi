package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// AI 模型服务适配器的集成测试：全程使用临时目录（newLibraryAIAdapterForTest 内部的 t.TempDir）与本地 httptest 假端点，
// 经真实的 settings / ai:httpRequest / ai:usage* 处理器往返；绝不触碰真实的 %APPDATA%\GuiZhi。

const adapterTestSecret = "sk-adapter-secret-key-0001"

// syncBuffer 是并发安全的日志缓冲：代理与适配器在不同 goroutine 里写日志。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// captureLog 接管标准日志，测试结束还原；返回的缓冲用来断言「密钥没进日志」。
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	return buf
}

func newSettingsAIAdapterForTest(t *testing.T) (aiSettingsAdapter, *Bridge) {
	t.Helper()
	_, b, _ := newLibraryAIAdapterForTest(t)
	return aiSettingsAdapter{b: b}, b
}

// aiConfigForEndpoint 造一份指向 url 的配置：1 个端点、对话与向量各 1 个模型，主文本路由指向对话模型。
func aiConfigForEndpoint(url string) services.AIConfig {
	return services.AIConfig{
		Providers: []services.AIProvider{{ID: "p1", Name: "Fake", Provider: "openai", APIProtocol: "openai", APIKey: adapterTestSecret, APIURL: url, Enabled: true}},
		Models: []services.AIModel{
			{ID: "m1", ProviderID: "p1", Provider: "openai", APIProtocol: "openai", APIKey: adapterTestSecret, APIURL: url, Model: "gpt-4o", IsDefault: true, Enabled: true,
				Capabilities: services.AICapabilities{Chat: true, Vision: true}, LastVerifiedAt: "2026-11-22T10:00:00.000Z"},
			{ID: "m2", ProviderID: "p1", Provider: "openai", APIProtocol: "openai", APIKey: adapterTestSecret, APIURL: url, Model: "text-embedding-3-small", Enabled: true,
				Capabilities: services.AICapabilities{Embedding: true}},
		},
		Routes: map[string]string{services.AIRouteMainText: "m1", services.AIRouteEmbedding: "m2"},
	}
}

func TestSettingsAIAdapterRoundTrip(t *testing.T) {
	ctx := t.Context()
	ad, _ := newSettingsAIAdapterForTest(t)

	empty, err := ad.Load(ctx)
	if err != nil || len(empty.Providers) != 0 || len(empty.Models) != 0 || empty.Routes == nil {
		t.Fatalf("空配置应读出空列表而不是 nil：%v %v", empty, err)
	}

	want := aiConfigForEndpoint("https://api.example.com/v1")
	if err := ad.Save(ctx, want); err != nil {
		t.Fatalf("保存：%v", err)
	}
	got, err := ad.Load(ctx)
	if err != nil {
		t.Fatalf("读取：%v", err)
	}
	if len(got.Providers) != 1 || got.Providers[0] != want.Providers[0] {
		t.Fatalf("端点应原样往返：%v", got.Providers)
	}
	if len(got.Models) != 2 || got.Models[0] != want.Models[0] || got.Models[1] != want.Models[1] {
		t.Fatalf("模型应原样往返（含能力与 lastVerifiedAt）：%v", got.Models)
	}
	if got.Routes[services.AIRouteMainText] != "m1" || got.Routes[services.AIRouteEmbedding] != "m2" {
		t.Fatalf("路由应原样往返：%v", got.Routes)
	}
}

func TestSettingsAIAdapterSaveFailureKeepsOldConfig(t *testing.T) {
	ctx := t.Context()
	ad, _ := newSettingsAIAdapterForTest(t)

	good := aiConfigForEndpoint("https://api.example.com/v1")
	if err := ad.Save(ctx, good); err != nil {
		t.Fatalf("保存：%v", err)
	}
	bad := good.Clone()
	bad.Providers[0].APIURL = "" // 主进程校验要求 apiUrl 非空
	err := ad.Save(ctx, bad)
	if err == nil {
		t.Fatal("非法端点应被主进程拒绝")
	}
	if strings.Contains(err.Error(), adapterTestSecret) {
		t.Fatalf("保存失败的原因不应带出密钥：%v", err)
	}
	got, err := ad.Load(ctx)
	if err != nil || got.Providers[0].APIURL != good.Providers[0].APIURL {
		t.Fatalf("失败后磁盘上的配置应保持原样：%v %v", got.Providers, err)
	}
}

func TestSettingsAIAdapterTestModelChatAndAuthFailure(t *testing.T) {
	ctx := t.Context()
	logs := captureLog(t)
	ad, _ := newSettingsAIAdapterForTest(t)

	var mu sync.Mutex
	var auths []string
	reject := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		deny := reject
		mu.Unlock()
		if deny {
			// 恶意或粗心的中转站把收到的密钥回显在错误里：适配器返回的错误必须抹掉它。
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided: `+adapterTestSecret+`"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	}))
	defer srv.Close()

	cfg := aiConfigForEndpoint(srv.URL)
	if err := ad.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	out, err := ad.TestModel(ctx, cfg.Models[0])
	if err != nil || !out.Success || out.Error != "" {
		t.Fatalf("对话测试应成功：%+v %v", out, err)
	}
	mu.Lock()
	if len(auths) != 1 || auths[0] != "Bearer "+adapterTestSecret {
		t.Fatalf("请求应带 Bearer 密钥（经代理转发）：%v", auths)
	}
	reject = true
	mu.Unlock()

	out, err = ad.TestModel(ctx, cfg.Models[0])
	if err != nil {
		t.Fatalf("鉴权失败是结果而不是 Go 错误：%v", err)
	}
	if out.Success || out.Error == "" {
		t.Fatalf("应报告失败并带原因：%+v", out)
	}
	if strings.Contains(out.Error, adapterTestSecret) {
		t.Fatalf("失败原因回显了密钥：%q", out.Error)
	}
	if strings.Contains(logs.String(), adapterTestSecret) {
		t.Fatalf("密钥进了日志：%s", logs.String())
	}
}

func TestSettingsAIAdapterTestModelEmbeddingRecordsUsage(t *testing.T) {
	ctx := t.Context()
	logs := captureLog(t)
	ad, _ := newSettingsAIAdapterForTest(t)

	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.6,0.8]}],"usage":{"prompt_tokens":2}}`)
	}))
	defer srv.Close()

	cfg := aiConfigForEndpoint(srv.URL)
	if err := ad.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	out, err := ad.TestModel(ctx, cfg.Models[1])
	if err != nil || !out.Success {
		t.Fatalf("向量测试应成功：%+v %v", out, err)
	}
	if !strings.HasSuffix(gotPath, "/embeddings") || gotBody["model"] != "text-embedding-3-small" {
		t.Fatalf("应走 embeddings 端点并带模型名：%s %v", gotPath, gotBody)
	}
	sum, err := ad.UsageSummary(ctx, 30)
	if err != nil || sum.Calls != 1 || len(sum.ByScenario) != 1 || sum.ByScenario[0].Scenario != "embedding" {
		t.Fatalf("向量测试应计入用量（与渲染层一致）：%+v %v", sum, err)
	}
	if strings.Contains(logs.String(), adapterTestSecret) {
		t.Fatalf("密钥进了日志：%s", logs.String())
	}
}

func TestSettingsAIAdapterTestModelImageGenUnsupported(t *testing.T) {
	ad, _ := newSettingsAIAdapterForTest(t)
	out, err := ad.TestModel(t.Context(), services.AIModel{ID: "x", Model: "dall-e-3", APIURL: "http://127.0.0.1:1", APIKey: adapterTestSecret,
		Capabilities: services.AICapabilities{ImageGeneration: true}})
	if err != nil || out.Success || out.Error == "" {
		t.Fatalf("文生图不提供连通测试，应直接给出原因而不发请求：%+v %v", out, err)
	}
}

func TestSettingsAIAdapterFetchModels(t *testing.T) {
	ctx := t.Context()
	logs := captureLog(t)
	ad, _ := newSettingsAIAdapterForTest(t)

	mode := "ok"
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		m := mode
		mu.Unlock()
		switch m {
		case "auth":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "bad key "+adapterTestSecret)
		case "missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			_, _ = io.WriteString(w, `{"data":[{"id":"b-model","owned_by":"acme"},{"id":"a-model"}]}`)
		}
	}))
	defer srv.Close()

	cfg := aiConfigForEndpoint(srv.URL)
	if err := ad.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	res, err := ad.FetchModels(ctx, srv.URL, adapterTestSecret, "openai")
	if err != nil || !res.Success || len(res.Models) != 2 || res.Models[0].ID != "a-model" || res.Models[1].OwnedBy != "acme" {
		t.Fatalf("应拉到按 ID 排序的模型列表：%+v %v", res, err)
	}

	mu.Lock()
	mode = "auth"
	mu.Unlock()
	res, err = ad.FetchModels(ctx, srv.URL, adapterTestSecret, "openai")
	if err != nil || res.Success || res.Reason != services.AIFetchReasonAuth {
		t.Fatalf("401 应归为鉴权失败：%+v %v", res, err)
	}
	if strings.Contains(res.Error, adapterTestSecret) {
		t.Fatalf("失败原因回显了密钥：%q", res.Error)
	}
	mu.Lock()
	mode = "missing"
	mu.Unlock()
	if res, _ = ad.FetchModels(ctx, srv.URL, adapterTestSecret, "openai"); res.Success || res.Reason != services.AIFetchReasonUnsupported {
		t.Fatalf("404 应归为不支持：%+v", res)
	}

	if _, err := ad.FetchModels(ctx, srv.URL, "  ", "openai"); err == nil {
		t.Fatal("缺密钥应直接报错而不发请求")
	}
	if strings.Contains(logs.String(), adapterTestSecret) {
		t.Fatalf("密钥进了日志：%s", logs.String())
	}
}

// SSRF：拉取模型与测试连接都必须经 ai:httpRequest 代理，链路本地地址（云元数据端点）被守卫挡掉，请求根本不会发出。
func TestSettingsAIAdapterBlocksForbiddenEndpointsThroughProxy(t *testing.T) {
	ctx := t.Context()
	logs := captureLog(t)
	ad, _ := newSettingsAIAdapterForTest(t)

	res, err := ad.FetchModels(ctx, "http://169.254.169.254/v1", adapterTestSecret, "openai")
	if err != nil || res.Success || res.Error == "" {
		t.Fatalf("拉取应被 SSRF 守卫拒绝：%+v %v", res, err)
	}
	if strings.Contains(res.Error, adapterTestSecret) {
		t.Fatalf("失败原因带出了密钥：%q", res.Error)
	}
	out, err := ad.TestModel(ctx, services.AIModel{ID: "x", Provider: "openai", APIProtocol: "openai", APIKey: adapterTestSecret,
		APIURL: "http://169.254.169.254/v1", Model: "gpt", Enabled: true, Capabilities: services.AICapabilities{Chat: true}})
	if err != nil || out.Success || out.Error == "" || strings.Contains(out.Error, adapterTestSecret) {
		t.Fatalf("测试连接应被 SSRF 守卫拒绝且不带出密钥：%+v %v", out, err)
	}
	if strings.Contains(logs.String(), adapterTestSecret) {
		t.Fatalf("密钥进了日志：%s", logs.String())
	}
}

func TestSettingsAIAdapterUsageSummaryAndClear(t *testing.T) {
	ctx := t.Context()
	ad, b := newSettingsAIAdapterForTest(t)

	empty, err := ad.UsageSummary(ctx, 30)
	if err != nil || empty.Calls != 0 || empty.ByScenario == nil || empty.ByModel == nil {
		t.Fatalf("没有用量时应读出空列表：%+v %v", empty, err)
	}
	for _, e := range []map[string]any{
		{"scenario": "summary", "model": "m-a", "promptTokens": 10, "completionTokens": 5},
		{"scenario": "summary", "model": "m-a", "promptTokens": 1, "completionTokens": 1, "failed": true},
		{"scenario": "qa", "model": "m-b", "promptTokens": 100, "completionTokens": 50},
	} {
		if _, err := b.call(ctx, "ai:usageRecord", e); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := ad.UsageSummary(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Days != 30 || sum.Calls != 3 || sum.FailedCalls != 1 || sum.PromptTokens != 111 || sum.CompletionTokens != 56 {
		t.Fatalf("合计不对：%+v", sum)
	}
	if len(sum.ByScenario) != 2 || sum.ByScenario[0].Scenario != "summary" || sum.ByScenario[0].Calls != 2 || sum.ByScenario[0].FailedCalls != 1 {
		t.Fatalf("按场景汇总不对：%+v", sum.ByScenario)
	}
	if len(sum.ByModel) != 2 || sum.ByModel[0].Model != "m-a" {
		t.Fatalf("按模型汇总不对：%+v", sum.ByModel)
	}

	if err := ad.ClearUsage(ctx); err != nil {
		t.Fatal(err)
	}
	if after, _ := ad.UsageSummary(ctx, 30); after.Calls != 0 || len(after.ByScenario) != 0 {
		t.Fatalf("清空后应没有用量：%+v", after)
	}
}
