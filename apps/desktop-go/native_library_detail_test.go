package main

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"

	"guizhi-mygo-shell/native/services"
)

// newLibraryDetailAdapterForTest 在真实的临时库上登记详情用到的处理器（条目读写、草稿保存、来源版本）。
func newLibraryDetailAdapterForTest(t *testing.T) (libraryAdapter, *Bridge, *Library) {
	t.Helper()
	svc, b, l := newLibraryOpsAdapterForTest(t)
	l.registerKnowledgeDraft(b)
	return svc, b, l
}

func tagNamesOf(tags []services.LibraryTag) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	return names
}

// 取详情经真实处理器往返：完整字段（正文与标签）能解码进 LibraryItem；不存在的条目是 found=false 而不是错误。
func TestLibraryAdapterGetItemAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryDetailAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "标题", "content": "正文第一行\n第二行", "tagNames": []string{"乙", "甲"}})

	item, found, err := svc.GetItem(ctx, id)
	if err != nil || !found {
		t.Fatalf("取详情：found=%v err=%v", found, err)
	}
	if item.ID != id || item.Title != "标题" || item.Content != "正文第一行\n第二行" || item.ItemType != services.LibraryTypeNote ||
		item.Status != services.LibraryStatusActive || item.DeletedAt != 0 || item.CreatedAt == 0 || item.UpdatedAt == 0 {
		t.Fatalf("字段不对：%+v", item)
	}
	if got := item.TagNames(); !slices.Equal(got, []string{"乙", "甲"}) && !slices.Equal(got, []string{"甲", "乙"}) {
		t.Fatalf("标签不对：%v", got)
	}
	if _, found, err = svc.GetItem(ctx, "missing"); err != nil || found {
		t.Fatalf("不存在的条目应是 found=false：found=%v err=%v", found, err)
	}
}

// 草稿保存经真实处理器往返：标题、正文、标签一起落盘；基线与服务端一致时没有冲突。
func TestLibraryAdapterSaveDraftAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryDetailAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "旧标题", "content": "旧正文", "tagNames": []string{"a"}})
	item, _, _ := svc.GetItem(ctx, id)

	base := services.LibraryDraftFields{Title: services.StringPtr(item.Title), Content: services.StringPtr(item.Content), TagNames: item.TagNames()}
	patch := services.LibraryDraftFields{Title: services.StringPtr("新标题"), Content: services.StringPtr("新正文"), TagNames: []string{"a", "b"}}
	res, err := svc.SaveDraft(ctx, services.LibraryDraftInput{ID: id, RequestID: "r1", Base: base, Patch: patch})
	if err != nil || !res.OK || res.Item == nil || len(res.Conflicts) != 0 {
		t.Fatalf("保存应成功：%+v %v", res, err)
	}
	if res.Item.Title != "新标题" || res.Item.Content != "新正文" || !slices.Equal(tagNamesOf(res.Item.Tags), []string{"a", "b"}) {
		t.Fatalf("返回的条目应是保存后的：%+v", res.Item)
	}
	if again, _, _ := svc.GetItem(ctx, id); again.Title != "新标题" || again.Content != "新正文" || again.UpdatedAt < item.UpdatedAt {
		t.Fatalf("应已落盘：%+v", again)
	}
}

// 冲突经真实处理器往返：基线与服务端不同、补丁也与服务端不同才算冲突；冲突时什么也不写，回复里带服务端的版本；
// 补丁恰好等于服务端的值不算冲突。
func TestLibraryAdapterSaveDraftConflictAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryDetailAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "标题", "content": "原文"})
	base := services.LibraryDraftFields{Title: services.StringPtr("标题"), Content: services.StringPtr("原文")}

	// 另一入口把正文改成了「别处的修改」。
	if _, err := b.call(ctx, "knowledge:update", id, map[string]any{"content": "别处的修改"}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SaveDraft(ctx, services.LibraryDraftInput{ID: id, RequestID: "r1", Base: base,
		Patch: services.LibraryDraftFields{Title: services.StringPtr("本地标题"), Content: services.StringPtr("我的修改")}})
	if err != nil || res.OK || !slices.Equal(res.Conflicts, []string{services.LibraryDraftFieldContent}) || res.Item == nil || res.Item.Content != "别处的修改" || res.Error == "" {
		t.Fatalf("正文冲突：%+v %v", res, err)
	}
	if now, _, _ := svc.GetItem(ctx, id); now.Title != "标题" || now.Content != "别处的修改" {
		t.Fatalf("冲突时什么也不能写（连没冲突的标题也不写）：%+v", now)
	}

	same := services.LibraryDraftFields{Content: services.StringPtr("别处的修改"), Title: services.StringPtr("本地标题")}
	if res, err = svc.SaveDraft(ctx, services.LibraryDraftInput{ID: id, RequestID: "r2", Base: base, Patch: same}); err != nil || !res.OK {
		t.Fatalf("补丁等于服务端的值不算冲突：%+v %v", res, err)
	}
}

// 标签按相对基线的增删合并：另一入口新加的标签保留，本地删掉的标签被删掉，本地新加的被加上。
func TestLibraryAdapterSaveDraftMergesTagsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryDetailAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "标题", "content": "正文", "tagNames": []string{"a", "b"}})
	if _, err := b.call(ctx, "knowledge:update", id, map[string]any{"tagNames": []string{"a", "b", "别处"}}); err != nil {
		t.Fatal(err)
	}
	// 本地基线是 [a b]：删了 b、加了 c。
	res, err := svc.SaveDraft(ctx, services.LibraryDraftInput{ID: id, RequestID: "r1",
		Base:  services.LibraryDraftFields{TagNames: []string{"a", "b"}},
		Patch: services.LibraryDraftFields{TagNames: []string{"a", "c"}}})
	if err != nil || !res.OK || res.Item == nil {
		t.Fatalf("保存：%+v %v", res, err)
	}
	got := tagNamesOf(res.Item.Tags)
	slices.Sort(got)
	if !slices.Equal(got, []string{"a", "c", "别处"}) {
		t.Fatalf("合并后应是 a、c、别处：%v", got)
	}
}

