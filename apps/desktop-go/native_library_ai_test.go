package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
)

func newLibraryAIAdapterForTest(t *testing.T) (libraryAdapter, *Bridge, *Library) {
	t.Helper()
	svc, b, l := newLibraryOpsAdapterForTest(t)
	l.configDir = l.paths.ConfigDir()
	l.registerSettings(b)
	l.registerAIUsage(b)
	l.registerAppRuntime(b)
	newAIProxy(l.configuredAIHosts).register(b)
	return svc, b, l
}

func configureAITestModels(t *testing.T, b *Bridge, srvURL, protocol, modelName string) {
	t.Helper()
	provider := "openai"
	switch protocol {
	case "anthropic":
		provider = "anthropic"
	case "gemini":
		provider = "gemini"
	}
	payload := map[string]any{
		"aiProviders": []any{
			map[string]any{
				"id":          "p1",
				"name":        "mock",
				"provider":    provider,
				"apiProtocol": protocol,
				"apiKey":      "sk-test-secret-key",
				"apiUrl":      srvURL,
				"enabled":     true,
			},
		},
		"aiModels": []any{
			map[string]any{
				"id":          "m1",
				"providerId":  "p1",
				"provider":    provider,
				"apiProtocol": protocol,
				"apiKey":      "sk-test-secret-key",
				"apiUrl":      srvURL,
				"model":       modelName,
				"isDefault":   true,
				"enabled":     true,
				"capabilities": map[string]any{
					"chat":   true,
					"vision": true,
				},
			},
		},
		"modelRouteDefaults": map[string]any{
			"mainText":   "m1",
			"fastText":   "m1",
			"visionText": "m1",
		},
	}
	if _, err := b.call(t.Context(), "settings:set", payload); err != nil {
		t.Fatalf("settings:set: %v", err)
	}
}

