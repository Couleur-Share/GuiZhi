/**
 * 导入服务组装：把队列、DAO、连接器与广播接到一起。
 */
import { KnowledgeItemDB, ImportTaskDB } from "@guizhi/db";
import type Database from "../../database/sqlite";
import type { ImportTask } from "@guizhi/shared/types";
import { extractContent } from "./connectors";
import {
  ImportQueue,
  createSourceRecordId,
  type ImportPersistence,
} from "./import-queue";

function createPersistence(db: Database.Database): ImportPersistence {
  const items = new KnowledgeItemDB(db);

  return {
    findDuplicate(normalizedUri, contentHash) {
      // 只匹配未删除条目；回收站中的重复项允许重新导入
      if (normalizedUri) {
        const byUri = db.get(
          `SELECT s.item_id AS item_id FROM source_records s
           JOIN knowledge_items i ON i.id = s.item_id
           WHERE s.normalized_uri = ? AND i.deleted_at IS NULL
           ORDER BY s.captured_at DESC LIMIT 1`,
          normalizedUri,
        ) as { item_id: string } | undefined;
        if (byUri) {
          return byUri.item_id;
        }
      }
      const byHash = db.get(
        `SELECT s.item_id AS item_id FROM source_records s
         JOIN knowledge_items i ON i.id = s.item_id
         WHERE s.content_hash = ? AND i.deleted_at IS NULL
         ORDER BY s.captured_at DESC LIMIT 1`,
        contentHash,
      ) as { item_id: string } | undefined;
      return byHash?.item_id ?? null;
    },

    saveItem({ extracted, collectionId, sourceKind, normalizedUri, contentHash }) {
      let itemId = "";
      const run = db.transaction(() => {
        const created = items.create({
          title: extracted.title || undefined,
          content: extracted.content,
          transcript: extracted.transcript ?? null,
          itemType: extracted.itemType,
          status: "inbox",
          collectionId,
        });
        itemId = created.id;
        db.run(
          `INSERT INTO source_records
             (id, item_id, source_type, source_uri, normalized_uri, content_hash, captured_at)
           VALUES (?, ?, ?, ?, ?, ?, ?)`,
          createSourceRecordId(),
          created.id,
          sourceKind,
          extracted.sourceUri,
          normalizedUri,
          contentHash,
          Date.now(),
        );
      });
      run();
      return itemId;
    },
  };
}

export interface ImportService {
  queue: ImportQueue;
  taskDb: ImportTaskDB;
}

function readToolPathSetting(
  db: Database.Database,
  key: string,
): string | null {
  const row = db.get("SELECT value FROM settings WHERE key = ?", [key]) as
    | { value: string }
    | undefined;
  if (!row) {
    return null;
  }
  try {
    const parsed = JSON.parse(row.value);
    return typeof parsed === "string" && parsed.trim() ? parsed.trim() : null;
  } catch {
    return null;
  }
}

/** 读取设置里的 yt-dlp 自定义路径（空 / 未配置返回 null，走托管版或 PATH） */
export function readYtDlpPathSetting(db: Database.Database): string | null {
  return readToolPathSetting(db, "ytDlpPath");
}

/** 读取设置里的 ffmpeg 自定义路径（空 / 未配置返回 null，走托管版或 PATH） */
export function readFfmpegPathSetting(db: Database.Database): string | null {
  return readToolPathSetting(db, "ffmpegPath");
}

export function createImportService(
  db: Database.Database,
  broadcast: (task: ImportTask) => void,
): ImportService {
  const taskDb = new ImportTaskDB(db);
  const queue = new ImportQueue({
    store: taskDb,
    persistence: createPersistence(db),
    extract: (kind, input, signal) =>
      extractContent(kind, input, signal, {
        getYtDlpPath: () => readYtDlpPathSetting(db),
        getFfmpegPath: () => readFfmpegPathSetting(db),
      }),
    onTaskChanged: broadcast,
  });
  return { queue, taskDb };
}
