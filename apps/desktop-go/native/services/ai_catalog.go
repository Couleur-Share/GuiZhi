package services

import (
	"net/url"
	"strings"

	"guizhi-mygo-shell/aiflow"
)

// AI 模型服务的目录数据与纯函数：供应商预设、模型分类、按模型名推断能力、端点地址处理。
// 全部移植自渲染层 components/settings/ai-workbench/{constants,helpers}.ts，无界面、无 IO，go test ./native/... 直接覆盖。

// AIProviderPreset 是供应商下拉里的一项（ProviderOption）。品牌名直接用 Fallback 显示；
// 只有「自定义」「智谱」「通义」「豆包」需要随界面语言翻译，由界面按 ID 取文案。
type AIProviderPreset struct {
	ID                   string
	Fallback             string
	DefaultURL           string
	RecommendedProtocol  string
	AllowsCustomProtocol bool
	// IconCategory 是图标分类（见 ModelCategory），决定用哪家品牌的图标。
	IconCategory string
}

var aiProviderPresets = []AIProviderPreset{
	{"custom", "Custom", "", AIProtocolOpenAI, true, "Custom"},
	{"openai", "OpenAI", "https://api.openai.com", AIProtocolOpenAI, false, "GPT"},
	{"openai-responses", "OpenAI-Response", "https://api.openai.com", AIProtocolOpenAI, false, "GPT"},
	{"google", "Gemini", "https://generativelanguage.googleapis.com", AIProtocolGemini, false, "Gemini"},
	{"anthropic", "Anthropic", "https://api.anthropic.com", AIProtocolAnthropic, false, "Claude"},
	{"azure-openai", "Azure OpenAI", "", AIProtocolOpenAI, true, "Azure OpenAI"},
	{"new-api", "New API", "", AIProtocolOpenAI, true, "New API"},
	{"ollama", "Ollama", "http://localhost:11434/v1", AIProtocolOpenAI, false, "Llama"},
	{"xai", "xAI", "https://api.x.ai", AIProtocolOpenAI, false, "Grok"},
	{"deepseek", "DeepSeek", "https://api.deepseek.com", AIProtocolOpenAI, false, "DeepSeek"},
	{"moonshot", "Moonshot", "https://api.moonshot.cn", AIProtocolOpenAI, false, "Moonshot"},
	{"zhipu", "Zhipu", "https://open.bigmodel.cn/api/paas", AIProtocolOpenAI, false, "GLM"},
	{"qwen", "Qwen", "https://dashscope.aliyuncs.com/compatible-mode", AIProtocolOpenAI, false, "Qwen"},
	{"doubao", "Doubao", "https://ark.cn-beijing.volces.com/api", AIProtocolOpenAI, false, "Doubao"},
}

// AIProviderPresets 返回全部预设（顺序即下拉顺序；返回副本）。
func AIProviderPresets() []AIProviderPreset {
	return append([]AIProviderPreset(nil), aiProviderPresets...)
}

// AIProviderPresetByID 按 ID 查预设。
func AIProviderPresetByID(id string) (AIProviderPreset, bool) {
	for _, p := range aiProviderPresets {
		if p.ID == id {
			return p, true
		}
	}
	return AIProviderPreset{}, false
}

type categoryRule struct {
	category string
	id       []string
	owner    []string
}

var modelCategoryRules = []categoryRule{
	{"GPT", []string{"gpt", "o1", "o3", "o4"}, []string{"openai"}},
	{"Claude", []string{"claude"}, []string{"anthropic"}},
	{"Gemini", []string{"gemini"}, []string{"google", "vertexai", "google-deepmind"}},
	{"DeepSeek", []string{"deepseek"}, []string{"deepseek"}},
	{"Qwen", []string{"qwen", "qwq"}, []string{"qwen", "aliyun", "dashscope"}},
	{"StepFun", []string{"stepfun", "step-"}, []string{"stepfun", "step"}},
	{"MiniMax", []string{"minimax", "abab"}, []string{"minimax"}},
	{"Doubao", []string{"doubao"}, []string{"doubao", "volcengine"}},
	{"GLM", []string{"glm", "zhipu"}, []string{"zhipu"}},
	{"Moonshot", []string{"moonshot", "kimi"}, []string{"moonshot"}},
	{"Baichuan", []string{"baichuan"}, []string{"baichuan"}},
	{"Grok", []string{"grok", "x-ai", "xai"}, []string{"x-ai", "xai"}},
	{"Command", []string{"command-r", "command-a", "cohere"}, []string{"cohere"}},
	{"Llama", []string{"llama", "meta-llama"}, []string{"meta", "meta-llama"}},
	{"Gemma", []string{"gemma"}, []string{"google"}},
	{"Mistral", []string{"mistral", "mixtral"}, []string{"mistral"}},
	{"Yi", []string{"yi-"}, []string{"01-ai", "zeroone", "zero-one"}},
	{"ERNIE", []string{"ernie", "wenxin"}, []string{"baidu", "wenxin"}},
	{"Spark", []string{"spark", "xunfei"}, []string{"xunfei", "iflytek"}},
	{"Hunyuan", []string{"hunyuan"}, []string{"tencent"}},
	{"InternLM", []string{"internlm"}, []string{"internlm", "shanghai-ai-lab"}},
	{"Phi", []string{"phi-"}, []string{"microsoft"}},
	{"Nova", []string{"nova-"}, []string{"amazon"}},
	{"Jamba", []string{"jamba"}, []string{"ai21"}},
	{"Sonar", []string{"sonar"}, []string{"perplexity"}},
}

