package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// newLibraryOpsAdapterForTest 在真实的临时库上登记条目操作用到的处理器（含批量与导出），返回接到它们的适配器与 Library。
func newLibraryOpsAdapterForTest(t *testing.T) (libraryAdapter, *Bridge, *Library) {
	t.Helper()
	_, paths, db := openAdapterTestDB(t)
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	l := &Library{db: db, dbPath: paths.DatabasePath(), paths: paths}
	l.register(b)
	l.registerCatalogWrite(b)
	l.registerWrite(b)
	l.registerList(b)
	l.registerKnowledgeBatch(b)
	l.registerExport(b)
	return libraryAdapter{b: b}, b, l
}

// createOpsItem 建一个条目并返回 ID。
func createOpsItem(t *testing.T, b *Bridge, in map[string]any) string {
	t.Helper()
	created, err := callAs[struct {
		ID string `json:"id"`
	}](t.Context(), b, "knowledge:create", in)
	if err != nil || created.ID == "" {
		t.Fatalf("创建条目 %v：%v", in["title"], err)
	}
	return created.ID
}

func opsAllQuery() services.LibraryListQuery {
	return services.LibraryListQuery{Scope: services.LibraryScopeAll, SortBy: services.LibrarySortUpdatedAt, SortOrder: services.LibraryOrderDesc}
}

// 批量经真实处理器往返：冻结给出全部命中的 ID；逐条记账（缺失的条目失败而不拖累其余）；同一批次重试复用已提交的结果、
// 换命令被拒；回收站里的条目不能改，只有回收站里的条目能彻底删除（带「清证据」）。
func TestLibraryAdapterBatchAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	a := createOpsItem(t, b, map[string]any{"title": "甲", "content": "甲"})
	bb := createOpsItem(t, b, map[string]any{"title": "乙", "content": "乙"})
	c := createOpsItem(t, b, map[string]any{"title": "丙", "content": "丙"})

	frozen, err := svc.Freeze(ctx, opsAllQuery())
	if err != nil || len(frozen) != 3 || !slices.Contains(frozen, a) || !slices.Contains(frozen, bb) || !slices.Contains(frozen, c) {
		t.Fatalf("冻结应给出全部 3 条：%v %v", frozen, err)
	}

	archive := services.LibraryBatchCommand{Kind: services.LibraryBatchStatus, Status: services.LibraryStatusArchived}
	res, err := svc.ExecuteBatch(ctx, "run-1", []string{a, bb, "missing"}, archive)
	if err != nil || len(res) != 3 || !res[0].OK || !res[1].OK || res[2].OK || res[2].Error != "条目已删除" {
		t.Fatalf("归档两条加一个不存在的：逐条记账 %+v %v", res, err)
	}
	if after, _ := svc.Freeze(ctx, opsAllQuery()); len(after) != 1 || after[0] != c {
		t.Fatalf("归档后「全部」范围里只剩丙：%v", after)
	}

	// 同一批次重试：已提交的复用成功，失败的照旧失败。
	res, err = svc.ExecuteBatch(ctx, "run-1", []string{a, "missing"}, archive)
	if err != nil || len(res) != 2 || !res[0].OK || res[1].OK {
		t.Fatalf("同一批次重试：已提交的复用成功、失败的照旧 %+v %v", res, err)
	}
	// 同一批次换命令：有回执的条目被拒。
	res, err = svc.ExecuteBatch(ctx, "run-1", []string{a}, services.LibraryBatchCommand{Kind: services.LibraryBatchTrash})
	if err != nil || len(res) != 1 || res[0].OK || res[0].Error != "同一批次不能更换操作，请重新发起" {
		t.Fatalf("同一批次换命令应被拒：%+v %v", res, err)
	}

	// 移入回收站后不能再改状态；只有回收站里的条目能彻底删除。
	if res, _ = svc.ExecuteBatch(ctx, "run-2", []string{c}, services.LibraryBatchCommand{Kind: services.LibraryBatchTrash}); len(res) != 1 || !res[0].OK {
		t.Fatalf("移到回收站：%+v", res)
	}
	if res, _ = svc.ExecuteBatch(ctx, "run-3", []string{c}, archive); len(res) != 1 || res[0].OK || res[0].Error != "条目已在回收站" {
		t.Fatalf("回收站里的条目不能改状态：%+v", res)
	}
	del := services.LibraryBatchCommand{Kind: services.LibraryBatchDelete, ClearEvidence: true}
	if res, _ = svc.ExecuteBatch(ctx, "run-4", []string{a, c}, del); len(res) != 2 || res[0].OK || res[0].Error != "只能彻底删除回收站中的条目" || !res[1].OK {
		t.Fatalf("彻底删除：没进回收站的被拒、回收站里的成功：%+v", res)
	}
	if trash, _ := svc.Freeze(ctx, services.LibraryListQuery{Scope: services.LibraryScopeTrash}); len(trash) != 0 {
		t.Fatalf("丙已彻底删除，回收站应为空：%v", trash)
	}
}

