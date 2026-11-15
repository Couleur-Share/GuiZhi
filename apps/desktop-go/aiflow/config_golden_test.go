package aiflow

// 配置归一化、场景模型解析与 runScenarioChat 的 golden 对拍；夹具来自 aiflow-golden-config.test.ts。

import (
	"context"
	"testing"
)

var (
	goldenScenarios = []Scenario{ScenarioSummary, ScenarioTagging, ScenarioQA, ScenarioWiki, ScenarioResearch, ScenarioTranscription, ScenarioOCR}
	goldenRoutes    = []Route{RouteMainText, RouteFastText, RouteVisionText, RouteEmbedding, RouteAudioText, RouteImageGen}
)

// configJSON 把 AIConfig 变成 golden 里的形状：TS 的 id 缺省时不出现。
func configJSON(cfg AIConfig, ok bool) any {
	if !ok {
		return nil
	}
	out := map[string]any{"provider": cfg.Provider, "apiProtocol": cfg.APIProtocol, "apiKey": cfg.APIKey, "apiUrl": cfg.APIURL, "model": cfg.Model}
	if cfg.ID != "" {
		out["id"] = cfg.ID
	}
	return out
}

func modelID(m *ModelConfig) any {
	if m == nil {
		return nil
	}
	return m.ID
}

func TestConfigGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string         `json:"name"`
			Settings map[string]any `json:"settings"`
			Expect   struct {
				Scenarios           map[string]any  `json:"scenarios"`
				Configured          map[string]bool `json:"configured"`
				Routes              map[string]any  `json:"routes"`
				RoutesWithProviders map[string]any  `json:"routesWithProviders"`
				Embedding           any             `json:"embedding"`
				Semantic            bool            `json:"semantic"`
				Normalized          map[string]any  `json:"normalized"`
			} `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "config", &golden)
	if len(golden.Cases) < 20 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			s := NormalizeAISettings(c.Settings)

			scenarios := map[string]any{}
			configured := map[string]bool{}
			for _, sc := range goldenScenarios {
				cfg, ok := s.ResolveScenarioConfig(sc, true, nil)
				scenarios[string(sc)] = configJSON(cfg, ok)
				configured[string(sc)] = ok
			}
			routes := map[string]any{}
			routesWithProviders := map[string]any{}
			for _, r := range goldenRoutes {
				routes[string(r)] = modelID(s.ResolveRouteModel(r, nil))
				routesWithProviders[string(r)] = modelID(s.ResolveRouteModel(r, s.Providers))
			}
			embCfg, embOK := s.ResolveEmbeddingConfig()

			modelIDs, protocols := []string{}, []string{}
			for _, m := range s.Models {
				modelIDs = append(modelIDs, m.ID)
				protocols = append(protocols, m.APIProtocol)
			}
			providerIDs := []string{}
			for _, p := range s.Providers {
				providerIDs = append(providerIDs, p.ID)
			}
			routeDefaults := map[string]string{}
			for k, v := range s.RouteDefaults {
				routeDefaults[string(k)] = v
			}
			scenarioDefaults := map[string]string{}
			for k, v := range s.ScenarioDefaults {
				scenarioDefaults[string(k)] = v
			}

			assertSame(t, "场景配置", c.Expect.Scenarios, scenarios)
			assertSame(t, "场景是否已配置", c.Expect.Configured, configured)
			assertSame(t, "路由模型（不带供应商）", c.Expect.Routes, routes)
			assertSame(t, "路由模型（带供应商）", c.Expect.RoutesWithProviders, routesWithProviders)
			assertSame(t, "embedding 配置", c.Expect.Embedding, configJSON(embCfg, embOK))
			if s.IsSemanticConfigured() != c.Expect.Semantic {
				t.Errorf("isSemanticConfigured：Go=%v TS=%v", s.IsSemanticConfigured(), c.Expect.Semantic)
			}
			assertSame(t, "归一化结果", c.Expect.Normalized, map[string]any{
				"modelIds": modelIDs, "providerIds": providerIDs, "routeDefaults": routeDefaults,
				"scenarioDefaults": scenarioDefaults, "protocols": protocols, "legacyProtocol": s.Legacy.APIProtocol,
			})
		})
	}
}

// staticConfig 是固定返回一份设置的 ConfigSource。
type staticConfig struct{ settings AISettings }

func (s staticConfig) AISettings() AISettings { return s.settings }

func TestInvokeGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string         `json:"name"`
			Settings map[string]any `json:"settings"`
			Scenario string         `json:"scenario"`
			Options  struct {
				Temperature *float64 `json:"temperature"`
				MaxTokens   int      `json:"maxTokens"`
				TimeoutMs   float64  `json:"timeoutMs"`
				Stream      bool     `json:"stream"`
			} `json:"options"`
			Script []scriptStep `json:"script"`
			Abort  *string      `json:"abort"`
			Expect struct {
				Requests []recordedRequest `json:"requests"`
				Usage    []usageRecord     `json:"usage"`
				Deltas   []string          `json:"deltas"`
				Warnings []string          `json:"warnings"`
				Result   map[string]any    `json:"result"`
				Error    map[string]any    `json:"error"`
			} `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "invoke", &golden)
	if len(golden.Cases) < 10 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.Abort != nil && *c.Abort == "before" {
				cancel()
			}
			fake := newFakeTransport(c.Script, cancel)
			warns := &warnRecorder{}
			usage := &usageRecorder{}
			runner := &Runner{
				Client: &Client{Transport: fake, Log: warns},
				Config: staticConfig{NormalizeAISettings(c.Settings)},
				Usage:  usage,
				Log:    warns,
			}

			var deltas []string
			opts := ScenarioChatOptions{Temperature: c.Options.Temperature, MaxTokens: c.Options.MaxTokens, TimeoutMs: c.Options.TimeoutMs}
			if c.Options.Stream {
				opts.OnDelta = func(chunk string) { deltas = append(deltas, chunk) }
			}
			res, err := runner.RunScenarioChat(ctx, Scenario(c.Scenario), []ChatMessage{{Role: "user", Content: "问"}}, opts)

			var result map[string]any
			var errInfo map[string]any
			if err != nil {
				errInfo = map[string]any{"message": err.Error(), "aborted": IsAborted(err)}
				if IsNotConfigured(err) {
					errInfo["notConfigured"] = true
				}
			} else {
				result = map[string]any{"content": res.Content, "model": res.Model}
				if res.FinishReason != "" {
					result["finishReason"] = res.FinishReason
				}
			}

			assertSame(t, "请求", nonNil(c.Expect.Requests), nonNil(fake.Requests))
			assertSame(t, "用量记录", nonNil(c.Expect.Usage), nonNil(usage.Entries))
			assertSame(t, "onDelta 回调", nonNil(c.Expect.Deltas), nonNil(deltas))
			assertSame(t, "警告日志", nonNil(c.Expect.Warnings), nonNil(warns.Messages))
			assertSame(t, "结果", c.Expect.Result, result)
			assertSame(t, "错误", c.Expect.Error, errInfo)
		})
	}
}
