import { randomBytes, randomUUID } from "node:crypto";
import type Database from "@guizhi/db/adapter";
import { MobileCaptureDB } from "@guizhi/db/mobile-capture";
import type { CaptureDelivery, CaptureDevice, CapturePairing, MobileCaptureSettings } from "@guizhi/shared/types/mobile-capture";
import type { ImportService } from "../import/import-service";
import { CaptureCredentials, type CaptureConnection } from "./credentials";
import { captureOrigin, CaptureHttpError, captureRequest } from "./transport";
import { logAppError } from "../../diagnostic-log";
export class CaptureReceiver {
  private connection: CaptureConnection | null = null;
  private credentials = new CaptureCredentials();
  private controller = new AbortController();
  private timer?: ReturnType<typeof setTimeout>;
  private inflight?: Promise<void>;
  private failures = 0;
  private error?: string;
  private lastReceivedAt?: number;
  private stopped = false;
  private receipts: MobileCaptureDB;
  constructor(private db: Database.Database, private imports: ImportService) {
    this.receipts = new MobileCaptureDB(db);
    try { this.connection = this.credentials.read(); } catch (error) { this.error = (error as Error).message; }
    this.receipts.reconcile();
    this.schedule(1000);
  }
  status(): MobileCaptureSettings {
    return { origin: this.connection?.origin ?? "", mailboxId: this.connection?.mailboxId ?? "", paused: this.connection?.paused ?? true,
      collectionId: this.connection?.collectionId ?? null, connected: !!this.connection?.mailboxId, persistent: this.credentials.secure(), error: this.error ?? this.connection?.notice, lastReceivedAt: this.lastReceivedAt };
  }
  private request<T>(path: string, body?: unknown, method = "GET") {
    if (!this.connection) throw new Error("请先用邀请码激活手机收集");
    return captureRequest<T>(this.connection.origin, path, this.connection.credential, body, method, this.controller.signal);
  }
  async activate(origin: string, invite: string) {
    origin = captureOrigin(origin);
    if (this.connection?.mailboxId) throw new Error("请先停用现有收件箱");
    if (!this.connection || this.connection.origin !== origin) this.connection = {
      origin, mailboxId: "", credential: randomBytes(32).toString("base64url"), requestId: randomUUID(), paused: true, collectionId: null,
    };
    // 请求编号和秘密先安全落盘，创建响应丢失后可用同一编号重试。
    this.credentials.write(this.connection);
    const response = await this.request<{ mailboxId: string }>("/v1/mailboxes", {
      requestId: this.connection.requestId, credential: this.connection.credential, invite,
    }, "POST");
    this.connection.mailboxId = response.mailboxId; this.connection.paused = false;
    this.credentials.write(this.connection); this.error = undefined; this.schedule(0);
    return this.status();
  }
  configure(paused: boolean, collectionId: string | null) {
    if (!this.connection) throw new Error("请先激活手机收集");
    if (collectionId && !this.db.get("SELECT id FROM collections WHERE id=?", collectionId)) throw new Error("目标知识库不存在");
    this.connection.paused = paused; this.connection.collectionId = collectionId; this.connection.notice = undefined;
    this.credentials.write(this.connection);
    if (paused) { this.controller.abort(); clearTimeout(this.timer); }
    this.controller = new AbortController(); this.stopped = false;
    if (!paused) this.schedule(0);
    return this.status();
  }
  async pairing() {
    const nonce = randomBytes(32).toString("base64url");
    const pair = await this.request<CapturePairing>("/v1/pairings", { nonce }, "POST");
    return { ...pair, url: `${this.connection!.origin}/#pair=${encodeURIComponent(pair.id)}&nonce=${nonce}` };
  }
  pairings() { return this.request<CapturePairing[]>("/v1/pairings"); }
  devices() { return this.request<CaptureDevice[]>("/v1/devices"); }
  confirm(id: string, deviceId: string) { return this.request(`/v1/pairings/${encodeURIComponent(id)}/confirm`, { deviceId }, "POST"); }
  revoke(id: string) { return this.request(`/v1/devices/${encodeURIComponent(id)}`, {}, "DELETE"); }
  async disable() {
    try { await this.request("/v1/mailbox", {}, "DELETE"); }
    catch (error) { if (!(error instanceof CaptureHttpError) || error.status !== 401) throw error; }
    this.stop(true); this.connection = null;
    // 无法删除旧凭证时也已被服务端撤销；下次激活覆盖安全配置。
    this.credentials.clear(); this.controller = new AbortController(); this.stopped = false;
    return this.status();
  }
  stop(pause = false) {
    this.stopped = true; clearTimeout(this.timer); this.controller.abort();
    if (pause && this.connection) { this.connection.paused = true; this.credentials.write(this.connection); }
  }
  private schedule(delay: number) {
    clearTimeout(this.timer);
    if (!this.stopped && this.connection?.mailboxId && !this.connection.paused) this.timer = setTimeout(() => { void this.tick(); }, delay);
  }
  tick(): Promise<void> {
    if (this.inflight !== undefined) return this.inflight;
    if (this.stopped || !this.connection?.mailboxId || this.connection.paused) return Promise.resolve();
    this.inflight = this.run().finally(() => { this.inflight = undefined; });
    return this.inflight;
  }
  private async run() {
    let delay = 30000;
    const signal = this.controller.signal;
    try {
      const meta = await this.request<{ protocol: number }>("/v1/meta"); signal.throwIfAborted();
      if (meta.protocol !== 1) throw new Error("手机收集服务协议不兼容，请升级应用");
      const connection = this.connection!;
      const relayKey = `${connection.origin}|${connection.mailboxId}`;
      if (connection.collectionId && !this.db.get("SELECT id FROM collections WHERE id=?", connection.collectionId)) {
        connection.collectionId = null; connection.notice = "目标知识库已删除，本次及后续收集将保存到未分类"; this.credentials.write(connection);
      } else this.error = undefined;
      const deliveries = await this.request<CaptureDelivery[]>("/v1/deliveries"); signal.throwIfAborted();
      if (!Array.isArray(deliveries) || deliveries.length > 50) throw new Error("手机收集响应无效");
      for (const delivery of deliveries) {
        signal.throwIfAborted();
        if (!/^[a-zA-Z0-9_-]{16,128}$/.test(delivery.id)) throw new Error("手机收集投递编号无效");
        const ids = this.receipts.receive(relayKey, delivery, connection.collectionId);
        this.imports.queue.schedulePersisted(ids);
        await this.request(`/v1/deliveries/${delivery.id}/ack`, {}, "POST"); signal.throwIfAborted();
        this.receipts.acknowledge(relayKey, delivery.id); this.lastReceivedAt = Date.now();
      }
      this.receipts.reconcile();
      for (const entry of this.receipts.pending(relayKey)) {
        signal.throwIfAborted();
        try {
          if (!entry.acked) {
            await this.request(`/v1/deliveries/${entry.id}/ack`, {}, "POST"); signal.throwIfAborted();
            this.receipts.acknowledge(relayKey, entry.id);
          }
          await this.request(`/v1/deliveries/${entry.id}/progress`, entry.progress, "PUT"); signal.throwIfAborted();
          this.receipts.sent(relayKey, entry.id, entry.progress.version);
        } catch (error) {
          signal.throwIfAborted();
          if (error instanceof CaptureHttpError && [404, 410].includes(error.status)) this.receipts.sent(relayKey, entry.id, entry.progress.version);
          else throw error;
        }
      }
      this.failures = 0;
      if (deliveries.length === 50) delay = 1000;
    } catch (error) {
      if (signal.aborted) return;
      this.error = error instanceof Error ? error.message : "手机收集暂时不可用";
      if (this.failures === 0) logAppError({ scope: "mobile-capture", action: "自动取件", message: this.error });
      delay = Math.min(300000, 1000 * 2 ** Math.min(++this.failures, 9)) * (0.8 + Math.random() * 0.2);
      if (error instanceof CaptureHttpError) {
        delay = Math.max(delay, error.retryAfter);
        if ([401, 403, 426].includes(error.status)) { this.connection!.paused = true; this.credentials.write(this.connection!); }
      }
    } finally { this.schedule(delay); }
  }
}
