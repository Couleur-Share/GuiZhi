import { describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_TABLES } from "@guizhi/db/schema";
import {
  addColumnIfMissing,
  getSchemaVersion,
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
