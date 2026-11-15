package aiflow

// 对话客户端栈、协议与错误文案的 golden 对拍；夹具来自 aiflow-golden-client.test.ts。

import (
	"context"
	"encoding/json"
	"testing"
)

type goldenChatOptions struct {
	Temperature       *float64 `json:"temperature"`
	MaxTokens         int      `json:"maxTokens"`
	Stream            bool     `json:"stream"`
	EnableThinking    bool     `json:"enableThinking"`
	TimeoutMs         float64  `json:"timeoutMs"`
	AllowEmptyContent bool     `json:"allowEmptyContent"`
}

type goldenMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type goldenChatCase struct {
	Name     string            `json:"name"`
	Config   goldenConfig      `json:"config"`
	Messages []goldenMessage   `json:"messages"`
	Options  goldenChatOptions `json:"options"`
	Script   []scriptStep      `json:"script"`
	Abort    *string           `json:"abort"`
	Calls    int               `json:"calls"`
	Expect   struct {
		Requests []recordedRequest `json:"requests"`
		Streamed []string          `json:"streamed"`
		Warnings []string          `json:"warnings"`
		Results  []any             `json:"results"`
		Errors   []any             `json:"errors"`
	} `json:"expect"`
}

type goldenProbe struct {
	Name   string       `json:"name"`
	Config goldenConfig `json:"config"`
	Prompt *string      `json:"prompt"`
	Script []scriptStep `json:"script"`
	Expect struct {
		Requests []recordedRequest `json:"requests"`
		Result   map[string]any    `json:"result"`
	} `json:"expect"`
}

func toMessages(in []goldenMessage) []ChatMessage {
	out := make([]ChatMessage, len(in))
	for i, m := range in {
		out[i] = ChatMessage{Role: m.Role, Content: m.Content}
	}
	return out
}

func TestChatGolden(t *testing.T) {
	var golden struct {
		Chat           []goldenChatCase `json:"chat"`
		TestConnection []goldenProbe    `json:"testConnection"`
	}
	loadGolden(t, "client", &golden)
	if len(golden.Chat) < 30 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Chat))
	}

	for _, c := range golden.Chat {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.Abort != nil && *c.Abort == "before" {
				cancel()
			}
			fake := newFakeTransport(c.Script, cancel)
			warns := &warnRecorder{}
			client := &Client{Transport: fake, Log: warns}

			var streamed []string
			results := []any{}
			errs := []any{}
			for i := 0; i < c.Calls; i++ {
				opts := ChatOptions{
					Temperature:       c.Options.Temperature,
					MaxTokens:         c.Options.MaxTokens,
					Stream:            c.Options.Stream,
					EnableThinking:    c.Options.EnableThinking,
					TimeoutMs:         c.Options.TimeoutMs,
					AllowEmptyContent: c.Options.AllowEmptyContent,
				}
				if opts.Stream {
					opts.OnStream = func(chunk string) { streamed = append(streamed, chunk) }
				}
				result, err := client.Chat(ctx, c.Config.toAIConfig(), toMessages(c.Messages), opts)
				if err != nil {
					results = append(results, nil)
					errs = append(errs, summarizeError(err))
				} else {
					results = append(results, resultSummary(result))
					errs = append(errs, nil)
				}
			}

			assertSame(t, "请求", nonNil(c.Expect.Requests), nonNil(fake.Requests))
			assertSame(t, "onStream 回调", nonNil(c.Expect.Streamed), nonNil(streamed))
			assertSame(t, "警告日志", nonNil(c.Expect.Warnings), nonNil(warns.Messages))
			assertSame(t, "结果", c.Expect.Results, results)
			assertSame(t, "错误", c.Expect.Errors, errs)
		})
	}

	for _, p := range golden.TestConnection {
		t.Run("testConnection/"+p.Name, func(t *testing.T) {
			fake := newFakeTransport(p.Script, nil)
			client := &Client{Transport: fake}
			prompt := ""
			if p.Prompt != nil {
				prompt = *p.Prompt
			}
			got := client.TestConnection(context.Background(), p.Config.toAIConfig(), prompt)
			summary := map[string]any{"id": got.ID, "success": got.Success, "model": got.Model, "provider": got.Provider}
			if got.Success {
				summary["response"] = got.Response
				if got.ThinkingContent != "" {
					summary["thinkingContent"] = got.ThinkingContent
				}
			} else {
				summary["error"] = got.Error
			}
			assertSame(t, "请求", nonNil(p.Expect.Requests), nonNil(fake.Requests))
			assertSame(t, "结果", p.Expect.Result, summary)
		})
	}
}

