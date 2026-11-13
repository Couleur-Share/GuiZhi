package main

// 对应 packages/core/src/ai-client.ts 与 ai-stream-result.ts：主进程侧的对话补全客户端。
//
// 渲染进程的对话走 ai:httpRequest 代理（ai_proxy.go）；这里服务的是主进程自己发起的调用——
// 视频/音频总结、文字稿排版、论坛总结、主题阅读等，它们不经过渲染进程。
// 出站走 activeNetProxy 选路（与 TS 里 fetch 被网络代理设置接管的效果一致）。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type aiClientConfig struct {
	Provider    string
	APIProtocol string
	APIKey      string
	APIURL      string
	Model       string
}

type aiChatMessage struct {
	Role    string // system | user | assistant
	Content string
}

type aiChatResult struct {
	Content string
	// openai 系 finish_reason；anthropic 的 max_tokens 归一化为 "length"
	FinishReason string
	// 接口没回报时为 nil
	Usage *chatTokenUsage
}

type aiChatOptions struct {
	Temperature    *float64
	MaxTokens      int // 0 = 默认 4096
	ResponseFormat string
	Timeout        time.Duration // 0 = 60 秒
	Stream         bool
	OnProgress     func(receivedChars int)
	OnDelta        func(text string)
}

// aiChatFunc 是对话调用的函数形态；业务代码依赖它而不是具体实现，测试可注入替身
// （对应 TS 里 options.chat 的测试注入）。
type aiChatFunc func(ctx context.Context, cfg aiClientConfig, messages []aiChatMessage, opts aiChatOptions) (aiChatResult, error)

const (
	aiRequestTimeout   = 60 * time.Second
	aiStreamMaxBytes   = 8 * 1024 * 1024
	aiDefaultMaxTokens = 4096
)

func f64(v float64) *float64 { return &v }

// isQwenModel 与渲染进程 `renderer/services/ai.ts` 的判定一致：中转站 provider 一律 custom，模型名才是线索。
func isQwenModel(c aiClientConfig) bool {
	p := strings.ToLower(c.Provider)
	return strings.Contains(p, "qwen") || strings.Contains(p, "dashscope") || strings.Contains(strings.ToLower(c.Model), "qwen")
}

// describeChatError 从错误响应体里抠 provider 给的说明，抠不出报状态码。
func describeChatError(status int, body string) string {
	var v any
	if err := json.Unmarshal([]byte(body), &v); err == nil {
		if m, ok := v.(map[string]any); ok {
			// `inner?.message ?? errorJson.message ?? ""`：?? 只对 null/undefined 回落，空串不回落
			var msg any
			if inner, ok := m["error"].(map[string]any); ok {
				msg = inner["message"]
			}
			if msg == nil {
				msg = m["message"]
			}
			if jsTruthy(msg) {
				if s, ok := msg.(string); ok {
					return s
				}
				return fmt.Sprint(msg)
			}
		}
	}
	return fmt.Sprintf("AI API request failed (%d)", status)
}

// chatHTTP 返回对话客户端；测试通过 l.aiChatHTTP 注入。
func (l *Library) chatHTTP() *http.Client {
	if l.aiChatHTTP != nil {
		return l.aiChatHTTP
	}
	l.aiChatHTTPOnce.Do(func() {
		l.aiChatHTTPDefault = &http.Client{Transport: newProxiedTransport()}
	})
	return l.aiChatHTTPDefault
}

