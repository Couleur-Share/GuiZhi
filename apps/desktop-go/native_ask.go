package main

// AI 问答（全局问答、围绕本文提问）的原生服务适配器：services.Ask → 现有 IPC 处理器与 aiflow。
//
// 会话持久化走 ask:* / askSession:* 处理器（保存时的证据脱敏、全文索引、自定义标题都在那里）；
// 问答本身用 aiflow 的 QAEngine / ArticleAsker，检索与读取依赖落到 knowledge:*、semantic:search、wiki:*、articleAsk:*，
// 请求出口是 bridgeTransport（ai:httpRequest / ai:httpStream 代理，SSRF 守卫与渲染进程共用）。
// 行为只有一个来源：渲染层的 askKnowledgeBase / askArticle 已经由 aiflow 逐行移植，这里只做接线与错误归类。
//
// 取消语义：ctx 取消 → 代理中断请求 → aiflow 返回 AbortError，本层统一翻成 context.Canceled。
// 机密：返回给界面和写进日志的错误文本都会抹掉当前设置里的全部 API Key。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

type askAdapter struct {
	b *Bridge
	// vectors 是查询向量缓存，跨提问共享（零值可用，内部有锁）。
	vectors *aiflow.QueryVectors
}

var _ services.Ask = (*askAdapter)(nil)

func newAskAdapter(b *Bridge) *askAdapter {
	return &askAdapter{b: b, vectors: &aiflow.QueryVectors{}}
}

// ---- 会话持久化 ----

func (a *askAdapter) QuerySessions(ctx context.Context, q services.AskSessionQuery) (services.AskSessionPage, error) {
	return callAs[services.AskSessionPage](ctx, a.b, "askSession:query", q)
}

func (a *askAdapter) ListSessions(ctx context.Context, f services.AskSessionFilter) ([]services.AskSessionMeta, error) {
	return callAs[[]services.AskSessionMeta](ctx, a.b, "ask:listSessions", f)
}

func (a *askAdapter) GetSession(ctx context.Context, id string) (*services.AskSessionRecord, error) {
	return callAs[*services.AskSessionRecord](ctx, a.b, "ask:getSession", id)
}

func (a *askAdapter) SaveSession(ctx context.Context, in services.SaveAskSessionInput) (services.AskSessionRecord, error) {
	return callAs[services.AskSessionRecord](ctx, a.b, "ask:saveSession", in)
}

func (a *askAdapter) DeleteSession(ctx context.Context, id string) (bool, error) {
	return callAs[bool](ctx, a.b, "ask:deleteSession", id)
}

func (a *askAdapter) UpdateSessionMeta(ctx context.Context, id string, patch services.AskSessionMetaPatch) (services.AskSessionRecord, error) {
	return callAs[services.AskSessionRecord](ctx, a.b, "askSession:meta", id, patch)
}

// ---- 配置与条目 ----

func (a *askAdapter) QAConfigured(ctx context.Context) (bool, error) {
	runner, err := newBridgeRunner(ctx, a.b, nil)
	if err != nil {
		return false, err
	}
	_, ok := runner.ResolveConfig(aiflow.ScenarioQA)
	return ok, nil
}

func (a *askAdapter) WebSearchConfigured(ctx context.Context) (bool, error) {
	out, err := callAs[struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Search  struct {
			Configured bool `json:"configured"`
		} `json:"search"`
	}](ctx, a.b, "themedReading:searchConfig")
	if err != nil {
		return false, err
	}
	if !out.Success {
		return false, errors.New(orStr(out.Error, "读取联网搜索配置失败"))
	}
	return out.Search.Configured, nil
}

func (a *askAdapter) ItemInfo(ctx context.Context, itemID string) (services.AskItem, bool, error) {
	item, err := getAskItem(ctx, a.b, itemID)
	if err != nil || item == nil {
		return services.AskItem{}, false, err
	}
	return services.AskItem{Title: item.Title, Deleted: item.DeletedAt != nil}, true, nil
}

func (a *askAdapter) SanitizeConversation(messagesJSON string, clearedItemIDs []string) (string, error) {
	cleared := make(map[string]bool, len(clearedItemIDs))
	for _, id := range clearedItemIDs {
		cleared[id] = true
	}
	return sanitizeEvidenceJSON(messagesJSON, cleared)
}

// ---- 问答 ----

// askRun 是一次提问的运行环境：按提问时刻的设置组装 Runner，并记下需要抹掉的 API Key。
type askRun struct {
	runner *aiflow.Runner
	keys   []string
}

func (a *askAdapter) newRun(ctx context.Context) (*askRun, error) {
	run := &askRun{}
	log := askLogger{b: a.b, run: run}
	runner, err := newBridgeRunner(ctx, a.b, log)
	if err != nil {
		return nil, err
	}
	run.runner = runner
	run.keys = apiKeysOf(runner.Config.AISettings())
	if _, ok := runner.ResolveConfig(aiflow.ScenarioQA); !ok {
		return nil, services.ErrAINotConfigured
	}
	return run, nil
}

