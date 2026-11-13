package main

// 对应 packages/db/src/import-task.ts 的写入侧与 import-task-query.ts 的 terminalFilter。
// 读取侧的 listPage 在 import_list.go。

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

// nstr 对应 patch 里「未传 / 传 null / 传字符串」三态的字段。
type nstr struct {
	set, null bool
	v         string
}

func nset(v string) nstr { return nstr{set: true, v: v} }
func nnull() nstr        { return nstr{set: true, null: true} }

func (n nstr) arg(existing any) any {
	if !n.set {
		return existing
	}
	if n.null {
		return nil
	}
	return n.v
}

// taskPatch 对应 ImportTaskDB.update 的 patch；指针为 nil 表示未传。
type taskPatch struct {
	status          *string
	stage           nstr
	err             nstr
	warning         nstr
	displayName     *string
	itemType        nstr
	resultItemID    nstr
	duplicateItemID nstr
	forceDuplicate  *bool
	stageStats      nstr // v 为已序列化的 JSON；null 表示清空
	captureStrategy *string
	commentLimit    *int64
}

func sp(s string) *string { return &s }

// clearableStatuses 是「清理已完成」的范围；有意排除 failed（它是重试入口）。
var clearableStatuses = []string{"completed", "canceled", "duplicate"}

func makeDisplayName(kind, input string) string {
	value := jsTrim(input)
	switch kind {
	case "url":
		return value
	case "file":
		last := value
		if i := strings.LastIndexAny(value, `\/`); i >= 0 {
			last = value[i+1:]
		}
		if last == "" {
			return value
		}
		return last
	}
	first := value
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = strings.TrimSuffix(first[:i], "\r")
	}
	if utf16Len(first) > 60 {
		return utf16Prefix(first, 60) + "…"
	}
	return first
}

// importTaskCreate 对应 ImportTaskDB.create；in 是 EnqueueImportInput（jsvalue），value 是已 trim 的输入。
func (l *Library) importTaskCreate(ctx context.Context, in any, value string) (map[string]any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	kind, _ := jsGet(in, "kind").(string)
	bind := func(key string) (any, error) {
		v := jsGet(in, key)
		if isNullish(v) {
			return nil, nil
		}
		return wikiBind(v)
	}
	collectionID, err := bind("collectionId")
	if err != nil {
		return nil, err
	}
	refreshID, err := bind("refreshOfItemId")
	if err != nil {
		return nil, err
	}
	var tagNames any
	switch t := jsGet(in, "tagNames").(type) {
	case jsArr:
		if len(t) > 0 {
			s, _ := jsStringify(t)
			tagNames = s
		}
	case string:
		if t != "" {
			tagNames = jsQuote(t)
		}
	}
	strategy := "standard"
	if v, ok := jsGet(in, "captureStrategy").(string); ok {
		strategy = v
	}
	var commentLimit int64
	if v, ok := jsGet(in, "commentLimit").(float64); ok {
		commentLimit = int64(v)
	}
	now, id := nowMs(), newUUID()
	_, err = l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: `INSERT INTO import_tasks
         (id, source_kind, source_input, display_name, status, collection_id, refresh_of_item_id, tag_names, capture_strategy, comment_limit, force_duplicate, created_at, updated_at)
       VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?)`,
		Args: []any{id, kind, value, nodeUTF8String(makeDisplayName(kind, value)), collectionID, refreshID, tagNames, strategy, commentLimit,
			b2i(vTruthy(jsGet(in, "forceDuplicate"))), now, now},
	}})
	if err != nil {
		return nil, err
	}
	task, err := l.importTaskGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("Failed to load created import task: %s", id)
	}
	return task, nil
}

// nodeUTF8String 把孤立代理项换成 U+FFFD（node-sqlite3-wasm 写入字符串时的行为）。
func nodeUTF8String(s string) string { return string(nodeUTF8(s)) }

func (l *Library) importTaskGet(ctx context.Context, id string) (map[string]any, error) {
	rows, err := l.db.Query(ctx, taskSelect+" WHERE id = ?", id)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	return mapImportTaskRow(rows.Columns, rows.Values[0]), nil
}

func (l *Library) importTaskIsForceDuplicate(ctx context.Context, id string) bool {
	rows, err := l.db.Query(ctx, "SELECT force_duplicate FROM import_tasks WHERE id = ?", id)
	return err == nil && len(rows.Values) > 0 && asInt(rows.Values[0][0]) == 1
}

func (l *Library) importTaskListByStatus(ctx context.Context, statuses ...string) ([]map[string]any, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = s
	}
	rows, err := l.db.Query(ctx, taskSelect+" WHERE status IN ("+strings.TrimSuffix(strings.Repeat("?, ", len(statuses)), ", ")+") ORDER BY created_at ASC", args...)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, mapImportTaskRow(rows.Columns, r))
	}
	return out, nil
}

