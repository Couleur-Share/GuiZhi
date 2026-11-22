package main

// 知识库 AI 工具（摘要、标签建议、图片 OCR 与局部更新字段）的原生服务适配器。
// 配置、代理请求、图片读取、用量计量与条目写入一律走现有 Bridge 处理器，与渲染进程共用一套实现。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

type staticAIConfig struct {
	settings aiflow.AISettings
}

func (s staticAIConfig) AISettings() aiflow.AISettings { return s.settings }

type bridgeTransport struct {
	b *Bridge
}

func (t bridgeTransport) payload(requestID string, req aiflow.TransportRequest) map[string]any {
	payload := map[string]any{
		"requestId": requestID,
		"method":    req.Method,
		"url":       req.URL,
		"headers":   req.Headers,
	}
	if req.Body != "" {
		payload["body"] = req.Body
	}
	if req.TimeoutMs > 0 {
		payload["timeoutMs"] = req.TimeoutMs
	}
	return payload
}

func (t bridgeTransport) Request(ctx context.Context, req aiflow.TransportRequest) (aiflow.TransportResponse, error) {
	return callAs[aiflow.TransportResponse](ctx, t.b, chAIHTTPRequest, t.payload("", req))
}

// bridgeStreamSeq 给进程内发起的流式请求编号：代理按 requestId 登记取消，同一进程内不能重复。
var bridgeStreamSeq atomic.Uint64

// RequestStream 走 ai:httpStream（同一个代理、同一套 SSRF 守卫）：没有调用方页面，
// 流式块经 ctx 里的 sink 交给 h，在读流的 goroutine 上同步回调；ctx 取消即中断请求。
func (t bridgeTransport) RequestStream(ctx context.Context, req aiflow.TransportRequest, h aiflow.StreamHandler) (aiflow.TransportResponse, error) {
	sctx := withAIStreamSink(ctx, func(channel string, payload aiStreamPayload) {
		switch channel {
		case chAIHTTPStreamChunk:
			if h.OnChunk != nil {
				h.OnChunk(payload.Chunk)
			}
		case chAIHTTPStreamError:
			if h.OnError != nil {
				h.OnError(payload.Error)
			}
		}
	})
	id := "native-" + strconv.FormatUint(bridgeStreamSeq.Add(1), 10)
	return callAs[aiflow.TransportResponse](sctx, t.b, chAIHTTPStream, t.payload(id, req))
}

type bridgeUsageSink struct {
	b   *Bridge
	ctx context.Context
}

func (u bridgeUsageSink) RecordUsage(entry aiflow.UsageEntry) {
	_, _ = u.b.call(context.WithoutCancel(u.ctx), "ai:usageRecord", map[string]any{
		"scenario":         entry.Scenario,
		"model":            entry.Model,
		"promptTokens":     entry.PromptTokens,
		"completionTokens": entry.CompletionTokens,
		"failed":           entry.Failed,
	})
}

func (a libraryAdapter) newAIRunner(ctx context.Context) (*aiflow.Runner, error) {
	return newBridgeRunner(ctx, a.b, nil)
}

// newBridgeRunner 按当前设置组装一个经壳代理发请求的 Runner；log 为 nil 表示不记诊断日志。
func newBridgeRunner(ctx context.Context, b *Bridge, log aiflow.Logger) (*aiflow.Runner, error) {
	raw, err := callAs[map[string]any](ctx, b, "settings:get")
	if err != nil {
		return nil, err
	}
	return &aiflow.Runner{
		Client: &aiflow.Client{Transport: bridgeTransport{b: b}, Log: log},
		Config: staticAIConfig{settings: aiflow.NormalizeAISettings(raw)},
		Usage:  bridgeUsageSink{b: b, ctx: ctx},
		Log:    log,
	}, nil
}

func mapLibraryAIError(err error) error {
	if err == nil {
		return nil
	}
	if aiflow.IsNotConfigured(err) {
		return services.ErrAINotConfigured
	}
	return err
}

