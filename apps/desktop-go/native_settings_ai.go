package main

// 设置页「AI 模型服务」分区的原生服务适配器（services.AISettings）。
//
// 配置读写走 settings:get / settings:set（落在 config/ai-models.json，校验与原子写入沿用现有处理器）；
// 连通性测试与拉取模型列表一律经 aiflow 的 Client / Runner，传输层是 bridgeTransport（ai:httpRequest 代理），
// 所以出站请求受 net_safety.go 的 SSRF 防护与网络代理设置约束，界面进程不直接联网；
// 用量走 ai:usageSummary / ai:usageClear。
//
// 机密：API Key 只出现在请求头与保存的配置里；返回给界面的错误一律抹掉当前请求的密钥（scrubSecret），
// 本文件不写任何日志。

import (
	"context"
	"errors"
	"strings"
	"time"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

type aiSettingsAdapter struct{ b *Bridge }

var _ services.AISettings = aiSettingsAdapter{}

// imageGenTestUnsupported 与渲染层一致：文生图的连通测试要真实出图并计费，不提供。
const imageGenTestUnsupported = "本版本不提供文生图模型的连通测试，请在主题阅读生成配图时验证"

// scrubSecret 把消息里出现的密钥抹成 ***。太短的密钥（如本地服务的占位值）不抹：抹了反而把普通词语打成码。
func scrubSecret(msg, secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) < 4 {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "***")
}

func (a aiSettingsAdapter) Load(ctx context.Context) (services.AIConfig, error) {
	cfg, err := callAs[services.AIConfig](ctx, a.b, "settings:get")
	if err != nil {
		return services.AIConfig{}, err
	}
	if cfg.Providers == nil {
		cfg.Providers = []services.AIProvider{}
	}
	if cfg.Models == nil {
		cfg.Models = []services.AIModel{}
	}
	if cfg.Routes == nil {
		cfg.Routes = map[string]string{}
	}
	return cfg, nil
}

func (a aiSettingsAdapter) Save(ctx context.Context, cfg services.AIConfig) error {
	// 只写端点、模型与路由三键：旧版单模型字段不由原生界面维护（主进程读配置时按模型列表派生它们）。
	_, err := a.b.call(ctx, "settings:set", map[string]any{
		"aiProviders":        orEmptySlice(cfg.Providers),
		"aiModels":           orEmptySlice(cfg.Models),
		"modelRouteDefaults": orEmptyMap(cfg.Routes),
	})
	return err
}

func orEmptySlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (a aiSettingsAdapter) TestModel(ctx context.Context, m services.AIModel) (services.AITestOutcome, error) {
	switch {
	case m.Capabilities.AudioTranscription:
		reply, err := callAs[struct {
			Success bool    `json:"success"`
			Latency float64 `json:"latency"`
			Error   string  `json:"error"`
		}](ctx, a.b, chMediaTestTranscription, map[string]any{"apiUrl": m.APIURL, "apiKey": m.APIKey, "model": m.Model})
		if err != nil {
			return services.AITestOutcome{}, err
		}
		return services.AITestOutcome{Success: reply.Success, LatencyMs: int64(reply.Latency), Error: scrubSecret(reply.Error, m.APIKey)}, nil
	case m.Capabilities.Embedding:
		return a.testEmbedding(ctx, m), nil
	case m.Capabilities.ImageGeneration:
		return services.AITestOutcome{Error: imageGenTestUnsupported}, nil
	}
	client := &aiflow.Client{Transport: bridgeTransport{b: a.b}}
	res := client.TestConnection(ctx, aiConfigOf(m), "")
	if !res.Success {
		return services.AITestOutcome{LatencyMs: res.LatencyMs, Error: scrubSecret(res.Error, m.APIKey)}, nil
	}
	return services.AITestOutcome{Success: true, LatencyMs: res.LatencyMs}, nil
}

func aiConfigOf(m services.AIModel) aiflow.AIConfig {
	return aiflow.AIConfig{ID: m.ID, Provider: m.Provider, APIProtocol: m.APIProtocol, APIKey: m.APIKey, APIURL: m.APIURL, Model: m.Model}
}

// testEmbedding 发一次真实的 embeddings 请求（"ping"），与渲染层 runModelConnectionTest 对向量模型的做法一致。
func (a aiSettingsAdapter) testEmbedding(ctx context.Context, m services.AIModel) services.AITestOutcome {
	runner := &aiflow.Runner{
		Client: &aiflow.Client{Transport: bridgeTransport{b: a.b}},
		Usage:  bridgeUsageSink{b: a.b, ctx: ctx},
	}
	start := time.Now()
	if _, err := runner.EmbedTexts(ctx, aiConfigOf(m), []string{"ping"}); err != nil {
		return services.AITestOutcome{Error: scrubSecret(err.Error(), m.APIKey)}
	}
	return services.AITestOutcome{Success: true, LatencyMs: time.Since(start).Milliseconds()}
}

func (a aiSettingsAdapter) FetchModels(ctx context.Context, apiURL, apiKey, apiProtocol string) (services.AIFetchModelsResult, error) {
	if strings.TrimSpace(apiURL) == "" || strings.TrimSpace(apiKey) == "" {
		return services.AIFetchModelsResult{}, errors.New("API URL and API key are required")
	}
	client := &aiflow.Client{Transport: bridgeTransport{b: a.b}}
	res := client.FetchModels(ctx, apiURL, apiKey, apiProtocol)
	if ctx.Err() != nil {
		return services.AIFetchModelsResult{}, ctx.Err()
	}
	out := services.AIFetchModelsResult{
		Success: res.Success, Error: scrubSecret(res.Error, apiKey), Reason: res.Reason,
		Models: make([]services.AIRemoteModel, 0, len(res.Models)),
	}
	for _, m := range res.Models {
		out.Models = append(out.Models, services.AIRemoteModel{ID: m.ID, Name: m.Name, OwnedBy: m.OwnedBy})
	}
	return out, nil
}

func (a aiSettingsAdapter) UsageSummary(ctx context.Context, days int) (services.AIUsageSummary, error) {
	sum, err := callAs[services.AIUsageSummary](ctx, a.b, "ai:usageSummary", days)
	if err != nil {
		return services.AIUsageSummary{}, err
	}
	if sum.ByScenario == nil {
		sum.ByScenario = []services.AIUsageRow{}
	}
	if sum.ByModel == nil {
		sum.ByModel = []services.AIUsageRow{}
	}
	return sum, nil
}

func (a aiSettingsAdapter) ClearUsage(ctx context.Context) error {
	_, err := a.b.call(ctx, "ai:usageClear")
	return err
}
