package aiflow

// AI 模型配置与场景路由（对应 renderer/services/ai-defaults.ts 与 stores/settings/settings-ai.ts）。
//
// 渲染层从设置里取 aiModels / aiProviders / modelRouteDefaults / scenarioModelDefaults，经 settings-persistence
// 的归一化后交给 ai-defaults 解析「场景 → 路由 → 模型」。这里原样复刻两段：NormalizeAISettings 吃解码后的
// JSON（形状同渲染层拿到的设置），Resolve* 做解析。数据来源在主包适配器里：config/ai-models.json 的供应商、
// 模型与路由，加上界面偏好里的 scenarioModelDefaults。

import (
	"strings"
)

// Scenario 是 AI 用量场景。
type Scenario string

const (
	ScenarioSummary       Scenario = "summary"
	ScenarioTagging       Scenario = "tagging"
	ScenarioQA            Scenario = "qa"
	ScenarioWiki          Scenario = "wiki"
	ScenarioResearch      Scenario = "research"
	ScenarioTranscription Scenario = "transcription"
	ScenarioOCR           Scenario = "ocr"
)

// Route 是模型路由。
type Route string

const (
	RouteMainText   Route = "mainText"
	RouteFastText   Route = "fastText"
	RouteVisionText Route = "visionText"
	RouteEmbedding  Route = "embedding"
	RouteAudioText  Route = "audioText"
	RouteImageGen   Route = "imageGen"
)

var scenarioRoute = map[Scenario]Route{
	ScenarioSummary:       RouteFastText,
	ScenarioTagging:       RouteFastText,
	ScenarioQA:            RouteMainText,
	ScenarioWiki:          RouteMainText,
	ScenarioResearch:      RouteMainText,
	ScenarioTranscription: RouteAudioText,
	ScenarioOCR:           RouteVisionText,
}

// RouteOf 对应 AI_SCENARIO_MODEL_ROUTE。
func RouteOf(s Scenario) Route { return scenarioRoute[s] }

// Capabilities 对应 AIModelCapabilities（归一化后全是布尔；chat 缺省为 true）。
type Capabilities struct {
	Chat, Vision, Reasoning, ToolUse, WebSearch     bool
	Embedding, Rerank, AudioTranscription, ImageGen bool
}

// ModelConfig 对应 AIModelConfig（归一化后）。
type ModelConfig struct {
	ID, ProviderID, Provider, APIProtocol, APIKey, APIURL, Model string
	IsDefault                                                    bool
	Capabilities                                                 Capabilities
	Enabled                                                      bool
}

// ProviderConfig 对应 AIProviderConfig（归一化后）。
type ProviderConfig struct {
	ID, Name, Provider, APIProtocol, APIKey, APIURL string
	Enabled                                         bool
}

// AIConfig 对应 AIConfig：一次调用所需的连接参数。
type AIConfig struct {
	ID, Provider, APIProtocol, APIKey, APIURL, Model string
}

// LegacyModel 是旧版单模型字段（aiProvider / aiApiKey / …），只兜底纯对话路由。
type LegacyModel struct {
	Provider, APIProtocol, APIKey, APIURL, Model string
}

// AISettings 是解析场景模型所需的全部输入（对应渲染层设置里的 AI 部分，已归一化）。
type AISettings struct {
	Providers        []ProviderConfig
	Models           []ModelConfig
	ScenarioDefaults map[Scenario]string
	RouteDefaults    map[Route]string
	Legacy           LegacyModel
}

// ConfigSource 每次调用时取当前的 AI 设置（用户随时可能在设置页改模型，不能在构造时缓存）。
type ConfigSource interface {
	AISettings() AISettings
}

// ToAIConfig 对应 toAIConfig。
func (m ModelConfig) ToAIConfig() AIConfig {
	return AIConfig{ID: m.ID, Provider: m.Provider, APIProtocol: m.APIProtocol, APIKey: m.APIKey, APIURL: m.APIURL, Model: m.Model}
}

// IsConfiguredModel 对应 isConfiguredModel。
func IsConfiguredModel(m *ModelConfig) bool {
	return m != nil && m.Enabled &&
		jsTrim(m.Provider) != "" && jsTrim(m.APIKey) != "" && jsTrim(m.APIURL) != "" && jsTrim(m.Model) != ""
}