// apiKeysOf 收集设置里出现的全部 API Key（过短的串会误伤正常文本，与 aiflow 的抹除规则一致）。
func apiKeysOf(s aiflow.AISettings) []string {
	var keys []string
	add := func(k string) {
		if len(k) >= 6 {
			keys = append(keys, k)
		}
	}
	for _, p := range s.Providers {
		add(p.APIKey)
	}
	for _, m := range s.Models {
		add(m.APIKey)
	}
	add(s.Legacy.APIKey)
	return keys
}

func scrubKeys(msg string, keys []string) string {
	for _, k := range keys {
		msg = strings.ReplaceAll(msg, k, "***")
	}
	return msg
}

// mapAskError 把引擎错误归类成 services.Ask 约定的哨兵；其余错误只保留脱敏后的文本。
func (r *askRun) mapAskError(err error) error {
	switch {
	case err == nil:
		return nil
	case aiflow.IsAborted(err):
		return context.Canceled
	case aiflow.IsNotConfigured(err):
		return services.ErrAINotConfigured
	case errors.Is(err, aiflow.ErrQANoSource):
		return services.ErrAskNoSource
	}
	msg := err.Error()
	if clean := scrubKeys(msg, r.keys); clean != msg {
		return errors.New(clean)
	}
	return err
}

// askLogger 把 aiflow 的诊断日志交给 log:appError（同一套截断与家目录脱敏），写入前再抹一遍 API Key。
type askLogger struct {
	b   *Bridge
	run *askRun
}

func (l askLogger) Warnf(format string, args ...any) {
	logAdapter{b: l.b}.AppError("ask", "ai", scrubKeys(fmt.Sprintf(format, args...), l.run.keys))
}

func (a *askAdapter) AskKnowledge(ctx context.Context, req services.AskRequest, cb services.AskCallbacks) (services.AskAnswer, error) {
	run, err := a.newRun(ctx)
	if err != nil {
		return services.AskAnswer{}, err
	}
	hybrid := &aiflow.Hybrid{Runner: run.runner, Source: askHybridSource{b: a.b}, Vectors: a.vectors}
	var onEvidence func([]aiflow.QASourceRef)
	if cb.OnEvidence != nil {
		onEvidence = func(sources []aiflow.QASourceRef) { cb.OnEvidence(toAskSources(sources)) }
	}
	deps := aiflow.NewQADeps(run.runner, hybrid, askQAStore{b: a.b}, cb.OnWarning, onEvidence)
	history := make([]aiflow.QaTurn, len(req.History))
	for i, turn := range req.History {
		history[i] = aiflow.QaTurn{Question: turn.Question, Answer: turn.Answer}
	}
	answer, err := (&aiflow.QAEngine{Deps: deps}).Ask(ctx, req.Question, history, cb.OnStep, cb.OnText)
	if err != nil {
		return services.AskAnswer{}, run.mapAskError(err)
	}
	return services.AskAnswer{
		Text: answer.Text, Sources: toAskSources(answer.Sources), Model: answer.Model,
		UsedFallback: answer.UsedFallback, Truncated: answer.Truncated,
	}, nil
}

func (a *askAdapter) AskArticle(ctx context.Context, req services.ArticleAskRequest, patch func(aiflow.ArticlePatch)) error {
	run, err := a.newRun(ctx)
	if err != nil {
		return err
	}
	asker := &aiflow.ArticleAsker{Runner: run.runner, Store: askArticleStore{b: a.b}}
	err = asker.Ask(ctx, aiflow.AskArticleInput{
		Target: req.Target, Question: req.Question, History: req.History,
		WebEnabled: req.WebEnabled, RequestID: req.RequestID, Context: req.Context,
	}, patch)
	return run.mapAskError(err)
}

func toAskSources(refs []aiflow.QASourceRef) []services.AskSource {
	out := make([]services.AskSource, len(refs))
	for i, ref := range refs {
		out[i] = services.AskSource{Ordinal: ref.Ordinal, Kind: string(ref.Kind), RefID: ref.RefID, Title: ref.Title, Evidence: ref.Evidence}
	}
	return out
}

// ---- 检索与读取依赖（每个方法对应渲染层的一个 IPC 调用） ----

// askItem 是 knowledge:get 里问答用到的字段；复核状态缺省时处理器已按 "clear" 给出。
type askItem struct {
	DeletedAt     *int64   `json:"deletedAt"`
	ReviewStatus  string   `json:"reviewStatus"`
	ReviewReasons []string `json:"reviewReasons"`
	UpdatedAt     *int64   `json:"updatedAt"`
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	Transcript    *string  `json:"transcript"`
}

func getAskItem(ctx context.Context, b *Bridge, id string) (*askItem, error) {
	return callAs[*askItem](ctx, b, "knowledge:get", id)
}

// askQAStore 实现 aiflow.QAStore。
type askQAStore struct{ b *Bridge }

