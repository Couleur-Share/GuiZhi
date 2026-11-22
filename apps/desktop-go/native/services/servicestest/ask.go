package servicestest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// AskFake 是 services.Ask 的假实现：会话存在内存里（保存、分页、筛选、置顶与证据脱敏的可见效果与真实处理器一致），
// 问答的执行由脚本驱动（见 ask_run.go：可推送步骤与流式文本、可卡住、可取消、可注入错误），并记录每次调用。
// 零值可用：问答模型与联网搜索都算已配置，没有脚本时回答一句回显。
//
// 假实现不含任何给用户看的文案：标题、回答、错误原因都由测试摆放或是英文占位。
type AskFake struct {
	mu       sync.Mutex
	clock    int64
	sessions map[string]*askRow
	items    map[string]services.AskItem

	qaOff, webOff bool
	failOps       map[string]error
	failSaves     int
	failSavesErr  error
	saveGate      chan struct{}

	queries  []services.AskSessionQuery
	lists    []services.AskSessionFilter
	gets     []string
	saves    []services.SaveAskSessionInput
	deletes  []string
	metas    []AskMetaCall
	sanitize []SanitizeCall

	run askRunState
}

// AskMetaCall 是一次 UpdateSessionMeta 调用记录。
type AskMetaCall struct {
	ID    string
	Patch services.AskSessionMetaPatch
}

// SanitizeCall 是一次 SanitizeConversation 调用记录。
type SanitizeCall struct {
	Cleared []string
}

// askRow 是库里的一行：custom 是改过的标题（处理器里的 ask_session_meta.custom_title），读出时优先于会话标题、保存不会覆盖它。
type askRow struct {
	rec    services.AskSessionRecord
	custom string
}

func (r *askRow) view() services.AskSessionRecord {
	rec := r.rec
	if r.custom != "" {
		rec.Title = r.custom
	}
	return rec
}

var _ services.Ask = (*AskFake)(nil)

// 可注入失败的操作名（Fail 的参数）。
const (
	AskOpQuery         = "query"
	AskOpList          = "list"
	AskOpGet           = "get"
	AskOpSave          = "save"
	AskOpDelete        = "delete"
	AskOpMeta          = "meta"
	AskOpQAConfigured  = "qaConfigured"
	AskOpWebConfigured = "webConfigured"
	AskOpItemInfo      = "itemInfo"
	AskOpSanitize      = "sanitize"
)

func (f *AskFake) with(fn func()) {
	f.mu.Lock()
	fn()
	f.mu.Unlock()
}

func (f *AskFake) tick() int64 {
	f.clock += 1000
	return f.clock
}

// Fail 让某个操作持续失败（err 为 nil 取消）；失败时不改任何状态，调用仍会被记录。
func (f *AskFake) Fail(op string, err error) {
	f.with(func() {
		if f.failOps == nil {
			f.failOps = map[string]error{}
		}
		if err == nil {
			delete(f.failOps, op)
			return
		}
		f.failOps[op] = err
	})
}

// FailSaves 让接下来的 n 次保存失败（之后恢复成功）：用来验证「保存失败、重试保存」。
func (f *AskFake) FailSaves(n int, err error) {
	f.with(func() { f.failSaves, f.failSavesErr = n, err })
}

// HoldSave 让下一次 SaveSession 卡住（在写入之前），直到返回的函数被调用（只卡一次）。
func (f *AskFake) HoldSave() (release func()) {
	gate := make(chan struct{})
	f.with(func() { f.saveGate = gate })
	return func() { close(gate) }
}

// SetQAConfigured 设定问答场景是否已配置模型（默认已配置）。
func (f *AskFake) SetQAConfigured(on bool) { f.with(func() { f.qaOff = !on }) }

// SetWebSearchConfigured 设定联网搜索是否已配置（默认已配置）。
func (f *AskFake) SetWebSearchConfigured(on bool) { f.with(func() { f.webOff = !on }) }

// PutItemTitle 摆好一篇条目的标题（ItemInfo 读它）；不摆的条目按不存在处理。
func (f *AskFake) PutItemTitle(id, title string) { f.PutItem(id, services.AskItem{Title: title}) }

// PutItem 摆好一篇条目（已删除的条目 Deleted 为 true，仍读得到）。
func (f *AskFake) PutItem(id string, item services.AskItem) {
	f.with(func() {
		if f.items == nil {
			f.items = map[string]services.AskItem{}
		}
		f.items[id] = item
	})
}

