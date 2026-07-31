/**
 * AI 用量统计 DAO。
 *
 * `usage` 字段此前在类型里声明了却从没被读过：一次提问最坏会触发十来次模型
 * 调用，Wiki 后台编译也在持续消耗，而界面上没有任何地方能看出用了多少。
 * 这里按 天 × 场景 × 模型 聚合，保留最近若干天。
 */
import type Database from "./adapter";
import type {
  AIUsageDailyRow,
  AIUsageModelRow,
  AIUsageSummary,
} from "@guizhi/shared/types";

/** 超出这个天数的记录在下次写入时清理 */
const RETENTION_DAYS = 90;

interface UsageRow {
  day: string;
  scenario: string;
  model: string;
  calls: number;
  failed_calls: number;
  prompt_tokens: number;
  completion_tokens: number;
}

interface UsageTotals {
  calls: number;
  failedCalls: number;
  promptTokens: number;
  completionTokens: number;
}

function addUsage(target: UsageTotals, row: UsageRow): void {
  target.calls += row.calls;
  target.failedCalls += row.failed_calls ?? 0;
  target.promptTokens += row.prompt_tokens;
  target.completionTokens += row.completion_tokens;
}

function sortByCallsDesc<T extends { calls: number }>(rows: T[]): T[] {
  return rows.sort((left, right) => right.calls - left.calls);
}

/** 本地日期（YYYY-MM-DD）：用量按用户所在时区的自然日聚合 */
export function toLocalDay(timestamp: number): string {
  const date = new Date(timestamp);
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

export class AIUsageDB {
  constructor(private readonly db: Database.Database) {}

  /**
   * 记一次模型调用。
   *
   * 失败的调用也要记：401 不花钱，但超时、限流重试、思考 token 烧光后返回
   * 空正文这些都已经产生了费用，只统计成功调用会让面板显著低估实际消耗。
   */
  record(entry: {
    scenario: string;
    model: string;
    promptTokens: number;
    completionTokens: number;
    failed?: boolean;
  }): void {
    const now = Date.now();
    const day = toLocalDay(now);
    const failed = entry.failed ? 1 : 0;
    this.db.run(
      `INSERT INTO ai_usage_daily
         (day, scenario, model, calls, failed_calls, prompt_tokens, completion_tokens, updated_at)
       VALUES (?, ?, ?, 1, ?, ?, ?, ?)
       ON CONFLICT(day, scenario, model) DO UPDATE SET
         calls = calls + 1,
         failed_calls = failed_calls + excluded.failed_calls,
         prompt_tokens = prompt_tokens + excluded.prompt_tokens,
         completion_tokens = completion_tokens + excluded.completion_tokens,
         updated_at = excluded.updated_at`,
      day,
      entry.scenario,
      entry.model,
      failed,
      Math.max(0, Math.trunc(entry.promptTokens)),
      Math.max(0, Math.trunc(entry.completionTokens)),
      now,
    );

    const cutoff = toLocalDay(now - RETENTION_DAYS * 24 * 60 * 60 * 1000);
    this.db.run("DELETE FROM ai_usage_daily WHERE day < ?", cutoff);
  }

  /** 最近 days 天的按场景 / 按模型汇总 + 总计 */
  summary(days: number): AIUsageSummary {
    const since = toLocalDay(
      Date.now() - Math.max(0, days - 1) * 24 * 60 * 60 * 1000,
    );
    const rows = this.db.all(
      `SELECT day, scenario, model, calls, failed_calls, prompt_tokens, completion_tokens
       FROM ai_usage_daily WHERE day >= ? ORDER BY day DESC`,
      since,
    ) as UsageRow[];

    const byScenario = new Map<string, AIUsageDailyRow>();
    const byModel = new Map<string, AIUsageModelRow>();
    const totals: UsageTotals = {
      calls: 0,
      failedCalls: 0,
      promptTokens: 0,
      completionTokens: 0,
    };
    for (const row of rows) {
      addUsage(totals, row);

      const scenarioRow = byScenario.get(row.scenario);
      if (scenarioRow) {
        addUsage(scenarioRow, row);
      } else {
        byScenario.set(row.scenario, {
          scenario: row.scenario,
          calls: row.calls,
          failedCalls: row.failed_calls ?? 0,
          promptTokens: row.prompt_tokens,
          completionTokens: row.completion_tokens,
        });
      }

      const modelKey = row.model || "(unknown)";
      const modelRow = byModel.get(modelKey);
      if (modelRow) {
        addUsage(modelRow, row);
      } else {
        byModel.set(modelKey, {
          model: modelKey,
          calls: row.calls,
          failedCalls: row.failed_calls ?? 0,
          promptTokens: row.prompt_tokens,
          completionTokens: row.completion_tokens,
        });
      }
    }

    return {
      days,
      ...totals,
      byScenario: sortByCallsDesc([...byScenario.values()]),
      byModel: sortByCallsDesc([...byModel.values()]),
    };
  }

  clear(): void {
    this.db.run("DELETE FROM ai_usage_daily");
  }
}
