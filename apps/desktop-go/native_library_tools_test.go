package main

import (
	"context"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// webAdapterForTest 在网页快照测试环境的桥上建适配器：桥的计数表要有（Invoke 会记频道调用次数）。
func webAdapterForTest(e *webSnapEnv) libraryAdapter {
	e.b.seen, e.b.missing = map[string]int{}, map[string]int{}
	return libraryAdapter{b: e.b}
}

// 来源评论经真实处理器往返：落库的评论按点赞数降序读回，字段（含可空的发布时间）都能解码；没有评论是空切片而不是错误。
func TestLibraryAdapterSourceCommentsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b, l := newLibraryOpsAdapterForTest(t)
	l.registerPlatformCapture(b)
	id := createOpsItem(t, b, map[string]any{"title": "小红书笔记", "content": "正文"})

	if got, err := svc.SourceComments(ctx, id); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("没有评论应是空切片：%v %v", got, err)
	}
	published := int64(1700000000000)
	if err := l.sourceCommentsUpsert(ctx, id, "xiaohongshu", []capturedComment{
		{ExternalID: "c1", AuthorName: "甲", Content: "第一条", LikeCount: 3, PublishedAt: &published},
		{ExternalID: "c2", AuthorName: "", Content: "第二条\n换行", LikeCount: 9},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.SourceComments(ctx, id)
	if err != nil || len(got) != 2 {
		t.Fatalf("读评论：%+v %v", got, err)
	}
	if got[0].ExternalID != "c2" || got[0].LikeCount != 9 || got[0].AuthorName != "" || got[0].PublishedAt != nil || got[0].Content != "第二条\n换行" {
		t.Fatalf("点赞多的在前、空作者与空发布时间解码成零值：%+v", got[0])
	}
	if got[1].ExternalID != "c1" || got[1].PublishedAt == nil || *got[1].PublishedAt != published || got[1].ItemID != id || got[1].Platform != "xiaohongshu" || got[1].CapturedAt == 0 {
		t.Fatalf("第二条的字段：%+v", got[1])
	}
}

// 重新采集的两类前置失败原样带回（不联网）：档位只能是 10、20、50；没有平台来源的条目不能采。
func TestLibraryAdapterRefreshSourceCommentsRejectsBeforeNetwork(t *testing.T) {
	ctx := t.Context()
	svc, b, l := newLibraryOpsAdapterForTest(t)
	l.registerPlatformCapture(b)
	id := createOpsItem(t, b, map[string]any{"title": "手工笔记", "content": "正文"})

	if _, err := svc.RefreshSourceComments(ctx, id, 7); err == nil || !strings.Contains(err.Error(), "无效的评论数量") {
		t.Fatalf("档位无效应失败：%v", err)
	}
	if _, err := svc.RefreshSourceComments(ctx, id, 0); err == nil || !strings.Contains(err.Error(), "无效的评论数量") {
		t.Fatalf("0 条也无效：%v", err)
	}
	if _, err := svc.RefreshSourceComments(ctx, id, 20); err == nil || !strings.Contains(err.Error(), "没有可补采评论的平台来源") {
		t.Fatalf("没有平台来源应失败：%v", err)
	}
}

// 原文版本经真实处理器往返：版本新的在前，带快照的版本 HasSnapshot 为真（无效快照被处理器移除），比较基线与待确认标记都能解码；
// 条目不存在作为 error 返回，不是空数据。
func TestLibraryAdapterWebVersionsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	e := newWebSnapEnv(t)
	svc := webAdapterForTest(e)
	name, hash := e.image("abc")
	e.version("v1", 1000, snapshotFor(name, hash, "<p>a</p>", ""), true)
	e.version("v3", 3000, nil, false)

	got, err := svc.WebVersions(ctx, e.itemID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 2 || got.Versions[0].ID != "v3" || got.Versions[1].ID != "v1" {
		t.Fatalf("版本按采集时间降序：%+v", got.Versions)
	}
	if got.Versions[0].HasSnapshot() || !got.Versions[1].HasSnapshot() {
		t.Fatalf("快照有无：v3=%v v1=%v", got.Versions[0].HasSnapshot(), got.Versions[1].HasSnapshot())
	}
	v := got.Versions[0]
	if v.Kind != services.WebVersionRemote || !v.Complete || v.EngineVersion != "wechat/1" || v.Markdown != "md" || v.CapturedAt != 3000 || v.ItemID != e.itemID {
		t.Fatalf("版本字段：%+v", v)
	}
	if got.Content != "正文内容" || got.Title != "公众号文章" || got.ContentHash != webContentHash("正文内容") || got.SummaryStale || !got.PendingVersion {
		t.Fatalf("比较基线：%+v", got)
	}
	if _, err := svc.WebVersions(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "条目不存在") {
		t.Fatalf("条目不存在应是错误：%v", err)
	}
}

// 采用经真实处理器往返：基线与此刻一致就把版本正文采用为条目正文；基线过期原样带回处理器的原因，什么也不写。
func TestLibraryAdapterAdoptWebVersionAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	e := newWebSnapEnv(t)
	e.l.registerCrawl(e.b)
	t.Cleanup(func() { e.l.crawl.close(context.Background()) })
	svc := webAdapterForTest(e)
	e.version("v1", 1000, nil, true)
	e.version("v2", 2000, nil, false)
	in := services.LibraryAdoptWebVersionInput{ItemID: e.itemID, VersionID: "v2", ExpectedContentHash: webContentHash("正文内容"), ExpectedTitle: "公众号文章"}

	stale := in
	stale.ExpectedContentHash = webContentHash("别的正文")
	if err := svc.AdoptWebVersion(ctx, stale); err == nil || !strings.Contains(err.Error(), "比较期间发生变化") {
		t.Fatalf("基线过期应失败：%v", err)
	}
	if after, err := svc.WebVersions(ctx, e.itemID); err != nil || after.Content != "正文内容" {
		t.Fatalf("失败时什么也不能写：%+v %v", after, err)
	}
	missing := in
	missing.VersionID = "ghost"
	if err := svc.AdoptWebVersion(ctx, missing); err == nil || !strings.Contains(err.Error(), "条目或完整版本不存在") {
		t.Fatalf("版本不存在应失败：%v", err)
	}
	if err := svc.AdoptWebVersion(ctx, in); err != nil {
		t.Fatalf("采用：%v", err)
	}
	after, err := svc.WebVersions(ctx, e.itemID)
	if err != nil || after.Content != "md" || after.ContentHash != webContentHash("md") {
		t.Fatalf("采用后正文应是版本的 markdown：%+v %v", after, err)
	}
}