type usageScenarioStat struct {
	Scenario         string `json:"scenario"`
	Calls            int    `json:"calls"`
	FailedCalls      int    `json:"failedCalls"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
}

func readUsageByScenario(t *testing.T, b *Bridge) map[string]usageScenarioStat {
	t.Helper()
	res, err := callAs[struct {
		ByScenario []usageScenarioStat `json:"byScenario"`
	}](t.Context(), b, "ai:usageSummary", 7)
	if err != nil {
		t.Fatalf("ai:usageSummary: %v", err)
	}
	out := make(map[string]usageScenarioStat, len(res.ByScenario))
	for _, s := range res.ByScenario {
		out[s.Scenario] = s
	}
	return out
}

func TestLibraryAIAdapterUnconfiguredReturnsSentinel(t *testing.T) {
	ctx := t.Context()
	svc, _, _ := newLibraryAIAdapterForTest(t)

	if _, err := svc.GenerateSummary(ctx, "标题", "正文内容"); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("GenerateSummary 未配置应返回 ErrAINotConfigured，实际：%v", err)
	}
	if _, err := svc.SuggestTags(ctx, "标题", "正文内容"); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("SuggestTags 未配置应返回 ErrAINotConfigured，实际：%v", err)
	}
	if _, err := svc.RecognizeImageText(ctx, "pic.png"); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("RecognizeImageText 未配置应返回 ErrAINotConfigured，实际：%v", err)
	}
}

func TestLibraryAIAdapterSummaryAndTagsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryAIAdapterForTest(t)

	var (
		mu       sync.Mutex
		reply    string
		finish   string
		requests []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, r.URL.Path+"|"+r.Header.Get("Authorization")+"|"+string(raw))
		curReply, curFinish := reply, finish
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message":       map[string]any{"content": curReply},
					"finish_reason": curFinish,
				},
			},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 8},
		})
	}))
	defer srv.Close()
	configureAITestModels(t, b, srv.URL, "openai", "gpt-4o-mini")

	mu.Lock()
	reply, finish = "核心要点：一、二。", "stop"
	mu.Unlock()
	sum, err := svc.GenerateSummary(ctx, "测试文章", "这里是足够长的文章正文。")
	if err != nil {
		t.Fatalf("GenerateSummary 失败：%v", err)
	}
	if sum.Text != "核心要点：一、二。" || sum.Truncated {
		t.Fatalf("摘要结果不对：%+v", sum)
	}

	mu.Lock()
	reply, finish = "半截摘要内容", "length"
	mu.Unlock()
	sumTrunc, err := svc.GenerateSummary(ctx, "长文", "另一段正文。")
	if err != nil {
		t.Fatalf("GenerateSummary(length) 失败：%v", err)
	}
	if sumTrunc.Text != "半截摘要内容" || !sumTrunc.Truncated {
		t.Fatalf("Truncated 应为 true：%+v", sumTrunc)
	}

	mu.Lock()
	reply, finish = "Go, 知识库, AI", "stop"
	mu.Unlock()
	tags, err := svc.SuggestTags(ctx, "标签测试", "Go 与 AI 知识库设计")
	if err != nil {
		t.Fatalf("SuggestTags 失败：%v", err)
	}
	if !slices.Equal(tags, []string{"Go", "知识库", "AI"}) {
		t.Fatalf("建议标签不对：%v", tags)
	}

	mu.Lock()
	gotReqs := slices.Clone(requests)
	mu.Unlock()
	if len(gotReqs) != 3 {
		t.Fatalf("应发出 3 次请求，实际 %d", len(gotReqs))
	}
	for _, rq := range gotReqs {
		if !strings.HasPrefix(rq, "/v1/chat/completions|Bearer sk-test-secret-key|") {
			t.Fatalf("请求路径或鉴权不对：%q", rq)
		}
	}

	stats := readUsageByScenario(t, b)
	if s := stats["summary"]; s.Calls != 2 || s.FailedCalls != 0 || s.PromptTokens != 24 || s.CompletionTokens != 16 {
		t.Fatalf("summary 用量统计不对：%+v", s)
	}
	if s := stats["tagging"]; s.Calls != 1 || s.FailedCalls != 0 || s.PromptTokens != 12 || s.CompletionTokens != 8 {
		t.Fatalf("tagging 用量统计不对：%+v", s)
	}
}

func TestLibraryAIAdapterRecognizeImageTextAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, l := newLibraryAIAdapterForTest(t)

	if err := os.MkdirAll(l.paths.ImagesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	imgBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(l.paths.ImagesDir(), "shot.png"), imgBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	wantB64 := base64.StdEncoding.EncodeToString(imgBytes)

	var (
		mu       sync.Mutex
		mode     string
		lastPath string
		lastHdr  http.Header
		lastBody string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		lastPath = r.URL.Path
		lastHdr = r.Header.Clone()
		lastBody = string(raw)
		curMode := mode
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch curMode {
		case "openai", "gemini":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": "图中第一行文字"}}},
				"usage":   map[string]any{"prompt_tokens": 30, "completion_tokens": 10},
			})
		case "anthropic":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "Claude OCR 文本"}},
				"usage":   map[string]any{"input_tokens": 20, "output_tokens": 5},
			})
		case "error401":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad vision key"}}`))
		}
	}))
	defer srv.Close()

	t.Run("openai", func(t *testing.T) {
		configureAITestModels(t, b, srv.URL, "openai", "gpt-4o")
		mu.Lock()
		mode = "openai"
		mu.Unlock()
		got, err := svc.RecognizeImageText(ctx, "shot.png")
		if err != nil || got != "图中第一行文字" {
			t.Fatalf("OpenAI OCR 失败：got=%q err=%v", got, err)
		}
		mu.Lock()
		p, h, body := lastPath, lastHdr, lastBody
		mu.Unlock()
		if p != "/v1/chat/completions" || h.Get("Authorization") != "Bearer sk-test-secret-key" {
			t.Fatalf("OpenAI 端点或头不对：path=%s auth=%s", p, h.Get("Authorization"))
		}
		if !strings.Contains(body, "data:image/png;base64,"+wantB64) {
			t.Fatalf("请求体未携带图片 data URL：%s", body)
		}
	})

	t.Run("gemini_strips_models_prefix", func(t *testing.T) {
		configureAITestModels(t, b, srv.URL, "gemini", "models/gemini-2.5-flash")
		mu.Lock()
		mode = "gemini"
		mu.Unlock()
		got, err := svc.RecognizeImageText(ctx, "shot.png")
		if err != nil || got != "图中第一行文字" {
			t.Fatalf("Gemini OCR 失败：got=%q err=%v", got, err)
		}
		mu.Lock()
		p, body := lastPath, lastBody
		mu.Unlock()
		if p != "/v1beta/openai/chat/completions" || !strings.Contains(body, `"model":"gemini-2.5-flash"`) {
			t.Fatalf("Gemini 兼容端点或模型名不对：path=%s body=%s", p, body)
		}
	})

	t.Run("anthropic", func(t *testing.T) {
		configureAITestModels(t, b, srv.URL, "anthropic", "claude-sonnet-4-5")
		mu.Lock()
		mode = "anthropic"
		mu.Unlock()
		got, err := svc.RecognizeImageText(ctx, "shot.png")
		if err != nil || got != "Claude OCR 文本" {
			t.Fatalf("Anthropic OCR 失败：got=%q err=%v", got, err)
		}
		mu.Lock()
		p, h, body := lastPath, lastHdr, lastBody
		mu.Unlock()
		if p != "/v1/messages" || h.Get("x-api-key") != "sk-test-secret-key" || h.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("Anthropic 端点或头不对：path=%s hdr=%v", p, h)
		}
		if !strings.Contains(body, `"type":"base64"`) || !strings.Contains(body, wantB64) {
			t.Fatalf("Anthropic 请求体未携带 base64 source：%s", body)
		}
	})

	t.Run("http_error", func(t *testing.T) {
		configureAITestModels(t, b, srv.URL, "openai", "gpt-4o")
		mu.Lock()
		mode = "error401"
		mu.Unlock()
		_, err := svc.RecognizeImageText(ctx, "shot.png")
		if err == nil || !strings.Contains(err.Error(), "bad vision key") {
			t.Fatalf("HTTP 401 错误未透传：%v", err)
		}
	})

	t.Run("missing_image", func(t *testing.T) {
		configureAITestModels(t, b, srv.URL, "openai", "gpt-4o")
		_, err := svc.RecognizeImageText(ctx, "missing.png")
		if err == nil || !strings.Contains(err.Error(), "missing.png") {
			t.Fatalf("缺失图片应报错：%v", err)
		}
	})

	stats := readUsageByScenario(t, b)
	if s := stats["ocr"]; s.Calls != 4 || s.FailedCalls != 1 || s.PromptTokens != 80 || s.CompletionTokens != 25 {
		t.Fatalf("ocr 用量统计不对（应计入 3 成功 + 1 失败）：%+v", s)
	}
}

