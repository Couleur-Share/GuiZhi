package main

// 对应 services/import/twolibra.ts：2Libra 公开主楼 + 平铺评论接口。

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
)

const (
	twolibraAPIBase         = "https://2libra.com/api"
	twolibraCommentPageSize = 50
	twolibraMaxCommentPages = 200 // 防止异常 total_pages 让采集无限请求
)

func twolibraCanonicalURL(nodeSlug, topicID string) string {
	return "https://2libra.com/post/" + encodeURIComponent(nodeSlug) + "/" + encodeURIComponent(topicID)
}

func describeTwolibraError(err error) string {
	msg := forumErrMsg(err)
	switch {
	case msg == forumCanceledMsg:
		return msg
	case strings.Contains(msg, "HTTP 404"):
		return "帖子不存在或已被删除"
	case strings.Contains(msg, "HTTP 403"):
		return "帖子需要登录或没有访问权限"
	case strings.Contains(msg, "HTTP 429"):
		return "2Libra 请求过于频繁，稍后重试"
	case reHTTP5xx.MatchString(msg) || msg == "请求超时":
		return "2Libra 服务器暂时无响应，已自动重试仍未成功，稍后再试"
	}
	return msg
}

func twolibraTransient(msg string) bool {
	return strings.Contains(msg, "HTTP 429") || isForumTransient(msg)
}

func (l *Library) twolibraRequest(ctx context.Context, rawURL string, deps forumDeps) (any, error) {
	get := l.forumGetJSON(deps)
	delays := deps.delays()
	var last error
	for attempt := 0; attempt <= len(delays); attempt++ {
		if attempt > 0 {
			if err := forumWait(ctx, delays[attempt-1]); err != nil {
				return nil, err
			}
		}
		v, err := get(ctx, rawURL)
		if err == nil {
			return v, nil
		}
		msg := err.Error()
		if msg == forumCanceledMsg || !twolibraTransient(msg) {
			return nil, errors.New(describeTwolibraError(err))
		}
		last = err
		logAppErrorf("import", "2Libra 接口请求失败", "第 %d 次请求失败（%s）", attempt+1, msg)
	}
	return nil, errors.New(describeTwolibraError(last))
}

// twolibraUnwrap 对应 unwrap：信封 {c:0, d} 以外都按不可用处理。
func twolibraUnwrap(payload any, unavailable string) (any, error) {
	o := fjObj(payload)
	var c any = jsUndefined
	var d any = jsUndefined
	var m any = jsUndefined
	if o != nil {
		c, d, m = o.get("c"), o.get("d"), o.get("m")
	}
	if n, ok := fjNum(c); !ok || n != 0 || isNullish(d) {
		if msg := forumNormalizeText(m); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, errors.New(unavailable)
	}
	return d, nil
}

func twolibraCommentAuthor(c *jsObj) string {
	if u := fjTrim(jsOptGet(c.get("author"), "username")); u != "" {
		return u
	}
	if vTruthy(c.get("alias_id")) {
		return "匿名用户"
	}
	return "未知用户"
}

// twolibraFloor 对应 `flat_floor ?? floor ?? fallback`。
func twolibraFloor(c *jsObj) (int, bool) {
	for _, k := range []string{"flat_floor", "floor"} {
		if v := c.get(k); !isNullish(v) {
			if n, ok := fjNum(v); ok {
				return int(n), true
			}
		}
	}
	return 0, false
}

func twolibraReplyTo(c *jsObj) *forumReplyTo {
	parentV := c.get("reply_comment")
	if isNullish(parentV) {
		parentV = c.get("parent")
	}
	parent := fjObj(parentV)
	if parent == nil || vTruthy(parent.get("is_deleted")) {
		return nil
	}
	snippet := normalizeForumSnippet(forumNormalizeText(parent.get("content")), 200)
	author := fjTrim(jsOptGet(parent.get("author"), "username"))
	if snippet == "" && author == "" {
		return nil
	}
	if author == "" {
		author = "未知用户"
	}
	r := &forumReplyTo{Author: author, Snippet: snippet}
	if f, ok := twolibraFloor(parent); ok {
		r.Floor = &f
	}
	return r
}