// PutSession 直接摆一条会话（不记为一次保存）；UpdatedAt 为 0 时取内部递增时钟。返回落到库里的记录。
func (f *AskFake) PutSession(rec services.AskSessionRecord) services.AskSessionRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec.UpdatedAt == 0 {
		rec.UpdatedAt = f.tick()
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = rec.UpdatedAt
	}
	f.putLocked(rec)
	return rec
}

func (f *AskFake) putLocked(rec services.AskSessionRecord) {
	if f.sessions == nil {
		f.sessions = map[string]*askRow{}
	}
	f.sessions[rec.ID] = &askRow{rec: rec}
}

// Session 读出一条会话（不记录调用）；没有返回 false。
func (f *AskFake) Session(id string) (rec services.AskSessionRecord, ok bool) {
	f.with(func() {
		if row := f.sessions[id]; row != nil {
			rec, ok = row.view(), true
		}
	})
	return rec, ok
}

// SessionCount 是库里的会话条数。
func (f *AskFake) SessionCount() (n int) { f.with(func() { n = len(f.sessions) }); return n }

// Saves 是收到的保存请求（含被注入失败拦下的），按发生顺序。
func (f *AskFake) Saves() (out []services.SaveAskSessionInput) {
	f.with(func() { out = slices.Clone(f.saves) })
	return out
}

// Deletes 是收到的删除请求（会话 ID），按发生顺序。
func (f *AskFake) Deletes() (out []string) {
	f.with(func() { out = slices.Clone(f.deletes) })
	return out
}

// Queries 是收到的分页查询，按发生顺序。
func (f *AskFake) Queries() (out []services.AskSessionQuery) {
	f.with(func() { out = slices.Clone(f.queries) })
	return out
}

// Lists 是收到的列表筛选，按发生顺序。
func (f *AskFake) Lists() (out []services.AskSessionFilter) {
	f.with(func() { out = slices.Clone(f.lists) })
	return out
}

// Gets 是读取过的会话 ID，按发生顺序。
func (f *AskFake) Gets() (out []string) { f.with(func() { out = slices.Clone(f.gets) }); return out }

// Metas 是收到的改标题 / 置顶请求，按发生顺序。
func (f *AskFake) Metas() (out []AskMetaCall) {
	f.with(func() { out = slices.Clone(f.metas) })
	return out
}

// Sanitizes 是收到的证据脱敏请求，按发生顺序。
func (f *AskFake) Sanitizes() (out []SanitizeCall) {
	f.with(func() { out = slices.Clone(f.sanitize) })
	return out
}

func (f *AskFake) failure(op string) error { return f.failOps[op] }

// ---- 会话 ----

func matchesFilter(m services.AskSessionMeta, scope, itemID string) bool {
	if scope != "" {
		got := m.Scope
		if got == "" {
			got = services.AskScopeKnowledge
		}
		if got != scope {
			return false
		}
	}
	return itemID == "" || m.ItemID == itemID
}

func (f *AskFake) ordered() []services.AskSessionRecord {
	rows := make([]services.AskSessionRecord, 0, len(f.sessions))
	for _, row := range f.sessions {
		rows = append(rows, row.view())
	}
	slices.SortFunc(rows, func(a, b services.AskSessionRecord) int {
		switch {
		case a.Pinned != b.Pinned:
			if a.Pinned {
				return -1
			}
			return 1
		case a.UpdatedAt != b.UpdatedAt:
			if a.UpdatedAt > b.UpdatedAt {
				return -1
			}
			return 1
		}
		return strings.Compare(b.ID, a.ID)
	})
	return rows
}

func queryFingerprint(q services.AskSessionQuery) string {
	from, to := int64(-1), int64(-1)
	if q.From != nil {
		from = *q.From
	}
	if q.To != nil {
		to = *q.To
	}
	return fmt.Sprintf("%s|%s|%s|%d|%d", q.Scope, q.ItemID, strings.TrimSpace(q.Search), from, to)
}

