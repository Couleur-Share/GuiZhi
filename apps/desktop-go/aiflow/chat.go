package aiflow

// 对话补全客户端（对应 renderer/services/ai.ts 的 chatCompletion 与 testAIConnection）。
//
// 与主包 ai_chat.go 的区别：那一份对应 packages/core 的主进程客户端（视频总结等），默认参数与重试规则不同；
// 这里对应渲染层 ai.ts——知识库的摘要、标签、问答、Wiki 编译走的就是它，原生界面必须发出同样的请求。
//
// 有意不移植的 ai.ts 选项：topP/topK/频率与存在惩罚、responseFormat、streamCallbacks。
// 它们在渲染层没有任何调用方（tests/unit/knowledge-ai/aiflow-golden-client.test.ts 有守卫：
// 一旦出现调用方就会失败，提醒来这里补移植）。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ChatMessage 是一条对话消息（内容为纯文本；多模态消息不经此路径，OCR 有自己的请求构造）。
type ChatMessage struct {
	Role    string // system | user | assistant
	Content string
}

// ChatOptions 对应 ChatCompletionOptions 中有调用方的那部分。
type ChatOptions struct {
	Temperature *float64 // nil → 0.7
	MaxTokens   int      // 0 → 2048
	Stream      bool
	// EnableThinking 为 true 时才请求思考模式；Qwen 只有流式才能开。
	EnableThinking bool
	// OnStream 在流式模式下逐块收到模型输出（delta.content）。
	OnStream  func(chunk string)
	TimeoutMs float64
	// AllowEmptyContent 只给连通性探针用：几个 token 的探针本来就不足以让思考类模型产出正文。
	AllowEmptyContent bool
}

// ChatResult 对应 ChatCompletionResult。
type ChatResult struct {
	Content         string
	ThinkingContent string
	// FinishReason 是 openai 系 finish_reason；anthropic 的 max_tokens 归一化为 "length"。空串表示接口没给。
	FinishReason string
	// Usage 为 nil 表示接口没回报（流式与部分中转站不返回；anthropic 路径不解析）。
	Usage *TokenUsage
}

// Logger 接收诊断日志；实现方负责脱敏（aiflow 已经把当前请求的 API Key 从消息里抹掉）。
type Logger interface {
	Warnf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Warnf(string, ...any) {}

// Client 是对话与 embeddings 请求的发送方。
type Client struct {
	Transport Transport
	Log       Logger
	// Now 用于连通性测试的耗时统计，测试可注入。
	Now func() time.Time
}

func (c *Client) logger() Logger {
	if c.Log == nil {
		return nopLogger{}
	}
	return c.Log
}

func (c *Client) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

const (
	defaultTemperature = 0.7
	defaultMaxTokens   = 2048

	connectionTestMaxTokens = 8
	connectionTestTimeoutMs = 12_000
	connectionTestPrompt    = "Reply with exactly: OK"
)

var reGPT5Plus = regexp.MustCompile(`gpt-[5-9]`)

// scrubSecret 把消息里出现的 API Key 抹掉（日志脱敏）。
func scrubSecret(msg, secret string) string {
	if len(secret) < 6 { // 过短的串会误伤正常文本
		return msg
	}
	return strings.ReplaceAll(msg, secret, "***")
}

// assertNonEmptyContent 空正文一律按失败处理。
//
// 思考类模型会把 max_tokens 预算整个花在推理上：HTTP 200、choices 齐全，但 finish_reason 是 length、
// content 是空字符串，正文全在 reasoning_content 里且被截断。沿用「有 choices 就算成功」的判断，
// 上层会把空结果当成有效输出静默落库（摘要被写成空串）。
func assertNonEmptyContent(content, finishReason string, hasThinking bool, maxTokens int, allowEmpty bool) error {
	if allowEmpty || jsTrim(content) != "" {
		return nil
	}
	if finishReason == "length" {
		if hasThinking {
			return fmt.Errorf("模型把 %d token 的输出预算全用在思考过程上（finish_reason=length），没有产出正文。请为该场景改用非思考模型。", maxTokens)
		}
		return fmt.Errorf("模型输出在 %d token 上限处被截断，没有产出正文。", maxTokens)
	}
	fr := finishReason
	if fr == "" {
		fr = "unknown"
	}
	return fmt.Errorf("模型返回了空正文（finish_reason=%s）", fr)
}

// contentText 对应 normalizeAssistantContent：字符串原样，内容块数组取 text 块拼接，其余按空串。
func contentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok || m["type"] != "text" {
				continue
			}
			if t := m["text"]; t != nil {
				b.WriteString(jsToString(t))
			}
		}
		return b.String()
	}
	return ""
}

