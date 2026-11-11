package main

// 对应 services/import/{discourse,appinn,linuxdo}.ts：白名单 Discourse 站点的公共帖子抓取核心。
// 标准接口 /t/topic/{id}.json；登录用户通常拿得到原生 Markdown raw，匿名响应常只有 cooked HTML，两种都支持。

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"guizhi-mygo-shell/extract"
)

const discoursePostIDsBatch = 30

type discourseSite struct {
	Platform            string
	Origin              string
	Label               string
	CanonicalURL        func(topicID string) string
	ForbiddenMessage    string
	UnauthorizedMessage string
}

var (
	appinnSite = discourseSite{
		Platform: "appinn", Origin: "https://meta.appinn.net", Label: "小众软件论坛",
		CanonicalURL: func(id string) string { return "https://meta.appinn.net/t/topic/" + id },
	}
	linuxdoSite = discourseSite{
		Platform: "linuxdo", Origin: "https://linux.do", Label: "LINUX DO",
		CanonicalURL:        linuxdoCanonicalURL,
		ForbiddenMessage:    "LINUX DO 访问被 Cloudflare 拦截，请先在「设置 → 采集」完成验证",
		UnauthorizedMessage: "该帖子需要登录后才能查看，请先在「设置 → 采集」登录 LINUX DO",
	}
)

func linuxdoCanonicalURL(topicID string) string { return "https://linux.do/t/topic/" + topicID }

// discoursePostBody 对应 postBody：优先原生 Markdown，其次把 cooked HTML 转成 Markdown。
func discoursePostBody(post *jsObj) string {
	if raw := forumNormalizeText(post.get("raw")); raw != "" {
		return raw
	}
	cooked := fjTrim(post.get("cooked"))
	if cooked == "" {
		return ""
	}
	return forumNormalizeText(extract.DiscourseHTMLToMarkdown(cooked))
}

func discourseAuthor(post *jsObj) string {
	if u := fjTrim(post.get("username")); u != "" {
		return u
	}
	return fjTrim(post.get("name"))
}

func needsAuthenticatedFetch(msg string) bool {
	return strings.Contains(msg, "HTTP 403") || strings.Contains(msg, "HTTP 401") || strings.Contains(msg, "接口未返回 JSON")
}

func describeDiscourseError(err error, site discourseSite) string {
	msg := forumErrMsg(err)
	switch {
	case msg == forumCanceledMsg:
		return msg
	case strings.Contains(msg, "HTTP 403"):
		if site.ForbiddenMessage != "" {
			return site.ForbiddenMessage
		}
		return site.Label + "拒绝访问（HTTP 403），请稍后重试"
	case strings.Contains(msg, "HTTP 401"):
		if site.UnauthorizedMessage != "" {
			return site.UnauthorizedMessage
		}
		return site.Label + "帖子需要登录后才能查看"
	case strings.Contains(msg, "HTTP 404"):
		return "帖子不存在、已被删除或无权访问"
	}
	if m := reHTTP5xxCapture.FindStringSubmatch(msg); m != nil {
		return site.Label + "服务器暂时无响应（HTTP " + m[1] + "），已自动重试仍未成功，稍后再试"
	}
	switch msg {
	case "请求超时":
		return "连接" + site.Label + "超时，已自动重试仍未成功，稍后再试"
	case "连接被中断":
		return "与" + site.Label + "的连接被中断，已自动重试仍未成功，稍后再试"
	}
	return msg
}

func (l *Library) discourseRequest(ctx context.Context, rawURL string, site discourseSite, deps forumDeps) (any, error) {
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
		if msg == forumCanceledMsg {
			return nil, err
		}
		if needsAuthenticatedFetch(msg) && deps.fetchAuthenticatedJSON != nil {
			v, aerr := deps.fetchAuthenticatedJSON(ctx, rawURL)
			if aerr == nil {
				return v, nil
			}
			if aerr.Error() == forumCanceledMsg {
				return nil, aerr
			}
			return nil, errors.New(describeDiscourseError(aerr, site))
		}
		if !isForumTransient(msg) {
			return nil, errors.New(describeDiscourseError(err, site))
		}
		last = err
		logAppErrorf("import", site.Label+"接口请求失败", "第 %d 次请求失败（%s）", attempt+1, msg)
	}
	return nil, errors.New(describeDiscourseError(last, site))
}

