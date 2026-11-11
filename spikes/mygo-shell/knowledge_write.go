package main

// 对应 packages/db/src/knowledge.ts 的写入侧：get / create / update / bulkUpdate / setStatus /
// moveToTrash / restore / deleteForever，以及 knowledge_fts、knowledge_fts_map 与标签关联的同步维护。
//
// MyGo 的 SQLite 事务是「一批静态语句」（BEGIN IMMEDIATE … COMMIT），不能在事务里穿插读取。
// 所以每个写操作先读后写：读出需要的现状，拼出完整语句批，一次提交。
// 归知本来就是单写入方，Library.writeMu 把 Go 侧的写操作串行化，等价于 TS 里同步 wasm 的串行性。

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

const bulkWriteChunkSize = 400

// ── 可选字段：区分「未传」「null」「有值」，对应 TS 里 undefined / null / value ──

type optString struct {
	Set, Null bool
	Val       string
}

func (o *optString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Val)
}

// orDefault 对应 JS 的 `x ?? def`：未传或 null 都取默认。
func (o optString) orDefault(def string) string {
	if !o.Set || o.Null {
		return def
	}
	return o.Val
}

// sql 对应「!== undefined 才覆盖」的字段：返回写库用的值（null → nil）。
func (o optString) sql(existing any) any {
	if !o.Set {
		return existing
	}
	if o.Null {
		return nil
	}
	return o.Val
}

type optBool struct {
	Set bool
	Val bool
}

func (o *optBool) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Val = false // JS：null 在 `? 1 : 0` 里是假值
		return nil
	}
	return json.Unmarshal(b, &o.Val)
}

type optStrings struct {
	Set bool
	Val []string
}

func (o *optStrings) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		return nil // normalizeTagNames(null) → []
	}
	return json.Unmarshal(b, &o.Val)
}

type CreateInput struct {
	Title         optString  `json:"title"`
	Content       optString  `json:"content"`
	Transcript    optString  `json:"transcript"`
	ItemType      optString  `json:"itemType"`
	Status        optString  `json:"status"`
	CollectionID  optString  `json:"collectionId"`
	TagNames      optStrings `json:"tagNames"`
	ReviewStatus  optString  `json:"reviewStatus"`
	ReviewReasons optStrings `json:"reviewReasons"`
}

type UpdateInput struct {
	Title         optString  `json:"title"`
	Content       optString  `json:"content"`
	Summary       optString  `json:"summary"`
	Transcript    optString  `json:"transcript"`
	ItemType      optString  `json:"itemType"`
	Status        optString  `json:"status"`
	CollectionID  optString  `json:"collectionId"`
	IsFavorite    optBool    `json:"isFavorite"`
	IsPinned      optBool    `json:"isPinned"`
	ReviewStatus  optString  `json:"reviewStatus"`
	ReviewReasons optStrings `json:"reviewReasons"`
	TagNames      optStrings `json:"tagNames"`
}

type BulkUpdateInput struct {
	CollectionID   optString  `json:"collectionId"`
	IsFavorite     optBool    `json:"isFavorite"`
	IsPinned       optBool    `json:"isPinned"`
	Status         optString  `json:"status"`
	AddTagNames    optStrings `json:"addTagNames"`
	RemoveTagNames optStrings `json:"removeTagNames"`
	ReviewStatus   optString  `json:"reviewStatus"`
	ReviewReasons  optStrings `json:"reviewReasons"`
}

// ── 工具 ──

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func nowMs() int64 { return time.Now().UnixMilli() }

