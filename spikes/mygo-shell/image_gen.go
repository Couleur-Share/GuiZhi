package main

// 文生图适配器。对应 services/illustration/image-gen.ts 中主题阅读用到的部分（generateImage 及其协议分流）：
//   - OpenAI gpt-image-* 不接受 response_format；其余模型必须显式要 b64_json，否则回 60 分钟过期的 URL；
//   - 只有 gpt-image-2 与 Gemini 能给出真正的 16:9，其余 OpenAI 模型只有三档固定尺寸；
//   - Gemini 走原生 generateContent（请求体里才带得了 aspectRatio）；Anthropic 没有文生图接口，给可读提示。
// 连接测试（probe）属于设置页，不在这里。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	imageGenTimeout     = 240 * time.Second
	imageRetryBudget    = 300 * time.Second
	imageResponseMax    = 40 * 1024 * 1024
	imageModerationCode = "moderation_blocked"
)

var imageDefaultRetryDelays = []time.Duration{2 * time.Second, 6 * time.Second}

type generatedImage struct {
	Data      []byte
	Extension string // 含点，如 ".png"
}

type imageGenOptions struct {
	Scenario string
	Timeout  time.Duration // 0 = 240 秒；上限 480 秒
	// OnRequest 每次 HTTP 尝试前同步记账；返回错误则阻止发送，不参与网络重试。
	OnRequest func() error
	// RetryDelays 为 nil 用默认退避；空切片表示不重试。
	RetryDelays []time.Duration
	retrySet    bool
}

// withNoRetry 明确不重试（主题阅读：生图按张计费，失败交给用户手动补生成）。
func (o imageGenOptions) withNoRetry() imageGenOptions {
	o.RetryDelays, o.retrySet = nil, true
	return o
}

func openAIImageSize(model, aspect string) string {
	if regexp.MustCompile(`(?i)gpt-image-2`).MatchString(model) {
		switch aspect {
		case "16:9":
			return "1536x864"
		case "4:3":
			return "1280x960"
		}
		return "1024x1024"
	}
	if aspect == "1:1" {
		return "1024x1024"
	}
	return "1536x1024"
}

func buildOpenAIImageBody(model, prompt, aspect string) string {
	body := newObj()
	body.set("model", model)
	body.set("prompt", prompt)
	body.set("n", float64(1))
	body.set("size", openAIImageSize(model, aspect))
	if !regexp.MustCompile(`(?i)gpt-image`).MatchString(model) {
		body.set("response_format", "b64_json")
	}
	s, _ := jsStringify(body)
	return s
}

func buildGeminiImageBody(prompt, aspect string) string {
	b, _ := json.Marshal(map[string]any{
		"contents":         []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": prompt}}}},
		"generationConfig": map[string]any{"responseModalities": []string{"IMAGE"}, "imageConfig": map[string]any{"aspectRatio": aspect}},
	})
	return string(b)
}

func buildImagesEndpoint(r resolvedProtocolBase) string {
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
		return base + "/images/generations"
	}
	return base + "/v1/images/generations"
}

func buildGeminiImageEndpoint(r resolvedProtocolBase, model string) string {
	base := strings.TrimSuffix(r.BaseURL, "/")
	if base == "" {
		return ""
	}
	if r.Explicit {
		return base
	}
	native := strings.TrimSuffix(base, "/openai")
	if !reVersionBetaSuffix.MatchString(native) {
		native += "/v1beta"
	}
	// encodeURIComponent 与 url.PathEscape 对模型名里常见字符（字母数字 . - _ /）的差异：只有 "/" 需要编码
	return native + "/models/" + strings.ReplaceAll(url.PathEscape(model), "/", "%2F") + ":generateContent"
}

type imagePayload struct {
	b64, mime, url string
}

func parseOpenAIImageResponse(raw string) (imagePayload, error) {
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return imagePayload{}, err
	}
	if list, ok := generic["data"].([]any); ok && len(list) > 0 {
		if first, ok := list[0].(map[string]any); ok {
			if s, ok := first["b64_json"].(string); ok && s != "" {
				return imagePayload{b64: s}, nil
			}
			if s, ok := first["url"].(string); ok && s != "" {
				return imagePayload{url: s}, nil
			}
		}
	}
	return imagePayload{}, errors.New(imageErrorMessage(generic, "文生图响应里没有图片数据"))
}

func parseGeminiImageResponse(raw string) (imagePayload, error) {
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return imagePayload{}, err
	}
	if cands, ok := generic["candidates"].([]any); ok && len(cands) > 0 {
		if c, ok := cands[0].(map[string]any); ok {
			if content, ok := c["content"].(map[string]any); ok {
				parts, _ := content["parts"].([]any)
				for _, part := range parts {
					pm, _ := part.(map[string]any)
					inline, _ := pm["inlineData"].(map[string]any)
					if inline == nil {
						inline, _ = pm["inline_data"].(map[string]any)
					}
					if data, ok := inline["data"].(string); ok && data != "" {
						mime, _ := inline["mimeType"].(string)
						if mime == "" {
							mime, _ = inline["mime_type"].(string)
						}
						return imagePayload{b64: data, mime: mime}, nil
					}
				}
			}
		}
	}
	return imagePayload{}, errors.New(imageErrorMessage(generic, "文生图响应里没有图片数据"))
}