func messagesJSON(messages []ChatMessage) []*obj {
	out := make([]*obj, len(messages))
	for i, m := range messages {
		out[i] = newObj().set("role", m.Role).set("content", m.Content)
	}
	return out
}

// Chat 对应 chatCompletion：调用 AI 模型进行对话（支持流式输出与思考模型）。
func (c *Client) Chat(ctx context.Context, cfg AIConfig, messages []ChatMessage, opts ChatOptions) (ChatResult, error) {
	providerID := jsLower(cfg.Provider)
	protocol := ResolveAIProtocol(cfg.APIProtocol, cfg.Provider, cfg.APIURL)
	isGemini := protocol == ProtocolGemini
	isAnthropic := protocol == ProtocolAnthropic
	normalizedModel := cfg.Model
	if isGemini {
		normalizedModel = strings.TrimPrefix(normalizedModel, "models/")
	}

	if cfg.APIKey == "" {
		return ChatResult{}, errors.New("API Key is not configured")
	}
	if cfg.APIURL == "" {
		return ChatResult{}, errors.New("API URL is not configured")
	}
	if cfg.Model == "" {
		return ChatResult{}, errors.New("No model selected")
	}

	endpoint := BuildChatEndpoint(ResolveProtocolBase(cfg.APIURL, protocol))

	temperature := defaultTemperature
	if opts.Temperature != nil {
		temperature = *opts.Temperature
	}
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}
	stream := opts.Stream
	if isAnthropic {
		stream = false
	}

	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	headers := BuildHeaders(protocol, cfg.APIKey, HeaderOptions{Accept: accept})

	// 需要 max_completion_tokens 的新模型；判断不准时由下面的「token 参数不兼容」重试兜底（Issue #21）。
	modelLower := jsLower(cfg.Model)
	useMaxCompletionTokens := strings.Contains(modelLower, "o1") ||
		strings.Contains(modelLower, "o3") ||
		strings.Contains(modelLower, "gpt-4o") ||
		strings.Contains(modelLower, "gpt-4.5") ||
		reGPT5Plus.MatchString(modelLower) ||
		strings.Contains(providerID, "openai")

	if isAnthropic {
		return c.chatAnthropic(ctx, cfg, endpoint, headers, messages, maxTokens, opts)
	}

	body := newObj().
		set("model", normalizedModel).
		set("messages", messagesJSON(messages)).
		set("temperature", temperature).
		set("stream", stream)
	if useMaxCompletionTokens {
		body.set("max_completion_tokens", maxTokens)
	} else {
		body.set("max_tokens", maxTokens)
	}

	// 思考模式：只有流式才能启用，非流式必须禁用（Qwen 会直接报错）。
	isQwen := strings.Contains(providerID, "qwen") || strings.Contains(providerID, "dashscope") ||
		strings.Contains(modelLower, "qwen")
	if isQwen {
		body.set("enable_thinking", stream && opts.EnableThinking)
	} else if opts.EnableThinking {
		body.set("enable_thinking", true) // 其他支持思考的模型（如 DeepSeek）
	}

	type sendResult struct {
		stream *ChatResult
		resp   *TransportResponse
	}
	send := func() (sendResult, error) {
		req := TransportRequest{Method: "POST", URL: endpoint, Headers: headers, Body: body.String(), TimeoutMs: opts.TimeoutMs}
		if stream {
			var state streamState
			var streamErr string
			var hasStreamErr bool
			resp, err := cancellable(ctx, func() (TransportResponse, error) {
				return c.Transport.RequestStream(ctx, req, StreamHandler{
					OnChunk: func(chunk string) { state.process(chunk, opts.OnStream, false) },
					OnError: func(message string) { streamErr, hasStreamErr = message, true },
				})
			})
			if err != nil {
				return sendResult{}, err
			}
			if !resp.OK {
				return sendResult{resp: &resp}, nil
			}
			if hasStreamErr {
				return sendResult{}, errors.New(streamErr)
			}
			state.process("", opts.OnStream, true)
			result := state.finalize()
			return sendResult{stream: &result}, nil
		}
		resp, err := cancellable(ctx, func() (TransportResponse, error) { return c.Transport.Request(ctx, req) })
		if err != nil {
			return sendResult{}, err
		}
		return sendResult{resp: &resp}, nil
	}

	result, err := send()
	if err != nil {
		return ChatResult{}, err
	}
	if result.resp != nil && !result.resp.OK {
		errorMessage := FormatHTTPError(*result.resp, FormatErrorOptions{})
		isTokenParamError := strings.Contains(errorMessage, "'max_tokens' is not supported") ||
			strings.Contains(errorMessage, "'max_completion_tokens' is not supported") ||
			strings.Contains(errorMessage, "Use 'max_completion_tokens' instead") ||
			strings.Contains(errorMessage, "Use 'max_tokens' instead")
		isThinkingParamError := strings.Contains(errorMessage, "enable_thinking must be set to false") ||
			strings.Contains(errorMessage, "enable_thinking only support stream") ||
			strings.Contains(errorMessage, "parameter.enable_thinking")

		switch {
		case isTokenParamError:
			c.logger().Warnf("[AI Service] Token parameter mismatch detected: %q. Retrying with alternative parameter...", scrubSecret(errorMessage, cfg.APIKey))
			if useMaxCompletionTokens {
				body.del("max_completion_tokens")
				body.set("max_tokens", maxTokens)
			} else {
				body.del("max_tokens")
				body.set("max_completion_tokens", maxTokens)
			}
		case isThinkingParamError:
			c.logger().Warnf("[AI Service] enable_thinking parameter error detected: %q. Retrying with enable_thinking=false...", scrubSecret(errorMessage, cfg.APIKey))
			body.set("enable_thinking", false)
		default:
			return ChatResult{}, errors.New(errorMessage)
		}
		result, err = send()
		if err != nil {
			return ChatResult{}, err
		}
		if result.resp != nil && !result.resp.OK {
			return ChatResult{}, errors.New(FormatHTTPError(*result.resp, FormatErrorOptions{}))
		}
	}

	if result.stream != nil {
		// 流式没有 finish_reason 可读（不传给空正文断言），只能凭「有没有攒到正文」判断
		if err := assertNonEmptyContent(result.stream.Content, "", result.stream.ThinkingContent != "", maxTokens, opts.AllowEmptyContent); err != nil {
			return ChatResult{}, err
		}
		return *result.stream, nil
	}
	if result.resp == nil {
		return ChatResult{}, errors.New("AI 返回结果为空")
	}

	// 非流式响应
	parsed, err := parseJSON(result.resp.Body)
	if err != nil {
		return ChatResult{}, fmt.Errorf("AI 响应不是合法 JSON: %w", err)
	}
	data, _ := parsed.(map[string]any)
	choices, _ := data["choices"].([]any)
	if len(choices) == 0 {
		return ChatResult{}, errors.New("AI 返回结果为空")
	}
	choice, _ := choices[0].(map[string]any)
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return ChatResult{}, errors.New("AI 返回结果格式异常：choices[0].message 缺失")
	}
	content := contentText(message["content"])
	finishReason, _ := choice["finish_reason"].(string)
	thinking, _ := message["reasoning_content"].(string)
	if err := assertNonEmptyContent(content, finishReason, thinking != "", maxTokens, opts.AllowEmptyContent); err != nil {
		return ChatResult{}, err
	}
	out := ChatResult{Content: content, ThinkingContent: thinking, FinishReason: finishReason}
	if usage := data["usage"]; jsTruthy(usage) {
		fields, _ := usage.(map[string]any)
		out.Usage = &TokenUsage{
			PromptTokens:     numberOrZero(fields["prompt_tokens"]),
			CompletionTokens: numberOrZero(fields["completion_tokens"]),
		}
	}
	return out, nil
}

