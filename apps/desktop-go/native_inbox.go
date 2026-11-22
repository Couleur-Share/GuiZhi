package main

// 处理中心（services.Inbox）的主包适配：把 inbox:*、semantic:*、wiki:compiler、import:retry 等现有 IPC 处理器，
// 以及 aiflow 的归类与语义索引执行器，包成原生界面用的窄接口。不写新的后端逻辑——
// 数据库读写都在处理器里，AI 请求都在 aiflow 里。

import (
	"context"
	"errors"
	"sync/atomic"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

// inboxAdapter 是 services.Inbox 的生产实现。必须以指针使用：语义索引的「正在执行」标记是实例状态。
type inboxAdapter struct {
	b        *Bridge
	ai       libraryAdapter // 复用它读设置、构造 aiflow.Runner 的逻辑
	indexing atomic.Bool
	index    *aiflow.BackgroundClient // 只用它的 RunSemanticIndexing：执行并把结果归类成回执
}

var _ services.Inbox = (*inboxAdapter)(nil)

func newInboxAdapter(b *Bridge) *inboxAdapter {
	a := &inboxAdapter{b: b, ai: libraryAdapter{b: b}}
	a.index = &aiflow.BackgroundClient{
		Semantic: inboxSemanticIndexer{a: a},
		Store:    inboxSemanticStore{b: b},
	}
	return a
}

// ---- 读取 ----

// List 对应 inbox:list；两个后台聚合行（语义索引、Wiki 编译）不由它产出，计数里 wiki-pending 恒为 0，由界面层另行补上。
func (a *inboxAdapter) List(ctx context.Context) (services.InboxList, error) {
	list, err := callAs[services.InboxList](ctx, a.b, "inbox:list")
	if list.Items == nil {
		list.Items = []services.InboxItem{}
	}
	return list, err
}

// WikiCandidates 对应渲染层 wikiPreview().counts：把当前 Wiki 模型的指纹交给 wiki:compiler 的 preview（没配模型时是空串，
// 候选会被计入「等待」）。
func (a *inboxAdapter) WikiCandidates(ctx context.Context) (services.InboxWikiCandidates, error) {
	runner, err := a.ai.newAIRunner(ctx)
	if err != nil {
		return services.InboxWikiCandidates{}, err
	}
	identity := (&aiflow.WikiDriver{Runner: runner}).ModelIdentity()
	res, err := callAs[struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Preview *struct {
			Counts services.InboxWikiCandidates `json:"counts"`
		} `json:"preview"`
	}](ctx, a.b, chWikiCompiler, map[string]any{"action": "preview", "model": identity})
	if err != nil {
		return services.InboxWikiCandidates{}, err
	}
	if !res.OK || res.Preview == nil {
		if res.Error == "" {
			res.Error = "编译预览失败"
		}
		return services.InboxWikiCandidates{}, errors.New(res.Error)
	}
	return res.Preview.Counts, nil
}

// WikiModelReady 对应 isScenarioConfigured("wiki")。
func (a *inboxAdapter) WikiModelReady(ctx context.Context) (bool, error) {
	runner, err := a.ai.newAIRunner(ctx)
	if err != nil {
		return false, err
	}
	_, ok := runner.ResolveConfig(aiflow.ScenarioWiki)
	return ok, nil
}

// SemanticStatus 对应 resolveEmbeddingConfig + semantic:status：没有配置 embedding 模型时不查状态（语义检索是可选增强）。
func (a *inboxAdapter) SemanticStatus(ctx context.Context) (services.InboxSemanticStatus, error) {
	raw, err := callAs[map[string]any](ctx, a.b, "settings:get")
	if err != nil {
		return services.InboxSemanticStatus{}, err
	}
	cfg, ok := aiflow.NormalizeAISettings(raw).ResolveEmbeddingConfig()
	if !ok {
		return services.InboxSemanticStatus{}, nil
	}
	status, err := callAs[services.InboxSemanticStatus](ctx, a.b, "semantic:status", cfg.Model)
	status.Configured = true
	return status, err
}

// ---- 写入 ----

func (a *inboxAdapter) Organize(ctx context.Context, in services.InboxOrganizeInput) (int, error) {
	return callAs[int](ctx, a.b, "inbox:organize", in)
}

func (a *inboxAdapter) MarkReviewed(ctx context.Context, itemIDs []string) (int, error) {
	return callAs[int](ctx, a.b, "inbox:markReviewed", nonNilStrings(itemIDs))
}

func (a *inboxAdapter) AcknowledgeImportWarning(ctx context.Context, taskID string) (int, error) {
	return callAs[int](ctx, a.b, "inbox:acknowledgeImportWarning", taskID)
}

// RetryImportTask 复用采集队列适配器的 Retry（任务不存在时处理器返回 null，翻译成 found=false）。
func (a *inboxAdapter) RetryImportTask(ctx context.Context, taskID string) (bool, error) {
	return importsAdapter{b: a.b}.Retry(ctx, taskID, services.ImportRetryOptions{})
}

