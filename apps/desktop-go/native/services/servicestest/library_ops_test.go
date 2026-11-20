package servicestest

import (
	"errors"
	"slices"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// 假实现与真实处理器的对拍：同一组场景在 native_library_ops_test.go 里对真实处理器跑过一遍，
// 这里的断言与那边逐条一致——假实现的规则一旦偏离真实处理器，两边的测试会各自失败在同一处。

func opsEntries() []services.LibraryEntry {
	mk := func(id string, updated int64) services.LibraryEntry {
		return services.LibraryEntry{ID: id, Title: id, Status: services.LibraryStatusActive, UpdatedAt: updated, Tags: []services.LibraryTag{}}
	}
	return []services.LibraryEntry{mk("a", 3), mk("b", 2), mk("c", 1)}
}

func allQuery() services.LibraryListQuery {
	return services.LibraryListQuery{Scope: services.LibraryScopeAll, SortBy: services.LibrarySortUpdatedAt, SortOrder: services.LibraryOrderDesc}
}

func TestFakeBatchMirrorsRealHandler(t *testing.T) {
	ctx := t.Context()
	f := (&Library{}).SeedEntries(opsEntries())

	frozen, err := f.Freeze(ctx, allQuery())
	if err != nil || !slices.Equal(frozen, []string{"a", "b", "c"}) {
		t.Fatalf("冻结应按列表顺序给出全部命中：%v %v", frozen, err)
	}

	archive := services.LibraryBatchCommand{Kind: services.LibraryBatchStatus, Status: services.LibraryStatusArchived}
	res, err := f.ExecuteBatch(ctx, "run-1", []string{"a", "b", "missing"}, archive)
	if err != nil || len(res) != 3 || !res[0].OK || !res[1].OK || res[2].OK || res[2].Error != "条目已删除" {
		t.Fatalf("逐条记账：%+v %v", res, err)
	}
	if after, _ := f.Freeze(ctx, allQuery()); !slices.Equal(after, []string{"c"}) {
		t.Fatalf("归档后「全部」范围里只剩 c：%v", after)
	}

	if res, _ = f.ExecuteBatch(ctx, "run-1", []string{"a", "missing"}, archive); len(res) != 2 || !res[0].OK || res[1].OK {
		t.Fatalf("同一批次重试：已提交的复用成功、失败的照旧 %+v", res)
	}
	if res, _ = f.ExecuteBatch(ctx, "run-1", []string{"a"}, services.LibraryBatchCommand{Kind: services.LibraryBatchTrash}); len(res) != 1 || res[0].OK || res[0].Error != "同一批次不能更换操作，请重新发起" {
		t.Fatalf("同一批次换命令应被拒：%+v", res)
	}

	if res, _ = f.ExecuteBatch(ctx, "run-2", []string{"c"}, services.LibraryBatchCommand{Kind: services.LibraryBatchTrash}); len(res) != 1 || !res[0].OK {
		t.Fatalf("移到回收站：%+v", res)
	}
	if res, _ = f.ExecuteBatch(ctx, "run-3", []string{"c"}, archive); len(res) != 1 || res[0].OK || res[0].Error != "条目已在回收站" {
		t.Fatalf("回收站里的条目不能改状态：%+v", res)
	}
	del := services.LibraryBatchCommand{Kind: services.LibraryBatchDelete, ClearEvidence: true}
	if res, _ = f.ExecuteBatch(ctx, "run-4", []string{"a", "c"}, del); len(res) != 2 || res[0].OK || res[0].Error != "只能彻底删除回收站中的条目" || !res[1].OK {
		t.Fatalf("彻底删除：没进回收站的被拒、回收站里的成功：%+v", res)
	}
	if trash, _ := f.Freeze(ctx, services.LibraryListQuery{Scope: services.LibraryScopeTrash}); len(trash) != 0 {
		t.Fatalf("c 已彻底删除，回收站应为空：%v", trash)
	}
	if got := f.ClearedEvidence(); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("带「清证据」删除的应记下：%v", got)
	}
}

