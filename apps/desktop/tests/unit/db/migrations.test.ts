import { describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_TABLES } from "@guizhi/db/schema";
import {
  addColumnIfMissing,
  getSchemaVersion,
  getTableDefinition,
  hasColumn,
  runMigrations,
  MIGRATIONS,
  SCHEMA_VERSION,
} from "@guizhi/db/migrations";

function createDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.exec(SCHEMA_TABLES);
  return db;
}

/** v0.6.0 之前的 knowledge_items：item_type 的 CHECK 里没有 'forum' */
const LEGACY_KNOWLEDGE_ITEMS = `
  CREATE TABLE knowledge_items (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    summary TEXT,
    transcript TEXT,
    item_type TEXT NOT NULL DEFAULT 'note'
      CHECK(item_type IN ('note','webpage','video','image','audio','document','snippet')),
    status TEXT NOT NULL DEFAULT 'inbox'
      CHECK(status IN ('inbox','ready','archived')),
    collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL,
    is_favorite INTEGER NOT NULL DEFAULT 0,
    is_pinned INTEGER NOT NULL DEFAULT 0,
    deleted_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
  )
`;

/**
 * 造一个「老版本 + 有数据」的库：条目本身，以及五张 ON DELETE CASCADE 子表
 * 各一行。重建 knowledge_items 时若外键没关掉，这些行会被隐式 DELETE 清空。
 */
function createLegacyDbWithData(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  // 先建全量结构，再把 knowledge_items 换成老定义
  db.exec(SCHEMA_TABLES);
  db.exec("DROP TABLE knowledge_items");
  db.exec(LEGACY_KNOWLEDGE_ITEMS);

  const now = Date.now();
  db.run(
    "INSERT INTO knowledge_items (id, title, content, item_type, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
    "item-1",
    "已有条目",
    "正文",
    "webpage",
    "ready",
    now,
    now,
  );
  db.run(
    "INSERT INTO tags (id, name, color_key, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
    "tag-1",
    "网络",
    "blue",
    now,
    now,
  );
  db.run(
    "INSERT INTO knowledge_item_tags (item_id, tag_id) VALUES (?, ?)",
    "item-1",
    "tag-1",
  );
  db.run(
    "INSERT INTO source_records (id, item_id, source_type, source_uri, captured_at) VALUES (?, ?, ?, ?, ?)",
    "src-1",
    "item-1",
    "url",
    "https://example.com/a",
    now,
  );
  db.run(
    "INSERT INTO wiki_pages (id, title, normalized_title, generated_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
    "page-1",
    "内网穿透",
    "内网穿透",
    now,
    now,
    now,
  );
  db.run(
    "INSERT INTO wiki_page_sources (page_id, item_id, created_at) VALUES (?, ?, ?)",
    "page-1",
    "item-1",
    now,
  );
  db.run(
    "INSERT INTO wiki_ingestions (item_id, content_hash, updated_at) VALUES (?, ?, ?)",
    "item-1",
    "hash-1",
    now,
  );
  const vector = new Float32Array([1, 0, 0]);
  db.run(
    "INSERT INTO knowledge_embeddings (item_id, chunk_index, chunk_text, content_hash, model, dims, vector, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
    "item-1",
    0,
    "正文",
    "hash-1",
    "text-embedding-3-small",
    3,
    new Uint8Array(vector.buffer).slice(),
    now,
  );
  return db;
}

function countRows(db: DatabaseAdapter.Database, table: string): number {
  const row = db.get(`SELECT COUNT(*) AS n FROM ${table}`) as { n: number };
  return row.n;
}

describe("schema 迁移执行器", () => {
  it("首次运行应用全部迁移并写入版本戳", () => {
    const db = createDb();
    const executed = runMigrations(db);

    expect(executed).toEqual(MIGRATIONS.map((migration) => migration.name));
    expect(getSchemaVersion(db)).toBe(SCHEMA_VERSION);
    db.close();
  });

  it("重复运行不再执行已应用的迁移", () => {
    const db = createDb();
    runMigrations(db);
    expect(runMigrations(db)).toEqual([]);
    db.close();
  });

  it("只补做未应用的那部分", () => {
    const db = createDb();
    db.exec(`
      CREATE TABLE IF NOT EXISTS schema_migrations (
        name TEXT PRIMARY KEY,
        applied_at INTEGER NOT NULL
      )
    `);
    db.run(
      "INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)",
      MIGRATIONS[0].name,
      Date.now(),
    );

    const executed = runMigrations(db);
    expect(executed).not.toContain(MIGRATIONS[0].name);
    expect(executed).toHaveLength(MIGRATIONS.length - 1);
    db.close();
  });

  it("对已经有目标列的老库保持幂等", () => {
    // 老库可能早就直接改过 schema.ts，迁移不能因为列已存在而炸
    const db = createDb();
    expect(hasColumn(db, "wiki_ingestions", "failure_count")).toBe(true);
    expect(() => runMigrations(db)).not.toThrow();
    db.close();
  });
});

