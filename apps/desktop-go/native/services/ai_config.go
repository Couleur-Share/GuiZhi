package services

import (
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AI 配置的纯函数：选择器（路由解析、能力判定、端点分组）与不可变更新（增删改供应商 / 模型、路由、默认模型）。
// 移植自渲染层 stores/settings/settings-ai{,-actions}.ts、services/ai-defaults.ts 与 ai-workbench/helpers.ts。
//
// 所有更新函数返回新的 AIConfig，不改接收者：界面持有的快照可以安全地交给后台保存。
// 与渲染层的有意差异：不回写旧版单模型字段（aiProvider 等）与 scenarioModelDefaults——
// Go 壳的 settings:set 对它们既不读取也不依赖（路由只看 modelRouteDefaults），原生界面也不展示它们。

// Clone 返回深拷贝（切片与路由表都复制）。
func (c AIConfig) Clone() AIConfig {
	out := c
	out.Providers = append([]AIProvider{}, c.Providers...)
	out.Models = append([]AIModel{}, c.Models...)
	out.Routes = make(map[string]string, len(c.Routes))
	for k, v := range c.Routes {
		out.Routes[k] = v
	}
	return out
}

// NewAIID 生成 `<prefix>_<毫秒时间戳>_<9 位随机>` 形式的 ID（供应商用 provider，模型用 model）。
func NewAIID(prefix string) string {
	return prefix + "_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + "_" + randBase36(9)
}

func randBase36(n int) string {
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(strconv.FormatUint(rand.Uint64(), 36))
	}
	return b.String()[:n]
}

// ── 选择器 ──

// FindProvider 找模型所属的供应商（findMatchingAIProvider）：有 ProviderID 就只认它，否则按 ID 等于供应商标识或四项连接字段全等。
func FindProvider(providers []AIProvider, m AIModel) (AIProvider, bool) {
	if strings.TrimSpace(m.ProviderID) != "" {
		for _, p := range providers {
			if p.ID == m.ProviderID {
				return p, true
			}
		}
		return AIProvider{}, false
	}
	for _, p := range providers {
		if p.ID == m.Provider ||
			(p.Provider == m.Provider && p.APIProtocol == m.APIProtocol && p.APIURL == m.APIURL && p.APIKey == m.APIKey) {
			return p, true
		}
	}
	return AIProvider{}, false
}

// IsModelActive 报告模型是否可用：自身启用，且所属供应商没被停用。
func IsModelActive(m AIModel, providers []AIProvider) bool {
	if !m.Enabled {
		return false
	}
	if len(providers) > 0 {
		if p, ok := FindProvider(providers, m); ok && !p.Enabled {
			return false
		}
	}
	return true
}

// IsConfiguredModel 报告模型是否配置完整：启用，且供应商、密钥、地址、模型名都非空。
func IsConfiguredModel(m AIModel) bool {
	return m.Enabled && strings.TrimSpace(m.Provider) != "" && strings.TrimSpace(m.APIKey) != "" &&
		strings.TrimSpace(m.APIURL) != "" && strings.TrimSpace(m.Model) != ""
}

// AICapability 是 ModelHasCapability 的能力名。
type AICapability string

// 能力名。
const (
	CapChat               AICapability = "chat"
	CapVision             AICapability = "vision"
	CapEmbedding          AICapability = "embedding"
	CapAudioTranscription AICapability = "audioTranscription"
	CapImageGeneration    AICapability = "imageGeneration"
)

// ModelHasCapability 报告模型是否具备某能力（视觉依附于对话）。
func ModelHasCapability(m AIModel, c AICapability) bool {
	switch c {
	case CapChat:
		return m.Capabilities.Chat
	case CapEmbedding:
		return m.Capabilities.Embedding
	case CapAudioTranscription:
		return m.Capabilities.AudioTranscription
	case CapImageGeneration:
		return m.Capabilities.ImageGeneration
	case CapVision:
		return m.Capabilities.Chat && m.Capabilities.Vision
	}
	return false
}

func (c AIConfig) activeWith(cap AICapability) []AIModel {
	var out []AIModel
	for _, m := range c.Models {
		if IsModelActive(m, c.Providers) && ModelHasCapability(m, cap) {
			out = append(out, m)
		}
	}
	return out
}

