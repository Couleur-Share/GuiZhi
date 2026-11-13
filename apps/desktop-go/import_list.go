package main

// 对应 packages/db/src/import-task.ts 的 listPage 与 import-task-query.ts（只读）。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

const mobileTask = "EXISTS (SELECT 1 FROM mobile_capture_tasks m WHERE m.task_id = import_tasks.id)"

// 复用投递关联识别历史手机任务，不返回中继地址、投递凭证等内部信息。
var taskSelect = `SELECT import_tasks.*,
  CASE WHEN ` + mobileTask + ` THEN 'mobile' ELSE 'desktop' END AS submitted_from,
  (SELECT r.created_at FROM mobile_capture_tasks m JOIN mobile_capture_receipts r
   ON r.relay_key = m.relay_key AND r.delivery_id = m.delivery_id
   WHERE m.task_id = import_tasks.id) AS received_at FROM import_tasks`

var importTaskStatuses = []string{"pending", "processing", "completed", "failed", "canceled", "duplicate"}

type importListQuery struct {
	Origin   *string  `json:"origin"`
	Status   *string  `json:"status"`
	Query    *string  `json:"query"`
	PageSize *float64 `json:"pageSize"`
	Cursor   *string  `json:"cursor"`
}

type sqlPart struct {
	sql    string
	params []any
}

