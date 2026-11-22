package services

import (
	"context"
	"fmt"
	"strings"
)

// 设置页「AI 模型服务」分区依赖的系统能力：供应商（端点）、模型、场景路由的读写，端点连通性测试，
// 远端模型列表拉取，AI 用量统计。
//
// 对应主进程的 settings:get / settings:set（AI 部分落在 config/ai-models.json）、ai:httpRequest（经 Go 壳代理的
// 出站请求，受 net_safety.go 的 SSRF 防护）、media:testTranscription、ai:usageSummary / ai:usageClear 频道。
// 界面（viva/screens/settings）只依赖本接口；适配器（package main 的 native_settings_ai.go）把它接到现有处理器，
// 行为与渲染层是同一个来源。
//
// 机密：API Key 在 AIProvider / AIModel / AILegacy / AIQuickSetupInput 里都是明文字段（配置文件本来就存明文），
// 但这些类型的 String / GoString 一律脱敏，所以 %v / %+v / %#v 打日志不会带出密钥；
// 界面对 API Key 只做掩码展示与掩码输入，错误详情与 toast 不得拼入密钥。

// 模型路由（settings 里的 modelRouteDefaults 的键）。
const (
	AIRouteMainText   = "mainText"
	AIRouteFastText   = "fastText"
	AIRouteVisionText = "visionText"
	AIRouteEmbedding  = "embedding"
	AIRouteAudioText  = "audioText"
	AIRouteImageGen   = "imageGen"
)

// AIRoutes 是全部路由，顺序即界面里「场景默认模型」卡片的顺序。
var AIRoutes = []string{AIRouteMainText, AIRouteFastText, AIRouteVisionText, AIRouteEmbedding, AIRouteAudioText, AIRouteImageGen}

// API 协议取值。
const (
	AIProtocolOpenAI    = "openai"
	AIProtocolGemini    = "gemini"
	AIProtocolAnthropic = "anthropic"
)

// 拉取模型列表失败的原因（AIFetchModelsResult.Reason）。
const (
	AIFetchReasonAuth        = "auth"
	AIFetchReasonNetwork     = "network"
	AIFetchReasonUnsupported = "unsupported"
	AIFetchReasonHTTP        = "http"
	AIFetchReasonParse       = "parse"
)

// AICapabilities 是模型能力（AIModelCapabilities）。Chat 缺省为 true；Vision 依附于 Chat。
type AICapabilities struct {
	Chat               bool `json:"chat"`
	Vision             bool `json:"vision"`
	Reasoning          bool `json:"reasoning"`
	ToolUse            bool `json:"toolUse"`
	WebSearch          bool `json:"webSearch"`
	Embedding          bool `json:"embedding"`
	Rerank             bool `json:"rerank"`
	AudioTranscription bool `json:"audioTranscription"`
	ImageGeneration    bool `json:"imageGeneration"`
}

// AIProvider 是一个端点（AIProviderConfig）：一组地址与密钥，名下可挂多个模型。
type AIProvider struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Provider    string `json:"provider"`
	APIProtocol string `json:"apiProtocol"`
	APIKey      string `json:"apiKey"`
	APIURL      string `json:"apiUrl"`
	Enabled     bool   `json:"enabled"`
}

// AIModel 是一个模型（AIModelConfig）。连接字段（Provider…APIURL）是所属端点的镜像，保存时由 SyncWithProvider 对齐。
type AIModel struct {
	ID           string         `json:"id"`
	ProviderID   string         `json:"providerId,omitempty"`
	Name         string         `json:"name,omitempty"`
	Provider     string         `json:"provider"`
	APIProtocol  string         `json:"apiProtocol"`
	APIKey       string         `json:"apiKey"`
	APIURL       string         `json:"apiUrl"`
	Model        string         `json:"model"`
	IsDefault    bool           `json:"isDefault,omitempty"`
	Enabled      bool           `json:"enabled"`
	Capabilities AICapabilities `json:"capabilities"`
	// LastVerifiedAt 是最近一次连通性测试成功的时间（ISO 8601 字符串，与渲染层同一个格式）。
	LastVerifiedAt string `json:"lastVerifiedAt,omitempty"`
}

// AILegacy 是旧版单模型字段（aiProvider / aiApiKey …）。主进程只在没有模型列表时才用，界面只用来提示迁移。
type AILegacy struct {
	Provider    string `json:"aiProvider"`
	APIProtocol string `json:"aiApiProtocol"`
	APIKey      string `json:"aiApiKey"`
	APIURL      string `json:"aiApiUrl"`
	Model       string `json:"aiModel"`
}

// HasContent 报告旧版字段里是否有任何内容。
func (l AILegacy) HasContent() bool {
	return strings.TrimSpace(l.APIKey) != "" || strings.TrimSpace(l.APIURL) != "" || strings.TrimSpace(l.Model) != ""
}

// AIConfig 是设置里与 AI 有关的整份状态：端点、模型、路由，加上旧版字段。
// 保存是整体替换（主进程做校验与规范化，失败则一个字都不写）。
type AIConfig struct {
	Providers []AIProvider      `json:"aiProviders"`
	Models    []AIModel         `json:"aiModels"`
	Routes    map[string]string `json:"modelRouteDefaults"`
	AILegacy
}

