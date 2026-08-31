import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { BackgroundJobDB } from "@guizhi/db/background-job";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("BackgroundJobDB", () => {
  let db: DatabaseAdapter.Database;
  let jobs: BackgroundJobDB;

  beforeEach(() => {
    db = createTestDb();
    jobs = new BackgroundJobDB(db);
  });

  it("按 kind + scopeId 幂等更新计划，不产生重复任务", () => {
    const first = jobs.upsert(
      "job-1",
      {
        kind: "platform-discovery",
        scopeId: "view-1",
        payload: { page: 1 },
        intervalMinutes: 1_440,
        nextRunAt: 2_000,
      },
      1_000,
    );
    const updated = jobs.upsert(
      "job-2",
      {
        kind: "platform-discovery",
        scopeId: "view-1",
        payload: { page: 2 },
        intervalMinutes: 360,
        nextRunAt: 3_000,
      },
      1_500,
    );

    expect(updated.id).toBe(first.id);
    expect(updated.payload).toEqual({ page: 2 });
    expect(updated.intervalMinutes).toBe(360);
    expect(jobs.list()).toHaveLength(1);
  });

  it("只领取已到期任务，并在租约有效期内阻止重复执行", () => {
    jobs.upsert(
      "due",
      { kind: "backup", nextRunAt: 1_000, intervalMinutes: 60 },
      500,
    );
    jobs.upsert(
      "future",
      { kind: "backup", scopeId: "future", nextRunAt: 10_000 },
      500,
    );

    const claimed = jobs.claimDue(["backup"], "worker-a", 5_000, 2_000);
    expect(claimed).toMatchObject({
      id: "due",
      state: "running",
      leaseOwner: "worker-a",
      leaseUntil: 7_000,
    });
    expect(jobs.claimDue(["backup"], "worker-b", 5_000, 2_001)).toBeNull();
  });

  it("应用崩溃后可由其他执行器重新领取过期租约", () => {
    jobs.upsert("job", { kind: "wiki-compile", nextRunAt: 100 }, 0);
    expect(jobs.claimDue(["wiki-compile"], "renderer-a", 1_000, 100)).not.toBeNull();

    const recovered = jobs.claimDue(["wiki-compile"], "renderer-b", 2_000, 1_101);
    expect(recovered).toMatchObject({
      id: "job",
      state: "running",
      leaseOwner: "renderer-b",
      leaseUntil: 3_101,
    });
  });

  it("执行中的任务可用心跳续租，非持有者不能续租", () => {
    jobs.upsert("job", { kind: "wiki-compile", nextRunAt: 100 }, 0);
    jobs.claimDue(["wiki-compile"], "renderer", 1_000, 100);

    expect(jobs.renewLease("job", "stranger", 2_000, 500)).toBe(false);
    expect(jobs.renewLease("job", "renderer", 2_000, 500)).toBe(true);
    expect(jobs.get("job")?.leaseUntil).toBe(2_500);
    expect(jobs.claimDue(["wiki-compile"], "other", 1_000, 1_101)).toBeNull();
  });

  it("周期任务成功后重排下一次执行并清空重试状态", () => {
    jobs.upsert(
      "job",
      { kind: "semantic-index", intervalMinutes: 30, nextRunAt: 100 },
      0,
    );
    jobs.claimDue(["semantic-index"], "renderer", 1_000, 100);
    jobs.fail("job", "renderer", "临时错误", 500, 200);
    jobs.claimDue(["semantic-index"], "renderer", 1_000, 700);

    expect(jobs.complete("job", "renderer", 1_000)).toBe(true);
    expect(jobs.get("job")).toMatchObject({
      state: "scheduled",
      nextRunAt: 1_801_000,
      attempt: 0,
      lastError: null,
      lastSuccessAt: 1_000,
    });
  });

  it("失败任务进入退避；登录失效可直接暂停", () => {
    jobs.upsert("retry", { kind: "backup", nextRunAt: 100 }, 0);
    jobs.claimDue(["backup"], "main", 1_000, 100);
    expect(jobs.fail("retry", "main", "磁盘繁忙", 900_000, 200)).toBe(true);
    expect(jobs.get("retry")).toMatchObject({
      state: "retry_wait",
      nextRunAt: 900_200,
      attempt: 1,
      lastError: "磁盘繁忙",
    });

    jobs.upsert("login", { kind: "platform-discovery", nextRunAt: 100 }, 0);
    jobs.claimDue(["platform-discovery"], "main", 1_000, 100);
    expect(jobs.fail("login", "main", "login_required", null, 200)).toBe(true);
    expect(jobs.get("login")).toMatchObject({
      state: "paused",
      nextRunAt: null,
      attempt: 1,
    });
  });

  it("非租约持有者不能完成或修改正在执行的任务", () => {
    jobs.upsert("job", { kind: "backup", nextRunAt: 100 }, 0);
    jobs.claimDue(["backup"], "owner", 1_000, 100);

    expect(jobs.complete("job", "stranger", 200)).toBe(false);
    expect(jobs.fail("job", "stranger", "错误", 1_000, 200)).toBe(false);
    expect(jobs.get("job")).toMatchObject({ state: "running", leaseOwner: "owner" });
  });
});
