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

/** 每批取用的分块数：内存峰值与让出频率的折中 */
const SEARCH_BATCH_SIZE = 500;

function yieldToEventLoop(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

/**
 * 余弦 top-k：向量均已 L2 归一化，点积即余弦相似度；
 * 条目分数取其分块最高分，返回按分数倒序的前 limit 个条目。
 *
 * 没有 ANN 索引，这里是全量扫描。分批取用并在批间让出事件循环——
 * 一次性搬完整份索引再连着算完，会把主进程占满：所有 IPC 排队，
 * 卡住的不只是问答，而是整个界面。
 */
export async function searchSemanticByVector(
  db: Database.Database,
  model: string,
  queryVector: Float32Array,
  limit: number,
): Promise<SemanticSearchHit[]> {
  const index = new SemanticIndexDB(db);
  const bestByItem = new Map<string, SemanticSearchHit>();
  const dims = queryVector.length;

  for (let offset = 0; ; offset += SEARCH_BATCH_SIZE) {
    const batch = index.loadChunksForSearch(model, SEARCH_BATCH_SIZE, offset);
    if (batch.length === 0) {
      break;
    }

    for (const chunk of batch) {
      // 维度不一致说明这批向量出自别的模型，跳过
      if (chunk.vector.length !== dims) {
        continue;
      }
      let dot = 0;
      for (let i = 0; i < dims; i++) {
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

    if (batch.length < SEARCH_BATCH_SIZE) {
      break;
    }
    await yieldToEventLoop();
  }

  return [...bestByItem.values()]
    .sort((left, right) => right.score - left.score)
    .slice(0, Math.max(1, limit));
}