// ---- AI 智能归类 ----

func (a *inboxAdapter) AIClassificationSources(ctx context.Context, itemIDs []string) ([]services.InboxAISource, error) {
	out, err := callAs[[]services.InboxAISource](ctx, a.b, "inbox:aiClassificationSources", nonNilStrings(itemIDs))
	if out == nil {
		out = []services.InboxAISource{}
	}
	return out, err
}

func (a *inboxAdapter) Classify(ctx context.Context, sources []services.InboxAISource, existing []string, onProgress func(completed, total int)) ([]services.InboxAIAssignment, error) {
	runner, err := a.ai.newAIRunner(ctx)
	if err != nil {
		return nil, err
	}
	in := make([]aiflow.InboxAIClassificationSource, len(sources))
	for i, s := range sources {
		in[i] = aiflow.InboxAIClassificationSource{ItemID: s.ItemID, Title: s.Title, Excerpt: s.Excerpt}
	}
	plan, err := runner.ClassifyInboxItems(ctx, in, existing, onProgress)
	if err != nil {
		return nil, mapLibraryAIError(err)
	}
	out := make([]services.InboxAIAssignment, len(plan))
	for i, p := range plan {
		out[i] = services.InboxAIAssignment{ItemID: p.ItemID, CollectionName: p.CollectionName}
	}
	return out, nil
}

func (a *inboxAdapter) ApplyAIClassification(ctx context.Context, assignments []services.InboxAIAssignment) (services.InboxAIApplyResult, error) {
	if assignments == nil {
		assignments = []services.InboxAIAssignment{}
	}
	res, err := callAs[services.InboxAIApplyResult](ctx, a.b, "inbox:applyAiClassification", map[string]any{"assignments": assignments})
	if res.CreatedCollectionNames == nil {
		res.CreatedCollectionNames = []string{}
	}
	return res, err
}

// ---- 语义索引 ----

func (a *inboxAdapter) SemanticIndexing() bool { return a.indexing.Load() }

// RunSemanticIndexing 手动执行一轮语义索引。已有一轮在跑时返回 nil；失败归在回执里（Kind 为 failed）。
func (a *inboxAdapter) RunSemanticIndexing(ctx context.Context) (*services.InboxSemanticNotice, error) {
	if !a.indexing.CompareAndSwap(false, true) {
		return nil, nil
	}
	defer a.indexing.Store(false)
	notice := a.index.RunSemanticIndexing(ctx, nil)
	if notice == nil {
		return nil, nil
	}
	return &services.InboxSemanticNotice{
		Kind: string(notice.Kind), Indexed: notice.Indexed, Failed: notice.Failed, Message: notice.Message,
	}, nil
}

// inboxSemanticIndexer 满足 aiflow.SemanticIndexer：每一轮现读设置、现建 Runner，所以模型配置变化后下一轮就生效。
type inboxSemanticIndexer struct{ a *inboxAdapter }

func (i inboxSemanticIndexer) RunSemanticIndexing(ctx context.Context, store aiflow.SemanticIndexStore, onProgress func(indexed int)) (aiflow.SemanticIndexRunResult, error) {
	runner, err := i.a.ai.newAIRunner(ctx)
	if err != nil {
		return aiflow.SemanticIndexRunResult{}, err
	}
	return runner.RunSemanticIndexing(ctx, store, onProgress)
}

// inboxSemanticStore 满足 aiflow.SemanticIndexStore：直接调 semantic:* 处理器。
type inboxSemanticStore struct{ b *Bridge }

func (s inboxSemanticStore) ListPending(ctx context.Context, model string, limit int) ([]aiflow.PendingSemanticItem, error) {
	rows, err := callAs[[]struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Content     string  `json:"content"`
		Transcript  *string `json:"transcript"`
		ContentHash string  `json:"contentHash"`
	}](ctx, s.b, "semantic:listPending", map[string]any{"model": model, "limit": limit})
	if err != nil {
		return nil, err
	}
	out := make([]aiflow.PendingSemanticItem, len(rows))
	for i, r := range rows {
		out[i] = aiflow.PendingSemanticItem{ID: r.ID, Title: r.Title, Content: r.Content, Transcript: r.Transcript, ContentHash: r.ContentHash}
	}
	return out, nil
}

func (s inboxSemanticStore) ApplyEmbeddings(ctx context.Context, in aiflow.ApplySemanticEmbeddingsInput) (bool, error) {
	chunks := make([]map[string]any, len(in.Chunks))
	for i, c := range in.Chunks {
		chunks[i] = map[string]any{"text": c.Text, "vector": c.Vector}
	}
	return callAs[bool](ctx, s.b, "semantic:applyEmbeddings", map[string]any{
		"itemId": in.ItemID, "contentHash": in.ContentHash, "model": in.Model, "dims": in.Dims, "chunks": chunks,
	})
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
