/**
 * 混合检索：FTS 关键词 + embedding 语义，RRF（Reciprocal Rank Fusion）融合。
 * embedding 未配置或调用失败时静默退化为纯 FTS（保持既有行为）。
 */
import type { QaSearchHit } from "./qa";
import { embedTexts, resolveEmbeddingConfig } from "./embeddings";

import { mergeHybridResults } from "@guizhi/shared/utils/hybrid-results";
export { mergeHybridResults } from "@guizhi/shared/utils/hybrid-results";
const SEMANTIC_MIN_SCORE = 0.25;

async function searchByFts(query: string, limit: number): Promise<QaSearchHit[]> {
  const result = await window.api.knowledge.list({
    scope: "all",
    search: query,
    // 自然语言问句走默认的 phrase 模式会被编译成一个逐字相邻的长 phrase，
    // 中文没有空格，结果必然是零命中
    searchMode: "recall",
    includeArchived: true,
    limit,
  });
  return result.entries.map((entry) => ({
    id: entry.id,
    title: entry.title || "无标题",
    snippet: entry.snippet ?? "",
  }));
}

async function searchBySemantic(
  query: string,
  limit: number,
): Promise<QaSearchHit[]> {
  const config = resolveEmbeddingConfig();
  if (!config) {
    return [];
  }
  try {
    const [vector] = await embedTexts(config, [query]);
    const hits = await window.api.semantic.search({
      model: config.model,
      vector,
      limit,
    });
    return hits
      .filter((hit) => hit.score >= SEMANTIC_MIN_SCORE)
      .map((hit) => ({
        id: hit.itemId,
        title: hit.title || "无标题",
        snippet: hit.snippet,
        // 命中的正是这一段，阅读时据此定位窗口
        matchText: hit.snippet,
      }));
  } catch (error) {
    // 语义检索是增强路径：失败不影响问答（FTS 兜底）
    console.warn(
      "[semantic] 语义检索失败，退化为关键词检索:",
      error instanceof Error ? error.message : error,
    );
    return [];
  }
}

/** QA 检索入口：两路并发，RRF 融合 */
export async function hybridSearchItems(
  query: string,
  limit: number,
): Promise<QaSearchHit[]> {
  const [ftsHits, semanticHits] = await Promise.all([
    searchByFts(query, limit),
    searchBySemantic(query, limit),
  ]);
  if (semanticHits.length === 0) {
    return ftsHits;
  }
  return mergeHybridResults(ftsHits, semanticHits, limit);
}
