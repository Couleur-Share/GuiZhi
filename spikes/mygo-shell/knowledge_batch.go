package main

// 对应 knowledge.ipc.ts 的 knowledge:selection、services/knowledge-batch.ts、
// packages/db 的 knowledge-batch-log / source-revisions / KnowledgeItemDB.freezeIds。
//
// 批量执行逐条独立记账：每条一个事务，改动、（可选）清问答证据、彻底删除与回执同提交；
// 磁盘资产清理放在提交之后，清理失败不能把已提交的条目反报为失败。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

// jsMember 对应 `v.key`：v 为 null/undefined 时抛 TypeError，消息与 V8 一致。
func jsMember(v any, key string) (any, error) {
	if isNullish(v) {
		return nil, fmt.Errorf("Cannot read properties of %s (reading '%s')", jsToString(v), key)
	}
	return jsGet(v, key), nil
}

func jsonOf(v any) ([]byte, error) {
	s, ok := jsStringify(v)
	if !ok {
		return []byte("null"), nil
	}
	return []byte(s), nil
}

// listSourceRevisions 对应 listSourceRevisions：某条目最近 20 份采集修订。
func (l *Library) listSourceRevisions(ctx context.Context, id any) ([]any, error) {
	arg, err := bindArg(id)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.Query(ctx, "SELECT id,title,content,transcript,review_json AS reviewJson,captured_at AS capturedAt FROM source_capture_revisions WHERE item_id=? ORDER BY captured_at DESC,id DESC LIMIT 20", arg)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, map[string]any{
			"id": r[0], "title": r[1], "content": r[2], "transcript": r[3], "reviewJson": r[4], "capturedAt": r[5],
		})
	}
	return out, nil
}

// freezeIds 对应 KnowledgeItemDB.freezeIds：按当前筛选/搜索/排除条件冻结 id 列表（不含 limit，含回收站范围）。
func (l *Library) freezeIds(ctx context.Context, query any) ([]string, error) {
	if _, err := jsMember(query, "scope"); err != nil {
		return nil, err
	}
	raw, err := jsonOf(query)
	if err != nil {
		return nil, err
	}
	var q ListQuery
	if err := json.Unmarshal(raw, &q); err != nil {
		return nil, err
	}
	var facet FacetCountsQuery
	if err := json.Unmarshal(raw, &facet); err != nil {
		return nil, err
	}
	sql := buildFacetCountSql(facet, "")
	var cond []string
	var params []any
	if len(q.ExcludedItemIDs) > 0 {
		cond = append(cond, "i.id NOT IN ("+placeholders(len(q.ExcludedItemIDs), ",")+")")
		params = append(params, anySlice(q.ExcludedItemIDs)...)
	}
	if q.CollectionScope != nil {
		var parts []string
		if len(q.CollectionScope.IDs) > 0 {
			parts = append(parts, "i.collection_id IN ("+placeholders(len(q.CollectionScope.IDs), ",")+")")
		}
		params = append(params, anySlice(q.CollectionScope.IDs)...)
		if q.CollectionScope.IncludeUncategorized {
			parts = append(parts, "i.collection_id IS NULL")
		}
		if len(parts) > 0 {
			cond = append(cond, "("+strings.Join(parts, " OR ")+")")
		} else {
			cond = append(cond, "0")
		}
	}
	var order string
	if sql.join != "" {
		order = "ORDER BY f.fts_rank ASC,i.updated_at DESC,i.id DESC"
	} else {
		column, ok := sortColumns[deref(q.SortBy, "updatedAt")]
		if !ok {
			column = sortColumns["updatedAt"]
		}
		direction := "DESC"
		if deref(q.SortOrder, "") == "asc" {
			direction = "ASC"
		}
		order = fmt.Sprintf("ORDER BY i.is_pinned DESC, %s %s, i.id %s", column, direction, direction)
	}
	where := sql.where
	if len(cond) > 0 {
		where += " AND " + strings.Join(cond, " AND ")
	}
	rows, err := l.db.Query(ctx, "SELECT i.id FROM knowledge_items i "+sql.join+" "+where+" "+order, append(append([]any{}, sql.params...), params...)...)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows.Values))
	for _, r := range rows.Values {
		ids = append(ids, r[0].(string))
	}
	return ids, nil
}

// freezeKnowledgeSelection 对应同名函数：保存冻结的 id 集合，顺带清掉超过一天的旧集合。
func (l *Library) freezeKnowledgeSelection(ctx context.Context, ids []string) (string, error) {
	id := newUUID()
	idsJSON, _ := json.Marshal(ids)
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.db.Transaction(ctx, []sqlite.Statement{
		{SQL: "DELETE FROM knowledge_selection_sets WHERE created_at<?", Args: []any{nowMs() - 86400000}},
		{SQL: "INSERT INTO knowledge_selection_sets VALUES(?,?,?)", Args: []any{id, string(idsJSON), nowMs()}},
	})
	return id, err
}

