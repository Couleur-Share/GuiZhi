package aiflow

// 混合检索（对应 services/knowledge-ai/hybrid-search.ts 与 packages/shared/utils/hybrid-results.ts）：
// FTS 关键词 + embedding 语义两路并发，RRF（Reciprocal Rank Fusion）融合。
// embedding 未配置或调用失败时静默退化为纯 FTS（保持既有行为）。

import (
	"context"
	"sort"
	"sync/atomic"
	"time"
)

const (
	rrfK = 60
	// semanticMinScore 语义命中的最低余弦相似度，低于此值多为噪声。
	semanticMinScore = 0.25
	// defaultSemanticTimeout：语义这一路最多等 3 秒，超时就用 FTS 的结果，不能拖慢整次检索。
	defaultSemanticTimeout = 3 * time.Second
)

// SearchHit 对应 QaSearchHit：一条检索命中。
type SearchHit struct {
	// ReviewStatus 为 "clear" | "needs_review"，空串表示没有这个字段。
	ReviewStatus string
	// ReviewReasons 为 nil 表示没有这个字段（空的非 nil 切片表示字段存在但为空）。
	ReviewReasons []string
	ID            string
	Title         string
	Snippet       string
	// MatchText 是语义检索实际命中的那段分块正文；有它才知道该读文档的哪一段。空串表示没有。
	MatchText string
}

// MergeHybridResults 对应 mergeHybridResults：RRF 融合两路排名，score(doc) = Σ 1/(k + rank)。
// 元数据（标题/摘录）优先取 FTS 结果，语义独有命中用其分块节选。
//
// matchText 单独并进来：两路都命中时元数据归 FTS，但 FTS 的 snippet 是文档开头、当不了定位线索，
// 语义那份命中片段不能跟着一起被丢掉。
func MergeHybridResults(ftsHits, semanticHits []SearchHit, limit int) []SearchHit {
	scores := map[string]float64{}
	order := []string{} // 首次出现的顺序：同分时按它排（JS 的 sort 稳定）
	meta := map[string]SearchHit{}
	matchTexts := map[string]string{}

	accumulate := func(hits []SearchHit, preferMeta bool) {
		for rank, h := range hits {
			if _, seen := scores[h.ID]; !seen {
				order = append(order, h.ID)
			}
			scores[h.ID] += 1 / float64(rrfK+rank+1)
			if _, has := meta[h.ID]; preferMeta || !has {
				meta[h.ID] = h
			}
			if h.MatchText != "" {
				if _, has := matchTexts[h.ID]; !has {
					matchTexts[h.ID] = h.MatchText
				}
			}
		}
	}
	accumulate(ftsHits, true)
	accumulate(semanticHits, false)

	sort.SliceStable(order, func(i, j int) bool { return scores[order[i]] > scores[order[j]] })
	keep := limit
	if keep < 1 {
		keep = 1
	}
	if keep > len(order) {
		keep = len(order)
	}

	out := make([]SearchHit, 0, keep)
	for _, id := range order[:keep] {
		h := meta[id]
		if matchText := matchTexts[id]; matchText != "" {
			h.MatchText = matchText
		}
		out = append(out, h)
	}
	return out
}

// KnowledgeListEntry 是 FTS 检索返回的一条（对应 window.api.knowledge.list 的 entries 里用到的字段）。
type KnowledgeListEntry struct {
	ID            string
	ReviewStatus  string
	ReviewReasons []string
	Title         string
	// Snippet 为 nil 表示没有摘录。
	Snippet *string
}

// SemanticSearchHit 对应 SemanticSearchHit：语义检索按条目聚合后的命中（条目内取最高分块）。
type SemanticSearchHit struct {
	ReviewStatus  string
	ReviewReasons []string
	ItemID        string
	Title         string
	// Snippet 是命中分块的文本节选。
	Snippet string
	// Score 是余弦相似度（向量已归一化，即点积）。
	Score float64
}

// HybridSource 是混合检索依赖的两个检索入口（对应 window.api.knowledge.list 与 window.api.semantic.search）。
// 取消走 ctx；生产实现在主包 native_aiflow.go，直接调对应的 Go 处理器。
type HybridSource interface {
	// SearchRecall 对应 knowledge.list({scope:"all", search, searchMode:"recall", includeArchived:true, limit})。
	// 自然语言问句走默认的 phrase 模式会被编译成一个逐字相邻的长 phrase，中文没有空格，结果必然是零命中，所以用 recall。
	SearchRecall(ctx context.Context, query string, limit int) ([]KnowledgeListEntry, error)
	// SemanticSearch 对应 semantic.search：用查询向量做余弦 top-k。
	SemanticSearch(ctx context.Context, model string, vector []float64, limit int) ([]SemanticSearchHit, error)
}

// Hybrid 是混合检索入口。
type Hybrid struct {
	Runner  *Runner
	Source  HybridSource
	Vectors *QueryVectors
	// SemanticTimeout 为 0 时取 3 秒；测试可缩短。
	SemanticTimeout time.Duration
}

func (h *Hybrid) timeout() time.Duration {
	if h.SemanticTimeout > 0 {
		return h.SemanticTimeout
	}
	return defaultSemanticTimeout
}

