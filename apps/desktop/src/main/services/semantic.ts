/**
 * 语义索引主进程服务：pending 判定 + 余弦 top-k 检索。
 *
 * 哈希是判定与落库的唯一锚点：listPending 计算并下发 contentHash，
 * 渲染进程完成嵌入后原样回传，避免两端标准化算法漂移。
 */
import { SemanticIndexDB } from "@guizhi/db";
import type {
  PendingSemanticItem,
  SemanticIndexStatus,
  SemanticSearchHit,
} from "@guizhi/shared/types";
import Database from "../database/sqlite";
import { computeContentHash } from "./import/content-hash";

const SNIPPET_MAX_LENGTH = 160;

interface EligibleItemRow {
  id: string;
  title: string;
  content: string;
  transcript: string | null;
}

/** 参与语义索引的规范文本（哈希与嵌入的共同基础） */
export function buildSemanticSourceText(
  title: string,
  content: string,
  transcript: string | null,
): string {
  const parts = [title.trim(), content.trim()];
  if (transcript?.trim()) {
    parts.push(transcript.trim());
  }
  return parts.filter(Boolean).join("\n");
}

/** 未删除且有实际内容的条目（空白笔记无可嵌入文本，不算 eligible） */
function listEligibleItems(db: Database.Database): EligibleItemRow[] {
  const rows = db.all(
    `SELECT id, title, content, transcript
     FROM knowledge_items
     WHERE deleted_at IS NULL
     ORDER BY updated_at DESC`,
  ) as EligibleItemRow[];
  return rows.filter(
    (row) => buildSemanticSourceText(row.title, row.content, row.transcript).length > 0,
  );
}

/** 待索引条目：无索引 / 内容哈希变化 / 模型切换 */
export function listPendingSemanticItems(
  db: Database.Database,
  model: string,
  limit: number,
): PendingSemanticItem[] {
  const states = new SemanticIndexDB(db).listItemStates();
  const pending: PendingSemanticItem[] = [];
  for (const row of listEligibleItems(db)) {
    const contentHash = computeContentHash(
      buildSemanticSourceText(row.title, row.content, row.transcript),
    );
    const state = states.get(row.id);
    if (state && state.contentHash === contentHash && state.model === model) {
      continue;
    }
    pending.push({
      id: row.id,
      title: row.title,
      content: row.content,
      transcript: row.transcript,
      contentHash,
    });
    if (pending.length >= limit) {
      break;
    }
  }
  return pending;
}

export function getSemanticStatus(
  db: Database.Database,
  model: string,
): SemanticIndexStatus {
  const semantic = new SemanticIndexDB(db);
  const states = semantic.listItemStates();
  const eligible = listEligibleItems(db);

  let indexedItems = 0;
  for (const row of eligible) {
    const state = states.get(row.id);
    if (!state || state.model !== model) {
      continue;
    }
    const contentHash = computeContentHash(
      buildSemanticSourceText(row.title, row.content, row.transcript),
    );
    if (state.contentHash === contentHash) {
      indexedItems++;
    }
  }

  return {
    indexedItems,
    eligibleItems: eligible.length,
    totalChunks: semantic.stats().totalChunks,
  };
}

/**
 * 余弦 top-k：向量均已 L2 归一化，点积即余弦相似度；
 * 条目分数取其分块最高分，返回按分数倒序的前 limit 个条目。
 */
export function searchSemanticByVector(
  db: Database.Database,
  model: string,
  queryVector: Float32Array,
  limit: number,
): SemanticSearchHit[] {
  const chunks = new SemanticIndexDB(db).loadChunksForSearch(model);
  const bestByItem = new Map<string, SemanticSearchHit>();

  for (const chunk of chunks) {
    if (chunk.vector.length !== queryVector.length) {
      continue;
    }
    let dot = 0;
    for (let i = 0; i < queryVector.length; i++) {
      dot += queryVector[i] * chunk.vector[i];
    }
    const existing = bestByItem.get(chunk.itemId);
    if (!existing || dot > existing.score) {
      const snippet = chunk.chunkText.replace(/\s+/g, " ").trim();
      bestByItem.set(chunk.itemId, {
        itemId: chunk.itemId,
        title: chunk.title,
        snippet:
          snippet.length > SNIPPET_MAX_LENGTH
            ? `${snippet.slice(0, SNIPPET_MAX_LENGTH)}…`
            : snippet,
        score: dot,
      });
    }
  }

  return [...bestByItem.values()]
    .sort((left, right) => right.score - left.score)
    .slice(0, Math.max(1, limit));
}
