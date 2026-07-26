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

/** 有实际可嵌入文本的条件（空白笔记不算 eligible） */
const ELIGIBLE_CONDITION = `
  i.deleted_at IS NULL
  AND (
    TRIM(i.title) != ''
    OR TRIM(i.content) != ''
    OR TRIM(COALESCE(i.transcript, '')) != ''
  )`;

/**
 * 可能需要重新索引的条目 id（只查 id，不读正文）。
 *
 * 原来的做法是把全库正文读进内存、逐条算 SHA 再比对，而这个判定每 5 分钟
 * 跑一轮、索引循环每批 10 条还要再跑一次——索引 N 条要做 N/10 次全库扫描。
 * 这里先用「没有索引行 / 换过模型 / 条目比索引新」在 SQL 里筛掉绝大多数，
 * 哈希仍是最终authority，只是不必为没动过的条目付出读正文的代价。
 *
 * updated_at 用 >= 比较：索引行写在读取条目之后，正常情况下严格更大；
 * 取等是为了不漏掉同一毫秒内完成的那种极端情形，代价只是多算一次哈希。
 */
function listCandidateItemIds(db: Database.Database, model: string): string[] {
  const rows = db.all(
    `SELECT i.id
     FROM knowledge_items i
     LEFT JOIN (
       SELECT item_id, MIN(model) AS model, MAX(updated_at) AS updated_at
       FROM knowledge_embeddings GROUP BY item_id
     ) e ON e.item_id = i.id
     WHERE ${ELIGIBLE_CONDITION}
       AND (e.item_id IS NULL OR e.model != ? OR i.updated_at >= e.updated_at)
     ORDER BY i.updated_at DESC`,
    model,
  ) as Array<{ id: string }>;
  return rows.map((row) => row.id);
}

function countEligibleItems(db: Database.Database): number {
  const row = db.get(
    `SELECT COUNT(*) AS c FROM knowledge_items i WHERE ${ELIGIBLE_CONDITION}`,
  ) as { c: number } | undefined;
  return row?.c ?? 0;
}

/**
 * 待索引条目：无索引 / 内容哈希变化 / 模型切换。
 *
 * SQL 预筛只看得到时间戳，哈希才是权威判定。被预筛捞出来、哈希一比却发现
 * 没变的条目（收藏、打标签、写 AI 摘要都会推高 updated_at，却都不影响
 * 参与嵌入的文本），顺手把它们的索引行时间戳抬到当前，让它们退出候选集合。
 *
 * 不这么做的后果实测过：这类条目会永远留在候选里，侧栏一直显示
 * 「索引 N 条新内容」，点下去 pending 是空的，界面上就是「点了没反应」。
 */
function resolvePendingItems(
  db: Database.Database,
  model: string,
  limit?: number,
): PendingSemanticItem[] {
  const index = new SemanticIndexDB(db);
  const states = index.listItemStates();
  const pending: PendingSemanticItem[] = [];
  const upToDate: string[] = [];

  for (const id of listCandidateItemIds(db, model)) {
    const row = db.get(
      "SELECT id, title, content, transcript FROM knowledge_items WHERE id = ?",
      id,
    ) as EligibleItemRow | undefined | null;
    if (!row) {
      continue;
    }
    const contentHash = computeContentHash(
      buildSemanticSourceText(row.title, row.content, row.transcript),
    );
    const state = states.get(row.id);
    if (state && state.contentHash === contentHash && state.model === model) {
      upToDate.push(row.id);
      continue;
    }
    pending.push({
      id: row.id,
      title: row.title,
      content: row.content,
      transcript: row.transcript,
      contentHash,
    });
    if (limit !== undefined && pending.length >= limit) {
      break;
    }
  }

  index.touchIndexedAt(upToDate);
  return pending;
}

export function listPendingSemanticItems(
  db: Database.Database,
  model: string,
  limit: number,
): PendingSemanticItem[] {
  return resolvePendingItems(db, model, limit);
}

/**
 * 索引进度。
 *
 * 「待索引」必须与真正要跑的那批条目同一个口径，否则按钮上的数字点不动：
 * 早先这里数的是 SQL 预筛出来的候选数，而候选里混着一批哈希其实没变的
 * 条目，它们进不了 pending，数字却一直挂在那儿。
 *
 * 走 resolvePendingItems 意味着要为候选条目读一次正文算哈希，但候选集合
 * 只包含「时间戳动过」的条目，稳态下很小；而且每算一次就会把确认没变的
 * 那些踢出候选，下一次更便宜。
 */
export function getSemanticStatus(
  db: Database.Database,
  model: string,
): SemanticIndexStatus {
  const eligibleItems = countEligibleItems(db);
  const pending = resolvePendingItems(db, model).length;

  return {
    indexedItems: Math.max(0, eligibleItems - pending),
    eligibleItems,
    totalChunks: new SemanticIndexDB(db).stats().totalChunks,
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
  // 打分只需要「哪个条目的哪一块、多少分」，正文留到最后再取
  const bestByItem = new Map<
    string,
    { chunkIndex: number; score: number }
  >();
  const dims = queryVector.length;

  let cursor = 0;
  for (;;) {
    const batch = index.loadVectorsForSearch(model, SEARCH_BATCH_SIZE, cursor);
    if (batch.length === 0) {
      break;
    }
    cursor = batch[batch.length - 1].rowid;

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
        bestByItem.set(chunk.itemId, {
          chunkIndex: chunk.chunkIndex,
          score: dot,
        });
      }
    }

    if (batch.length < SEARCH_BATCH_SIZE) {
      break;
    }
    await yieldToEventLoop();
  }

  const top = [...bestByItem.entries()]
    .sort((left, right) => right[1].score - left[1].score)
    .slice(0, Math.max(1, limit));

  const snippets = new Map(
    index
      .loadChunkSnippets(
        top.map(([itemId, best]) => ({ itemId, chunkIndex: best.chunkIndex })),
      )
      .map((row) => [`${row.itemId}:${row.chunkIndex}`, row]),
  );

  return top.map(([itemId, best]) => {
    const row = snippets.get(`${itemId}:${best.chunkIndex}`);
    const snippet = (row?.chunkText ?? "").replace(/\s+/g, " ").trim();
    return {
      itemId,
      title: row?.title ?? "",
      snippet:
        snippet.length > SNIPPET_MAX_LENGTH
          ? `${snippet.slice(0, SNIPPET_MAX_LENGTH)}…`
          : snippet,
      score: best.score,
    };
  });
}