// findMatchingProvider 对应 findMatchingAIProvider。
func findMatchingProvider(providers []ProviderConfig, m ModelConfig) *ProviderConfig {
	if jsTrim(m.ProviderID) != "" {
		for i := range providers {
			if providers[i].ID == m.ProviderID {
				return &providers[i]
			}
		}
		return nil
	}
	for i := range providers {
		p := &providers[i]
		if p.ID == m.Provider ||
			(p.Provider == m.Provider && p.APIProtocol == m.APIProtocol && p.APIURL == m.APIURL && p.APIKey == m.APIKey) {
			return p
		}
	}
	return nil
}

// isModelActive 对应 isModelActive：模型自身启用，且所属供应商没被停用。
func isModelActive(m ModelConfig, providers []ProviderConfig) bool {
	if !m.Enabled {
		return false
	}
	if len(providers) > 0 {
		if p := findMatchingProvider(providers, m); p != nil && !p.Enabled {
			return false
		}
	}
	return true
}

// hasModelCapability 对应 hasModelCapability：vision 等依附于 chat 能力。
func hasModelCapability(m ModelConfig, capability string) bool {
	c := m.Capabilities
	switch capability {
	case "chat":
		return c.Chat
	case "embedding":
		return c.Embedding
	case "rerank":
		return c.Rerank
	case "audioTranscription":
		return c.AudioTranscription
	case "imageGeneration":
		return c.ImageGen
	case "vision":
		return c.Chat && c.Vision
	case "reasoning":
		return c.Chat && c.Reasoning
	case "toolUse":
		return c.Chat && c.ToolUse
	case "webSearch":
		return c.Chat && c.WebSearch
	}
	return false
}

func modelsByCapability(models []ModelConfig, capability string, providers []ProviderConfig) []ModelConfig {
	var out []ModelConfig
	for _, m := range models {
		if isModelActive(m, providers) && hasModelCapability(m, capability) {
			out = append(out, m)
		}
	}
	return out
}

// routeCandidates 对应 getRouteCandidateModels：
// embedding / visionText / imageGen 严格按能力过滤；audioText 转写模型优先、对话模型兜底；其余为对话模型。
func routeCandidates(models []ModelConfig, route Route, providers []ProviderConfig) []ModelConfig {
	switch route {
	case RouteEmbedding:
		return modelsByCapability(models, "embedding", providers)
	case RouteVisionText:
		return modelsByCapability(models, "vision", providers)
	case RouteImageGen:
		return modelsByCapability(models, "imageGeneration", providers)
	case RouteAudioText:
		return append(modelsByCapability(models, "audioTranscription", providers), modelsByCapability(models, "chat", providers)...)
	}
	return modelsByCapability(models, "chat", providers)
}

// pickRouteModel 对应 pickRouteModel：显式指定的优先；audioText / imageGen 只认显式设置，不回退。
func pickRouteModel(candidates []ModelConfig, route Route, explicitID string) *ModelConfig {
	if explicitID != "" {
		for i := range candidates {
			if candidates[i].ID == explicitID {
				return &candidates[i]
			}
		}
	}
	if route == RouteAudioText || route == RouteImageGen {
		return nil
	}
	for i := range candidates {
		if candidates[i].IsDefault {
			return &candidates[i]
		}
	}
	if len(candidates) > 0 {
		return &candidates[0]
	}
	return nil
}

// ResolveRouteModel 对应 resolveRouteModel。
func (s AISettings) ResolveRouteModel(route Route, providers []ProviderConfig) *ModelConfig {
	return pickRouteModel(routeCandidates(s.Models, route, providers), route, s.RouteDefaults[route])
}

// ResolveScenarioModel 对应 resolveScenarioModel：路由默认优先，其次场景默认。
func (s AISettings) ResolveScenarioModel(scenario Scenario, providers []ProviderConfig) *ModelConfig {
	route := RouteOf(scenario)
	explicit, ok := s.RouteDefaults[route]
	if !ok {
		explicit = s.ScenarioDefaults[scenario]
	}
	return pickRouteModel(routeCandidates(s.Models, route, providers), route, explicit)
}