var providerCategory = map[string]string{
	"openai": "GPT", "openai-responses": "GPT", "azure-openai": "GPT", "anthropic": "Claude", "google": "Gemini",
	"deepseek": "DeepSeek", "qwen": "Qwen", "stepfun": "StepFun", "minimax": "MiniMax", "doubao": "Doubao",
	"zhipu": "GLM", "moonshot": "Moonshot", "baichuan": "Baichuan", "xai": "Grok", "x-ai": "Grok",
	"cohere": "Command", "llama": "Llama", "meta": "Llama", "gemma": "Gemma", "mistral": "Mistral",
	"yi": "Yi", "ernie": "ERNIE", "spark": "Spark", "hunyuan": "Hunyuan", "internlm": "InternLM",
	"microsoft": "Phi", "amazon": "Nova", "ai21": "Jamba", "perplexity": "Sonar", "ollama": "Llama",
}

func containsAny(s string, keywords ...string) bool {
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// ProviderIconCategory 是供应商 ID 对应的图标分类（getProviderIconCategory）；未知的给 Other。
func ProviderIconCategory(providerID string) string {
	if p, ok := AIProviderPresetByID(providerID); ok {
		return p.IconCategory
	}
	if c, ok := providerCategory[strings.ToLower(providerID)]; ok {
		return c
	}
	return "Other"
}

// ModelCategory 给模型归类（getModelCategory）：供应商明确时按供应商，否则按模型 ID 关键词、再按 owned_by。
// provider 为空表示不看供应商（拉取模型列表时只有模型 ID 与 owned_by）。
func ModelCategory(provider, modelID, ownedBy string) string {
	p := strings.ToLower(provider)
	if p != "" && p != "custom" {
		if c, ok := providerCategory[p]; ok {
			return c
		}
	}
	id := strings.ToLower(modelID)
	owner := strings.ToLower(ownedBy)
	for _, r := range modelCategoryRules {
		if containsAny(id, r.id...) {
			return r.category
		}
	}
	for _, r := range modelCategoryRules {
		if containsAny(owner, r.owner...) {
			return r.category
		}
	}
	switch {
	case containsAny(id, "embedding", "text-embedding"):
		return "Embedding"
	case strings.Contains(id, "rerank"):
		return "Rerank"
	case containsAny(id, "whisper", "tts"):
		return "Audio"
	case containsAny(id, "dall-e", "stable-diffusion"):
		return "Image"
	}
	return "Other"
}

// DefaultAICapabilities 是新模型的默认能力：只有对话。
func DefaultAICapabilities() AICapabilities { return AICapabilities{Chat: true} }

// InferModelCapabilities 按模型 ID 推断能力（inferModelAttributes）：向量、转写、文生图、重排各自一类（非对话），
// 其余按关键词判定视觉、推理、联网。关键词表是启发式的，用户可在模型表单里改。
func InferModelCapabilities(modelID string) AICapabilities {
	id := strings.ToLower(modelID)
	caps := DefaultAICapabilities()
	switch {
	case containsAny(id, "embedding", "text-embedding", "bge-m3", "gte-", "e5-", "jina-embeddings"):
		caps.Chat, caps.Embedding = false, true
	case containsAny(id, "whisper", "transcri", "paraformer", "sensevoice", "funasr"):
		caps.Chat, caps.AudioTranscription = false, true
	case containsAny(id, "gpt-image", "dall-e", "stable-diffusion", "flux", "-image", "seedream", "wanx", "kolors"):
		caps.Chat, caps.ImageGeneration = false, true
	case containsAny(id, "rerank", "reranker", "jina-reranker", "bge-reranker"):
		caps.Chat, caps.Rerank = false, true
	default:
		caps.Vision = containsAny(id, "vision", "vl", "gpt-4o", "gpt-5", "o3", "o4", "claude-3", "claude-4", "gemini", "qwen-vl", "glm-4v")
		caps.Reasoning = containsAny(id, "reasoning", "o1", "o3", "o4", "deepseek-r1", "qwen3", "qwq", "grok-3", "grok-4")
		caps.WebSearch = containsAny(id, "search", "sonar", "perplexity", "grok")
	}
	return caps
}

// 能力开关：渲染层 BaseFields 的互斥规则。视觉与推理依附于对话（勾选时回到对话模型，并退出专用模型）；
// 向量、转写、文生图是专用模型（勾选时退出对话，且互斥）。每个函数返回新值，不改原值。

// ToggleVision 切换视觉能力：回到对话模型，退出向量、转写、文生图。
func (c AICapabilities) ToggleVision(on bool) AICapabilities {
	c.Chat, c.Vision = true, on
	c.Embedding, c.AudioTranscription, c.ImageGeneration = false, false, false
	return c
}

// ToggleReasoning 切换推理能力：回到对话模型，退出向量、转写、文生图。
func (c AICapabilities) ToggleReasoning(on bool) AICapabilities {
	c.Chat, c.Reasoning = true, on
	c.Embedding, c.AudioTranscription, c.ImageGeneration = false, false, false
	return c
}

// ToggleEmbedding 切换向量能力：勾选后是专用模型（退出对话与视觉，推理关闭）。
func (c AICapabilities) ToggleEmbedding(on bool) AICapabilities {
	return c.dedicated(on, func(c *AICapabilities) { c.Embedding = on })
}

// ToggleAudioTranscription 切换转写能力：勾选后是专用模型。
func (c AICapabilities) ToggleAudioTranscription(on bool) AICapabilities {
	return c.dedicated(on, func(c *AICapabilities) { c.AudioTranscription = on })
}

// ToggleImageGeneration 切换文生图能力：勾选后是专用模型。
func (c AICapabilities) ToggleImageGeneration(on bool) AICapabilities {
	return c.dedicated(on, func(c *AICapabilities) { c.ImageGeneration = on })
}

// dedicated 实现专用能力的共同规则：对话取反、视觉关闭、三项专用能力先清零再按 set 设置；勾选时推理关闭，取消时保留。
func (c AICapabilities) dedicated(on bool, set func(*AICapabilities)) AICapabilities {
	c.Chat, c.Vision = !on, false
	if on {
		c.Reasoning = false
	}
	c.Embedding, c.AudioTranscription, c.ImageGeneration = false, false, false
	set(&c)
	return c
}

// 端点地址处理：包一层 aiflow 的实现，保证界面预览、保存规范化与真正发请求用的是同一份规则。

// NormalizeAPIURL 规范化用户输入的端点地址（去掉结尾 / 与已知端点后缀；以 # 结尾的原样保留标记）。
func NormalizeAPIURL(apiURL string) string { return aiflow.NormalizeAPIURLInput(apiURL) }

// APIEndpointPreview 是对话端点的展示形式（# 结尾不补路径，缺 /v1 自动补全）。
func APIEndpointPreview(apiURL, protocol string) string {
	return aiflow.APIEndpointPreview(apiURL, protocol)
}

// NormalizeAIProtocol 归一化 API 协议：显式取值优先，否则按供应商与地址推断，默认 openai。
func NormalizeAIProtocol(value, provider, apiURL string) string {
	return aiflow.ResolveAIProtocol(value, strings.TrimSpace(provider), strings.TrimSpace(apiURL))
}

// EndpointHost 取地址的主机（含端口）；解析不了时退回原文或 fallback。
func EndpointHost(apiURL, fallback string) string {
	if u, err := url.Parse(apiURL); err == nil && u.Host != "" {
		return u.Host
	}
	if apiURL != "" {
		return apiURL
	}
	return fallback
}

// 内置本地转写引擎（FunASR）写入配置时使用的固定 ID（与 funasr_manager.go 及渲染层 FUNASR_PROVIDER_ID / FUNASR_MODEL_ID 一致）：
// 界面据此把它的条目显示为「内置、只读」，安装与卸载都在「采集与转写」分区。
const (
	AIFunasrProviderID = "provider_local_funasr"
	AIFunasrModelID    = "model_local_sensevoice"
)

// EndpointCategory 是端点图标的分类（getEndpointCategory）：供应商已知时按供应商，否则看第一个模型。
func EndpointCategory(provider string, models []AIModel) string {
	if c, ok := providerCategory[strings.ToLower(provider)]; ok {
		return c
	}
	if len(models) > 0 {
		return ModelCategory(models[0].Provider, firstNonEmpty(models[0].Model, models[0].ID), "")
	}
	return "Other"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// MaskAPIKey 是密钥的展示形态（maskApiKey）：空或不超过 8 个字符一律六颗星；否则只露出前 3 位与后 4 位。
// 界面里任何要「展示」密钥的地方都只能经它，明文只存在于输入框内部且默认是圆点。
func MaskAPIKey(apiKey string) string {
	trimmed := strings.TrimSpace(apiKey)
	runes := []rune(trimmed)
	if len(runes) <= 8 {
		return "******"
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}

// BaseURL 是端点地址去掉结尾 / 与已知端点后缀后的基础地址（getBaseUrl）；以 # 结尾只去掉 #。
func BaseURL(apiURL string) string { return aiflow.GetBaseURL(apiURL) }
