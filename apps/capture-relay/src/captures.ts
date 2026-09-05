import { parseCaptureSubmission } from "@guizhi/shared/utils/capture-submission";
import type { CaptureProgress, CaptureReceipt } from "@guizhi/shared/types/mobile-capture";
import { RelayDatabase, assert, DAY, hash, id } from "./database";
import type { Principal } from "./accounts";
interface CaptureRow {
  id: string; request_id: string; fingerprint: string; input: string | null; mode: string; item_count: number;
  created_at: number; expires_at: number; state: CaptureReceipt["state"]; progress: string | null; version: number;
}
const receipt = (r: CaptureRow): CaptureReceipt => ({ id: r.id, requestId: r.request_id, itemCount: r.item_count,
  createdAt: r.created_at, expiresAt: r.expires_at, state: r.state, progress: r.progress ? JSON.parse(r.progress) : null });
export interface Limits { dailyItems: number; pendingItems: number; pendingBytes: number; retentionDays: number }
export const DEFAULT_LIMITS: Limits = { dailyItems: 200, pendingItems: 500, pendingBytes: 5 * 1024 * 1024, retentionDays: 30 };
export class Captures {
  constructor(public db: RelayDatabase, public limits = DEFAULT_LIMITS) {}
  submit(p: Principal, body: unknown, now = Date.now()) {
    const { submission, items } = parseCaptureSubmission(body);
    const fingerprint = hash(JSON.stringify([submission.input, submission.mode]));
    return this.db.transaction(() => {
      const existing = this.db.get<CaptureRow>("SELECT * FROM captures WHERE device_id=? AND request_id=?", p.id, submission.requestId);
      if (existing) { assert(existing.fingerprint === fingerprint, "request_conflict", 409); return receipt(existing); }
      const day = Math.floor(now / DAY), bytes = Buffer.byteLength(submission.input);
      const usage = this.db.get<{ items: number }>("SELECT items FROM daily_usage WHERE mailbox_id=? AND day=?", p.mailboxId, day)?.items ?? 0;
      assert(usage + items.length <= this.limits.dailyItems, "daily_limit", 429);
      const pending = this.db.get<{ items: number; bytes: number }>("SELECT coalesce(sum(item_count),0) AS items,coalesce(sum(bytes),0) AS bytes FROM captures WHERE mailbox_id=? AND state='accepted'", p.mailboxId)!;
      assert(pending.items + items.length <= this.limits.pendingItems && pending.bytes + bytes <= this.limits.pendingBytes, "inbox_full", 429);
      const captureId = id();
      this.db.run(`INSERT INTO captures(id,mailbox_id,device_id,request_id,fingerprint,input,mode,item_count,bytes,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
        captureId, p.mailboxId, p.id, submission.requestId, fingerprint, submission.input, submission.mode, items.length, bytes, now, now + this.limits.retentionDays * DAY);
      this.db.run("INSERT INTO daily_usage VALUES(?,?,?) ON CONFLICT(mailbox_id,day) DO UPDATE SET items=items+excluded.items", p.mailboxId, day, items.length);
      return receipt(this.db.get<CaptureRow>("SELECT * FROM captures WHERE id=?", captureId)!);
    });
  }
  deliveries(mailboxId: string, after: string) {
    // UUID 排序的无状态页游标；一轮扫完从头再取，不依赖时钟顺序。
    return this.db.all<CaptureRow>("SELECT * FROM captures WHERE mailbox_id=? AND state='accepted' AND expires_at>? AND id>? ORDER BY id LIMIT 50", mailboxId, Date.now(), after)
      .map(r => ({ ...receipt(r), input: r.input, mode: r.mode }));
  }
  acknowledge(mailboxId: string, captureId: string) {
    const row = this.db.get<CaptureRow>("SELECT * FROM captures WHERE id=? AND mailbox_id=?", captureId, mailboxId);
    assert(row, "not_found", 404);
    assert(row.state !== "expired", "capture_expired", 410);
    this.db.run("UPDATE captures SET state='received',input=NULL,received_at=coalesce(received_at,?) WHERE id=?", Date.now(), captureId);
    return { success: true };
  }
  progress(mailboxId: string, captureId: string, body: unknown) {
    const p = body as CaptureProgress;
    assert(p && Number.isSafeInteger(p.version) && p.version > 0 && Array.isArray(p.items), "invalid_progress");
    return this.db.transaction(() => {
      const row = this.db.get<CaptureRow>("SELECT * FROM captures WHERE id=? AND mailbox_id=?", captureId, mailboxId);
      assert(row, "not_found", 404);
      assert(row.state === "received", "ack_required", 409);
      assert(p.items.length === row.item_count && p.items.every((item, index) => item.index === index &&
        ["pending", "processing", "completed", "failed", "canceled", "duplicate"].includes(item.status) &&
        (!item.error || ["login_required", "rate_limited", "network", "capture_failed", "incomplete"].includes(item.error))), "invalid_progress");
      // 重建白名单对象，拒绝正文和底层报错被偷渡进记录。
      const clean = { version: p.version, items: p.items.map(i => ({ index: i.index, status: i.status, ...(i.error ? { error: i.error } : {}) })) };
      const json = JSON.stringify(clean);
      if (p.version === row.version) assert(row.progress === json, "version_conflict", 409);
      if (p.version > row.version) this.db.run("UPDATE captures SET version=?,progress=? WHERE id=?", p.version, json, captureId);
      return { success: true, version: Math.max(p.version, row.version) };
    });
  }
  history(p: Principal, before: number) {
    return this.db.all<CaptureRow>(`SELECT c.* FROM captures c WHERE c.mailbox_id=? AND c.created_at<? AND
      (c.device_id=? OR c.device_id IN(SELECT id FROM devices WHERE parent_id=?)) ORDER BY created_at DESC,id DESC LIMIT 100`, p.mailboxId, before, p.id, p.kind === "phone" ? p.id : "").map(receipt);
  }
}
