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

  /** 加载指定模型的全部分块向量（排除回收站条目） */
  loadChunksForSearch(model: string): SemanticChunkRecord[] {
    const rows = this.db.all(
      `SELECT e.item_id, e.chunk_index, e.chunk_text, e.dims, e.vector,
              i.title AS title
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id AND i.deleted_at IS NULL
       WHERE e.model = ?`,
      model,
    ) as ChunkRow[];
    return rows.map((row) => ({
      itemId: row.item_id,
      chunkIndex: row.chunk_index,
      chunkText: row.chunk_text,
      title: row.title,
      vector: blobToVector(row.vector, row.dims),
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

  clearAll(): number {
    return this.db.run("DELETE FROM knowledge_embeddings").changes;
  }
}
