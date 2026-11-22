package services

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AI 快速配置向导的纯逻辑：目标 → 角色、按模型名推荐、把各角色的选择合成一份整体配置。
// 移植自渲染层 ai-quick-setup/quick-setup-models.ts 与 stores/settings/settings-ai-actions.ts 的 applyAiQuickSetup。

// QuickSetupGoal 是向导第一步勾选的目标。
type QuickSetupGoal string

// 向导目标。
const (
	GoalBasic    QuickSetupGoal = "basic"
	GoalSemantic QuickSetupGoal = "semantic"
	GoalVision   QuickSetupGoal = "vision"
	GoalAudio    QuickSetupGoal = "audio"
	GoalImageGen QuickSetupGoal = "imageGen"
)

// QuickSetupGoals 是全部目标，顺序即向导里卡片的顺序。
var QuickSetupGoals = []QuickSetupGoal{GoalBasic, GoalSemantic, GoalVision, GoalAudio, GoalImageGen}

// QuickSetupRole 是向导里需要选一个模型的角色。
type QuickSetupRole string

// 向导角色。
const (
	RoleText      QuickSetupRole = "text"
	RoleEmbedding QuickSetupRole = "embedding"
	RoleVision    QuickSetupRole = "vision"
	RoleAudio     QuickSetupRole = "audio"
	RoleImageGen  QuickSetupRole = "imageGen"
)

// Capabilities 是角色对应模型的能力。
func (r QuickSetupRole) Capabilities() AICapabilities {
	switch r {
	case RoleEmbedding:
		return AICapabilities{Embedding: true}
	case RoleVision:
		return AICapabilities{Chat: true, Vision: true}
	case RoleAudio:
		return AICapabilities{AudioTranscription: true}
	case RoleImageGen:
		return AICapabilities{ImageGeneration: true}
	}
	return AICapabilities{Chat: true}
}

// Routes 是角色占用的路由。
func (r QuickSetupRole) Routes() []string {
	switch r {
	case RoleEmbedding:
		return []string{AIRouteEmbedding}
	case RoleVision:
		return []string{AIRouteVisionText}
	case RoleAudio:
		return []string{AIRouteAudioText}
	case RoleImageGen:
		return []string{AIRouteImageGen}
	}
	return []string{AIRouteMainText, AIRouteFastText}
}

// RolesForGoals 把勾选的目标换成需要选模型的角色（顺序固定）。
func RolesForGoals(goals map[QuickSetupGoal]bool) []QuickSetupRole {
	var roles []QuickSetupRole
	pairs := []struct {
		goal QuickSetupGoal
		role QuickSetupRole
	}{{GoalBasic, RoleText}, {GoalSemantic, RoleEmbedding}, {GoalVision, RoleVision}, {GoalAudio, RoleAudio}, {GoalImageGen, RoleImageGen}}
	for _, p := range pairs {
		if goals[p.goal] {
			roles = append(roles, p.role)
		}
	}
	return roles
}

var roleHints = map[QuickSetupRole][]*regexp.Regexp{
	RoleText:      compileHints(`gpt-4`, `claude`, `gemini`, `qwen`, `deepseek`, `llama`),
	RoleEmbedding: compileHints(`embed`, `bge`, `e5(?:-|$)`, `gte`),
	RoleVision:    compileHints(`vision`, `\bvl\b`, `gpt-4o`, `gemini`, `qwen.*vl`),
	RoleAudio:     compileHints(`whisper`, `transcri`, `speech.*text`),
	RoleImageGen:  compileHints(`gpt-image`, `dall-e`, `imagen`, `image.*gener`),
}

func compileHints(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(`(?i)` + p)
	}
	return out
}

// RecommendModel 在模型列表里挑角色的推荐项：第一个命中提示词的；都不命中取第一个；列表空返回空串。
func RecommendModel(modelIDs []string, role QuickSetupRole) string {
	for _, id := range modelIDs {
		for _, re := range roleHints[role] {
			if re.MatchString(id) {
				return id
			}
		}
	}
	if len(modelIDs) > 0 {
		return modelIDs[0]
	}
	return ""
}

// QuickSetupProvider 是向导里填的服务。
type QuickSetupProvider struct {
	Name        string
	Provider    string
	APIProtocol string
	APIURL      string
	APIKey      string
}