// nonNil 让 nil 切片与空切片在 JSON 里都是 []。
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func TestProtocolGolden(t *testing.T) {
	var golden struct {
		ResolveProtocol []struct {
			Config struct {
				APIProtocol string `json:"apiProtocol"`
				Provider    string `json:"provider"`
				APIURL      string `json:"apiUrl"`
			} `json:"config"`
			Expect string `json:"expect"`
		} `json:"resolveProtocol"`
		BaseURLs []struct {
			URL                  string `json:"url"`
			GetBaseURL           string `json:"getBaseUrl"`
			NormalizeAPIURLInput string `json:"normalizeApiUrlInput"`
		} `json:"baseUrls"`
		Endpoints []struct {
			URL      string `json:"url"`
			Protocol string `json:"protocol"`
			Resolved struct {
				Protocol string `json:"protocol"`
				Explicit bool   `json:"explicit"`
				BaseURL  string `json:"baseUrl"`
			} `json:"resolved"`
			Chat       string `json:"chat"`
			Embeddings string `json:"embeddings"`
			Models     string `json:"models"`
		} `json:"endpoints"`
		Headers []struct {
			Protocol string `json:"protocol"`
			Options  *struct {
				Accept              string `json:"accept"`
				ContentType         *bool  `json:"contentType"`
				UseNativeGeminiAuth bool   `json:"useNativeGeminiAuth"`
			} `json:"options"`
			Expect map[string]string `json:"expect"`
		} `json:"headers"`
		Usage []struct {
			Payload  any    `json:"payload"`
			Protocol string `json:"protocol"`
			Expect   *struct {
				PromptTokens     float64 `json:"promptTokens"`
				CompletionTokens float64 `json:"completionTokens"`
			} `json:"expect"`
		} `json:"usage"`
		Text []struct {
			Payload  any     `json:"payload"`
			Protocol string  `json:"protocol"`
			Expect   *string `json:"expect"`
		} `json:"text"`
	}
	loadGolden(t, "protocol", &golden)

	for i, c := range golden.ResolveProtocol {
		got := ResolveAIProtocol(c.Config.APIProtocol, c.Config.Provider, c.Config.APIURL)
		if got != c.Expect {
			t.Errorf("resolveAIProtocol #%d %+v：Go=%q TS=%q", i, c.Config, got, c.Expect)
		}
	}
	for _, c := range golden.BaseURLs {
		if got := GetBaseURL(c.URL); got != c.GetBaseURL {
			t.Errorf("getBaseUrl(%q)：Go=%q TS=%q", c.URL, got, c.GetBaseURL)
		}
		if got := NormalizeAPIURLInput(c.URL); got != c.NormalizeAPIURLInput {
			t.Errorf("normalizeApiUrlInput(%q)：Go=%q TS=%q", c.URL, got, c.NormalizeAPIURLInput)
		}
	}
	for _, c := range golden.Endpoints {
		resolved := ResolveProtocolBase(c.URL, c.Protocol)
		if resolved.Protocol != c.Resolved.Protocol || resolved.Explicit != c.Resolved.Explicit || resolved.BaseURL != c.Resolved.BaseURL {
			t.Errorf("resolveProtocolBase(%q,%s)：Go=%+v TS=%+v", c.URL, c.Protocol, resolved, c.Resolved)
		}
		if got := BuildChatEndpoint(resolved); got != c.Chat {
			t.Errorf("chat 端点(%q,%s)：Go=%q TS=%q", c.URL, c.Protocol, got, c.Chat)
		}
		if got := BuildEmbeddingsEndpoint(resolved); got != c.Embeddings {
			t.Errorf("embeddings 端点(%q,%s)：Go=%q TS=%q", c.URL, c.Protocol, got, c.Embeddings)
		}
		if got := BuildModelsEndpoint(resolved); got != c.Models {
			t.Errorf("models 端点(%q,%s)：Go=%q TS=%q", c.URL, c.Protocol, got, c.Models)
		}
	}
	for _, c := range golden.Headers {
		opts := HeaderOptions{}
		if c.Options != nil {
			opts.Accept = c.Options.Accept
			opts.NoContentType = c.Options.ContentType != nil && !*c.Options.ContentType
			opts.UseNativeGeminiAuth = c.Options.UseNativeGeminiAuth
		}
		assertSame(t, "请求头 "+c.Protocol, c.Expect, BuildHeaders(c.Protocol, "sk-abc", opts))
	}
	for i, c := range golden.Usage {
		got := ExtractUsage(c.Payload, c.Protocol)
		switch {
		case c.Expect == nil && got != nil:
			t.Errorf("extractUsage #%d(%v,%s)：TS 无用量，Go=%+v", i, c.Payload, c.Protocol, *got)
		case c.Expect != nil && got == nil:
			t.Errorf("extractUsage #%d(%v,%s)：TS=%+v，Go 无用量", i, c.Payload, c.Protocol, *c.Expect)
		case c.Expect != nil && (got.PromptTokens != c.Expect.PromptTokens || got.CompletionTokens != c.Expect.CompletionTokens):
			t.Errorf("extractUsage #%d(%v,%s)：Go=%+v TS=%+v", i, c.Payload, c.Protocol, *got, *c.Expect)
		}
	}
	for i, c := range golden.Text {
		got, ok := ExtractChatText(c.Payload, c.Protocol)
		switch {
		case c.Expect == nil && ok:
			t.Errorf("extractText #%d(%v,%s)：TS=null，Go=%q", i, c.Payload, c.Protocol, got)
		case c.Expect != nil && (!ok || got != *c.Expect):
			t.Errorf("extractText #%d(%v,%s)：Go=%q(ok=%v) TS=%q", i, c.Payload, c.Protocol, got, ok, *c.Expect)
		}
	}
}

func TestErrorFormatGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string `json:"name"`
			Response struct {
				Status  int               `json:"status"`
				Body    string            `json:"body"`
				Headers map[string]string `json:"headers"`
				Error   *string           `json:"error"`
			} `json:"response"`
			Options *struct {
				Operation     string `json:"operation"`
				Fallback      string `json:"fallback"`
				MaxTextLength int    `json:"maxTextLength"`
			} `json:"options"`
			Expect string `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "errors", &golden)
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			resp := TransportResponse{Status: c.Response.Status, Body: c.Response.Body, Headers: c.Response.Headers, Error: c.Response.Error}
			var opts FormatErrorOptions
			if c.Options != nil {
				opts = FormatErrorOptions{Operation: c.Options.Operation, Fallback: c.Options.Fallback, MaxTextLength: c.Options.MaxTextLength}
			}
			if got := FormatHTTPError(resp, opts); got != c.Expect {
				t.Errorf("Go=%q\nTS=%q", got, c.Expect)
			}
		})
	}
}

var _ = json.Marshal // 保持 encoding/json 在文件内可用（后续用例会用到）
