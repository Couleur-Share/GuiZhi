package servicestest

import (
	"context"
	"slices"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

// 问答执行的脚本：AskFake 按入队顺序消费，每次问答一份；队列空了用默认脚本（回显问题）。

// AskScript 描述一次全局问答怎么「演」：先推步骤、警告、证据与流式文本，再（可选）卡住，最后给结果或错误。
type AskScript struct {
	Steps    []string
	Warnings []string
	Evidence [][]services.AskSource
	// Texts 是依次推送的「到目前为止的全文」。
	Texts []string
	// Gate 非 nil 时，推完上面的内容后等它关闭；等待期间 ctx 被取消就按 context.Canceled 结束。
	Gate chan struct{}
	// After 是卡住结束（放行或取消）之后仍然推送的流式文本：模拟取消之后迟到的回调。
	After  []string
	Answer services.AskAnswer
	Err    error
}

// ArticleScript 描述一次围绕本文问答怎么「演」：依次推补丁，（可选）卡住，最后成功或返回错误。
type ArticleScript struct {
	Patches []aiflow.ArticlePatch
	Gate    chan struct{}
	// After 是卡住结束之后仍然推送的补丁：模拟取消之后迟到的回调。
	After []aiflow.ArticlePatch
	Err   error
}

type askRunState struct {
	knowledge []AskScript
	article   []ArticleScript
	askCalls  []services.AskRequest
	artCalls  []services.ArticleAskRequest
}

// QueueAsk 追加全局问答脚本。
func (f *AskFake) QueueAsk(scripts ...AskScript) {
	f.with(func() { f.run.knowledge = append(f.run.knowledge, scripts...) })
}

// QueueArticle 追加围绕本文问答的脚本。
func (f *AskFake) QueueArticle(scripts ...ArticleScript) {
	f.with(func() { f.run.article = append(f.run.article, scripts...) })
}

// AskCalls 是收到的全局问答请求（历史已拷贝），按发生顺序。
func (f *AskFake) AskCalls() (out []services.AskRequest) {
	f.with(func() { out = slices.Clone(f.run.askCalls) })
	return out
}

// ArticleCalls 是收到的围绕本文问答请求，按发生顺序。
func (f *AskFake) ArticleCalls() (out []services.ArticleAskRequest) {
	f.with(func() { out = slices.Clone(f.run.artCalls) })
	return out
}

func waitRun(ctx context.Context, gate chan struct{}) error {
	if gate == nil {
		return nil
	}
	select {
	case <-gate:
		return nil
	case <-ctx.Done():
		return context.Canceled
	}
}

// AskKnowledge 按脚本演一次全局问答。
func (f *AskFake) AskKnowledge(ctx context.Context, req services.AskRequest, cb services.AskCallbacks) (services.AskAnswer, error) {
	f.mu.Lock()
	req.History = slices.Clone(req.History)
	f.run.askCalls = append(f.run.askCalls, req)
	script := AskScript{Answer: services.AskAnswer{Text: "echo:" + req.Question}}
	if len(f.run.knowledge) > 0 {
		script = f.run.knowledge[0]
		f.run.knowledge = f.run.knowledge[1:]
	}
	f.mu.Unlock()

	for _, step := range script.Steps {
		if cb.OnStep != nil {
			cb.OnStep(step)
		}
	}
	for _, warning := range script.Warnings {
		if cb.OnWarning != nil {
			cb.OnWarning(warning)
		}
	}
	for _, batch := range script.Evidence {
		if cb.OnEvidence != nil {
			cb.OnEvidence(slices.Clone(batch))
		}
	}
	for _, text := range script.Texts {
		if cb.OnText != nil {
			cb.OnText(text)
		}
	}
	waitErr := waitRun(ctx, script.Gate)
	for _, text := range script.After {
		if cb.OnText != nil {
			cb.OnText(text)
		}
	}
	if waitErr != nil {
		return services.AskAnswer{}, waitErr
	}
	if script.Err != nil {
		return services.AskAnswer{}, script.Err
	}
	return script.Answer, nil
}

// AskArticle 按脚本演一次围绕本文问答。
func (f *AskFake) AskArticle(ctx context.Context, req services.ArticleAskRequest, patch func(aiflow.ArticlePatch)) error {
	f.mu.Lock()
	req.History = slices.Clone(req.History)
	f.run.artCalls = append(f.run.artCalls, req)
	answer := "echo:" + req.Question
	done := "done"
	script := ArticleScript{Patches: []aiflow.ArticlePatch{
		{Answer: &answer, Status: &done, ClearStep: true},
	}}
	if len(f.run.article) > 0 {
		script = f.run.article[0]
		f.run.article = f.run.article[1:]
	}
	f.mu.Unlock()

	for _, p := range script.Patches {
		patch(p)
	}
	waitErr := waitRun(ctx, script.Gate)
	for _, p := range script.After {
		patch(p)
	}
	if waitErr != nil {
		return waitErr
	}
	return script.Err
}
