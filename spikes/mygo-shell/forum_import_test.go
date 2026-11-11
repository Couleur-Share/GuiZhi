package main

// 论坛连接器接入点测试：URL 导入分发、刷新讨论、重新生成总结（抓取与模型调用均走注入的本地夹具，不碰真实站点）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fgStaticFetcher 按 URL 返回固定 JSON 文本的 fetchJSON；缺失的 URL 返回 HTTP 404。
func fgStaticFetcher(pages map[string]string) func(context.Context, string) (any, error) {
	return func(_ context.Context, url string) (any, error) {
		body, ok := pages[url]
		if !ok {
			return nil, errors.New("HTTP 404")
		}
		return jsParse(body)
	}
}

func TestForumImportURLDispatch(t *testing.T) {
	l := &Library{}
	l.forumPost = forumPostDeps{
		fetch: forumDeps{fetchJSON: fgStaticFetcher(map[string]string{
			"https://www.v2ex.com/api/topics/show.json?id=777":        `[{"title":"导入测试帖","content":"主楼","member":{"username":"u"},"node":{"title":"技术"},"created":1700000000,"replies":1,"url":"https://www.v2ex.com/t/777"}]`,
			"https://www.v2ex.com/api/replies/show.json?topic_id=777": `[{"content":"一楼","member":{"username":"r"},"created":1700000100}]`,
		})},
		getSummaryConfig: func() *aiClientConfig { return nil },
		// NGA 走注入的抓取：不触网，一律 404
		nga: ngaDeps{retryDelays: []time.Duration{}, fetchRawText: func(context.Context, string, safeRequestOptions) (fetchRawTextResult, error) {
			return fetchRawTextResult{}, errors.New("HTTP 404")
		}},
	}
	var stages []string
	ex, err := l.importExtractURL(context.Background(), map[string]any{"sourceInput": " https://www.v2ex.com/t/777#reply1 "}, func(s string) { stages = append(stages, s) })
	if err != nil {
		t.Fatal(err)
	}
	if ex.ItemType != "forum" || ex.Title != "导入测试帖" || ex.SourceURI == nil || *ex.SourceURI != "https://www.v2ex.com/t/777" {
		t.Fatalf("条目字段不符：%+v", ex)
	}
	for _, want := range []string{"> 平台：V2EX · 作者：u · 节点：技术 · 1 条回复", "> 未配置文本模型，讨论总结未生成；原始讨论已完整入库。", "## 正文\n\n主楼", "### 1 楼 · r"} {
		if !strings.Contains(ex.Content, want) {
			t.Errorf("正文缺少 %q：\n%s", want, ex.Content)
		}
	}
	if strings.Join(stages, ",") != "forum-replies" {
		t.Errorf("阶段 = %v", stages)
	}

	// 未接入的平台如实降级，不假装成功
	for url, want := range map[string]string{
		"https://bbs.nga.cn/read.php?tid=123": "论坛帖子抓取失败：[network] 帖子不存在或已被删除",
		"https://www.nodeseek.com/post-123-1": "论坛帖子抓取失败：NodeSeek 浏览器采集服务未就绪",
		"https://www.v2ex.com/t/999":          "论坛帖子抓取失败：帖子不存在或已被删除",
	} {
		ex, err := l.importExtractURL(context.Background(), map[string]any{"sourceInput": url}, nil)
		if err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		if ex.DegradedReason != want {
			t.Errorf("%s: DegradedReason = %q，期望 %q", url, ex.DegradedReason, want)
		}
	}
}