func twolibraToReply(c *jsObj, fallbackFloor int) *forumReply {
	if vTruthy(c.get("is_deleted")) {
		return nil
	}
	content := forumNormalizeText(c.get("content"))
	if content == "" {
		return nil
	}
	floor, ok := twolibraFloor(c)
	if !ok {
		floor = fallbackFloor
	}
	return &forumReply{
		Floor: floor, Author: twolibraCommentAuthor(c), Content: content,
		CreatedAt: forumISOMillis(c.get("created_at")), ReplyTo: twolibraReplyTo(c),
	}
}

// fetchTwolibraThread 抓取公开主楼和全部平铺评论；分页缺失时保留已取得内容并给出 warning。
func (l *Library) fetchTwolibraThread(ctx context.Context, topicID string, deps forumDeps) (*forumThread, error) {
	pv, err := l.twolibraRequest(ctx, twolibraAPIBase+"/posts/"+encodeURIComponent(topicID), deps)
	if err != nil {
		return nil, err
	}
	pd, err := twolibraUnwrap(pv, "帖子不存在、已被删除或需要登录后查看")
	if err != nil {
		return nil, err
	}
	post := fjObj(pd)
	if post == nil {
		post = newObj()
	}
	title := fjTrim(post.get("title"))
	slug := fjTrim(jsOptGet(post.get("node"), "slug"))
	if title == "" || slug == "" {
		return nil, errors.New("2Libra 帖子响应缺少标题或节点信息")
	}
	pageSize := deps.commentPageSize
	if pageSize == 0 {
		pageSize = twolibraCommentPageSize
	}
	pageLimit := deps.maxCommentPages
	if pageLimit == 0 {
		pageLimit = twolibraMaxCommentPages
	}
	var replies []forumReply
	seen := map[string]bool{}
	var warnings []string
	totalPages := 1.0
	var reportedTotal *float64

	for page := 1; float64(page) <= math.Min(totalPages, float64(pageLimit)); page++ {
		if ctx.Err() != nil {
			return nil, errors.New(forumCanceledMsg)
		}
		u := twolibraAPIBase + "/comments/list/flat?post_short_id=" + encodeURIComponent(topicID) +
			"&page=" + strconv.Itoa(page) + "&limit=" + strconv.Itoa(pageSize)
		raw, err := l.twolibraRequest(ctx, u, deps)
		var data any
		if err == nil {
			data, err = twolibraUnwrap(raw, "评论列表不可用")
		}
		if err != nil {
			if err.Error() == forumCanceledMsg {
				return nil, err
			}
			warnings = append(warnings, "评论第 "+strconv.Itoa(page)+" 页抓取失败（"+err.Error()+"），讨论可能不完整")
			break
		}
		totalPages = 1
		if tp, ok := fjNum(jsOptGet(data, "total_pages")); ok {
			totalPages = math.Max(1, tp)
		}
		if t, ok := fjNum(jsOptGet(data, "total")); ok {
			reportedTotal = &t
		}
		if items, ok := fjArr(jsOptGet(data, "items")); ok {
			for _, it := range items {
				c := fjObj(it)
				if c == nil {
					continue
				}
				if key := fjTrim(c.get("id")); key != "" {
					if seen[key] {
						continue
					}
					seen[key] = true
				}
				if r := twolibraToReply(c, len(replies)+1); r != nil {
					replies = append(replies, *r)
				}
			}
		}
	}
	if totalPages > float64(pageLimit) {
		warnings = append(warnings, "评论超过 "+strconv.Itoa(pageLimit*pageSize)+" 条安全上限，超出部分未入库")
	}

	shortID := fjTrim(post.get("short_id"))
	if shortID == "" {
		shortID = topicID
	}
	node := fjTrim(jsOptGet(post.get("node"), "name"))
	if node == "" {
		node = slug
	}
	replyCount := len(replies)
	if n, ok := fjNum(post.get("comment_count")); ok && !isNullish(post.get("comment_count")) {
		replyCount = int(n)
	} else if reportedTotal != nil {
		replyCount = int(*reportedTotal)
	}
	return &forumThread{
		Platform: "twolibra", TopicID: shortID, Title: title,
		Author: fjTrim(jsOptGet(post.get("author"), "username")), Node: node,
		CreatedAt: forumISOMillis(post.get("created_at")), ReplyCount: replyCount,
		Content: forumNormalizeText(post.get("content")), Replies: replies, ReplyRetention: "all",
		WebpageURL: twolibraCanonicalURL(slug, shortID), WarningReason: strings.Join(warnings, "；"),
	}, nil
}
