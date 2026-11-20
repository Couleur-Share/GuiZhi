package servicestest

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"guizhi-mygo-shell/native/services"
)

//go:embed generated-entries.json
var generatedEntriesJSON []byte

// generatedFiller 是 GenerateEntries 的填充文字：基准与对照用的测试数据，不是界面文案，
// 放在 JSON 里是因为 nocjk 门禁不允许 Go 源码里出现中文字面量。
type generatedFiller struct {
	Tags      []services.LibraryTag `json:"tags"`
	Title     string                `json:"title"`
	LongTitle string                `json:"longTitle"`
	Snippet   string                `json:"snippet"`
}

func loadGeneratedFiller() generatedFiller {
	var filler generatedFiller
	if err := json.Unmarshal(generatedEntriesJSON, &filler); err != nil {
		panic(err) // 嵌入的常量解码失败是程序错误
	}
	return filler
}

// 条目列表的假实现：在 SeedEntries 给的全集上做筛选、排序与翻页，规则取自真实处理器（knowledge_list.go）里
// 与界面行为有关的那部分——范围的含义、置顶优先、排序字段与方向、搜索时按相关度（这里取全集里的先后）、
// 游标绑定查询条件（条件变了就拒绝）。全文检索（bm25）、摘要的截取不在这里模拟。

// errStaleCursor 与真实处理器的错误同文：换了筛选之后拿旧游标去取下一页。
var errStaleCursor = errors.New("知识列表游标无效或已过期")

// defaultListLimit 是没给 Limit 时的页大小。
const defaultListLimit = 50

// SeedEntries 设置条目全集（拷贝一份）；返回自己以便链式书写。全集的先后是「相关度」的顺序（搜索时按它排）。
func (f *Library) SeedEntries(entries []services.LibraryEntry) *Library {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = slices.Clone(entries)
	return f
}

// HoldList 让下一次 List 卡住，直到返回的函数被调用（只卡一次），用来构造「旧响应晚于新响应」的竞态。
func (f *Library) HoldList() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.listGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// ListQueries 返回迄今收到的列表查询，按发生顺序。
func (f *Library) ListQueries() []services.LibraryListQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.listQueries)
}

// fingerprint 是游标绑定的查询条件：不含分页参数。
func fingerprint(q services.LibraryListQuery) string {
	sortBy, order := q.SortBy, q.SortOrder
	if sortBy == "" {
		sortBy = services.LibrarySortUpdatedAt
	}
	if order == "" {
		order = services.LibraryOrderDesc
	}
	return strings.Join([]string{q.Scope, q.CollectionID, q.TagID, q.Platform, strings.TrimSpace(q.Search), sortBy, order}, "\x1f")
}

// inScope 是范围的含义（真实处理器的 WHERE）：回收站只看已删除的，其余范围排除已删除的；
// 「全部」不含归档，「未归合集」不含归档，「收藏」含归档的收藏。
func inScope(e services.LibraryEntry, scope string) bool {
	deleted := e.DeletedAt != 0
	switch scope {
	case services.LibraryScopeTrash:
		return deleted
	case services.LibraryScopeUncategorized:
		return !deleted && e.CollectionID == "" && e.Status != services.LibraryStatusArchived
	case services.LibraryScopeFavorites:
		return !deleted && e.IsFavorite
	case services.LibraryScopeArchived:
		return !deleted && e.Status == services.LibraryStatusArchived
	default:
		return !deleted && e.Status != services.LibraryStatusArchived
	}
}

func hasTag(e services.LibraryEntry, id string) bool {
	for _, t := range e.Tags {
		if t.ID == id {
			return true
		}
	}
	return false
}

// matches 报告条目是否符合查询的全部条件。
func matches(e services.LibraryEntry, q services.LibraryListQuery) bool {
	if !inScope(e, q.Scope) {
		return false
	}
	if q.CollectionID != "" && e.CollectionID != q.CollectionID {
		return false
	}
	if q.TagID != "" && !hasTag(e, q.TagID) {
		return false
	}
	if q.Platform != "" && e.Platform != q.Platform {
		return false
	}
	if needle := strings.ToLower(strings.TrimSpace(q.Search)); needle != "" {
		return strings.Contains(strings.ToLower(e.Title), needle) || strings.Contains(strings.ToLower(e.Snippet), needle)
	}
	return true
}

// less 是无搜索时的排序：置顶在前；然后按字段与方向；相同时更新时间新的在前，再按 ID 定序。
func less(a, b services.LibraryEntry, sortBy, order string) bool {
	if a.IsPinned != b.IsPinned {
		return a.IsPinned
	}
	var cmp int
	switch sortBy {
	case services.LibrarySortCreatedAt:
		cmp = compareInt(a.CreatedAt, b.CreatedAt)
	case services.LibrarySortTitle:
		cmp = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)) // COLLATE NOCASE
	default:
		cmp = compareInt(a.UpdatedAt, b.UpdatedAt)
	}
	if order != services.LibraryOrderAsc {
		cmp = -cmp
	}
	if cmp != 0 {
		return cmp < 0
	}
	if a.UpdatedAt != b.UpdatedAt {
		return a.UpdatedAt > b.UpdatedAt
	}
	return a.ID < b.ID
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// encodeCursor 的内容只有指纹与下一页的起点；界面不应解析它。
func encodeCursor(fp string, next int) string { return "c\x1f" + strconv.Itoa(next) + "\x1f" + fp }

