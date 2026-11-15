package aiflow

// 对应 packages/shared/utils/ai-protocol.ts：协议判定、端点拼接、请求头、响应文本与用量提取。
// 只移植 aiflow 用到的部分（对话、embeddings、模型列表）；文生图与转写端点不在无头 AI 编排的范围内。

import (
	"math"
	"regexp"
	"strings"
)

// 协议取值。
const (
	ProtocolOpenAI    = "openai"
	ProtocolGemini    = "gemini"
	ProtocolAnthropic = "anthropic"
)

// ResolvedBase 对应 ResolvedAIProtocolBase。
type ResolvedBase struct {
	Protocol string
	Explicit bool // 地址以 # 结尾：原样使用，不补路径
	BaseURL  string
}

var endpointSuffixes = []string{
	"/chat/completions", "/completions", "/models", "/embeddings", "/images/generations",
}

var (
	reVersionSuffix     = regexp.MustCompile(`/v\d+$`)
	reVersionBetaSuffix = regexp.MustCompile(`/v\d+(?:beta)?$`)
)

// ResolveAIProtocol 对应 resolveAIProtocol：显式协议优先，否则按 provider 与地址推断，默认 openai。
func ResolveAIProtocol(apiProtocol, provider, apiURL string) string {
	switch apiProtocol {
	case ProtocolOpenAI, ProtocolGemini, ProtocolAnthropic:
		return apiProtocol
	}
	p, u := jsLower(provider), jsLower(apiURL)
	if p == "anthropic" || strings.Contains(u, "api.anthropic.com") {
		return ProtocolAnthropic
	}
	if p == "google" || p == "gemini" || strings.Contains(u, "generativelanguage.googleapis.com") {
		return ProtocolGemini
	}
	return ProtocolOpenAI
}

// GetBaseURL 对应 getBaseUrl：去掉结尾 / 与已知端点后缀；以 # 结尾只去掉 #。
func GetBaseURL(apiURL string) string {
	if apiURL == "" {
		return ""
	}
	u := jsTrim(apiURL)
	if strings.HasSuffix(u, "#") {
		return u[:len(u)-1]
	}
	u = strings.TrimSuffix(u, "/")
	for _, suffix := range endpointSuffixes {
		if strings.HasSuffix(u, suffix) {
			return u[:len(u)-len(suffix)]
		}
	}
	return u
}

// NormalizeAPIURLInput 对应 normalizeApiUrlInput。
func NormalizeAPIURLInput(apiURL string) string {
	if apiURL == "" {
		return ""
	}
	trimmed := jsTrim(apiURL)
	explicit := strings.HasSuffix(trimmed, "#")
	raw := trimmed
	if explicit {
		raw = trimmed[:len(trimmed)-1]
	}
	normalized := GetBaseURL(raw)
	if normalized == "" {
		if explicit {
			return "#"
		}
		return ""
	}
	if explicit {
		return normalized + "#"
	}
	return normalized
}

// ResolveProtocolBase 对应 resolveProtocolBase。
func ResolveProtocolBase(apiURL, protocol string) ResolvedBase {
	trimmed := jsTrim(apiURL)
	explicit := strings.HasSuffix(trimmed, "#")
	raw := trimmed
	if explicit {
		raw = trimmed[:len(trimmed)-1]
	}
	return ResolvedBase{Protocol: protocol, Explicit: explicit, BaseURL: GetBaseURL(raw)}
}

func buildGeminiChatEndpoint(base string) string {
	if strings.HasSuffix(base, "/openai") {
		return base + "/chat/completions"
	}
	if reVersionBetaSuffix.MatchString(base) {
		return base + "/openai/chat/completions"
	}
	return base + "/v1beta/openai/chat/completions"
}