func discoursePostsBatchURL(site discourseSite, topicID string, ids []float64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = "post_ids[]=" + encodeURIComponent(jsNumberString(id))
	}
	return site.Origin + "/t/" + encodeURIComponent(topicID) + "/posts.json?" + strings.Join(parts, "&")
}

func (l *Library) discourseLoadAllPosts(ctx context.Context, topicID string, topic *jsObj, site discourseSite, deps forumDeps) ([]*jsObj, error) {
	postStream := jsGet(topic, "post_stream")
	var initial []*jsObj
	if arr, ok := fjArr(jsGet(postStream, "posts")); ok {
		for _, p := range arr {
			if o := fjObj(p); o != nil {
				initial = append(initial, o)
			}
		}
	}
	var stream []float64
	if arr, ok := fjArr(jsGet(postStream, "stream")); ok {
		for _, s := range arr {
			if n, ok := fjNum(s); ok {
				stream = append(stream, n)
			}
		}
	}
	byID := map[float64]*jsObj{}
	for _, p := range initial {
		if id, ok := fjNum(p.get("id")); ok {
			byID[id] = p
		}
	}
	var missing []float64
	for _, id := range stream {
		if _, ok := byID[id]; !ok {
			missing = append(missing, id)
		}
	}
	for offset := 0; offset < len(missing); offset += discoursePostIDsBatch {
		if ctx.Err() != nil {
			return nil, errors.New(forumCanceledMsg)
		}
		end := min(offset+discoursePostIDsBatch, len(missing))
		payload, err := l.discourseRequest(ctx, discoursePostsBatchURL(site, topicID, missing[offset:end]), site, deps)
		if err != nil {
			if err.Error() == forumCanceledMsg {
				return nil, err
			}
			logAppErrorf("import", site.Label+"补拉楼层失败", "已保留已获取部分：%v", err)
			break
		}
		if arr, ok := fjArr(jsGet(jsGet(payload, "post_stream"), "posts")); ok {
			for _, p := range arr {
				if o := fjObj(p); o != nil {
					if id, ok := fjNum(o.get("id")); ok {
						byID[id] = o
					}
				}
			}
		}
	}
	var ordered []*jsObj
	for _, id := range stream {
		if p, ok := byID[id]; ok {
			ordered = append(ordered, p)
		}
	}
	if len(ordered) == 0 {
		out := append([]*jsObj{}, initial...)
		num := func(p *jsObj) float64 { n, _ := fjNum(p.get("post_number")); return n }
		sort.SliceStable(out, func(i, j int) bool { return num(out[i]) < num(out[j]) })
		return out, nil
	}
	return ordered, nil
}

func (l *Library) discourseBuildReplyTo(post *jsObj, byNumber map[float64]*jsObj) *forumReplyTo {
	n, ok := fjNum(post.get("reply_to_post_number"))
	if !ok || n <= 0 {
		return nil
	}
	floor := int(n)
	target, ok := byNumber[n]
	placeholder := "（" + strconv.Itoa(floor) + " 楼）"
	if !ok {
		return &forumReplyTo{Author: "", Floor: &floor, Snippet: placeholder}
	}
	snippet := normalizeForumSnippet(discoursePostBody(target), 200)
	if snippet == "" {
		return &forumReplyTo{Author: discourseAuthor(target), Floor: &floor, Snippet: placeholder}
	}
	return &forumReplyTo{Author: discourseAuthor(target), Floor: &floor, Snippet: snippet}
}

var (
	discourseCategoryMu    sync.Mutex
	discourseCategoryCache = map[string]map[float64]string{}
)