func (s askQAStore) GetItem(ctx context.Context, id string) (*aiflow.QAKnowledgeItem, error) {
	item, err := getAskItem(ctx, s.b, id)
	if err != nil || item == nil {
		return nil, err
	}
	return &aiflow.QAKnowledgeItem{
		DeletedAt: item.DeletedAt, ReviewStatus: item.ReviewStatus, ReviewReasons: item.ReviewReasons,
		UpdatedAt: item.UpdatedAt, Title: item.Title, Content: item.Content, Transcript: item.Transcript,
	}, nil
}

func (s askQAStore) WikiCatalog(ctx context.Context) ([]aiflow.WikiCatalogEntry, error) {
	return callAs[[]aiflow.WikiCatalogEntry](ctx, s.b, "wiki:catalog")
}

func (s askQAStore) WikiSearch(ctx context.Context, query string, limit int) ([]aiflow.WikiSearchHit, error) {
	return callAs[[]aiflow.WikiSearchHit](ctx, s.b, "wiki:search", query, limit)
}

func (s askQAStore) WikiPage(ctx context.Context, id string) (*aiflow.WikiPageDetail, error) {
	return callAs[*aiflow.WikiPageDetail](ctx, s.b, "wiki:getPage", id)
}

// askHybridSource 实现 aiflow.HybridSource：FTS 召回走 knowledge:list，语义检索走 semantic:search。
type askHybridSource struct{ b *Bridge }

func (s askHybridSource) SearchRecall(ctx context.Context, query string, limit int) ([]aiflow.KnowledgeListEntry, error) {
	page, err := callAs[struct {
		Entries []struct {
			ID            string   `json:"id"`
			Title         string   `json:"title"`
			Snippet       *string  `json:"snippet"`
			ReviewStatus  string   `json:"reviewStatus"`
			ReviewReasons []string `json:"reviewReasons"`
		} `json:"entries"`
	}](ctx, s.b, "knowledge:list", map[string]any{
		"scope": "all", "search": query, "searchMode": "recall", "includeArchived": true, "limit": limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]aiflow.KnowledgeListEntry, len(page.Entries))
	for i, e := range page.Entries {
		out[i] = aiflow.KnowledgeListEntry{ID: e.ID, Title: e.Title, Snippet: e.Snippet, ReviewStatus: e.ReviewStatus, ReviewReasons: e.ReviewReasons}
	}
	return out, nil
}

func (s askHybridSource) SemanticSearch(ctx context.Context, model string, vector []float64, limit int) ([]aiflow.SemanticSearchHit, error) {
	hits, err := callAs[[]struct {
		ItemID        string   `json:"itemId"`
		Title         string   `json:"title"`
		Snippet       string   `json:"snippet"`
		Score         float64  `json:"score"`
		ReviewStatus  string   `json:"reviewStatus"`
		ReviewReasons []string `json:"reviewReasons"`
	}](ctx, s.b, "semantic:search", map[string]any{"model": model, "vector": vector, "limit": limit})
	if err != nil {
		return nil, err
	}
	out := make([]aiflow.SemanticSearchHit, len(hits))
	for i, h := range hits {
		out[i] = aiflow.SemanticSearchHit{
			ReviewStatus: h.ReviewStatus, ReviewReasons: h.ReviewReasons,
			ItemID: h.ItemID, Title: h.Title, Snippet: h.Snippet, Score: h.Score,
		}
	}
	return out, nil
}

// askArticleStore 实现 aiflow.ArticleStore。
type askArticleStore struct{ b *Bridge }

func (s askArticleStore) GetItem(ctx context.Context, itemID string) (*aiflow.ArticleItem, error) {
	item, err := getAskItem(ctx, s.b, itemID)
	if err != nil || item == nil {
		return nil, err
	}
	return &aiflow.ArticleItem{DeletedAt: item.DeletedAt, ReviewStatus: item.ReviewStatus, ReviewReasons: item.ReviewReasons}, nil
}

func (s askArticleStore) Context(ctx context.Context, target aiflow.ArticleTarget, question string) (aiflow.ArticleContextResult, error) {
	out, err := callAs[struct {
		Success bool                   `json:"success"`
		Context *aiflow.ArticleContext `json:"context"`
		Error   string                 `json:"error"`
	}](ctx, s.b, chArticleAskContext, map[string]any{"target": target, "question": question})
	return aiflow.ArticleContextResult{Success: out.Success, Context: out.Context, Error: out.Error}, err
}

func (s askArticleStore) Search(ctx context.Context, requestID string, queries []string) (aiflow.ArticleSearchResult, error) {
	out, err := callAs[struct {
		Success  bool                   `json:"success"`
		Sources  []aiflow.ArticleSource `json:"sources"`
		Warnings []string               `json:"warnings"`
		Error    string                 `json:"error"`
	}](ctx, s.b, chArticleAskSearch, map[string]any{"requestId": requestID, "queries": queries})
	return aiflow.ArticleSearchResult{Success: out.Success, Sources: out.Sources, Warnings: out.Warnings, Error: out.Error}, err
}

// CancelSearch 只是触发处理器里登记的 cancel，几乎立即返回；失败无可补救，忽略。
func (s askArticleStore) CancelSearch(requestID string) {
	_, _ = s.b.call(context.Background(), chArticleAskCancel, requestID)
}
