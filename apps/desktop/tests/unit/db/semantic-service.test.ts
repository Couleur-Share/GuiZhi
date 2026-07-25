import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { SemanticIndexDB } from "@guizhi/db/semantic";
import {
  buildSemanticSourceText,
  getSemanticStatus,
  listPendingSemanticItems,
  searchSemanticByVector,
} from "../../../src/main/services/semantic";

const MODEL = "text-embedding-3-small";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("语义索引 pending 判定与状态", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;
  let semantic: SemanticIndexDB;

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
    semantic = new SemanticIndexDB(db);
  });

  function applyPending(itemId: string, contentHash: string, model = MODEL) {
    semantic.replaceItemChunks({
      itemId,
      contentHash,
      model,
      dims: 2,
      chunks: [{ text: "chunk", vector: new Float32Array([1, 0]) }],
    });
  }

  it("新条目待索引；落库（回传同一哈希）后不再 pending", () => {
    const item = items.create({ title: "笔记", content: "语义检索测试内容" });
    items.create({ title: "", content: "" }); // 空条目不算 eligible

    const pendingBefore = listPendingSemanticItems(db, MODEL, 10);
    expect(pendingBefore).toHaveLength(1);
    expect(pendingBefore[0].id).toBe(item.id);
    expect(pendingBefore[0].contentHash).toBeTruthy();

    applyPending(item.id, pendingBefore[0].contentHash);
    expect(listPendingSemanticItems(db, MODEL, 10)).toHaveLength(0);

    const status = getSemanticStatus(db, MODEL);
    expect(status).toEqual({
      indexedItems: 1,
      eligibleItems: 1,
      totalChunks: 1,
    });
  });

  it("内容修改或模型切换后重新 pending", () => {
    const item = items.create({ title: "笔记", content: "原始内容" });
    const [pending] = listPendingSemanticItems(db, MODEL, 10);
    applyPending(item.id, pending.contentHash);

    // 内容变化 → 哈希变化 → pending
    items.update(item.id, { content: "修改后的内容" });
    expect(listPendingSemanticItems(db, MODEL, 10)).toHaveLength(1);

    // 换模型 → 旧索引不可用 → pending
    const [updated] = listPendingSemanticItems(db, MODEL, 10);
    applyPending(item.id, updated.contentHash);
    expect(listPendingSemanticItems(db, "other-model", 10)).toHaveLength(1);
  });

  it("回收站条目不参与 pending 与 eligible", () => {
    const item = items.create({ title: "t", content: "c" });
    items.moveToTrash([item.id]);
    expect(listPendingSemanticItems(db, MODEL, 10)).toHaveLength(0);
    expect(getSemanticStatus(db, MODEL).eligibleItems).toBe(0);
  });

  it("buildSemanticSourceText 拼接标题/正文/转写", () => {
    expect(buildSemanticSourceText("标题", "正文", "转写")).toBe(
      "标题\n正文\n转写",
    );
    expect(buildSemanticSourceText("", "  ", null)).toBe("");
  });
});

describe("searchSemanticByVector（余弦 top-k）", () => {
  it("按点积倒序返回条目级最高分，维度不匹配的分块被跳过", () => {
    const db = createTestDb();
    const items = new KnowledgeItemDB(db);
    const semantic = new SemanticIndexDB(db);

    const close = items.create({ title: "相近条目", content: "a" });
    const far = items.create({ title: "远离条目", content: "b" });

    semantic.replaceItemChunks({
      itemId: close.id,
      contentHash: "h1",
      model: MODEL,
      dims: 2,
      chunks: [
        { text: "低分块", vector: new Float32Array([0, 1]) },
        { text: "高分块内容", vector: new Float32Array([0.98, 0.199]) },
      ],
    });
    semantic.replaceItemChunks({
      itemId: far.id,
      contentHash: "h2",
      model: MODEL,
      dims: 2,
      chunks: [{ text: "反向", vector: new Float32Array([-1, 0]) }],
    });

    const hits = searchSemanticByVector(db, MODEL, new Float32Array([1, 0]), 5);
    expect(hits).toHaveLength(2);
    expect(hits[0].itemId).toBe(close.id);
    expect(hits[0].snippet).toContain("高分块内容");
    expect(hits[0].score).toBeGreaterThan(0.9);
    expect(hits[1].score).toBeLessThan(0);

    // limit 生效
    expect(searchSemanticByVector(db, MODEL, new Float32Array([1, 0]), 1)).toHaveLength(1);
    // 维度不匹配 → 无命中
    expect(
      searchSemanticByVector(db, MODEL, new Float32Array([1, 0, 0]), 5),
    ).toHaveLength(0);
  });
});