// RouteCandidates 是一条路由的候选模型：向量、视觉、文生图严格按能力过滤；
// 转写路由转写模型在前、对话模型兜底（中转站的模型名无法可靠判定）；其余路由是对话模型。
func (c AIConfig) RouteCandidates(route string) []AIModel {
	switch route {
	case AIRouteEmbedding:
		return c.activeWith(CapEmbedding)
	case AIRouteVisionText:
		return c.activeWith(CapVision)
	case AIRouteImageGen:
		return c.activeWith(CapImageGeneration)
	case AIRouteAudioText:
		return append(c.activeWith(CapAudioTranscription), c.activeWith(CapChat)...)
	}
	return c.activeWith(CapChat)
}

// ResolveRoute 求一条路由当前生效的模型：显式设置且仍是候选就用它；
// 转写与文生图不回退（主进程只认显式设置，界面不能显示一个「已配置」而实际调用报未配置）；其余取默认模型或第一个候选。
func (c AIConfig) ResolveRoute(route string) (AIModel, bool) {
	candidates := c.RouteCandidates(route)
	if id := c.Routes[route]; id != "" {
		for _, m := range candidates {
			if m.ID == id {
				return m, true
			}
		}
	}
	if route == AIRouteAudioText || route == AIRouteImageGen {
		return AIModel{}, false
	}
	for _, m := range candidates {
		if m.IsDefault {
			return m, true
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true
	}
	return AIModel{}, false
}

// ProviderLabeler 把供应商 ID 翻成显示名（界面按语言提供；DefaultProviderLabel 是不翻译的版本）。
type ProviderLabeler func(providerID string) string

// DefaultProviderLabel 取预设的品牌名；未知供应商显示 ID 本身，ID 为空显示 Unknown。
func DefaultProviderLabel(providerID string) string {
	if p, ok := AIProviderPresetByID(providerID); ok {
		return p.Fallback
	}
	if providerID == "" {
		return "Unknown"
	}
	return providerID
}

// EndpointDisplayName 是端点的显示名：自定义名称，否则供应商名。
func EndpointDisplayName(name, provider string, label ProviderLabeler) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return label(provider)
}

// FormatModelWithProvider 把模型名与供应商名拼成「模型 (供应商)」；供应商为空、未知，或模型名已含供应商名时只给模型名。
func FormatModelWithProvider(modelName, providerName string) string {
	model, provider := strings.TrimSpace(modelName), strings.TrimSpace(providerName)
	if provider == "" || strings.EqualFold(provider, "unknown") || strings.Contains(strings.ToLower(model), strings.ToLower(provider)) {
		return model
	}
	return model + " (" + provider + ")"
}

// ModelProviderName 是模型所属供应商的显示名：匹配到供应商记录用它的显示名，否则按模型自带的供应商标识。
func ModelProviderName(m AIModel, providers []AIProvider, label ProviderLabeler) string {
	if p, ok := FindProvider(providers, m); ok {
		return EndpointDisplayName(p.Name, p.Provider, label)
	}
	return label(m.Provider)
}

// ModelBaseName 是模型的显示名（不带供应商）：别名，否则模型名。
func ModelBaseName(m AIModel) string {
	if n := strings.TrimSpace(m.Name); n != "" {
		return n
	}
	return m.Model
}

// ModelDisplayName 是模型的完整显示名；providers 为空时不拼供应商。
func ModelDisplayName(m AIModel, providers []AIProvider, label ProviderLabeler) string {
	base := ModelBaseName(m)
	if len(providers) == 0 {
		return base
	}
	return FormatModelWithProvider(base, ModelProviderName(m, providers, label))
}

// ── 端点分组 ──

// AIEndpointGroup 是界面里一张端点卡：一个供应商记录（或没有记录时按模型合成的一组）加它名下的模型。
type AIEndpointGroup struct {
	Key string
	// ProviderConfigID 为空表示合成分组：没有对应的供应商记录。
	ProviderConfigID string
	Name             string
	Provider         string
	APIProtocol      string
	APIKey           string
	APIURL           string
	Enabled          bool
	Models           []AIModel
}

// DisplayName 是端点卡的标题。
func (g AIEndpointGroup) DisplayName(label ProviderLabeler) string {
	return EndpointDisplayName(g.Name, g.Provider, label)
}

// EffectiveAPIKey 是端点的密钥：自己的，没有就取第一个模型的（合成分组只有模型上带密钥）。
func (g AIEndpointGroup) EffectiveAPIKey() string {
	if g.APIKey != "" {
		return g.APIKey
	}
	if len(g.Models) > 0 {
		return g.Models[0].APIKey
	}
	return ""
}

