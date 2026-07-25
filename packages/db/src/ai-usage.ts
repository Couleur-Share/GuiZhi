/**
 * AI 用量统计 DAO。
 *
 * `usage` 字段此前在类型里声明了却从没被读过：一次提问最坏会触发十来次模型
 * 调用，Wiki 后台编译也在持续消耗，而界面上没有任何地方能看出用了多少。
 * 这里按 天 × 场景 × 模型 聚合，保留最近若干天。
 */
import type Database from "./adapter";
import type { AIUsageDailyRow, AIUsageSummary } from "@guizhi/shared/types";

/** 超出这个天数的记录在下次写入时清理 */
const RETENTION_DAYS = 90;

interface UsageRow {
  day: string;
  scenario: string;
  model: string;
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
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

  record(entry: {
    scenario: string;
    model: string;
    promptTokens: number;
    completionTokens: number;
  }): void {
    const now = Date.now();
    const day = toLocalDay(now);
    this.db.run(
      `INSERT INTO ai_usage_daily
         (day, scenario, model, calls, prompt_tokens, completion_tokens, updated_at)
       VALUES (?, ?, ?, 1, ?, ?, ?)
       ON CONFLICT(day, scenario, model) DO UPDATE SET
         calls = calls + 1,
         prompt_tokens = prompt_tokens + excluded.prompt_tokens,
         completion_tokens = completion_tokens + excluded.completion_tokens,
         updated_at = excluded.updated_at`,
      day,
      entry.scenario,
      entry.model,
      Math.max(0, Math.trunc(entry.promptTokens)),
      Math.max(0, Math.trunc(entry.completionTokens)),
      now,
    );

    const cutoff = toLocalDay(now - RETENTION_DAYS * 24 * 60 * 60 * 1000);
    this.db.run("DELETE FROM ai_usage_daily WHERE day < ?", cutoff);
  }

  /** 最近 days 天的按场景汇总 + 总计 */
  summary(days: number): AIUsageSummary {
    const since = toLocalDay(
      Date.now() - Math.max(0, days - 1) * 24 * 60 * 60 * 1000,
    );
    const rows = this.db.all(
      `SELECT day, scenario, model, calls, prompt_tokens, completion_tokens
       FROM ai_usage_daily WHERE day >= ? ORDER BY day DESC`,
      since,
    ) as UsageRow[];

    const byScenario = new Map<string, AIUsageDailyRow>();
    let calls = 0;
    let promptTokens = 0;
    let completionTokens = 0;
    for (const row of rows) {
      calls += row.calls;
      promptTokens += row.prompt_tokens;
      completionTokens += row.completion_tokens;
      const existing = byScenario.get(row.scenario);
      if (existing) {
        existing.calls += row.calls;
        existing.promptTokens += row.prompt_tokens;
        existing.completionTokens += row.completion_tokens;
      } else {
        byScenario.set(row.scenario, {
          scenario: row.scenario,
          calls: row.calls,
          promptTokens: row.prompt_tokens,
          completionTokens: row.completion_tokens,
        });
      }
    }

    return {
      days,
      calls,
      promptTokens,
      completionTokens,
      byScenario: [...byScenario.values()].sort(
        (left, right) => right.calls - left.calls,
      ),
    };
  }

  clear(): void {
    this.db.run("DELETE FROM ai_usage_daily");
  }
}
