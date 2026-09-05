/**
 * 语义索引 DAO。
 *
 * 向量以 L2 归一化的 Float32 小端字节序 BLOB 存储；
 * 检索走内存暴力点积（个人库规模足够，未来可替换 sqlite-vec）。
 * 回收站条目不参与检索（查询时按 deleted_at 过滤），
 * 彻底删除靠外键 CASCADE 清理。
 */
import type Database from "./adapter";

export interface SemanticChunkRecord {
  itemId: string;
  chunkIndex: number;
  chunkText: string;
  title: string;
  vector: Float32Array;
}

/** 打分阶段够用的最小投影：不带正文与标题 */
export interface SemanticVectorRecord {
  itemId: string;
  chunkIndex: number;
  vector: Float32Array;
  /** 游标翻页用：调用方把最后一条的 rowid 传回来取下一批 */
  rowid: number;
}

/** 命中分块的展示信息（只为最终 top-k 取） */
export interface SemanticChunkSnippet {
  itemId: string;
  chunkIndex: number;
  chunkText: string;
  title: string;
}

export interface SemanticItemState {
  itemId: string;
  contentHash: string;
  model: string;
}

interface StateRow {
  item_id: string;
  content_hash: string;
  model: string;
}

interface ChunkRow {
  item_id: string;
  chunk_index: number;
  chunk_text: string;
  title: string;
  dims: number;
  vector: Uint8Array;
}

export function vectorToBlob(vector: Float32Array): Uint8Array {
  return new Uint8Array(vector.buffer, vector.byteOffset, vector.byteLength).slice();
}

export function blobToVector(blob: Uint8Array, dims: number): Float32Array {
  // BLOB 可能不是 4 字节对齐的视图，拷贝一份再解释为 Float32
  const copy = new Uint8Array(blob);
  const vector = new Float32Array(copy.buffer, 0, dims);
  return vector;
}

export class SemanticIndexDB {
  constructor(private readonly db: Database.Database) {}

  /** 每个条目的索引状态（哈希 + 模型），pending 判定用 */
  listItemStates(): Map<string, SemanticItemState> {
    const rows = this.db.all(
      `SELECT item_id, content_hash, model
       FROM knowledge_embeddings
       WHERE chunk_index = 0`,
    ) as StateRow[];
    const result = new Map<string, SemanticItemState>();
    for (const row of rows) {
      result.set(row.item_id, {
        itemId: row.item_id,
        contentHash: row.content_hash,
        model: row.model,
      });
    }
    return result;
  }

