import { CollectionDB, ImportTaskDB, KnowledgeItemDB } from "@guizhi/db";
import type Database from "../database/sqlite";
import type {
  InboxItem,
  InboxItemKind,
  InboxAiClassificationApplyInput,
  InboxAiClassificationApplyResult,
  InboxAiClassificationSource,
  InboxListResult,
  InboxOrganizeInput,
} from "@guizhi/shared/types";
import {
  INBOX_AI_CLASSIFICATION_MAX_ITEMS,
  INBOX_AI_CLASSIFICATION_MAX_NEW_COLLECTIONS,
  aiCollectionNameKey,
  isReservedNewCollectionName,
  isValidAiCollectionName,
  normalizeAiCollectionName,
} from "@guizhi/shared/utils/inbox-classification";

const CLASSIFICATION_EXCERPT_LENGTH = 1_600;

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

/**
 * 统一聚合需要人工判断的条目；语义仅生成一张聚合卡。
 * Wiki 的精确待编译数依赖渲染进程编译器的素材指纹、提示词版本和退避规则，
 * 由 inbox.store 在结果返回后补入，主进程不能用“可编译条目数”冒充。
 */
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
    ...reviewRows.map((row): InboxItem => ({
      kind: "review-required",
      id: `review:${row.id}`,
      itemId: row.id,
      title: row.title,
      reasons: parseReasons(row.review_reasons),
      createdAt: row.created_at,
    })),
  );

  const unclassifiedRows = db.all(
    `SELECT id, title, created_at FROM knowledge_items
     WHERE deleted_at IS NULL AND status = 'active' AND collection_id IS NULL
       AND review_status != 'needs_review'
     ORDER BY created_at DESC LIMIT 100`,
  ) as Array<{ id: string; title: string; created_at: number }>;
  items.push(
    ...unclassifiedRows.map((row): InboxItem => ({
      kind: "unclassified",
      id: `unclassified:${row.id}`,
      itemId: row.id,
      title: row.title,
      createdAt: row.created_at,
    })),
  );

  const issueRows = db.all(
    `SELECT id, display_name, status, error, warning, result_item_id,
            duplicate_item_id, created_at
     FROM import_tasks
     WHERE status IN ('failed','duplicate')
        OR (warning IS NOT NULL AND warning_acknowledged_at IS NULL)
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
    ...issueRows.map((row): InboxItem => ({
      kind: "import-issue",
      id: `import:${row.id}`,
      taskId: row.id,
      title: row.display_name,
      status: row.status,
      message: row.error || row.warning || "需要处理",
      resultItemId: row.result_item_id,
      duplicateItemId: row.duplicate_item_id,
      createdAt: row.created_at,
    })),
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
  if (semanticPending > 0) {
    items.unshift({
      kind: "semantic-pending",
      id: "aggregate:semantic",
      count: semanticPending,
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
      "SELECT COUNT(*) AS count FROM import_tasks WHERE status IN ('failed','duplicate') OR (warning IS NOT NULL AND warning_acknowledged_at IS NULL)",
    ),
    "discovery-candidate": scalarCount(
      db,
      "SELECT COUNT(*) AS count FROM platform_discovery_candidates WHERE state = 'new'",
    ),
    "semantic-pending": semanticPending,
    "wiki-pending": 0,
  };
  return {
    items,
    counts,
    // 按内容身份去重；全量查询不受分组列表上限影响，后台任务不计入。
    total: scalarCount(
      db,
      `SELECT COUNT(*) AS count FROM (
        SELECT 'item:' || id AS identity FROM knowledge_items
        WHERE deleted_at IS NULL AND status = 'active'
          AND (review_status = 'needs_review' OR collection_id IS NULL)
        UNION
        SELECT CASE WHEN COALESCE(result_item_id, duplicate_item_id) IS NOT NULL
          THEN 'item:' || COALESCE(result_item_id, duplicate_item_id)
          ELSE 'import:' || id END FROM import_tasks
        WHERE status IN ('failed','duplicate')
          OR (warning IS NOT NULL AND warning_acknowledged_at IS NULL)
        UNION
        SELECT 'candidate:' || platform || ':' || external_id
        FROM platform_discovery_candidates WHERE state = 'new'
      )`,
    ),
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

/** 保留导入警告原文，仅将已完成任务从处理中心移除。 */
export function acknowledgeInboxImportWarning(
  db: Database.Database,
  taskId: string,
): number {
  return new ImportTaskDB(db).acknowledgeWarning(taskId) ? 1 : 0;
}

function uniqueClassificationIds(itemIds: string[]): string[] {
  const ids = [
    ...new Set(
      itemIds
        .filter((id) => typeof id === "string" && id.trim())
        .map((id) => id.trim()),
    ),
  ];
  if (ids.length > INBOX_AI_CLASSIFICATION_MAX_ITEMS) {
    throw new Error(
      `一次最多智能归类 ${INBOX_AI_CLASSIFICATION_MAX_ITEMS} 条内容`,
    );
  }
  return ids;
}

/** 只读取点击时仍符合“未归知识库”口径的条目。 */
export function listInboxAiClassificationSources(
  db: Database.Database,
  itemIds: string[],
): InboxAiClassificationSource[] {
  const ids = uniqueClassificationIds(itemIds);
  if (ids.length === 0) return [];
  const rows = db.all(
    `SELECT id, title,
            SUBSTR(CASE WHEN TRIM(COALESCE(summary, '')) != '' THEN summary
                        ELSE COALESCE(NULLIF(content, ''), transcript, '') END,
                   1, ?) AS excerpt
     FROM knowledge_items
     WHERE id IN (${ids.map(() => "?").join(",")})
       AND deleted_at IS NULL AND status = 'active' AND collection_id IS NULL
       AND review_status != 'needs_review'`,
    CLASSIFICATION_EXCERPT_LENGTH,
    ...ids,
  ) as Array<{ id: string; title: string; excerpt: string | null }>;
  const byId = new Map(rows.map((row) => [row.id, row]));
  return ids.flatMap((id) => {
    const row = byId.get(id);
    return row
      ? [
          {
            itemId: row.id,
            title: row.title,
            excerpt: (row.excerpt ?? "")
              .replace(/\s+/g, " ")
              .trim()
              .slice(0, CLASSIFICATION_EXCERPT_LENGTH),
          },
        ]
      : [];
  });
}

/**
 * 原子应用 AI 归类计划：复用同名知识库，缺失的先创建再分组批量归入。
 * 开始于 AI 调用后被用户手动分类的条目会跳过，不覆盖新状态。
 */
export function applyInboxAiClassification(
  db: Database.Database,
  input: InboxAiClassificationApplyInput,
): InboxAiClassificationApplyResult {
  const rawAssignments = Array.isArray(input?.assignments)
    ? input.assignments
    : [];
  if (rawAssignments.length > INBOX_AI_CLASSIFICATION_MAX_ITEMS) {
    throw new Error(
      `一次最多智能归类 ${INBOX_AI_CLASSIFICATION_MAX_ITEMS} 条内容`,
    );
  }
  const seenIds = new Set<string>();
  const assignments = rawAssignments.map((assignment) => {
    const itemId = assignment?.itemId?.trim();
    const collectionName = normalizeAiCollectionName(
      assignment?.collectionName ?? "",
    );
    if (!itemId || seenIds.has(itemId)) {
      throw new Error("AI 归类计划包含缺失或重复的条目");
    }
    if (!isValidAiCollectionName(collectionName)) {
      throw new Error(
        `AI 生成了无效的知识库名称：${collectionName || "空名称"}`,
      );
    }
    seenIds.add(itemId);
    return { itemId, collectionName };
  });
  if (assignments.length === 0) {
    return { classified: 0, skipped: 0, createdCollectionNames: [] };
  }

  const eligible = new Set(
    listInboxAiClassificationSources(
      db,
      assignments.map((assignment) => assignment.itemId),
    ).map((source) => source.itemId),
  );
  const applicable = assignments.filter((assignment) =>
    eligible.has(assignment.itemId),
  );
  if (applicable.length === 0) {
    return {
      classified: 0,
      skipped: assignments.length,
      createdCollectionNames: [],
    };
  }

  const collections = new CollectionDB(db);
  const existingByName = new Map(
    collections
      .list()
      .map((collection) => [aiCollectionNameKey(collection.name), collection]),
  );
  const missingNames = [
    ...new Map(
      applicable
        .filter(
          (assignment) =>
            !existingByName.has(aiCollectionNameKey(assignment.collectionName)),
        )
        .map((assignment) => [
          aiCollectionNameKey(assignment.collectionName),
          assignment.collectionName,
        ]),
    ).values(),
  ];
  const reserved = missingNames.find(isReservedNewCollectionName);
  if (reserved) {
    throw new Error(`AI 不能创建保留的导航名称：${reserved}`);
  }
  if (missingNames.length > INBOX_AI_CLASSIFICATION_MAX_NEW_COLLECTIONS) {
    throw new Error(
      `AI 计划新建 ${missingNames.length} 个知识库，超过安全上限 ${INBOX_AI_CLASSIFICATION_MAX_NEW_COLLECTIONS}`,
    );
  }

  const createdCollectionNames: string[] = [];
  let classified = 0;
  const run = db.transaction(() => {
    for (const name of missingNames) {
      const created = collections.create({ name });
      existingByName.set(aiCollectionNameKey(name), created);
      createdCollectionNames.push(created.name);
    }
    const itemIdsByCollection = new Map<string, string[]>();
    for (const assignment of applicable) {
      const collection = existingByName.get(
        aiCollectionNameKey(assignment.collectionName),
      );
      if (!collection) {
        throw new Error(`无法解析知识库：${assignment.collectionName}`);
      }
      const itemIds = itemIdsByCollection.get(collection.id) ?? [];
      itemIds.push(assignment.itemId);
      itemIdsByCollection.set(collection.id, itemIds);
    }
    const items = new KnowledgeItemDB(db);
    for (const [collectionId, itemIds] of itemIdsByCollection) {
      classified += items.bulkUpdate(itemIds, { collectionId });
    }
  });
  run();
  return {
    classified,
    skipped: assignments.length - applicable.length,
    createdCollectionNames,
  };
}
