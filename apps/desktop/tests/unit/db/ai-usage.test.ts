import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { AIUsageDB, toLocalDay } from "@guizhi/db/ai-usage";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("AIUsageDB", () => {
  let db: DatabaseAdapter.Database;
  let usage: AIUsageDB;

  beforeEach(() => {
    db = createTestDb();
    usage = new AIUsageDB(db);
  });

  it("同一天同场景同模型累加，不新增行", () => {
    usage.record({
      scenario: "qa",
      model: "gpt-4o",
      promptTokens: 100,
      completionTokens: 30,
    });
    usage.record({
      scenario: "qa",
      model: "gpt-4o",
      promptTokens: 50,
      completionTokens: 20,
    });

    const summary = usage.summary(30);
    expect(summary.calls).toBe(2);
    expect(summary.promptTokens).toBe(150);
    expect(summary.completionTokens).toBe(50);
    expect(summary.byScenario).toHaveLength(1);
    expect(summary.byScenario[0]).toMatchObject({ scenario: "qa", calls: 2 });
  });

  it("按场景分组并按调用次数倒序", () => {
    usage.record({
      scenario: "wiki",
      model: "m",
      promptTokens: 10,
      completionTokens: 1,
    });
    for (let index = 0; index < 3; index++) {
      usage.record({
        scenario: "qa",
        model: "m",
        promptTokens: 10,
        completionTokens: 1,
      });
    }

    const summary = usage.summary(30);
    expect(summary.byScenario.map((row) => row.scenario)).toEqual([
      "qa",
      "wiki",
    ]);
    expect(summary.calls).toBe(4);
  });

  it("provider 不回报 token 时只累加调用次数", () => {
    usage.record({
      scenario: "summary",
      model: "m",
      promptTokens: 0,
      completionTokens: 0,
    });
    const summary = usage.summary(30);
    expect(summary.calls).toBe(1);
    expect(summary.promptTokens + summary.completionTokens).toBe(0);
  });

  it("统计窗口之外的记录不计入", () => {
    const oldDay = toLocalDay(Date.now() - 40 * 24 * 60 * 60 * 1000);
    db.run(
      `INSERT INTO ai_usage_daily
         (day, scenario, model, calls, prompt_tokens, completion_tokens, updated_at)
       VALUES (?, 'qa', 'm', 5, 500, 100, ?)`,
      oldDay,
      Date.now(),
    );
    usage.record({
      scenario: "qa",
      model: "m",
      promptTokens: 1,
      completionTokens: 1,
    });

    expect(usage.summary(7).calls).toBe(1);
    expect(usage.summary(90).calls).toBe(6);
  });

  it("负数与小数被规整，避免脏数据污染统计", () => {
    usage.record({
      scenario: "qa",
      model: "m",
      promptTokens: -5,
      completionTokens: 3.7,
    });
    const summary = usage.summary(30);
    expect(summary.promptTokens).toBe(0);
    expect(summary.completionTokens).toBe(3);
  });

  it("失败调用单独计数，仍计入总次数", () => {
    usage.record({
      scenario: "illustration",
      model: "gpt-image-2",
      promptTokens: 0,
      completionTokens: 0,
    });
    usage.record({
      scenario: "illustration",
      model: "gpt-image-2",
      promptTokens: 0,
      completionTokens: 0,
      failed: true,
    });

    const summary = usage.summary(30);
    expect(summary.calls).toBe(2);
    expect(summary.failedCalls).toBe(1);
    expect(summary.byScenario[0]).toMatchObject({
      scenario: "illustration",
      calls: 2,
      failedCalls: 1,
    });
  });

  it("clear 清空统计", () => {
    usage.record({
      scenario: "qa",
      model: "m",
      promptTokens: 1,
      completionTokens: 1,
    });
    usage.clear();
    expect(usage.summary(30).calls).toBe(0);
  });
});
