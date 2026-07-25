/**
 * 导入任务 DAO。队列本体在主进程内存中调度，
 * 本表提供持久化与重启恢复能力。
 */
import { randomUUID } from "crypto";
import type Database from "./adapter";
import type {
  EnqueueImportInput,
  ImportStage,
  ImportTask,
  ImportTaskStatus,
} from "@guizhi/shared/types";

interface TaskRow {
  id: string;
  source_kind: ImportTask["sourceKind"];
  source_input: string;
  display_name: string;
  status: ImportTaskStatus;
  stage: ImportStage | null;
  error: string | null;
  result_item_id: string | null;
  duplicate_item_id: string | null;
  collection_id: string | null;
  force_duplicate: number;
  created_at: number;
  updated_at: number;
}

const FINISHED_STATUSES: ImportTaskStatus[] = [
  "completed",
  "failed",
  "canceled",
  "duplicate",
];

function mapRow(row: TaskRow): ImportTask {
  return {
    id: row.id,
    sourceKind: row.source_kind,
    sourceInput: row.source_input,
    displayName: row.display_name,
    status: row.status,
    stage: row.stage,
    error: row.error,
    resultItemId: row.result_item_id,
    duplicateItemId: row.duplicate_item_id,
    collectionId: row.collection_id,
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
         (id, source_kind, source_input, display_name, status, collection_id, force_duplicate, created_at, updated_at)
       VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?)`,
      id,
      input.kind,
      input.input,
      makeDisplayName(input),
      input.collectionId ?? null,
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
      resultItemId: string | null;
      duplicateItemId: string | null;
      forceDuplicate: boolean;
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
         status = ?, stage = ?, error = ?, result_item_id = ?, duplicate_item_id = ?, force_duplicate = ?, updated_at = ?
       WHERE id = ?`,
      patch.status ?? existing.status,
      patch.stage !== undefined ? patch.stage : existing.stage,
      patch.error !== undefined ? patch.error : existing.error,
      patch.resultItemId !== undefined
        ? patch.resultItemId
        : existing.result_item_id,
      patch.duplicateItemId !== undefined
        ? patch.duplicateItemId
        : existing.duplicate_item_id,
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
    const placeholders = FINISHED_STATUSES.map(() => "?").join(", ");
    return this.db.run(
      `DELETE FROM import_tasks WHERE status IN (${placeholders})`,
      ...FINISHED_STATUSES,
    ).changes;
  }
}
