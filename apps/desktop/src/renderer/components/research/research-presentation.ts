import type { ResearchCandidate, ResearchRunDetail, ResearchRunStatus, ResearchSource, ResearchSourceRun, ResearchSourceStatus } from "@guizhi/shared/types";

/** 只清理末尾话题串；正文中的话题、C#、工单编号与 URL 片段仍有语义。 */
function splitTrailingTopics(value: string): { text: string; topics: string[] } {
  const text = value.trim();
  const suffix = /(?:[#＃][\p{L}\p{M}\p{N}_]+\s*)+$/u.exec(text);
  if (!suffix) return { text, topics: [] };
  const prefix = text.slice(0, suffix.index);
  if (/[a-z0-9_/#＃]$/i.test(prefix) || /(?:https?:\/\/|www\.)\S*$/i.test(prefix)) {
    return { text, topics: [] };
  }
  const topics = [...suffix[0].matchAll(/[#＃]([\p{L}\p{M}\p{N}_]+)/gu)].map((match) => match[1]);
  if (!topics.some((topic) => /[\p{L}\p{M}_]/u.test(topic))) return { text, topics: [] };
  return { text: prefix.trim(), topics };
}

/** 保留原始候选供评分、报告与导入使用；展示清理同时适用于历史记录。 */
export function candidateDisplayText(candidate: Pick<ResearchCandidate, "source" | "title" | "snippet">): { title: string; snippet: string } {
  if (candidate.source !== "douyin") return { title: candidate.title, snippet: candidate.snippet };
  const cleaned = splitTrailingTopics(candidate.title);
  // 纯话题作品仍用话题文字作标题，避免变成无法辨认的空白条目。
  const title = cleaned.text || cleaned.topics.join(" · ") || "抖音作品";
  const snippet = splitTrailingTopics(candidate.snippet).text;
  const compact = (text: string) => text.replace(/\s+/g, " ").trim();
  return { title, snippet: compact(snippet) === compact(title) ? "" : snippet };
}

export const SOURCE_NAMES: Record<ResearchSource, string> = {
  web: "网页",
  xiaohongshu: "小红书", douyin: "抖音", bilibili: "哔哩哔哩",
};
export const RUN_STATUS_NAMES: Record<ResearchRunStatus, string> = {
  collecting: "正在采集", ready: "采集完成", partial: "采集结束 · 部分平台异常", failed: "采集失败", canceled: "已取消采集",
};
const SOURCE_STATUS_NAMES: Record<ResearchSourceStatus, string> = {
  pending: "等待采集", running: "正在采集", succeeded: "采集完成", partial: "部分完成", login_required: "需要登录", failed: "采集失败", canceled: "已取消",
};
export function sourceStatusName(source: ResearchSourceRun): string {
  return source.status === "succeeded" && source.collectedCount === 0 ? "未找到候选" : SOURCE_STATUS_NAMES[source.status];
}
export function sourceDescription(source: ResearchSourceRun): string {
  if (source.source === "douyin" && source.errorCode === "verification_required") return "抖音搜索页要求安全验证，验证通过后会自动补采抖音。";
  if (source.error) return source.error.replace(/^\[[^\]]+\]\s*/, "");
  if (source.status === "pending") return "等待可用的采集窗口";
  if (source.status === "running") return source.progress || "正在加载搜索页并读取候选";
  if (source.status === "succeeded" && source.collectedCount === 0) return "本次搜索未找到所选时间范围内的候选，可扩大时间范围或调整关键词重试。";
  if (source.status === "canceled") return "已停止采集，已取得的候选仍保留。";
  return `已收集 ${source.collectedCount} 条候选`;
}
export function researchSummary(detail: ResearchRunDetail): string {
  const done = detail.sources.filter((source) => source.status !== "pending" && source.status !== "running").length;
  const covered = detail.sources.filter((source) => source.collectedCount > 0).length;
  return detail.run.status === "collecting"
    ? `已结束 ${done}/${detail.sources.length} 个平台 · 已收集 ${detail.candidates.length} 条候选`
    : `${detail.candidates.length} 条候选 · ${covered}/${detail.sources.length} 个平台有结果`;
}
export function elapsedTime(start: number, end: number): string {
  const seconds = Math.max(0, Math.floor((end - start) / 1000));
  return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
}