// 补丁的 JSON 形状经真实处理器往返：移入合集、追加标签、收藏一次完成；collectionId 指向空串时写出 null、条目回到未归合集。
func TestLibraryAdapterBulkPatchAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	col, err := svc.CreateCollection(ctx, "合集", "")
	if err != nil {
		t.Fatal(err)
	}
	x := createOpsItem(t, b, map[string]any{"title": "甲", "content": "甲"})
	y := createOpsItem(t, b, map[string]any{"title": "乙", "content": "乙"})

	patch := services.LibraryEntryPatch{CollectionID: services.StringPtr(col.ID), AddTagNames: []string{"新标签"}, IsFavorite: services.BoolPtr(true)}
	res, err := svc.ExecuteBatch(ctx, "r1", []string{x, y}, services.LibraryBatchCommand{Kind: services.LibraryBatchUpdate, Patch: &patch})
	if err != nil || len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("批量改字段：%+v %v", res, err)
	}
	page, err := svc.List(ctx, services.LibraryListQuery{Scope: services.LibraryScopeFavorites, CollectionID: col.ID})
	if err != nil || page.Total != 2 {
		t.Fatalf("两条都应在合集里且已收藏：%+v %v", page, err)
	}
	if tags, _ := svc.Tags(ctx); len(tags) != 1 || tags[0].Name != "新标签" {
		t.Fatalf("追加标签应新建标签：%+v", tags)
	}

	out := services.LibraryEntryPatch{CollectionID: services.StringPtr("")}
	if res, _ = svc.ExecuteBatch(ctx, "r2", []string{x, y}, services.LibraryBatchCommand{Kind: services.LibraryBatchUpdate, Patch: &out}); len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("移出合集：%+v", res)
	}
	counts, err := svc.Counts(ctx, services.LibraryFacetQuery{Scope: services.LibraryScopeAll})
	if err != nil || counts.Uncategorized != 2 {
		t.Fatalf("移出合集后两条都应是未归合集：%+v %v", counts, err)
	}
}

// 单条更新经真实处理器往返：收藏、置顶、标签整体替换与清空；返回的条目能解码进 LibraryEntry（没有摘要）；条目不存在是 found=false 而不是错误。
func TestLibraryAdapterUpdateEntryAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "甲", "content": "正文"})

	entry, found, err := svc.UpdateEntry(ctx, id, services.LibraryEntryPatch{IsFavorite: services.BoolPtr(true), IsPinned: services.BoolPtr(true)})
	if err != nil || !found || entry.ID != id || entry.Title != "甲" || !entry.IsFavorite || !entry.IsPinned || entry.Snippet != "" {
		t.Fatalf("收藏与置顶：%+v found=%v %v", entry, found, err)
	}
	entry, _, err = svc.UpdateEntry(ctx, id, services.LibraryEntryPatch{TagNames: []string{"beta", "alpha"}})
	if err != nil || len(entry.Tags) != 2 || entry.Tags[0].Name != "alpha" || entry.Tags[1].Name != "beta" {
		t.Fatalf("标签整体替换（按名称排序）：%+v %v", entry.Tags, err)
	}
	entry, _, err = svc.UpdateEntry(ctx, id, services.LibraryEntryPatch{TagNames: []string{}})
	if err != nil || len(entry.Tags) != 0 || !entry.IsFavorite {
		t.Fatalf("空切片应清空标签且不动收藏：%+v %v", entry, err)
	}
	if _, found, err = svc.UpdateEntry(ctx, "missing", services.LibraryEntryPatch{IsFavorite: services.BoolPtr(true)}); err != nil || found {
		t.Fatalf("条目不存在应是 found=false 而不是错误：found=%v %v", found, err)
	}
}