// chatCompletion 对应 TS chatCompletion。
func (l *Library) chatCompletion(ctx context.Context, cfg aiClientConfig, messages []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
	if cfg.APIKey == "" {
		return aiChatResult{}, errors.New("AI API Key is not configured")
	}
	if cfg.APIURL == "" {
		return aiChatResult{}, errors.New("AI API URL is not configured")
	}
	if cfg.Model == "" {
		return aiChatResult{}, errors.New("AI model is not configured")
	}
	protocol := resolveAIProtocol(cfg.APIProtocol, cfg.Provider, cfg.APIURL)
	endpoint := buildChatEndpoint(resolveProtocolBase(cfg.APIURL, protocol))
	accept := "application/json"
	if opts.Stream {
		accept = "text/event-stream"
	}
	headers := buildHeadersForProtocol(protocol, cfg.APIKey, headerOpts{Accept: accept})

	isAnthropic := protocol == "anthropic"
	model := cfg.Model
	if protocol == "gemini" {
		model = strings.TrimPrefix(model, "models/")
	}
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = aiDefaultMaxTokens
	}

	body := newOmap()
	if isAnthropic {
		var msgs []any
		for _, m := range messages {
			if m.Role == "system" {
				continue
			}
			role := "user"
			if m.Role == "assistant" {
				role = "assistant"
			}
			msgs = append(msgs, newOmap().set("role", role).set("content", m.Content))
		}
		if msgs == nil {
			msgs = []any{}
		}
		body.set("model", model).set("max_tokens", maxTokens).set("messages", msgs).set("stream", opts.Stream)
		for _, m := range messages {
			if m.Role == "system" { // find：只取第一条 system
				if m.Content != "" {
					body.set("system", m.Content)
				}
				break
			}
		}
	} else {
		msgs := make([]any, len(messages))
		for i, m := range messages {
			msgs[i] = newOmap().set("role", m.Role).set("content", m.Content)
		}
		temp := 0.3
		if opts.Temperature != nil {
			temp = *opts.Temperature
		}
		body.set("model", model).set("messages", msgs).set("temperature", temp).set("max_tokens", maxTokens).set("stream", opts.Stream)
		if opts.ResponseFormat != "" {
			body.set("response_format", newOmap().set("type", opts.ResponseFormat))
		}
		// Qwen3 系是混合推理模型默认开思考，主进程这几条链路的思考过程读不到也用不上，只是白烧 token 和时间。
		if isQwenModel(cfg) {
			body.set("enable_thinking", false)
		}
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = aiRequestTimeout
	}
	ctx, cancel := context.WithTimeoutCause(ctx, timeout, errors.New("AI 请求超时（"+timeout.String()+"）"))
	defer cancel()

	send := func() (*http.Response, error) {
		raw, err := body.MarshalJSON()
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("User-Agent", "node")
		resp, err := l.chatHTTP().Do(req)
		if err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
			return nil, err
		}
		return resp, nil
	}

	resp, err := send()
	if err != nil {
		return aiChatResult{}, err
	}
	defer func() { resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		errText := readBodyLossy(resp.Body)
		// 端点不认识 enable_thinking 时摘掉重发一次；只认「报错文本里点名了这个参数」，
		// 按状态码判会把限流、余额不足一并卷进来，白重发一次。
		if _, has := body.get("enable_thinking"); has && strings.Contains(strings.ToLower(errText), "enable_thinking") {
			fmt.Printf("[ai] 端点拒绝 enable_thinking，摘掉后重发一次：%s\n", utf16Head(errText, 200))
			body.del("enable_thinking")
			resp.Body.Close()
			resp, err = send()
			if err != nil {
				return aiChatResult{}, err
			}
			errText = ""
			if resp.StatusCode < 200 || resp.StatusCode > 299 {
				errText = readBodyLossy(resp.Body)
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return aiChatResult{}, errors.New(describeChatError(resp.StatusCode, errText))
		}
	}

	if opts.Stream && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readChatStream(resp.Body, protocol, opts.OnProgress, opts.OnDelta)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return aiChatResult{}, cause
		}
		return aiChatResult{}, err
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return aiChatResult{}, err
	}
	obj, _ := parsed.(map[string]any)
	var content string
	var haveContent bool
	if isAnthropic {
		var sb strings.Builder
		if arr, ok := obj["content"].([]any); ok {
			for _, it := range arr {
				if m, ok := it.(map[string]any); ok && m["type"] == "text" {
					if s, ok := m["text"].(string); ok {
						sb.WriteString(s)
					}
				}
			}
		}
		content, haveContent = sb.String(), true
	} else if choices, ok := obj["choices"].([]any); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]any); ok {
			if msg, ok := c0["message"].(map[string]any); ok {
				content, haveContent = msg["content"].(string)
			}
		}
	}
	if !haveContent {
		return aiChatResult{}, errors.New("AI API returned an unexpected response format")
	}
	var rawFinish any
	if isAnthropic {
		rawFinish = obj["stop_reason"]
	} else if choices, ok := obj["choices"].([]any); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]any); ok {
			rawFinish = c0["finish_reason"]
		}
	}
	finish, _ := rawFinish.(string)
	if isAnthropic && finish == "max_tokens" {
		finish = "length"
	}
	return aiChatResult{Content: content, FinishReason: finish, Usage: extractUsageFromChatResponse(obj, protocol)}, nil
}

func readBodyLossy(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return lossyUTF8(b)
}

