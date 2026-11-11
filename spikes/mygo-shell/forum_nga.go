package main

// 对应 services/import/nga.ts 的抓取与线程组装：`read.php?tid=&page=&lite=js` 分页读取（访客 guestJs 握手）、
// 楼主发言按 authorid 拉取、总结采样页、父楼按 pid 补拉、附件图本地化。入库策略：主楼 + 楼主回复，
// 另按页采样他人回复专供讨论总结。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ngaDeps 汇总可注入依赖；零值即生产行为。
type ngaDeps struct {
	fetchRawText func(ctx context.Context, url string, o safeRequestOptions) (fetchRawTextResult, error)
	// downloadImage 返回的目录由调用方负责删除
	downloadImage  func(ctx context.Context, url string) (dir, filePath string, err error)
	saveImageAsset func(filePath string) (string, error)
	retryDelays    []time.Duration // 非 nil 时覆盖默认退避；空切片表示不重试
	imageLimit     *int
	maxSummaryPage *int
	maxOpPages     *int
	maxParentFetch *int
}

func (d ngaDeps) delays() []time.Duration {
	if d.retryDelays != nil {
		return d.retryDelays
	}
	return forumRetryDelays
}

func ngaLimit(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

func describeNgaError(err error) string {
	msg := forumErrMsg(err)
	switch {
	case msg == forumCanceledMsg:
		return msg
	case strings.Contains(msg, "需要登录") || strings.Contains(msg, "访客不能"):
		return msg
	case strings.Contains(msg, "HTTP 404"):
		return "帖子不存在或已被删除"
	}
	if m := reHTTP5xxCapture.FindStringSubmatch(msg); m != nil {
		return "NGA 服务器暂时无响应（HTTP " + m[1] + "），已自动重试仍未成功，稍后再试"
	}
	switch msg {
	case "请求超时":
		return "连接 NGA 超时，已自动重试仍未成功，稍后再试"
	case "连接被中断":
		return "与 NGA 的连接被中断，已自动重试仍未成功，稍后再试"
	}
	return msg
}

// ---- 数据访问（NgaPost / NgaTopic 等都是宽松的 JSON 对象） ----

func ngaPostList(raw any) []*jsObj {
	var out []*jsObj
	switch r := raw.(type) {
	case jsArr:
		for _, e := range r {
			if o := fjObj(e); o != nil {
				out = append(out, o)
			}
		}
	case *jsObj:
		keys := append([]string(nil), r.keys()...)
		num := func(k string) (float64, bool) {
			n := jsToNumber(k)
			return n, !math.IsNaN(n)
		}
		sort.SliceStable(keys, func(i, j int) bool {
			a, aok := num(keys[i])
			b, bok := num(keys[j])
			return aok && bok && a < b
		})
		for _, k := range keys {
			if o := fjObj(r.m[k]); o != nil {
				out = append(out, o)
			}
		}
	}
	return out
}

func ngaAttachList(raw any) []*jsObj {
	var out []*jsObj
	switch r := raw.(type) {
	case jsArr:
		for _, e := range r {
			if o := fjObj(e); o != nil {
				out = append(out, o)
			}
		}
	case *jsObj:
		for _, k := range r.keys() {
			if o := fjObj(r.m[k]); o != nil {
				out = append(out, o)
			}
		}
	}
	return out
}

func ngaStrField(o *jsObj, key string) string {
	s, _ := o.get(key).(string)
	return s
}

// ngaLou 对应 `typeof post.lou === "number"`。
func ngaLou(p *jsObj) (float64, bool) { return fjNum(p.get("lou")) }

func ngaResolveAttachBase(data *jsObj) string {
	view := fjTrim(jsOptGet(data.get("__GLOBAL"), "_ATTACH_BASE_VIEW"))
	switch {
	case view == "":
		return ngaDefaultAttach
	case reHTTPPrefix.MatchString(view):
		return strings.TrimSuffix(view, "/")
	}
	return "https://" + strings.TrimSuffix(view, "/")
}

func ngaCollectImageURLs(post *jsObj, attachBase string) []string {
	var urls []string
	seen := map[string]bool{}
	push := func(raw string) {
		if raw == "" {
			return
		}
		u, ok := resolveNgaImageURL(raw, attachBase)
		if !ok || seen[u] {
			return
		}
		seen[u] = true
		urls = append(urls, u)
	}
	for _, a := range ngaAttachList(post.get("attachs")) {
		if ngaAttachIsImage(a) && ngaStrField(a, "attachurl") != "" {
			push(ngaStrField(a, "attachurl"))
		}
	}
	for _, m := range reImgBlock.FindAllStringSubmatch(ngaStrField(post, "content"), -1) {
		push(m[1])
	}
	return urls
}

// ngaAttachIsImage 对应 `(attach.type ?? "img") === "img"`。
func ngaAttachIsImage(a *jsObj) bool {
	t := a.get("type")
	if isNullish(t) {
		return true
	}
	s, ok := t.(string)
	return ok && s == "img"
}

func ngaResolveAuthor(post *jsObj, users *jsObj, fallback string) string {
	uid := post.get("authorid")
	// __U 里的昵称优先：lite=js 分页里 post.author 经常缺席，只剩 authorid
	if !isNullish(uid) && users != nil {
		if name := fjTrim(jsOptGet(users.get(jsToString(uid)), "username")); name != "" {
			return name
		}
	}
	if direct := fjTrim(post.get("author")); direct != "" {
		return direct
	}
	if fb := jsTrim(fallback); fb != "" {
		return fb
	}
	if !isNullish(uid) && jsToNumber(uid) < 0 {
		return "匿名"
	}
	if !isNullish(uid) {
		return "UID:" + jsToString(uid)
	}
	return ""
}

func ngaResolveNodeName(data *jsObj, fid any) string {
	f := data.get("__F")
	if isNullish(fid) || !vTruthy(f) {
		return ""
	}
	if entry := jsGet(f, jsToString(fid)); vTruthy(entry) {
		if name, ok := jsGet(entry, "name").(string); ok {
			return jsTrim(name)
		}
	}
	if root, ok := jsGet(f, "name").(string); ok {
		return jsTrim(root)
	}
	return ""
}

func ngaMillis(v any) int64 {
	if n, ok := fjNum(v); ok && !math.IsInf(n, 0) && !math.IsNaN(n) && n > 0 {
		return int64(n * 1000)
	}
	return time.Now().UnixMilli()
}

func ngaPageURL(topicID string, page int, authorID *float64) string {
	q := "tid=" + url.QueryEscape(topicID) + "&page=" + strconv.Itoa(page) + "&lite=js"
	if authorID != nil && !math.IsNaN(*authorID) && !math.IsInf(*authorID, 0) && *authorID != 0 {
		q += "&authorid=" + url.QueryEscape(jsNumberString(*authorID))
	}
	return ngaOrigin + "/read.php?" + q
}

func ngaPidURL(pid float64) string {
	return ngaOrigin + "/read.php?pid=" + encodeURIComponent(jsNumberString(pid)) + "&lite=js"
}

func ngaGuestDenied() error {
	return newPlatformParseError("guest_denied", "需要登录后才能查看该帖（访客无权访问）")
}

// ngaAssertPageOK 校验接口层错误并返回 data。
func ngaAssertPageOK(store any) (*jsObj, error) {
	errObj := jsOptGet(store, "error")
	code, _ := jsOptGet(errObj, "0").(string)
	if code != "" {
		if strings.HasPrefix(code, "15") {
			return nil, ngaGuestDenied()
		}
		if strings.HasPrefix(code, "5") {
			return nil, newPlatformParseError("note_unavailable", "帖子过旧或访问受限，暂时无法采集")
		}
		detail := ""
		if s, ok := jsOptGet(errObj, "1").(string); ok {
			detail = jsTrim(reHTMLTagAny.ReplaceAllString(s, ""))
		}
		if detail != "" {
			return nil, newPlatformParseError("note_unavailable", "NGA 拒绝访问："+code+"（"+utf16Slice(detail, 80)+"）")
		}
		return nil, newPlatformParseError("note_unavailable", "NGA 拒绝访问："+code)
	}
	data := jsOptGet(store, "data")
	msg := jsOptGet(data, "__MESSAGE")
	if n, ok := fjNum(jsOptGet(msg, "0")); ok && n != 0 {
		detail := jsOptGet(msg, "1")
		if n == 15 {
			return nil, ngaGuestDenied()
		}
		suffix := ""
		if isNullish(detail) {
			detail = ""
		}
		if vTruthy(detail) {
			suffix = "（" + utf16Slice(jsToString(detail), 80) + "）"
		}
		return nil, newPlatformParseError("note_unavailable", "NGA 拒绝访问："+jsNumberString(n)+suffix)
	}
	if o := fjObj(data); o != nil {
		return o, nil
	}
	if !vTruthy(data) {
		return nil, newPlatformParseError("structure_missing", "NGA 接口未返回帖子数据")
	}
	return newObj(), nil
}

func (l *Library) ngaFetch(deps ngaDeps) func(context.Context, string, safeRequestOptions) (fetchRawTextResult, error) {
	if deps.fetchRawText != nil {
		return deps.fetchRawText
	}
	return func(ctx context.Context, u string, o safeRequestOptions) (fetchRawTextResult, error) {
		f := l.webFetcher
		if f == nil {
			f = defaultSafeFetcher
		}
		return f.fetchRawText(ctx, u, o, 0)
	}
}

func ngaRequestOptions(topicID, cookie string, accept403 bool) safeRequestOptions {
	o := safeRequestOptions{UserAgent: ngaUA, Referer: ngaCanonicalURL(topicID), Cookie: cookie}
	if accept403 {
		o.AcceptStatuses = []int{403}
	}
	return o
}

type ngaPageResult struct {
	store  any
	cookie string
}

// ngaRequestPage 读取一页；403 且带 guestJs 时握手后重读。网络类瞬时故障按退避重试。
func (l *Library) ngaRequestPage(ctx context.Context, topicID string, page int, cookie string, deps ngaDeps, authorID *float64) (ngaPageResult, error) {
	get := l.ngaFetch(deps)
	delays := deps.delays()
	cur := cookie
	rawURL := ngaPageURL(topicID, page, authorID)
	var last error
	for attempt := 0; attempt <= len(delays); attempt++ {
		if attempt > 0 {
			if err := forumWait(ctx, delays[attempt-1]); err != nil {
				return ngaPageResult{}, err
			}
		}
		res, err := get(ctx, rawURL, ngaRequestOptions(topicID, cur, true)) // 始终接受 403：分页中途 guest 会话失效时要能读挑战正文再握手
		if err == nil {
			if m := mergeNgaCookies(cur, res.SetCookies, res.Text); m != "" {
				cur = m
			}
			text := res.Text
			if res.Status == 403 {
				if cur == "" || !strings.Contains(cur, "guestJs=") {
					err = ngaGuestDenied()
				} else {
					var retry fetchRawTextResult
					retry, err = get(ctx, rawURL, ngaRequestOptions(topicID, cur, false))
					if err == nil {
						if m := mergeNgaCookies(cur, retry.SetCookies, retry.Text); m != "" {
							cur = m
						}
						text = retry.Text
					}
				}
			}
			if err == nil {
				var store any
				if store, err = parseNgaStore(text); err == nil {
					return ngaPageResult{store: store, cookie: cur}, nil
				}
			}
		}
		msg := err.Error()
		if msg == forumCanceledMsg {
			return ngaPageResult{}, err
		}
		var ppe *platformParseError
		if errors.As(err, &ppe) {
			return ngaPageResult{}, err
		}
		if strings.Contains(msg, "需要登录") || strings.Contains(msg, "拒绝访问") || strings.Contains(msg, "无法解析") {
			code := "note_unavailable"
			if strings.Contains(msg, "需要登录") {
				code = "guest_denied"
			} else if strings.Contains(msg, "无法解析") {
				code = "structure_missing"
			}
			return ngaPageResult{}, newPlatformParseError(code, describeNgaError(err))
		}
		if !isForumTransient(msg) {
			return ngaPageResult{}, newPlatformParseError("network", describeNgaError(err))
		}
		last = err
		logAppErrorf("import", "NGA 请求失败", "第 %d 页第 %d 次请求失败（%s）", page, attempt+1, msg)
	}
	return ngaPageResult{}, newPlatformParseError("network", describeNgaError(last))
}

type ngaPidResult struct {
	post   *jsObj
	users  *jsObj
	cookie string
}

// ngaRequestPid 按 pid 拉单楼（补楼主回复的父楼上下文）；失败只记日志，取消才向上抛。
func (l *Library) ngaRequestPid(ctx context.Context, pid float64, topicID, cookie string, deps ngaDeps) (ngaPidResult, error) {
	get := l.ngaFetch(deps)
	cur := cookie
	fail := func(err error) (ngaPidResult, error) {
		if err != nil && err.Error() == forumCanceledMsg {
			return ngaPidResult{}, err
		}
		if err != nil {
			logAppErrorf("import", "NGA 补拉父楼失败", "pid=%s：%v", jsNumberString(pid), err)
		}
		return ngaPidResult{users: newObj(), cookie: cur}, nil
	}
	res, err := get(ctx, ngaPidURL(pid), ngaRequestOptions(topicID, cur, true))
	if err != nil {
		return fail(err)
	}
	if m := mergeNgaCookies(cur, res.SetCookies, res.Text); m != "" {
		cur = m
	}
	text := res.Text
	if res.Status == 403 {
		if !strings.Contains(cur, "guestJs=") {
			return ngaPidResult{users: newObj(), cookie: cur}, nil
		}
		retry, err := get(ctx, ngaPidURL(pid), ngaRequestOptions(topicID, cur, false))
		if err != nil {
			return fail(err)
		}
		if m := mergeNgaCookies(cur, retry.SetCookies, retry.Text); m != "" {
			cur = m
		}
		text = retry.Text
	}
	store, err := parseNgaStore(text)
	if err != nil {
		return fail(err)
	}
	data, err := ngaAssertPageOK(store)
	if err != nil {
		return fail(err)
	}
	posts := ngaPostList(data.get("__R"))
	var post *jsObj
	for _, p := range posts {
		if n, ok := fjNum(p.get("pid")); ok && n == pid {
			post = p
			break
		}
	}
	if post == nil {
		for _, p := range posts {
			if n, ok := ngaLou(p); !ok || n != 0 {
				post = p
				break
			}
		}
	}
	if post == nil && len(posts) > 0 {
		post = posts[0]
	}
	users := fjObj(data.get("__U"))
	if users == nil {
		users = newObj()
	}
	return ngaPidResult{post: post, users: users, cookie: cur}, nil
}

// ---- 图片 ----

func ngaSniffImageExt(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ".jpg"
	}
	defer f.Close()
	head := make([]byte, 12)
	n, _ := f.Read(head)
	head = head[:n]
	has := func(prefix string, off int) bool {
		return len(head) >= off+len(prefix) && string(head[off:off+len(prefix)]) == prefix
	}
	switch {
	case has("\xff\xd8\xff", 0):
		return ".jpg"
	case has("\x89PNG\r\n\x1a\n", 0):
		return ".png"
	case has("GIF8", 0):
		return ".gif"
	case has("RIFF", 0) && has("WEBP", 8):
		return ".webp"
	case has("BM", 0):
		return ".bmp"
	}
	return ".jpg"
}