// importTaskUpdate 对应 ImportTaskDB.update；任务不存在返回 nil。
func (l *Library) importTaskUpdate(ctx context.Context, id string, p taskPatch) (map[string]any, error) {
	l.writeMu.Lock()
	rows, err := l.db.Query(ctx, taskSelect+" WHERE id = ?", id)
	if err != nil || len(rows.Values) == 0 {
		l.writeMu.Unlock()
		return nil, err
	}
	col := func(name string) any { return rows.Values[0][slices.Index(rows.Columns, name)] }
	status := col("status")
	if p.status != nil {
		status = *p.status
	}
	// patch.warning !== undefined 时清除「已知悉」标记
	ack := col("warning_acknowledged_at")
	if p.warning.set {
		ack = nil
	}
	displayName := col("display_name")
	if p.displayName != nil && jsTrim(*p.displayName) != "" {
		displayName = jsTrim(*p.displayName)
	}
	forceDup := col("force_duplicate")
	if p.forceDuplicate != nil {
		forceDup = b2i(*p.forceDuplicate)
	}
	stageStats := col("stage_stats")
	if p.stageStats.set {
		stageStats = nil
		if !p.stageStats.null && p.stageStats.v != "" && p.stageStats.v != "[]" {
			stageStats = p.stageStats.v
		}
	}
	strategy := col("capture_strategy")
	if p.captureStrategy != nil {
		strategy = *p.captureStrategy
	}
	commentLimit := col("comment_limit")
	if p.commentLimit != nil {
		commentLimit = *p.commentLimit
	}
	_, err = l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: `UPDATE import_tasks SET
         status = ?, stage = ?, error = ?, warning = ?, warning_acknowledged_at = ?,
         display_name = ?, item_type = ?,
         result_item_id = ?, duplicate_item_id = ?, stage_stats = ?, capture_strategy = ?,
         comment_limit = ?, force_duplicate = ?,
         updated_at = ?
       WHERE id = ?`,
		Args: []any{status, p.stage.arg(col("stage")), p.err.arg(col("error")), p.warning.arg(col("warning")), ack,
			displayName, p.itemType.arg(col("item_type")),
			p.resultItemID.arg(col("result_item_id")), p.duplicateItemID.arg(col("duplicate_item_id")), stageStats, strategy,
			commentLimit, forceDup, nowMs(), id},
	}})
	l.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	return l.importTaskGet(ctx, id)
}

func (l *Library) importTaskResetProcessing(ctx context.Context) (int64, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	res, err := l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: "UPDATE import_tasks SET status = 'pending', stage = NULL, updated_at = ? WHERE status = 'processing'", Args: []any{nowMs()},
	}})
	if err != nil {
		return 0, err
	}
	return res[0].Changes, nil
}

func (l *Library) importTaskExec(ctx context.Context, sql string, args ...any) (int64, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	res, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: sql, Args: args}})
	if err != nil {
		return 0, err
	}
	return res[0].Changes, nil
}

func (l *Library) importTaskClearFinished(ctx context.Context) (int64, error) {
	args := make([]any, len(clearableStatuses))
	for i, s := range clearableStatuses {
		args[i] = s
	}
	return l.importTaskExec(ctx, "DELETE FROM import_tasks WHERE status IN ("+strings.TrimSuffix(strings.Repeat("?, ", len(args)), ", ")+")", args...)
}

func (l *Library) importTaskRemove(ctx context.Context, id any) (bool, error) {
	n, err := l.importTaskExec(ctx, "DELETE FROM import_tasks WHERE id = ? AND status NOT IN ('pending', 'processing')", id)
	return n > 0, err
}

// terminalFilter 对应 import-task-query.ts 的 terminalFilter；query 是 IPC 传来的原始值。
func terminalFilter(query any) (sqlPart, error) {
	scope, _ := jsGet(query, "scope").(string)
	if !vTruthy(query) || (scope != "filtered" && scope != "all") {
		return sqlPart{}, errors.New("清理范围不合法")
	}
	var search, status sqlPart
	if scope == "filtered" {
		// origin 只有 undefined 才跳过校验；null 会被判为不合法
		origin := "all"
		if v := jsGet(query, "origin"); v != jsUndefined {
			s, ok := v.(string)
			if !ok || !slices.Contains([]string{"all", "mobile", "desktop"}, s) {
				return sqlPart{}, errors.New("提交来源筛选不合法")
			}
			origin = s
		}
		keyword := ""
		switch q := jsGet(query, "query").(type) {
		case string:
			keyword = q
		case jsUndefinedT, jsNullT:
		default:
			return sqlPart{}, errors.New("query.trim is not a function")
		}
		var err error
		if search, err = taskSearch(origin, keyword); err != nil {
			return sqlPart{}, err
		}
		switch s := jsGet(query, "status").(type) {
		case string:
			status = taskStatusFilter(s)
		case jsUndefinedT, jsNullT:
		default:
			if vTruthy(s) {
				arg, err := wikiBind(s)
				if err != nil {
					return sqlPart{}, err
				}
				status = sqlPart{sql: " AND status = ?", params: []any{arg}}
			}
		}
	}
	return sqlPart{
		sql:    "status IN ('completed','failed','canceled','duplicate')" + search.sql + status.sql,
		params: append(append([]any{}, search.params...), status.params...),
	}, nil
}

func (l *Library) importTaskCountTerminal(ctx context.Context, query any) (int64, error) {
	f, err := terminalFilter(query)
	if err != nil {
		return 0, err
	}
	rows, err := l.db.Query(ctx, "SELECT COUNT(*) AS count FROM import_tasks WHERE "+f.sql, f.params...)
	if err != nil {
		return 0, err
	}
	return asInt(rows.Values[0][0]), nil
}

func (l *Library) importTaskClearTerminal(ctx context.Context, query any) (int64, error) {
	f, err := terminalFilter(query)
	if err != nil {
		return 0, err
	}
	return l.importTaskExec(ctx, "DELETE FROM import_tasks WHERE "+f.sql, f.params...)
}
