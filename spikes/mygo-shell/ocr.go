package main

// 主进程图片 OCR：采集图文作品时逐图识别，识别结果随条目一并入库。
// 对应 services/media/ocr.ts 与 shared/utils/ocr-request.ts（请求构造与响应解析与渲染进程手动识别共用）。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ocrSectionHeading：识别结果在正文中的小节标题。采集时自动写入、详情页重新识别时整节替换，两端必须一致。
const ocrSectionHeading = "## 图中文字"

const (
	ocrPrompt = "请识别图片中的所有文字，尽量保留原始排版结构，输出为 Markdown。" +
		"只输出识别出的文字内容，不要添加任何解释或前后缀。" +
		"如果图片中没有文字，输出：（图中未识别到文字）"
	ocrMaxTokens = 4000
	ocrTimeout   = 120 * time.Second
)

var ocrMimeByExt = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "webp": "image/webp", "gif": "image/gif", "bmp": "image/bmp",
}

func imageMimeFromFileName(fileName string) string {
	ext := strings.ToLower(fileName[strings.LastIndex(fileName, ".")+1:]) // split(".").pop()：无点时整串即扩展名
	if m, ok := ocrMimeByExt[ext]; ok {
		return m
	}
	return "image/png"
}

var reDataURLBase64 = regexp.MustCompile(`^data:(.+?);base64,(.+)$`)

// buildOcrRequestBody 构造 OCR 请求体。Anthropic 的 /v1/messages 不认 image_url，图片要拆成 base64 的 source 块，
// max_tokens 也是必填顶层字段；Gemini 走 OpenAI 兼容层，与 OpenAI 同构。
func buildOcrRequestBody(model, imageDataURL, protocol string) (string, error) {
	var body *omap
	if protocol == "anthropic" {
		parts := []any{newOmap().set("type", "text").set("text", ocrPrompt)}
		if m := reDataURLBase64.FindStringSubmatch(imageDataURL); m != nil {
			parts = append(parts, newOmap().set("type", "image").set("source",
				newOmap().set("type", "base64").set("media_type", m[1]).set("data", m[2])))
		}
		body = newOmap().set("model", model).set("max_tokens", ocrMaxTokens).
			set("messages", []any{newOmap().set("role", "user").set("content", parts)}).set("stream", false)
	} else {
		parts := []any{
			newOmap().set("type", "text").set("text", ocrPrompt),
			newOmap().set("type", "image_url").set("image_url", newOmap().set("url", imageDataURL)),
		}
		body = newOmap().set("model", model).
			set("messages", []any{newOmap().set("role", "user").set("content", parts)}).
			set("temperature", 0).set("max_tokens", ocrMaxTokens).set("stream", false)
	}
	raw, err := body.MarshalJSON()
	return string(raw), err
}

// extractTextFromChatResponse 从各协议的对话响应里提取纯文本；取不到返回 ok=false。
func extractTextFromChatResponse(payload any, protocol string) (string, bool) {
	obj, isObj := payload.(map[string]any)
	if !isObj {
		return "", false
	}
	flatten := func(v any) (string, bool) {
		switch x := v.(type) {
		case string:
			return x, true
		case []any:
			var sb strings.Builder
			for _, part := range x {
				if po, ok := part.(map[string]any); ok {
					if t, has := po["text"]; has && t != nil {
						sb.WriteString(jsToString(t))
					}
				}
			}
			return sb.String(), true
		}
		return "", false
	}
	if protocol == "anthropic" {
		return flatten(obj["content"])
	}
	choices, ok := obj["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", false
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	return flatten(msg["content"])
}

func parseOcrResponse(body, protocol string) (string, error) {
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", err
	}
	text, ok := extractTextFromChatResponse(payload, protocol)
	if !ok {
		return "", errors.New("OCR 响应缺少文本内容")
	}
	return jsTrim(text), nil
}

type ocrModelConfig struct {
	APIURL      string
	APIKey      string
	Model       string
	APIProtocol string
	Provider    string
}

// resolveOcrConfig 从共享 AI 配置解析 visionText 路由模型；未配置返回 nil。
func (l *Library) resolveOcrConfig() *ocrModelConfig {
	cfg, err := l.readAIConfig()
	if err != nil {
		fmt.Printf("[media] 读取视觉模型配置失败: %v\n", err)
		return nil
	}
	id, _ := cfg.Routes.vals["visionText"].(string)
	if id == "" {
		return nil
	}
	for _, m := range cfg.Models {
		if m.vals["id"] != id {
			continue
		}
		str := func(k string) string { s, _ := m.vals[k].(string); return jsTrim(s) }
		out := &ocrModelConfig{APIURL: str("apiUrl"), APIKey: str("apiKey"), Model: str("model")}
		if out.APIURL == "" || out.APIKey == "" || out.Model == "" {
			return nil
		}
		out.APIProtocol, _ = m.vals["apiProtocol"].(string)
		out.Provider, _ = m.vals["provider"].(string)
		return out
	}
	return nil
}

// recognizeImageFile 识别本地图片文件中的文字。图文采集一条最多 9 张图就是 9 次视觉模型调用，逐张记用量。
func (l *Library) recognizeImageFile(ctx context.Context, filePath string, c ocrModelConfig) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	mime := imageMimeFromFileName(filepath.Base(filePath))
	protocol := resolveAIProtocol(c.APIProtocol, c.Provider, c.APIURL)
	endpoint := buildChatEndpoint(resolveProtocolBase(c.APIURL, protocol))
	body, err := buildOcrRequestBody(c.Model, "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(raw), protocol)
	if err != nil {
		return "", err
	}

	tctx, cancel := context.WithTimeoutCause(ctx, ocrTimeout, errors.New("OCR 请求超时（"+ocrTimeout.String()+"）"))
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodPost, endpoint, bytes.NewReader([]byte(body)))
	if err != nil {
		return "", err
	}
	for k, v := range buildHeadersForProtocol(protocol, c.APIKey, headerOpts{}) {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "node")
	fail := func() { l.recordMainAIUsage(ctx, mainAIUsage{Scenario: "ocr", Model: c.Model, Failed: true}) }
	resp, err := l.chatHTTP().Do(req)
	if err != nil {
		fail()
		if cause := context.Cause(tctx); cause != nil {
			return "", cause
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		fail()
		detail, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OCR 请求失败 (HTTP %d): %s", resp.StatusCode, utf16Slice(string(detail), 300))
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fail()
		return "", err
	}
	usage := mainAIUsage{Scenario: "ocr", Model: c.Model}
	var payload any
	if json.Unmarshal(respBody, &payload) == nil {
		if u := extractUsageFromChatResponse(payload, protocol); u != nil {
			usage.PromptTokens, usage.CompletionTokens = u.PromptTokens, u.CompletionTokens
		}
	}
	l.recordMainAIUsage(ctx, usage)
	text, err := parseOcrResponse(string(respBody), protocol)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", errors.New("OCR 未识别到内容")
	}
	return text, nil
}
