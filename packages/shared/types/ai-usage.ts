/** AI 用量统计（按天 × 场景 × 模型聚合后的投影） */

export interface AIUsageDailyRow {
  scenario: string;
  calls: number;
  /** 其中失败的次数（超时与限流同样产生费用，不记就会低估消耗） */
  failedCalls: number;
  promptTokens: number;
  completionTokens: number;
}

export interface AIUsageSummary {
  /** 统计窗口天数 */
  days: number;
  calls: number;
  failedCalls: number;
  promptTokens: number;
  completionTokens: number;
  byScenario: AIUsageDailyRow[];
}

export interface AIUsageRecordInput {
  scenario: string;
  model: string;
  promptTokens: number;
  completionTokens: number;
  /** 调用失败（仍然计入次数） */
  failed?: boolean;
}
