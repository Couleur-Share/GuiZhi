package main

// 对应 services/web-capture/crawl-service.ts 与 ipc/web-capture.ipc.ts 里的 crawl:* 频道：
// 文档批次的调度（robots、sitemap 候选、逐页采集、入库、发现新链接、暂停/取消/恢复）。
// 批次库（crawl_job.go）、robots（robots.go）、采集（web_capture.go）、原文版本（web_source.go）都已移植。
//
// 与 TS 的差异（均为实现形态，行为一致）：
//   - TS 的 persist 在同步事务里一次完成，Go 的 MyGo 事务不能穿插读取，所以「读→生成语句→单次 Transaction」，
//     新建条目路径里条目、来源记录、原文版本、导入任务、页面状态与新发现的链接仍在同一个事务里。
//   - 已有条目 + duplicatePolicy=update 时，webSourceCheck 是自己的事务；其后的任务/页面状态是第二个事务。
//     两者之间崩溃只会让页面回到 pending，下一轮重采得到 unchanged，结果幂等。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/egoist/mygo/plugins/sqlite"
)

const crawlAbortMessage = "This operation was aborted" // DOMException AbortError 的 message

type crawlService struct {
	l       *Library
	mu      sync.Mutex
	running map[string]context.CancelFunc
	done    map[string]chan struct{}
}

func newCrawlService(l *Library) *crawlService {
	return &crawlService{l: l, running: map[string]context.CancelFunc{}, done: map[string]chan struct{}{}}
}

func (s *crawlService) create(ctx context.Context, input any) (string, error) {
	if cid := jsGet(input, "collectionId"); vTruthy(cid) {
		id, isStr := cid.(string)
		found := false
		if isStr {
			rows, err := s.l.db.Query(ctx, "SELECT id FROM collections WHERE id=?", id)
			if err != nil {
				return "", err
			}
			found = len(rows.Values) > 0
		}
		if !found {
			return "", errors.New("目标集合不存在，请重新选择")
		}
	}
	job, err := s.l.crawlJobCreate(ctx, input)
	if err != nil {
		return "", err
	}
	s.start(job.ID)
	return job.ID, nil
}

// start 在后台恢复批次；错误按 TS 的 `void this.resume(id)` 丢弃，只留日志。
func (s *crawlService) start(id string) {
	go func() {
		if err := s.resume(id); err != nil {
			logAppErrorf("main", "网页批次", "%v", err)
		}
	}()
}

func (s *crawlService) abort(id string) {
	s.mu.Lock()
	cancel := s.running[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *crawlService) pause(ctx context.Context, id string) error {
	if err := s.l.crawlJobSetStatus(ctx, id, "paused", nil); err != nil {
		return err
	}
	s.abort(id)
	return nil
}

func (s *crawlService) cancel(ctx context.Context, id string) error {
	if err := s.l.crawlJobSetStatus(ctx, id, "canceled", nil); err != nil {
		return err
	}
	s.abort(id)
	pages, err := s.l.crawlPages(ctx, id)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if p.Status == "pending" {
			p.Status = "canceled"
			if err := s.l.crawlSavePage(ctx, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// close 暂停所有运行中的批次并等待它们退出。
func (s *crawlService) close(ctx context.Context) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.running))
	for id := range s.running {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		if err := s.pause(ctx, id); err != nil {
			logAppErrorf("main", "网页批次", "关闭时暂停失败：%v", err)
		}
	}
	s.mu.Lock()
	waits := make([]chan struct{}, 0, len(s.done))
	for _, ch := range s.done {
		waits = append(waits, ch)
	}
	s.mu.Unlock()
	for _, ch := range waits {
		<-ch
	}
}

func (s *crawlService) resume(id string) error {
	bg := context.Background()
	s.mu.Lock()
	if _, ok := s.running[id]; ok {
		s.mu.Unlock()
		return nil
	}
	job, err := s.l.crawlJobGet(bg, id)
	if err != nil || job == nil || job.Status == "canceled" {
		s.mu.Unlock()
		if err != nil {
			return err
		}
		if job == nil {
			return errors.New("采集批次不存在")
		}
		return errors.New("已取消的批次不能继续")
	}
	ctx, cancel := context.WithCancel(bg)
	finished := make(chan struct{})
	s.running[id], s.done[id] = cancel, finished
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.running, id)
		delete(s.done, id)
		s.mu.Unlock()
		close(finished)
	}()
	if err := s.l.crawlJobSetStatus(bg, id, "running", nil); err != nil {
		return err
	}
	if err := s.run(ctx, *job); err != nil && ctx.Err() == nil {
		return s.l.crawlJobSetStatus(bg, id, "paused", sp(err.Error()))
	}
	return nil
}