func taskSearch(origin string, query string) (sqlPart, error) {
	if !slices.Contains([]string{"all", "mobile", "desktop"}, origin) {
		return sqlPart{}, errors.New("提交来源筛选不合法")
	}
	keyword := jsLower(strings.TrimFunc(query, jsSpace))
	source := ""
	switch origin {
	case "mobile":
		source = " AND " + mobileTask
	case "desktop":
		source = " AND NOT " + mobileTask
	}
	if keyword == "" {
		return sqlPart{sql: source}, nil
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(keyword)
	pattern := "%" + escaped + "%"
	return sqlPart{
		sql: source + ` AND (LOWER(display_name) LIKE ? ESCAPE '\'
 OR LOWER(source_input) LIKE ? ESCAPE '\'
 OR LOWER(COALESCE(error, '')) LIKE ? ESCAPE '\' OR LOWER(COALESCE(warning, '')) LIKE ? ESCAPE '\')`,
		params: []any{pattern, pattern, pattern, pattern},
	}, nil
}

func taskStatusFilter(status string) sqlPart {
	switch status {
	case "all", "":
		return sqlPart{}
	case "active":
		return sqlPart{sql: " AND status IN ('pending','processing')"}
	case "degraded":
		return sqlPart{sql: " AND status = 'completed' AND COALESCE(warning, '') <> ''"}
	}
	return sqlPart{sql: " AND status = ?", params: []any{status}}
}

type importCursor struct {
	CreatedAt int64
	ID        string
}

func encodeImportCursor(c importCursor) string {
	raw := fmt.Sprintf(`{"createdAt":%d,"id":%s}`, c.CreatedAt, jsonString(c.ID))
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeImportCursor(v *string) *importCursor {
	if v == nil || *v == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*v)
	if err != nil {
		return nil
	}
	var p struct {
		CreatedAt any `json:"createdAt"`
		ID        any `json:"id"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	createdAt, ok1 := p.CreatedAt.(float64)
	id, ok2 := p.ID.(string)
	if !ok1 || !ok2 || math.IsInf(createdAt, 0) || math.IsNaN(createdAt) {
		return nil
	}
	return &importCursor{CreatedAt: int64(createdAt), ID: id}
}

func parseImportTagNames(raw any) []string {
	s, _ := raw.(string)
	out := []string{}
	if s == "" {
		return out
	}
	var arr []any
	if json.Unmarshal([]byte(s), &arr) != nil {
		return out
	}
	for _, x := range arr {
		if name, ok := x.(string); ok {
			out = append(out, name)
		}
	}
	return out
}

// parseStageStats：只保留 stage 为字符串、ms 为数字的对象；没有有效项或解析失败一律当「没有统计」。
func parseStageStats(raw any) any {
	s, _ := raw.(string)
	if s == "" {
		return nil
	}
	var arr []any
	if json.Unmarshal([]byte(s), &arr) != nil {
		return nil
	}
	stats := []any{}
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		_, stageOK := m["stage"].(string)
		_, msOK := m["ms"].(float64)
		if stageOK && msOK {
			stats = append(stats, m)
		}
	}
	if len(stats) == 0 {
		return nil
	}
	return stats
}

func orDefault(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}

// importTaskColumns 与 TASK_SELECT 的列顺序无关：按列名取值。
func mapImportTaskRow(cols []string, row []any) map[string]any {
	get := map[string]any{}
	for i, c := range cols {
		get[c] = row[i]
	}
	return map[string]any{
		"id": get["id"], "origin": orDefault(get["submitted_from"], "desktop"), "receivedAt": orDefault(get["received_at"], nil),
		"sourceKind": get["source_kind"], "sourceInput": get["source_input"], "displayName": get["display_name"],
		"status": get["status"], "stage": get["stage"], "error": get["error"], "warning": get["warning"],
		"itemType": get["item_type"], "resultItemId": get["result_item_id"], "duplicateItemId": get["duplicate_item_id"],
		"collectionId": get["collection_id"], "refreshOfItemId": get["refresh_of_item_id"],
		"tagNames": parseImportTagNames(get["tag_names"]), "stageStats": parseStageStats(get["stage_stats"]),
		"captureStrategy": orDefault(get["capture_strategy"], "standard"), "commentLimit": orDefault(get["comment_limit"], int64(0)),
		"createdAt": get["created_at"], "updatedAt": get["updated_at"],
	}
}

func (l *Library) importListPage(ctx context.Context, q importListQuery) (map[string]any, error) {
	// JS: Math.min(Math.max(Math.round(pageSize ?? 50), 20), 100)；Math.round 对 .5 向上取整
	pageSizeF := 50.0
	if q.PageSize != nil {
		pageSizeF = math.Floor(*q.PageSize + 0.5)
	}
	pageSize := int(math.Min(math.Max(pageSizeF, 20), 100))
	origin, status, query := "all", "all", ""
	if q.Origin != nil {
		origin = *q.Origin
	}
	if q.Status != nil {
		status = *q.Status
	}
	if q.Query != nil {
		query = *q.Query
	}
	cursor := decodeImportCursor(q.Cursor)

	search, err := taskSearch(origin, query)
	if err != nil {
		return nil, err
	}
	statusSQL := taskStatusFilter(status)
	cursorSQL := ""
	var cursorParams []any
	if cursor != nil {
		cursorSQL = " AND (created_at < ? OR (created_at = ? AND id < ?))"
		cursorParams = []any{cursor.CreatedAt, cursor.CreatedAt, cursor.ID}
	}
	args := append(append(append(append([]any{}, search.params...), statusSQL.params...), cursorParams...), pageSize+1)
	rows, err := l.db.Query(ctx, taskSelect+" WHERE 1 = 1"+search.sql+statusSQL.sql+cursorSQL+
		" ORDER BY created_at DESC, id DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows.Values) > pageSize
	pageRows := rows.Values
	if hasMore {
		pageRows = rows.Values[:pageSize]
	}
	idx := func(name string) int { return slices.Index(rows.Columns, name) }
	entries := make([]map[string]any, 0, len(pageRows))
	for _, r := range pageRows {
		entries = append(entries, mapImportTaskRow(rows.Columns, r))
	}
	var nextCursor any
	if hasMore && len(pageRows) > 0 {
		tail := pageRows[len(pageRows)-1]
		nextCursor = encodeImportCursor(importCursor{CreatedAt: asInt(tail[idx("created_at")]), ID: tail[idx("id")].(string)})
	}

	activeRows, err := l.db.Query(ctx, taskSelect+" WHERE status IN ('pending','processing')"+search.sql+
		" ORDER BY created_at ASC, id ASC", search.params...)
	if err != nil {
		return nil, err
	}
	active := make([]map[string]any, 0, len(activeRows.Values))
	for _, r := range activeRows.Values {
		active = append(active, mapImportTaskRow(activeRows.Columns, r))
	}

	totalRows, err := l.db.Query(ctx, "SELECT COUNT(*) AS count FROM import_tasks WHERE 1 = 1"+search.sql+statusSQL.sql,
		append(append([]any{}, search.params...), statusSQL.params...)...)
	if err != nil {
		return nil, err
	}
	countRows, err := l.db.Query(ctx, "SELECT status, COUNT(*) AS count FROM import_tasks WHERE 1 = 1"+search.sql+" GROUP BY status",
		search.params...)
	if err != nil {
		return nil, err
	}
	counts := map[string]any{}
	for _, s := range importTaskStatuses {
		counts[s] = int64(0)
	}
	for _, r := range countRows.Values {
		counts[r[0].(string)] = asInt(r[1])
	}
	degraded, err := l.db.Query(ctx, `SELECT COUNT(*) AS count FROM import_tasks WHERE status = 'completed'
         AND COALESCE(warning, '') <> ''`+search.sql, search.params...)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"entries": entries, "active": active, "nextCursor": nextCursor,
		"total": asInt(totalRows.Values[0][0]), "counts": counts, "degradedCount": asInt(degraded.Values[0][0]),
	}, nil
}

func (l *Library) registerImportList(b *Bridge) {
	b.handlers["import:list"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := argAt(a, 0)
		// IPC：传对象走分页；没传（或传 null/非对象）只返回第一页的 entries 数组
		if len(raw) == 0 || raw[0] != '{' {
			res, err := l.importListPage(ctx, importListQuery{})
			if err != nil {
				return nil, err
			}
			return res["entries"], nil
		}
		var q importListQuery
		if err := json.Unmarshal(raw, &q); err != nil {
			return nil, err
		}
		return l.importListPage(ctx, q)
	}
}
