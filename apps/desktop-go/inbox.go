package main

// 对应 apps/desktop/src/main/services/inbox.ts 的 listInboxItems（只读）。

import (
	"context"
	"encoding/json"
)

func (l *Library) scalarCount(ctx context.Context, sql string) (int64, error) {
	rows, err := l.db.Query(ctx, sql)
	if err != nil {
		return 0, err
	}
	if len(rows.Values) == 0 {
		return 0, nil
	}
	return asInt(rows.Values[0][0]), nil
}

func (l *Library) inboxList(ctx context.Context) (any, error) {
	items := []map[string]any{}

	review, err := l.db.Query(ctx, `SELECT id, title, review_reasons, created_at FROM knowledge_items
     WHERE deleted_at IS NULL AND status = 'active' AND review_status = 'needs_review'
     ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	for _, r := range review.Values {
		id, _ := r[0].(string)
		items = append(items, map[string]any{
			"kind": "review-required", "id": "review:" + id, "itemId": r[0], "title": r[1],
			"reasons": parseReviewReasons(r[2]), "createdAt": r[3],
		})
	}

	unclassified, err := l.db.Query(ctx, `SELECT id, title, created_at FROM knowledge_items
     WHERE deleted_at IS NULL AND status = 'active' AND collection_id IS NULL
       AND review_status != 'needs_review'
     ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	for _, r := range unclassified.Values {
		id, _ := r[0].(string)
		items = append(items, map[string]any{
			"kind": "unclassified", "id": "unclassified:" + id, "itemId": r[0], "title": r[1], "createdAt": r[2],
		})
	}

	issues, err := l.db.Query(ctx, `SELECT id, display_name, status, error, warning, result_item_id,
            duplicate_item_id, created_at
     FROM import_tasks
     WHERE status IN ('failed','duplicate')
        OR (warning IS NOT NULL AND warning_acknowledged_at IS NULL)
     ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	for _, r := range issues.Values {
		id, _ := r[0].(string)
		// JS 的 `error || warning || "需要处理"`：空串与 null 同样落到下一个
		message := "需要处理"
		if s, _ := r[4].(string); s != "" {
			message = s
		}
		if s, _ := r[3].(string); s != "" {
			message = s
		}
		items = append(items, map[string]any{
			"kind": "import-issue", "id": "import:" + id, "taskId": r[0], "title": r[1], "status": r[2],
			"message": message, "resultItemId": r[5], "duplicateItemId": r[6], "createdAt": r[7],
		})
	}

	semanticPending, err := l.scalarCount(ctx, `SELECT COUNT(*) AS count FROM knowledge_items i
     WHERE i.deleted_at IS NULL AND i.status = 'active'
       AND NOT EXISTS (SELECT 1 FROM knowledge_embeddings e WHERE e.item_id = i.id)`)
	if err != nil {
		return nil, err
	}
	if semanticPending > 0 {
		items = append([]map[string]any{{
			"kind": "semantic-pending", "id": "aggregate:semantic", "count": semanticPending, "createdAt": nowMs(),
		}}, items...)
	}

	counts := map[string]any{"semantic-pending": semanticPending, "wiki-pending": 0}
	for key, sql := range map[string]string{
		"review-required":     "SELECT COUNT(*) AS count FROM knowledge_items WHERE deleted_at IS NULL AND status = 'active' AND review_status = 'needs_review'",
		"unclassified":        "SELECT COUNT(*) AS count FROM knowledge_items WHERE deleted_at IS NULL AND status = 'active' AND collection_id IS NULL AND review_status != 'needs_review'",
		"import-issue":        "SELECT COUNT(*) AS count FROM import_tasks WHERE status IN ('failed','duplicate') OR (warning IS NOT NULL AND warning_acknowledged_at IS NULL)",
	} {
		n, err := l.scalarCount(ctx, sql)
		if err != nil {
			return nil, err
		}
		counts[key] = n
	}

	// 角标只计「不处理就有风险或损失」的项；按内容身份去重，全量查询不受分组上限影响。
	total, err := l.scalarCount(ctx, `SELECT COUNT(*) AS count FROM (
        SELECT 'item:' || id AS identity FROM knowledge_items
        WHERE deleted_at IS NULL AND status = 'active'
          AND review_status = 'needs_review'
        UNION
        SELECT CASE WHEN COALESCE(result_item_id, duplicate_item_id) IS NOT NULL
          THEN 'item:' || COALESCE(result_item_id, duplicate_item_id)
          ELSE 'import:' || id END FROM import_tasks
        WHERE status IN ('failed','duplicate')
          OR (warning IS NOT NULL AND warning_acknowledged_at IS NULL)
      )`)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "counts": counts, "total": total}, nil
}

func (l *Library) registerInbox(b *Bridge) {
	b.handlers["inbox:list"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		return l.inboxList(ctx)
	}
}