  /** 整条目替换分块（事务）：先删旧行，再写新行 */
  replaceItemChunks(params: {
    itemId: string;
    contentHash: string;
    model: string;
    dims: number;
    chunks: { text: string; vector: Float32Array }[];
  }): void {
    const now = Date.now();
    const run = this.db.transaction(() => {
      this.db.run(
        "DELETE FROM knowledge_embeddings WHERE item_id = ?",
        params.itemId,
      );
      params.chunks.forEach((chunk, index) => {
        this.db.run(
          `INSERT INTO knowledge_embeddings
             (item_id, chunk_index, chunk_text, content_hash, model, dims, vector, updated_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
          params.itemId,
          index,
          chunk.text,
          params.contentHash,
          params.model,
          params.dims,
          vectorToBlob(chunk.vector),
          now,
        );
      });
    });
    run();
  }

  deleteItem(itemId: string): void {
    this.db.run("DELETE FROM knowledge_embeddings WHERE item_id = ?", itemId);
  }

  /**
   * 把索引行的时间戳抬到当前，用于「哈希校验后确认没变」的条目。
   *
   * pending 的 SQL 预筛只能比时间戳（`i.updated_at >= e.updated_at`），
   * 而收藏、打标签、写 AI 摘要这类改动同样会推高条目的 updated_at，
   * 却完全不影响参与嵌入的文本。不把索引行的时间戳抬上来，这些条目会
   * 永远留在候选集合里：状态栏一直显示有待索引，点下去却因为哈希没变
   * 而无事可做。
   */
  touchIndexedAt(itemIds: string[]): number {
    if (itemIds.length === 0) {
      return 0;
    }
    const placeholders = itemIds.map(() => "?").join(", ");
    // 取「当前时刻」与「条目时间戳 +1」的较大值：预筛条件是 >=（有意为之，
    // 免得漏掉同毫秒完成的索引），只写 Date.now() 的话，同一毫秒内发生的
    // 元数据改动 + 本次抬升会打平，条目要等到下一毫秒才退出候选。
    return this.db.run(
      `UPDATE knowledge_embeddings SET updated_at = MAX(
         ?,
         COALESCE(
           (SELECT i.updated_at + 1 FROM knowledge_items i
             WHERE i.id = knowledge_embeddings.item_id),
           0
         )
       )
       WHERE item_id IN (${placeholders})`,
      Date.now(),
      ...itemIds,
    ).changes;
  }

  /**
   * 加载指定模型的分块向量（排除回收站条目）。
   *
   * 支持分页：全量取回会把整份索引一次性搬过 wasm 边界（万级条目约几百 MB），
   * 检索侧按批取用，内存峰值和事件循环占用都能控制住。
   */
  loadChunksForSearch(
    model: string,
    limit?: number,
    offset = 0,
  ): SemanticChunkRecord[] {
    const params: unknown[] = [model];
    let pageClause = "";
    if (limit !== undefined) {
      // rowid 排序稳定，保证分页不重不漏
      pageClause = " ORDER BY e.rowid LIMIT ? OFFSET ?";
      params.push(limit, offset);
    }
    const rows = this.db.all(
      `SELECT e.item_id, e.chunk_index, e.chunk_text, e.dims, e.vector,
              i.title AS title
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id AND i.deleted_at IS NULL
       WHERE e.model = ?${pageClause}`,
      ...params,
    ) as ChunkRow[];
    return rows.map((row) => ({
      itemId: row.item_id,
      chunkIndex: row.chunk_index,
      chunkText: row.chunk_text,
      title: row.title,
      vector: blobToVector(row.vector, row.dims),
    }));
  }

  /**
   * 打分用的向量流。
   *
   * 与 loadChunksForSearch 的区别是不取 chunk_text 与 title——它们只在最终
   * top-k 上用得着，却要为每一个分块跨一次 wasm 边界、在 JS 侧建一个字符串。
   * 万级分块下这是白搬几十 MB 和一轮无谓的 GC 压力。
   */
  loadVectorsForSearch(
    model: string,
    limit?: number,
    afterRowid = 0,
    scope?: { collectionId?: string; excludedIds: string[] },
  ): SemanticVectorRecord[] {
    // 按 rowid 游标翻页，不用 LIMIT/OFFSET：OFFSET 要求 SQLite 先扫过
    // 并丢弃前 N 行连接结果，分批遍历全表就成了平方级。实测 6 万分块下
    // 偏移翻页 15.1s，游标翻页 1.1s。
    const params: unknown[] = [model, afterRowid];
    let scopeClause = "";
    if (scope?.collectionId) { scopeClause += " AND i.collection_id = ?"; params.push(scope.collectionId); }
    if (scope?.excludedIds.length) { scopeClause += ` AND i.id NOT IN (${scope.excludedIds.map(() => "?").join(",")})`; params.push(...scope.excludedIds); }
    let pageClause = "";
    if (limit !== undefined) {
      pageClause = " LIMIT ?";
      params.push(limit);
    }
    const rows = this.db.all(
      `SELECT e.rowid AS rowid, e.item_id, e.chunk_index, e.dims, e.vector
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id AND i.deleted_at IS NULL
       WHERE e.model = ? AND e.rowid > ?${scopeClause}
       ORDER BY e.rowid${pageClause}`,
      ...params,
    ) as Array<Omit<ChunkRow, "chunk_text" | "title"> & { rowid: number }>;
    return rows.map((row) => ({
      itemId: row.item_id,
      chunkIndex: row.chunk_index,
      vector: blobToVector(row.vector, row.dims),
      rowid: row.rowid,
    }));
  }

  /** 取指定分块的展示文本（检索完成后只对 top-k 调用） */
  loadChunkSnippets(
    keys: Array<{ itemId: string; chunkIndex: number }>,
  ): SemanticChunkSnippet[] {
    if (keys.length === 0) {
      return [];
    }
    const placeholders = keys.map(() => "(?, ?)").join(", ");
    const params = keys.flatMap((key) => [key.itemId, key.chunkIndex]);
    const rows = this.db.all(
      `SELECT e.item_id, e.chunk_index, e.chunk_text, i.title AS title
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id
       WHERE (e.item_id, e.chunk_index) IN (VALUES ${placeholders})`,
      ...params,
    ) as Array<Pick<ChunkRow, "item_id" | "chunk_index" | "chunk_text" | "title">>;
    return rows.map((row) => ({
      itemId: row.item_id,
      chunkIndex: row.chunk_index,
      chunkText: row.chunk_text,
      title: row.title,
    }));
  }

  stats(): { indexedItems: number; totalChunks: number } {
    const row = this.db.get(
      `SELECT COUNT(DISTINCT e.item_id) AS items, COUNT(*) AS chunks
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id AND i.deleted_at IS NULL`,
    ) as { items: number | null; chunks: number | null } | undefined;
    return {
      indexedItems: row?.items ?? 0,
      totalChunks: row?.chunks ?? 0,
    };
  }

  /** ANN 侧车的代际指纹；新增、替换、删除任一分块都会变化。 */
  generation(model: string): string {
    const row = this.db.get(
      `SELECT COUNT(*) AS count, COALESCE(MAX(updated_at),0) AS updated,
              COALESCE(SUM(rowid),0) AS rowids, COALESCE(MAX(dims),0) AS dims
       FROM knowledge_embeddings WHERE model=?`,
      model,
    ) as { count: number; updated: number; rowids: number; dims: number } | undefined;
    return `${row?.dims ?? 0}-${row?.count ?? 0}-${row?.updated ?? 0}-${row?.rowids ?? 0}`;
  }

  clearAll(): number {
    return this.db.run("DELETE FROM knowledge_embeddings").changes;
  }
}
