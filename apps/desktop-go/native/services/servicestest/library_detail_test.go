package servicestest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// 假实现的详情规则与真实处理器对齐（对应的往返测试在主包 native_library_detail_test.go），这里逐条核对假实现自己。

func detailFake() *Library {
	f := &Library{}
	f.SeedEntries([]services.LibraryEntry{
		{ID: "a", Title: "标题", ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive, Tags: []services.LibraryTag{{ID: "t1", Name: "a"}, {ID: "t2", Name: "b"}}},
		{ID: "gone", Title: "回收站里的", DeletedAt: 5},
	})
	f.SeedDetail("a", DetailBody{Content: "原文", SourceURI: "https://example.com/x"})
	return f
}

func str(s string) *string { return &s }

func TestGetItemJoinsEntryAndBody(t *testing.T) {
	f := detailFake()
	item, found, err := f.GetItem(context.Background(), "a")
	if err != nil || !found || item.Title != "标题" || item.Content != "原文" || item.SourceURI != "https://example.com/x" ||
		!slices.Equal(item.TagNames(), []string{"a", "b"}) || item.ItemType != services.LibraryTypeNote {
		t.Fatalf("%+v found=%v err=%v", item, found, err)
	}
	if _, found, _ = f.GetItem(context.Background(), "missing"); found {
		t.Fatal("不存在的条目应是 found=false")
	}
	if got := f.Gets(); !slices.Equal(got, []string{"a", "missing"}) {
		t.Fatalf("调用记录：%v", got)
	}
	f.FailOn("GetItem", errors.New("读取失败"))
	if _, _, err = f.GetItem(context.Background(), "a"); err == nil {
		t.Fatal("注入的失败要返回")
	}
}

func TestSaveDraftWritesAndMergesTags(t *testing.T) {
	f := detailFake()
	ctx := context.Background()
	res, err := f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", RequestID: "r",
		Base:  services.LibraryDraftFields{Title: str("标题"), Content: str("原文"), TagNames: []string{"a", "b"}},
		Patch: services.LibraryDraftFields{Title: str("新标题"), Content: str("新正文"), TagNames: []string{"a", "c"}}})
	if err != nil || !res.OK || res.Item == nil || res.Item.Title != "新标题" || res.Item.Content != "新正文" || !slices.Equal(res.Item.TagNames(), []string{"a", "c"}) {
		t.Fatalf("保存：%+v %v", res, err)
	}
	if now, _ := f.ItemNow("a"); now.Content != "新正文" || now.Title != "新标题" {
		t.Fatalf("应已落到假实现里：%+v", now)
	}
	// 另一入口新加了标签：按相对基线的增删合并，别处新加的保留。
	f.EditExternally("a", func(it *services.LibraryItem) { it.Tags = append(it.Tags, services.LibraryTag{Name: "别处"}) })
	res, _ = f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", RequestID: "r2",
		Base:  services.LibraryDraftFields{TagNames: []string{"a", "c"}},
		Patch: services.LibraryDraftFields{TagNames: []string{"a"}}})
	got := res.Item.TagNames()
	slices.Sort(got)
	if !res.OK || !slices.Equal(got, []string{"a", "别处"}) {
		t.Fatalf("合并：%+v", res)
	}
	if len(f.Drafts()) != 2 {
		t.Fatalf("应记下两次请求：%d", len(f.Drafts()))
	}
}

func TestSaveDraftConflictWritesNothing(t *testing.T) {
	f := detailFake()
	ctx := context.Background()
	f.EditExternally("a", func(it *services.LibraryItem) { it.Content = "别处的修改" })
	base := services.LibraryDraftFields{Title: str("标题"), Content: str("原文")}

	res, err := f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", RequestID: "r1", Base: base,
		Patch: services.LibraryDraftFields{Title: str("本地标题"), Content: str("我的修改")}})
	if err != nil || res.OK || !slices.Equal(res.Conflicts, []string{services.LibraryDraftFieldContent}) || res.Item == nil || res.Item.Content != "别处的修改" || res.Error != draftConflictReason {
		t.Fatalf("正文冲突：%+v %v", res, err)
	}
	if now, _ := f.ItemNow("a"); now.Title != "标题" || now.Content != "别处的修改" {
		t.Fatalf("冲突时什么也不能写：%+v", now)
	}
	// 补丁恰好等于服务端的值不算冲突；基线里没有这个字段、补丁又与服务端不同，算冲突。
	if res, _ = f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", RequestID: "r2", Base: base, Patch: services.LibraryDraftFields{Content: str("别处的修改")}}); !res.OK {
		t.Fatalf("补丁等于服务端的值不算冲突：%+v", res)
	}
	if res, _ = f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", RequestID: "r3", Patch: services.LibraryDraftFields{Title: str("无基线")}}); res.OK || len(res.Conflicts) != 1 {
		t.Fatalf("没有基线又与服务端不同应冲突：%+v", res)
	}
}