// String 给日志用：密钥脱敏。
func (p QuickSetupProvider) String() string {
	return "{name=" + p.Name + " provider=" + p.Provider + " protocol=" + p.APIProtocol + " url=" + p.APIURL + " key=" + maskKey(p.APIKey) + "}"
}

// GoString 让 %#v 同样脱敏。
func (p QuickSetupProvider) GoString() string { return "services.QuickSetupProvider" + p.String() }

// QuickSetupModel 是向导里选定的一个模型。
type QuickSetupModel struct {
	Name         string
	Model        string
	Capabilities AICapabilities
	// Verified 为真表示连通测试通过（落盘为 lastVerifiedAt）。
	Verified bool
}

// QuickSetupInput 是向导交出的整套配置：一个服务、若干模型、路由到模型下标的映射。
type QuickSetupInput struct {
	Provider QuickSetupProvider
	Models   []QuickSetupModel
	// Routes 把路由映射到 Models 的下标。
	Routes map[string]int
}

// BuildQuickSetupInput 把各角色的选择合成输入：同一个模型名被多个角色选中时合并成一个模型（能力取并集），
// 已验证只在所有角色都验证过时成立。空选择的角色跳过。
func BuildQuickSetupInput(provider QuickSetupProvider, roles []QuickSetupRole, selections map[QuickSetupRole]string, verified map[QuickSetupRole]bool) QuickSetupInput {
	in := QuickSetupInput{Provider: provider, Routes: map[string]int{}}
	indexes := map[string]int{}
	for _, role := range roles {
		model := strings.TrimSpace(selections[role])
		if model == "" {
			continue
		}
		idx, seen := indexes[model]
		roleCaps := role.Capabilities()
		if !seen {
			idx = len(in.Models)
			indexes[model] = idx
			in.Models = append(in.Models, QuickSetupModel{Name: model, Model: model, Capabilities: roleCaps, Verified: verified[role]})
		} else {
			m := &in.Models[idx]
			c := m.Capabilities
			merged := AICapabilities{
				Chat:               c.Chat || roleCaps.Chat,
				Vision:             c.Vision || roleCaps.Vision,
				Embedding:          c.Embedding || roleCaps.Embedding,
				AudioTranscription: c.AudioTranscription || roleCaps.AudioTranscription,
				ImageGeneration:    c.ImageGeneration || roleCaps.ImageGeneration,
				Reasoning:          c.Reasoning, ToolUse: c.ToolUse, WebSearch: c.WebSearch, Rerank: c.Rerank,
			}
			m.Capabilities = merged
			m.Verified = m.Verified && verified[role]
		}
		for _, route := range role.Routes() {
			in.Routes[route] = idx
		}
	}
	return in
}

// ApplyQuickSetup 把向导结果并入配置：新增一个供应商与它的模型（第一个模型成为默认，原有模型取消默认），
// 路由按下标指向新模型，已有的其他路由保持。now 用于生成 lastVerifiedAt 与模型 ID。
func (c AIConfig) ApplyQuickSetup(in QuickSetupInput, now time.Time) AIConfig {
	next := c.Clone()
	provider := normalizeProvider(AIProvider{
		Name: in.Provider.Name, Provider: in.Provider.Provider, APIProtocol: in.Provider.APIProtocol,
		APIKey: in.Provider.APIKey, APIURL: in.Provider.APIURL, Enabled: true,
	})
	provider.ID = NewAIID("provider")
	next.Providers = append(next.Providers, provider)
	for i := range next.Models {
		next.Models[i].IsDefault = false
	}
	created := make([]AIModel, len(in.Models))
	for i, qm := range in.Models {
		m := applyProvider(AIModel{
			ID:           "model_" + strconv.FormatInt(now.UnixMilli(), 10) + "_" + strconv.Itoa(i) + "_" + randBase36(5),
			Name:         strings.TrimSpace(qm.Name),
			Model:        strings.TrimSpace(qm.Model),
			Capabilities: NormalizeCapabilities(qm.Capabilities),
			IsDefault:    i == 0,
			Enabled:      true,
		}, provider)
		if qm.Verified {
			m.LastVerifiedAt = now.UTC().Format("2006-01-02T15:04:05.000Z")
		}
		created[i] = m
	}
	next.Models = append(next.Models, created...)
	for route, idx := range in.Routes {
		if idx >= 0 && idx < len(created) {
			next.Routes[route] = created[idx].ID
		}
	}
	return next
}
