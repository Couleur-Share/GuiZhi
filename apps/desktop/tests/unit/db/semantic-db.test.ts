import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { SemanticIndexDB, blobToVector, vectorToBlob } from "@guizhi/db/semantic";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("vector <-> blob 编解码", () => {
  it("Float32Array 往返无损", () => {
    const vector = new Float32Array([0.25, -1.5, 3.75, 0]);
    const restored = blobToVector(vectorToBlob(vector), 4);
    expect(Array.from(restored)).toEqual([0.25, -1.5, 3.75, 0]);
  });
});

describe("SemanticIndexDB", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;
  let semantic: SemanticIndexDB;

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
    semantic = new SemanticIndexDB(db);
  });

  function indexItem(
    itemId: string,
    vectors: number[][],
    model = "text-embedding-3-small",
  ): void {
    semantic.replaceItemChunks({
      itemId,
      contentHash: `hash-${itemId}`,
      model,
      dims: vectors[0].length,
      chunks: vectors.map((values, index) => ({
        text: `chunk-${index}`,
        vector: new Float32Array(values),
      })),
    });
  }

  it("BLOB 向量经 SQLite 存取后无损（核心可行性验证）", () => {
    const item = items.create({ title: "向量条目", content: "内容" });
    indexItem(item.id, [[0.6, 0.8], [1, 0]]);

    const chunks = semantic.loadChunksForSearch("text-embedding-3-small");
    expect(chunks).toHaveLength(2);
    const first = chunks.find((chunk) => chunk.chunkIndex === 0)!;
    expect(Array.from(first.vector)).toEqual([
      expect.closeTo(0.6, 5),
      expect.closeTo(0.8, 5),
    ]);
    expect(first.title).toBe("向量条目");
    expect(first.chunkText).toBe("chunk-0");
  });

  it("replaceItemChunks 是整条目替换", () => {
    const item = items.create({ title: "t", content: "c" });
    indexItem(item.id, [[1, 0], [0, 1], [1, 1]]);
    indexItem(item.id, [[0.5, 0.5]]);

    expect(semantic.stats().totalChunks).toBe(1);
    expect(semantic.listItemStates().get(item.id)?.contentHash).toBe(
      `hash-${item.id}`,
    );
  });

  it("回收站条目不参与检索，恢复后重新可见，彻底删除级联清理", () => {
    const item = items.create({ title: "t", content: "c" });
    indexItem(item.id, [[1, 0]]);

    items.moveToTrash([item.id]);
    expect(semantic.loadChunksForSearch("text-embedding-3-small")).toHaveLength(0);
    expect(semantic.stats().indexedItems).toBe(0);

    items.restore([item.id]);
    expect(semantic.loadChunksForSearch("text-embedding-3-small")).toHaveLength(1);

    items.deleteForever([item.id]);
    const orphan = db.get(
      "SELECT COUNT(*) AS count FROM knowledge_embeddings",
    ) as { count: number };
    expect(orphan.count).toBe(0);
  });

  it("检索按模型过滤（换模型后旧向量不可比）", () => {
    const item = items.create({ title: "t", content: "c" });
    indexItem(item.id, [[1, 0]], "model-a");
    expect(semantic.loadChunksForSearch("model-b")).toHaveLength(0);
    expect(semantic.loadChunksForSearch("model-a")).toHaveLength(1);
  });
});
