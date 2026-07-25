/** AI 用量统计（按天 × 场景 × 模型聚合后的投影） */

export interface AIUsageDailyRow {
  scenario: string;
  calls: number;
  promptTokens: number;
  completionTokens: number;
}

export interface AIUsageSummary {
  /** 统计窗口天数 */
  days: number;
  calls: number;
  promptTokens: number;
  completionTokens: number;
  byScenario: AIUsageDailyRow[];
}

export interface AIUsageRecordInput {
  scenario: string;
  model: string;
  promptTokens: number;
  completionTokens: number;
}