func TestForumRefreshAndRegenerate(t *testing.T) {
	e := newWebSnapEnv(t)
	l := e.l
	ctx := context.Background()
	const uri = "https://meta.appinn.net/t/topic/4242"
	topic := func(posts int) string {
		ps := make([]string, 0, posts)
		ids := make([]string, 0, posts)
		for i := 1; i <= posts; i++ {
			ps = append(ps, `{"id":`+itoa(i)+`,"post_number":`+itoa(i)+`,"username":"u`+itoa(i)+`","created_at":"2024-05-01T12:00:00Z","raw":"第 `+itoa(i)+` 楼"}`)
			ids = append(ids, itoa(i))
		}
		return `{"title":"小众帖子标题足够长","posts_count":` + itoa(posts) + `,"details":{"category":{"name":"分类"}},"post_stream":{"posts":[` + strings.Join(ps, ",") + `],"stream":[` + strings.Join(ids, ",") + `]}}`
	}
	pages := map[string]string{uri: "", "https://meta.appinn.net/t/topic/4242.json": topic(3)}
	l.forumPost = forumPostDeps{
		fetch:            forumDeps{fetchJSON: func(c context.Context, u string) (any, error) { return fgStaticFetcher(pages)(c, u) }},
		getSummaryConfig: func() *aiClientConfig { return &aiClientConfig{Model: "m"} },
	}
	// 先按 3 楼导入成条目，再让帖子长到 5 楼
	ex, err := l.extractForumPost(ctx, forumTarget{Platform: "appinn", TopicID: "4242"}, l.forumPost)
	if err != nil || ex.DegradedReason != "" {
		t.Fatalf("导入失败：%v %+v", err, ex)
	}
	v, err := l.knowledgeCreate(ctx, CreateInput{Title: optStr(ex.Title), Content: optStr(ex.Content), ItemType: optStr("forum")})
	if err != nil {
		t.Fatal(err)
	}
	item := toGeneric(t, v).(map[string]any)
	item["sourceUri"] = uri
	id := item["id"].(string)
	pages["https://meta.appinn.net/t/topic/4242.json"] = topic(5)

	// 刷新讨论：替换逐楼内容、更新回复数
	res := l.refreshForumDiscussion(ctx, item)
	if res["success"] != true {
		t.Fatalf("刷新失败：%v", res)
	}
	got := toGeneric(t, res["item"]).(map[string]any)["content"].(string)
	if !strings.Contains(got, "## 讨论（4 条）") || !strings.Contains(got, "### 5 楼 · u5") || !strings.Contains(got, "· 4 条回复") {
		t.Fatalf("刷新后正文不符：\n%s", got)
	}

	// 重新生成总结：先给条目写入旧总结，刷新应追加过期提示
	l.forumPost.summarize = func(_ context.Context, in forumSummaryInput, _ aiClientConfig) (*forumSummaryResult, error) {
		if len(in.Replies) != 4 || in.Title != item["title"] {
			t.Errorf("总结输入不符：%+v", in)
		}
		return &forumSummaryResult{Summary: "新总结\n- 要点"}, nil
	}
	item["content"] = got
	res = l.regenerateForumSummary(ctx, item)
	if res["success"] != true {
		t.Fatalf("重新生成失败：%v", res)
	}
	got2 := toGeneric(t, res["item"]).(map[string]any)["content"].(string)
	if !strings.Contains(got2, "## 讨论总结\n\n新总结\n- 要点") {
		t.Fatalf("总结未写入：\n%s", got2)
	}
	saved, _ := l.knowledgeGet(ctx, id)
	if toGeneric(t, saved).(map[string]any)["content"] != got2 {
		t.Fatal("总结未落库")
	}

	// 失败路径：站点抓取失败、不支持的平台、模型未配置、总结为空
	pages["https://meta.appinn.net/t/topic/4242.json"] = `{"errors":["帖子已删除"]}`
	if r := l.refreshForumDiscussion(ctx, item); r["success"] != false || r["error"] != "刷新小众软件论坛讨论失败：帖子已删除" {
		t.Errorf("抓取失败 = %v", r)
	}
	if r := l.regenerateForumSummary(ctx, item); r["success"] != false || r["error"] != "无法获取完整的小众软件论坛讨论：帖子已删除" {
		t.Errorf("重新生成抓取失败 = %v", r)
	}
	v2ex := map[string]any{"id": id, "itemType": "forum", "sourceUri": "https://www.v2ex.com/t/1", "content": got2, "title": "t"}
	if r := l.refreshForumDiscussion(ctx, v2ex); r["error"] != "仅 LINUX DO、小众软件论坛与 2Libra 条目支持刷新讨论" {
		t.Errorf("不支持平台 = %v", r)
	}
	l.forumPost.getSummaryConfig = func() *aiClientConfig { return nil }
	if r := l.regenerateForumSummary(ctx, v2ex); r["notConfigured"] != true {
		t.Errorf("未配置模型 = %v", r)
	}
	l.forumPost.getSummaryConfig = func() *aiClientConfig { return &aiClientConfig{Model: "m"} }
	l.forumPost.summarize = func(context.Context, forumSummaryInput, aiClientConfig) (*forumSummaryResult, error) { return nil, nil }
	if r := l.regenerateForumSummary(ctx, v2ex); r["error"] != "模型未返回有效的讨论总结" {
		t.Errorf("总结为空 = %v", r)
	}
	if r := l.regenerateForumSummary(ctx, map[string]any{"id": id, "content": "只有元数据\n", "title": "t"}); r["error"] != "该条目没有可用于总结的讨论内容" {
		t.Errorf("无讨论内容 = %v", r)
	}
}

func TestUpdateForumReplyCount(t *testing.T) {
	cases := map[string]string{
		"> 平台：V2EX · 作者：a · 节点：n · 3 条回复\n> 发布：2024-01-01": "> 平台：V2EX · 作者：a · 节点：n · 9 条回复\n> 发布：2024-01-01",
		"> 平台：NGA · 作者：a · 12 条回复（入库保留楼主 2 条）\n> 发布：x":     "> 平台：NGA · 作者：a · 9 条回复（入库保留楼主 2 条）\n> 发布：x",
		"正文里写了 3 条回复不应被改":                                  "正文里写了 3 条回复不应被改",
		"> 平台：V2EX · 3 条回复了很多":                             "> 平台：V2EX · 3 条回复了很多",
		"> 平台：V2EX · 3 条回复\n> 平台：X · 4 条回复":                "> 平台：V2EX · 9 条回复\n> 平台：X · 4 条回复",
	}
	for in, want := range cases {
		if got := updateForumReplyCount(in, 9); got != want {
			t.Errorf("updateForumReplyCount(%q) = %q，期望 %q", in, got, want)
		}
	}
}

var _ = json.Marshal

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
