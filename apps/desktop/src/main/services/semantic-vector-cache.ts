/**
 * 语义向量进程内缓存。
 *
 * 每次搜索都从 SQLite 读 BLOB 并解码，wasm 边界成本往往高于点积本身。
 * 按 Database 实例 + model 缓存；写入（apply / clear）必须显式失效。
 *
 * ANN 门槛（未达前不做）：totalChunks >= 50_000，或缓存命中后
 * lastSearchMs 中位仍 > 500ms。届时倾向纯 JS/WASM HNSW 侧车文件。
 */
import type { SemanticIndexDB } from "@guizhi/db";

export interface SemanticVectorCache {
  model: string;
  dims: number;
  /** 连续存放的归一化向量，长度 = chunkCount * dims */
  vectors: Float32Array;
  itemIds: string[];
  chunkIndexes: number[];
}

const cachesByDb = new WeakMap<object, Map<string, SemanticVectorCache>>();

export function invalidateSemanticVectorCache(
  db?: object,
  model?: string,
): void {
  if (!db) {
    return;
  }
  const byModel = cachesByDb.get(db);
  if (!byModel) {
    return;
  }
  if (model) {
    byModel.delete(model);
    return;
  }
  byModel.clear();
}

export function getSemanticVectorCache(
  db: object,
  model: string,
): SemanticVectorCache | undefined {
  return cachesByDb.get(db)?.get(model);
}

/** 游标全量加载并写入缓存；维度不一致的分块跳过 */
export function buildSemanticVectorCache(
  db: object,
  index: SemanticIndexDB,
  model: string,
): SemanticVectorCache {
  const itemIds: string[] = [];
  const chunkIndexes: number[] = [];
  const parts: Float32Array[] = [];
  let dims = 0;
  let cursor = 0;
  const pageSize = 500;

  for (;;) {
    const batch = index.loadVectorsForSearch(model, pageSize, cursor);
    if (batch.length === 0) {
      break;
    }
    cursor = batch[batch.length - 1].rowid;
    for (const chunk of batch) {
      if (chunk.vector.length === 0) {
        continue;
      }
      if (dims === 0) {
        dims = chunk.vector.length;
      }
      if (chunk.vector.length !== dims) {
        continue;
      }
      itemIds.push(chunk.itemId);
      chunkIndexes.push(chunk.chunkIndex);
      parts.push(chunk.vector);
    }
    if (batch.length < pageSize) {
      break;
    }
  }

  const vectors = new Float32Array(parts.length * Math.max(dims, 1));
  for (let index_ = 0; index_ < parts.length; index_++) {
    vectors.set(parts[index_], index_ * dims);
  }

  const cache: SemanticVectorCache = {
    model,
    dims,
    vectors,
    itemIds,
    chunkIndexes,
  };
  let byModel = cachesByDb.get(db);
  if (!byModel) {
    byModel = new Map();
    cachesByDb.set(db, byModel);
  }
  byModel.set(model, cache);
  return cache;
}

export function ensureSemanticVectorCache(
  db: object,
  index: SemanticIndexDB,
  model: string,
): SemanticVectorCache {
  return (
    getSemanticVectorCache(db, model) ??
    buildSemanticVectorCache(db, index, model)
  );
}