func (a libraryAdapter) GenerateSummary(ctx context.Context, title, content string) (services.LibrarySummaryResult, error) {
	runner, err := a.newAIRunner(ctx)
	if err != nil {
		return services.LibrarySummaryResult{}, err
	}
	res, err := runner.GenerateSummary(ctx, title, content)
	if err != nil {
		return services.LibrarySummaryResult{}, mapLibraryAIError(err)
	}
	return services.LibrarySummaryResult{Text: res.Text, Truncated: res.Truncated}, nil
}

func (a libraryAdapter) SuggestTags(ctx context.Context, title, content string) ([]string, error) {
	runner, err := a.newAIRunner(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := runner.SuggestTags(ctx, title, content)
	if err != nil {
		return nil, mapLibraryAIError(err)
	}
	return tags, nil
}

func (a libraryAdapter) RecognizeImageText(ctx context.Context, assetFileName string) (string, error) {
	runner, err := a.newAIRunner(ctx)
	if err != nil {
		return "", err
	}
	cfg, ok := runner.ResolveConfig(aiflow.ScenarioOCR)
	if !ok {
		return "", services.ErrAINotConfigured
	}
	b64, err := callAs[string](ctx, a.b, "image:readBase64", assetFileName)
	if err != nil {
		return "", err
	}
	if b64 == "" {
		return "", fmt.Errorf("无法读取本地图片：%s", assetFileName)
	}

	protocol := aiflow.ResolveAIProtocol(cfg.APIProtocol, cfg.Provider, cfg.APIURL)
	normalizedModel := cfg.Model
	if protocol == aiflow.ProtocolGemini {
		normalizedModel = strings.TrimPrefix(normalizedModel, "models/")
	}
	dataURL := "data:" + imageMimeFromFileName(assetFileName) + ";base64," + b64
	body, err := buildOcrRequestBody(normalizedModel, dataURL, protocol)
	if err != nil {
		return "", err
	}
	endpoint := aiflow.BuildChatEndpoint(aiflow.ResolveProtocolBase(cfg.APIURL, protocol))
	headers := aiflow.BuildHeaders(protocol, cfg.APIKey, aiflow.HeaderOptions{Accept: "application/json"})

	resp, err := bridgeTransport{b: a.b}.Request(ctx, aiflow.TransportRequest{
		Method:    http.MethodPost,
		URL:       endpoint,
		Headers:   headers,
		Body:      body,
		TimeoutMs: float64(ocrTimeout.Milliseconds()),
	})
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	if !resp.OK {
		runner.Usage.RecordUsage(aiflow.UsageEntry{
			Scenario: string(aiflow.ScenarioOCR),
			Model:    normalizedModel,
			Failed:   true,
		})
		return "", errors.New(aiflow.FormatHTTPError(resp, aiflow.FormatErrorOptions{Operation: "OCR"}))
	}

	text, err := parseOcrResponse(resp.Body, protocol)
	if err != nil {
		runner.Usage.RecordUsage(aiflow.UsageEntry{
			Scenario: string(aiflow.ScenarioOCR),
			Model:    normalizedModel,
			Failed:   true,
		})
		return "", err
	}
	var raw any
	_ = json.Unmarshal([]byte(resp.Body), &raw)
	entry := aiflow.UsageEntry{Scenario: string(aiflow.ScenarioOCR), Model: normalizedModel}
	if u := aiflow.ExtractUsage(raw, protocol); u != nil {
		entry.PromptTokens = u.PromptTokens
		entry.CompletionTokens = u.CompletionTokens
	}
	runner.Usage.RecordUsage(entry)
	return text, nil
}

func (a libraryAdapter) UpdateItemFields(ctx context.Context, id string, patch services.LibraryItemFieldsPatch) (services.LibraryItem, bool, error) {
	item, err := callAs[*services.LibraryItem](ctx, a.b, "knowledge:update", id, patch)
	if err != nil || item == nil {
		return services.LibraryItem{}, false, err
	}
	return *item, true, nil
}
