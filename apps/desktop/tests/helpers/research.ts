import type { ResearchRunDetail, ResearchSource } from "@guizhi/shared/types";

export function researchFixture(): ResearchRunDetail {
  const now = Date.now();
  const sources: ResearchSource[] = ["bilibili", "douyin", "xiaohongshu"];
  return {
    run: {
      id: "run-1", topic: "GPT-6", dayRange: 7, rangeFrom: now - 7 * 86400000, rangeTo: now,
      depth: "quick", sources, status: "partial", reportStatus: "none", reportMarkdown: null,
      reportError: null, reportPromptVersion: null, savedItemId: null, candidateCount: 2,
      clusterCount: 0, createdAt: now - 50000, updatedAt: now, completedAt: now,
    },
    sources: sources.map((source, index) => ({
      runId: "run-1", source, status: index === 2 ? "failed" : "succeeded", method: "fixture",
      collectedCount: index === 2 ? 0 : 1, errorCode: index === 2 ? "navigation_timeout" : null,
      error: index === 2 ? "[navigation_timeout] 平台搜索页加载超时，请检查网络或代理后重试" : null,
      startedAt: now - 50000, finishedAt: now,
    })),
    candidates: sources.slice(0, 2).map((source, index) => ({
      id: `candidate-${index}`, runId: "run-1", source, externalId: `${index}`,
      url: `https://www.${source}.com/video/${index}`, normalizedUrl: `https://www.${source}.com/video/${index}`,
      title: index === 0 ? "B 站视频候选" : "抖音视频候选", author: "作者", snippet: "候选摘要",
      publishedAt: now - 86400000, dateConfidence: "high", mediaType: "video", engagement: {},
      discoveryMethod: "fixture", relevanceScore: 90, recencyScore: 85, engagementScore: 80,
      overallScore: 88, clusterId: null, state: "available", importTaskId: null, importedItemId: null,
      createdAt: now, updatedAt: now,
    })),
    clusters: [],
  };
}
