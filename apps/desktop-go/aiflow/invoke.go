package aiflow

// 场景化 AI 调用（对应 services/knowledge-ai/ai-invoke.ts）：从设置解析场景对应的模型并发起对话，
// 失败与成功都进用量统计。摘要、标签、问答、Wiki 编译全经此收口。

import "context"

// scenarioChatTimeoutMs：知识域的调用都是「长提示词 + 整段生成」，代理层 30 秒的兜底超时是按连通性探测定的，
// 套到这里会在生成中途被掐断。
const scenarioChatTimeoutMs = 120_000

// UsageEntry 是一次调用的计量。
type UsageEntry struct {
	Scenario         string
	Model            string
	PromptTokens     float64
	CompletionTokens float64
	Failed           bool
}

// UsageSink 记录一次调用的消耗。统计失败不能影响业务结果，实现方自行吞掉错误。
type UsageSink interface {
	RecordUsage(entry UsageEntry)
}

// Runner 是 aiflow 的入口：把配置来源、传输层与用量统计串起来。
type Runner struct {
	Client *Client
	Config ConfigSource
	Usage  UsageSink // 可为 nil
	Log    Logger    // 可为 nil
}

func (r *Runner) logger() Logger {
	if r.Log != nil {
		return r.Log
	}
	return nopLogger{}
}

// ResolveConfig 对应 resolveConfig（不传 aiProviders：与渲染层一致，供应商停用状态只在 embedding 解析里生效）。
func (r *Runner) ResolveConfig(scenario Scenario) (AIConfig, bool) {
	return r.Config.AISettings().ResolveScenarioConfig(scenario, true, nil)
}

// IsScenarioConfigured 对应 isScenarioConfigured / isAiConfiguredForScenario。
func (r *Runner) IsScenarioConfigured(scenario Scenario) bool {
	_, ok := r.ResolveConfig(scenario)
	return ok
}

func (r *Runner) recordUsage(e UsageEntry) {
	if r.Usage != nil {
		r.Usage.RecordUsage(e)
	}
}

// ScenarioChatOptions 对应 runScenarioChat 的 options；取消走 ctx。
type ScenarioChatOptions struct {
	Temperature *float64
	MaxTokens   int
	// TimeoutMs 为 0 时用场景默认的 120 秒。
	TimeoutMs float64
	// OnDelta 非 nil 时走流式，逐块回调原始输出。
	// 问答是唯一有人盯着屏幕等的场景；摘要、标签、Wiki 编译都是后台批处理，整段返回反而省掉一路 SSE 解析。
	OnDelta func(chunk string)
}

// ScenarioChatResult 对应 ScenarioChatResult。
type ScenarioChatResult struct {
	Content string
	Model   string
	// FinishReason 为 "length" 表示输出撞上 max_tokens 被截断。
	FinishReason string
}

// RunScenarioChat 对应 runScenarioChat。
func (r *Runner) RunScenarioChat(ctx context.Context, scenario Scenario, messages []ChatMessage, opts ScenarioChatOptions) (ScenarioChatResult, error) {
	cfg, ok := r.ResolveConfig(scenario)
	if !ok {
		return ScenarioChatResult{}, ErrNotConfigured
	}
	timeout := opts.TimeoutMs
	if timeout == 0 {
		timeout = scenarioChatTimeoutMs
	}
	result, err := r.Client.Chat(ctx, cfg, messages, ChatOptions{
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
		Stream:      opts.OnDelta != nil,
		OnStream:    opts.OnDelta,
		TimeoutMs:   timeout,
	})
	if err != nil {
		// 失败的调用同样要进统计：超时、限流重试、思考 token 烧光后返回空正文都已经产生了费用，
		// 只记成功会让面板显著低估实际消耗
		r.recordUsage(UsageEntry{Scenario: string(scenario), Model: cfg.Model, Failed: true})
		return ScenarioChatResult{}, err
	}
	// 截断是「HTTP 200 但没写完」，调用方不看就永远发现不了。这里是知识域唯一的收口点，
	// 先记一笔，让静默截断变成日志里数得出来的事件。
	if result.FinishReason == "length" {
		limit := "默认"
		if opts.MaxTokens != 0 {
			limit = jsNumberString(float64(opts.MaxTokens))
		}
		r.logger().Warnf("[ai] %s 输出被 max_tokens 截断（上限 %s），结果可能不完整", scenario, limit)
	}
	entry := UsageEntry{Scenario: string(scenario), Model: cfg.Model}
	if result.Usage != nil {
		entry.PromptTokens = result.Usage.PromptTokens
		entry.CompletionTokens = result.Usage.CompletionTokens
	}
	r.recordUsage(entry)
	return ScenarioChatResult{Content: result.Content, Model: cfg.Model, FinishReason: result.FinishReason}, nil
}
