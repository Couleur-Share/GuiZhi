import { describe, it, expect } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { MobileCaptureDB } from "@guizhi/db/mobile-capture";
import { ImportTaskDB } from "@guizhi/db/import-task";
import { parseCaptureSubmission } from "@guizhi/shared/utils/capture-submission";
import { ImportQueue } from "../../../src/main/services/import/import-queue";
import type { CaptureDelivery } from "@guizhi/shared/types/mobile-capture";
const delivery: CaptureDelivery = { id: "delivery-00000001", requestId: "request-000000001", input: "https://example.com/a?token=A%2FB\nhttps://example.com/b", mode: "auto", itemCount: 2, createdAt: 1, expiresAt: 9999999999999 };
function database() { const db = new Database(":memory:"); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES); return db; }
describe("手机收集事务与重放", () => {
  it("原文、收据和任务一并提交；ACK 丢失与清理后重放不重建", () => {
    const db = database(); try {
      const receipts = new MobileCaptureDB(db), dao = new ImportTaskDB(db);
      const ids = receipts.receive("relay|mailbox", delivery, null);
      expect(ids).toHaveLength(2); expect(dao.get(ids[0])?.sourceInput).toContain("token=A%2FB");
      expect(receipts.receive("relay|mailbox", delivery, null)).toEqual(ids);
      db.run("DELETE FROM import_tasks");
      expect(receipts.receive("relay|mailbox", delivery, null)).toEqual(ids); expect(dao.list()).toHaveLength(0);
      expect(db.get("SELECT original_input FROM mobile_capture_receipts")).toEqual({ original_input: delivery.input });
    } finally { db.close(); }
  });
  it("中途创建任务失败全部回滚", () => {
    const db = database(); try {
      db.exec("CREATE TRIGGER fail_second BEFORE INSERT ON import_tasks WHEN NEW.source_input LIKE '%/b' BEGIN SELECT RAISE(ABORT,'模拟落盘失败'); END");
      expect(() => new MobileCaptureDB(db).receive("r", delivery, null)).toThrow();
      for (const table of ["import_tasks", "mobile_capture_receipts", "mobile_capture_outbox"]) expect(db.all(`SELECT * FROM ${table}`)).toHaveLength(0);
    } finally { db.close(); }
  });
  it("状态待发送持久化、乱序确认不会清除较新的版本，结果脱敏", () => {
    const db = database(); try {
      const receipts = new MobileCaptureDB(db), dao = new ImportTaskDB(db), ids = receipts.receive("r", delivery, null);
      receipts.observe(dao.update(ids[0], { status: "failed", error: "[login_required] https://secret.example/token=123" })!);
      receipts.sent("r", delivery.id, 1);
      expect(receipts.pending("r")[0].progress.version).toBe(2);
      expect(JSON.stringify(receipts.pending("r"))).not.toContain("secret.example");
      expect(receipts.pending("r")[0].progress.items[0].error).toBe("login_required");
      receipts.sent("r", delivery.id, 2); expect(receipts.pending("r")).toEqual([]);
      dao.update(ids[1], { status: "completed" }); receipts.reconcile(); expect(receipts.pending("r")[0].progress.version).toBe(3);
    } finally { db.close(); }
  });
  it("持久化调度尊重暂停，不会重复启动", async () => {
    const db = database(); try {
      const dao = new ImportTaskDB(db), receipts = new MobileCaptureDB(db), ids = receipts.receive("r", delivery, null);
      let calls = 0;
      const queue = new ImportQueue({ store: dao, onTaskChanged: t => receipts.observe(t), extract: async () => { calls++; throw new Error("合成失败"); },
        persistence: { findDuplicate: () => null, rememberSourceAccess: () => {}, saveItem: () => "" } });
      queue.pause(); queue.schedulePersisted(ids); queue.schedulePersisted(ids); expect(calls).toBe(0); expect(queue.getState().pendingCount).toBe(2);
      queue.resume(); await queue.drain(); expect(calls).toBe(2); queue.schedulePersisted(ids); expect(calls).toBe(2);
    } finally { db.close(); }
  });
});
describe("共享手机输入解析", () => {
  for (const [input, kind, count] of [
    ["复制打开抖音 https://v.douyin.com/AbCd/", "url", 1],
    ["小红书 http://xhslink.com/a/abc，复制本条信息", "url", 1],
    ["https://mp.weixin.qq.com/s?__biz=abc&mid=12&idx=1&sn=secret", "url", 1],
    ["明天读 https://example.com/article", "text", 1],
    ["普通文字笔记", "text", 1],
    ["https://example.com/a\nhttps://example.com/a\nhttps://example.com/b", "url", 2],
  ] as const) it(input, () => {
    const parsed = parseCaptureSubmission({ requestId: "request-00000001", input, mode: "auto" }); expect(parsed.items).toHaveLength(count); expect(parsed.items[0].kind).toBe(kind); expect(parsed.submission.input).toBe(input);
  });
});
