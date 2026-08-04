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
    // 复核字段是后续增量；从更早的重建表升级时也必须补上。
    expect(hasColumn(db, "knowledge_items", "review_status")).toBe(true);
    expect(hasColumn(db, "knowledge_items", "review_reasons")).toBe(true);
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
    // status 是 'active' 而非入库时的 'ready'：0008 在 0003 之后把三态折叠成两态
    expect(item).toEqual({
      title: "已有条目",
      item_type: "webpage",
      status: "active",
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

describe("0008-item-status-two-state 重建表迁移", () => {
  function statusOf(db: DatabaseAdapter.Database, id: string): string {
    return (
      db.get("SELECT status FROM knowledge_items WHERE id = ?", id) as {
        status: string;
      }
    ).status;
  }

  it("inbox 与 ready 一并折叠成 active，archived 原样保留", () => {
    const db = createLegacyDbWithData();
    for (const [id, status] of [
      ["item-inbox", "inbox"],
      ["item-archived", "archived"],
    ]) {
      db.run(
        "INSERT INTO knowledge_items (id, title, status, created_at, updated_at) VALUES (?, ?, ?, 1, 1)",
        id,
        id,
        status,
      );
    }

    runMigrations(db);

    // item-1 建库时是 ready
    expect(statusOf(db, "item-1")).toBe("active");
    expect(statusOf(db, "item-inbox")).toBe("active");
    expect(statusOf(db, "item-archived")).toBe("archived");
    db.close();
  });

  it("新 CHECK 拒绝旧状态值，且级联子表数据不丢", () => {
    const db = createLegacyDbWithData();
    runMigrations(db);

    expect(getTableDefinition(db, "knowledge_items")).toContain("'active'");
    expect(() =>
      db.run(
        "INSERT INTO knowledge_items (id, status, created_at, updated_at) VALUES ('x', 'inbox', 1, 1)",
      ),
    ).toThrow();

    // 0008 同样是「建新表→拷数据→删旧表」，外键没关就会连带清空这些子表
    expect(countRows(db, "knowledge_item_tags")).toBe(1);
    expect(countRows(db, "source_records")).toBe(1);
    expect(countRows(db, "wiki_page_sources")).toBe(1);
    expect(countRows(db, "wiki_ingestions")).toBe(1);
    expect(countRows(db, "knowledge_embeddings")).toBe(1);
    db.close();
  });

  it("对已经是两态的库保持幂等", () => {
    const db = createDb();
    expect(getTableDefinition(db, "knowledge_items")).toContain("'active'");
    expect(() => runMigrations(db)).not.toThrow();
    expect(runMigrations(db)).toEqual([]);
    db.close();
  });
});

describe("0009-source-platform 回填迁移", () => {
  function platformsById(
    db: DatabaseAdapter.Database,
  ): Record<string, string | null> {
    const rows = db.all("SELECT id, platform FROM source_records") as Array<{
      id: string;
      platform: string | null;
    }>;
    return Object.fromEntries(rows.map((row) => [row.id, row.platform]));
  }

  function addSource(
    db: DatabaseAdapter.Database,
    id: string,
    sourceType: string,
    sourceUri: string | null,
    platform: string | null = null,
  ): void {
    db.run(
      `INSERT INTO source_records
         (id, item_id, source_type, source_uri, platform, captured_at)
       VALUES (?, ?, ?, ?, ?, 1)`,
      id,
      "item-1",
      sourceType,
      sourceUri,
      platform,
    );
  }

  it("按来源链接补齐老库的 platform", () => {
    const db = createLegacyDbWithData();
    addSource(db, "src-douyin", "url", "https://www.douyin.com/video/741234");
    addSource(db, "src-v2ex", "url", "https://www.v2ex.com/t/1227616");
    addSource(db, "src-file", "file", "D:\\notes\\a.md");
    addSource(db, "src-text", "text", null);

    runMigrations(db);

    const platforms = platformsById(db);
    // src-1 是建库时写的 https://example.com/a
    expect(platforms["src-1"]).toBe("web");
    expect(platforms["src-douyin"]).toBe("douyin");
    expect(platforms["src-v2ex"]).toBe("v2ex");
    expect(platforms["src-file"]).toBe("local");
    expect(platforms["src-text"]).toBeNull();
    db.close();
  });

  it("旧版迁移留下的自定义取值被重算成统一词表", () => {
    const db = createLegacyDbWithData();
    addSource(
      db,
      "src-legacy",
      "url",
      "https://www.bilibili.com/video/BV1xx411c7mD",
      "Bilibili-Legacy",
    );

    runMigrations(db);

    expect(platformsById(db)["src-legacy"]).toBe("bilibili");
    db.close();
  });

  it("建出按平台查询用的索引", () => {
    const db = createDb();
    runMigrations(db);

    const indexes = db
      .all(
        "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'source_records'",
      )
      .map((row) => (row as { name: string }).name);
    expect(indexes).toContain("idx_sources_platform");
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

  it("0010：老库的 import_tasks 补出 warning 列，既有任务不受影响", () => {
    const db = new DatabaseAdapter(":memory:");
    // v0.11.0 之前的 import_tasks：只有 error，没有 warning
    db.exec(`
      CREATE TABLE import_tasks (
        id TEXT PRIMARY KEY,
        source_kind TEXT NOT NULL,
        source_input TEXT NOT NULL,
        display_name TEXT NOT NULL,
        status TEXT NOT NULL DEFAULT 'pending',
        stage TEXT,
        error TEXT,
        item_type TEXT,
        result_item_id TEXT,
        duplicate_item_id TEXT,
        collection_id TEXT,
        tag_names TEXT,
        force_duplicate INTEGER NOT NULL DEFAULT 0,
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL
      )
    `);
    db.run(
      "INSERT INTO import_tasks (id, source_kind, source_input, display_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
      "task-1",
      "url",
      "https://v.douyin.com/abc/",
      "老任务",
      "completed",
      1,
      1,
    );
    expect(hasColumn(db, "import_tasks", "warning")).toBe(false);

    runMigrations(db);

    expect(hasColumn(db, "import_tasks", "warning")).toBe(true);
    // 补列不改既有行：历史任务当时没记降级原因，只能是 NULL
    const row = db.get(
      "SELECT status, warning FROM import_tasks WHERE id = ?",
      "task-1",
    ) as { status: string; warning: string | null };
    expect(row.status).toBe("completed");
    expect(row.warning).toBeNull();
    db.close();
  });
});