// maskKey 把密钥缩成 `***`（有）或 `(empty)`（无），日志里能看出是否配置了而看不到内容。
func maskKey(key string) string {
	if strings.TrimSpace(key) == "" {
		return "(empty)"
	}
	return "***"
}

// String 给日志与 %v 用：API Key 一律脱敏。
func (p AIProvider) String() string {
	return fmt.Sprintf("{id=%s name=%q provider=%s protocol=%s url=%s key=%s enabled=%t}",
		p.ID, p.Name, p.Provider, p.APIProtocol, p.APIURL, maskKey(p.APIKey), p.Enabled)
}

// GoString 让 %#v 同样脱敏。
func (p AIProvider) GoString() string { return "services.AIProvider" + p.String() }

// String 给日志与 %v 用：API Key 一律脱敏。
func (m AIModel) String() string {
	return fmt.Sprintf("{id=%s providerId=%s model=%s provider=%s protocol=%s url=%s key=%s enabled=%t}",
		m.ID, m.ProviderID, m.Model, m.Provider, m.APIProtocol, m.APIURL, maskKey(m.APIKey), m.Enabled)
}

// GoString 让 %#v 同样脱敏。
func (m AIModel) GoString() string { return "services.AIModel" + m.String() }

// String 给日志与 %v 用：API Key 一律脱敏。
func (l AILegacy) String() string {
	return fmt.Sprintf("{provider=%s protocol=%s url=%s model=%s key=%s}",
		l.Provider, l.APIProtocol, l.APIURL, l.Model, maskKey(l.APIKey))
}

// GoString 让 %#v 同样脱敏。
func (l AILegacy) GoString() string { return "services.AILegacy" + l.String() }

// String 给日志与 %v 用：整份配置只输出数量与路由，不展开端点与模型。
func (c AIConfig) String() string {
	return fmt.Sprintf("{providers=%d models=%d routes=%v legacy=%s}", len(c.Providers), len(c.Models), c.Routes, c.AILegacy.String())
}

// GoString 让 %#v 同样脱敏。
func (c AIConfig) GoString() string { return "services.AIConfig" + c.String() }

// AITestOutcome 是一次连通性测试的结果。Error 是给人看的原因（已不含密钥）。
type AITestOutcome struct {
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latencyMs"`
	Error     string `json:"error"`
}

// AIRemoteModel 是远端模型列表里的一项（ModelInfo）。
type AIRemoteModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OwnedBy string `json:"ownedBy"`
}

// AIFetchModelsResult 是拉取模型列表的结果（FetchModelsResult）。失败不是 Go 错误：界面要按 Reason 区分提示。
type AIFetchModelsResult struct {
	Success bool            `json:"success"`
	Models  []AIRemoteModel `json:"models"`
	// Error 是失败原因原文（来自远端响应摘要，不含请求头，所以不含密钥）。
	Error string `json:"error"`
	// Reason 见 AIFetchReason*。
	Reason string `json:"reason"`
}

// AIUsageRow 是用量表的一行：按场景（Scenario）或按模型（Model）汇总，二选一有值。
type AIUsageRow struct {
	Scenario         string `json:"scenario"`
	Model            string `json:"model"`
	Calls            int64  `json:"calls"`
	FailedCalls      int64  `json:"failedCalls"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
}

// AIUsageSummary 是最近 Days 天的 AI 用量汇总（ai:usageSummary）。
type AIUsageSummary struct {
	Days             int          `json:"days"`
	Calls            int64        `json:"calls"`
	FailedCalls      int64        `json:"failedCalls"`
	PromptTokens     int64        `json:"promptTokens"`
	CompletionTokens int64        `json:"completionTokens"`
	ByScenario       []AIUsageRow `json:"byScenario"`
	ByModel          []AIUsageRow `json:"byModel"`
}

// AISettings 是「AI 模型服务」分区读写与探测的全部入口。
//
// 全部走 Go 壳：出站 HTTP 经 ai:httpRequest 代理（SSRF 防护、代理设置、超时都在那里），界面进程不直接联网。
type AISettings interface {
	// Load 读取当前 AI 配置（供应商、模型、路由、旧版字段）。
	Load(ctx context.Context) (AIConfig, error)
	// Save 整体替换 AI 配置。主进程校验失败（如端点地址非法）返回带原因的错误，此时磁盘上的配置保持原样。
	Save(ctx context.Context, cfg AIConfig) error
	// TestModel 按模型能力走真实端点做一次连通性测试：转写模型走转写探针，向量模型走 embeddings，其余走一次极小的对话。
	// 测试本身失败（鉴权、网络、限流）不是 Go 错误，而是 Success=false 的结果；err 只表示没能发起测试。
	TestModel(ctx context.Context, m AIModel) (AITestOutcome, error)
	// FetchModels 拉取端点的远端模型列表。apiKey 与 apiURL 都不能为空。
	FetchModels(ctx context.Context, apiURL, apiKey, apiProtocol string) (AIFetchModelsResult, error)
	// UsageSummary 读最近 days 天（1–90）的用量汇总。
	UsageSummary(ctx context.Context, days int) (AIUsageSummary, error)
	// ClearUsage 清空全部用量记录（不可恢复）。
	ClearUsage(ctx context.Context) error
}