func (l *Library) ngaDefaultDownloadImage(ctx context.Context, rawURL string) (string, string, error) {
	f := l.webFetcher
	if f == nil {
		f = defaultSafeFetcher
	}
	dir, filePath, err := f.downloadToTempFile(ctx, rawURL, "image.bin", mediaSizeLimits["image"], safeRequestOptions{UserAgent: ngaUA, Referer: ngaOrigin + "/"})
	if err != nil {
		return "", "", err
	}
	renamed := filepath.Join(dir, "image"+ngaSniffImageExt(filePath))
	if err := os.Rename(filePath, renamed); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return dir, renamed, nil
}

// ngaLocalizeImages 把附件图下载进资产目录；超限保留外链，失败不阻断。
func (l *Library) ngaLocalizeImages(ctx context.Context, urls []string, deps ngaDeps) (map[string]string, []string, error) {
	limit := ngaLimit(deps.imageLimit, ngaImageLimit)
	imageMap := map[string]string{}
	var warnings []string
	download := deps.downloadImage
	if download == nil {
		download = l.ngaDefaultDownloadImage
	}
	save := deps.saveImageAsset
	if save == nil {
		save = func(p string) (string, error) { return l.paths.saveMediaAsset(p, "image") }
	}
	downloaded, skipped, failed := 0, 0, 0
	for _, u := range urls {
		if ctx.Err() != nil {
			return nil, nil, errors.New(forumCanceledMsg)
		}
		if downloaded >= limit {
			skipped++
			continue
		}
		dir, filePath, err := download(ctx, u)
		if err == nil {
			var name string
			if name, err = save(filePath); err == nil {
				imageMap[u] = mediaProtocolURL("image", name)
				downloaded++
			}
		}
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		if err != nil {
			if err.Error() == forumCanceledMsg || ctx.Err() != nil {
				return nil, nil, errors.New(forumCanceledMsg)
			}
			failed++
			logAppErrorf("import", "NGA 附件图下载失败", "%s：%v", u, err)
		}
	}
	if skipped > 0 {
		warnings = append(warnings, fmt.Sprintf("附件图超过 %d 张上限，另有 %d 张保留外链", limit, skipped))
	}
	if failed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d 张附件图下载失败，已保留外链", failed))
	}
	return imageMap, warnings, nil
}

