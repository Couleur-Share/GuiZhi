package main

// 对应 services/import/{import-queue,import-service,stage-stats}.ts：并发调度、DB 持久化、重启恢复、
// 取消/重试、变更广播，以及去重与入库。抽取（extract）是注入的，文本/文件在 import_connectors.go，
// url 类来源待采集一族迁入。

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/sqlite"
)

const defaultImportConcurrency = 2

// ---- 阶段耗时（stage-stats.ts） ----

// stageStat 保持 JS 对象的属性插入顺序：stage、ms、calls、failedCalls、promptTokens、completionTokens、models。
type stageStat struct {
	stage string
	ms    int64
	keys  []string
	nums  map[string]int64
	model []string
}

func (s *stageStat) addNum(key string, n int64) {
	if _, ok := s.nums[key]; !ok {
		s.keys = append(s.keys, key)
	}
	s.nums[key] += n
}

type stageRecorder struct {
	now       func() int64
	stats     []*stageStat
	current   *stageStat
	enteredAt int64
}

func newStageRecorder() *stageRecorder {
	r := &stageRecorder{now: nowMs}
	r.enteredAt = r.now()
	return r
}

// transition 切换到新阶段（"" 表示任务结束）并结算上一个阶段；同一阶段再次进入时累加到原有那条。
func (r *stageRecorder) transition(stage string) {
	at := r.now()
	if r.current != nil {
		r.current.ms += max(0, at-r.enteredAt)
	}
	r.enteredAt = at
	if stage == "" {
		r.current = nil
		return
	}
	for _, e := range r.stats {
		if e.stage == stage {
			r.current = e
			return
		}
	}
	r.current = &stageStat{stage: stage, nums: map[string]int64{}}
	r.stats = append(r.stats, r.current)
}

// aiCallRecord 对应 AiCallRecord；记到当前阶段，阶段之外的调用丢弃。
type aiCallRecord struct {
	Failed                         bool
	PromptTokens, CompletionTokens int64
	Model                          string
}

func (r *stageRecorder) recordAICall(rec aiCallRecord) {
	c := r.current
	if c == nil {
		return
	}
	c.addNum("calls", 1)
	if rec.Failed {
		c.addNum("failedCalls", 1)
	}
	if rec.PromptTokens != 0 {
		c.addNum("promptTokens", rec.PromptTokens)
	}
	if rec.CompletionTokens != 0 {
		c.addNum("completionTokens", rec.CompletionTokens)
	}
	if rec.Model != "" && !slices.Contains(c.model, rec.Model) {
		c.model = append(c.model, rec.Model)
	}
}

