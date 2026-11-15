package aiflow

// 全局问答的生产依赖组装（对应 qa.ts 的 createQaDeps）：检索走 FTS + 语义混合，读取走 knowledge 与 wiki 的存取，
// 对话走 qa 场景模型。

import (
	"context"
	"sync"
)

// QAKnowledgeItem 是 knowledge.get 返回里问答用到的字段。
type QAKnowledgeItem struct {
	DeletedAt     *int64 // 非 nil 表示已删除（回收站）
	ReviewStatus  string // 空串表示未设置
	ReviewReasons []string
	UpdatedAt     *int64
	Title         string // 空串在 NewQADeps 里回落为「无标题」
	Content       string
	Transcript    *string
}

// QAStore 是全局问答依赖的存取方，每个方法对应渲染层的一个 IPC 调用；条目或页面不存在返回 nil。
type QAStore interface {
	GetItem(ctx context.Context, id string) (*QAKnowledgeItem, error)
	WikiCatalog(ctx context.Context) ([]WikiCatalogEntry, error)
	WikiSearch(ctx context.Context, query string, limit int) ([]WikiSearchHit, error)
	WikiPage(ctx context.Context, id string) (*WikiPageDetail, error)
}

// NewQADeps 对应 createQaDeps。onWarning 同一句话只回调一次：语义检索降级的提示每次检索都会产生，
// 重复提示没有信息量；onEvidence 在每次阅读之后与兜底管线组装好资料之后回调（见 QADeps.OnEvidence）。
func NewQADeps(runner *Runner, hybrid *Hybrid, store QAStore, onWarning func(message string), onEvidence func(sources []QASourceRef)) QADeps {
	var mu sync.Mutex
	warned := map[string]bool{}
	warnOnce := func(message string) {
		mu.Lock()
		seen := warned[message]
		warned[message] = true
		mu.Unlock()
		if !seen && onWarning != nil {
			onWarning(message)
		}
	}

	return QADeps{
		OnEvidence: onEvidence,
		Chat: func(ctx context.Context, messages []ChatMessage, opts ScenarioChatOptions) (ScenarioChatResult, error) {
			return runner.RunScenarioChat(ctx, ScenarioQA, messages, opts)
		},
		SearchItems: func(ctx context.Context, query string, limit int) ([]SearchHit, error) {
			return hybrid.HybridSearchItems(ctx, query, limit, warnOnce)
		},
		ReadItem: func(ctx context.Context, id string) (*QAItemContent, error) {
			item, err := store.GetItem(ctx, id)
			if err != nil || item == nil || item.DeletedAt != nil {
				return nil, err
			}
			return &QAItemContent{
				ReviewStatus: item.ReviewStatus, ReviewReasons: item.ReviewReasons, UpdatedAt: item.UpdatedAt,
				Title:   orDefault(item.Title, "无标题"),
				Content: item.Content, Transcript: item.Transcript,
			}, nil
		},
		WikiCatalog:     store.WikiCatalog,
		SearchWikiPages: store.WikiSearch,
		ReadWikiPage:    store.WikiPage,
	}
}
