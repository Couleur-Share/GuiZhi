/**
 * 导入任务 DAO。队列本体在主进程内存中调度，
 * 本表提供持久化与重启恢复能力。
 */
import { randomUUID } from "crypto";
import type Database from "./adapter";
import { IMPORT_TASK_STATUSES } from "@guizhi/shared/types";
import type {
  EnqueueImportInput,
  ImportStage,
  ImportStageStat,
  ImportTask,
  ImportTaskStatus,
  ImportCaptureStrategy,
  ImportTaskListQuery,
  ImportTaskListResult,
  ImportTaskClearQuery,
  CommentLimit,
  KnowledgeItemType,
} from "@guizhi/shared/types";

interface TaskRow {
  id: string;
  source_kind: ImportTask["sourceKind"];
  source_input: string;
  display_name: string;
  status: ImportTaskStatus;
  stage: ImportStage | null;
  error: string | null;
  warning: string | null;
  item_type: KnowledgeItemType | null;
  result_item_id: string | null;
  duplicate_item_id: string | null;
  collection_id: string | null;
  refresh_of_item_id: string | null;
  tag_names: string | null;
  stage_stats: string | null;
  capture_strategy: ImportCaptureStrategy;
  comment_limit: CommentLimit;
  force_duplicate: number;
  created_at: number;
  updated_at: number;
}

/** tag_names 存的是 JSON 数组；老行为 NULL，坏数据当成没有标签 */
function parseTagNames(raw: string | null): string[] {
  if (!raw) {
    return [];
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed)
      ? parsed.filter((name): name is string => typeof name === "string")
      : [];
  } catch {
    return [];
  }
}

/**
 * stage_stats 存的是 JSON 数组；老行为 NULL。
 *
 * 坏数据一律当成「没有统计」而不是抛错：这一列纯属观测，
 * 解析不动它不该让整个导入列表读不出来。
 */
function parseStageStats(raw: string | null): ImportStageStat[] | null {
  if (!raw) {
    return null;
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return null;
    }
    const stats = parsed.filter(
      (entry): entry is ImportStageStat =>
        typeof entry === "object" &&
        entry !== null &&
        typeof (entry as ImportStageStat).stage === "string" &&
        typeof (entry as ImportStageStat).ms === "number",
    );
    return stats.length > 0 ? stats : null;
  } catch {
    return null;
  }
}

/**
 * 「清理已完成」的清理范围。
 *
 * 有意排除 failed：失败任务保留着原始输入与失败原因，是用户唯一的重试入口，
 * 一并清掉等于让这批链接彻底消失。
 */
const CLEARABLE_STATUSES: ImportTaskStatus[] = [
  "completed",
  "canceled",
  "duplicate",
];

const TERMINAL_STATUSES: ImportTaskStatus[] = [
  "completed",
  "failed",
  "canceled",
  "duplicate",
];

interface ImportCursor {
  createdAt: number;
  id: string;
}

function encodeCursor(cursor: ImportCursor): string {
  return Buffer.from(JSON.stringify(cursor), "utf8").toString("base64url");
}

function decodeCursor(value: string | null | undefined): ImportCursor | null {
  if (!value) return null;
  try {
    const parsed = JSON.parse(
      Buffer.from(value, "base64url").toString("utf8"),
    ) as Partial<ImportCursor>;
    return Number.isFinite(parsed.createdAt) && typeof parsed.id === "string"
      ? { createdAt: Number(parsed.createdAt), id: parsed.id }
      : null;
  } catch {
    return null;
  }
}

function normalizeListQuery(query?: ImportTaskListQuery): Required<
  Pick<ImportTaskListQuery, "status" | "query" | "pageSize">
> & { cursor: ImportCursor | null } {
  const pageSize = Math.min(Math.max(Math.round(query?.pageSize ?? 50), 20), 100);
  return {
    status: query?.status ?? "all",
    query: query?.query?.trim() ?? "",
    pageSize,
    cursor: decodeCursor(query?.cursor),
  };
}

function buildSearchClause(keyword: string): { sql: string; params: string[] } {
  if (!keyword) return { sql: "", params: [] };
  const pattern = `%${keyword.toLowerCase()}%`;
  return {
    sql: ` AND (LOWER(display_name) LIKE ? OR LOWER(source_input) LIKE ?
      OR LOWER(COALESCE(error, '')) LIKE ? OR LOWER(COALESCE(warning, '')) LIKE ?)`,
    params: [pattern, pattern, pattern, pattern],
  };
}