// run 对应 resume 的主体；返回非 nil 表示批次需要暂停（TS 的外层 catch）。
func (s *crawlService) run(ctx context.Context, job crawlJob) error {
	l, id := s.l, job.ID
	policies := map[string]robotsPolicy{}
	fetcher := l.webFetcher
	if fetcher == nil {
		fetcher = defaultSafeFetcher
	}
	for index, seed := range job.Input.Seeds {
		origin := webOrigin(seed.URL)
		if _, ok := policies[origin]; !ok {
			p, err := fetcher.loadRobots(ctx, origin)
			if err != nil {
				return err
			}
			policies[origin] = p
		}
		if seed.Mode != "directory" {
			continue
		}
		scope, err := newWebScope(seed.URL, seed.Directory)
		if err != nil {
			return err
		}
		policy := policies[origin]
		sitemaps := policy.Sitemaps
		if len(sitemaps) > 3 { // sitemap 只提供候选，最多读取 3 个文件，不展开索引树
			sitemaps = sitemaps[:3]
		}
		for _, url := range sitemaps {
			if !inWebScope(url, webScope{Origin: origin, Directory: "/"}) || !robotsAllows(url, policy) {
				continue
			}
			resp, err := fetcher.webTextRequest(ctx, url, func(u string) bool { return webOrigin(u) == origin && robotsAllows(u, policy) })
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				continue
			}
			for _, link := range sitemapLocs(resp.Text) {
				if link != "" && inWebScope(link, scope) && isWebPageLink(link) {
					if _, err := l.crawlDiscover(ctx, id, link, 1, index); err != nil {
						return err
					}
				}
			}
		}
	}
	for ctx.Err() == nil {
		pages, err := l.crawlPages(ctx, id)
		if err != nil {
			return err
		}
		var page *crawlPage
		for i := range pages {
			if pages[i].Status == "pending" {
				page = &pages[i]
				break
			}
		}
		if page == nil {
			break
		}
		if err := s.crawlOnePage(ctx, job, *page, policies); err != nil {
			return err
		}
	}
	if ctx.Err() == nil {
		return l.crawlJobSetStatus(context.Background(), id, "completed", nil)
	}
	return nil
}

func (s *crawlService) crawlOnePage(ctx context.Context, job crawlJob, page crawlPage, policies map[string]robotsPolicy) error {
	l := s.l
	seed := job.Input.Seeds[page.SeedIndex]
	var scope *webScope
	if seed.Mode == "directory" {
		sc, err := newWebScope(seed.URL, seed.Directory)
		if err != nil {
			return err
		}
		scope = &sc
	}
	if !robotsAllows(page.URL, policies[webOrigin(seed.URL)]) {
		skipped := page
		skipped.Status, skipped.Error = "skipped", "robots.txt 禁止访问"
		return l.crawlSavePage(ctx, skipped)
	}
	running := page
	running.Status = "running"
	if err := l.crawlSavePage(ctx, running); err != nil {
		return err
	}
	fail := func(cause error) error {
		failed := page
		failed.Error = cause.Error()
		if ctx.Err() != nil {
			failed.Status = "pending"
			if cur, err := l.crawlJobGet(context.Background(), job.ID); err == nil && cur != nil && cur.Status == "canceled" {
				failed.Status = "canceled"
			}
		} else {
			failed.Status = "failed"
		}
		return l.crawlSavePage(context.Background(), failed)
	}
	result := l.captureWebPage(ctx, webCaptureRequest{TaskID: page.ID, Purpose: job.Input.Purpose, URL: page.URL, Scope: scope}, func(stage string) {
		st := page
		st.Status, st.Stage = "running", stage
		if err := l.crawlSavePage(context.Background(), st); err != nil {
			logAppErrorf("main", "网页批次", "写入采集阶段失败：%v", err)
		}
	})
	if ctx.Err() != nil {
		return fail(errors.New(crawlAbortMessage))
	}
	if result.Error != nil || !result.Complete {
		msg := "正文不完整，未入库"
		if result.Error != nil {
			msg = result.Error.Message
		}
		return fail(errors.New(msg))
	}
	if err := s.persist(ctx, job, page, result, scope); err != nil {
		return fail(err)
	}
	return nil
}