// ResolveScenarioConfig 对应 resolveScenarioAIConfig；providers 为 nil 时不考虑供应商的停用状态
// （ai-invoke.ts 的 resolveConfig 就不传 aiProviders，embeddings.ts 才传）。
func (s AISettings) ResolveScenarioConfig(scenario Scenario, allowLegacy bool, providers []ProviderConfig) (AIConfig, bool) {
	if m := s.ResolveScenarioModel(scenario, providers); IsConfiguredModel(m) {
		return m.ToAIConfig(), true
	}
	// 旧版单模型字段只兜底纯对话路由（问答、摘要类场景）
	route := RouteOf(scenario)
	l := s.Legacy
	if allowLegacy && (route == RouteMainText || route == RouteFastText) &&
		jsTrim(l.Provider) != "" && jsTrim(l.APIKey) != "" && jsTrim(l.APIURL) != "" && jsTrim(l.Model) != "" {
		return AIConfig{Provider: l.Provider, APIProtocol: l.APIProtocol, APIKey: l.APIKey, APIURL: l.APIURL, Model: l.Model}, true
	}
	return AIConfig{}, false
}

// ResolveEmbeddingConfig 对应 resolveEmbeddingConfig：embedding 路由对应的模型，未配置返回 false（语义检索静默降级）。
func (s AISettings) ResolveEmbeddingConfig() (AIConfig, bool) {
	m := s.ResolveRouteModel(RouteEmbedding, s.Providers)
	if !IsConfiguredModel(m) {
		return AIConfig{}, false
	}
	return m.ToAIConfig(), true
}

// IsSemanticConfigured 对应 isSemanticConfigured。
func (s AISettings) IsSemanticConfigured() bool {
	_, ok := s.ResolveEmbeddingConfig()
	return ok
}

// ---- 归一化（settings-ai.ts） ----

// normalizeAIProtocol 对应 normalizeAIProtocol（写配置时用，provider 用「精确相等」判断，且不识别 claude）。
func normalizeAIProtocol(value any, provider, apiURL string) string {
	if s, ok := value.(string); ok && (s == ProtocolOpenAI || s == ProtocolGemini || s == ProtocolAnthropic) {
		return s
	}
	p := jsLower(jsTrim(provider))
	u := jsLower(jsTrim(apiURL))
	if p == "anthropic" || strings.Contains(u, "api.anthropic.com") {
		return ProtocolAnthropic
	}
	if p == "google" || p == "gemini" || strings.Contains(u, "generativelanguage.googleapis.com") {
		return ProtocolGemini
	}
	return ProtocolOpenAI
}

func rawString(m map[string]any, key string) (string, bool) {
	s, ok := m[key].(string)
	return s, ok
}

func rawNonBlank(m map[string]any, key string) (string, bool) {
	s, ok := rawString(m, key)
	return s, ok && jsTrim(s) != ""
}

// normalizeCapabilities 对应 normalizeAIModelCapabilities：chat 缺省开启，vision 依附 chat。
func normalizeCapabilities(value any) Capabilities {
	m, ok := value.(map[string]any)
	if !ok {
		return Capabilities{Chat: true}
	}
	chat := m["chat"] != false
	return Capabilities{
		Chat:               chat,
		Vision:             chat && m["vision"] == true,
		Reasoning:          m["reasoning"] == true,
		ToolUse:            m["toolUse"] == true,
		WebSearch:          m["webSearch"] == true,
		Embedding:          m["embedding"] == true,
		Rerank:             m["rerank"] == true,
		AudioTranscription: m["audioTranscription"] == true,
		ImageGen:           m["imageGeneration"] == true,
	}
}

func normalizeModels(value any) []ModelConfig {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []ModelConfig
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok1 := rawNonBlank(m, "id")
		provider, ok2 := rawNonBlank(m, "provider")
		apiURL, ok3 := rawNonBlank(m, "apiUrl")
		model, ok4 := rawNonBlank(m, "model")
		if !(ok1 && ok2 && ok3 && ok4) {
			continue
		}
		provider, apiURL = jsTrim(provider), jsTrim(apiURL)
		providerID, _ := rawNonBlank(m, "providerId")
		apiKey, _ := rawString(m, "apiKey")
		out = append(out, ModelConfig{
			ID:           jsTrim(id),
			ProviderID:   jsTrim(providerID),
			Provider:     provider,
			APIProtocol:  normalizeAIProtocol(m["apiProtocol"], provider, apiURL),
			APIKey:       apiKey,
			APIURL:       apiURL,
			Model:        jsTrim(model),
			IsDefault:    jsTruthy(m["isDefault"]),
			Capabilities: normalizeCapabilities(m["capabilities"]),
			Enabled:      m["enabled"] != false,
		})
	}
	return out
}