function mapRow(row: TaskRow): ImportTask {
  return {
    id: row.id,
    sourceKind: row.source_kind,
    sourceInput: row.source_input,
    displayName: row.display_name,
    status: row.status,
    stage: row.stage,
    error: row.error,
    warning: row.warning,
    itemType: row.item_type,
    resultItemId: row.result_item_id,
    duplicateItemId: row.duplicate_item_id,
    collectionId: row.collection_id,
    refreshOfItemId: row.refresh_of_item_id,
    tagNames: parseTagNames(row.tag_names),
    stageStats: parseStageStats(row.stage_stats),
    captureStrategy: row.capture_strategy ?? "standard",
    commentLimit: row.comment_limit ?? 0,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function makeDisplayName(input: EnqueueImportInput): string {
  const value = input.input.trim();
  if (input.kind === "url") {
    return value;
  }
  if (input.kind === "file") {
    const segments = value.split(/[\\/]/);
    return segments[segments.length - 1] || value;
  }
  const firstLine = value.split(/\r?\n/, 1)[0] ?? "";
  return firstLine.length > 60 ? `${firstLine.slice(0, 60)}…` : firstLine;
}

export class ImportTaskDB {
  constructor(private readonly db: Database.Database) {}

  create(input: EnqueueImportInput): ImportTask {
    const now = Date.now();
    const id = randomUUID();
    this.db.run(
      `INSERT INTO import_tasks
         (id, source_kind, source_input, display_name, status, collection_id, refresh_of_item_id, tag_names, capture_strategy, comment_limit, force_duplicate, created_at, updated_at)
       VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?)`,
      id,
      input.kind,
      input.input,
      makeDisplayName(input),
      input.collectionId ?? null,
      input.refreshOfItemId ?? null,
      input.tagNames?.length ? JSON.stringify(input.tagNames) : null,
      input.captureStrategy ?? "standard",
      input.commentLimit ?? 0,
      input.forceDuplicate ? 1 : 0,
      now,
      now,
    );
    const task = this.get(id);
    if (!task) {
      throw new Error(`Failed to load created import task: ${id}`);
    }
    return task;
  }

  get(id: string): ImportTask | null {
    const row = this.db.get(
      "SELECT * FROM import_tasks WHERE id = ?",
      id,
    ) as TaskRow | undefined;
    return row ? mapRow(row) : null;
  }

  /** 是否需要跳过去重（「仍要创建副本」重新入队的任务） */
  isForceDuplicate(id: string): boolean {
    const row = this.db.get(
      "SELECT force_duplicate FROM import_tasks WHERE id = ?",
      id,
    ) as { force_duplicate: number } | undefined;
    return row?.force_duplicate === 1;
  }

  list(limit = 200): ImportTask[] {
    const rows = this.db.all(
      "SELECT * FROM import_tasks ORDER BY created_at DESC LIMIT ?",
      limit,
    ) as TaskRow[];
    return rows.map(mapRow);
  }

  listPage(query?: ImportTaskListQuery): ImportTaskListResult {
    const normalized = normalizeListQuery(query);
    const search = buildSearchClause(normalized.query);
    const statusSql = normalized.status === "all" ? "" : " AND status = ?";
    const statusParams =
      normalized.status === "all" ? [] : [normalized.status];
    const cursorSql = normalized.cursor
      ? " AND (created_at < ? OR (created_at = ? AND id < ?))"
      : "";
    const cursorParams = normalized.cursor
      ? [
          normalized.cursor.createdAt,
          normalized.cursor.createdAt,
          normalized.cursor.id,
        ]
      : [];
    const rows = this.db.all(
      `SELECT * FROM import_tasks WHERE 1 = 1${search.sql}${statusSql}${cursorSql}
       ORDER BY created_at DESC, id DESC LIMIT ?`,
      ...search.params,
      ...statusParams,
      ...cursorParams,
      normalized.pageSize + 1,
    ) as TaskRow[];
    const hasMore = rows.length > normalized.pageSize;
    const pageRows = rows.slice(0, normalized.pageSize);
    const tail = pageRows[pageRows.length - 1];

    const activeRows = this.db.all(
      `SELECT * FROM import_tasks WHERE status IN ('pending','processing')${search.sql}
       ORDER BY created_at ASC, id ASC`,
      ...search.params,
    ) as TaskRow[];
    const totalRow = this.db.get(
      `SELECT COUNT(*) AS count FROM import_tasks WHERE 1 = 1${search.sql}${statusSql}`,
      ...search.params,
      ...statusParams,
    ) as { count: number };
    const countRows = this.db.all(
      `SELECT status, COUNT(*) AS count FROM import_tasks WHERE 1 = 1${search.sql}
       GROUP BY status`,
      ...search.params,
    ) as Array<{ status: ImportTaskStatus; count: number }>;
    const counts = Object.fromEntries(
      ([...IMPORT_TASK_STATUSES] as ImportTaskStatus[]).map((status) => [status, 0]),
    ) as Record<ImportTaskStatus, number>;
    for (const row of countRows) counts[row.status] = row.count;

    return {
      entries: pageRows.map(mapRow),
      active: activeRows.map(mapRow),
      nextCursor:
        hasMore && tail
          ? encodeCursor({ createdAt: tail.created_at, id: tail.id })
          : null,
      total: totalRow.count,
      counts,
    };
  }

  countTerminal(query: ImportTaskClearQuery): number {
    const search = buildSearchClause(query.scope === "filtered" ? query.query?.trim() ?? "" : "");
    const requestedStatus = query.scope === "filtered" ? query.status ?? "all" : "all";
    const statuses =
      requestedStatus !== "all" && TERMINAL_STATUSES.includes(requestedStatus)
        ? [requestedStatus]
        : TERMINAL_STATUSES;
    const placeholders = statuses.map(() => "?").join(",");
    const row = this.db.get(
      `SELECT COUNT(*) AS count FROM import_tasks
       WHERE status IN (${placeholders})${search.sql}`,
      ...statuses,
      ...search.params,
    ) as { count: number };
    return row.count;
  }

  clearTerminal(query: ImportTaskClearQuery): number {
    const search = buildSearchClause(query.scope === "filtered" ? query.query?.trim() ?? "" : "");
    const requestedStatus = query.scope === "filtered" ? query.status ?? "all" : "all";
    const statuses =
      requestedStatus !== "all" && TERMINAL_STATUSES.includes(requestedStatus)
        ? [requestedStatus]
        : TERMINAL_STATUSES;
    const placeholders = statuses.map(() => "?").join(",");
    return this.db.run(
      `DELETE FROM import_tasks WHERE status IN (${placeholders})${search.sql}`,
      ...statuses,
      ...search.params,
    ).changes;
  }

  listByStatus(statuses: ImportTaskStatus[]): ImportTask[] {
    if (statuses.length === 0) {
      return [];
    }
    const placeholders = statuses.map(() => "?").join(", ");
    const rows = this.db.all(
      `SELECT * FROM import_tasks WHERE status IN (${placeholders}) ORDER BY created_at ASC`,
      ...statuses,
    ) as TaskRow[];
    return rows.map(mapRow);
  }

  update(
    id: string,
    patch: Partial<{
      status: ImportTaskStatus;
      stage: ImportStage | null;
      error: string | null;
      /** 已入库但内容有缺失的原因；与 error 分开，见 ImportTask.warning */
      warning: string | null;
      /** 抽取拿到真实标题后回写，替换建任务时的原始 URL / 首行 */
      displayName: string;
      itemType: KnowledgeItemType | null;
      resultItemId: string | null;
      duplicateItemId: string | null;
      forceDuplicate: boolean;
      /** 各阶段耗时与 AI 开销；传 null 清空（重试时用） */
      stageStats: ImportStageStat[] | null;
      captureStrategy: ImportCaptureStrategy;
      commentLimit: CommentLimit;
    }>,
  ): ImportTask | null {
    const existing = this.db.get(
      "SELECT * FROM import_tasks WHERE id = ?",
      id,
    ) as TaskRow | undefined;
    if (!existing) {
      return null;
    }
    this.db.run(
      `UPDATE import_tasks SET
         status = ?, stage = ?, error = ?, warning = ?, display_name = ?, item_type = ?,
         result_item_id = ?, duplicate_item_id = ?, stage_stats = ?, capture_strategy = ?,
         comment_limit = ?, force_duplicate = ?,
         updated_at = ?
       WHERE id = ?`,
      patch.status ?? existing.status,
      patch.stage !== undefined ? patch.stage : existing.stage,
      patch.error !== undefined ? patch.error : existing.error,
      patch.warning !== undefined ? patch.warning : existing.warning,
      patch.displayName?.trim() || existing.display_name,
      patch.itemType !== undefined ? patch.itemType : existing.item_type,
      patch.resultItemId !== undefined
        ? patch.resultItemId
        : existing.result_item_id,
      patch.duplicateItemId !== undefined
        ? patch.duplicateItemId
        : existing.duplicate_item_id,
      patch.stageStats !== undefined
        ? patch.stageStats?.length
          ? JSON.stringify(patch.stageStats)
          : null
        : existing.stage_stats,
      patch.captureStrategy ?? existing.capture_strategy,
      patch.commentLimit ?? existing.comment_limit,
      patch.forceDuplicate !== undefined
        ? patch.forceDuplicate
          ? 1
          : 0
        : existing.force_duplicate,
      Date.now(),
      id,
    );
    return this.get(id);
  }

  /** 启动恢复：上次退出时仍在处理中的任务复位为待处理。 */
  resetProcessingToPending(): number {
    return this.db.run(
      "UPDATE import_tasks SET status = 'pending', stage = NULL, updated_at = ? WHERE status = 'processing'",
      Date.now(),
    ).changes;
  }

  clearFinished(): number {
    const placeholders = CLEARABLE_STATUSES.map(() => "?").join(", ");
    return this.db.run(
      `DELETE FROM import_tasks WHERE status IN (${placeholders})`,
      ...CLEARABLE_STATUSES,
    ).changes;
  }

  /**
   * 删除一条已结束的任务。
   *
   * failed 有意不进「清理已完成」（它是重试入口），但也得有单独的出口——
   * 否则失败任务永久堆积，超过 list 的 200 条窗口后连看都看不到，
   * 却仍然占着位置把更早的任务挤出去。
   */
  remove(id: string): boolean {
    return (
      this.db.run(
        "DELETE FROM import_tasks WHERE id = ? AND status NOT IN ('pending', 'processing')",
        id,
      ).changes > 0
    );
  }
}