// readChatStream 收集受限 SSE 文本；只在完整结束后把结果交给调用方（对应 readChatStream）。
func readChatStream(r io.Reader, protocol string, onProgress func(int), onDelta func(string)) (aiChatResult, error) {
	var (
		buf          []byte
		content      strings.Builder
		contentLen   int // UTF-16 码元数，对应 content.length
		finished     bool
		terminal     bool
		total        int
		finishReason string
		usage        = map[string]any{}
	)
	event := func(block string) error {
		var data []string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimLeftFunc(line[5:], jsSpace))
			}
		}
		joined := strings.Join(data, "\n")
		if joined == "" {
			return nil
		}
		if joined == "[DONE]" {
			finished, terminal = true, true
			return nil
		}
		var v any
		if err := json.Unmarshal([]byte(joined), &v); err != nil {
			return err
		}
		if v == nil {
			return errors.New("Cannot read properties of null (reading 'error')")
		}
		obj, _ := v.(map[string]any)
		if jsTruthy(obj["error"]) || obj["type"] == "error" {
			return errors.New("模型流式响应报告错误")
		}
		before := contentLen
		var delta string
		if protocol == "anthropic" {
			typ, _ := obj["type"].(string)
			switch typ {
			case "content_block_delta":
				if d, ok := obj["delta"].(map[string]any); ok && d["type"] == "text_delta" {
					delta, _ = d["text"].(string)
				}
			case "message_start":
				if msg, ok := obj["message"].(map[string]any); ok {
					if u, ok := msg["usage"].(map[string]any); ok {
						for k, val := range u {
							usage[k] = val
						}
					}
				}
			case "message_delta":
				if u, ok := obj["usage"].(map[string]any); ok {
					for k, val := range u {
						usage[k] = val
					}
				}
				finishReason = ""
				if d, ok := obj["delta"].(map[string]any); ok {
					finishReason, _ = d["stop_reason"].(string)
				}
			case "message_stop":
				finished, terminal = true, true
			}
		} else if choices, ok := obj["choices"].([]any); ok && len(choices) > 0 {
			if c0, ok := choices[0].(map[string]any); ok {
				if d, ok := c0["delta"].(map[string]any); ok {
					if s, ok := d["content"].(string); ok {
						delta = s
					}
				}
				if fr, ok := c0["finish_reason"].(string); ok && fr != "" {
					finishReason, finished = fr, true
				}
			}
		}
		if u, ok := obj["usage"]; ok && protocol != "anthropic" && jsTruthy(u) {
			if m, ok := u.(map[string]any); ok {
				usage = m
			} else {
				usage = map[string]any{}
			}
		}
		if delta != "" {
			content.WriteString(delta)
			contentLen += utf16Len(delta)
		}
		if contentLen != before {
			if onProgress != nil {
				onProgress(contentLen)
			}
			if onDelta != nil {
				onDelta(delta)
			}
		}
		return nil
	}

	chunk := make([]byte, 32*1024)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			total += n
			if total > aiStreamMaxBytes {
				return aiChatResult{}, errors.New("模型响应超过大小上限")
			}
			buf = append(buf, chunk[:n]...)
			// SSE 允许 CRLF，保留分块边界上的 CR，等 LF 到达后再归一。
			buf = bytes.ReplaceAll(buf, []byte("\r\n"), []byte("\n"))
			for !terminal {
				i := bytes.Index(buf, []byte("\n\n"))
				if i < 0 {
					break
				}
				block := lossyUTF8(buf[:i])
				buf = buf[i+2:]
				if e := event(block); e != nil {
					return aiChatResult{}, e
				}
			}
			if terminal { // 结束标记后主动回收连接；代理可能不会立即关闭 SSE 响应
				break
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return aiChatResult{}, err
		}
	}
	if !terminal && len(bytes.TrimFunc(buf, func(r rune) bool { return jsSpace(r) })) > 0 {
		if e := event(lossyUTF8(buf)); e != nil {
			return aiChatResult{}, e
		}
	}
	if !finished || jsTrim(content.String()) == "" {
		return aiChatResult{}, errors.New("模型流式响应未完整结束，请重试")
	}
	if finishReason == "max_tokens" {
		finishReason = "length"
	}
	return aiChatResult{Content: content.String(), FinishReason: finishReason, Usage: extractUsageFromChatResponse(map[string]any{"usage": usage}, protocol)}, nil
}

// del 删除键（保持其余键序）。
func (m *omap) del(k string) {
	if _, ok := m.vals[k]; !ok {
		return
	}
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}