func normalizeProviders(value any) []ProviderConfig {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []ProviderConfig
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok1 := rawNonBlank(m, "id")
		provider, ok2 := rawNonBlank(m, "provider")
		apiURL, ok3 := rawNonBlank(m, "apiUrl")
		if !(ok1 && ok2 && ok3) {
			continue
		}
		provider, apiURL = jsTrim(provider), jsTrim(apiURL)
		name, _ := rawString(m, "name")
		apiKey, _ := rawString(m, "apiKey")
		out = append(out, ProviderConfig{
			ID:          jsTrim(id),
			Name:        jsTrim(name),
			Provider:    provider,
			APIProtocol: normalizeAIProtocol(m["apiProtocol"], provider, apiURL),
			APIKey:      apiKey,
			APIURL:      apiURL,
			Enabled:     m["enabled"] != false,
		})
	}
	return out
}

var (
	allScenarios = []Scenario{ScenarioSummary, ScenarioTagging, ScenarioQA, ScenarioWiki, ScenarioResearch, ScenarioTranscription, ScenarioOCR}
	allRoutes    = []Route{RouteMainText, RouteFastText, RouteVisionText, RouteEmbedding, RouteAudioText, RouteImageGen}
)

func normalizeScenarioDefaults(value any) map[Scenario]string {
	out := map[Scenario]string{}
	m, ok := value.(map[string]any)
	if !ok {
		return out
	}
	for _, sc := range allScenarios {
		if id, ok := m[string(sc)].(string); ok && jsTrim(id) != "" {
			out[sc] = jsTrim(id)
		}
	}
	return out
}

func normalizeRouteDefaults(value any) map[Route]string {
	out := map[Route]string{}
	m, ok := value.(map[string]any)
	if !ok {
		return out
	}
	for _, r := range allRoutes {
		if id, ok := m[string(r)].(string); ok && jsTrim(id) != "" {
			out[r] = jsTrim(id)
		}
	}
	return out
}

// deriveRouteDefaults 对应 deriveModelRouteDefaultsFromScenarios：旧版只存场景默认时据此补出路由默认。
func deriveRouteDefaults(s map[Scenario]string) map[Route]string {
	next := map[Route]string{}
	if v, ok := s[ScenarioQA]; ok {
		next[RouteMainText] = v
	} else if v, ok := s[ScenarioWiki]; ok {
		next[RouteMainText] = v
	}
	if v, ok := s[ScenarioOCR]; ok {
		next[RouteVisionText] = v
	}
	if v, ok := s[ScenarioSummary]; ok {
		next[RouteFastText] = v
	} else if v, ok := s[ScenarioTagging]; ok {
		next[RouteFastText] = v
	}
	return next
}

// NormalizeAISettings 对应 settings-persistence 的 normalizeSharedSettingsState 里 AI 的部分。
// raw 是解码后的设置对象，认这些键：aiProviders、aiModels、modelRouteDefaults、scenarioModelDefaults 与
// 旧版单模型字段 aiProvider / aiApiProtocol / aiApiKey / aiApiUrl / aiModel。
func NormalizeAISettings(raw map[string]any) AISettings {
	str := func(key string) string { s, _ := raw[key].(string); return s }
	legacyProvider := str("aiProvider")
	legacyURL := str("aiApiUrl")
	s := AISettings{
		Providers:        normalizeProviders(raw["aiProviders"]),
		Models:           normalizeModels(raw["aiModels"]),
		ScenarioDefaults: normalizeScenarioDefaults(raw["scenarioModelDefaults"]),
		RouteDefaults:    normalizeRouteDefaults(raw["modelRouteDefaults"]),
		Legacy: LegacyModel{
			Provider:    legacyProvider,
			APIProtocol: normalizeAIProtocol(raw["aiApiProtocol"], legacyProvider, legacyURL),
			APIKey:      str("aiApiKey"),
			APIURL:      legacyURL,
			Model:       str("aiModel"),
		},
	}
	if len(s.RouteDefaults) == 0 {
		s.RouteDefaults = deriveRouteDefaults(s.ScenarioDefaults)
	}
	return s
}
