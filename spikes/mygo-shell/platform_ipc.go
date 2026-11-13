package main

// 登录态采集的 IPC 与导入接入：对应 ipc/platform-capture.ipc.ts（平台发现已移除，不提供）、
// platform-capture/authenticated-platforms.ts、packages/db 的 SourceCommentDB。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

func requirePlatformArg(v any) (string, error) {
	p, ok := isPlatformCapturePlatform(v)
	if !ok {
		return "", errors.New("不支持的平台")
	}
	return p, nil
}

// ---- 来源评论库 ----

func (l *Library) sourceCommentsList(ctx context.Context, itemID string) ([]any, error) {
	rows, err := l.db.Query(ctx, `SELECT id, item_id, platform, external_id, author_name, content, like_count, published_at, captured_at
         FROM source_comments WHERE item_id = ? ORDER BY like_count DESC, captured_at DESC`, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		o := newObj()
		for i, k := range []string{"id", "itemId", "platform", "externalId", "authorName", "content", "likeCount", "publishedAt", "capturedAt"} {
			o.set(k, r[i])
		}
		out = append(out, o)
	}
	return out, nil
}

func (l *Library) sourceCommentsUpsert(ctx context.Context, itemID, platform string, comments []capturedComment) error {
	if len(comments) == 0 {
		return nil
	}
	now := nowMs()
	stmts := make([]sqlite.Statement, 0, len(comments))
	for _, c := range comments {
		var published any
		if c.PublishedAt != nil {
			published = *c.PublishedAt
		}
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO source_comments
             (id, item_id, platform, external_id, author_name, content, like_count, published_at, captured_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT(item_id, platform, external_id) DO UPDATE SET
             author_name = excluded.author_name, content = excluded.content, like_count = excluded.like_count,
             published_at = excluded.published_at, captured_at = excluded.captured_at`,
			Args: []any{newUUID(), itemID, platform, c.ExternalID, utf16Slice(c.AuthorName, 200), utf16Slice(c.Content, 5000), max(c.LikeCount, 0), published, now},
		})
	}
	_, err := l.db.Transaction(ctx, stmts)
	return err
}

// ---- 认证采集 → 导入连接器 ----

func platformFromAuthenticatedURL(raw string) string {
	u, err := parseWhatwgURL(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "xhslink.com" || host == "xhslink.cn" || strings.HasSuffix(host, ".xiaohongshu.com"):
		return "xiaohongshu"
	case host == "iesdouyin.com" || strings.HasSuffix(host, ".douyin.com"):
		return "douyin"
	case host == "linux.do" || strings.HasSuffix(host, ".linux.do"):
		return "linuxdo"
	}
	return ""
}

// 认证模式优先消费页面自身发出的 JSON 响应；只把已拿到的对象包成现有解析器所需的数据外壳，不构造请求，也不接触签名参数。
func findJSRecord(v any, pred func(*jsObj) bool, depth int) *jsObj {
	if depth > 10 || isNullish(v) {
		return nil
	}
	if arr, ok := v.(jsArr); ok {
		for _, e := range arr {
			if m := findJSRecord(e, pred, depth+1); m != nil {
				return m
			}
		}
		return nil
	}
	o, ok := v.(*jsObj)
	if !ok {
		return nil
	}
	if pred(o) {
		return o
	}
	for _, k := range o.keys() {
		if m := findJSRecord(o.get(k), pred, depth+1); m != nil {
			return m
		}
	}
	return nil
}

func scriptAssign(name string, v any) string {
	s, _ := jsStringify(v)
	return "<script>" + name + " = " + s + "</script>"
}

func douyinNetworkResponseHTML(payloads []any) string {
	for _, p := range payloads {
		if loader := findJSRecord(p, func(r *jsObj) bool { return vTruthy(r.get("loaderData")) }, 0); loader != nil {
			return scriptAssign("window._ROUTER_DATA", loader)
		}
		if info := findJSRecord(p, func(r *jsObj) bool { _, ok := r.get("item_list").(jsArr); return ok }, 0); info != nil {
			capture := newObj()
			capture.set("videoInfoRes", info)
			loader := newObj()
			loader.set("capture", capture)
			data := newObj()
			data.set("loaderData", loader)
			return scriptAssign("window._ROUTER_DATA", data)
		}
		if detail := findJSRecord(p, func(r *jsObj) bool { return vTruthy(r.get("aweme_detail")) }, 0); detail != nil {
			info := newObj()
			info.set("item_list", jsArr{detail.get("aweme_detail")})
			capture := newObj()
			capture.set("videoInfoRes", info)
			loader := newObj()
			loader.set("capture", capture)
			data := newObj()
			data.set("loaderData", loader)
			return scriptAssign("window._ROUTER_DATA", data)
		}
	}
	return ""
}

func xhsNetworkResponseHTML(payloads []any) string {
	for _, p := range payloads {
		state := findJSRecord(p, func(r *jsObj) bool {
			note, ok := r.get("note").(*jsObj)
			return ok && vTruthy(note.get("noteDetailMap"))
		}, 0)
		if state != nil {
			return scriptAssign("window.__INITIAL_STATE__", state)
		}
		if m := findJSRecord(p, func(r *jsObj) bool { return vTruthy(r.get("noteDetailMap")) }, 0); m != nil {
			wrap := newObj()
			wrap.set("note", m)
			return scriptAssign("window.__INITIAL_STATE__", wrap)
		}
	}
	return ""
}

func (l *Library) fetchAuthenticatedDouyin(ctx context.Context, url string) (*douyinAweme, error) {
	page, err := l.platformCap.capturePage(ctx, "douyin", url)
	if err != nil {
		return nil, err
	}
	id := extractAwemeID(page.FinalURL)
	if id == "" {
		id = extractAwemeID(url)
	}
	if id == "" {
		return nil, pcErr("platform_changed", "无法从登录态页面识别抖音作品 ID")
	}
	html := douyinNetworkResponseHTML(page.JSONPayloads)
	if html == "" {
		html = page.HTML
	}
	aweme, perr := l.parseDouyinRouterData(html, id)
	if perr != nil {
		return nil, pcErr("platform_changed", "抖音登录态页面结构可能已变更")
	}
	return aweme, nil
}

func (l *Library) fetchAuthenticatedXiaohongshu(ctx context.Context, url string) (*xiaohongshuNote, error) {
	page, err := l.platformCap.capturePage(ctx, "xiaohongshu", url)
	if err != nil {
		return nil, err
	}
	id := extractXhsNoteID(page.FinalURL)
	if id == "" {
		id = extractXhsNoteID(url)
	}
	html := xhsNetworkResponseHTML(page.JSONPayloads)
	if html == "" {
		html = page.HTML
	}
	note, perr := l.parseXiaohongshuNote(html, id)
	if perr != nil {
		return nil, pcErr("platform_changed", "小红书登录态页面结构可能已变更")
	}
	return note, nil
}

// wirePlatformCapture 把登录态采集接入导入连接器与评论补采。
func (l *Library) wirePlatformCapture() {
	s := l.platformCap
	l.forumPost.fetchNodeseek = func(ctx context.Context, topicID string) (*forumThread, error) {
		return s.captureNodeseek(ctx, topicID)
	}
	l.forumPost.fetchAuthenticatedJSON = func(ctx context.Context, url string) (any, error) {
		return s.fetchJSONViaSession(ctx, "linuxdo", url)
	}
	l.captureComments = func(ctx context.Context, task map[string]any, itemID string) error {
		input, _ := task["sourceInput"].(string)
		platform := platformFromAuthenticatedURL(input)
		if platform == "" || task["captureStrategy"] != "authenticated" {
			return errors.New("热门评论需要受支持平台的登录态采集")
		}
		if platform == "linuxdo" { // LINUX DO 的全部楼层已随论坛正文入库
			return nil
		}
		limit, _ := task["commentLimit"].(int64)
		comments, err := s.captureSourceComments(ctx, platform, input, int(limit))
		if err != nil {
			return err
		}
		return l.sourceCommentsUpsert(ctx, itemID, platform, comments)
	}
}

// applyPlatformVideoDeps 给视频连接器接上平台浏览器：抖音公开作品回退；认证模式下改走登录态页面。
func (l *Library) applyPlatformVideoDeps(d *videoDeps, authenticated bool, platform string) {
	if l.platformCap == nil {
		return
	}
	if d.douyin.captureDetail == nil {
		d.douyin.captureDetail = func(ctx context.Context, awemeID string) (string, error) {
			return l.platformCap.captureDouyinDetail(ctx, awemeID)
		}
	}
	if !authenticated {
		return
	}
	switch platform {
	case "douyin":
		d.douyin.fetch = l.fetchAuthenticatedDouyin
	case "xiaohongshu":
		d.xiaohongshu.fetch = l.fetchAuthenticatedXiaohongshu
	}
}

func (l *Library) registerPlatformCapture(b *Bridge) {
	s := l.platformCap
	b.handlers["platformCapture:status"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return s.statuses(), nil
	}
	b.handlers["platformCapture:login"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		var platformArg any = raw
		force, verification := false, ""
		if rec, ok := raw.(*jsObj); ok {
			platformArg = rec.get("platform")
			force = rec.get("forceRelogin") == true
			if v, ok := rec.get("verificationUrl").(string); ok {
				verification = v
			}
		}
		platform, err := requirePlatformArg(platformArg)
		if err != nil {
			return nil, err
		}
		if platform != "nodeseek" {
			verification = ""
		}
		return s.login(ctx, platform, force, verification)
	}
	b.handlers["platformCapture:cancelLogin"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		platform, err := requirePlatformArg(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		return s.cancelForPlatform("login", platform), nil
	}
	b.handlers["platformCapture:logout"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		platform, err := requirePlatformArg(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		return nil, s.logout(ctx, platform)
	}
	b.handlers["platformCapture:clearAll"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return nil, s.clearAll(ctx)
	}
	b.handlers["platformCapture:listComments"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.sourceCommentsList(ctx, nullishToEmpty(jsArg(a, 0)))
	}
	b.handlers["platformCapture:refreshComments"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		limit := jsGet(raw, "limit")
		if !isCommentLimit(limit) || numericOf(limit) == 0 {
			return nil, errors.New("无效的评论数量")
		}
		itemID, _ := jsGet(raw, "itemId").(string)
		rows, err := l.db.Query(ctx, `SELECT source_uri FROM source_records
         WHERE item_id = ? AND platform IN ('xiaohongshu','douyin')
         ORDER BY captured_at DESC LIMIT 1`, itemID)
		if err != nil {
			return nil, err
		}
		uri := ""
		if len(rows.Values) > 0 {
			uri, _ = rows.Values[0][0].(string)
		}
		platform := ""
		if uri != "" {
			platform = detectPlatformCapturePlatform(uri)
		}
		if uri == "" || platform == "" {
			return nil, errors.New("该条目没有可补采评论的平台来源")
		}
		comments, err := s.captureSourceComments(ctx, platform, uri, int(numericOf(limit)))
		if err != nil {
			return nil, err
		}
		if err := l.sourceCommentsUpsert(ctx, itemID, platform, comments); err != nil {
			return nil, err
		}
		return l.sourceCommentsList(ctx, itemID)
	}
}

func nullishToEmpty(v any) string {
	if isNullish(v) {
		return ""
	}
	return jsToString(v)
}