// normalizeTagNames：去空白、按小写去重、保留首次出现的写法。
func normalizeTagNames(names []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range names {
		name := strings.TrimFunc(raw, jsSpace)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

func normalizeReviewReasons(reasons []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range reasons {
		r = strings.TrimFunc(r, jsSpace)
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// jsStringifyStrings 等价于 JSON.stringify(string[])。
func jsStringifyStrings(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = jsonString(s)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func reviewReasonsSQL(reasons []string) any {
	if len(reasons) == 0 {
		return nil
	}
	return jsStringifyStrings(reasons)
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

type tagRow struct {
	id, name, colorKey   string
	createdAt, updatedAt int64
}

var localeCollator = collate.New(language.Chinese)

// ── 读 ──

const itemColumns = `i.id, i.title, i.content, i.summary, i.transcript, i.item_type, i.status, i.collection_id,
  i.is_favorite, i.is_pinned, i.review_status, i.review_reasons, i.deleted_at, i.created_at, i.updated_at`

func (l *Library) knowledgeGet(ctx context.Context, id string) (any, error) {
	rows, err := l.db.Query(ctx, `SELECT `+itemColumns+`, (
         SELECT s.source_uri FROM source_records s
         WHERE s.item_id = i.id
         ORDER BY s.captured_at DESC LIMIT 1
       ) AS source_uri
       FROM knowledge_items i WHERE i.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	r := rows.Values[0]
	tags, err := l.loadTagsFor(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	t := tags[id]
	if t == nil {
		t = []map[string]any{}
	}
	reviewStatus := r[10]
	if reviewStatus == nil {
		reviewStatus = "clear"
	}
	return map[string]any{
		"id": r[0], "title": r[1], "content": r[2], "summary": r[3], "transcript": r[4],
		"itemType": r[5], "status": r[6], "collectionId": r[7],
		"isFavorite": asInt(r[8]) == 1, "isPinned": asInt(r[9]) == 1,
		"reviewStatus": reviewStatus, "reviewReasons": parseReviewReasons(r[11]),
		"sourceUri": r[15], "deletedAt": r[12], "createdAt": r[13], "updatedAt": r[14],
		"tags": t,
	}, nil
}

// ── 标签与 FTS 的语句生成 ──

// replaceTagsStmts 对应 replaceTags：先清关联，再按名称（不区分大小写）找或建，最后建关联。
// 返回语句、解析出的标签名（按 localeCompare 排序，供 FTS 的 tags 列使用）。
func (l *Library) replaceTagsStmts(ctx context.Context, itemID string, names []string, now int64, pending map[string][2]string) ([]sqlite.Statement, []string, error) {
	stmts := []sqlite.Statement{{SQL: "DELETE FROM knowledge_item_tags WHERE item_id = ?", Args: []any{itemID}}}
	resolved := make([]string, 0, len(names))
	for _, name := range names {
		var tagID, tagName string
		// pending：同一批语句里更早创建、数据库里还看不到的标签（批量更新时多个条目会共用新标签）
		if p, ok := pending[strings.ToLower(name)]; ok {
			tagID, tagName = p[0], p[1]
		} else if rows, err := l.db.Query(ctx, "SELECT id, name FROM tags WHERE LOWER(name) = LOWER(?)", name); err != nil {
			return nil, nil, err
		} else if len(rows.Values) > 0 {
			tagID, _ = rows.Values[0][0].(string)
			tagName, _ = rows.Values[0][1].(string)
		} else {
			tagID, tagName = newUUID(), name
			pending[strings.ToLower(name)] = [2]string{tagID, tagName}
			stmts = append(stmts, sqlite.Statement{
				SQL:  "INSERT INTO tags (id, name, color_key, created_at, updated_at) VALUES (?, ?, 'gray', ?, ?)",
				Args: []any{tagID, name, now, now},
			})
		}
		stmts = append(stmts, sqlite.Statement{
			SQL:  "INSERT OR IGNORE INTO knowledge_item_tags (item_id, tag_id) VALUES (?, ?)",
			Args: []any{itemID, tagID},
		})
		resolved = append(resolved, tagName)
	}
	sort.SliceStable(resolved, func(a, b int) bool { return localeCollator.CompareString(resolved[a], resolved[b]) < 0 })
	return stmts, resolved, nil
}

// writeFtsStmts 对应 deleteFtsRow + writeFts：映射表有记录就按 rowid 删（避免 UNINDEXED 列线性扫描），否则按 item_id 删。
func (l *Library) writeFtsStmts(ctx context.Context, itemID, title, content string, tagNames []string) ([]sqlite.Statement, error) {
	stmts, err := l.deleteFtsStmts(ctx, itemID)
	if err != nil {
		return nil, err
	}
	return append(stmts,
		sqlite.Statement{
			SQL:  "INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?, ?, ?, ?)",
			Args: []any{itemID, SegmentTextForFts(title), SegmentTextForFts(content), SegmentTextForFts(strings.Join(tagNames, " "))},
		},
		sqlite.Statement{
			SQL:  "INSERT OR REPLACE INTO knowledge_fts_map (item_id, fts_rowid) VALUES (?, last_insert_rowid())",
			Args: []any{itemID},
		}), nil
}

func (l *Library) deleteFtsStmts(ctx context.Context, itemID string) ([]sqlite.Statement, error) {
	rows, err := l.db.Query(ctx, "SELECT fts_rowid FROM knowledge_fts_map WHERE item_id = ?", itemID)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) > 0 {
		return []sqlite.Statement{
			{SQL: "DELETE FROM knowledge_fts WHERE rowid = ?", Args: []any{rows.Values[0][0]}},
			{SQL: "DELETE FROM knowledge_fts_map WHERE item_id = ?", Args: []any{itemID}},
		}, nil
	}
	return []sqlite.Statement{{SQL: "DELETE FROM knowledge_fts WHERE item_id = ?", Args: []any{itemID}}}, nil
}

func (l *Library) currentTagNames(ctx context.Context, itemID string) ([]string, error) {
	m, err := l.loadTagsFor(ctx, []string{itemID})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, t := range m[itemID] {
		names = append(names, t["name"].(string))
	}
	return names, nil
}

// ── 写 ──

func (l *Library) knowledgeCreate(ctx context.Context, in CreateInput) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	id, stmts, err := l.knowledgeCreateStmts(ctx, in)
	if err != nil {
		return nil, err
	}
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return l.knowledgeGet(ctx, id)
}

// knowledgeCreateStmts 生成创建条目所需的全部语句，由调用方决定事务边界（调用方须持有 writeMu）。
func (l *Library) knowledgeCreateStmts(ctx context.Context, in CreateInput) (string, []sqlite.Statement, error) {
	now, id := nowMs(), newUUID()
	tagNames := normalizeTagNames(in.TagNames.Val)
	reasons := normalizeReviewReasons(in.ReviewReasons.Val)
	title, content := in.Title.orDefault(""), in.Content.orDefault("")
	reviewStatus := in.ReviewStatus.orDefault("clear")
	if !in.ReviewStatus.Set || in.ReviewStatus.Null {
		if len(reasons) > 0 {
			reviewStatus = "needs_review"
		}
	}
	stmts := []sqlite.Statement{{
		SQL: `INSERT INTO knowledge_items
           (id, title, content, transcript, item_type, status, collection_id, is_favorite, is_pinned, review_status, review_reasons, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?, ?)`,
		Args: []any{id, title, content, in.Transcript.sql(nil), in.ItemType.orDefault("note"), in.Status.orDefault("active"),
			in.CollectionID.sql(nil), reviewStatus, reviewReasonsSQL(reasons), now, now},
	}}
	tagStmts, resolved, err := l.replaceTagsStmts(ctx, id, tagNames, now, map[string][2]string{})
	if err != nil {
		return "", nil, err
	}
	ftsStmts, err := l.writeFtsStmts(ctx, id, title, content, resolved)
	if err != nil {
		return "", nil, err
	}
	return id, append(append(stmts, tagStmts...), ftsStmts...), nil
}
func (l *Library) knowledgeUpdate(ctx context.Context, id string, in UpdateInput) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	rows, err := l.db.Query(ctx, `SELECT `+itemColumns+` FROM knowledge_items i WHERE i.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	e := rows.Values[0]
	existingReasons := parseReviewReasons(e[11])
	reasons := existingReasons
	if in.ReviewReasons.Set {
		reasons = normalizeReviewReasons(in.ReviewReasons.Val)
	}
	str := func(v any) string { s, _ := v.(string); return s }
	title := in.Title.orDefault(str(e[1]))
	content := in.Content.orDefault(str(e[2]))
	reviewStatus := str(e[10])
	if in.ReviewStatus.Set && !in.ReviewStatus.Null {
		reviewStatus = in.ReviewStatus.Val
	} else if in.ReviewReasons.Set && len(reasons) > 0 {
		reviewStatus = "needs_review"
	}
	isFav, isPin := asInt(e[8]) == 1, asInt(e[9]) == 1
	if in.IsFavorite.Set {
		isFav = in.IsFavorite.Val
	}
	if in.IsPinned.Set {
		isPin = in.IsPinned.Val
	}
	now := nowMs()
	stmts := []sqlite.Statement{{
		SQL: `UPDATE knowledge_items SET
           title = ?, content = ?, summary = ?, transcript = ?,
           item_type = ?, status = ?, collection_id = ?,
           is_favorite = ?, is_pinned = ?, review_status = ?, review_reasons = ?, updated_at = ?
         WHERE id = ?`,
		Args: []any{title, content, in.Summary.sql(e[3]), in.Transcript.sql(e[4]),
			in.ItemType.orDefault(str(e[5])), in.Status.orDefault(str(e[6])), in.CollectionID.sql(e[7]),
			b2i(isFav), b2i(isPin), reviewStatus, reviewReasonsSQL(reasons), now, id},
	}}
	var tagNames []string
	if in.TagNames.Set {
		tagStmts, resolved, err := l.replaceTagsStmts(ctx, id, normalizeTagNames(in.TagNames.Val), now, map[string][2]string{})
		if err != nil {
			return nil, err
		}
		stmts, tagNames = append(stmts, tagStmts...), resolved
	} else if tagNames, err = l.currentTagNames(ctx, id); err != nil {
		return nil, err
	}
	ftsStmts, err := l.writeFtsStmts(ctx, id, title, content, tagNames)
	if err != nil {
		return nil, err
	}
	stmts = append(stmts, ftsStmts...)
	// 用户或模型明确写入摘要后，网页更新留下的旧摘要提示已经处理（该表不一定存在）
	if in.Summary.Set {
		t, err := l.db.Query(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='web_source_baselines'")
		if err != nil {
			return nil, err
		}
		if len(t.Values) > 0 {
			stmts = append(stmts, sqlite.Statement{SQL: "UPDATE web_source_baselines SET summary_stale=0 WHERE item_id=?", Args: []any{id}})
		}
	}
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return l.knowledgeGet(ctx, id)
}

func uniqueStrings(ss []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// knowledgeBulkUpdate 对应 bulkUpdate：自己加锁并开事务；批处理要把它与回执写进同一个事务时用 bulkUpdateStmts。
func (l *Library) knowledgeBulkUpdate(ctx context.Context, ids []string, in BulkUpdateInput) (any, error) {
	if len(ids) == 0 {
		return int64(0), nil
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	stmts, updateCount, err := l.bulkUpdateStmts(ctx, ids, in)
	if err != nil {
		return nil, err
	}
	results, err := l.db.Transaction(ctx, stmts)
	if err != nil {
		return nil, err
	}
	var changed int64
	for i := 0; i < updateCount; i++ {
		changed += results[i].Changes
	}
	return changed, nil
}

// bulkUpdateStmts 生成 bulkUpdate 的全部语句；前 updateCount 条是 UPDATE，其 Changes 之和即返回值。调用方须持有 writeMu。
func (l *Library) bulkUpdateStmts(ctx context.Context, ids []string, in BulkUpdateInput) (stmts []sqlite.Statement, updateCount int, err error) {
	addNames := normalizeTagNames(in.AddTagNames.Val)
	removeNames := map[string]bool{}
	for _, n := range normalizeTagNames(in.RemoveTagNames.Val) {
		removeNames[strings.ToLower(n)] = true
	}
	touchesTags := len(addNames) > 0 || len(removeNames) > 0
	target := uniqueStrings(ids)
	now := nowMs()

	assignments := []string{"updated_at = ?"}
	assignArgs := []any{now}
	if in.CollectionID.Set {
		assignments = append(assignments, "collection_id = ?")
		assignArgs = append(assignArgs, in.CollectionID.sql(nil))
	}
	if in.Status.Set {
		assignments = append(assignments, "status = ?")
		assignArgs = append(assignArgs, in.Status.sql(nil))
	}
	if in.IsFavorite.Set {
		assignments = append(assignments, "is_favorite = ?")
		assignArgs = append(assignArgs, b2i(in.IsFavorite.Val))
	}
	if in.IsPinned.Set {
		assignments = append(assignments, "is_pinned = ?")
		assignArgs = append(assignArgs, b2i(in.IsPinned.Val))
	}
	if in.ReviewStatus.Set {
		assignments = append(assignments, "review_status = ?")
		assignArgs = append(assignArgs, in.ReviewStatus.sql(nil))
	}
	if in.ReviewReasons.Set {
		assignments = append(assignments, "review_reasons = ?")
		assignArgs = append(assignArgs, reviewReasonsSQL(in.ReviewReasons.Val))
	}

	pending := map[string][2]string{}
	for start := 0; start < len(target); start += bulkWriteChunkSize {
		end := min(start+bulkWriteChunkSize, len(target))
		batch := target[start:end]
		stmts = append(stmts, sqlite.Statement{
			SQL:  "UPDATE knowledge_items SET " + strings.Join(assignments, ", ") + " WHERE id IN (" + placeholders(len(batch), ", ") + ")",
			Args: append(append([]any{}, assignArgs...), anySlice(batch)...),
		})
		updateCount++
	}
	if touchesTags {
		for _, id := range target {
			rows, err := l.db.Query(ctx, "SELECT id, title, content FROM knowledge_items WHERE id = ?", id)
			if err != nil {
				return nil, 0, err
			}
			if len(rows.Values) == 0 {
				continue
			}
			title, _ := rows.Values[0][1].(string)
			content, _ := rows.Values[0][2].(string)
			current, err := l.currentTagNames(ctx, id)
			if err != nil {
				return nil, 0, err
			}
			var kept []string
			for _, n := range current {
				if !removeNames[strings.ToLower(n)] {
					kept = append(kept, n)
				}
			}
			tagStmts, resolved, err := l.replaceTagsStmts(ctx, id, normalizeTagNames(append(kept, addNames...)), now, pending)
			if err != nil {
				return nil, 0, err
			}
			ftsStmts, err := l.writeFtsStmts(ctx, id, title, content, resolved)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(append(stmts, tagStmts...), ftsStmts...)
		}
	}
	return stmts, updateCount, nil
}

// perIDStmts 逐条 UPDATE 的语句；批处理把它们与回执放进同一个事务。
func perIDStmts(ids []string, sql string, args func(id string) []any) []sqlite.Statement {
	stmts := make([]sqlite.Statement, len(ids))
	for i, id := range ids {
		stmts[i] = sqlite.Statement{SQL: sql, Args: args(id)}
	}
	return stmts
}

// perIDUpdate 对应 setStatus / moveToTrash / restore：逐条 UPDATE，汇总实际改动数。
func (l *Library) perIDUpdate(ctx context.Context, ids []string, sql string, args func(id string) []any) (any, error) {
	if len(ids) == 0 {
		return int64(0), nil
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	results, err := l.db.Transaction(ctx, perIDStmts(ids, sql, args))
	if err != nil {
		return nil, err
	}
	var changed int64
	for _, r := range results {
		changed += r.Changes
	}
	return changed, nil
}

func (l *Library) setStatus(ctx context.Context, ids []string, status string) (any, error) {
	now := nowMs()
	return l.perIDUpdate(ctx, ids, "UPDATE knowledge_items SET status = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
		func(id string) []any { return []any{status, now, id} })
}

func (l *Library) moveToTrash(ctx context.Context, ids []string) (any, error) {
	now := nowMs()
	return l.perIDUpdate(ctx, ids, "UPDATE knowledge_items SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
		func(id string) []any { return []any{now, now, id} })
}

func (l *Library) restore(ctx context.Context, ids []string) (any, error) {
	now := nowMs()
	return l.perIDUpdate(ctx, ids, "UPDATE knowledge_items SET deleted_at = NULL, updated_at = ? WHERE id = ? AND deleted_at IS NOT NULL",
		func(id string) []any { return []any{now, id} })
}

// deleteForeverStmts 生成彻底删除的语句与各条 DELETE 的下标（其 Changes 之和即返回值）；调用方须持有 writeMu。
func (l *Library) deleteForeverStmts(ctx context.Context, ids []string) (stmts []sqlite.Statement, deletes []int, err error) {
	for _, id := range ids {
		fts, err := l.deleteFtsStmts(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		stmts = append(stmts, fts...)
		deletes = append(deletes, len(stmts))
		stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM knowledge_items WHERE id = ?", Args: []any{id}})
	}
	return stmts, deletes, nil
}

func normalizeIDs(raw json.RawMessage) []string {
	var arr []any
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	out := []string{}
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func argAt(args []json.RawMessage, i int) json.RawMessage {
	if i < len(args) {
		return args[i]
	}
	return json.RawMessage("null")
}

func (l *Library) registerWrite(b *Bridge) {
	decode := func(raw json.RawMessage, v any) error {
		if string(raw) == "null" || len(raw) == 0 {
			return nil // 对应 `input ?? {}`
		}
		return json.Unmarshal(raw, v)
	}
	b.handlers["knowledge:get"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var id string
		_ = json.Unmarshal(argAt(a, 0), &id)
		return l.knowledgeGet(ctx, id)
	}
	b.handlers["knowledge:create"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var in CreateInput
		if err := decode(argAt(a, 0), &in); err != nil {
			return nil, err
		}
		return l.knowledgeCreate(ctx, in)
	}
	b.handlers["knowledge:update"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var id string
		_ = json.Unmarshal(argAt(a, 0), &id)
		var in UpdateInput
		if err := decode(argAt(a, 1), &in); err != nil {
			return nil, err
		}
		return l.knowledgeUpdate(ctx, id, in)
	}
	b.handlers["knowledge:bulkUpdate"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var in BulkUpdateInput
		if err := decode(argAt(a, 1), &in); err != nil {
			return nil, err
		}
		return l.knowledgeBulkUpdate(ctx, normalizeIDs(argAt(a, 0)), in)
	}
	b.handlers["knowledge:setStatus"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var status string
		_ = json.Unmarshal(argAt(a, 1), &status)
		return l.setStatus(ctx, normalizeIDs(argAt(a, 0)), status)
	}
	// moveToTrash 在 TS 里还会取消这些条目的主题阅读任务（内存态）；主题阅读迁移前没有可取消的任务，挂钩留空。
	b.handlers["knowledge:moveToTrash"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		ids := normalizeIDs(argAt(a, 0))
		if l.cancelThemedReading != nil {
			l.cancelThemedReading(ids)
		}
		return l.moveToTrash(ctx, ids)
	}
	b.handlers["knowledge:restore"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.restore(ctx, normalizeIDs(argAt(a, 0)))
	}
}