// snapshotJSON 序列化当前快照（含进行中阶段的已用时长）。
func (r *stageRecorder) snapshotJSON() string {
	pending := int64(0)
	if r.current != nil {
		pending = max(0, r.now()-r.enteredAt)
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, e := range r.stats {
		if i > 0 {
			b.WriteByte(',')
		}
		ms := e.ms
		if e == r.current {
			ms += pending
		}
		b.WriteString(`{"stage":` + jsQuote(e.stage) + `,"ms":` + strconv.FormatInt(ms, 10))
		for _, k := range e.keys {
			b.WriteString(`,"` + k + `":` + strconv.FormatInt(e.nums[k], 10))
		}
		if len(e.model) > 0 {
			ms, _ := json.Marshal(e.model)
			b.WriteString(`,"models":` + string(ms))
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}

// ---- 队列 ----

type importExtractFn func(ctx context.Context, task map[string]any, onStage func(string)) (*extractedContent, error)

type importQueue struct {
	l             *Library
	concurrency   int
	extract       importExtractFn
	resolveSource func(ctx context.Context, raw string) (string, bool)
	onChanged     func(task map[string]any)

	mu         sync.Mutex
	pending    []string
	running    map[string]context.CancelFunc
	paused     bool
	sourceRuns map[string]chan struct{}
	webVerify  map[string]bool // webVerificationTasks
	saveMu     sync.Mutex      // 判重 + 入库的原子段
}

func newImportQueue(l *Library, extract importExtractFn, onChanged func(map[string]any)) *importQueue {
	return &importQueue{
		l: l, concurrency: defaultImportConcurrency, extract: extract, onChanged: onChanged,
		running: map[string]context.CancelFunc{}, sourceRuns: map[string]chan struct{}{}, webVerify: map[string]bool{},
		resolveSource: func(_ context.Context, raw string) (string, bool) { return sourceIdentity(raw) },
	}
}

func (q *importQueue) notify(task map[string]any) {
	if q.onChanged != nil && task != nil {
		q.onChanged(task)
	}
}

func (q *importQueue) state() map[string]any {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.stateLocked()
}

func (q *importQueue) stateLocked() map[string]any {
	return map[string]any{"paused": q.paused, "runningCount": len(q.running), "pendingCount": len(q.pending), "concurrency": q.concurrency}
}

func (q *importQueue) Pause() map[string]any {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.paused = true
	return q.stateLocked()
}

func (q *importQueue) Resume() map[string]any {
	q.mu.Lock()
	q.paused = false
	q.pumpLocked()
	st := q.stateLocked()
	q.mu.Unlock()
	return st
}

// Recover 启动恢复：把上次遗留的 processing 复位并连同 pending 一起入队。
// 必须按状态查询而不是取「最近 200 条」，否则一次入队几百个文件后重启，最早那批会永远不被调度。
func (q *importQueue) Recover(ctx context.Context) error {
	if _, err := q.l.importTaskResetProcessing(ctx); err != nil {
		return err
	}
	pending, err := q.l.importTaskListByStatus(ctx, "pending")
	if err != nil {
		return err
	}
	// listByStatus 已按 created_at 升序；JS 的 sort 稳定，同一毫秒保持原顺序
	for _, task := range pending {
		id := task["id"].(string)
		// 启动恢复不能自行弹出可见浏览器：登录态任务保留原策略，但停在可手动重试的状态
		if task["captureStrategy"] == "authenticated" {
			q.mu.Lock()
			if i := slices.Index(q.pending, id); i >= 0 {
				q.pending = slices.Delete(q.pending, i, i+1)
			}
			q.mu.Unlock()
			recovered, err := q.l.importTaskUpdate(ctx, id, taskPatch{status: sp("failed"), stage: nnull(), err: nset("[login_required] 登录态任务已从中断中恢复，请手动重试")})
			if err != nil {
				return err
			}
			q.notify(recovered)
			continue
		}
		q.mu.Lock()
		if !slices.Contains(q.pending, id) {
			q.pending = append(q.pending, id)
		}
		q.mu.Unlock()
	}
	q.mu.Lock()
	q.pumpLocked()
	q.mu.Unlock()
	return nil
}

// Enqueue 对应 enqueue(inputs)；inputs 是已通过校验的 EnqueueImportInput（jsvalue）。
func (q *importQueue) Enqueue(ctx context.Context, inputs jsArr) ([]map[string]any, error) {
	tasks := []map[string]any{}
	for _, input := range inputs {
		value := ""
		if s, ok := jsGet(input, "input").(string); ok {
			value = jsTrim(s)
		}
		if value == "" {
			continue
		}
		kind, _ := jsGet(input, "kind").(string)
		identity, hasIdentity := "", false
		if kind == "url" && !vTruthy(jsGet(input, "forceDuplicate")) && !vTruthy(jsGet(input, "refreshOfItemId")) {
			identity, hasIdentity = sourceIdentity(value)
		}
		var existing map[string]any
		if hasIdentity {
			active, err := q.l.importTaskListByStatus(ctx, "pending", "processing")
			if err != nil {
				return tasks, err
			}
			for _, t := range active {
				if t["refreshOfItemId"] != nil || q.l.importTaskIsForceDuplicate(ctx, t["id"].(string)) {
					continue
				}
				if other, ok := sourceIdentity(t["sourceInput"].(string)); ok && other == identity {
					existing = t
					break
				}
			}
		}
		if existing != nil {
			shared, err := q.l.importTaskUpdate(ctx, existing["id"].(string), taskPatch{warning: nset("相同来源已在队列中，已合并到现有任务")})
			if err != nil {
				return tasks, err
			}
			if shared == nil {
				shared = existing
			}
			tasks = append(tasks, shared)
			q.notify(shared)
			continue
		}
		task, err := q.l.importTaskCreate(ctx, input, value)
		if err != nil {
			return tasks, err
		}
		tasks = append(tasks, task)
		q.mu.Lock()
		q.pending = append(q.pending, task["id"].(string))
		q.mu.Unlock()
		q.notify(task)
	}
	q.mu.Lock()
	q.pumpLocked()
	q.mu.Unlock()
	return tasks, nil
}

func (q *importQueue) Cancel(ctx context.Context, id string) (bool, error) {
	q.mu.Lock()
	if cancel, ok := q.running[id]; ok {
		q.mu.Unlock()
		cancel()
		return true, nil
	}
	i := slices.Index(q.pending, id)
	if i < 0 {
		q.mu.Unlock()
		return false, nil
	}
	q.pending = slices.Delete(q.pending, i, i+1)
	q.mu.Unlock()
	task, err := q.l.importTaskUpdate(ctx, id, taskPatch{status: sp("canceled"), stage: nnull()})
	if err != nil {
		return true, err
	}
	q.notify(task)
	return true, nil
}

type retryOptions struct {
	forceDuplicate  *bool
	captureStrategy *string
	commentLimit    *int64
}

// Retry 重试失败/取消的任务；返回 nil 表示任务不存在。
func (q *importQueue) Retry(ctx context.Context, id string, o retryOptions) (map[string]any, error) {
	existing, err := q.l.importTaskGet(ctx, id)
	if err != nil || existing == nil {
		return nil, err
	}
	if s := existing["status"]; s == "pending" || s == "processing" {
		return existing, nil
	}
	task, err := q.l.importTaskUpdate(ctx, id, taskPatch{
		status: sp("pending"), stage: nnull(), err: nnull(), warning: nnull(), duplicateItemID: nnull(),
		stageStats:     nnull(), // 上一轮的耗时不能留：叠在这一轮上，两个数字都不再说明任何事
		forceDuplicate: o.forceDuplicate, captureStrategy: o.captureStrategy, commentLimit: o.commentLimit,
	})
	if err != nil {
		return nil, err
	}
	if task != nil {
		q.mu.Lock()
		q.pending = append(q.pending, task["id"].(string))
		q.mu.Unlock()
		q.notify(task)
		q.mu.Lock()
		q.pumpLocked()
		q.mu.Unlock()
	}
	return task, nil
}

// pumpLocked 起跑待处理任务，直到并发上限；调用方须持有 q.mu。
func (q *importQueue) pumpLocked() {
	if q.paused {
		return
	}
	for len(q.running) < q.concurrency && len(q.pending) > 0 {
		id := q.pending[0]
		q.pending = q.pending[1:]
		ctx, cancel := context.WithCancel(context.Background())
		q.running[id] = cancel
		go func() {
			q.runTask(ctx, id)
			cancel()
			q.mu.Lock()
			delete(q.running, id)
			q.pumpLocked()
			q.mu.Unlock()
		}()
	}
}

// Idle 等待队列清空（测试与关停用）。
func (q *importQueue) Idle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.running) == 0 && len(q.pending) == 0
}

func (q *importQueue) updateAndNotify(ctx context.Context, id string, p taskPatch, hasStage bool, stage string, rec *stageRecorder) {
	if hasStage {
		rec.transition(stage)
		p.stageStats = nset(rec.snapshotJSON())
	}
	task, err := q.l.importTaskUpdate(context.Background(), id, p)
	if err != nil {
		logAppErrorf("import-queue", "update", "写任务 %s 失败：%v", id, err)
		return
	}
	q.notify(task)
}

func (q *importQueue) runTask(ctx context.Context, id string) {
	task, err := q.l.importTaskGet(context.Background(), id)
	if err != nil || task == nil || task["status"] != "pending" {
		return
	}
	q.processTask(ctx, id, task, newStageRecorder())
}

func abortedErr(ctx context.Context) error {
	if ctx.Err() != nil {
		return errors.New("已取消")
	}
	return nil
}

func (q *importQueue) processTask(ctx context.Context, id string, task map[string]any, rec *stageRecorder) {
	bg := context.Background()
	stagePatch := func(stage string) (taskPatch, string) {
		return taskPatch{stage: nset(stage)}, stage
	}
	q.updateAndNotify(ctx, id, taskPatch{status: sp("processing"), stage: nset("fetching")}, true, "fetching", rec)

	var extracted *extractedContent
	var releaseSource func()
	sourceKey := ""
	defer func() {
		if releaseSource != nil {
			releaseSource()
		}
		if extracted != nil {
			q.l.disposeExtractedContent(extracted)
		}
	}()
	fail := func(err error) {
		if ctx.Err() != nil {
			q.updateAndNotify(ctx, id, taskPatch{status: sp("canceled"), stage: nnull()}, true, "", rec)
			return
		}
		q.updateAndNotify(ctx, id, taskPatch{status: sp("failed"), stage: nnull(), err: nset(err.Error())}, true, "", rec)
	}

	if err := abortedErr(ctx); err != nil {
		fail(err)
		return
	}
	force := q.l.importTaskIsForceDuplicate(bg, id)
	if task["sourceKind"] == "url" && !force && task["refreshOfItemId"] == nil {
		key, ok := q.resolveSource(ctx, task["sourceInput"].(string))
		if err := abortedErr(ctx); err != nil {
			fail(err)
			return
		}
		if ok {
			sourceKey = key
			for {
				q.mu.Lock()
				ch, busy := q.sourceRuns[sourceKey]
				q.mu.Unlock()
				if !busy {
					break
				}
				q.updateAndNotify(ctx, id, taskPatch{warning: nset("相同来源正在处理，等待已有任务结果")}, false, "", rec)
				select {
				case <-ch:
				case <-ctx.Done():
				}
				if err := abortedErr(ctx); err != nil {
					fail(err)
					return
				}
			}
			if dupID := q.l.importFindDuplicate(bg, &sourceKey, ""); dupID != "" {
				_ = q.l.importRememberSourceAccess(bg, dupID, sourceKey, task["sourceInput"].(string))
				p := taskPatch{status: sp("duplicate"), stage: nnull(), duplicateItemID: nset(dupID)}
				if title, itemType, ok := q.l.importDuplicateMeta(bg, dupID); ok {
					p.displayName, p.itemType = &title, nset(itemType)
				}
				q.updateAndNotify(ctx, id, p, true, "", rec)
				return
			}
			ch := make(chan struct{})
			q.mu.Lock()
			q.sourceRuns[sourceKey] = ch
			q.mu.Unlock()
			releaseSource = func() {
				q.mu.Lock()
				delete(q.sourceRuns, sourceKey)
				q.mu.Unlock()
				close(ch)
			}
		}
	}

	onStage := func(stage string) {
		p, s := stagePatch(stage)
		q.updateAndNotify(ctx, id, p, true, s, rec)
	}
	var err error
	extracted, err = q.extract(withAICallSink(ctx, rec.recordAICall), task, onStage)
	if err != nil {
		fail(err)
		return
	}
	if err := abortedErr(ctx); err != nil {
		fail(err)
		return
	}
	// 标题与类型在这里回写：建任务时只有原始 URL，一列长得一样的链接没法辨认采的是什么
	title := extracted.Title
	q.updateAndNotify(ctx, id, taskPatch{stage: nset("extracting"), displayName: &title, itemType: nset(extracted.ItemType)}, true, "extracting", rec)

	// 降级结果按失败处理：不入库，也不登记来源，否则空壳条目会占住 normalized_uri，重试永远判重为重复
	if extracted.DegradedReason != "" {
		q.updateAndNotify(ctx, id, taskPatch{status: sp("failed"), stage: nnull(), err: nset(extracted.DegradedReason)}, true, "", rec)
		return
	}

	var normalizedURI *string
	if task["sourceKind"] == "url" && extracted.SourceURI != nil && *extracted.SourceURI != "" {
		if n, ok := sourceIdentity(*extracted.SourceURI); ok {
			normalizedURI = &n
		}
	}
	hashed := extracted.Content
	if hashed == "" {
		hashed = extracted.Title
	}
	contentHash := computeContentHash(hashed)

	// 判重与入库必须原子：TS 版靠单线程同步段保证，这里并发 2 个任务，没有这把锁时
	// 内容相同的两个任务会同时通过判重并各存一条。
	var dupID, resultItemID string
	commit := func() (stage string) {
		q.saveMu.Lock()
		defer q.saveMu.Unlock()
		if !force && task["refreshOfItemId"] == nil {
			if dupID = q.l.importFindDuplicate(bg, normalizedURI, contentHash); dupID != "" {
				if task["sourceKind"] == "url" && normalizedURI != nil {
					_ = q.l.importRememberSourceAccess(bg, dupID, *normalizedURI, task["sourceInput"].(string))
				}
				return "duplicate"
			}
		}
		if abortedErr(ctx) != nil {
			return "aborted"
		}
		q.updateAndNotify(ctx, id, taskPatch{stage: nset("saving")}, true, "saving", rec)
		resultItemID, err = q.l.importSaveItem(bg, importSaveParams{
			extracted: extracted, task: task, normalizedURI: normalizedURI, contentHash: contentHash,
		})
		if err != nil {
			return "error"
		}
		return "saved"
	}
	switch commit() {
	case "duplicate":
		q.updateAndNotify(ctx, id, taskPatch{status: sp("duplicate"), stage: nnull(), duplicateItemID: nset(dupID)}, true, "", rec)
		return
	case "aborted":
		fail(abortedErr(ctx))
		return
	case "error":
		fail(err)
		return
	}
	var commentsWarning string
	if n, _ := task["commentLimit"].(int64); n > 0 && q.l.captureComments != nil {
		q.updateAndNotify(ctx, id, taskPatch{stage: nset("comments")}, true, "comments", rec)
		if err := q.l.captureComments(ctx, task, resultItemID); err != nil {
			commentsWarning = "热门评论采集失败：" + err.Error()
		}
	}
	var warnings []string
	for _, w := range []string{extracted.WarningReason, commentsWarning} {
		if w != "" {
			warnings = append(warnings, w)
		}
	}
	warning := nnull()
	if len(warnings) > 0 {
		warning = nset(strings.Join(warnings, "；"))
	}
	// 入库了但内容有缺失时，「已完成」这三个字必须带上下文
	q.updateAndNotify(ctx, id, taskPatch{status: sp("completed"), stage: nnull(), warning: warning, resultItemID: nset(resultItemID)}, true, "", rec)
}

// ---- 去重与入库（import-service.ts 的 createPersistence） ----

func (l *Library) importFindDuplicate(ctx context.Context, normalizedURI *string, contentHash string) string {
	// 只匹配未删除条目；回收站中的重复项允许重新导入
	if normalizedURI != nil {
		rows, err := l.db.Query(ctx, `SELECT s.item_id AS item_id FROM source_records s
           JOIN knowledge_items i ON i.id = s.item_id
           WHERE s.normalized_uri = ? AND i.deleted_at IS NULL
           ORDER BY s.captured_at DESC LIMIT 1`, *normalizedURI)
		if err == nil && len(rows.Values) > 0 {
			return rows.Values[0][0].(string)
		}
	}
	if contentHash == "" {
		return ""
	}
	rows, err := l.db.Query(ctx, `SELECT s.item_id AS item_id FROM source_records s
         JOIN knowledge_items i ON i.id = s.item_id
         WHERE s.content_hash = ? AND i.deleted_at IS NULL
         ORDER BY s.captured_at DESC LIMIT 1`, contentHash)
	if err == nil && len(rows.Values) > 0 {
		return rows.Values[0][0].(string)
	}
	return ""
}

func (l *Library) importDuplicateMeta(ctx context.Context, id string) (string, string, bool) {
	rows, err := l.db.Query(ctx, "SELECT title, item_type FROM knowledge_items WHERE id = ?", id)
	if err != nil || len(rows.Values) == 0 {
		return "", "", false
	}
	title, _ := rows.Values[0][0].(string)
	itemType, _ := rows.Values[0][1].(string)
	return title, itemType, true
}

// importRememberSourceAccess 对应 SourceAccessDB.remember：只刷新规范链接相同的来源。
func (l *Library) importRememberSourceAccess(ctx context.Context, itemID, normalizedURI, accessURI string) error {
	if normalizedURI == "" || jsTrim(accessURI) == "" {
		return nil
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: `UPDATE source_records SET access_uri = ?
       WHERE item_id = ? AND normalized_uri = ? AND source_type = 'url'`,
		Args: []any{jsTrim(accessURI), itemID, normalizedURI},
	}})
	return err
}