// readKnowledgeBatch 对应同名函数。
func (l *Library) readKnowledgeBatch(ctx context.Context, runID any) (map[string]any, error) {
	arg, err := bindArg(runID)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.Query(ctx, "SELECT command_json,result_json FROM knowledge_batch_results WHERE run_id=? ORDER BY updated_at,item_id", arg)
	if err != nil {
		return nil, err
	}
	results := make([]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		v, err := jsParse(r[1].(string))
		if err != nil {
			return nil, err
		}
		results = append(results, jsToPlain(v))
	}
	out := map[string]any{"results": results}
	if len(rows.Values) > 0 {
		v, err := jsParse(rows.Values[0][0].(string))
		if err != nil {
			return nil, err
		}
		out["command"] = jsToPlain(v)
	}
	return out, nil
}

// batchIDs 对应 `[...new Set(ids)]`：null/undefined 得到空集，字符串按码点拆开，数组按元素去重。
func batchIDs(ids any) ([]any, error) {
	var items []any
	switch v := ids.(type) {
	case jsUndefinedT, jsNullT:
	case string:
		for _, r := range v {
			items = append(items, string(r))
		}
	case jsArr:
		items = v
	case float64, bool:
		return nil, fmt.Errorf("%s %s is not iterable (cannot read property Symbol(Symbol.iterator))", jsTypeName(v), jsToString(v))
	default:
		return nil, errors.New("object is not iterable (cannot read property Symbol(Symbol.iterator))")
	}
	seen := map[string]bool{}
	var out []any
	for _, it := range items {
		key := fmt.Sprintf("%T:%v", it, it)
		if !seen[key] {
			seen[key] = true
			out = append(out, it)
		}
	}
	return out, nil
}

func jsTypeName(v any) string {
	switch v.(type) {
	case float64:
		return "number"
	case bool:
		return "boolean"
	}
	return "object"
}

var errBatchAlreadyDone = errors.New("batch item already committed")

// executeBatchItem 在一个事务内完成一条的改动与回执；返回提交后要清理的资产候选。
func (l *Library) executeBatchItem(ctx context.Context, id any, command any, commandJSON string, runID any, preserveReceipt *bool) ([]string, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	idArg, err := bindArg(id)
	if err != nil {
		return nil, err
	}
	if vTruthy(runID) {
		rid, err := bindArg(runID)
		if err != nil {
			return nil, err
		}
		prev, err := l.db.Query(ctx, "SELECT command_json,result_json FROM knowledge_batch_results WHERE run_id=? AND item_id=?", rid, idArg)
		if err != nil {
			return nil, err
		}
		if len(prev.Values) > 0 {
			if prev.Values[0][0].(string) != commandJSON {
				*preserveReceipt = true
				return nil, errors.New("同一批次不能更换操作，请重新发起")
			}
			// IPC 回执丢失时重试同一批次，复用已提交结果，尤其不能重做彻底删除。
			parsed, err := jsParse(prev.Values[0][1].(string))
			if err != nil {
				return nil, err
			}
			if vTruthy(jsGet(parsed, "ok")) {
				return nil, errBatchAlreadyDone
			}
		}
	}
	itemID, _ := id.(string)
	row, err := l.db.Query(ctx, "SELECT deleted_at FROM knowledge_items WHERE id = ?", idArg)
	if err != nil {
		return nil, err
	}
	if len(row.Values) == 0 {
		return nil, errors.New("条目已删除")
	}
	deleted := row.Values[0][0] != nil
	kind := jsGet(command, "kind")
	if kind != "restore" && kind != "delete" && deleted {
		return nil, errors.New("条目已在回收站")
	}
	ids := []string{itemID}
	var stmts []sqlite.Statement
	var assets []string
	now := nowMs()
	switch kind {
	case "update":
		patch := jsGet(command, "patch")
		if isNullish(patch) {
			return nil, fmt.Errorf("Cannot read properties of %s (reading 'addTagNames')", jsToString(patch))
		}
		raw, _ := jsonOf(patch)
		var in BulkUpdateInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if stmts, _, err = l.bulkUpdateStmts(ctx, ids, in); err != nil {
			return nil, err
		}
	case "status":
		status, err := bindArg(jsGet(command, "status"))
		if err != nil {
			return nil, err
		}
		stmts = perIDStmts(ids, "UPDATE knowledge_items SET status = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
			func(id string) []any { return []any{status, now, id} })
	case "trash":
		stmts = perIDStmts(ids, "UPDATE knowledge_items SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
			func(id string) []any { return []any{now, now, id} })
	case "restore":
		stmts = perIDStmts(ids, "UPDATE knowledge_items SET deleted_at = NULL, updated_at = ? WHERE id = ? AND deleted_at IS NOT NULL",
			func(id string) []any { return []any{now, id} })
	case "delete":
		if !deleted {
			return nil, errors.New("只能彻底删除回收站中的条目")
		}
		if assets, err = l.listAssetRefs(ctx, ids); err != nil {
			return nil, err
		}
		if vTruthy(jsGet(command, "clearEvidence")) {
			clear, err := l.clearAskEvidenceStmts(ctx, ids)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, clear...)
		}
		del, _, err := l.deleteForeverStmts(ctx, ids)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, del...)
	default:
		return nil, errors.New("批量操作类型无效")
	}
	// 回执写失败时回滚本条修改，不能让界面把已执行条目当成失败重试。
	if vTruthy(runID) {
		receipt, err := receiptStmt(runID, id, commandJSON, batchResult(id, nil))
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, receipt)
	}
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return assets, nil
}