// endpointKey 是端点分组键：有供应商 ID（或记录自己的 ID）用 `provider:<id>`，否则按连接三元组。
func endpointKey(id, providerID, provider, protocol, apiURL string) string {
	if v := strings.TrimSpace(providerID); v != "" {
		return "provider:" + v
	}
	if v := strings.TrimSpace(id); v != "" {
		return "provider:" + v
	}
	return provider + "::" + protocol + "::" + apiURL
}

// EndpointGroups 把供应商与模型归成端点分组，按供应商标识排序（大小写不敏感，稳定）。
// 没有供应商记录的模型各自合成一个分组（键取模型 ID）。
func (c AIConfig) EndpointGroups() []AIEndpointGroup {
	var groups []*AIEndpointGroup
	byKey := map[string]*AIEndpointGroup{}
	for _, p := range c.Providers {
		key := endpointKey(p.ID, "", p.Provider, p.APIProtocol, p.APIURL)
		g := &AIEndpointGroup{Key: key, ProviderConfigID: p.ID, Name: p.Name, Provider: p.Provider,
			APIProtocol: p.APIProtocol, APIKey: p.APIKey, APIURL: p.APIURL, Enabled: p.Enabled}
		if _, dup := byKey[key]; !dup {
			groups = append(groups, g)
		}
		byKey[key] = g
	}
	for _, m := range c.Models {
		var key string
		if owner := findGroupForModel(groups, m); owner != nil {
			key = owner.Key
		} else {
			key = endpointKey(m.ID, m.ProviderID, m.Provider, m.APIProtocol, m.APIURL)
		}
		g, ok := byKey[key]
		if !ok {
			g = &AIEndpointGroup{Key: key, ProviderConfigID: m.ProviderID, Provider: m.Provider, APIProtocol: m.APIProtocol,
				APIKey: m.APIKey, APIURL: m.APIURL, Enabled: m.Enabled}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.Models = append(g.Models, m)
	}
	out := make([]AIEndpointGroup, len(groups))
	for i, g := range groups {
		out[i] = *g
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Provider) < strings.ToLower(out[j].Provider)
	})
	return out
}

func findGroupForModel(groups []*AIEndpointGroup, m AIModel) *AIEndpointGroup {
	if strings.TrimSpace(m.ProviderID) != "" {
		for _, g := range groups {
			if g.ProviderConfigID == m.ProviderID {
				return g
			}
		}
		return nil
	}
	for _, g := range groups {
		if g.Provider == m.Provider && g.APIProtocol == m.APIProtocol && g.APIURL == m.APIURL && g.APIKey == m.APIKey {
			return g
		}
	}
	return nil
}

// ── 规范化 ──

// NormalizeCapabilities 保证能力一致：视觉只在对话模型上成立。
func NormalizeCapabilities(c AICapabilities) AICapabilities {
	c.Vision = c.Chat && c.Vision
	return c
}

// normalizeProvider 规范化端点字段：名称、标识、密钥、地址去空白，协议按需推断。
func normalizeProvider(p AIProvider) AIProvider {
	p.Name = strings.TrimSpace(p.Name)
	p.Provider = strings.TrimSpace(p.Provider)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.APIURL = strings.TrimSpace(p.APIURL)
	p.APIProtocol = NormalizeAIProtocol(p.APIProtocol, p.Provider, p.APIURL)
	return p
}

// applyProvider 把端点的连接字段镜像到模型上。
func applyProvider(m AIModel, p AIProvider) AIModel {
	m.ProviderID, m.Provider, m.APIProtocol, m.APIKey, m.APIURL = p.ID, p.Provider, p.APIProtocol, p.APIKey, p.APIURL
	return m
}

// ── 不可变更新 ──

// AddProvider 追加一个供应商，返回新配置与新供应商的 ID。
func (c AIConfig) AddProvider(p AIProvider) (AIConfig, string) {
	next := c.Clone()
	p = normalizeProvider(p)
	p.ID = NewAIID("provider")
	next.Providers = append(next.Providers, p)
	return next, p.ID
}

// UpdateProvider 改一个供应商，并把连接字段同步到它名下的模型。patch 收到当前值的副本。找不到 ID 时原样返回。
func (c AIConfig) UpdateProvider(id string, patch func(*AIProvider)) AIConfig {
	next := c.Clone()
	idx := -1
	for i, p := range next.Providers {
		if p.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return next
	}
	updated := next.Providers[idx]
	patch(&updated)
	updated = normalizeProvider(updated)
	updated.ID = id
	next.Providers[idx] = updated
	for i, m := range next.Models {
		linked := m.ProviderID == id
		if !linked && strings.TrimSpace(m.ProviderID) == "" {
			_, linked = FindProvider([]AIProvider{updated}, m)
		}
		if linked {
			next.Models[i] = applyProvider(m, updated)
		}
	}
	return next
}

