import type Database from "./adapter";
import { ImportTaskDB } from "./import-task";
import type { ImportTask } from "@guizhi/shared/types";
import type { CaptureDelivery, CaptureItemResult, CaptureProgress } from "@guizhi/shared/types/mobile-capture";
import { parseCaptureSubmission } from "@guizhi/shared/utils/capture-submission";
interface ReceiptRow { relay_key: string; delivery_id: string; task_ids: string; progress: string; version: number; acked: number }
function result(task: ImportTask, index: number): CaptureItemResult {
  let error: CaptureItemResult["error"];
  if (task.warning) error = "incomplete";
  if (task.status === "failed") {
    const message = task.error ?? "";
    error = /login_required|验证码|登录/i.test(message) ? "login_required" : /429|rate.limit/i.test(message) ? "rate_limited" :
      /network|fetch failed|timeout|网络|超时/i.test(message) ? "network" : "capture_failed";
  }
  return { index, status: task.status, ...(error ? { error } : {}) };
}
export class MobileCaptureDB {
  constructor(private db: Database.Database) {}
  receive(relayKey: string, delivery: CaptureDelivery, collectionId: string | null): string[] {
    return this.db.transaction(() => {
      const existing = this.db.get("SELECT task_ids FROM mobile_capture_receipts WHERE relay_key=? AND delivery_id=?", relayKey, delivery.id) as { task_ids: string } | undefined;
      if (existing) return JSON.parse(existing.task_ids) as string[];
      const { items } = parseCaptureSubmission(delivery);
      if (items.length !== delivery.itemCount) throw new Error("手机收集协议不兼容：项目数量不一致");
      const target = collectionId && this.db.get("SELECT id FROM collections WHERE id=?", collectionId) ? collectionId : null;
      const tasks = items.map(item => new ImportTaskDB(this.db).create({ ...item, collectionId: target, captureStrategy: "standard", commentLimit: 0 }));
      const ids = tasks.map(t => t.id), progress = tasks.map(result);
      this.db.run("INSERT INTO mobile_capture_receipts(relay_key,delivery_id,original_input,task_ids,progress,created_at) VALUES(?,?,?,?,?,?)",
        relayKey, delivery.id, delivery.input, JSON.stringify(ids), JSON.stringify(progress), Date.now());
      for (const task of tasks) this.db.run("INSERT INTO mobile_capture_tasks VALUES(?,?,?)", task.id, relayKey, delivery.id);
      this.db.run("INSERT INTO mobile_capture_outbox VALUES(?,?,1)", relayKey, delivery.id);
      return ids;
    })();
  }
  observe(task: ImportTask) {
    const link = this.db.get("SELECT relay_key,delivery_id FROM mobile_capture_tasks WHERE task_id=?", task.id) as { relay_key: string; delivery_id: string } | undefined;
    if (!link) return;
    this.db.transaction(() => {
      const row = this.db.get("SELECT * FROM mobile_capture_receipts WHERE relay_key=? AND delivery_id=?", link.relay_key, link.delivery_id) as ReceiptRow;
      const ids = JSON.parse(row.task_ids) as string[], items = JSON.parse(row.progress) as CaptureItemResult[];
      const index = ids.indexOf(task.id); if (index < 0) return;
      const next = result(task, index); if (JSON.stringify(items[index]) === JSON.stringify(next)) return;
      items[index] = next;
      this.db.run("UPDATE mobile_capture_receipts SET progress=?,version=version+1 WHERE relay_key=? AND delivery_id=?", JSON.stringify(items), link.relay_key, link.delivery_id);
      this.db.run("INSERT INTO mobile_capture_outbox VALUES(?,?,?) ON CONFLICT(relay_key,delivery_id) DO UPDATE SET version=excluded.version", link.relay_key, link.delivery_id, row.version + 1);
    })();
  }
  reconcile() {
    // 任务状态写完、广播前崩溃也能修复；任务被清理后保留最后结果及幂等收据。
    const dao = new ImportTaskDB(this.db);
    const rows = this.db.all("SELECT task_id FROM mobile_capture_tasks") as { task_id: string }[];
    for (const row of rows) {
      const task = dao.get(row.task_id);
      if (task) this.observe(task);
      else this.db.run("DELETE FROM mobile_capture_tasks WHERE task_id=?", row.task_id);
    }
    this.db.run(`UPDATE mobile_capture_receipts SET original_input='',task_ids='[]' WHERE
      NOT EXISTS(SELECT 1 FROM mobile_capture_tasks t WHERE t.relay_key=mobile_capture_receipts.relay_key AND t.delivery_id=mobile_capture_receipts.delivery_id)
      AND NOT EXISTS(SELECT 1 FROM mobile_capture_outbox o WHERE o.relay_key=mobile_capture_receipts.relay_key AND o.delivery_id=mobile_capture_receipts.delivery_id)`);
  }
  pending(relayKey: string): { id: string; acked: boolean; progress: CaptureProgress }[] {
    return (this.db.all(`SELECT r.* FROM mobile_capture_receipts r JOIN mobile_capture_outbox o
      ON r.relay_key=o.relay_key AND r.delivery_id=o.delivery_id WHERE r.relay_key=? LIMIT 100`, relayKey) as ReceiptRow[])
      .map(row => ({ id: row.delivery_id, acked: row.acked === 1, progress: { version: row.version, items: JSON.parse(row.progress) } }));
  }
  acknowledge(relayKey: string, id: string) { this.db.run("UPDATE mobile_capture_receipts SET acked=1 WHERE relay_key=? AND delivery_id=?", relayKey, id); }
  sent(relayKey: string, id: string, version: number) { this.db.run("DELETE FROM mobile_capture_outbox WHERE relay_key=? AND delivery_id=? AND version=?", relayKey, id, version); }
}
