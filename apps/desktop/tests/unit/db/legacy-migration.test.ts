import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import {
  mapLegacyItemType,
  mapLegacySourceType,
  mapLegacyStatus,
  mapLegacyTagColor,
  migrateLegacyDatabase,
  parseLegacyUtc,
} from "@guizhi/db/legacy-migration";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { WikiDB } from "@guizhi/db/wiki";

/** 构造旧版（EF Core）schema 的模拟库并插入样本数据。 */
function createLegacyDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.exec(`
    CREATE TABLE Collections (Id TEXT PRIMARY KEY, Name TEXT, Description TEXT, Icon TEXT,
      SortOrder INTEGER, IsDefault INTEGER, CreatedAtUtc TEXT, UpdatedAtUtc TEXT,
      IsDeleted INTEGER, DeletedAtUtc TEXT);
    CREATE TABLE Tags (Id TEXT PRIMARY KEY, Name TEXT, NormalizedName TEXT, ColorKey INTEGER,
      CreatedAtUtc TEXT, UpdatedAtUtc TEXT);
    CREATE TABLE KnowledgeItems (Id TEXT PRIMARY KEY, Title TEXT, Summary TEXT,
      IsSummaryUserEdited INTEGER, Content TEXT, ContentFormat INTEGER, ItemType INTEGER,
      Status INTEGER, SourceId TEXT, CollectionId TEXT, IsFavorite INTEGER, IsPinned INTEGER,
      WordCount INTEGER, Language TEXT, CreatedAtUtc TEXT, UpdatedAtUtc TEXT,
      LastOpenedAtUtc TEXT, IsDeleted INTEGER, DeletedAtUtc TEXT, Transcript TEXT);
    CREATE TABLE KnowledgeItemTags (KnowledgeItemId TEXT, TagId TEXT, CreatedAtUtc TEXT);
    CREATE TABLE SourceRecords (Id TEXT PRIMARY KEY, SourceType INTEGER, OriginalUri TEXT,
      CanonicalUri TEXT, Platform TEXT, ExternalId TEXT, OriginalTitle TEXT, Author TEXT,
      PublishedAtUtc TEXT, CapturedAtUtc TEXT, RawMetadataJson TEXT, ContentHash TEXT,
      CreatedAtUtc TEXT, UpdatedAtUtc TEXT);
    CREATE TABLE WikiPages (Id TEXT PRIMARY KEY, Title TEXT, NormalizedTitle TEXT,
      Kind INTEGER, Summary TEXT, Body TEXT, AliasesJson TEXT, Provider TEXT, Model TEXT,
      PromptVersion TEXT, GeneratedAtUtc TEXT, CreatedAtUtc TEXT, UpdatedAtUtc TEXT);
    CREATE TABLE WikiPageLinks (FromPageId TEXT, ToPageId TEXT, CreatedAtUtc TEXT);
    CREATE TABLE WikiPageSources (WikiPageId TEXT, KnowledgeItemId TEXT, CreatedAtUtc TEXT);
    CREATE TABLE WikiIngestions (KnowledgeItemId TEXT PRIMARY KEY, ContentHash TEXT,
      Model TEXT, PromptVersion TEXT, UpdatedAtUtc TEXT);
  `);

  const T = "2026-07-19 16:35:19.7184038";
  db.run(
    "INSERT INTO Collections VALUES ('COL-1','技术','', 'folder', 1, 0, ?, ?, 0, NULL)",
    T,
    T,
  );
  db.run(
    "INSERT INTO Collections VALUES ('COL-DEL','已删集合','', NULL, 2, 0, ?, ?, 1, ?)",
    T,
    T,
    T,
  );
  db.run("INSERT INTO Tags VALUES ('TAG-1','前端','前端', 1, ?, ?)", T, T);
  db.run("INSERT INTO Tags VALUES ('TAG-2','性能','性能', 4, ?, ?)", T, T);

  // 条目：网页类型、Ready、含摘要与来源、属于集合
  db.run(
    `INSERT INTO KnowledgeItems VALUES
     ('ITEM-1','Web性能优化','- 要点摘要', 0, '正文内容 LCP INP', 1, 1, 2, 'SRC-1', 'COL-1',
      1, 0, 100, 'zh', ?, ?, NULL, 0, NULL, NULL)`,
    T,
    T,
  );
  // 条目：Processing 状态（应映射 inbox）、集合指向已删集合（应置空）
  db.run(
    `INSERT INTO KnowledgeItems VALUES
     ('ITEM-2','处理中的笔记', NULL, 0, '内容乙', 0, 0, 1, NULL, 'COL-DEL',
      0, 1, 10, 'zh', ?, ?, NULL, 0, NULL, '转写稿')`,
    T,
    T,
  );
  // 条目：软删除（回收站）
  db.run(
    `INSERT INTO KnowledgeItems VALUES
     ('ITEM-3','回收站条目', NULL, 0, '已删内容', 0, 0, 2, NULL, NULL,
      0, 0, 5, 'zh', ?, ?, NULL, 1, ?, NULL)`,
    T,
    T,
    T,
  );
  db.run("INSERT INTO KnowledgeItemTags VALUES ('ITEM-1','TAG-1', ?)", T);
  db.run("INSERT INTO KnowledgeItemTags VALUES ('ITEM-1','TAG-2', ?)", T);
  db.run(
    "INSERT INTO KnowledgeItemTags VALUES ('ITEM-1','TAG-MISSING', ?)",
    T,
  );
  db.run(
    `INSERT INTO SourceRecords VALUES
     ('SRC-1', 3, 'https://example.com/a?utm_source=x', 'https://example.com/a', 'web',
      NULL, '原标题', NULL, NULL, ?, NULL, 'hash-abc', ?, ?)`,
    T,
    T,
    T,
  );

  db.run(
    `INSERT INTO WikiPages VALUES
     ('WIKI-1','Web 性能','WEB 性能', 0, '性能主题页', '正文 [[核心指标]]', NULL,
      'OpenAICompatible','gpt-test','wiki-compile-v1', ?, ?, ?)`,
    T,
    T,
    T,
  );
  db.run(
    `INSERT INTO WikiPages VALUES
     ('WIKI-2','核心指标','核心指标', 2, '概念页', '指标正文', '["CWV"]',
      'OpenAICompatible','gpt-test','wiki-compile-v1', ?, ?, ?)`,
    T,
    T,
    T,
  );
  db.run("INSERT INTO WikiPageLinks VALUES ('WIKI-1','WIKI-2', ?)", T);
  db.run("INSERT INTO WikiPageSources VALUES ('WIKI-1','ITEM-1', ?)", T);
  db.run(
    "INSERT INTO WikiIngestions VALUES ('ITEM-1','legacy-hash','gpt-test','wiki-compile-v1', ?)",
    T,
  );

  return db;
}

function createTargetDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("枚举与时间映射", () => {
  it("parseLegacyUtc 解析 EF 时间文本为 UTC 毫秒", () => {
    const ms = parseLegacyUtc("2026-07-19 16:35:19.7184038");
    expect(new Date(ms).toISOString()).toBe("2026-07-19T16:35:19.718Z");
  });

  it("枚举映射", () => {
    expect(mapLegacyItemType(1)).toBe("webpage");
    expect(mapLegacyItemType(99)).toBe("note");
    expect(mapLegacyStatus(3)).toBe("archived");
    // 旧版那几种「没处理完」的状态在两态模型里都是活跃
    expect(mapLegacyStatus(2)).toBe("active");
    expect(mapLegacyStatus(1)).toBe("active");
    expect(mapLegacyStatus(4)).toBe("active");
    expect(mapLegacyStatus(0)).toBe("active");
    expect(mapLegacyTagColor(1)).toBe("blue");
    expect(mapLegacyTagColor(9)).toBe("gray");
    expect(mapLegacySourceType(3)).toBe("url");
    expect(mapLegacySourceType(2)).toBe("file");
    expect(mapLegacySourceType(0)).toBe("text");
  });
});

describe("migrateLegacyDatabase", () => {
  let legacy: DatabaseAdapter.Database;
  let target: DatabaseAdapter.Database;

  beforeEach(() => {
    legacy = createLegacyDb();
    target = createTargetDb();
  });

  it("全量迁移：统计与内容正确", () => {
    const stats = migrateLegacyDatabase(target, legacy);
    expect(stats).toMatchObject({
      collections: 1, // 软删集合被跳过
      tags: 2,
      items: 3,
      itemTags: 2, // 缺失标签的关联被跳过
      sources: 1,
      wikiPages: 2,
      wikiLinks: 1,
      wikiSources: 1,
      wikiIngestions: 1,
    });

    const items = new KnowledgeItemDB(target);
    const item1 = items.get("ITEM-1")!;
    expect(item1.itemType).toBe("webpage");
    expect(item1.status).toBe("active");
    expect(item1.summary).toBe("- 要点摘要");
    expect(item1.isFavorite).toBe(true);
    expect(item1.collectionId).toBe("COL-1");
    expect(item1.tags.map((tag) => tag.name).sort()).toEqual(["前端", "性能"]);
    expect(item1.createdAt).toBe(Date.parse("2026-07-19T16:35:19.718Z"));

    // Processing → active；已删集合引用置空；转写稿保留
    const item2 = items.get("ITEM-2")!;
    expect(item2.status).toBe("active");
    expect(item2.collectionId).toBeNull();
    expect(item2.transcript).toBe("转写稿");

    // 软删条目在回收站
    const item3 = items.get("ITEM-3")!;
    expect(item3.deletedAt).not.toBeNull();
  });

  it("迁移后 FTS 可检索（回收站条目除外）", () => {
    migrateLegacyDatabase(target, legacy);
    const items = new KnowledgeItemDB(target);

    const hits = items.list({ scope: "all", search: "性能" });
    expect(hits.entries.map((entry) => entry.id)).toContain("ITEM-1");

    const trashedHits = items.list({ scope: "all", search: "已删内容" });
    expect(trashedHits.entries).toHaveLength(0);
  });

  it("Wiki 页面、链接、来源与指纹完整迁移", () => {
    migrateLegacyDatabase(target, legacy);
    const wiki = new WikiDB(target);

    const catalog = wiki.getCatalog();
    expect(catalog).toHaveLength(2);

    const conceptPage = catalog.find((entry) => entry.id === "WIKI-2")!;
    expect(conceptPage.kind).toBe("concept");
    expect(conceptPage.aliasesJson).toBe('["CWV"]');
    expect(wiki.getPage("WIKI-2")!.backlinks.map((entry) => entry.id)).toEqual([
      "WIKI-1",
    ]);
    expect(wiki.getPage("WIKI-1")!.sources[0].itemId).toBe("ITEM-1");

    const ingestions = wiki.listIngestions();
    expect(ingestions[0]).toMatchObject({
      itemId: "ITEM-1",
      contentHash: "legacy-hash",
      promptVersion: "wiki-compile-v1",
    });
  });

  it("来源记录字段映射（URL 类型 + 规范化 URI + 哈希）", () => {
    migrateLegacyDatabase(target, legacy);
    const source = target.get(
      "SELECT * FROM source_records WHERE item_id = 'ITEM-1'",
    ) as Record<string, unknown>;
    expect(source.source_type).toBe("url");
    expect(source.source_uri).toBe("https://example.com/a?utm_source=x");
    expect(source.normalized_uri).toBe("https://example.com/a");
    expect(source.content_hash).toBe("hash-abc");
  });

  it("目标库非空时拒绝迁移", () => {
    const items = new KnowledgeItemDB(target);
    items.create({ title: "已有数据", content: "x" });
    expect(() => migrateLegacyDatabase(target, legacy)).toThrow(/不是空的/);
  });

  it("源库不是旧版归知时报错", () => {
    const bogus = new DatabaseAdapter(":memory:");
    bogus.exec("CREATE TABLE foo (id TEXT)");
    expect(() => migrateLegacyDatabase(target, bogus)).toThrow(
      /KnowledgeItems/,
    );
  });
});