// chatAnthropic 是 Anthropic 协议的非流式路径：system 拆到顶层，max_tokens 是必填的顶层字段。
func (c *Client) chatAnthropic(ctx context.Context, cfg AIConfig, endpoint string, headers map[string]string, messages []ChatMessage, maxTokens int, opts ChatOptions) (ChatResult, error) {
	var anthropicMessages []*obj
	var system *string
	for _, m := range messages {
		if m.Role == "system" {
			if system == nil { // find：只取第一条 system
				content := m.Content
				system = &content
			}
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		anthropicMessages = append(anthropicMessages, newObj().set("role", role).set("content", m.Content))
	}
	if anthropicMessages == nil {
		anthropicMessages = []*obj{}
	}
	body := newObj().set("model", cfg.Model).set("max_tokens", maxTokens).set("messages", anthropicMessages).set("stream", false)
	if system != nil {
		body.set("system", *system)
	}

	// TS 版这条路径不挂取消（signal 过不去），Go 版统一尊重 ctx——有意的改进。
	resp, err := cancellable(ctx, func() (TransportResponse, error) {
		return c.Transport.Request(ctx, TransportRequest{Method: "POST", URL: endpoint, Headers: headers, Body: body.String(), TimeoutMs: opts.TimeoutMs})
	})
	if err != nil {
		return ChatResult{}, err
	}
	if !resp.OK {
		return ChatResult{}, errors.New(FormatHTTPError(resp, FormatErrorOptions{}))
	}
	parsed, err := parseJSON(resp.Body)
	if err != nil {
		return ChatResult{}, fmt.Errorf("AI 响应不是合法 JSON: %w", err)
	}
	data, _ := parsed.(map[string]any)
	var text strings.Builder
	if items, ok := data["content"].([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok && m["type"] == "text" {
				if s, ok := m["text"].(string); ok {
					text.WriteString(s)
				}
			}
		}
	}
	finishReason, _ := data["stop_reason"].(string)
	if finishReason == "max_tokens" {
		finishReason = "length"
	}
	if err := assertNonEmptyContent(text.String(), finishReason, false, maxTokens, opts.AllowEmptyContent); err != nil {
		return ChatResult{}, err
	}
	return ChatResult{Content: text.String(), FinishReason: finishReason}, nil
}

// TestResult 对应 AITestResult。
type TestResult struct {
	ID              string
	Success         bool
	Response        string
	ThinkingContent string
	Error           string
	LatencyMs       int64
	Model           string
	Provider        string
}

// TestConnection 对应 testAIConnection：用极小的探针验证端点和模型能否响应，不继承长文本生成参数。
func (c *Client) TestConnection(ctx context.Context, cfg AIConfig, prompt string) TestResult {
	start := c.now()
	if prompt == "" {
		prompt = connectionTestPrompt
	}
	zero := 0.0
	result, err := c.Chat(ctx, cfg, []ChatMessage{{Role: "user", Content: prompt}}, ChatOptions{
		Temperature:       &zero,
		MaxTokens:         connectionTestMaxTokens,
		TimeoutMs:         connectionTestTimeoutMs,
		AllowEmptyContent: true, // 探针只有几个 token，思考类模型必然答不出正文；这里验的是连通性
	})
	latency := c.now().Sub(start).Milliseconds()
	if err != nil {
		msg := err.Error()
		if msg == "" {
			msg = "未知错误"
		}
		return TestResult{ID: cfg.ID, Success: false, Error: msg, LatencyMs: latency, Model: cfg.Model, Provider: cfg.Provider}
	}
	return TestResult{
		ID: cfg.ID, Success: true, Response: result.Content, ThinkingContent: result.ThinkingContent,
		LatencyMs: latency, Model: cfg.Model, Provider: cfg.Provider,
	}
}