func TestLibraryAIAdapterUpdateItemFieldsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryAIAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "原题", "content": "原始正文"})

	newSummary := "新 AI 摘要"
	item1, found, err := svc.UpdateItemFields(ctx, id, services.LibraryItemFieldsPatch{Summary: &newSummary})
	if err != nil || !found {
		t.Fatalf("仅改摘要失败：found=%v err=%v", found, err)
	}
	if item1.Summary != "新 AI 摘要" || item1.Content != "原始正文" {
		t.Fatalf("摘要或正文不符：%+v", item1)
	}

	newContent := "原始正文\n\n## 图中文字\n\n新识别内容"
	item2, found, err := svc.UpdateItemFields(ctx, id, services.LibraryItemFieldsPatch{Content: &newContent})
	if err != nil || !found {
		t.Fatalf("仅改正文失败：found=%v err=%v", found, err)
	}
	if item2.Content != newContent || item2.Summary != "新 AI 摘要" {
		t.Fatalf("局部更新破坏了其他字段：%+v", item2)
	}

	if _, found, err := svc.UpdateItemFields(ctx, "missing-item-id", services.LibraryItemFieldsPatch{Summary: &newSummary}); err != nil || found {
		t.Fatalf("不存在条目应返回 found=false, err=nil；实际 found=%v err=%v", found, err)
	}
}
