import { KnowledgeItemDB, WikiDB } from "@guizhi/db";
import type Database from "../database/sqlite";
import type {
  InboxItem,
  InboxItemKind,
  InboxListResult,
  InboxOrganizeInput,
} from "@guizhi/shared/types";

function parseReasons(value: string | null): string[] {
  if (!value) return [];
  try {
    const parsed = JSON.parse(value) as unknown;
    return Array.isArray(parsed)
      ? parsed.filter((reason): reason is string => typeof reason === "string")
      : [];
  } catch {
    return [];
  }
}

function scalarCount(db: Database.Database, sql: string): number {
  return (db.get(sql) as { count: number }).count;
}

/** 统一聚合需要人工判断的条目；语义/Wiki 仅生成一张聚合卡。 */
export function listInboxItems(db: Database.Database): InboxListResult {
  const items: InboxItem[] = [];
  const reviewRows = db.all(
    `SELECT id, title, review_reasons, created_at FROM knowledge_items
     WHERE deleted_at IS NULL AND status = 'active' AND review_status = 'needs_review'
     ORDER BY created_at DESC LIMIT 100`,
  ) as Array<{
    id: string;
    title: string;
    review_reasons: string | null;
    created_at: number;
  }>;
  items.push(
    ...reviewRows.map(
      (row): InboxItem => ({
        kind: "review-required",
        id: `review:${row.id}`,
        itemId: row.id,
        title: row.title,
        reasons: parseReasons(row.review_reasons),
        createdAt: row.created_at,
      }),
    ),
  );

  const unclassifiedRows = db.all(
    `SELECT id, title, created_at FROM knowledge_items
     WHERE deleted_at IS NULL AND status = 'active' AND collection_id IS NULL
       AND review_status != 'needs_review'
     ORDER BY created_at DESC LIMIT 100`,
  ) as Array<{ id: string; title: string; created_at: number }>;
  items.push(
    ...unclassifiedRows.map(
      (row): InboxItem => ({
        kind: "unclassified",
        id: `unclassified:${row.id}`,
        itemId: row.id,
        title: row.title,
        createdAt: row.created_at,
      }),
    ),
  );

  const issueRows = db.all(
    `SELECT id, display_name, status, error, warning, result_item_id,
            duplicate_item_id, created_at
     FROM import_tasks
     WHERE status IN ('failed','duplicate') OR warning IS NOT NULL
     ORDER BY created_at DESC LIMIT 100`,
  ) as Array<{
    id: string;
    display_name: string;
    status: "failed" | "duplicate" | "completed";
    error: string | null;
    warning: string | null;
    result_item_id: string | null;
    duplicate_item_id: string | null;
    created_at: number;
  }>;
  items.push(
    ...issueRows.map(
      (row): InboxItem => ({
        kind: "import-issue",
        id: `import:${row.id}`,
        taskId: row.id,
        title: row.display_name,
        status: row.status,
        message: row.error || row.warning || "需要处理",
        resultItemId: row.result_item_id,
        duplicateItemId: row.duplicate_item_id,
        createdAt: row.created_at,
      }),
    ),
  );

  const candidateRows = db.all(
    `SELECT view_id, external_id, item_json, first_seen_at
     FROM platform_discovery_candidates WHERE state = 'new'
     ORDER BY first_seen_at DESC LIMIT 100`,
  ) as Array<{
    view_id: string;
    external_id: string;
    item_json: string;
    first_seen_at: number;
  }>;
  items.push(
    ...candidateRows.map((row): InboxItem => {
      let title = row.external_id;
      try {
        const parsed = JSON.parse(row.item_json) as { title?: unknown };
        if (typeof parsed.title === "string" && parsed.title.trim()) {
          title = parsed.title.trim();
        }
      } catch {
        // 候选 JSON 损坏仍显示，不能让它从处理中心消失
      }
      return {
        kind: "discovery-candidate",
        id: `candidate:${row.view_id}:${row.external_id}`,
        viewId: row.view_id,
        externalId: row.external_id,
        title,
        createdAt: row.first_seen_at,
      };
    }),
  );

  const semanticPending = scalarCount(
    db,
    `SELECT COUNT(*) AS count FROM knowledge_items i
     WHERE i.deleted_at IS NULL AND i.status = 'active'
       AND NOT EXISTS (SELECT 1 FROM knowledge_embeddings e WHERE e.item_id = i.id)`,
  );
  const wikiPending = new WikiDB(db).listCompilableItems().length;
  if (semanticPending > 0) {
    items.unshift({
      kind: "semantic-pending",
      id: "aggregate:semantic",
      count: semanticPending,
      createdAt: Date.now(),
    });
  }
  if (wikiPending > 0) {
    items.unshift({
      kind: "wiki-pending",
      id: "aggregate:wiki",
      count: wikiPending,
      createdAt: Date.now(),
    });
  }

  const counts: Record<InboxItemKind, number> = {
    "review-required": scalarCount(
      db,
      "SELECT COUNT(*) AS count FROM knowledge_items WHERE deleted_at IS NULL AND status = 'active' AND review_status = 'needs_review'",
    ),
    unclassified: scalarCount(
      db,
      "SELECT COUNT(*) AS count FROM knowledge_items WHERE deleted_at IS NULL AND status = 'active' AND collection_id IS NULL AND review_status != 'needs_review'",
    ),
    "import-issue": scalarCount(
      db,
      "SELECT COUNT(*) AS count FROM import_tasks WHERE status IN ('failed','duplicate') OR warning IS NOT NULL",
    ),
    "discovery-candidate": scalarCount(
      db,
      "SELECT COUNT(*) AS count FROM platform_discovery_candidates WHERE state = 'new'",
    ),
    "semantic-pending": semanticPending > 0 ? 1 : 0,
    "wiki-pending": wikiPending > 0 ? 1 : 0,
  };
  return {
    items,
    counts,
    total: Object.values(counts).reduce((sum, count) => sum + count, 0),
  };
}

export function organizeInboxItems(
  db: Database.Database,
  input: InboxOrganizeInput,
): number {
  return new KnowledgeItemDB(db).bulkUpdate(input.itemIds, {
    collectionId: input.collectionId,
    addTagNames: input.addTagNames,
  });
}

export function markInboxItemsReviewed(
  db: Database.Database,
  itemIds: string[],
): number {
  return new KnowledgeItemDB(db).bulkUpdate(itemIds, {
    reviewStatus: "clear",
    reviewReasons: [],
  });
}