// QuerySessions 按置顶、更新时间倒序分页；游标是「筛选指纹 + 偏移」，筛选变了再拿旧游标会报错（与处理器一致）。
func (f *AskFake) QuerySessions(_ context.Context, q services.AskSessionQuery) (services.AskSessionPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if err := f.failure(AskOpQuery); err != nil {
		return services.AskSessionPage{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 500)
	fp := queryFingerprint(q)
	offset := 0
	if q.Cursor != nil && *q.Cursor != "" {
		head, tail, ok := strings.Cut(*q.Cursor, "#")
		n, err := strconv.Atoi(tail)
		if !ok || err != nil || head != fp {
			return services.AskSessionPage{}, errors.New("session filter changed, reload from the first page")
		}
		offset = n
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	var matched []services.AskSessionMeta
	for _, rec := range f.ordered() {
		if !matchesFilter(rec.AskSessionMeta, q.Scope, q.ItemID) {
			continue
		}
		if q.From != nil && rec.UpdatedAt < *q.From || q.To != nil && rec.UpdatedAt > *q.To {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(rec.Title+"\n"+rec.MessagesJSON), search) {
			continue
		}
		matched = append(matched, rec.AskSessionMeta)
	}
	page := services.AskSessionPage{Entries: []services.AskSessionMeta{}}
	if offset < len(matched) {
		end := min(offset+limit, len(matched))
		page.Entries = slices.Clone(matched[offset:end])
		if end < len(matched) {
			next := fp + "#" + strconv.Itoa(end)
			page.NextCursor = &next
		}
	}
	if q.Cursor == nil || *q.Cursor == "" {
		page.Articles = f.articlesLocked()
	}
	return page, nil
}

// articlesLocked 是每篇文章最近一次会话的 {文章 ID, 标题}，按标题排序。
func (f *AskFake) articlesLocked() []services.AskArticleRef {
	seen := map[string]bool{}
	var out []services.AskArticleRef
	for _, rec := range f.ordered() {
		if !rec.IsArticle() || rec.ItemID == "" || seen[rec.ItemID] {
			continue
		}
		seen[rec.ItemID] = true
		out = append(out, services.AskArticleRef{ID: rec.ItemID, Title: rec.ArticleTitle})
	}
	slices.SortFunc(out, func(a, b services.AskArticleRef) int {
		return strings.Compare(a.Title+"\x00"+a.ID, b.Title+"\x00"+b.ID)
	})
	return out
}

// ListSessions 是最近 100 条（带筛选）。
func (f *AskFake) ListSessions(_ context.Context, filter services.AskSessionFilter) ([]services.AskSessionMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists = append(f.lists, filter)
	if err := f.failure(AskOpList); err != nil {
		return nil, err
	}
	out := []services.AskSessionMeta{}
	for _, rec := range f.ordered() {
		if matchesFilter(rec.AskSessionMeta, filter.Scope, filter.ItemID) && len(out) < 100 {
			out = append(out, rec.AskSessionMeta)
		}
	}
	return out, nil
}

// GetSession 读一条会话；不存在返回 nil。
func (f *AskFake) GetSession(_ context.Context, id string) (*services.AskSessionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, id)
	if err := f.failure(AskOpGet); err != nil {
		return nil, err
	}
	row := f.sessions[id]
	if row == nil {
		return nil, nil
	}
	rec := row.view()
	return &rec, nil
}

// SaveSession 写入（或覆盖）一条会话：创建时间保留，更新时间取递增时钟；围绕本文的会话必须带匹配的文章关联。
func (f *AskFake) SaveSession(ctx context.Context, in services.SaveAskSessionInput) (services.AskSessionRecord, error) {
	f.mu.Lock()
	f.saves = append(f.saves, in)
	gate := f.saveGate
	f.saveGate = nil
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return services.AskSessionRecord{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSaves > 0 {
		f.failSaves--
		return services.AskSessionRecord{}, f.failSavesErr
	}
	if err := f.failure(AskOpSave); err != nil {
		return services.AskSessionRecord{}, err
	}
	if in.Scope == services.AskScopeArticle && (in.ItemID == "" || in.Target == nil || in.Target.ItemID != in.ItemID) {
		return services.AskSessionRecord{}, errors.New("article session needs a matching article")
	}
	now := f.tick()
	rec := services.AskSessionRecord{MessagesJSON: in.MessagesJSON}
	rec.ID, rec.Title, rec.CreatedAt, rec.UpdatedAt = in.ID, in.Title, now, now
	if old := f.sessions[in.ID]; old != nil {
		rec.CreatedAt, rec.Pinned = old.rec.CreatedAt, old.rec.Pinned
	}
	if in.Scope == services.AskScopeArticle {
		web := in.WebEnabled == nil || *in.WebEnabled
		rec.Scope, rec.ItemID, rec.ArticleTitle, rec.WebEnabled, rec.Target = in.Scope, in.ItemID, in.ArticleTitle, &web, in.Target
	}
	custom := ""
	if old := f.sessions[in.ID]; old != nil {
		custom = old.custom
	}
	f.putLocked(rec)
	f.sessions[in.ID].custom = custom
	out := rec
	if custom != "" {
		out.Title = custom
	}
	return out, nil
}

// DeleteSession 删一条会话；返回是否真的删掉了。
func (f *AskFake) DeleteSession(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, id)
	if err := f.failure(AskOpDelete); err != nil {
		return false, err
	}
	_, ok := f.sessions[id]
	delete(f.sessions, id)
	return ok, nil
}

// UpdateSessionMeta 改标题（1–200 字）与置顶。
func (f *AskFake) UpdateSessionMeta(_ context.Context, id string, patch services.AskSessionMetaPatch) (services.AskSessionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metas = append(f.metas, AskMetaCall{ID: id, Patch: patch})
	if err := f.failure(AskOpMeta); err != nil {
		return services.AskSessionRecord{}, err
	}
	row := f.sessions[id]
	if row == nil {
		return services.AskSessionRecord{}, errors.New("session not found")
	}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" || len([]rune(title)) > 200 {
			return services.AskSessionRecord{}, errors.New("session title must be 1-200 characters")
		}
		row.custom = title
	}
	if patch.Pinned != nil {
		row.rec.Pinned = *patch.Pinned
	}
	return row.view(), nil
}