describe("0003-item-type-forum 重建表迁移", () => {
  it("放行 forum 类型，且既有条目与全部级联子表数据都不丢", () => {
    const db = createLegacyDbWithData();
    expect(getTableDefinition(db, "knowledge_items")).not.toContain("'forum'");
    expect(() =>
      db.run(
        "INSERT INTO knowledge_items (id, item_type, created_at, updated_at) VALUES ('x', 'forum', 1, 1)",
      ),
    ).toThrow();

    runMigrations(db);

    // 论坛条目现在能入库
    expect(getTableDefinition(db, "knowledge_items")).toContain("'forum'");
    db.run(
      "INSERT INTO knowledge_items (id, title, item_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
      "item-2",
      "V2EX 帖子",
      "forum",
      2,
      2,
    );

    // 原有条目与五张 ON DELETE CASCADE 子表逐一核对
    const item = db.get(
      "SELECT title, item_type, status FROM knowledge_items WHERE id = ?",
      "item-1",
    );
    expect(item).toEqual({
      title: "已有条目",
      item_type: "webpage",
      status: "ready",
    });
    expect(countRows(db, "knowledge_item_tags")).toBe(1);
    expect(countRows(db, "source_records")).toBe(1);
    expect(countRows(db, "wiki_page_sources")).toBe(1);
    expect(countRows(db, "wiki_ingestions")).toBe(1);
    expect(countRows(db, "knowledge_embeddings")).toBe(1);

    db.close();
  });

  it("重建后索引与外键强制都恢复原状", () => {
    const db = createLegacyDbWithData();
    runMigrations(db);

    const indexes = db
      .all(
        "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'knowledge_items'",
      )
      .map((row) => (row as { name: string }).name);
    for (const expected of [
      "idx_items_status",
      "idx_items_collection",
      "idx_items_updated",
      "idx_items_deleted",
      "idx_items_favorite",
    ]) {
      expect(indexes).toContain(expected);
    }

    // 迁移期间关掉的外键强制必须还回去，否则后续删除不再级联
    const pragma = db.pragma("foreign_keys") as { foreign_keys: number }[];
    expect(pragma[0].foreign_keys).toBe(1);
    db.run("DELETE FROM knowledge_items WHERE id = ?", "item-1");
    expect(countRows(db, "source_records")).toBe(0);

    db.close();
  });

  it("对已经放行 forum 的库保持幂等", () => {
    const db = createDb();
    expect(getTableDefinition(db, "knowledge_items")).toContain("'forum'");
    expect(() => runMigrations(db)).not.toThrow();
    expect(runMigrations(db)).toEqual([]);
    db.close();
  });
});

describe("addColumnIfMissing", () => {
  it("缺列才加，已存在时静默跳过", () => {
    const db = new DatabaseAdapter(":memory:");
    db.exec("CREATE TABLE demo (id TEXT PRIMARY KEY)");

    expect(hasColumn(db, "demo", "extra")).toBe(false);
    addColumnIfMissing(db, "demo", "extra", "INTEGER NOT NULL DEFAULT 0");
    expect(hasColumn(db, "demo", "extra")).toBe(true);

    expect(() =>
      addColumnIfMissing(db, "demo", "extra", "INTEGER NOT NULL DEFAULT 0"),
    ).not.toThrow();
    db.close();
  });

  it("老库缺列时迁移能真正补上", () => {
    const db = new DatabaseAdapter(":memory:");
    // 模拟 v0.4.1 之前的 wiki_ingestions（没有退避相关列）
    db.exec(`
      CREATE TABLE wiki_ingestions (
        item_id TEXT PRIMARY KEY,
        content_hash TEXT NOT NULL,
        model TEXT NOT NULL DEFAULT '',
        prompt_version TEXT NOT NULL DEFAULT '',
        updated_at INTEGER NOT NULL
      )
    `);
    expect(hasColumn(db, "wiki_ingestions", "failure_count")).toBe(false);

    runMigrations(db);

    expect(hasColumn(db, "wiki_ingestions", "failure_count")).toBe(true);
    expect(hasColumn(db, "wiki_ingestions", "next_attempt_at")).toBe(true);
    db.close();
  });
});
