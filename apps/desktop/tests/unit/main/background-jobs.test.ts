import { describe, expect, it, vi } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { BackgroundJobRuntime } from "../../../src/main/services/background-jobs";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("BackgroundJobRuntime", () => {
  it("到期才把 Renderer 任务发出，失败后按 15 分钟退避再领取", async () => {
    let now = 1_000;
    const sendRendererJob = vi.fn();
    const runtime = new BackgroundJobRuntime(createTestDb(), {
      ownerId: "main-test",
      now: () => now,
      sendRendererJob,
    });

    runtime.syncRendererJobs({ wikiEnabled: false, semanticEnabled: true });
    await runtime.tick();
    expect(sendRendererJob).not.toHaveBeenCalled();

    now += 60_000;
    await runtime.tick();
    expect(sendRendererJob).toHaveBeenCalledTimes(1);
    const first = sendRendererJob.mock.calls[0][0];
    expect(first).toMatchObject({ kind: "semantic-index", state: "running" });

    expect(runtime.failRendererJob(first.id, "网络错误")).toBe(true);
    expect(runtime.list().find((job) => job.id === first.id)).toMatchObject({
      state: "retry_wait",
      attempt: 1,
      nextRunAt: now + 15 * 60_000,
    });

    now += 15 * 60_000;
    await runtime.tick();
    expect(sendRendererJob).toHaveBeenCalledTimes(2);
  });

  it("主进程任务由注册 handler 执行并按周期重排", async () => {
    const now = 5_000;
    const runtime = new BackgroundJobRuntime(createTestDb(), {
      ownerId: "main-test",
      now: () => now,
      sendRendererJob: vi.fn(),
    });
    const handler = vi.fn(async () => undefined);
    runtime.registerHandler("backup", handler);
    runtime.schedule("auto-backup", {
      kind: "backup",
      intervalMinutes: 60,
      nextRunAt: now,
    });

    await runtime.tick();
    await vi.waitFor(() => expect(handler).toHaveBeenCalledTimes(1));
    expect(runtime.list().find((job) => job.id === "auto-backup")).toMatchObject({
      state: "scheduled",
      nextRunAt: now + 60 * 60_000,
      lastSuccessAt: now,
    });
  });

  it("Renderer 心跳续租后，原租约到期点不会重复派发", async () => {
    let now = 0;
    const sendRendererJob = vi.fn();
    const runtime = new BackgroundJobRuntime(createTestDb(), {
      ownerId: "main-test",
      now: () => now,
      leaseDurationMs: 90_000,
      sendRendererJob,
    });
    runtime.syncRendererJobs({ wikiEnabled: false, semanticEnabled: true });
    now = 60_000;
    await runtime.tick();
    const job = sendRendererJob.mock.calls[0][0];

    now = 120_000;
    expect(runtime.renewRendererJob(job.id)).toBe(true);
    now = 151_000;
    await runtime.tick();
    expect(sendRendererJob).toHaveBeenCalledTimes(1);
  });

  it("应用重启后同步配置不会把已经到期的持久化任务推迟", () => {
    let now = 1_000;
    const db = createTestDb();
    const first = new BackgroundJobRuntime(db, {
      ownerId: "first",
      now: () => now,
      sendRendererJob: vi.fn(),
    });
    first.syncRendererJobs({ wikiEnabled: true, semanticEnabled: false });
    const dueAt = first.list().find((job) => job.kind === "wiki-compile")?.nextRunAt;

    now += 10 * 60_000;
    const afterRestart = new BackgroundJobRuntime(db, {
      ownerId: "second",
      now: () => now,
      sendRendererJob: vi.fn(),
    });
    afterRestart.syncRendererJobs({ wikiEnabled: true, semanticEnabled: false });
    expect(
      afterRestart.list().find((job) => job.kind === "wiki-compile")?.nextRunAt,
    ).toBe(dueAt);
  });
});