func ngaAppendOrphanAttachments(markdown string, post *jsObj, attachBase string, imageMap map[string]string) string {
	embedded := map[string]bool{}
	for _, m := range reMDImage.FindAllStringSubmatch(markdown, -1) {
		embedded[m[1]] = true
	}
	var extras []string
	for _, a := range ngaAttachList(post.get("attachs")) {
		if !ngaAttachIsImage(a) || ngaStrField(a, "attachurl") == "" {
			continue
		}
		resolved, ok := resolveNgaImageURL(ngaStrField(a, "attachurl"), attachBase)
		if !ok {
			continue
		}
		target := resolved
		if local, ok := imageMap[resolved]; ok {
			target = local
		}
		if embedded[target] || embedded[resolved] {
			continue
		}
		extras = append(extras, "![图]("+target+")")
	}
	if len(extras) == 0 {
		return markdown
	}
	return jsTrim(markdown + "\n\n" + strings.Join(extras, "\n\n"))
}

// ---- 线程组装 ----

// ngaFloorMap 对应 Map<lou, post>：按首次出现的顺序保留。
type ngaFloorMap struct {
	keys  []float64
	posts map[float64]*jsObj
}

func (m *ngaFloorMap) has(lou float64) bool { _, ok := m.posts[lou]; return ok }

