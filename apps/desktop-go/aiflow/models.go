package aiflow

// 模型列表拉取与解析（对应 services/ai-models.ts）。
//
// 各家 /models 的响应形态不同：OpenAI 与 Anthropic 都是 `data[]` 但字段不同，Gemini 是 `models[]`
// 且 id 带 `models/` 前缀，还有中转站直接返回裸数组。这里把四种形态归一成 ModelInfo。

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ModelInfo 对应 ModelInfo（Gemini 另带 description）。
type ModelInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	OwnedBy     string   `json:"owned_by,omitempty"`
	Created     *float64 `json:"created,omitempty"`
	Description string   `json:"description,omitempty"`
}

// FetchModelsResult 对应 FetchModelsResult。
type FetchModelsResult struct {
	Success  bool
	Models   []ModelInfo
	Error    string
	Reason   string // auth | network | unsupported | http | parse
	Endpoint string
	Status   *int // 拿到过 HTTP 响应（含 status 0 的传输层失败）才有值
}

const fetchModelsTimeoutMs = 12_000

var reTimeout = regexp.MustCompile(`(?i)timeout`)

// APIEndpointPreview 对应 getApiEndpointPreview：展示用的完整对话端点（# 结尾不补路径，缺 /v1 自动补全）。
func APIEndpointPreview(apiURL, protocol string) string {
	if apiURL == "" {
		return ""
	}
	if protocol == "" {
		protocol = ProtocolOpenAI
	}
	return BuildChatEndpoint(ResolveProtocolBase(apiURL, protocol))
}

// FetchModels 对应 fetchAvailableModels。
func (c *Client) FetchModels(ctx context.Context, apiURL, apiKey, apiProtocol string) FetchModelsResult {
	if apiKey == "" || apiURL == "" {
		return FetchModelsResult{Error: "Please fill in API Key and API URL first"}
	}
	if apiProtocol == "" {
		apiProtocol = ProtocolOpenAI
	}
	endpoint := BuildModelsEndpoint(ResolveProtocolBase(apiURL, apiProtocol))
	resolved := ResolveAIProtocol(apiProtocol, "", apiURL)
	headers := BuildHeaders(resolved, apiKey, HeaderOptions{
		Accept: "application/json", UseNativeGeminiAuth: resolved == ProtocolGemini,
	})

	// 与 TS 一致：模型列表请求不挂取消（设置页的一次性探测）。
	resp, err := c.Transport.Request(ctx, TransportRequest{Method: "GET", URL: endpoint, Headers: headers, TimeoutMs: fetchModelsTimeoutMs})
	if err != nil {
		return modelsFailure(err.Error())
	}

	if !resp.OK {
		errorText := resp.Body
		if resp.Error != nil {
			errorText = *resp.Error
		}
		reason := "http"
		switch {
		case resp.Status == 401 || resp.Status == 403:
			reason = "auth"
		case resp.Status == 0 && reTimeout.MatchString(errorText):
			reason = "network"
		case resp.Status == 404 || resp.Status == 405 || resp.Status == 501:
			reason = "unsupported"
		}
		message := fmt.Sprintf("获取模型列表失败: %d - %s", resp.Status, u16Head(errorText, 100))
		if resp.Status == 0 {
			message = u16Head(errorText, 120)
		}
		status := resp.Status
		return FetchModelsResult{Error: message, Reason: reason, Endpoint: endpoint, Status: &status}
	}

	parsed, err := parseJSON(resp.Body)
	if err != nil {
		return modelsFailure(err.Error())
	}
	if models, ok := parseModelList(parsed, apiProtocol); ok {
		return FetchModelsResult{Success: true, Models: models}
	}
	return FetchModelsResult{Error: "无法解析模型列表响应", Reason: "unsupported", Endpoint: endpoint}
}

func modelsFailure(message string) FetchModelsResult {
	if message == "" {
		message = "获取模型列表失败"
	}
	lower := jsLower(message)
	reason := "http"
	if strings.Contains(lower, "failed to fetch") || strings.Contains(lower, "network") {
		reason = "network"
	}
	return FetchModelsResult{Error: message, Reason: reason}
}

func sortModels(models []ModelInfo) {
	sort.SliceStable(models, func(i, j int) bool { return jsLocaleCompare(models[i].ID, models[j].ID) < 0 })
}

// parseModelList 把四种响应形态归一成 ModelInfo；都不是返回 ok=false。
func parseModelList(data any, apiProtocol string) ([]ModelInfo, bool) {
	root, isMap := data.(map[string]any)
	if isMap && apiProtocol == ProtocolAnthropic {
		if list, ok := root["data"].([]any); ok {
			models := []ModelInfo{}
			for _, raw := range list {
				m, _ := raw.(map[string]any)
				id, ok := m["id"].(string)
				if !ok {
					continue
				}
				name := id
				if dn, _ := m["display_name"].(string); dn != "" {
					name = dn
				}
				info := ModelInfo{ID: id, Name: name, OwnedBy: "Anthropic"}
				if created, _ := m["created_at"].(string); created != "" {
					if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
						ms := float64(t.UnixMilli())
						info.Created = &ms
					}
				}
				models = append(models, info)
			}
			sortModels(models)
			return models, true
		}
	}
	if isMap {
		if list, ok := root["data"].([]any); ok { // OpenAI 格式
			models := []ModelInfo{}
			for _, raw := range list {
				m, _ := raw.(map[string]any)
				if !jsTruthy(m["id"]) {
					continue
				}
				id := jsToString(m["id"])
				info := ModelInfo{ID: id, Name: id}
				info.OwnedBy, _ = m["owned_by"].(string)
				if created, ok := m["created"].(float64); ok {
					info.Created = &created
				}
				models = append(models, info)
			}
			sortModels(models)
			return models, true
		}
		if list, ok := root["models"].([]any); ok { // Gemini 格式
			models := []ModelInfo{}
			for _, raw := range list {
				m, _ := raw.(map[string]any)
				name, _ := m["name"].(string)
				if name == "" {
					continue
				}
				id := strings.TrimPrefix(name, "models/") // "models/gemini-pro" → "gemini-pro"
				display, _ := m["displayName"].(string)
				info := ModelInfo{ID: id, Name: id, OwnedBy: "Google"}
				if display != "" {
					info.Name = fmt.Sprintf("%s (%s)", display, id)
				}
				info.Description, _ = m["description"].(string)
				models = append(models, info)
			}
			sortModels(models)
			return models, true
		}
	}
	if list, ok := data.([]any); ok { // 某些 API 直接返回数组
		models := []ModelInfo{}
		for _, raw := range list {
			m, _ := raw.(map[string]any)
			id, model := jsToStringTruthy(m["id"]), jsToStringTruthy(m["model"])
			if id == "" && model == "" {
				continue
			}
			pickedID := id
			if pickedID == "" {
				pickedID = model
			}
			name := jsToStringTruthy(m["name"])
			if name == "" {
				name = pickedID
			}
			models = append(models, ModelInfo{ID: pickedID, Name: name})
		}
		return models, true
	}
	return nil, false
}

// jsToStringTruthy 返回真值的字符串形式，假值返回空串（对应 `a || b` 链里取字符串）。
func jsToStringTruthy(v any) string {
	if !jsTruthy(v) {
		return ""
	}
	return jsToString(v)
}
