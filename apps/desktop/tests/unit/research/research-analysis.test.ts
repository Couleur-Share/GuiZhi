import { describe, expect, it } from "vitest";
import type { ResearchCandidate } from "@guizhi/shared/types";
import { analyzeResearchCandidates, researchTokens } from "@guizhi/shared/utils/research-analysis";

function candidate(
  id: string,
  source: ResearchCandidate["source"],
  title: string,
  overrides: Partial<ResearchCandidate> = {},
): ResearchCandidate {
  return {
    id,
    runId: "run",
    source,
    externalId: id,
    url: `https://example.com/${id}`,
    normalizedUrl: `https://example.com/${id}`,
    title,
    author: "",
    snippet: "",
    publishedAt: null,
    dateConfidence: "low",
    mediaType: "video",
    engagement: {},
    discoveryMethod: "fixture",
    relevanceScore: 0,
    recencyScore: 0,
    engagementScore: 0,
    overallScore: 0,
    clusterId: null,
    state: "available",
    importTaskId: null,
    importedItemId: null,
    createdAt: 1,
    updatedAt: 1,
    ...overrides,
  };
}

describe("research analysis", () => {
  it("同时生成 CJK bigram 与小写字母数字 token", () => {
    expect([...researchTokens("本地 AI Agent")]).toEqual(expect.arrayContaining(["本", "地", "本地", "ai", "agent"]));
  });

  it("未知日期和全缺失互动使用基线并施加扣分", () => {
    const result = analyzeResearchCandidates("本地 AI", 0, 1000, [
      candidate("a", "douyin", "本地 AI"),
    ]);
    expect(result.candidates[0]).toMatchObject({
      relevanceScore: 100,
      recencyScore: 35,
      engagementScore: 35,
      overallScore: 56,
    });
  });

  it("互动百分位仅在同来源内计算且稳定处理并列", () => {
    const result = analyzeResearchCandidates("主题", 0, 100, [
      candidate("a", "bilibili", "主题 A", { engagement: { views: 10 }, publishedAt: 50, dateConfidence: "high" }),
      candidate("b", "bilibili", "主题 B", { engagement: { views: 10 }, publishedAt: 50, dateConfidence: "high" }),
      candidate("c", "bilibili", "主题 C", { engagement: { views: 1000 }, publishedAt: 50, dateConfidence: "high" }),
    ]);
    const scores = Object.fromEntries(result.candidates.map((item) => [item.id, item.engagementScore]));
    expect(scores).toEqual({ c: 100, a: 25, b: 25 });
  });

  it("只把跨来源且超过阈值的标题组成聚合热点", () => {
    const result = analyzeResearchCandidates("本地知识库", 0, 100, [
      candidate("x", "xiaohongshu", "本地 AI 知识库搭建指南"),
      candidate("d", "douyin", "本地 AI 知识库搭建教程"),
      candidate("b", "bilibili", "完全不同的旅行记录"),
    ]);
    expect(result.clusters).toHaveLength(1);
    expect(result.clusters[0].sourceCount).toBe(2);
    expect(result.candidates.filter((item) => item.clusterId)).toHaveLength(2);
  });
});