func decodeCursor(cursor, fp string) (int, error) {
	parts := strings.SplitN(cursor, "\x1f", 3)
	if len(parts) != 3 || parts[0] != "c" || parts[2] != fp {
		return 0, errStaleCursor
	}
	next, err := strconv.Atoi(parts[1])
	if err != nil || next < 0 {
		return 0, errStaleCursor
	}
	return next, nil
}

// orderedHits 是查询的全部命中，按列表的顺序排好（List 与 Freeze 共用）：无搜索时按排序字段，搜索时保持全集里的先后（相关度）。
func orderedHits(all []services.LibraryEntry, q services.LibraryListQuery) []services.LibraryEntry {
	var hits []services.LibraryEntry
	for _, e := range all {
		if matches(e, q) {
			hits = append(hits, e)
		}
	}
	if strings.TrimSpace(q.Search) == "" {
		sortBy, order := q.SortBy, q.SortOrder
		slices.SortStableFunc(hits, func(a, b services.LibraryEntry) int {
			switch {
			case less(a, b, sortBy, order):
				return -1
			case less(b, a, sortBy, order):
				return 1
			}
			return 0
		})
	}
	return hits
}

// List 实现 services.Library。
func (f *Library) List(ctx context.Context, q services.LibraryListQuery) (services.LibraryPage, error) {
	f.mu.Lock()
	f.listQueries = append(f.listQueries, q)
	f.calls = append(f.calls, "List")
	gate, fail, all := f.listGate, f.failures["List"], f.entries
	f.listGate = nil // 只卡下一次
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return services.LibraryPage{}, ctx.Err()
		}
	}
	if fail != nil {
		return services.LibraryPage{}, fail
	}

	fp := fingerprint(q)
	start := q.Offset
	if q.Cursor != "" {
		next, err := decodeCursor(q.Cursor, fp)
		if err != nil {
			return services.LibraryPage{}, err
		}
		start = next
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}

	hits := orderedHits(all, q)

	page := services.LibraryPage{Total: len(hits), Entries: []services.LibraryEntry{}}
	if start < len(hits) {
		end := min(start+limit, len(hits))
		page.Entries = slices.Clone(hits[start:end])
		if end < len(hits) {
			page.NextCursor = encodeCursor(fp, end)
		}
	}
	return page, nil
}

// GenerateEntries 生成 n 条确定性的条目（对照与基准用）：标题长短不一（含会折成两行的长标题与空标题），
// 约 1/50 置顶、1/17 收藏，类型轮换，约 1/9 带来源平台，约 1/5 归档，约 1/20 在回收站；更新时间递减。
func GenerateEntries(n int) []services.LibraryEntry {
	types := []string{
		services.LibraryTypeNote, services.LibraryTypeWebpage, services.LibraryTypeVideo, services.LibraryTypeImage,
		services.LibraryTypeAudio, services.LibraryTypeDocument, services.LibraryTypeSnippet, services.LibraryTypeForum,
	}
	platforms := []string{"bilibili", "youtube", "xiaohongshu", "douyin"}
	filler := loadGeneratedFiller()
	tags := filler.Tags
	const base int64 = 1_794_790_800_000
	out := make([]services.LibraryEntry, n)
	for i := range out {
		title := fmt.Sprintf(filler.Title, i)
		switch i % 7 {
		case 3:
			title = fmt.Sprintf(filler.LongTitle, i)
		case 5:
			title = ""
		}
		e := services.LibraryEntry{
			ID: fmt.Sprintf("item-%06d", i), Title: title, Snippet: fmt.Sprintf(filler.Snippet, i),
			ItemType: types[i%len(types)], Status: services.LibraryStatusActive, ReviewStatus: "clear", ReviewReasons: []string{},
			IsPinned: i%50 == 0, IsFavorite: i%17 == 0,
			CreatedAt: base - int64(i)*90_000 - 3_600_000, UpdatedAt: base - int64(i)*60_000,
			Tags: []services.LibraryTag{},
		}
		if i%9 == 0 {
			e.Platform = platforms[(i/9)%len(platforms)]
		}
		if i%5 == 4 {
			e.Status = services.LibraryStatusArchived
		}
		if i%20 == 19 {
			e.DeletedAt = e.UpdatedAt + 1000
		}
		if i%3 == 0 {
			e.Tags = append(e.Tags, tags[i%len(tags)])
		}
		out[i] = e
	}
	return out
}