// crawlExistingItem 对应 persist 里按规范化网址查找未删除的已有条目。
func (l *Library) crawlExistingItem(ctx context.Context, finalURI, pageURI any) (string, error) {
	rows, err := l.db.Query(ctx, "SELECT s.item_id FROM source_records s JOIN knowledge_items i ON i.id=s.item_id WHERE (s.normalized_uri=? OR s.normalized_uri=?) AND i.deleted_at IS NULL ORDER BY s.captured_at DESC LIMIT 1", finalURI, pageURI)
	if err != nil || len(rows.Values) == 0 {
		return "", err
	}
	id, _ := rows.Values[0][0].(string)
	return id, nil
}

func normalizedURIArg(raw string) any {
	if u, ok := normalizeURL(raw); ok {
		return u
	}
	return nil
}

func (s *crawlService) persist(ctx context.Context, job crawlJob, page crawlPage, result webCaptureResult, scope *webScope) error {
	l := s.l
	resultJSON, err := marshalNoEscape(result)
	if err != nil {
		return err
	}
	saved := page
	saved.Result = json.RawMessage(resultJSON)
	saved.Stage = ""

	if job.Input.Purpose != "documents" {
		saved.Status = "added"
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		return l.crawlCommit(ctx, job, page, saved, result, scope, nil)
	}

	finalURI, pageURI := normalizedURIArg(result.FinalURL), normalizedURIArg(page.URL)
	l.writeMu.Lock()
	existing, err := l.crawlExistingItem(ctx, finalURI, pageURI)
	if err != nil {
		l.writeMu.Unlock()
		return err
	}
	if existing == "" { // 新建条目：条目、来源记录、原文版本与页面状态同一事务
		defer l.writeMu.Unlock()
		in := CreateInput{
			Title:    optString{Set: true, Val: result.Title},
			Content:  optString{Set: true, Val: result.Markdown},
			ItemType: optString{Set: true, Val: "webpage"},
		}
		if cid := job.Input.collectionIDString(); cid != "" {
			in.CollectionID = optString{Set: true, Val: cid}
		}
		itemID, stmts, err := l.knowledgeCreateStmts(ctx, in)
		if err != nil {
			return err
		}
		stmts = append(stmts, sqlite.Statement{
			SQL:  "INSERT INTO source_records (id,item_id,source_type,source_uri,normalized_uri,content_hash,captured_at,platform) VALUES (?,?,?,?,?,?,?,?)",
			Args: []any{newUUID(), itemID, "url", result.FinalURL, finalURI, result.ContentHash, result.CapturedAt, platformArg(result.FinalURL)},
		})
		stmts = append(stmts, webSourceInitStmts(itemID, result.Title, result.Markdown, result)...)
		saved.ItemID, saved.Status = itemID, "added"
		return l.crawlCommit(ctx, job, page, saved, result, scope, stmts)
	}
	l.writeMu.Unlock()

	saved.ItemID, saved.Status = existing, "duplicate"
	if job.Input.DuplicatePolicy == "update" {
		if saved.Status, err = l.webSourceCheck(ctx, existing, result); err != nil {
			return err
		}
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return l.crawlCommit(ctx, job, page, saved, result, scope, nil)
}

func platformArg(rawURL string) any {
	if pl, ok := resolveSourcePlatform("url", &rawURL); ok {
		return pl
	}
	return nil
}

// crawlCommit 追加导入任务、页面状态与新发现的链接，并与 prefix 一起单次提交。调用方须持有 writeMu。
func (l *Library) crawlCommit(ctx context.Context, job crawlJob, page, saved crawlPage, result webCaptureResult, scope *webScope, prefix []sqlite.Statement) error {
	stmts := prefix
	if job.Input.Purpose == "documents" {
		taskStmts, taskID, err := l.crawlTaskStmts(ctx, job, page, saved, result)
		if err != nil {
			return err
		}
		stmts = append(stmts, taskStmts...)
		if taskID != "" {
			saved.ImportTaskID = taskID
		}
	}
	stmts = append(stmts, crawlSaveStmt(saved))
	if scope != nil && page.Depth < job.Input.MaxDepth {
		disc, err := l.crawlDiscoverStmts(ctx, job, page, result.Links, scope)
		if err != nil {
			return err
		}
		stmts = append(stmts, disc...)
	}
	_, err := l.db.Transaction(ctx, stmts)
	return err
}

// crawlTaskStmts 对应 persist 之后的 ImportTaskDB create/update：在导入历史里留下这一页的记录。
func (l *Library) crawlTaskStmts(ctx context.Context, job crawlJob, page, saved crawlPage, result webCaptureResult) ([]sqlite.Statement, string, error) {
	status, dup := "completed", any(nil)
	if saved.Status == "duplicate" {
		status, dup = "duplicate", saved.ItemID
	}
	var warning any
	if saved.Status == "pending-version" {
		warning = "原文有新版本可比较，已保留当前正文"
	}
	title := jsTrim(result.Title)
	now := nowMs()
	if page.ImportTaskID != "" {
		rows, err := l.db.Query(ctx, "SELECT id FROM import_tasks WHERE id = ?", page.ImportTaskID)
		if err != nil || len(rows.Values) == 0 {
			return nil, "", err // 任务已被清理：与 TS 一致，不再补建
		}
		return []sqlite.Statement{{
			SQL: `UPDATE import_tasks SET status = ?, display_name = COALESCE(NULLIF(?, ''), display_name), item_type = 'webpage',
         result_item_id = ?, duplicate_item_id = ?, warning = ?, warning_acknowledged_at = NULL, updated_at = ? WHERE id = ?`,
			Args: []any{status, nodeUTF8String(title), saved.ItemID, dup, warning, now, page.ImportTaskID},
		}}, page.ImportTaskID, nil
	}
	name := title
	if name == "" {
		name = makeDisplayName("url", page.URL)
	}
	var collection any
	if cid := job.Input.collectionIDString(); cid != "" {
		collection = cid
	}
	id := newUUID()
	return []sqlite.Statement{{
		SQL: `INSERT INTO import_tasks
         (id, source_kind, source_input, display_name, status, collection_id, refresh_of_item_id, tag_names, capture_strategy, comment_limit, force_duplicate, created_at, updated_at, item_type, result_item_id, duplicate_item_id, warning)
       VALUES (?, 'url', ?, ?, ?, ?, NULL, NULL, 'standard', 0, 0, ?, ?, 'webpage', ?, ?, ?)`,
		Args: []any{id, jsTrim(page.URL), nodeUTF8String(name), status, collection, now, now, saved.ItemID, dup, warning},
	}}, id, nil
}

// crawlDiscoverStmts 对应在事务里逐个 jobs.discover：页数上限、深度上限与同批次内网址去重按顺序累计。
func (l *Library) crawlDiscoverStmts(ctx context.Context, job crawlJob, page crawlPage, links []string, scope *webScope) ([]sqlite.Statement, error) {
	rows, err := l.db.Query(ctx, "SELECT url FROM crawl_pages WHERE job_id=?", job.ID)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, r := range rows.Values {
		u, _ := r[0].(string)
		known[u] = true
	}
	var stmts []sqlite.Statement
	depth := page.Depth + 1
	for _, link := range links {
		if !inWebScope(link, *scope) || !isWebPageLink(link) {
			continue
		}
		if len(known) >= job.Input.MaxPages || depth > job.Input.MaxDepth {
			continue
		}
		next, err := newCrawlPage(job.ID, link, depth, page.SeedIndex)
		if err != nil {
			return nil, err
		}
		if known[next.URL] {
			continue
		}
		known[next.URL] = true
		stmts = append(stmts, crawlDiscoverStmt(next))
	}
	return stmts, nil
}

func (l *Library) registerCrawl(b *Bridge) {
	svc := newCrawlService(l)
	l.crawl = svc
	if err := l.crawlRecover(context.Background()); err != nil {
		logAppErrorf("main", "网页批次", "恢复遗留批次失败：%v", err)
	}
	recordValue := func(v any) (string, error) {
		s, ok := v.(string)
		if !ok || s == "" || utf16Len(s) > 100 {
			return "", errors.New("记录 ID 无效")
		}
		return s, nil
	}
	recordID := func(a []json.RawMessage) (string, error) { return recordValue(jsArg(a, 0)) }
	// 对应 response()：成功 {ok:true,data}，失败 {ok:false,error}
	reply := func(data any, err error) (any, error) {
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		if data == nil {
			return map[string]any{"ok": true}, nil
		}
		return map[string]any{"ok": true, "data": data}, nil
	}
	b.handlers["crawl:create"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		in := jsArg(a, 0)
		if p, _ := jsGet(in, "purpose").(string); p != "documents" {
			return reply(nil, errors.New("网页研究批次必须通过研究流程创建"))
		}
		id, err := svc.create(ctx, in)
		if err != nil {
			return reply(nil, err)
		}
		return reply(id, nil)
	}
	b.handlers["crawl:list"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		jobs, err := l.crawlJobList(ctx)
		if err != nil {
			return reply(nil, err)
		}
		return reply(jobs, nil)
	}
	b.handlers["crawl:get"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, err := recordID(a)
		if err != nil {
			return reply(nil, err)
		}
		job, err := l.crawlJobGet(ctx, id)
		if err != nil {
			return reply(nil, err)
		}
		pages, err := l.crawlPages(ctx, id)
		if err != nil {
			return reply(nil, err)
		}
		return reply(map[string]any{"job": job, "pages": pages}, nil)
	}
	b.handlers["crawl:pause"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, err := recordID(a)
		if err == nil {
			err = svc.pause(ctx, id)
		}
		return reply(nil, err)
	}
	b.handlers["crawl:resume"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, err := recordID(a)
		if err != nil {
			return reply(nil, err)
		}
		job, err := l.crawlJobGet(ctx, id)
		if err != nil {
			return reply(nil, err)
		}
		if job == nil || job.Status == "canceled" {
			return reply(nil, errors.New("批次不存在或已经取消"))
		}
		go func() {
			if err := svc.resume(id); err != nil { // 对应 .catch(error => setStatus(id, "paused", String(error)))
				_ = l.crawlJobSetStatus(context.Background(), id, "paused", sp(fmt.Sprintf("Error: %v", err)))
			}
		}()
		return reply(nil, nil)
	}
	b.handlers["crawl:cancel"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, err := recordID(a)
		if err == nil {
			err = svc.cancel(ctx, id)
		}
		return reply(nil, err)
	}
	b.handlers["crawl:retry"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, err := recordID(a)
		if err == nil {
			err = l.crawlJobRetry(ctx, id)
		}
		if err == nil {
			svc.start(id)
		}
		return reply(nil, err)
	}
	// web:adopt：把某个完整的原文版本采用为正文（调用方给出比较时的正文哈希与标题作乐观锁）
	b.handlers["web:adopt"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		in := jsArg(a, 0)
		itemID, err := recordValue(jsGet(in, "itemId"))
		if err != nil {
			return reply(nil, err)
		}
		versionID, err := recordValue(jsGet(in, "versionId"))
		if err != nil {
			return reply(nil, err)
		}
		hash, hashOK := jsGet(in, "expectedContentHash").(string)
		title, titleOK := jsGet(in, "expectedTitle").(string)
		if !hashOK || !reHex64.MatchString(hash) || !titleOK {
			return reply(nil, errors.New("版本比较基线无效"))
		}
		return reply(nil, l.webSourceAdopt(ctx, webAdoptInput{ItemID: itemID, VersionID: versionID, ExpectedContentHash: hash, ExpectedTitle: title}))
	}
}

var reHex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