// ---- 配置与条目 ----

func (f *AskFake) QAConfigured(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(AskOpQAConfigured); err != nil {
		return false, err
	}
	return !f.qaOff, nil
}

func (f *AskFake) WebSearchConfigured(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(AskOpWebConfigured); err != nil {
		return false, err
	}
	return !f.webOff, nil
}

func (f *AskFake) ItemInfo(_ context.Context, itemID string) (services.AskItem, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(AskOpItemInfo); err != nil {
		return services.AskItem{}, false, err
	}
	item, ok := f.items[itemID]
	return item, ok, nil
}

// SanitizeConversation 是处理器里 sanitizeEvidenceJson 的简化版：来源（条目引用、证据快照或文章目标）指向已清除的条目时，
// 换成只留序号与类别的占位（标题 "cleared"、证据正文清空、cleared 为真）；回答文本不动。
func (f *AskFake) SanitizeConversation(messagesJSON string, cleared []string) (string, error) {
	f.mu.Lock()
	f.sanitize = append(f.sanitize, SanitizeCall{Cleared: slices.Clone(cleared)})
	err := f.failure(AskOpSanitize)
	f.mu.Unlock()
	if err != nil {
		return "", err
	}
	var messages []map[string]any
	if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
		return "", errors.New("conversation format is invalid")
	}
	set := map[string]bool{}
	for _, id := range cleared {
		set[id] = true
	}
	for _, message := range messages {
		for _, key := range []string{"sources", "evidenceSources"} {
			message[key] = clearSources(message[key], set)
		}
		if ctx, ok := message["context"].(map[string]any); ok {
			ctx["sources"] = clearSources(ctx["sources"], set)
		}
	}
	out, err := json.Marshal(messages)
	return string(out), err
}

func clearSources(raw any, cleared map[string]bool) any {
	list, ok := raw.([]any)
	if !ok {
		return raw
	}
	out := make([]any, len(list))
	for i, entry := range list {
		src, _ := entry.(map[string]any)
		if src == nil || !sourceCleared(src, cleared) {
			out[i] = entry
			continue
		}
		stub := map[string]any{"ordinal": src["ordinal"], "kind": src["kind"], "refId": src["refId"], "title": "cleared", "cleared": true}
		if ev, ok := src["evidence"].(map[string]any); ok {
			stub["evidence"] = map[string]any{"version": 1, "kind": ev["kind"], "title": "cleared", "text": "", "fingerprint": "",
				"capturedAt": ev["capturedAt"], "reviewStatus": "clear", "reviewReasons": []any{}, "cleared": true}
		}
		out[i] = stub
	}
	return out
}

func sourceCleared(src map[string]any, cleared map[string]bool) bool {
	if id, _ := src["refId"].(string); src["kind"] == "item" && cleared[id] {
		return true
	}
	if target, ok := src["target"].(map[string]any); ok {
		if id, _ := target["itemId"].(string); cleared[id] {
			return true
		}
	}
	ev, _ := src["evidence"].(map[string]any)
	if ev == nil {
		return false
	}
	if id, _ := ev["sourceId"].(string); cleared[id] {
		return true
	}
	ids, _ := ev["sourceItemIds"].([]any)
	for _, raw := range ids {
		if id, _ := raw.(string); cleared[id] {
			return true
		}
	}
	return false
}
