import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { ImportTaskDB } from "@guizhi/db/import-task";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("ImportTaskDB", () => {
  let db: DatabaseAdapter.Database;
  let tasks: ImportTaskDB;

  beforeEach(() => {
    db = createTestDb();
    tasks = new ImportTaskDB(db);
  });

  it("创建任务：文本取首行做显示名", () => {
    const task = tasks.create({
      kind: "text",
      input: "第一行标题\n第二行内容",
    });
    expect(task.status).toBe("pending");
    expect(task.displayName).toBe("第一行标题");
    expect(task.sourceKind).toBe("text");
  });

  it("创建任务：文件取文件名做显示名", () => {
    const task = tasks.create({
      kind: "file",
      input: "C:\\Users\\me\\docs\\读书笔记.md",
    });
    expect(task.displayName).toBe("读书笔记.md");
  });

  it("更新状态与去重结果", () => {
    const task = tasks.create({ kind: "url", input: "https://example.com" });
    const updated = tasks.update(task.id, {
      status: "duplicate",
      duplicateItemId: "item-1",
    })!;
    expect(updated.status).toBe("duplicate");
    expect(updated.duplicateItemId).toBe("item-1");
  });

  it("forceDuplicate 标志读写", () => {
    const task = tasks.create({
      kind: "text",
      input: "内容",
      forceDuplicate: true,
    });
    expect(tasks.isForceDuplicate(task.id)).toBe(true);

    tasks.update(task.id, { forceDuplicate: false });
    expect(tasks.isForceDuplicate(task.id)).toBe(false);
  });

  it("resetProcessingToPending 只复位 processing", () => {
    const running = tasks.create({ kind: "text", input: "运行中" });
    const done = tasks.create({ kind: "text", input: "已完成" });
    tasks.update(running.id, { status: "processing", stage: "fetching" });
    tasks.update(done.id, { status: "completed" });

    const changed = tasks.resetProcessingToPending();
    expect(changed).toBe(1);
    expect(tasks.get(running.id)!.status).toBe("pending");
    expect(tasks.get(running.id)!.stage).toBeNull();
    expect(tasks.get(done.id)!.status).toBe("completed");
  });

  it("clearFinished 清掉终态任务，保留进行中的与失败的", () => {
    const pending = tasks.create({ kind: "text", input: "待处理" });
    const failed = tasks.create({ kind: "text", input: "失败" });
    const duplicate = tasks.create({ kind: "text", input: "重复" });
    const completed = tasks.create({ kind: "text", input: "已完成" });
    tasks.update(failed.id, { status: "failed", error: "err" });
    tasks.update(duplicate.id, { status: "duplicate" });
    tasks.update(completed.id, { status: "completed" });

    const removed = tasks.clearFinished();
    expect(removed).toBe(2);
    // failed 必须留下：它保存着原始输入与失败原因，是用户唯一的重试入口
    const remainingIds = tasks.list().map((task) => task.id);
    expect(remainingIds).toHaveLength(2);
    expect(remainingIds).toContain(pending.id);
    expect(remainingIds).toContain(failed.id);
  });

  it("listByStatus 按状态过滤", () => {
    tasks.create({ kind: "text", input: "甲" });
    const second = tasks.create({ kind: "text", input: "乙" });
    tasks.update(second.id, { status: "failed" });

    expect(tasks.listByStatus(["pending"])).toHaveLength(1);
    expect(tasks.listByStatus(["failed"])).toHaveLength(1);
    expect(tasks.listByStatus(["pending", "failed"])).toHaveLength(2);
  });
});
