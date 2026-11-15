package aiflow

// golden 对拍的 Go 侧公用件。夹具由 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-*.test.ts
// 用真实的 TS 实现导出（约定见 aiflow-golden-kit.ts），这里用同样的脚本重放。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// loadGolden 读取 testdata/golden/<name>.json。
func loadGolden(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden", name+".json"))
	if err != nil {
		t.Fatalf("读取 golden %q 失败：%v（用 UPDATE_GOLDEN=1 运行 aiflow-golden-*.test.ts 导出）", name, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("解析 golden %q 失败：%v", name, err)
	}
}

// normalized 把任意值经 JSON 往返成通用结构，用来做深比较（忽略 Go 与 TS 在类型上的差别）。
func normalized(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	return out
}

// assertSame 比较期望（golden）与实际结果，不等时给出两侧的缩进 JSON。
func assertSame(t *testing.T, what string, want, got any) {
	t.Helper()
	w, g := normalized(t, want), normalized(t, got)
	if reflect.DeepEqual(w, g) {
		return
	}
	wj, _ := json.MarshalIndent(w, "", "  ")
	gj, _ := json.MarshalIndent(g, "", "  ")
	t.Errorf("%s 与 TS 不一致\n--- TS（golden）---\n%s\n--- Go ---\n%s", what, wj, gj)
}

// scriptStep 对应 aiflow-golden-kit.ts 的 ScriptStep。
type scriptStep struct {
	Status      *int              `json:"status"`
	OK          *bool             `json:"ok"`
	StatusText  string            `json:"statusText"`
	Body        string            `json:"body"`
	Headers     map[string]string `json:"headers"`
	Error       *string           `json:"error"`
	Chunks      []string          `json:"chunks"`
	StreamError *string           `json:"streamError"`
	Throws      *string           `json:"throws"`
	AbortDuring bool              `json:"abortDuring"`
}

func (s scriptStep) response() TransportResponse {
	status := 200
	if s.Status != nil {
		status = *s.Status
	}
	ok := status >= 200 && status < 300
	if s.OK != nil {
		ok = *s.OK
	}
	headers := s.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	return TransportResponse{OK: ok, Status: status, StatusText: s.StatusText, Body: s.Body, Headers: headers, Error: s.Error}
}

// recordedRequest 对应 RecordedRequest：kind 是 request 或 stream。
type recordedRequest struct {
	Kind      string            `json:"kind"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Body      *string           `json:"body,omitempty"`
	TimeoutMs *float64          `json:"timeoutMs,omitempty"`
}

// fakeTransport 按脚本回放，并记录收到的请求。cancel 在 abortDuring 的那一步被调用。
type fakeTransport struct {
	mu       sync.Mutex
	steps    []scriptStep
	Requests []recordedRequest
	cancel   context.CancelFunc
}

func newFakeTransport(steps []scriptStep, cancel context.CancelFunc) *fakeTransport {
	return &fakeTransport{steps: append([]scriptStep(nil), steps...), cancel: cancel}
}

func (f *fakeTransport) record(kind string, req TransportRequest) {
	rec := recordedRequest{Kind: kind, Method: req.Method, URL: req.URL, Headers: req.Headers}
	if rec.Headers == nil {
		rec.Headers = map[string]string{}
	}
	if req.Body != "" {
		body := req.Body
		rec.Body = &body
	}
	if req.TimeoutMs != 0 {
		ms := req.TimeoutMs
		rec.TimeoutMs = &ms
	}
	f.Requests = append(f.Requests, rec)
}

func (f *fakeTransport) take() (scriptStep, error) {
	if len(f.steps) == 0 {
		return scriptStep{}, errors.New("场景脚本已用尽：请求次数比预期多")
	}
	step := f.steps[0]
	f.steps = f.steps[1:]
	return step, nil
}

func (f *fakeTransport) Request(_ context.Context, req TransportRequest) (TransportResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("request", req)
	step, err := f.take()
	if err != nil {
		return TransportResponse{}, err
	}
	if step.Throws != nil {
		return TransportResponse{}, errors.New(*step.Throws)
	}
	if step.AbortDuring && f.cancel != nil {
		f.cancel()
	}
	return step.response(), nil
}

func (f *fakeTransport) RequestStream(_ context.Context, req TransportRequest, h StreamHandler) (TransportResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("stream", req)
	step, err := f.take()
	if err != nil {
		return TransportResponse{}, err
	}
	if step.Throws != nil {
		return TransportResponse{}, errors.New(*step.Throws)
	}
	for _, chunk := range step.Chunks {
		h.OnChunk(chunk)
	}
	if step.StreamError != nil {
		h.OnError(*step.StreamError)
	}
	if step.AbortDuring && f.cancel != nil {
		f.cancel()
	}
	return step.response(), nil
}

// warnRecorder 收集 Warnf 输出。
type warnRecorder struct {
	mu       sync.Mutex
	Messages []string
}

func (w *warnRecorder) Warnf(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Messages = append(w.Messages, fmt.Sprintf(format, args...))
}

// usageRecorder 收集用量记录，字段与 aiflow-golden-kit.ts 的 UsageRecord 一致。
type usageRecorder struct {
	mu      sync.Mutex
	Entries []usageRecord
}

type usageRecord struct {
	Scenario         string  `json:"scenario"`
	Model            string  `json:"model"`
	PromptTokens     float64 `json:"promptTokens"`
	CompletionTokens float64 `json:"completionTokens"`
	Failed           *bool   `json:"failed,omitempty"`
}

func (u *usageRecorder) RecordUsage(e UsageEntry) {
	u.mu.Lock()
	defer u.mu.Unlock()
	rec := usageRecord{Scenario: e.Scenario, Model: e.Model, PromptTokens: e.PromptTokens, CompletionTokens: e.CompletionTokens}
	if e.Failed {
		failed := true
		rec.Failed = &failed
	}
	u.Entries = append(u.Entries, rec)
}

// goldenConfig 对应 TS 的 AIConfig（JSON 键是 camelCase）。
type goldenConfig struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	APIProtocol string `json:"apiProtocol"`
	APIKey      string `json:"apiKey"`
	APIURL      string `json:"apiUrl"`
	Model       string `json:"model"`
}

func (c goldenConfig) toAIConfig() AIConfig {
	return AIConfig{ID: c.ID, Provider: c.Provider, APIProtocol: c.APIProtocol, APIKey: c.APIKey, APIURL: c.APIURL, Model: c.Model}
}

// errorSummary 对应 describeError 的输出。
type errorSummary struct {
	Message string `json:"message"`
	Aborted bool   `json:"aborted"`
}

func summarizeError(err error) *errorSummary {
	if err == nil {
		return nil
	}
	return &errorSummary{Message: err.Error(), Aborted: IsAborted(err)}
}

// resultSummary 把 ChatResult 变成 golden 里的形状（空字段不出现）。
func resultSummary(r ChatResult) map[string]any {
	out := map[string]any{"content": r.Content}
	if r.ThinkingContent != "" {
		out["thinkingContent"] = r.ThinkingContent
	}
	if r.FinishReason != "" {
		out["finishReason"] = r.FinishReason
	}
	if r.Usage != nil {
		out["usage"] = map[string]any{"promptTokens": r.Usage.PromptTokens, "completionTokens": r.Usage.CompletionTokens}
	}
	return out
}

// mustContain 断言 s 包含 sub。
func mustContain(t *testing.T, what, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("%s 应包含 %q，实际：%q", what, sub, s)
	}
}