// DeleteProvider 删一个供应商连同它名下的模型（modelIDs）。
// 只删记录会让没有 providerId 的模型各自合成一个分组，行不但不消失还会变多，所以必须一起删。
// 合成分组没有供应商记录（providerID 为空），此时只删模型。被删模型若是默认模型，第一个剩余模型接任；路由里指向它们的条目一并清掉。
func (c AIConfig) DeleteProvider(providerID string, modelIDs []string) AIConfig {
	next := c.Clone()
	removed := map[string]bool{}
	for _, id := range modelIDs {
		removed[id] = true
	}
	next.Models = next.Models[:0:0]
	droppedDefault := false
	for _, m := range c.Models {
		if removed[m.ID] {
			droppedDefault = droppedDefault || m.IsDefault
			continue
		}
		next.Models = append(next.Models, m)
	}
	if droppedDefault && len(next.Models) > 0 {
		next.Models[0].IsDefault = true
	}
	next.Providers = next.Providers[:0:0]
	for _, p := range c.Providers {
		if providerID == "" || p.ID != providerID {
			next.Providers = append(next.Providers, p)
		}
	}
	dropRoutes(next.Routes, removed)
	return next
}

func dropRoutes(routes map[string]string, removed map[string]bool) {
	for route, id := range routes {
		if removed[id] {
			delete(routes, route)
		}
	}
}

// AddModel 追加一个模型，返回新配置与新模型的 ID。模型挂到匹配的供应商上并镜像其连接字段；第一个模型成为默认模型。
func (c AIConfig) AddModel(m AIModel) (AIConfig, string) {
	next := c.Clone()
	m.ID = NewAIID("model")
	if p, ok := FindProvider(next.Providers, m); ok {
		m = applyProvider(m, p)
	}
	m.Capabilities = NormalizeCapabilities(m.Capabilities)
	m.IsDefault = len(next.Models) == 0
	next.Models = append(next.Models, m)
	return next, m.ID
}

// UpdateModel 改一个模型：patch 收到当前值的副本；之后重新对齐所属供应商的连接字段并规范化能力。找不到 ID 时原样返回。
func (c AIConfig) UpdateModel(id string, patch func(*AIModel)) AIConfig {
	next := c.Clone()
	for i, m := range next.Models {
		if m.ID != id {
			continue
		}
		patch(&m)
		m.ID = id
		if p, ok := FindProvider(next.Providers, m); ok {
			m = applyProvider(m, p)
		}
		m.Capabilities = NormalizeCapabilities(m.Capabilities)
		next.Models[i] = m
		break
	}
	return next
}

// DeleteModel 删一个模型；它若是默认模型，第一个剩余模型接任；路由里指向它的条目一并清掉。
func (c AIConfig) DeleteModel(id string) AIConfig {
	next := c.Clone()
	next.Models = next.Models[:0:0]
	droppedDefault := false
	for _, m := range c.Models {
		if m.ID == id {
			droppedDefault = m.IsDefault
			continue
		}
		next.Models = append(next.Models, m)
	}
	if droppedDefault && len(next.Models) > 0 {
		next.Models[0].IsDefault = true
	}
	dropRoutes(next.Routes, map[string]bool{id: true})
	return next
}

// SetDefaultModel 把某个模型设为默认（其余取消）；ID 不存在时原样返回。
func (c AIConfig) SetDefaultModel(id string) AIConfig {
	next := c.Clone()
	found := false
	for _, m := range next.Models {
		found = found || m.ID == id
	}
	if !found {
		return next
	}
	for i := range next.Models {
		next.Models[i].IsDefault = next.Models[i].ID == id
	}
	return next
}

// SetRoute 设置一条路由的模型；modelID 为空表示清除（回到自动选择）。
func (c AIConfig) SetRoute(route, modelID string) AIConfig {
	next := c.Clone()
	if modelID == "" {
		delete(next.Routes, route)
	} else {
		next.Routes[route] = modelID
	}
	return next
}

// SyntheticProviderFor 为合成分组生成一个正式的供应商记录（删除其最后一个模型前先留住端点，否则端点会随之消失）。
func SyntheticProviderFor(g AIEndpointGroup, name string) AIProvider {
	return AIProvider{Name: name, Provider: g.Provider, APIProtocol: g.APIProtocol, APIKey: g.EffectiveAPIKey(), APIURL: g.APIURL, Enabled: true}
}