func imageErrorMessage(generic map[string]any, fallback string) string {
	if e, ok := generic["error"].(map[string]any); ok {
		if m, ok := e["message"].(string); ok && m != "" {
			return m
		}
	}
	return fallback
}

var imageMimeExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/jpg": ".jpg", "image/webp": ".webp"}

var reDataURLImage = regexp.MustCompile(`(?s)^data:(image/[\w.+-]+);base64,(.*)$`)

func decodeBase64Image(p imagePayload) (generatedImage, error) {
	data, mime := p.b64, p.mime
	if m := reDataURLImage.FindStringSubmatch(data); m != nil {
		data, mime = m[2], m[1]
	}
	data = strings.Join(strings.Fields(data), "")
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		// Node 的 Buffer.from(…, "base64") 宽容：缺填充或尾部多余字符不报错
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(data, "="))
		if err != nil {
			raw = nil
		}
	}
	if len(raw) == 0 {
		return generatedImage{}, errors.New("文生图返回的图片数据为空")
	}
	if int64(len(raw)) > mediaSizeLimits["image"] {
		return generatedImage{}, errors.New("文生图返回的图片超过大小上限")
	}
	ext := imageMimeExt[strings.ToLower(mime)]
	if ext == "" {
		ext = ".png"
	}
	return generatedImage{Data: raw, Extension: ext}, nil
}

// imageAttemptError 是单次尝试的失败；Retryable 决定要不要换一次上游重发。
type imageAttemptError struct {
	msg       string
	Retryable bool
}

func (e *imageAttemptError) Error() string { return e.msg }

func isRetryableImageStatus(status int) bool { return status == 429 || status >= 500 }

