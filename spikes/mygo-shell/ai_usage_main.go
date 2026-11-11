package main

// 对应 services/ai-call-context.ts 与 ai-usage.ts：主进程发起的 AI 调用的归属与记账。
//
// TS 用 AsyncLocalStorage 把「当前导入任务」隐式带到 recordMainAiUsage；Go 里用 context 值，
// 沿调用链自然传递，嵌套以最内层为准。

import (
	"context"
	"fmt"
)

type aiCallSinkKeyT struct{}

// withAICallSink 在 ctx 的整个调用链内，把主进程发起的 AI 调用汇报给 sink。
func withAICallSink(ctx context.Context, sink func(aiCallRecord)) context.Context {
	return context.WithValue(ctx, aiCallSinkKeyT{}, sink)
}

// reportAICall 汇报一次 AI 调用；不在任何作用域内时是空操作。
func reportAICall(ctx context.Context, rec aiCallRecord) {
	if sink, ok := ctx.Value(aiCallSinkKeyT{}).(func(aiCallRecord)); ok && sink != nil {
		sink(rec)
	}
}

// mainAIUsage 是 recordMainAiUsage 的入参。
type mainAIUsage struct {
	Scenario         string // summary | formatting | transcription | ...
	Model            string
	PromptTokens     float64
	CompletionTokens float64
	Failed           bool
}

// recordMainAIUsage 记账失败绝不影响主流程：为了记一笔账把用户一次几十秒的生成搞失败是本末倒置。
func (l *Library) recordMainAIUsage(ctx context.Context, e mainAIUsage) {
	if e.Model == "" {
		return
	}
	// 先归属到当前导入任务（阶段统计只写在任务行上，不该因用量库不可用而丢）
	reportAICall(ctx, aiCallRecord{Failed: e.Failed, PromptTokens: int64(e.PromptTokens), CompletionTokens: int64(e.CompletionTokens), Model: e.Model})
	if l.db == nil {
		return
	}
	// 用 WithoutCancel：任务被取消时，已发生的调用仍应入账
	if err := l.aiUsageRecord(context.WithoutCancel(ctx), e.Scenario, e.Model, e.PromptTokens, e.CompletionTokens, e.Failed); err != nil {
		fmt.Printf("[usage] 记录 AI 用量失败: %v\n", err)
	}
}
