import { afterEach, beforeEach, describe, expect, it } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { MIGRATIONS, hasColumn } from "@guizhi/db/migrations";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { ImportTaskDB } from "@guizhi/db/import-task";
import { SourceAccessDB } from "@guizhi/db/source-access";

const canonical = "https://www.xiaohongshu.com/explore/123";
const original = "https://xhslink.cn/o/original";
const migration = MIGRATIONS.find((m) => m.name === "0028-source-access-uri")!;

describe("来源访问入口迁移", () => {
  let db: Database.Database;
  let items: KnowledgeItemDB;
  let tasks: ImportTaskDB;
  beforeEach(() => {
    db = new Database(":memory:");
    db.pragma("foreign_keys = ON");
    db.exec(SCHEMA_TABLES.replace("  access_uri TEXT,", ""));
    db.exec(SCHEMA_INDEXES);
    items = new KnowledgeItemDB(db);
    tasks = new ImportTaskDB(db);
  });
  afterEach(() => db.close());

  function source(itemId: string, id = "source-1", uri = canonical) {
    db.run(
      "INSERT INTO source_records (id,item_id,source_type,source_uri,normalized_uri,captured_at) VALUES (?,?,'url',?,?,1)",
      id,
      itemId,
      uri,
      uri,
    );
  }
  function task(itemId: string, input = original, status = "completed") {
    const created = tasks.create({ kind: "url", input });
    db.run(
      "UPDATE import_tasks SET result_item_id=?, status=? WHERE id=?",
      itemId,
      status,
      created.id,
    );
    return created.id;
  }

  it("旧库回填最近一次成功导入的链接，清理任务后仍可用", () => {
    const item = items.create({
      title: "旧视频",
      content: "原文",
      transcript: "原文字稿",
      itemType: "video",
    });
    source(item.id);
    const oldTask = task(item.id, "https://xhslink.cn/o/old");
    db.run("UPDATE import_tasks SET updated_at=1 WHERE id=?", oldTask);
    task(item.id);
    task(item.id, "https://xhslink.cn/o/failed", "failed");
    migration.up(db);
    expect(hasColumn(db, "source_records", "access_uri")).toBe(true);
    expect(new SourceAccessDB(db).get(item.id, canonical)).toBe(original);
    tasks.clearTerminal({ scope: "all" });
    expect(new SourceAccessDB(db).get(item.id, canonical)).toBe(original);
    expect(items.get(item.id)?.sourceUri).toBe(canonical);
    expect(items.get(item.id)?.transcript).toBe("原文字稿");
  });

  it("迁移幂等，后续保存的新链接不会被历史任务覆盖", () => {
    const item = items.create({ title: "视频" });
    source(item.id);
    task(item.id);
    migration.up(db);
    const access = new SourceAccessDB(db);
    access.remember(item.id, canonical, "https://xhslink.cn/o/new");
    migration.up(db);
    expect(access.get(item.id, canonical)).toBe("https://xhslink.cn/o/new");
  });

  it("历史任务已清理时保持空值，仍保留规范来源", () => {
    const item = items.create({ title: "无历史" });
    source(item.id);
    migration.up(db);
    expect(new SourceAccessDB(db).get(item.id, canonical)).toBeNull();
    expect(items.get(item.id)?.sourceUri).toBe(canonical);
  });

  it("多来源旧条目不猜访问入口，避免把同一链接回填给不同来源", () => {
    const item = items.create({ title: "多来源" });
    source(item.id);
    source(item.id, "source-2", "https://www.xiaohongshu.com/explore/456");
    task(item.id);
    migration.up(db);
    expect(db.all("SELECT access_uri FROM source_records")).toEqual([
      { access_uri: null },
      { access_uri: null },
    ]);
  });

  it("更新只作用于对应规范来源，内容相同的另一网站不受影响", () => {
    const item = items.create({ title: "多来源" });
    source(item.id);
    source(item.id, "source-2", "https://example.com/video");
    migration.up(db);
    const access = new SourceAccessDB(db);
    access.remember(item.id, canonical, original);
    expect(access.get(item.id, canonical)).toBe(original);
    expect(access.get(item.id, "https://example.com/video")).toBeNull();
    expect(
      db.all("SELECT source_uri, normalized_uri FROM source_records"),
    ).toEqual([
      { source_uri: canonical, normalized_uri: canonical },
      {
        source_uri: "https://example.com/video",
        normalized_uri: "https://example.com/video",
      },
    ]);
  });
});
