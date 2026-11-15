package aiflow

// 图片 OCR（对应 services/knowledge-ai/ocr.ts 与 shared/utils/ocr-request.ts）：
// 走 ocr 场景（visionText 路由）的多模态 chat 请求，图片以 data URL 内联。
//
// 主包的采集管线另有一份 OCR（ocr.go，逐图识别图文作品并随条目入库），请求构造与响应解析同源；
// 两份合并留到 aiflow 成为唯一 AI 客户端之后（见 docs/agents/native-ui-development.md）。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// OCRSectionHeading 是识别结果在正文中的小节标题。采集时自动写入、详情页重新识别时整节替换，两端必须一致。
const OCRSectionHeading = "## 图中文字"

const (
	OCRPrompt = "请识别图片中的所有文字，尽量保留原始排版结构，输出为 Markdown。" +
		"只输出识别出的文字内容，不要添加任何解释或前后缀。" +
		"如果图片中没有文字，输出：（图中未识别到文字）"
	ocrMaxTokens = 4000
	ocrTimeoutMs = 120_000
)

var imageMimeByExt = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg",
	"webp": "image/webp", "gif": "image/gif", "bmp": "image/bmp",
}

// ImageMimeFromFileName 按扩展名推断图片 MIME，未知按 png。
func ImageMimeFromFileName(fileName string) string {
	extension := jsLower(fileName[strings.LastIndex(fileName, ".")+1:]) // split(".").pop()：无点时整串即扩展名
	if mime, ok := imageMimeByExt[extension]; ok {
		return mime
	}
	return "image/png"
}

// reDataURL 对应 /^data:(.+?);base64,(.+)$/：JS 的 . 不匹配 \n \r U+2028 U+2029。
var reDataURL = regexp.MustCompile(`^data:([^\n\r\x{2028}\x{2029}]+?);base64,([^\n\r\x{2028}\x{2029}]+)$`)

// BuildOcrRequestBody 构造 OCR 请求体。Anthropic 的 /v1/messages 不认 image_url，图片必须拆成 base64 的
// source 块，max_tokens 也是必填顶层字段；Gemini 走 OpenAI 兼容层，与 OpenAI 同构。
func BuildOcrRequestBody(model, imageDataURL, protocol string) string {
	if protocol == "anthropic" {
		parts := []any{newObj().set("type", "text").set("text", OCRPrompt)}
		if m := reDataURL.FindStringSubmatch(imageDataURL); m != nil {
			parts = append(parts, newObj().set("type", "image").set("source",
				newObj().set("type", "base64").set("media_type", m[1]).set("data", m[2])))
		}
		return newObj().set("model", model).set("max_tokens", ocrMaxTokens).
			set("messages", []any{newObj().set("role", "user").set("content", parts)}).
			set("stream", false).String()
	}
	parts := []any{
		newObj().set("type", "text").set("text", OCRPrompt),
		newObj().set("type", "image_url").set("image_url", newObj().set("url", imageDataURL)),
	}
	return newObj().set("model", model).
		set("messages", []any{newObj().set("role", "user").set("content", parts)}).
		set("temperature", 0).set("max_tokens", ocrMaxTokens).set("stream", false).String()
}

// ParseOcrResponse 解析对话响应正文（各协议结构不同，统一走共享提取器）。
func ParseOcrResponse(body, protocol string) (string, error) {
	payload, err := parseJSON(body)
	if err != nil {
		return "", fmt.Errorf("OCR 响应不是合法 JSON: %v", err)
	}
	text, ok := ExtractChatText(payload, protocol)
	if !ok {
		return "", errors.New("OCR 响应缺少文本内容")
	}
	return jsTrim(text), nil
}

// ImageReader 读取图片资产文件并返回 base64（对应 window.electron.readImageBase64 / image:readBase64）。
type ImageReader interface {
	ReadImageBase64(ctx context.Context, assetFileName string) (string, error)
}

// IsOCRConfigured 对应 isOcrConfigured：ocr 场景固定走 visionText 路由，候选已按 vision 能力过滤，不做旧版字段兜底。
func (r *Runner) IsOCRConfigured() bool {
	_, ok := r.resolveOCRConfig()
	return ok
}

func (r *Runner) resolveOCRConfig() (AIConfig, bool) {
	return r.Config.AISettings().ResolveScenarioConfig(ScenarioOCR, false, nil)
}

// RecognizeImageText 识别图片资产里的文字。图文采集逐张调视觉模型，9 张图就是 9 次，不记等于漏掉一整类消耗。
func (r *Runner) RecognizeImageText(ctx context.Context, assetFileName string) (string, error) {
	cfg, ok := r.resolveOCRConfig()
	if !ok {
		return "", ErrNotConfigured
	}
	base64 := ""
	if r.Images != nil {
		var err error
		if base64, err = r.Images.ReadImageBase64(ctx, assetFileName); err != nil {
			return "", err // 读取失败的原因（文件不存在、权限等）原样交给调用方展示
		}
	}
	if base64 == "" {
		return "", errors.New("无法读取图片资产文件")
	}

	mime := ImageMimeFromFileName(assetFileName)
	protocol := ResolveAIProtocol(cfg.APIProtocol, cfg.Provider, cfg.APIURL)
	endpoint := BuildChatEndpoint(ResolveProtocolBase(cfg.APIURL, protocol))
	resp, err := cancellable(ctx, func() (TransportResponse, error) {
		return r.Client.Transport.Request(ctx, TransportRequest{
			Method:    "POST",
			URL:       endpoint,
			Headers:   BuildHeaders(protocol, cfg.APIKey, HeaderOptions{}),
			Body:      BuildOcrRequestBody(cfg.Model, "data:"+mime+";base64,"+base64, protocol),
			TimeoutMs: ocrTimeoutMs,
		})
	})
	if err != nil {
		r.recordUsage(UsageEntry{Scenario: "ocr", Model: cfg.Model, Failed: true})
		return "", err
	}
	if !resp.OK {
		r.recordUsage(UsageEntry{Scenario: "ocr", Model: cfg.Model, Failed: true})
		detail := resp.Body
		if resp.Error != nil && *resp.Error != "" { // `response.error || response.body`：空串落到 body
			detail = *resp.Error
		}
		return "", fmt.Errorf("OCR 请求失败 (HTTP %d): %s", resp.Status, u16Head(detail, 300))
	}

	entry := UsageEntry{Scenario: "ocr", Model: cfg.Model}
	if payload, err := parseJSON(resp.Body); err == nil {
		// 渲染层的 OCR 只记次数不记 token；主包的采集 OCR 记 token，这里对齐后者，面板上两条路的口径一致
		if usage := ExtractUsage(payload, protocol); usage != nil {
			entry.PromptTokens, entry.CompletionTokens = usage.PromptTokens, usage.CompletionTokens
		}
	}
	r.recordUsage(entry)

	text, err := ParseOcrResponse(resp.Body, protocol)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", errors.New("OCR 未识别到内容")
	}
	return text, nil
}
