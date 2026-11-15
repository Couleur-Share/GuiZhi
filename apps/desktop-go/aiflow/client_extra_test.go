package aiflow

// 不进 golden 的行为：TS 与 Go 有意不同的地方，以及 Go 侧独有的保护（日志脱敏、畸形响应不崩溃）。
// 差异清单见 docs/agents/native-ui-development.md。

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func okStep(body string) scriptStep { return scriptStep{Body: body} }

func newTestClient(steps []scriptStep, cancel context.CancelFunc) (*Client, *fakeTransport, *warnRecorder) {
	fake := newFakeTransport(steps, cancel)
	warns := &warnRecorder{}
	return &Client{Transport: fake, Log: warns}, fake, warns
}

var testConfig = AIConfig{ID: "m1", Provider: "custom", APIProtocol: "openai", APIKey: "sk-secret-1234567890", APIURL: "https://api.example.com/v1", Model: "test-model"}

func TestChatAnthropicHonorsCancellation(t *testing.T) {
	// TS 版 Anthropic 路径不挂取消（signal 过不去），Go 版统一尊重 ctx——有意的改进。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, _, _ := newTestClient([]scriptStep{{AbortDuring: true, Body: `{"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn"}`}}, cancel)
	cfg := AIConfig{Provider: "anthropic", APIProtocol: "anthropic", APIKey: "k", APIURL: "https://api.anthropic.com", Model: "claude"}
	_, err := client.Chat(ctx, cfg, []ChatMessage{{Role: "user", Content: "hi"}}, ChatOptions{})
	if !IsAborted(err) {
		t.Fatalf("Anthropic 路径应当尊重取消，实际：%v", err)
	}
}

func TestChatMalformedBodiesDoNotPanic(t *testing.T) {
	bodies := []string{"not json", "null", "5", `"str"`, "[]", `{"choices":[null]}`, `{"choices":[{}]}`, `{"choices":[{"message":null}]}`, `{"choices":"x"}`}
	for _, body := range bodies {
		client, _, _ := newTestClient([]scriptStep{okStep(body)}, nil)
		_, err := client.Chat(context.Background(), testConfig, []ChatMessage{{Role: "user", Content: "hi"}}, ChatOptions{})
		if err == nil {
			t.Errorf("畸形响应 %q 应当报错", body)
		}
	}
	client, _, _ := newTestClient([]scriptStep{okStep("not json")}, nil)
	_, err := client.Chat(context.Background(), testConfig, []ChatMessage{{Role: "user", Content: "hi"}}, ChatOptions{})
	mustContain(t, "非法 JSON 的错误", err.Error(), "AI 响应不是合法 JSON")
}

func TestChatRedactsAPIKeyInWarnings(t *testing.T) {
	// 错误文案是上游给的，万一回显了请求里的 key，日志里必须抹掉
	steps := []scriptStep{
		{Status: intPtr(400), Body: `{"error":{"message":"bad request for key sk-secret-1234567890: 'max_tokens' is not supported"}}`},
		okStep(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	}
	client, _, warns := newTestClient(steps, nil)
	if _, err := client.Chat(context.Background(), testConfig, []ChatMessage{{Role: "user", Content: "hi"}}, ChatOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(warns.Messages) != 1 {
		t.Fatalf("应当有 1 条警告，实际 %d", len(warns.Messages))
	}
	if strings.Contains(warns.Messages[0], "sk-secret") {
		t.Errorf("警告日志泄露了 API Key：%s", warns.Messages[0])
	}
	mustContain(t, "警告日志", warns.Messages[0], "***")
}

func intPtr(n int) *int { return &n }

func TestParseEmbeddingsDuplicateIndexIsGap(t *testing.T) {
	// TS 版用 some() 检查连续性，会跳过空洞；Go 版按注释本意报错
	body := `{"data":[{"index":0,"embedding":[1,2]},{"index":0,"embedding":[3,4]}]}`
	_, err := ParseEmbeddingsResponse(body, 2)
	if err == nil || !strings.Contains(err.Error(), "索引不连续") {
		t.Fatalf("重复 index 应报「索引不连续」，实际：%v", err)
	}
	body = `{"data":[{"index":1.5,"embedding":[1]},{"index":0,"embedding":[3]}]}`
	if _, err := ParseEmbeddingsResponse(body, 2); err == nil {
		t.Fatal("小数 index 不落位，应当报索引不连续")
	}
}

func TestFetchModelsMalformed(t *testing.T) {
	for _, body := range []string{"not json", "null", "5", `"x"`} {
		client, _, _ := newTestClient([]scriptStep{okStep(body)}, nil)
		r := client.FetchModels(context.Background(), "https://api.example.com", "k", "")
		if r.Success {
			t.Errorf("畸形响应 %q 不应成功", body)
		}
	}
}

func TestScrubSecretShortKeysAreKept(t *testing.T) {
	// 过短的 key 会误伤正常文本，保持原样（已有 APIKey 长度校验在配置侧）
	if got := scrubSecret("abc abc", "abc"); got != "abc abc" {
		t.Errorf("短 key 不应被替换：%q", got)
	}
	if got := scrubSecret("token sk-123456 end", "sk-123456"); got != "token *** end" {
		t.Errorf("长 key 应被替换：%q", got)
	}
}

func TestRunnerDoesNotRecordUsageWhenNotConfigured(t *testing.T) {
	usage := &usageRecorder{}
	runner := &Runner{Client: &Client{Transport: newFakeTransport(nil, nil)}, Config: staticConfig{}, Usage: usage}
	_, err := runner.RunScenarioChat(context.Background(), ScenarioQA, nil, ScenarioChatOptions{})
	if !errors.Is(err, ErrNotConfigured) || !IsNotConfigured(err) {
		t.Fatalf("应当返回未配置错误，实际：%v", err)
	}
	if len(usage.Entries) != 0 {
		t.Errorf("未配置不应记用量：%+v", usage.Entries)
	}
}