func batchResult(id any, err error) *jsObj {
	o := newObj()
	o.set("id", id)
	o.set("ok", err == nil)
	if err != nil {
		o.set("error", err.Error())
	}
	return o
}

func receiptStmt(runID, id any, commandJSON string, result *jsObj) (sqlite.Statement, error) {
	rid, err := bindArg(runID)
	if err != nil {
		return sqlite.Statement{}, err
	}
	iid, err := bindArg(id)
	if err != nil {
		return sqlite.Statement{}, err
	}
	resultJSON, _ := jsStringify(result)
	return sqlite.Statement{SQL: "INSERT OR REPLACE INTO knowledge_batch_results VALUES(?,?,?,?,?)", Args: []any{rid, iid, commandJSON, resultJSON, nowMs()}}, nil
}

// executeKnowledgeBatch 对应同名函数：只对调用瞬间冻结的 ID 逐条操作，每条独立记账。
func (l *Library) executeKnowledgeBatch(ctx context.Context, ids any, command any, runID any) ([]any, error) {
	targets, err := batchIDs(ids)
	if err != nil {
		return nil, err
	}
	commandJSON, _ := jsStringify(command)
	results := make([]any, 0, len(targets))
	for _, id := range targets {
		preserve := false
		assets, err := l.executeBatchItem(ctx, id, command, commandJSON, runID, &preserve)
		if errors.Is(err, errBatchAlreadyDone) {
			err = nil
		}
		result := batchResult(id, err)
		results = append(results, jsToPlain(result))
		if err != nil {
			assets = nil
			if vTruthy(runID) && !preserve {
				l.recordFailureReceipt(ctx, runID, id, commandJSON, result)
			}
		}
		// 磁盘清理放在提交之后；清理失败不能把已经提交的条目反报为失败。
		if len(assets) > 0 {
			if _, err := l.cleanupOrphanAssets(ctx, assets); err != nil {
				logAppErrorf("knowledge.batch", "清理已删除条目资产", "%s", err.Error())
			}
		}
	}
	return results, nil
}

func (l *Library) recordFailureReceipt(ctx context.Context, runID, id any, commandJSON string, result *jsObj) {
	stmt, err := receiptStmt(runID, id, commandJSON, result)
	if err == nil {
		l.writeMu.Lock()
		_, err = l.db.Transaction(ctx, []sqlite.Statement{stmt})
		l.writeMu.Unlock()
	}
	if err != nil {
		logAppErrorf("knowledge.batch", "保存失败回执", "%s", err.Error())
	}
}

func (l *Library) knowledgeSelection(ctx context.Context, input any) (any, error) {
	action, err := jsMember(input, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "source-versions":
		versions, err := l.listSourceRevisions(ctx, jsGet(input, "id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "versions": versions}, nil
	case "freeze":
		ids, err := l.freezeIds(ctx, jsGet(input, "query"))
		if err != nil {
			return nil, err
		}
		selectionID, err := l.freezeKnowledgeSelection(ctx, ids)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "ids": ids, "selectionId": selectionID}, nil
	case "results":
		out, err := l.readKnowledgeBatch(ctx, jsGet(input, "runId"))
		if err != nil {
			return nil, err
		}
		out["ok"] = true
		return out, nil
	case "execute":
		command := jsGet(input, "command")
		kind, err := jsMember(command, "kind")
		if err != nil {
			return nil, err
		}
		ids := jsGet(input, "ids")
		if kind == "trash" || kind == "delete" {
			if l.cancelThemedReading != nil {
				if arr, ok := ids.(jsArr); ok {
					var s []string
					for _, v := range arr {
						if str, ok := v.(string); ok {
							s = append(s, str)
						}
					}
					l.cancelThemedReading(s)
				}
			}
		}
		results, err := l.executeKnowledgeBatch(ctx, ids, command, jsGet(input, "runId"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "results": results}, nil
	}
	return map[string]any{"ok": false, "error": "批量命令无效"}, nil
}

func (l *Library) registerKnowledgeBatch(b *Bridge) {
	b.handlers["knowledge:selection"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		out, err := l.knowledgeSelection(ctx, jsArg(a, 0))
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return out, nil
	}
}
