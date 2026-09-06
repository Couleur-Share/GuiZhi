import { afterEach, beforeEach, describe, expect, it } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { ImportTaskDB } from "@guizhi/db/import-task";
import { MobileCaptureDB } from "@guizhi/db/mobile-capture";

describe("导入提交来源与操作范围", () => {
  let db: Database.Database;
  let tasks: ImportTaskDB;
  let mobile: MobileCaptureDB;
  beforeEach(() => {
    db = new Database(":memory:");
    db.exec(SCHEMA_TABLES);
    db.exec(SCHEMA_INDEXES);
    tasks = new ImportTaskDB(db);
    mobile = new MobileCaptureDB(db);
  });
  afterEach(() => db.close());
  function receive(id: string) {
    return mobile.receive("private-relay", {
      id, requestId: `request-00000000-${id}`, input: `手机 ${id}`, mode: "text", itemCount: 1,
      createdAt: 1, expiresAt: Date.now() + 60_000,
    }, null)[0];
  }

  it("历史投递关联识别手机来源，重试和状态更新保留接收时间且不泄露中继信息", () => {
    const desktop = tasks.create({ kind: "text", input: "桌面" });
    const id = receive("delivery-1");
    const task = tasks.get(id)!;
    expect(task.origin).toBe("mobile");
    expect(task.receivedAt).toBeGreaterThan(1);
    expect(JSON.stringify(task)).not.toContain("private-relay");
    expect(tasks.get(desktop.id)).toMatchObject({ origin: "desktop", receivedAt: null });
    expect(tasks.update(id, { status: "failed" })?.origin).toBe("mobile");
    expect(tasks.update(id, { status: "pending" })?.receivedAt).toBe(task.receivedAt);
    expect(tasks.listByStatus(["pending"]).find(t => t.id === id)?.origin).toBe("mobile");
  });

  it("分页、进行中和计数都应用来源；有缺失计数覆盖未加载历史", () => {
    for (let index = 0; index < 25; index++) {
      tasks.update(receive(`phone-${index}`), { status: "completed", warning: "缺少文字稿" });
      tasks.update(tasks.create({ kind: "text", input: `桌面 ${index}` }).id, { status: "failed" });
    }
    const pending = receive("waiting");
    tasks.create({ kind: "text", input: "桌面等待" });
    const page = tasks.listPage({ origin: "mobile", status: "degraded", pageSize: 20 });
    expect(page.entries).toHaveLength(20);
    expect(page.entries.every(t => t.origin === "mobile")).toBe(true);
    expect(page.total).toBe(25);
    expect(page.degradedCount).toBe(25);
    expect(page.counts).toMatchObject({ completed: 25, failed: 0, pending: 1 });
    expect(page.active.map(t => t.id)).toEqual([pending]);
    const second = tasks.listPage({ origin: "mobile", status: "degraded", pageSize: 20, cursor: page.nextCursor });
    expect(second.entries).toHaveLength(5);
    expect(new Set([...page.entries, ...second.entries].map(t => t.id)).size).toBe(25);
    expect(second.nextCursor).toBeNull();
  });

  it("预览与删除严格组合来源、搜索和状态，进行中清理不扩大范围", () => {
    const failed = receive("failed");
    const done = receive("done");
    const degraded = receive("degraded");
    const pending = receive("pending");
    tasks.update(failed, { status: "failed", error: "目标错误" });
    tasks.update(done, { status: "completed" });
    tasks.update(degraded, { status: "completed", warning: "文字稿缺失" });
    const desktop = tasks.create({ kind: "text", input: "目标错误" });
    tasks.update(desktop.id, { status: "failed" });
    const filter = { scope: "filtered", origin: "mobile", status: "failed", query: "目标错误" } as const;
    expect(tasks.countTerminal(filter)).toBe(1);
    expect(tasks.clearTerminal(filter)).toBe(1);
    expect(tasks.get(desktop.id)).not.toBeNull();
    expect(tasks.countTerminal({ scope: "filtered", origin: "mobile", status: "active" })).toBe(0);
    expect(tasks.clearTerminal({ scope: "filtered", origin: "mobile", status: "pending" })).toBe(0);
    expect(tasks.clearTerminal({ scope: "filtered", origin: "mobile", status: "degraded" })).toBe(1);
    expect(tasks.get(done)).not.toBeNull();
    expect(tasks.clearTerminal({ scope: "all", origin: "mobile", status: "failed", query: "无匹配" })).toBe(2);
    expect(tasks.get(pending)).not.toBeNull();
  });

  it("搜索中的百分号与下划线按字面匹配，清理不扩大到其他任务", () => {
    const literal = tasks.create({ kind: "text", input: "进度 50%_完成" });
    const other = tasks.create({ kind: "text", input: "进度 5000完成" });
    tasks.update(literal.id, { status: "completed" });
    tasks.update(other.id, { status: "completed" });
    const query = { scope: "filtered", origin: "desktop", query: "%_" } as const;
    expect(tasks.listPage(query).entries.map(t => t.id)).toEqual([literal.id]);
    expect(tasks.countTerminal(query)).toBe(1);
    expect(tasks.clearTerminal(query)).toBe(1);
    expect(tasks.get(other.id)).not.toBeNull();
  });
});