type hybridResult struct {
	hits []SearchHit
	err  error
}

// abortable 对应 abortable：取消只结束当前等待，底层调用由发起方负责中止（这里 fn 自己也看 ctx，只是不依赖它）。
func abortable[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := fn()
		done <- result{value, err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		var zero T
		return zero, ErrAborted
	}
}

// HybridSearchItems 对应 hybridSearchItems：QA 检索入口，两路并发，RRF 融合。
// onWarning 在语义这一路失败或超时时回调（一句给用户看的话）；它可能在后台协程里被调用，但一定先于本函数返回。
func (h *Hybrid) HybridSearchItems(ctx context.Context, query string, limit int, onWarning func(message string)) ([]SearchHit, error) {
	if ctx.Err() != nil {
		return nil, ErrAborted
	}
	// 任一路先失败时，取消另一路（其结果已经没人要了）
	runCtx, cancelRun := context.WithCancel(ctx)

	ftsCh := make(chan hybridResult, 1)
	semCh := make(chan hybridResult, 1)
	go func() {
		hits, err := h.searchByFTS(runCtx, query, limit)
		ftsCh <- hybridResult{hits, err}
	}()
	go func() {
		hits, err := h.searchBySemantic(runCtx, ctx, query, limit, onWarning)
		semCh <- hybridResult{hits, err}
	}()

	semReceived := false
	defer func() {
		cancelRun()
		// 语义协程只依赖 ctx，取消后很快返回；等它收尾是为了保证 onWarning 不会在本函数返回之后才被调用
		if !semReceived {
			<-semCh
		}
	}()

	var ftsHits, semanticHits []SearchHit
	for received := 0; received < 2; {
		select {
		case r := <-ftsCh:
			if r.err != nil {
				return nil, r.err
			}
			ftsHits = r.hits
			received++
		case r := <-semCh:
			semReceived = true
			if r.err != nil {
				return nil, r.err
			}
			semanticHits = r.hits
			received++
		case <-ctx.Done():
			return nil, ErrAborted
		}
	}
	if ctx.Err() != nil {
		return nil, ErrAborted
	}
	if len(semanticHits) == 0 {
		return ftsHits, nil
	}
	return MergeHybridResults(ftsHits, semanticHits, limit), nil
}

func (h *Hybrid) searchByFTS(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	entries, err := h.Source.SearchRecall(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, len(entries))
	for i, entry := range entries {
		hit := SearchHit{ID: entry.ID, ReviewStatus: entry.ReviewStatus, ReviewReasons: entry.ReviewReasons, Title: entry.Title}
		if hit.Title == "" {
			hit.Title = "无标题"
		}
		if entry.Snippet != nil {
			hit.Snippet = *entry.Snippet
		}
		hits[i] = hit
	}
	return hits, nil
}

// searchBySemantic：user 是调用方的 ctx（用来区分「用户取消」与「超时/内部失败」），parent 是并发组的 ctx。
func (h *Hybrid) searchBySemantic(parent, user context.Context, query string, limit int, onWarning func(string)) ([]SearchHit, error) {
	cfg, ok := h.Runner.Config.AISettings().ResolveEmbeddingConfig()
	if !ok {
		return nil, nil
	}
	semCtx, cancel := context.WithCancel(parent)
	var timedOut atomic.Bool
	timer := time.AfterFunc(h.timeout(), func() {
		timedOut.Store(true)
		cancel()
	})
	defer func() {
		timer.Stop()
		cancel()
	}()

	// catch 块：用户取消向上抛；其余一律退化成「没有语义命中」并提示一句
	degrade := func() ([]SearchHit, error) {
		if user.Err() != nil {
			return nil, ErrAborted
		}
		if parent.Err() != nil {
			// 并发组已经收尾（FTS 那一路先失败）：这一路的结果没人要了，也不必再提示
			return nil, nil
		}
		if onWarning != nil {
			if timedOut.Load() {
				onWarning("语义检索等待超过 3 秒，本次使用本地关键词结果。")
			} else {
				onWarning("语义检索暂不可用，本次使用本地关键词结果。")
			}
		}
		return nil, nil
	}

	if user.Err() != nil {
		return nil, ErrAborted
	}
	vector, err := h.Vectors.Get(semCtx, h.Runner, cfg, query)
	if err != nil {
		return degrade()
	}
	hits, err := abortable(semCtx, func() ([]SemanticSearchHit, error) {
		return h.Source.SemanticSearch(semCtx, cfg.Model, vector, limit)
	})
	if err != nil {
		return degrade()
	}
	if user.Err() != nil {
		return nil, ErrAborted
	}

	out := make([]SearchHit, 0, len(hits))
	for _, hit := range hits {
		if !(hit.Score >= semanticMinScore) { // NaN 一并排除
			continue
		}
		title := hit.Title
		if title == "" {
			title = "无标题"
		}
		out = append(out, SearchHit{
			ID: hit.ItemID, ReviewStatus: hit.ReviewStatus, ReviewReasons: hit.ReviewReasons,
			Title: title, Snippet: hit.Snippet, MatchText: hit.Snippet,
		})
	}
	return out, nil
}
