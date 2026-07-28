import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { CollectionDB } from "@guizhi/db/collection";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { DEFAULT_MCP_SCOPE, type McpScope } from "@guizhi/shared/utils/mcp-scope";
import { readItem, searchKnowledge } from "../../../src/mcp/tools";

const ALL_SCOPE: McpScope = { ...DEFAULT_MCP_SCOPE };

const TRANSCRIPT =
  "大家好，今天我们聊聊状态管理。先说结论，Redux 用得好好的就别动它。";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

let sourceSeq = 0;

function addSource(
  db: DatabaseAdapter.Database,
  itemId: string,
  platform: string,
  sourceUri: string,
): void {
  sourceSeq += 1;
  db.run(
    `INSERT INTO source_records
       (id, item_id, source_type, source_uri, platform, captured_at)
     VALUES (?, ?, 'url', ?, ?, ?)`,
    `src-${sourceSeq}`,
    itemId,
    sourceUri,
    platform,
    Date.now(),
  );
}

describe("MCP 工具", () => {
  let db: DatabaseAdapter.Database;
  let videoId: string;
  let noteId: string;

  beforeEach(() => {
    db = createTestDb();
    const frontend = new CollectionDB(db).create({ name: "前端" });
    const items = new KnowledgeItemDB(db);

    const video = items.create({
      title: "用 Zustand 替换 Redux 的三个前提",
      content: [
        "> 平台：哔哩哔哩 · 作者：前端小册 · 时长：12:52",
        "",
        "## 视频总结",
        "",
        "本期讨论把 Redux 迁到 Zustand 的判断标准。",
      ].join("\n"),
      transcript: TRANSCRIPT,
      itemType: "video",
      collectionId: frontend.id,
      tagNames: ["Zustand"],
    });
    videoId = video.id;
    addSource(db, video.id, "bilibili", "https://www.bilibili.com/video/BV1xx");

    const note = items.create({
      title: "煮咖啡的水温",
      content: "手冲建议 92 度。",
      itemType: "note",
    });
    noteId = note.id;
  });

  describe("search_knowledge", () => {
    it("命中的条目带上 id、类型、平台与知识库", () => {
      const output = searchKnowledge(db, { query: "Zustand" }, ALL_SCOPE);

      expect(output).toContain(`id=${videoId}`);
      expect(output).toContain("type=video");
      expect(output).toContain("platform=bilibili");
      expect(output).toContain("collection=前端");
      expect(output).toContain("用 Zustand 替换 Redux 的三个前提");
      // 结果里要指出下一步该调什么，否则模型拿到 id 也不知道能干嘛
      expect(output).toContain("read_item");
    });

    it("中文词组走 recall 分词，不要求逐字连续", () => {
      // phrase 模式下这个串会被编译成一个要求逐字相邻的短语，必然零命中
      const output = searchKnowledge(db, { query: "Zustand 判断标准" }, ALL_SCOPE);
      expect(output).toContain(videoId);
    });

    it("按平台筛选", () => {
      expect(searchKnowledge(db, { query: "Zustand", platform: "bilibili" }, ALL_SCOPE)).toContain(
        videoId,
      );
      expect(searchKnowledge(db, { query: "Zustand", platform: "douyin" }, ALL_SCOPE)).toContain(
        "没有找到",
      );
    });

    it("按知识库名筛选；名字不存在时列出现有的", () => {
      expect(searchKnowledge(db, { query: "Zustand", collection: "前端" }, ALL_SCOPE)).toContain(
        videoId,
      );

      const missing = searchKnowledge(
        db,
        { query: "Zustand", collection: "不存在的库" },
        ALL_SCOPE,
      );
      expect(missing).toContain("没有名为「不存在的库」的知识库");
      expect(missing).toContain("前端");
    });

    it("零命中时给的是可行动的提示，不是空字符串", () => {
      const output = searchKnowledge(db, { query: "量子力学" }, ALL_SCOPE);
      expect(output).toContain("没有找到");
      expect(output.trim().length).toBeGreaterThan(10);
    });

    it("空关键词不当成「列出全库」", () => {
      expect(searchKnowledge(db, { query: "   " }, ALL_SCOPE)).toContain("请给出检索关键词");
    });

    it("limit 被夹在 1..50", () => {
      const output = searchKnowledge(db, { query: "的", limit: 999 }, ALL_SCOPE);
      expect(output).not.toContain("Error");
    });
  });

  describe("read_item", () => {
    it("返回 AI 交接稿：front matter、阅读须知、文字稿俱全", () => {
      const { text, found } = readItem(db, { id: videoId }, ALL_SCOPE);

      expect(found).toBe(true);
      expect(text).toContain('source: "https://www.bilibili.com/video/BV1xx"');
      expect(text).toContain('collection: "前端"');
      expect(text).toContain('platform: "哔哩哔哩"');
      expect(text).toContain("【阅读须知】");
      expect(text).toContain("dacker");
      expect(text).toContain("## 口播文字稿");
      expect(text).toContain(TRANSCRIPT);
    });

    it("includeFullText=false 时略去文字稿但留下说明", () => {
      const { text } = readItem(db, { id: videoId, includeFullText: false }, ALL_SCOPE);

      expect(text).not.toContain(TRANSCRIPT);
      expect(text).toContain("本次未包含");
    });

    it("没有转写稿的笔记不挂 ASR 免责声明", () => {
      const { text, found } = readItem(db, { id: noteId }, ALL_SCOPE);

      expect(found).toBe(true);
      expect(text).toContain("手冲建议 92 度。");
      expect(text).not.toContain("dacker");
    });

    it("查无此条走 found=false，并提示重新检索", () => {
      const { text, found } = readItem(db, { id: "does-not-exist" }, ALL_SCOPE);

      expect(found).toBe(false);
      expect(text).toContain("找不到");
      expect(text).toContain("search_knowledge");
    });

    it("空 id 不去查库", () => {
      expect(readItem(db, { id: "  " }, ALL_SCOPE).found).toBe(false);
    });
  });

  describe("可访问范围", () => {
    /** 只放行「前端」库；未分类不放行 */
    function frontendOnly(): McpScope {
      const frontend = new CollectionDB(db)
        .list()
        .find((collection) => collection.name === "前端");
      return {
        mode: "selected",
        allowedCollectionIds: [frontend!.id],
        allowUncategorized: false,
      };
    }

    it("范围外的条目搜不到", () => {
      // 「煮咖啡的水温」是未分类的，不在放行范围里
      const output = searchKnowledge(db, { query: "咖啡" }, frontendOnly());
      expect(output).toContain("没有找到");
      expect(output).not.toContain(noteId);
    });

    it("范围内的条目照常搜得到", () => {
      expect(searchKnowledge(db, { query: "Zustand" }, frontendOnly())).toContain(
        videoId,
      );
    });

    it("放行未分类后，未分类条目又可见", () => {
      const scope: McpScope = { ...frontendOnly(), allowUncategorized: true };
      expect(searchKnowledge(db, { query: "咖啡" }, scope)).toContain(noteId);
    });

    it("范围外的知识库名字不出现在「现有知识库」提示里", () => {
      const scope: McpScope = {
        mode: "selected",
        allowedCollectionIds: [],
        allowUncategorized: true,
      };
      expect(
        searchKnowledge(db, { query: "Zustand", collection: "前端" }, scope),
      ).toContain("没有名为「前端」的知识库");
    });

    it("范围外的条目 read_item 明说在范围外，不伪装成不存在", () => {
      const { text, found } = readItem(db, { id: noteId }, frontendOnly());

      expect(found).toBe(false);
      expect(text).toContain("不在归知向 MCP 开放的范围内");
      expect(text).not.toContain("可能已被删除");
    });

    it("一个都没放行时直接说清楚，而不是回「没有找到」", () => {
      const empty: McpScope = {
        mode: "selected",
        allowedCollectionIds: [],
        allowUncategorized: false,
      };
      expect(searchKnowledge(db, { query: "Zustand" }, empty)).toContain(
        "没有向 MCP 开放任何知识库",
      );
    });
  });
});