func parseImageErrorBody(text string) (message, code string) {
	var v struct {
		Error struct {
			Message any `json:"message"`
			Code    any `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &v); err == nil {
		m, _ := v.Error.Message.(string)
		c, _ := v.Error.Code.(string)
		if m != "" {
			return truncateRunes(m, 300), c
		}
	}
	return truncateRunes(strings.Join(strings.Fields(text), " "), 300), ""
}

func truncateRunes(s string, n int) string {
	u := toUTF16(s)
	if len(u) <= n {
		return s
	}
	var sb strings.Builder
	for _, r := range string(unitsToRunes(u[:n])) {
		sb.WriteRune(r)
	}
	return sb.String()
}

func unitsToRunes(u []uint16) []rune {
	var out []rune
	for i := 0; i < len(u); i++ {
		c := rune(u[i])
		if c >= 0xd800 && c <= 0xdbff && i+1 < len(u) && u[i+1] >= 0xdc00 && u[i+1] <= 0xdfff {
			c = 0x10000 + (c-0xd800)<<10 + (rune(u[i+1]) - 0xdc00)
			i++
		}
		out = append(out, c)
	}
	return out
}

func describeImageHTTPFailure(status int, body string) string {
	message, code := parseImageErrorBody(body)
	if code == imageModerationCode {
		return fmt.Sprintf("文生图被内容安全拦截，改写图题、物件或标注词后再试 (HTTP %d %s): %s", status, code, message)
	}
	c := ""
	if code != "" {
		c = " " + code
	}
	return fmt.Sprintf("文生图请求失败 (HTTP %d%s): %s", status, c, message)
}

type imageEndpoint struct {
	url, body string
	headers   map[string]string
	parse     func(string) (imagePayload, error)
}

func resolveImageEndpoint(cfg aiClientConfig, prompt, aspect string) (imageEndpoint, error) {
	protocol := resolveAIProtocol(cfg.APIProtocol, cfg.Provider, cfg.APIURL)
	if protocol == "anthropic" {
		return imageEndpoint{}, errors.New("Anthropic 协议没有文生图接口——请把配图路由指向 OpenAI 或 Gemini 协议的模型")
	}
	base := resolveProtocolBase(cfg.APIURL, protocol)
	if protocol == "gemini" {
		u := buildGeminiImageEndpoint(base, cfg.Model)
		if u == "" {
			return imageEndpoint{}, errors.New("无法拼出 Gemini 生图端点，请检查该模型的 API 地址")
		}
		return imageEndpoint{u, buildGeminiImageBody(prompt, aspect), buildHeadersForProtocol(protocol, cfg.APIKey, headerOpts{NativeGemin: true}), parseGeminiImageResponse}, nil
	}
	u := buildImagesEndpoint(base)
	if u == "" {
		return imageEndpoint{}, errors.New("无法拼出文生图端点，请检查该模型的 API 地址")
	}
	return imageEndpoint{u, buildOpenAIImageBody(cfg.Model, prompt, aspect), buildHeadersForProtocol(protocol, cfg.APIKey, headerOpts{}), parseOpenAIImageResponse}, nil
}

func imageRequestTimeout(o imageGenOptions) (time.Duration, error) {
	d := o.Timeout
	if d == 0 {
		d = imageGenTimeout
	}
	if d <= 0 || d > 480*time.Second {
		return 0, errors.New("文生图请求超时上限必须在 1–480000 毫秒之间")
	}
	return d, nil
}

var reImageURLExt = regexp.MustCompile(`(?i)\.(png|jpe?g|webp)(?:$|[?#])`)

func (l *Library) downloadGeneratedImage(ctx context.Context, rawURL string) (generatedImage, error) {
	ext := ".png"
	if m := reImageURLExt.FindStringSubmatch(rawURL); m != nil {
		ext = "." + strings.ReplaceAll(strings.ToLower(m[1]), "jpeg", "jpg")
	}
	f := l.webFetcher
	if f == nil {
		f = defaultSafeFetcher
	}
	dir, file, err := f.downloadToTempFile(ctx, rawURL, "image"+ext, mediaSizeLimits["image"], safeRequestOptions{Accept: "image/*"})
	if err != nil {
		return generatedImage{}, err
	}
	defer os.RemoveAll(dir)
	data, err := os.ReadFile(file)
	if err != nil {
		return generatedImage{}, err
	}
	return generatedImage{Data: data, Extension: ext}, nil
}

func (l *Library) requestImage(ctx context.Context, ep imageEndpoint, o imageGenOptions) (generatedImage, error) {
	timeout, err := imageRequestTimeout(o)
	if err != nil {
		return generatedImage{}, err
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, ep.url, bytes.NewReader([]byte(ep.body)))
	if err != nil {
		return generatedImage{}, &imageAttemptError{"文生图请求失败：" + err.Error(), true}
	}
	for k, v := range ep.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.chatHTTP().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return generatedImage{}, context.Cause(ctx)
		}
		if errors.Is(err, context.DeadlineExceeded) || rctx.Err() != nil {
			return generatedImage{}, &imageAttemptError{fmt.Sprintf("文生图请求超时（%d 秒无响应）", int(timeout.Seconds()+0.5)), true}
		}
		return generatedImage{}, &imageAttemptError{"文生图请求失败：" + err.Error(), true}
	}
	defer resp.Body.Close()
	text, err := io.ReadAll(io.LimitReader(resp.Body, imageResponseMax+1))
	if err != nil {
		if ctx.Err() != nil {
			return generatedImage{}, context.Cause(ctx)
		}
		return generatedImage{}, &imageAttemptError{"文生图请求失败：" + err.Error(), true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return generatedImage{}, &imageAttemptError{describeImageHTTPFailure(resp.StatusCode, string(text)), isRetryableImageStatus(resp.StatusCode)}
	}
	if len(text) > imageResponseMax {
		return generatedImage{}, errors.New("文生图响应过大，已中止")
	}
	payload, err := ep.parse(string(text))
	if err != nil {
		return generatedImage{}, err
	}
	if payload.url != "" {
		return l.downloadGeneratedImage(ctx, payload.url)
	}
	return decodeBase64Image(payload)
}

// generateImage 按提示词生成一张图；成功按「一张图」记账，失败记一次失败。
func (l *Library) generateImage(ctx context.Context, prompt, aspect string, cfg aiClientConfig, o imageGenOptions) (generatedImage, error) {
	ep, err := resolveImageEndpoint(cfg, prompt, aspect)
	if err != nil {
		return generatedImage{}, err
	}
	scenario := o.Scenario
	if scenario == "" {
		scenario = "illustration"
	}
	delays := o.RetryDelays
	if !o.retrySet && delays == nil {
		delays = imageDefaultRetryDelays
	}
	deadline := time.Now().Add(imageRetryBudget)
	for attempt := 0; ; attempt++ {
		if o.OnRequest != nil {
			if _, err := imageRequestTimeout(o); err != nil {
				return generatedImage{}, err
			}
			if ctx.Err() != nil {
				return generatedImage{}, context.Cause(ctx)
			}
			if err := o.OnRequest(); err != nil {
				return generatedImage{}, err
			}
		}
		img, err := l.requestImage(ctx, ep, o)
		if err == nil {
			l.recordMainAIUsage(ctx, mainAIUsage{Scenario: scenario, Model: cfg.Model})
			return img, nil
		}
		if ctx.Err() != nil {
			return generatedImage{}, err
		}
		var ae *imageAttemptError
		retryable := errors.As(err, &ae) && ae.Retryable
		if !retryable || attempt >= len(delays) || !time.Now().Before(deadline) {
			l.recordMainAIUsage(ctx, mainAIUsage{Scenario: scenario, Model: cfg.Model, Failed: true})
			if attempt > 0 {
				return generatedImage{}, fmt.Errorf("%s（已自动重试 %d 次）", err.Error(), attempt)
			}
			return generatedImage{}, err
		}
		select {
		case <-time.After(delays[attempt]):
		case <-ctx.Done():
			return generatedImage{}, errors.New("已取消")
		}
	}
}