type importSaveParams struct {
	extracted     *extractedContent
	task          map[string]any
	normalizedURI *string
	contentHash   string
}

func (l *Library) importSaveItem(ctx context.Context, p importSaveParams) (string, error) {
	ex, task := p.extracted, p.task
	sourceKind, _ := task["sourceKind"].(string)
	sourceInput, _ := task["sourceInput"].(string)
	reasons := assessImportReview(ex.Content, ex.ItemType, ex.WarningReason, sourceKind)

	refresh, _ := task["refreshOfItemId"].(string)
	if refresh != "" && ex.WebCapture != nil && ex.WebCapture.Snapshot != nil && isWechatURL(sourceInput) {
		// 公众号补采：只追加原文快照版本，不改正文
		rows, err := l.db.Query(ctx, "SELECT source_uri FROM source_records WHERE item_id=? ORDER BY captured_at DESC LIMIT 1", refresh)
		if err != nil {
			return "", err
		}
		original := ""
		if len(rows.Values) > 0 {
			original, _ = rows.Values[0][0].(string)
		}
		a, aok := normalizeURL(original)
		b, bok := normalizeURL(sourceInput)
		if len(rows.Values) == 0 || aok != bok || a != b {
			return "", errors.New("补采来源与原条目不一致")
		}
		return refresh, l.webSourceAttach(ctx, refresh, *ex.WebCapture)
	}
	if refresh != "" {
		rows, err := l.db.Query(ctx, `SELECT i.deleted_at, (
         SELECT s.source_uri FROM source_records s WHERE s.item_id = i.id ORDER BY s.captured_at DESC LIMIT 1
       ) AS source_uri FROM knowledge_items i WHERE i.id = ?`, refresh)
		if err != nil {
			return "", err
		}
		if len(rows.Values) == 0 || rows.Values[0][0] != nil {
			return "", errors.New("来源条目不存在或已移入回收站")
		}
		originalURI, _ := rows.Values[0][1].(string)
		current := sourceInput
		if ex.SourceURI != nil && *ex.SourceURI != "" {
			current = *ex.SourceURI
		}
		a, aok := sourceIdentity(originalURI)
		b, bok := sourceIdentity(current)
		if originalURI == "" || aok != bok || a != b {
			return "", errors.New("更新来源与原条目不一致")
		}
		access := sourceInput
		if p.normalizedURI != nil && *p.normalizedURI != "" {
			access = *p.normalizedURI
		}
		reviewJSON, _ := jsStringify(stringsToJS(reasons))
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		stmts := []sqlite.Statement{{
			SQL:  "INSERT INTO source_capture_revisions VALUES(?,?,?,?,?,?,?)",
			Args: []any{newUUID(), refresh, ex.Title, ex.Content, ptrSQL(ex.Transcript), reviewJSON, nowMs()},
		}}
		if jsTrim(sourceInput) != "" && access != "" {
			stmts = append(stmts, sqlite.Statement{
				SQL: `UPDATE source_records SET access_uri = ?
       WHERE item_id = ? AND normalized_uri = ? AND source_type = 'url'`,
				Args: []any{jsTrim(sourceInput), refresh, access},
			})
		}
		_, err = l.db.Transaction(ctx, stmts)
		return refresh, err
	}

	in := CreateInput{}
	set := func(o *optString, v string) { *o = optString{Set: true, Val: v} }
	if ex.Title != "" {
		set(&in.Title, ex.Title)
	}
	set(&in.Content, ex.Content)
	if ex.Transcript != nil {
		set(&in.Transcript, *ex.Transcript)
	}
	set(&in.ItemType, ex.ItemType)
	if c, ok := task["collectionId"].(string); ok {
		set(&in.CollectionID, c)
	}
	if tags, _ := task["tagNames"].([]string); len(tags) > 0 {
		in.TagNames = optStrings{Set: true, Val: tags}
	}
	// 状态留在条目而非 import_tasks：用户清理任务记录后仍可回到原文完成复核
	if len(reasons) > 0 {
		set(&in.ReviewStatus, "needs_review")
	} else {
		set(&in.ReviewStatus, "clear")
	}
	in.ReviewReasons = optStrings{Set: true, Val: reasons}

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	id, stmts, err := l.knowledgeCreateStmts(ctx, in)
	if err != nil {
		return "", err
	}
	if ex.WebCapture != nil {
		stmts = append(stmts, webSourceInitStmts(id, in.Title.orDefault(""), in.Content.orDefault(""), *ex.WebCapture)...)
	}
	var accessURI, normalized any
	if sourceKind == "url" {
		accessURI = sourceInput
	}
	if p.normalizedURI != nil {
		normalized = *p.normalizedURI
	}
	var platform any
	if pl, ok := resolveSourcePlatform(sourceKind, ex.SourceURI); ok {
		platform = pl
	}
	stmts = append(stmts, sqlite.Statement{
		SQL: `INSERT INTO source_records
             (id, item_id, source_type, source_uri, access_uri, normalized_uri, content_hash, platform, captured_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		Args: []any{newUUID(), id, sourceKind, ptrSQL(ex.SourceURI), accessURI, normalized, p.contentHash, platform, nowMs()},
	})
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return "", err
	}
	return id, nil
}

func ptrSQL(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func stringsToJS(ss []string) jsArr {
	out := make(jsArr, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func logAppErrorf(scope, action, format string, args ...any) {
	log.Printf("[%s] %s: "+format, append([]any{scope, action}, args...)...)
}
