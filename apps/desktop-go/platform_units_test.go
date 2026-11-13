package main

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

func nodeseekFixture(topic string, floors []int, pageLinks string) string {
	var sb strings.Builder
	sb.WriteString(`<html><head><title>帖子 - NodeSeek</title></head><body>`)
	sb.WriteString(`<h1><a class="post-title-link" href="/post-` + topic + `-1">测试帖子</a></h1>`)
	sb.WriteString(`<div class="content-category"><a href="/categories/tech">技术</a></div>`)
	for _, f := range floors {
		id := strconv.Itoa(f)
		sb.WriteString(`<div class="content-item" id="` + id + `"><span class="author-name">用户` + id + `</span>`)
		sb.WriteString(`<span class="date-created"><time datetime="2026-01-02T03:04:05Z"></time></span>`)
		sb.WriteString(`<article class="post-content"><p>第` + id + `层内容 <a href="/user/1">链接</a><img src="//img.example.com/a.png"><script>alert(1)</script></p></article></div>`)
	}
	sb.WriteString(`<nav aria-label="pagination">` + pageLinks + `</nav></body></html>`)
	return sb.String()
}

func TestParseNodeseekPage(t *testing.T) {
	src := nodeseekFixture("123", []int{0, 1, 2}, `<a href="/post-123-1">1</a><a href="/post-123-3">3</a><a href="https://evil.example.com/post-123-9">9</a><a href="/post-999-8">8</a>`)
	p, err := parseNodeseekPage(src, "123", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "测试帖子" || p.Node != "技术" || p.PageCount != 3 || len(p.Posts) != 3 {
		t.Fatalf("解析结果不符：%+v", p)
	}
	m := p.Posts[0]
	if m.Floor != 0 || m.Author != "用户0" || m.CreatedAt != 1767323045000 {
		t.Fatalf("主楼不符：%+v", m)
	}
	if !strings.Contains(m.Content, "https://www.nodeseek.com/user/1") || !strings.Contains(m.Content, "https://img.example.com/a.png") || strings.Contains(m.Content, "alert") {
		t.Fatalf("链接应补全为绝对地址并去掉脚本：%q", m.Content)
	}
}

func TestParseNodeseekRejects(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"验证页":    {`<html><head><title>Just a moment...</title></head><body><div id="cf-wrapper"></div></body></html>`, "验证"},
		"结构变化":   {`<html><head><title>x</title></head><body>hello</body></html>`, "不可读或页面结构已变化"},
		"无主楼":    {nodeseekFixture("123", []int{1}, ""), "主楼或回复未加载"},
		"空正文":    {strings.Replace(nodeseekFixture("123", []int{0}, ""), "<p>第0层内容", "<p>", 1), ""},
		"标题指向别帖": {strings.Replace(nodeseekFixture("123", []int{0}, ""), "/post-123-1", "/post-124-1", 1), "不可读"},
	}
	for name, c := range cases {
		_, err := parseNodeseekPage(c.src, "123", 1)
		if name == "空正文" {
			// 正文只剩链接与图片时仍有 Markdown，此用例仅确认不会 panic
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s：期望含 %q，实际 %v", name, c.want, err)
		}
	}
}