func (m *ngaFloorMap) first() (*jsObj, bool) {
	if len(m.keys) == 0 {
		return nil, false
	}
	return m.posts[m.keys[0]], true
}

func (m *ngaFloorMap) sortedKeys() []float64 {
	ks := append([]float64(nil), m.keys...)
	sort.SliceStable(ks, func(i, j int) bool { return ks[i] < ks[j] })
	return ks
}

func ngaDedupeByFloor(posts []*jsObj) *ngaFloorMap {
	m := &ngaFloorMap{posts: map[float64]*jsObj{}}
	for _, p := range posts {
		lou, ok := ngaLou(p)
		if !ok {
			lou = float64(len(m.keys))
		}
		if !m.has(lou) {
			m.keys = append(m.keys, lou)
			m.posts[lou] = p
		}
	}
	return m
}

func ngaReplyToFrom(c ngaReplyContext) *forumReplyTo {
	if !c.HasReplyTo {
		return nil
	}
	return &forumReplyTo{Author: c.Author, Snippet: c.Snippet}
}

type ngaThreadInput struct {
	topicID       string
	topic         *jsObj
	page1         *jsObj
	samplePosts   []*jsObj
	opPosts       []*jsObj
	users         *jsObj
	attachBase    string
	cookie        string
	extraWarnings []string
}

