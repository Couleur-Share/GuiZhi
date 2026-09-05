/**
 * 导入服务组装：把队列、DAO、连接器与广播接到一起。
 */
import { KnowledgeItemDB, ImportTaskDB, SourceCommentDB, SourceAccessDB } from "@guizhi/db";
import type Database from "../../database/sqlite";
import {
  DEFAULT_NETWORK_PROXY_SETTINGS,
  type ImportTask,
  type NetworkProxySettings,
} from "@guizhi/shared/types";
import { normalizeNetworkProxySettings } from "@guizhi/shared/utils/network-proxy";
import { resolveSourcePlatform } from "@guizhi/shared/utils/source-platforms";
import { extractContent } from "./connectors";
import { assessImportReview } from "./review-assessment";
import {
  ImportQueue,
  createSourceRecordId,
  type ImportPersistence,
} from "./import-queue";
import { getBrowserCaptureService } from "../platform-capture/browser-capture";
import {
  fetchAuthenticatedDouyin,
  fetchAuthenticatedLinuxdoJson,
  fetchAuthenticatedXiaohongshu,
  platformFromAuthenticatedUrl,
} from "../platform-capture/authenticated-platforms";
import { captureSourceComments } from "../platform-capture/source-comments";

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

    rememberSourceAccess(itemId, normalizedUri, accessUri) {
      new SourceAccessDB(db).remember(itemId, normalizedUri, accessUri);
    },

    saveItem({
      extracted,
      collectionId,
      tagNames,
      sourceKind,
      sourceInput,
      normalizedUri,
      contentHash,
    }) {
      let itemId = "";
      const review = assessImportReview(extracted, sourceKind);
      const run = db.transaction(() => {
        const created = items.create({
          title: extracted.title || undefined,
          content: extracted.content,
          transcript: extracted.transcript ?? null,
          itemType: extracted.itemType,
          collectionId,
          tagNames: tagNames.length > 0 ? tagNames : undefined,
          // 状态留在条目而非 import_tasks：用户清理任务记录后仍可回到原文
          // 完成复核。评估既接住连接器已知的部分失败，也检查解析后的短正文
          // 与不可识别字符；它们都不阻断对已获得内容的保存。
          reviewStatus: review.reviewRequired ? "needs_review" : "clear",
          reviewReasons: review.reasons,
        });
        itemId = created.id;
        db.run(
          `INSERT INTO source_records
             (id, item_id, source_type, source_uri, access_uri, normalized_uri, content_hash, platform, captured_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          createSourceRecordId(),
          created.id,
          sourceKind,
          extracted.sourceUri,
          sourceKind === "url" ? sourceInput : null,
          normalizedUri,
          contentHash,
          resolveSourcePlatform(sourceKind, extracted.sourceUri),
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
    { value: string } | undefined;
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

/** 导入时是否区分说话人（默认关：它让转写慢一倍，且只对多人内容有意义） */
export function readTranscribeDiarizeSetting(db: Database.Database): boolean {
  const row = db.get("SELECT value FROM settings WHERE key = ?", [
    "transcribeDiarize",
  ]) as { value: string } | undefined;
  if (!row) {
    return false;
  }
  try {
    return JSON.parse(row.value) === true;
  } catch {
    return false;
  }
}

export function readNetworkProxySetting(
  db: Database.Database,
): NetworkProxySettings {
  const row = db.get("SELECT value FROM settings WHERE key = ?", [
    "networkProxy",
  ]) as { value: string } | undefined;
  if (!row) return { ...DEFAULT_NETWORK_PROXY_SETTINGS };
  try {
    return normalizeNetworkProxySettings(JSON.parse(row.value));
  } catch {
    return { ...DEFAULT_NETWORK_PROXY_SETTINGS };
  }
}

export function createImportService(
  db: Database.Database,
  broadcast: (task: ImportTask) => void,
): ImportService {
  const taskDb = new ImportTaskDB(db);
  const commentDb = new SourceCommentDB(db);
  const browserCapture = getBrowserCaptureService({
    getNetworkProxy: () => readNetworkProxySetting(db),
  });
  const queue = new ImportQueue({
    store: taskDb,
    persistence: createPersistence(db),
    extract: (task, signal, onStage) => {
      if (task.captureStrategy === "authenticated") {
        onStage("browser-capture");
      }
      return extractContent(task.sourceKind, task.sourceInput, signal, {
        captureStrategy: task.captureStrategy,
        getYtDlpPath: () => readYtDlpPathSetting(db),
        getFfmpegPath: () => readFfmpegPathSetting(db),
        getDiarize: () => readTranscribeDiarizeSetting(db),
        fetchAuthenticatedDouyin: (url, requestSignal) =>
          fetchAuthenticatedDouyin(browserCapture, url, requestSignal),
        fetchAuthenticatedXiaohongshu: (url, requestSignal) =>
          fetchAuthenticatedXiaohongshu(browserCapture, url, requestSignal),
        fetchAuthenticatedLinuxdoJson: (url, requestSignal) =>
          fetchAuthenticatedLinuxdoJson(browserCapture, url, requestSignal),
        onStage,
      });
    },
    captureComments: async (task, resultItemId, signal) => {
      const platform = platformFromAuthenticatedUrl(task.sourceInput);
      if (!platform || task.captureStrategy !== "authenticated") {
        throw new Error("热门评论需要受支持平台的登录态采集");
      }
      // LINUX DO 的全部楼层已随论坛正文入库，不再重复写 source_comments。
      if (platform === "linuxdo") {
        return;
      }
      const comments = await captureSourceComments(
        browserCapture,
        platform,
        task.sourceInput,
        task.commentLimit,
        signal,
      );
      commentDb.upsertMany(
        comments.map((comment) => ({
          ...comment,
          itemId: resultItemId,
          platform,
        })),
      );
    },
    onTaskChanged: broadcast,
  });
  return { queue, taskDb };
}
