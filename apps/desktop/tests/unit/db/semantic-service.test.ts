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

  it("判定不再全库加载正文：未变更的条目一条都不读", () => {
    const item = items.create({ title: "笔记", content: "内容" });
    const [pending] = listPendingSemanticItems(db, MODEL, 10);
    applyPending(item.id, pending.contentHash);

    // 候选筛选在 SQL 里完成，索引是最新的就不该再回表取正文
    const reads: string[] = [];
    const originalGet = db.get.bind(db);
    db.get = ((sql: string, ...params: unknown[]) => {
      reads.push(sql);
      return originalGet(sql, ...params);
    }) as typeof db.get;

    expect(listPendingSemanticItems(db, MODEL, 10)).toHaveLength(0);
    expect(reads.some((sql) => sql.includes("content"))).toBe(false);

    db.get = originalGet;
  });

  it("只改收藏标记（正文没动）不会触发重新嵌入", () => {
    const item = items.create({ title: "笔记", content: "内容" });
    const [pending] = listPendingSemanticItems(db, MODEL, 10);
    applyPending(item.id, pending.contentHash);

    // updated_at 会被推进，但哈希不变——SQL 放行、哈希拦下
    items.update(item.id, { isFavorite: true });
    expect(listPendingSemanticItems(db, MODEL, 10)).toHaveLength(0);
  });

  it("buildSemanticSourceText 拼接标题/正文/转写", () => {
    expect(buildSemanticSourceText("标题", "正文", "转写")).toBe(
      "标题\n正文\n转写",
    );
    expect(buildSemanticSourceText("", "  ", null)).toBe("");
  });
});

describe("searchSemanticByVector（余弦 top-k）", () => {
  it("按点积倒序返回条目级最高分，维度不匹配的分块被跳过", async () => {
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

    const hits = await searchSemanticByVector(
      db,
      MODEL,
      new Float32Array([1, 0]),
      5,
    );
    expect(hits).toHaveLength(2);
    expect(hits[0].itemId).toBe(close.id);
    expect(hits[0].snippet).toContain("高分块内容");
    expect(hits[0].score).toBeGreaterThan(0.9);
    expect(hits[1].score).toBeLessThan(0);

    // limit 生效
    expect(
      await searchSemanticByVector(db, MODEL, new Float32Array([1, 0]), 1),
    ).toHaveLength(1);
    // 维度不匹配 → 无命中
    expect(
      await searchSemanticByVector(db, MODEL, new Float32Array([1, 0, 0]), 5),
    ).toHaveLength(0);
  });

  it("打分阶段不搬正文，只对 top-k 取展示文本", async () => {
    const db = createTestDb();
    const items = new KnowledgeItemDB(db);
    const semantic = new SemanticIndexDB(db);

    for (let index = 0; index < 5; index++) {
      const item = items.create({ title: `条目 ${index}`, content: "x" });
      semantic.replaceItemChunks({
        itemId: item.id,
        contentHash: `h-${index}`,
        model: MODEL,
        dims: 2,
        chunks: [
          { text: `很长的分块正文 ${index}`, vector: new Float32Array([1, 0]) },
          { text: `另一块 ${index}`, vector: new Float32Array([0, 1]) },
        ],
      });
    }

    const queries: string[] = [];
    const originalAll = db.all.bind(db);
    db.all = ((sql: string, ...params: unknown[]) => {
      queries.push(sql);
      return originalAll(sql, ...params);
    }) as typeof db.all;

    const hits = await searchSemanticByVector(
      db,
      MODEL,
      new Float32Array([1, 0]),
      2,
    );
    db.all = originalAll;

    // 打分那条查询不该带上 chunk_text（10 个分块全搬一遍只为最后用 2 条）
    const scoringQueries = queries.filter((sql) => sql.includes("e.vector"));
    expect(scoringQueries.length).toBeGreaterThan(0);
    for (const sql of scoringQueries) {
      expect(sql).not.toContain("chunk_text");
    }

    // 展示文本仍然正确落到结果里
    expect(hits).toHaveLength(2);
    expect(hits[0].snippet).toContain("很长的分块正文");
    expect(hits[0].title).toMatch(/^条目 /);
  });

  it("分块数超过单批上限时跨批次结果一致", async () => {
    const db = createTestDb();
    const items = new KnowledgeItemDB(db);
    const semantic = new SemanticIndexDB(db);

    // 600 个条目各一块，跨过 500 的批次边界；最后一个条目最贴近查询向量
    for (let index = 0; index < 600; index++) {
      const item = items.create({ title: `条目 ${index}`, content: "x" });
      const angle = (Math.PI / 2) * (1 - index / 600);
      semantic.replaceItemChunks({
        itemId: item.id,
        contentHash: `h-${index}`,
        model: MODEL,
        dims: 2,
        chunks: [
          {
            text: `分块 ${index}`,
            vector: new Float32Array([Math.cos(angle), Math.sin(angle)]),
          },
        ],
      });
    }

    const hits = await searchSemanticByVector(
      db,
      MODEL,
      new Float32Array([1, 0]),
      3,
    );
    expect(hits).toHaveLength(3);
    expect(hits[0].snippet).toContain("分块 599");
    // 分数单调递减，说明跨批次的 top-k 归并没有丢结果
    expect(hits[0].score).toBeGreaterThan(hits[1].score);
    expect(hits[1].score).toBeGreaterThan(hits[2].score);
  });

  it("分批遍历走 rowid 游标，不用 OFFSET", async () => {
    const db = createTestDb();
    const items = new KnowledgeItemDB(db);
    const semantic = new SemanticIndexDB(db);

    for (let index = 0; index < 20; index++) {
      const item = items.create({ title: `条目 ${index}`, content: "x" });
      semantic.replaceItemChunks({
        itemId: item.id,
        contentHash: `h-${index}`,
        model: MODEL,
        dims: 2,
        chunks: [{ text: `块 ${index}`, vector: new Float32Array([1, 0]) }],
      });
    }

    // OFFSET 要求 SQLite 扫过并丢弃前 N 行，分批遍历全表即成平方级：
    // 实测 6 万分块下偏移翻页 15.1s、游标翻页 1.1s
    const queries: string[] = [];
    const originalAll = db.all.bind(db);
    db.all = ((sql: string, ...params: unknown[]) => {
      queries.push(sql);
      return originalAll(sql, ...params);
    }) as typeof db.all;
    await searchSemanticByVector(db, MODEL, new Float32Array([1, 0]), 3);
    db.all = originalAll;

    const scoringQueries = queries.filter((sql) => sql.includes("e.vector"));
    expect(scoringQueries.length).toBeGreaterThan(0);
    for (const sql of scoringQueries) {
      expect(sql).not.toMatch(/\bOFFSET\b/i);
      expect(sql).toContain("e.rowid > ?");
    }

    // 游标翻页要不重不漏地覆盖全表
    const seen: string[] = [];
    let cursor = 0;
    for (;;) {
      const batch = semantic.loadVectorsForSearch(MODEL, 7, cursor);
      if (batch.length === 0) {
        break;
      }
      seen.push(...batch.map((row) => row.itemId));
      cursor = batch[batch.length - 1].rowid;
    }
    expect(seen).toHaveLength(20);
    expect(new Set(seen).size).toBe(20);
  });
});
