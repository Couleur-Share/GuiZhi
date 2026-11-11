package main

// 对应 packages/shared/utils/ai-protocol.ts 中主进程调用 AI 需要的部分：
// 协议判定、端点拼接、请求头与用量提取。
//
// 注意与 aiconfig.go 的 normalizeProtocol 区分：后者是 ai-config.ts 写配置时的归一化
// （provider 用「包含」判断，含 claude），这里是请求时的 resolveAIProtocol（provider 精确相等）。

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type resolvedProtocolBase struct {
	Protocol string
	Explicit bool
	BaseURL  string
}

var protocolEndpointSuffixes = []string{
	"/chat/completions", "/completions", "/models", "/embeddings", "/images/generations",
}

// resolveAIProtocol：显式协议优先；否则按 provider 与地址推断，默认 openai。
func resolveAIProtocol(apiProtocol, provider, apiURL string) string {
	switch apiProtocol {
	case "openai", "gemini", "anthropic":
		return apiProtocol
	}
	p, u := strings.ToLower(provider), strings.ToLower(apiURL)
	if p == "anthropic" || strings.Contains(u, "api.anthropic.com") {
		return "anthropic"
	}
	if p == "google" || p == "gemini" || strings.Contains(u, "generativelanguage.googleapis.com") {
		return "gemini"
	}
	return "openai"
}

// getBaseURL 对应 getBaseUrl：去掉结尾 `/` 与已知端点后缀；以 `#` 结尾的是「原样使用」标记，只去掉 `#`。
func getBaseURL(apiURL string) string {
	if apiURL == "" {
		return ""
	}
	u := jsTrim(apiURL)
	if strings.HasSuffix(u, "#") {
		return u[:len(u)-1]
	}
	u = strings.TrimSuffix(u, "/")
	for _, suffix := range protocolEndpointSuffixes {
		if strings.HasSuffix(u, suffix) {
			return u[:len(u)-len(suffix)]
		}
	}
	return u
}

func resolveProtocolBase(apiURL, protocol string) resolvedProtocolBase {
	trimmed := jsTrim(apiURL)
	explicit := strings.HasSuffix(trimmed, "#")
	raw := trimmed
	if explicit {
		raw = trimmed[:len(trimmed)-1]
	}
	return resolvedProtocolBase{Protocol: protocol, Explicit: explicit, BaseURL: getBaseURL(raw)}
}

var (
	reJSDecimal         = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
	reJSRadix           = regexp.MustCompile(`^0(?:[xX][0-9a-fA-F]+|[oO][0-7]+|[bB][01]+)$`)
	reVersionSuffix     = regexp.MustCompile(`/v\d+$`)
	reVersionBetaSuffix = regexp.MustCompile(`/v\d+(?:beta)?$`)
)

func buildGeminiChatEndpoint(base string) string {
	if strings.HasSuffix(base, "/openai") {
		return base + "/chat/completions"
	}
	if reVersionBetaSuffix.MatchString(base) {
		return base + "/openai/chat/completions"
	}
	return base + "/v1beta/openai/chat/completions"
}

func buildChatEndpoint(r resolvedProtocolBase) string {
	base := strings.TrimSuffix(r.BaseURL, "/")
	if base == "" || r.Explicit {
		return base
	}
	switch r.Protocol {
	case "gemini":
		return buildGeminiChatEndpoint(base)
	case "anthropic":
		if strings.HasSuffix(base, "/messages") {
			return base
		}
		if reVersionSuffix.MatchString(base) {
			return base + "/messages"
		}
		return base + "/v1/messages"
	}
	if reVersionSuffix.MatchString(base) {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

// buildTranscriptionsEndpoint 只有 OpenAI 兼容协议提供 multipart 的 /audio/transcriptions，其余返回空串。
func buildTranscriptionsEndpointFromBase(r resolvedProtocolBase) string {
	base := strings.TrimSuffix(r.BaseURL, "/")
	if base == "" {
		return ""
	}
	if r.Explicit {
		return base
	}
	if r.Protocol != "openai" {
		return ""
	}
	if reVersionSuffix.MatchString(base) {
		return base + "/audio/transcriptions"
	}
	return base + "/v1/audio/transcriptions"
}

type headerOpts struct {
	Accept      string
	NoContent   bool // 对应 contentType: false
	NativeGemin bool // 对应 useNativeGeminiAuth
}

func buildHeadersForProtocol(protocol, apiKey string, o headerOpts) map[string]string {
	h := map[string]string{}
	if !o.NoContent {
		h["Content-Type"] = "application/json"
	}
	if o.Accept != "" {
		h["Accept"] = o.Accept
	}
	switch {
	case protocol == "anthropic":
		h["x-api-key"] = apiKey
		h["anthropic-version"] = "2023-06-01"
	case protocol == "gemini" && o.NativeGemin:
		h["x-goog-api-key"] = apiKey
	default:
		h["Authorization"] = "Bearer " + apiKey
	}
	return h
}

// chatTokenUsage 对应 ChatTokenUsage。
type chatTokenUsage struct {
	PromptTokens, CompletionTokens float64
}

// jsNumber 近似 JavaScript 的 Number(value)；ok=false 表示 NaN。
// 覆盖 JSON 里会出现的类型：数字、字符串（含空串与首尾空白）、布尔、null；数组与对象视为 NaN
// （真实 JS 里 Number([]) 为 0，Number([5]) 为 5，用量字段不会这样出现）。
func jsNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x)
	case string:
		s := jsTrim(x)
		switch {
		case s == "":
			return 0, true
		case reJSDecimal.MatchString(s):
			f, err := strconv.ParseFloat(s, 64)
			return f, err == nil || errors.Is(err, strconv.ErrRange)
		case reJSRadix.MatchString(s):
			n, err := strconv.ParseUint(s[2:], map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[s[1]], 64)
			return float64(n), err == nil
		case s == "Infinity" || s == "+Infinity":
			return math.Inf(1), true // 有限性判断会排除
		case s == "-Infinity":
			return math.Inf(-1), true
		}
		return 0, false
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, true
	}
	return 0, false
}

// extractUsageFromChatResponse 对应同名 TS 函数；接口没回报时返回 nil。
// payload 是 JSON 解码后的对象（map[string]any）。
func extractUsageFromChatResponse(payload any, protocol string) *chatTokenUsage {
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	raw := obj["usage"]
	if raw == nil { // `??`：仅 null/undefined 时回落到 Gemini 原生的 usageMetadata
		raw = obj["usageMetadata"]
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	pick := func(names ...string) (float64, bool) {
		for _, n := range names {
			if v, has := fields[n]; has {
				if f, ok := jsNumber(v); ok && !math.IsInf(f, 0) {
					return f, true
				}
			}
		}
		return 0, false
	}
	var prompt, completion float64
	var hasPrompt, hasCompletion bool
	if protocol == "anthropic" {
		prompt, hasPrompt = pick("input_tokens")
		completion, hasCompletion = pick("output_tokens")
	} else {
		prompt, hasPrompt = pick("prompt_tokens", "promptTokenCount")
		completion, hasCompletion = pick("completion_tokens", "candidatesTokenCount")
	}
	if !hasPrompt && !hasCompletion {
		return nil
	}
	return &chatTokenUsage{PromptTokens: prompt, CompletionTokens: completion}
}