// 业务失败在回复里（不是 Go 错误）：回收站里的条目、不存在的条目不能存草稿。
func TestLibraryAdapterSaveDraftSoftFailuresAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryDetailAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "标题", "content": "正文"})
	patch := services.LibraryDraftFields{Title: services.StringPtr("新")}

	if res, err := svc.SaveDraft(ctx, services.LibraryDraftInput{ID: "missing", RequestID: "r1", Patch: patch}); err != nil || res.OK || res.Error == "" {
		t.Fatalf("不存在的条目：%+v %v", res, err)
	}
	if _, err := svc.ExecuteBatch(ctx, "run", []string{id}, services.LibraryBatchCommand{Kind: services.LibraryBatchTrash}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SaveDraft(ctx, services.LibraryDraftInput{ID: id, RequestID: "r2", Patch: patch})
	if err != nil || res.OK || res.Error != "条目不存在或已移到回收站" || len(res.Conflicts) != 0 {
		t.Fatalf("回收站里的条目不能存草稿：%+v %v", res, err)
	}
}

// 来源版本经真实处理器往返：新的在前，字段能解码进 LibrarySourceVersion；没有版本是空列表而不是错误。
func TestLibraryAdapterSourceVersionsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, l := newLibraryDetailAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "标题", "content": "正文"})

	if versions, err := svc.SourceVersions(ctx, id); err != nil || len(versions) != 0 {
		t.Fatalf("没有版本应是空列表：%+v %v", versions, err)
	}
	stmts := []sqlite.Statement{
		{SQL: "INSERT INTO source_capture_revisions VALUES(?,?,?,?,?,?,?)", Args: []any{"v1", id, "旧标题", "旧正文", nil, `["正文过短"]`, int64(1000)}},
		{SQL: "INSERT INTO source_capture_revisions VALUES(?,?,?,?,?,?,?)", Args: []any{"v2", id, "新标题", "新正文", "文字稿", `[]`, int64(2000)}},
	}
	l.writeMu.Lock()
	_, err := l.db.Transaction(ctx, stmts)
	l.writeMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	versions, err := svc.SourceVersions(ctx, id)
	if err != nil || len(versions) != 2 {
		t.Fatalf("两个版本：%+v %v", versions, err)
	}
	want := services.LibrarySourceVersion{ID: "v2", Title: "新标题", Content: "新正文", Transcript: "文字稿", ReviewJSON: "[]", CapturedAt: 2000}
	if versions[0] != want {
		t.Fatalf("新的在前：%+v", versions[0])
	}
	if versions[1].ID != "v1" || versions[1].Transcript != "" || versions[1].ReviewJSON != `["正文过短"]` {
		t.Fatalf("旧版本（文字稿为空）：%+v", versions[1])
	}
}

// 复核状态的补丁经真实处理器往返：标记已复核（clear 加清空原因）、改回待复核（带原因）。
func TestLibraryAdapterReviewPatchAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryDetailAdapterForTest(t)
	id := createOpsItem(t, b, map[string]any{"title": "标题", "content": "正文", "reviewStatus": services.LibraryReviewNeedsReview, "reviewReasons": []string{"正文过短", "缺少图片"}})

	item, _, _ := svc.GetItem(ctx, id)
	if item.ReviewStatus != services.LibraryReviewNeedsReview || !slices.Equal(item.ReviewReasons, []string{"正文过短", "缺少图片"}) {
		t.Fatalf("创建时的复核状态：%+v", item)
	}
	entry, found, err := svc.UpdateEntry(ctx, id, services.LibraryEntryPatch{ReviewStatus: services.StringPtr(services.LibraryReviewClear), ReviewReasons: []string{}})
	if err != nil || !found || entry.ReviewStatus != services.LibraryReviewClear || len(entry.ReviewReasons) != 0 {
		t.Fatalf("标记已复核：%+v found=%v %v", entry, found, err)
	}
	entry, _, err = svc.UpdateEntry(ctx, id, services.LibraryEntryPatch{ReviewStatus: services.StringPtr(services.LibraryReviewNeedsReview), ReviewReasons: []string{"采用了旧版本"}})
	if err != nil || entry.ReviewStatus != services.LibraryReviewNeedsReview || !slices.Equal(entry.ReviewReasons, []string{"采用了旧版本"}) {
		t.Fatalf("改回待复核：%+v %v", entry, err)
	}
}

// 媒体能力经真实处理器往返：没配置转写引擎时不支持区分说话人；回复里的 true 能解码成 Diarization。
func TestLibraryAdapterMediaCapabilitiesAgainstRealHandler(t *testing.T) {
	ctx := t.Context()
	e := newMediaEnv(t, false)
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
	e.l.registerMedia(b)
	svc := libraryAdapter{b: b}
	caps, err := svc.MediaCapabilities(ctx)
	if err != nil || caps.Diarization {
		t.Fatalf("没配置转写引擎：%+v err=%v", caps, err)
	}
	b.handlers[chMediaCapabilities] = func(context.Context, []json.RawMessage) (any, error) {
		return map[string]any{"diarization": true}, nil
	}
	if caps, err = svc.MediaCapabilities(ctx); err != nil || !caps.Diarization {
		t.Fatalf("回复 true 应解码：%+v err=%v", caps, err)
	}
}