func TestFetchNodeseekThread(t *testing.T) {
	pages := map[string]string{
		nodeseekCanonicalURL("77", 1): nodeseekFixture("77", []int{0, 1}, `<a href="/post-77-2">2</a>`),
		nodeseekCanonicalURL("77", 2): nodeseekFixture("77", []int{2, 3}, `<a href="/post-77-2">2</a>`),
	}
	var order []string
	th, err := fetchNodeseekThread(context.Background(), "77", func(u string) (string, error) {
		order = append(order, u)
		return pages[u], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if th.Title != "测试帖子" || th.ReplyCount != 3 || len(th.Replies) != 3 || th.Replies[0].Floor != 1 || th.Replies[2].Floor != 3 || th.Platform != "nodeseek" {
		t.Fatalf("帖子不符：%+v", th)
	}
	if len(order) != 2 || !strings.HasSuffix(order[1], "-2") {
		t.Fatalf("应按页串行抓取：%v", order)
	}
	if _, err := fetchNodeseekThread(context.Background(), "abc", nil); err == nil {
		t.Fatal("应拒绝非法帖子编号")
	}
	// 任意一页失败则整次失败
	_, err = fetchNodeseekThread(context.Background(), "77", func(u string) (string, error) {
		if strings.HasSuffix(u, "-2") {
			return "", context.DeadlineExceeded
		}
		return pages[u], nil
	})
	if err == nil {
		t.Fatal("第 2 页失败应整体失败")
	}
}

func TestScanPlatformComments(t *testing.T) {
	payload, err := jsParse(`{"data":{"comments":[
		{"comment_id":"a","content":"热门","like_count":"12","create_time":1700000000,"user":{"nickname":"甲"},"sub_comments":[{"comment_id":"a1","content":"子评论","like_count":100}]},
		{"cid":2,"text":"  数字ID\u0007  ","digg_count":3,"create_time":1700000000123,"author":{"name":"乙"}},
		{"comment_id":"p","content":"回复型","parent_comment_id":"a","like_count":50},
		{"comment_id":"z","content":"根评论零父","parent_comment_id":"0","like_count":1},
		{"comment_id":"e","content":"","like_count":9}
	]}}`)
	if err != nil {
		t.Fatal(err)
	}
	got := scanPlatformComments([]any{payload}, 10)
	ids := []string{}
	for _, c := range got {
		ids = append(ids, c.ExternalID)
	}
	if strings.Join(ids, ",") != "a,2,z" {
		t.Fatalf("顶层评论顺序/过滤不符：%v", ids)
	}
	if got[0].LikeCount != 12 || *got[0].PublishedAt != 1700000000000 || got[1].Content != "数字ID" || got[1].AuthorName != "乙" || *got[1].PublishedAt != 1700000000123 {
		t.Fatalf("字段不符：%+v", got)
	}
	if n := len(scanPlatformComments([]any{payload}, 2)); n != 2 {
		t.Fatalf("limit 应生效：%d", n)
	}
}

func TestIndicatesLoginRequired(t *testing.T) {
	yes := []string{`{"code":10001,"msg":"未登录"}`, `{"data":{"status_msg":"login_required"}}`, `[{"message":"请先登录"}]`}
	no := []string{`{"status_code":2}`, `{"code":0,"msg":"ok"}`, `{"title":"如何登录银行"}`, `[]`, `null`}
	for _, s := range yes {
		v, _ := jsParse(s)
		if !indicatesLoginRequired(v, 0) {
			t.Errorf("应判定为需登录：%s", s)
		}
	}
	for _, s := range no {
		v, _ := jsParse(s)
		if indicatesLoginRequired(v, 0) {
			t.Errorf("误判为需登录：%s", s)
		}
	}
	v, _ := jsParse(`{"code":10001,"msg":"未登录"}`)
	if !indicatesLoginRequired(v, 0) {
		t.Error("明确的未登录响应必须识别")
	}
	for raw, want := range map[string]bool{"https://www.douyin.com/login": true, "https://x.com/passport/login?a=1": true, "https://www.douyin.com/video/1": false, "not a url": false} {
		if isLoginPage(raw) != want {
			t.Errorf("isLoginPage(%s) 期望 %v", raw, want)
		}
	}
}

func TestPlatformFromAuthenticatedURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.xiaohongshu.com/explore/1": "xiaohongshu", "https://xhslink.com/abc": "xiaohongshu",
		"https://www.douyin.com/video/1": "douyin", "https://iesdouyin.com/share/video/1": "douyin", "https://www.iesdouyin.com/x": "",
		"https://linux.do/t/1": "linuxdo", "https://evildouyin.com/": "", "https://example.com/": "",
	} {
		if got := platformFromAuthenticatedURL(raw); got != want {
			t.Errorf("%s：期望 %q 得到 %q", raw, want, got)
		}
	}
}

func TestSourceCommentsDB(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "c.db"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Execute(ctx, `CREATE TABLE source_comments (id TEXT PRIMARY KEY, item_id TEXT NOT NULL, platform TEXT NOT NULL, external_id TEXT NOT NULL,
		author_name TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '', like_count INTEGER NOT NULL DEFAULT 0, published_at INTEGER, captured_at INTEGER NOT NULL,
		UNIQUE(item_id, platform, external_id))`); err != nil {
		t.Fatal(err)
	}
	l := &Library{db: db}
	pub := int64(1700000000000)
	if err := l.sourceCommentsUpsert(ctx, "item1", "douyin", []capturedComment{{ExternalID: "a", AuthorName: "甲", Content: "旧", LikeCount: 1, PublishedAt: &pub}, {ExternalID: "b", Content: "乙", LikeCount: 5}}); err != nil {
		t.Fatal(err)
	}
	// 再次写入同一条应更新而非新增
	if err := l.sourceCommentsUpsert(ctx, "item1", "douyin", []capturedComment{{ExternalID: "a", AuthorName: "甲", Content: "新", LikeCount: 9}}); err != nil {
		t.Fatal(err)
	}
	list, err := l.sourceCommentsList(ctx, "item1")
	if err != nil || len(list) != 2 {
		t.Fatalf("应有 2 条：%v %v", list, err)
	}
	first := list[0].(*jsObj)
	if jsToString(first.get("externalId")) != "a" || jsToString(first.get("content")) != "新" || first.get("publishedAt") != nil {
		t.Fatalf("更新或排序不符：%s", func() string { s, _ := jsStringify(first); return s }())
	}
	if other, _ := l.sourceCommentsList(ctx, "item2"); len(other) != 0 {
		t.Fatal("不同条目互不可见")
	}
	if err := l.sourceCommentsUpsert(ctx, "item1", "douyin", nil); err != nil {
		t.Fatal("空列表应直接成功")
	}
}