// AI 交接稿经真实处理器往返：完整版含标题与正文；空内容、不存在各给业务错误；精简版对长文字稿略去长文本并报出字数。
func TestLibraryAdapterAiHandoffAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	col, err := svc.CreateCollection(ctx, "学习", "")
	if err != nil {
		t.Fatal(err)
	}
	note := createOpsItem(t, b, map[string]any{"title": "标题甲", "content": "正文乙", "tagNames": []string{"标签丙"}, "collectionId": col.ID})
	h, err := svc.AiHandoff(ctx, note, true)
	if err != nil || h.Title != "标题甲" || !strings.Contains(h.Text, "标题甲") || !strings.Contains(h.Text, "正文乙") || h.CharCount == 0 || h.OmittedChars != 0 {
		t.Fatalf("完整版：%+v %v", h, err)
	}
	if !strings.Contains(h.Text, "学习") || !strings.Contains(h.Text, "标签丙") {
		t.Fatalf("交接稿应带合集名与标签：%q", h.Text)
	}

	empty := createOpsItem(t, b, map[string]any{"title": "空", "content": "  "})
	if _, err := svc.AiHandoff(ctx, empty, true); !errors.Is(err, services.ErrHandoffEmpty) {
		t.Fatalf("没有正文与文字稿应是 ErrHandoffEmpty：%v", err)
	}
	if _, err := svc.AiHandoff(ctx, "missing", true); !errors.Is(err, services.ErrLibraryItemMissing) {
		t.Fatalf("条目不存在应是 ErrLibraryItemMissing：%v", err)
	}

	video := createOpsItem(t, b, map[string]any{"title": "视频", "content": "总结", "itemType": "video", "transcript": strings.Repeat("字", 600)})
	full, err := svc.AiHandoff(ctx, video, true)
	if err != nil || full.OmittedChars != 0 {
		t.Fatalf("长文字稿的完整版不略去：%+v %v", full, err)
	}
	brief, err := svc.AiHandoff(ctx, video, false)
	if err != nil || brief.OmittedChars <= 0 || brief.CharCount >= full.CharCount {
		t.Fatalf("精简版应略去长文本并更短：brief=%+v full=%d %v", brief, full.CharCount, err)
	}
}

// 另存交接稿经真实的导出处理器：成功写文件并回路径；用户取消不是失败；空稿件被拒。保存对话框由测试替换。
func TestLibraryAdapterExportAiHandoffAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, _, l := newLibraryOpsAdapterForTest(t)
	path := filepath.Join(t.TempDir(), "out.md")
	l.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) { return path, nil }
	res, err := svc.ExportAiHandoff(ctx, "标题", "交接稿内容")
	if err != nil || !res.Success || res.FilePath != path {
		t.Fatalf("另存成功：%+v %v", res, err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "交接稿内容" {
		t.Fatalf("文件内容：%q %v", raw, err)
	}

	l.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) { return "", nil }
	if res, err = svc.ExportAiHandoff(ctx, "标题", "交接稿内容"); err != nil || res.Success || !res.Canceled {
		t.Fatalf("用户取消不算失败：%+v %v", res, err)
	}
	if res, err = svc.ExportAiHandoff(ctx, "标题", "  "); err != nil || res.Success || res.Error != "交接稿内容为空" {
		t.Fatalf("空稿件应被拒：%+v %v", res, err)
	}
}

// 补采经 web:snapshot-enqueue 的回复壳：ok 为真是成功，为假带处理器给的原因。
func TestLibraryAdapterSupplementWechatReplyShell(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	var got [][]string
	reply := map[string]any{"ok": true, "data": []any{}}
	b.handlers["web:snapshot-enqueue"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		var ids []string
		_ = json.Unmarshal(a[0], &ids)
		got = append(got, ids)
		return reply, nil
	}
	if err := svc.SupplementWechat(ctx, []string{"a", "b"}); err != nil || len(got) != 1 || !slices.Equal(got[0], []string{"a", "b"}) {
		t.Fatalf("补采成功：%v %v", got, err)
	}
	reply = map[string]any{"ok": false, "error": "单次补采 1–50 篇文章"}
	if err := svc.SupplementWechat(ctx, []string{"a"}); err == nil || err.Error() != "单次补采 1–50 篇文章" {
		t.Fatalf("失败应带处理器给的原因：%v", err)
	}
}