func (l *Library) ngaFinalizeThread(ctx context.Context, in ngaThreadInput, deps ngaDeps) (*forumThread, error) {
	opByFloor := ngaDedupeByFloor(in.opPosts)
	sampleByFloor := ngaDedupeByFloor(in.samplePosts)
	cookie := in.cookie

	var opPost *jsObj
	switch {
	case opByFloor.has(0):
		opPost = opByFloor.posts[0]
	case sampleByFloor.has(0):
		opPost = sampleByFloor.posts[0]
	default:
		if p, ok := opByFloor.first(); ok {
			opPost = p
		} else if p, ok := sampleByFloor.first(); ok {
			opPost = p
		}
	}
	if opPost == nil {
		return nil, newPlatformParseError("structure_missing", "帖子没有主楼内容")
	}

	// 附件图只处理主楼 + 入库的楼主回复，不给采样里的水楼烧配额
	var imageSources []*jsObj
	if !opByFloor.has(0) {
		imageSources = append(imageSources, opPost)
	}
	for _, k := range opByFloor.keys {
		imageSources = append(imageSources, opByFloor.posts[k])
	}
	var allURLs []string
	seenURLs := map[string]bool{}
	for _, p := range imageSources {
		for _, u := range ngaCollectImageURLs(p, in.attachBase) {
			if !seenURLs[u] {
				seenURLs[u] = true
				allURLs = append(allURLs, u)
			}
		}
	}
	imageMap, imgWarnings, err := l.ngaLocalizeImages(ctx, allURLs, deps)
	if err != nil {
		return nil, err
	}
	warnings := append(append([]string{}, in.extraWarnings...), imgWarnings...)

	formatPost := func(bbcode string, post *jsObj) string {
		return ngaAppendOrphanAttachments(ngaBbcodeToMarkdown(bbcode, in.attachBase, imageMap), post, in.attachBase, imageMap)
	}

	subject := in.topic.get("subject")
	if isNullish(subject) {
		subject = opPost.get("subject")
	}
	subj, _ := subject.(string)
	title := jsTrim(decodeNgaSubject(subj))
	if title == "" {
		title = "NGA 帖子 " + in.topicID
	}
	topicAuthor, _ := in.topic.get("author").(string)
	author := ngaResolveAuthor(opPost, in.users, topicAuthor)
	content := formatPost(ngaStrField(opPost, "content"), opPost)

	maxParent := ngaLimit(deps.maxParentFetch, ngaParentFetchMax)
	parentFetches := 0
	type parentInfo struct {
		author  string
		floor   *int
		snippet string
	}
	parentCache := map[float64]parentInfo{}

	toReply := func(lou float64, post *jsObj, replyTo *forumReplyTo, bbcode string) *forumReply {
		body := formatPost(bbcode, post)
		if body == "" && replyTo == nil {
			return nil
		}
		return &forumReply{
			Floor: int(lou), Author: ngaResolveAuthor(post, in.users, ""), Content: body,
			CreatedAt: ngaMillis(post.get("postdatetimestamp")), ReplyTo: replyTo,
		}
	}

	replies := []forumReply{}
	for _, lou := range opByFloor.sortedKeys() {
		if lou == 0 {
			continue
		}
		post := opByFloor.posts[lou]
		ctxReply := extractNgaReplyContext(ngaStrField(post, "content"))
		replyTo := ngaReplyToFrom(ctxReply)
		needParent := ctxReply.HasReplyTo && ctxReply.HasPid && ctxReply.Pid > 0 && jsTrim(ctxReply.Snippet) == ""
		if needParent && parentFetches < maxParent {
			pid := ctxReply.Pid
			cached, ok := parentCache[pid]
			if !ok {
				parentFetches++
				fetched, err := l.ngaRequestPid(ctx, pid, in.topicID, cookie, deps)
				if err != nil {
					return nil, err
				}
				if fetched.cookie != "" {
					cookie = fetched.cookie
				}
				spreadInto(in.users, fetched.users)
				if fetched.post != nil {
					info := parentInfo{author: ngaResolveAuthor(fetched.post, in.users, ""), snippet: plainSnippetFromBbcode(ngaStrField(fetched.post, "content"))}
					if info.author == "" {
						info.author = ctxReply.Author
					}
					if n, ok := ngaLou(fetched.post); ok {
						f := int(n)
						info.floor = &f
					}
					parentCache[pid] = info
					cached, ok = info, true
				}
			}
			if ok {
				replyTo = &forumReplyTo{Author: cached.author, Floor: cached.floor, Snippet: cached.snippet}
			}
		}
		if r := toReply(lou, post, replyTo, ctxReply.Content); r != nil {
			replies = append(replies, *r)
		}
	}

	summaryReplies := []forumReply{}
	for _, lou := range sampleByFloor.sortedKeys() {
		if lou == 0 {
			continue
		}
		post := sampleByFloor.posts[lou]
		c := extractNgaReplyContext(ngaStrField(post, "content"))
		if r := toReply(lou, post, ngaReplyToFrom(c), c.Content); r != nil {
			summaryReplies = append(summaryReplies, *r)
		}
	}
	// 采样页几乎没有他人回复时，用楼主回复撑总结素材
	material := summaryReplies
	if len(material) == 0 {
		material = replies
	}

	createdSrc := in.topic.get("postdate")
	if isNullish(createdSrc) {
		createdSrc = opPost.get("postdatetimestamp")
	}
	replyCount := len(replies)
	if n, ok := fjNum(in.topic.get("replies")); ok {
		replyCount = int(n)
	}
	return &forumThread{
		Platform: "nga", TopicID: in.topicID, Title: title, Author: author,
		Node:      ngaResolveNodeName(in.page1, in.topic.get("fid")),
		CreatedAt: ngaMillis(createdSrc), ReplyCount: replyCount, Content: content,
		Replies: replies, SummaryReplies: material, ReplyRetention: "op-only",
		WebpageURL: ngaCanonicalURL(in.topicID), WarningReason: strings.Join(warnings, "；"),
	}, nil
}