// 补丁：移入合集、追加标签、收藏；指向空串的合集 ID 移出合集；标签整体替换与清空；单条更新返回没有摘要的条目。
func TestFakePatchesMirrorRealHandler(t *testing.T) {
	ctx := t.Context()
	entries := opsEntries()
	entries[0].Snippet = "摘要"
	f := (&Library{}).SeedEntries(entries)

	patch := services.LibraryEntryPatch{CollectionID: services.StringPtr("col-1"), AddTagNames: []string{"新标签"}, IsFavorite: services.BoolPtr(true)}
	if res, _ := f.ExecuteBatch(ctx, "r1", []string{"a", "b"}, services.LibraryBatchCommand{Kind: services.LibraryBatchUpdate, Patch: &patch}); len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("批量改字段：%+v", res)
	}
	got := f.EntriesNow()
	if got[0].CollectionID != "col-1" || !got[0].IsFavorite || len(got[0].Tags) != 1 || got[0].Tags[0].Name != "新标签" || got[2].IsFavorite {
		t.Fatalf("补丁应落到 a、b 上、不碰 c：%+v", got)
	}
	if tags := f.TagsNow(); len(tags) != 1 || tags[0].Name != "新标签" {
		t.Fatalf("追加标签应新建标签：%+v", tags)
	}
	out := services.LibraryEntryPatch{CollectionID: services.StringPtr("")}
	f.ExecuteBatch(ctx, "r2", []string{"a"}, services.LibraryBatchCommand{Kind: services.LibraryBatchUpdate, Patch: &out})
	if f.EntriesNow()[0].CollectionID != "" {
		t.Fatal("空串应把条目移出合集")
	}

	entry, found, err := f.UpdateEntry(ctx, "a", services.LibraryEntryPatch{TagNames: []string{"beta", "alpha"}})
	if err != nil || !found || entry.Snippet != "" || len(entry.Tags) != 2 || entry.Tags[0].Name != "alpha" {
		t.Fatalf("标签整体替换并按名称排序、返回的条目没有摘要：%+v found=%v %v", entry, found, err)
	}
	if f.EntriesNow()[0].Snippet != "摘要" {
		t.Fatal("单条更新不能丢掉全集里的摘要")
	}
	if entry, _, _ = f.UpdateEntry(ctx, "a", services.LibraryEntryPatch{TagNames: []string{}}); len(entry.Tags) != 0 {
		t.Fatalf("空切片应清空标签：%+v", entry.Tags)
	}
	if _, found, err = f.UpdateEntry(ctx, "missing", services.LibraryEntryPatch{IsFavorite: services.BoolPtr(true)}); err != nil || found {
		t.Fatalf("条目不存在应是 found=false 而不是错误：%v %v", found, err)
	}
}

// 注入的失败只落在指定的那一条上，其余照常；失败写回执，同一批次重试时会再次执行它。
func TestFakeInjectedItemFailure(t *testing.T) {
	ctx := t.Context()
	f := (&Library{}).SeedEntries(opsEntries())
	f.FailBatchItem("b", "磁盘写入失败")
	trash := services.LibraryBatchCommand{Kind: services.LibraryBatchTrash}
	res, _ := f.ExecuteBatch(ctx, "r", []string{"a", "b", "c"}, trash)
	if !res[0].OK || res[1].OK || res[1].Error != "磁盘写入失败" || !res[2].OK {
		t.Fatalf("只有 b 失败：%+v", res)
	}
	f.FailBatchItem("b", "")
	res, _ = f.ExecuteBatch(ctx, "r", []string{"b"}, trash)
	if !res[0].OK {
		t.Fatalf("取消注入后重试 b 应成功：%+v", res)
	}
}

// 交接稿：默认的确定性文本、业务错误与精简版；另存的默认结果与注入。
func TestFakeHandoffAndExport(t *testing.T) {
	ctx := t.Context()
	f := (&Library{}).SeedEntries(opsEntries())
	h, err := f.AiHandoff(ctx, "a", true)
	if err != nil || h.Title != "a" || h.Text != "handoff:a:full" || h.CharCount != len([]rune(h.Text)) {
		t.Fatalf("默认完整版：%+v %v", h, err)
	}
	if _, err = f.AiHandoff(ctx, "missing", true); !errors.Is(err, services.ErrLibraryItemMissing) {
		t.Fatalf("不存在：%v", err)
	}
	f.FailHandoff("b", services.ErrHandoffEmpty)
	if _, err = f.AiHandoff(ctx, "b", true); !errors.Is(err, services.ErrHandoffEmpty) {
		t.Fatalf("注入的空内容：%v", err)
	}
	res, err := f.ExportAiHandoff(ctx, "标题", "稿")
	if err != nil || !res.Success || len(f.Exports()) != 1 || f.Exports()[0].Text != "稿" {
		t.Fatalf("默认另存成功并记下请求：%+v %v", res, err)
	}
	f.SetExportResult(services.LibraryExportResult{Canceled: true})
	if res, _ = f.ExportAiHandoff(ctx, "标题", "稿"); !res.Canceled || res.Success {
		t.Fatalf("注入的取消：%+v", res)
	}
}