// BuildChatEndpoint 对应 buildChatEndpointFromBase。
func BuildChatEndpoint(r ResolvedBase) string {
	base := strings.TrimSuffix(r.BaseURL, "/")
	if base == "" || r.Explicit {
		return base
	}
	switch r.Protocol {
	case ProtocolGemini:
		return buildGeminiChatEndpoint(base)
	case ProtocolAnthropic:
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

// BuildEmbeddingsEndpoint 对应 buildEmbeddingsEndpointFromBase：anthropic 没有 embeddings，返回空串。
func BuildEmbeddingsEndpoint(r ResolvedBase) string {
	base := strings.TrimSuffix(r.BaseURL, "/")
	if base == "" || r.Explicit {
		return base
	}
	switch r.Protocol {
	case ProtocolAnthropic:
		return ""
	case ProtocolGemini:
		if strings.HasSuffix(base, "/openai") {
			return base + "/embeddings"
		}
		if reVersionBetaSuffix.MatchString(base) {
			return base + "/openai/embeddings"
		}
		return base + "/v1beta/openai/embeddings"
	}
	if reVersionSuffix.MatchString(base) {
		return base + "/embeddings"
	}
	return base + "/v1/embeddings"
}

// BuildModelsEndpoint 对应 buildModelsEndpointFromBase。
func BuildModelsEndpoint(r ResolvedBase) string {
	base := strings.TrimSuffix(r.BaseURL, "/")
	if base == "" || r.Explicit {
		return base
	}
	if r.Protocol == ProtocolGemini {
		gemini := strings.TrimSuffix(base, "/openai")
		if reVersionBetaSuffix.MatchString(gemini) {
			return gemini + "/models"
		}
		return gemini + "/v1beta/models"
	}
	protocolBase := strings.TrimSuffix(base, "/messages")
	if reVersionSuffix.MatchString(protocolBase) {
		return protocolBase + "/models"
	}
	return protocolBase + "/v1/models"
}

// HeaderOptions 对应 buildHeadersForProtocol 的 options。
type HeaderOptions struct {
	Accept              string
	NoContentType       bool // 对应 contentType: false
	UseNativeGeminiAuth bool
}

// BuildHeaders 对应 buildHeadersForProtocol。
func BuildHeaders(protocol, apiKey string, o HeaderOptions) map[string]string {
	h := map[string]string{}
	if !o.NoContentType {
		h["Content-Type"] = "application/json"
	}
	if o.Accept != "" {
		h["Accept"] = o.Accept
	}
	switch {
	case protocol == ProtocolAnthropic:
		h["x-api-key"] = apiKey
		h["anthropic-version"] = "2023-06-01"
	case protocol == ProtocolGemini && o.UseNativeGeminiAuth:
		h["x-goog-api-key"] = apiKey
	default:
		h["Authorization"] = "Bearer " + apiKey
	}
	return h
}

// TokenUsage 对应 ChatTokenUsage / ChatCompletionUsage。
type TokenUsage struct {
	PromptTokens, CompletionTokens float64
}

// ExtractUsage 对应 extractUsageFromChatResponse；接口没回报时返回 nil。payload 是 JSON 解码后的值。
func ExtractUsage(payload any, protocol string) *TokenUsage {
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	raw := obj["usage"]
	if raw == nil { // `??` 只在 null/undefined 时回落到 Gemini 原生的 usageMetadata
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
	if protocol == ProtocolAnthropic {
		prompt, hasPrompt = pick("input_tokens")
		completion, hasCompletion = pick("output_tokens")
	} else {
		prompt, hasPrompt = pick("prompt_tokens", "promptTokenCount")
		completion, hasCompletion = pick("completion_tokens", "candidatesTokenCount")
	}
	if !hasPrompt && !hasCompletion {
		return nil
	}
	return &TokenUsage{PromptTokens: prompt, CompletionTokens: completion}
}

// ExtractChatText 对应 extractTextFromChatResponse：OpenAI/Gemini 兼容层取 choices[0].message.content，
// Anthropic 取顶层 content 块数组；取不到返回 ok=false。
func ExtractChatText(payload any, protocol string) (string, bool) {
	obj, ok := payload.(map[string]any)
	if !ok {
		return "", false
	}
	flatten := func(value any) (string, bool) {
		switch v := value.(type) {
		case string:
			return v, true
		case []any:
			var b strings.Builder
			for _, part := range v {
				if m, ok := part.(map[string]any); ok {
					if t, has := m["text"]; has {
						if t == nil {
							continue // String(undefined ?? "") → ""
						}
						b.WriteString(jsToString(t))
					}
				}
			}
			return b.String(), true
		}
		return "", false
	}
	if protocol == ProtocolAnthropic {
		return flatten(obj["content"])
	}
	choices, ok := obj["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", false
	}
	first, _ := choices[0].(map[string]any)
	message, _ := first["message"].(map[string]any)
	return flatten(message["content"])
}
