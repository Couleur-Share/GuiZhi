import { afterEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES } from "@guizhi/db/schema";
import { CaptureReceiver } from "../../../src/main/services/mobile-capture/receiver";
import type { ImportService } from "../../../src/main/services/import/import-service";
const mocks = vi.hoisted(() => ({ request: vi.fn(), write: vi.fn(), connection: { origin: "https://capture.example.com", mailboxId: "mailbox", credential: "secret", requestId: "request", paused: false, collectionId: null } }));
vi.mock("../../../src/main/diagnostic-log", () => ({ logAppError: vi.fn() }));
vi.mock("../../../src/main/services/mobile-capture/credentials", () => ({ CaptureCredentials: class {
  read() { return { ...mocks.connection }; } secure() { return true; } write(value: unknown) { mocks.write(value); } clear() {}
} }));
vi.mock("../../../src/main/services/mobile-capture/transport", () => ({ captureOrigin: (v: string) => v, captureRequest: (...args: unknown[]) => mocks.request(...args), CaptureHttpError: class extends Error {} }));
afterEach(() => { vi.clearAllMocks(); });
describe("手机取件生命周期", () => {
  it("ACK 丢失后重放只重新确认，任务仍然只有一组", async () => {
    const db = new Database(":memory:"); db.exec(SCHEMA_TABLES);
    const schedulePersisted = vi.fn(); let ackAttempts = 0;
    const delivery = { id: "delivery-00000001", requestId: "request-00000001", input: "https://example.com/a", mode: "urls", itemCount: 1 };
    mocks.request.mockImplementation(async (_origin, path) => {
      if (path === "/v1/meta") return { protocol: 1 };
      if (path === "/v1/deliveries") return [delivery];
      if (path.endsWith("/ack") && ++ackAttempts === 1) throw new Error("响应丢失");
      return { success: true };
    });
    const receiver = new CaptureReceiver(db, { queue: { schedulePersisted } } as unknown as ImportService);
    try {
      await receiver.tick(); await receiver.tick(); expect(db.all("SELECT * FROM import_tasks")).toHaveLength(1);
      expect(schedulePersisted.mock.calls[0][0]).toEqual(schedulePersisted.mock.calls[1][0]); expect(ackAttempts).toBe(2);
    } finally { receiver.stop(); db.close(); }
  });
  it("恢复期间取消在途请求，旧响应回来后不触碰已关闭数据库", async () => {
    const db = new Database(":memory:"); db.exec(SCHEMA_TABLES);
    let finish: (value: unknown) => void = () => {};
    mocks.request.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
    const schedulePersisted = vi.fn(), receiver = new CaptureReceiver(db, { queue: { schedulePersisted } } as unknown as ImportService);
    const pending = receiver.tick(); receiver.stop(true); db.close(); finish({ protocol: 1 }); await pending;
    expect(schedulePersisted).not.toHaveBeenCalled(); expect(mocks.write).toHaveBeenCalledWith(expect.objectContaining({ paused: true }));
    expect(mocks.request.mock.calls[0][5].aborted).toBe(true);
  });
  it("暂停取件不发网络请求，恢复后才重新尝试", async () => {
    const db = new Database(":memory:"); db.exec(SCHEMA_TABLES);
    mocks.request.mockResolvedValue({ protocol: 1 });
    const receiver = new CaptureReceiver(db, {} as ImportService);
    try { receiver.configure(true, null); await receiver.tick(); expect(mocks.request).not.toHaveBeenCalled(); }
    finally { receiver.stop(); db.close(); }
  });
});