// fetchNgaThread 抓取 NGA 帖子：主楼入库，讨论区只留楼主回复；另采样若干页供总结。
func (l *Library) fetchNgaThread(ctx context.Context, topicID string, deps ngaDeps) (*forumThread, error) {
	first, err := l.ngaRequestPage(ctx, topicID, 1, "", deps, nil)
	if err != nil {
		return nil, err
	}
	page1, err := ngaAssertPageOK(first.store)
	if err != nil {
		return nil, err
	}
	topic := fjObj(page1.get("__T"))
	if topic == nil {
		return nil, newPlatformParseError("note_unavailable", "帖子不存在或已被删除")
	}
	attachBase := ngaResolveAttachBase(page1)
	users := newObj()
	spreadInto(users, page1.get("__U"))
	samplePosts := ngaPostList(page1.get("__R"))

	rows := float64(len(samplePosts))
	if n, ok := fjNum(page1.get("__ROWS")); ok && n > 0 {
		rows = n
	}
	totalPages := int(math.Max(1, math.Ceil(rows/ngaDefaultPageSize)))
	summaryCap := min(totalPages, ngaLimit(deps.maxSummaryPage, ngaSummaryMaxPages))

	cookie := first.cookie
	var warnings []string
	for page := 2; page <= summaryCap; page++ {
		if ctx.Err() != nil {
			return nil, errors.New(forumCanceledMsg)
		}
		next, err := l.ngaRequestPage(ctx, topicID, page, cookie, deps, nil)
		var data *jsObj
		if err == nil {
			if next.cookie != "" {
				cookie = next.cookie
			}
			data, err = ngaAssertPageOK(next.store)
		}
		if err != nil {
			if err.Error() == forumCanceledMsg {
				return nil, err
			}
			logAppErrorf("import", "NGA 总结采样失败", "第 %d 页：%v", page, err)
			warnings = append(warnings, fmt.Sprintf("第 %d 页及之后采样失败（%s），讨论总结素材可能不完整", page, err.Error()))
			break
		}
		spreadInto(users, data.get("__U"))
		samplePosts = append(samplePosts, ngaPostList(data.get("__R"))...)
	}
	if totalPages > summaryCap {
		warnings = append(warnings, fmt.Sprintf("原帖约 %d 页，讨论总结仅采样前 %d 页；讨论区只保留楼主回复", totalPages, summaryCap))
	}

	var opFromSample *jsObj
	for _, p := range samplePosts {
		if n, ok := ngaLou(p); ok && n == 0 {
			opFromSample = p
			break
		}
	}
	if opFromSample == nil && len(samplePosts) > 0 {
		opFromSample = samplePosts[0]
	}
	var opAuthorID *float64
	if n, ok := fjNum(topic.get("authorid")); ok {
		opAuthorID = &n
	} else if opFromSample != nil {
		if n, ok := fjNum(opFromSample.get("authorid")); ok {
			opAuthorID = &n
		}
	}

	var opPosts []*jsObj
	maxOpPages := ngaLimit(deps.maxOpPages, ngaOpMaxPages)
	hasAuthor := opAuthorID != nil && *opAuthorID != 0
	if hasAuthor {
		for page := 1; page <= maxOpPages; page++ {
			if ctx.Err() != nil {
				return nil, errors.New(forumCanceledMsg)
			}
			next, err := l.ngaRequestPage(ctx, topicID, page, cookie, deps, opAuthorID)
			var data *jsObj
			if err == nil {
				if next.cookie != "" {
					cookie = next.cookie
				}
				data, err = ngaAssertPageOK(next.store)
			}
			if err != nil {
				if err.Error() == forumCanceledMsg {
					return nil, err
				}
				logAppErrorf("import", "NGA 楼主发言拉取失败", "第 %d 页：%v", page, err)
				warnings = append(warnings, fmt.Sprintf("楼主发言第 %d 页抓取失败（%s），楼主回复可能不完整", page, err.Error()))
				break
			}
			spreadInto(users, data.get("__U"))
			pagePosts := ngaPostList(data.get("__R"))
			if len(pagePosts) == 0 {
				break
			}
			opPosts = append(opPosts, pagePosts...)
			if len(pagePosts) < ngaDefaultPageSize {
				break
			}
		}
	}
	// authorid 过滤失败或不可用时，从采样里挑同作者的楼，总比讨论区全空强
	if len(opPosts) == 0 && hasAuthor {
		for _, p := range samplePosts {
			if n, ok := fjNum(p.get("authorid")); ok && n == *opAuthorID {
				opPosts = append(opPosts, p)
			}
		}
		if len(opPosts) > 0 {
			warnings = append(warnings, "未能按楼主筛选拉取完整发言，讨论区仅含采样页内的楼主回复")
		}
	} else if len(opPosts) == 0 && opFromSample != nil {
		opPosts = append(opPosts, opFromSample)
		warnings = append(warnings, "无法识别楼主账号，讨论区未收录后续回复")
	}

	return l.ngaFinalizeThread(ctx, ngaThreadInput{
		topicID: topicID, topic: topic, page1: page1, samplePosts: samplePosts, opPosts: opPosts,
		users: users, attachBase: attachBase, cookie: cookie, extraWarnings: warnings,
	}, deps)
}