func (l *Library) discourseResolveNode(ctx context.Context, topic *jsObj, site discourseSite, deps forumDeps) (string, error) {
	if c := fjTrim(jsOptGet(jsOptGet(jsGet(topic, "details"), "category"), "name")); c != "" {
		return c, nil
	}
	if cid, ok := fjNum(topic.get("category_id")); ok {
		discourseCategoryMu.Lock()
		categories, cached := discourseCategoryCache[site.Origin]
		discourseCategoryMu.Unlock()
		if !cached {
			payload, err := l.forumGetJSON(deps)(ctx, site.Origin+"/site.json")
			if err != nil {
				if err.Error() == forumCanceledMsg {
					return "", err
				}
				logAppErrorf("import", site.Label+"分类信息读取失败", "继续保留帖子：%v", err)
			} else {
				categories = map[float64]string{}
				if arr, ok := fjArr(jsGet(payload, "categories")); ok {
					for _, e := range arr {
						if id, ok := fjNum(jsGet(e, "id")); ok {
							categories[id] = fjTrim(jsGet(e, "name"))
						}
					}
				}
				// 测试注入的数据不写全局缓存，避免用例之间共享伪站点状态
				if deps.fetchJSON == nil {
					discourseCategoryMu.Lock()
					discourseCategoryCache[site.Origin] = categories
					discourseCategoryMu.Unlock()
				}
			}
		}
		if name := jsTrim(categories[cid]); name != "" {
			return name, nil
		}
	}
	var tags []string
	if arr, ok := fjArr(jsGet(topic, "tags")); ok {
		for _, t := range arr {
			if vTruthy(t) {
				tags = append(tags, jsToString(t))
			}
		}
	}
	return strings.Join(tags, " · "), nil
}

func (l *Library) fetchDiscourseThread(ctx context.Context, topicID string, site discourseSite, deps forumDeps) (*forumThread, error) {
	tv, err := l.discourseRequest(ctx, site.Origin+"/t/topic/"+encodeURIComponent(topicID)+".json", site, deps)
	if err != nil {
		return nil, err
	}
	topic := fjObj(tv)
	if topic == nil {
		topic = newObj()
	}
	if errs, ok := fjArr(jsGet(topic, "errors")); ok && len(errs) > 0 {
		parts := make([]string, len(errs))
		for i, e := range errs {
			parts[i] = jsToString(e)
		}
		return nil, errors.New(strings.Join(parts, "；"))
	}
	posts, err := l.discourseLoadAllPosts(ctx, topicID, topic, site, deps)
	if err != nil {
		return nil, err
	}
	if len(posts) == 0 {
		return nil, errors.New("帖子没有可读内容")
	}
	byNumber := map[float64]*jsObj{}
	for _, p := range posts {
		if n, ok := fjNum(p.get("post_number")); ok {
			byNumber[n] = p
		}
	}
	op := posts[0]
	details := jsGet(topic, "details")
	opAuthor := fjTrim(jsOptGet(jsGet(details, "created_by"), "name"))
	if opAuthor == "" {
		opAuthor = fjTrim(jsOptGet(jsGet(details, "created_by"), "username"))
	}
	if opAuthor == "" {
		opAuthor = discourseAuthor(op)
	}
	var replies []forumReply
	for _, p := range posts {
		n, _ := fjNum(p.get("post_number"))
		if n <= 1 {
			continue
		}
		content := discoursePostBody(p)
		replyTo := l.discourseBuildReplyTo(p, byNumber)
		if content == "" && replyTo == nil {
			continue
		}
		replies = append(replies, forumReply{
			Floor: int(n), Author: discourseAuthor(p), Content: content,
			CreatedAt: forumISOMillis(p.get("created_at")), ReplyTo: replyTo,
		})
	}
	replyCount := max(0, len(posts)-1)
	if n, ok := fjNum(topic.get("posts_count")); ok {
		replyCount = max(0, int(n)-1)
	}
	title := forumNormalizeText(topic.get("title"))
	if title == "" {
		title = fmt.Sprintf("%s帖子 %s", site.Label, topicID)
	}
	node, err := l.discourseResolveNode(ctx, topic, site, deps)
	if err != nil {
		return nil, err
	}
	created := topic.get("created_at")
	if isNullish(created) {
		created = op.get("created_at")
	}
	return &forumThread{
		Platform: site.Platform, TopicID: topicID, Title: title, Author: opAuthor, Node: node,
		CreatedAt: forumISOMillis(created), ReplyCount: replyCount, Content: discoursePostBody(op),
		Replies: replies, ReplyRetention: "all", WebpageURL: site.CanonicalURL(topicID),
	}, nil
}
