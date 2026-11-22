package ask

import (
	"context"
	"slices"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// historyPageSize 是历史列表每页条数。
const historyPageSize = 50

// HistoryView 是历史会话列表每帧读取的快照（写时复制）。
type HistoryView struct {
	// Articles 是「关联文章」筛选下拉的候选，只在第一页返回；翻页或刷新时沿用上一次的。
	Articles []services.AskArticleRef
	Entries  []services.AskSessionMeta
	Query    services.AskSessionQuery
	// HasMore 为 true 表示还有更早的会话（Load(true) 加载下一页）。
	HasMore bool
	Loading bool
	// Error 非空时界面显示错误并给出重试（Load(false)）。
	Error string
}

// HistoryStore 是历史会话列表（ask-history.store.ts）：全文搜索、范围 / 时间 / 文章筛选、游标翻页、改标题与置顶。
// 全局问答的历史面板与围绕本文的「本文历史」各用一个实例，互不影响筛选条件。
type HistoryStore struct {
	svc        services.Ask
	guard      kit.Guard
	invalidate func()
	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context

	mu   sync.Mutex
	view HistoryView
	// next 是下一页游标（nil 表示没有更多）。
	next *string
	// generation 让过时的响应失效：筛选条件一变，之前发出的请求即使晚到也不能覆盖新结果。
	generation int
}

// NewHistoryStore 创建历史列表状态。
func NewHistoryStore(svc services.Ask, guard kit.Guard, invalidate func()) *HistoryStore {
	return &HistoryStore{svc: svc, guard: guard, invalidate: invalidate}
}

// View 返回当前快照。
func (h *HistoryStore) View() HistoryView {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.view
}

func (h *HistoryStore) wake() {
	if h.invalidate != nil {
		h.invalidate()
	}
}

func (h *HistoryStore) ctx() context.Context {
	if h.Ctx != nil {
		return h.Ctx
	}
	return context.Background()
}

// Filter 换一组筛选条件并从第一页重新加载（清空旧结果，之前发出的请求作废）。
func (h *HistoryStore) Filter(query services.AskSessionQuery) {
	h.mu.Lock()
	h.generation++
	h.view.Query, h.view.Entries, h.view.HasMore, h.view.Loading = query, nil, false, false
	h.next = nil
	h.mu.Unlock()
	h.wake()
	h.Load(false)
}

// Load 加载历史。more 为 false 时重读第一页（并补齐已经翻开的条数，刷新后不会「缩回去」）；为 true 时加载下一页，
// 正在加载或没有更多时忽略。同一时刻只认最后发出的那一次请求。
func (h *HistoryStore) Load(more bool) {
	h.mu.Lock()
	if more && (h.view.Loading || h.next == nil) {
		h.mu.Unlock()
		return
	}
	h.generation++
	req := h.generation
	input := h.view.Query
	input.Limit = historyPageSize
	input.Cursor = nil
	if more {
		input.Cursor = copyOf(h.next)
	}
	h.view.Loading, h.view.Error = true, ""
	h.mu.Unlock()
	h.wake()

	result, err := h.svc.QuerySessions(h.ctx(), input)
	if err == nil && !more {
		h.mu.Lock()
		target := len(h.view.Entries)
		h.mu.Unlock()
		for result.NextCursor != nil && len(result.Entries) < target {
			page := input
			page.Cursor = result.NextCursor
			var nextPage services.AskSessionPage
			if nextPage, err = h.svc.QuerySessions(h.ctx(), page); err != nil {
				break
			}
			if !h.isCurrent(req) {
				return
			}
			result.Entries = append(slices.Clone(result.Entries), nextPage.Entries...)
			result.NextCursor = nextPage.NextCursor
		}
	}

	h.mu.Lock()
	defer func() {
		h.mu.Unlock()
		h.wake()
	}()
	if req != h.generation {
		return // 已被更新的请求取代：它负责收尾（含 Loading）
	}
	h.view.Loading = false
	if err != nil {
		h.view.Error = err.Error()
		return
	}
	if result.Articles != nil {
		h.view.Articles = result.Articles
	}
	if more {
		merged := slices.Clone(h.view.Entries)
		for _, e := range result.Entries {
			if !slices.ContainsFunc(merged, func(old services.AskSessionMeta) bool { return old.ID == e.ID }) {
				merged = append(merged, e)
			}
		}
		h.view.Entries = merged
	} else {
		h.view.Entries = result.Entries
	}
	h.next = result.NextCursor
	h.view.HasMore = result.NextCursor != nil
}

func (h *HistoryStore) isCurrent(req int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return req == h.generation
}

// Update 改会话标题或置顶（nil 的字段不改），成功后重读列表。失败经 kit.Guard 弹出「更新会话失败」，返回 false。
func (h *HistoryStore) Update(id string, patch services.AskSessionMetaPatch) bool {
	return h.guard.Run(locales.AskActionUpdateSession(), func() error {
		if _, err := h.svc.UpdateSessionMeta(h.ctx(), id, patch); err != nil {
			return err
		}
		h.Load(false)
		return nil
	})
}