func TestSaveDraftFailures(t *testing.T) {
	f := detailFake()
	ctx := context.Background()
	patch := services.LibraryDraftFields{Title: str("新")}
	for _, id := range []string{"missing", "gone"} {
		if res, err := f.SaveDraft(ctx, services.LibraryDraftInput{ID: id, Patch: patch}); err != nil || res.OK || res.Error != draftGoneReason {
			t.Fatalf("%s：%+v %v", id, res, err)
		}
	}
	f.FailDraft("磁盘已满")
	if res, err := f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", Patch: patch}); err != nil || res.OK || res.Error != "磁盘已满" {
		t.Fatalf("业务失败：%+v %v", res, err)
	}
	f.FailDraft("")
	f.FailOn("SaveDraft", errors.New("连接断开"))
	if _, err := f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", Patch: patch}); err == nil {
		t.Fatal("调用失败要返回 error")
	}
	f.FailOn("SaveDraft", nil)
	if res, _ := f.SaveDraft(ctx, services.LibraryDraftInput{ID: "a", Base: services.LibraryDraftFields{Title: str("标题")}, Patch: patch}); !res.OK {
		t.Fatalf("取消注入后应成功：%+v", res)
	}
}

func TestDetailGatesHoldOnce(t *testing.T) {
	f := detailFake()
	release := f.HoldGet()
	done := make(chan services.LibraryItem, 1)
	go func() {
		item, _, _ := f.GetItem(context.Background(), "a")
		done <- item
	}()
	select {
	case <-done:
		t.Fatal("闸门没放行就不该返回")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	if item := <-done; item.ID != "a" {
		t.Fatalf("放行后应返回：%+v", item)
	}
	// 只卡一次。
	if _, found, _ := f.GetItem(context.Background(), "a"); !found {
		t.Fatal("第二次不该再卡")
	}

	release = f.HoldSave()
	saved := make(chan services.LibraryDraftResult, 1)
	go func() {
		res, _ := f.SaveDraft(context.Background(), services.LibraryDraftInput{ID: "a", Base: services.LibraryDraftFields{Title: str("标题")}, Patch: services.LibraryDraftFields{Title: str("新")}})
		saved <- res
	}()
	select {
	case <-saved:
		t.Fatal("保存闸门没放行就不该返回")
	case <-time.After(50 * time.Millisecond):
	}
	if now, _ := f.ItemNow("a"); now.Title != "标题" {
		t.Fatalf("闸门在写入之前：%+v", now)
	}
	release()
	if res := <-saved; !res.OK {
		t.Fatalf("放行后应成功：%+v", res)
	}
}

func TestSourceVersionsAndReviewPatch(t *testing.T) {
	f := detailFake()
	ctx := context.Background()
	if vs, err := f.SourceVersions(ctx, "a"); err != nil || len(vs) != 0 {
		t.Fatalf("没有版本：%+v %v", vs, err)
	}
	f.SeedSourceVersions("a", []services.LibrarySourceVersion{{ID: "v2", CapturedAt: 2}, {ID: "v1", CapturedAt: 1}})
	if vs, _ := f.SourceVersions(ctx, "a"); len(vs) != 2 || vs[0].ID != "v2" {
		t.Fatalf("新的在前：%+v", vs)
	}
	entry, found, err := f.UpdateEntry(ctx, "a", services.LibraryEntryPatch{ReviewStatus: services.StringPtr(services.LibraryReviewNeedsReview), ReviewReasons: []string{"正文过短"}})
	if err != nil || !found || entry.ReviewStatus != services.LibraryReviewNeedsReview || !slices.Equal(entry.ReviewReasons, []string{"正文过短"}) {
		t.Fatalf("复核补丁：%+v %v", entry, err)
	}
	if item, _ := f.ItemNow("a"); item.ReviewStatus != services.LibraryReviewNeedsReview || len(item.ReviewReasons) != 1 {
		t.Fatalf("详情应看到同一份复核状态：%+v", item)
	}
	entry, _, _ = f.UpdateEntry(ctx, "a", services.LibraryEntryPatch{ReviewStatus: services.StringPtr(services.LibraryReviewClear), ReviewReasons: []string{}})
	if entry.ReviewStatus != services.LibraryReviewClear || len(entry.ReviewReasons) != 0 {
		t.Fatalf("标记已复核：%+v", entry)
	}
}

func TestMergeDraftTags(t *testing.T) {
	cases := []struct{ base, edited, current, want []string }{
		{[]string{"a", "b"}, []string{"a", "c"}, []string{"a", "b", "x"}, []string{"a", "x", "c"}},
		{nil, []string{"A "}, []string{"a"}, []string{"A "}},                   // 以 trim + 小写作键，最后写入的值胜出
		{[]string{"a"}, nil, []string{"a", "b"}, []string{"b"}},                // 本地清空：基线里的都删掉
		{[]string{"a"}, []string{"a"}, []string{"a", "b"}, []string{"a", "b"}}, // 本地没动：保留别处的
		{[]string{"İ"}, []string{}, []string{"i\u0307", "z"}, []string{"z"}},   // JS 的 toLowerCase：İ → i + U+0307
	}
	for _, c := range cases {
		if got := services.MergeDraftTags(c.base, c.edited, c.current); !slices.Equal(got, c.want) {
			t.Errorf("MergeDraftTags(%v, %v, %v) = %v，要 %v", c.base, c.edited, c.current, got, c.want)
		}
	}
}
