package servicestest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// Library 是 services.Library 的假实现。零值可用：没有合集、没有标签、计数全是零。
//
// 记录每次调用（Calls）与计数查询（CountQueries），可对某个方法注入失败（FailOn），
// 也可让计数请求卡住（HoldCounts）来构造「旧响应晚于新响应」的竞态。
type Library struct {
	mu          sync.Mutex
	counts      services.LibraryCounts
	collections []services.LibraryCollection
	tags        []services.LibraryTag
	failures    map[string]error
	calls       []string
	queries     []services.LibraryFacetQuery
	gate        chan struct{}
	nextID      int

	// 条目列表（见 library_list.go）：条目全集、收到的列表查询、让下一次 List 卡住的闸门。
	entries     []services.LibraryEntry
	listQueries []services.LibraryListQuery
	listGate    chan struct{}

	// 条目操作（见 library_ops.go）：批量回执、注入的失败、交接稿等。
	ops opsState
}

var _ services.Library = (*Library)(nil)

// Seed 设置初始数据；返回自己以便链式书写。
func (f *Library) Seed(counts services.LibraryCounts, collections []services.LibraryCollection, tags []services.LibraryTag) *Library {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts = counts
	f.collections = append([]services.LibraryCollection(nil), collections...)
	f.tags = append([]services.LibraryTag(nil), tags...)
	return f
}

// SetCounts 换掉之后 Counts 返回的结果。
func (f *Library) SetCounts(counts services.LibraryCounts) {
	f.mu.Lock()
	f.counts = counts
	f.mu.Unlock()
}

// FailOn 让某个方法（"Counts"、"List"、"Collections"、"CreateCollection"、"UpdateCollection"、"DeleteCollection"、
// "Tags"、"UpdateTag"、"DeleteTag"、"Freeze"、"ExecuteBatch"、"UpdateEntry"、"SupplementWechat"、"AiHandoff"、"ExportAiHandoff"）
// 返回 err；err 为 nil 取消注入。
func (f *Library) FailOn(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures == nil {
		f.failures = map[string]error{}
	}
	if err == nil {
		delete(f.failures, method)
		return
	}
	f.failures[method] = err
}

// HoldCounts 让下一次 Counts 卡住，直到返回的函数被调用（只卡一次，之后的调用不受影响）。
func (f *Library) HoldCounts() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.gate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// Calls 返回迄今的调用记录（"方法" 或 "方法:参数"），按发生顺序。
func (f *Library) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// CountQueries 返回迄今收到的计数查询。
func (f *Library) CountQueries() []services.LibraryFacetQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.LibraryFacetQuery(nil), f.queries...)
}

// CollectionsNow 返回假实现里此刻的合集（测试断言写入结果用）。
func (f *Library) CollectionsNow() []services.LibraryCollection {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.LibraryCollection(nil), f.collections...)
}

// TagsNow 返回假实现里此刻的标签。
func (f *Library) TagsNow() []services.LibraryTag {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.LibraryTag(nil), f.tags...)
}

// begin 记一次调用并取注入的失败；mu 由调用方不持有。
func (f *Library) begin(call, method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.failures[method]
}

func (f *Library) Counts(ctx context.Context, q services.LibraryFacetQuery) (services.LibraryCounts, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.calls = append(f.calls, "Counts")
	gate, err, counts := f.gate, f.failures["Counts"], f.counts
	f.gate = nil // 只卡下一次
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return services.LibraryCounts{}, ctx.Err()
		}
	}
	if err != nil {
		return services.LibraryCounts{}, err
	}
	return counts, nil
}

func (f *Library) Collections(ctx context.Context) ([]services.LibraryCollection, error) {
	if err := f.begin("Collections", "Collections"); err != nil {
		return nil, err
	}
	return f.CollectionsNow(), nil
}

func (f *Library) CreateCollection(ctx context.Context, name, icon string) (services.LibraryCollection, error) {
	if err := f.begin("CreateCollection:"+name, "CreateCollection"); err != nil {
		return services.LibraryCollection{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	created := services.LibraryCollection{ID: fmt.Sprintf("col-new-%d", f.nextID), Name: name, Icon: icon, SortOrder: len(f.collections)}
	f.collections = append(f.collections, created)
	return created, nil
}

func (f *Library) UpdateCollection(ctx context.Context, id, name, icon string) error {
	if err := f.begin("UpdateCollection:"+id, "UpdateCollection"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.collections {
		if f.collections[i].ID == id {
			f.collections[i].Name, f.collections[i].Icon = name, icon
			return nil
		}
	}
	return errors.New("合集不存在")
}

func (f *Library) DeleteCollection(ctx context.Context, id string) (bool, error) {
	if err := f.begin("DeleteCollection:"+id, "DeleteCollection"); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.collections {
		if f.collections[i].ID == id {
			f.collections = append(f.collections[:i:i], f.collections[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *Library) Tags(ctx context.Context) ([]services.LibraryTag, error) {
	if err := f.begin("Tags", "Tags"); err != nil {
		return nil, err
	}
	return f.TagsNow(), nil
}

func (f *Library) UpdateTag(ctx context.Context, id string, patch services.LibraryTagPatch) error {
	if err := f.begin("UpdateTag:"+id, "UpdateTag"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tags {
		if f.tags[i].ID != id {
			continue
		}
		if patch.Name != nil {
			f.tags[i].Name = *patch.Name
		}
		if patch.ColorKey != nil {
			f.tags[i].ColorKey = *patch.ColorKey
		}
		// 与服务端一致：标签按名称排序（SQLite 的 BINARY 排序即字节序）。
		sort.SliceStable(f.tags, func(a, b int) bool { return f.tags[a].Name < f.tags[b].Name })
		return nil
	}
	return errors.New("标签不存在")
}

func (f *Library) DeleteTag(ctx context.Context, id string) (bool, error) {
	if err := f.begin("DeleteTag:"+id, "DeleteTag"); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tags {
		if f.tags[i].ID == id {
			f.tags = append(f.tags[:i:i], f.tags[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
