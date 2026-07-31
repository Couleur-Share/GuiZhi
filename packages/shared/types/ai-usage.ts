/** AI 用量统计（按天 × 场景 × 模型聚合后的投影） */

/**
 * 用量面板的场景词表。
 *
 * 它刻意不复用 settings 的 `AIUsageScenario`——那个类型的含义是「用户可以
 * 为其单独指定模型的场景」，而这里是「面板上要分几栏」。两者原本重合，
 * 加上排版、配图、embedding 之后不再重合：这三项各自绑死在一条路由上
 * （fastText / imageGen / embedding），做成可指定模型的场景没有意义，
 * 但它们实实在在地花钱，必须在面板上有一栏。
 *
 * settings 的那个类型是本词表的子集，可以直接赋值过来。
 */
export const AI_USAGE_SCENARIOS = [
  "qa",
  "wiki",
  "summary",
  "tagging",
  "ocr",
  "transcription",
  "formatting",
  "embedding",
  "illustration",
] as const;

export type AIUsageScenarioId = (typeof AI_USAGE_SCENARIOS)[number];

export interface AIUsageDailyRow {
  scenario: string;
  calls: number;
  /** 其中失败的次数（超时与限流同样产生费用，不记就会低估消耗） */
  failedCalls: number;
  promptTokens: number;
  completionTokens: number;
}

/** 按模型名聚合；同名模型跨场景合并（写入时记的是 API 模型名） */
export interface AIUsageModelRow {
  model: string;
  calls: number;
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
  byModel: AIUsageModelRow[];
}

export interface AIUsageRecordInput {
  scenario: string;
  model: string;
  promptTokens: number;
  completionTokens: number;
  /** 调用失败（仍然计入次数） */
  failed?: boolean;
}
