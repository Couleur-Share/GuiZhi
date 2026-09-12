/**
 * 混合检索：FTS 关键词 + embedding 语义，RRF（Reciprocal Rank Fusion）融合。
 * embedding 未配置或调用失败时静默退化为纯 FTS（保持既有行为）。
 */
import { withCancellation } from "../ai-transport";
import { abortable } from "@guizhi/shared/utils/abortable";
import type { QaSearchHit } from "./qa";
import { queryVector } from "./query-vectors";
import { resolveEmbeddingConfig } from "./embeddings";

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
    id: entry.id, reviewStatus: entry.reviewStatus, reviewReasons: entry.reviewReasons,
    title: entry.title || "无标题",
    snippet: entry.snippet ?? "",
  }));
}

async function searchBySemantic(
  query: string,
  limit: number,
  options: { signal?: AbortSignal; onWarning?: (message: string) => void },
): Promise<QaSearchHit[]> {
  const config = resolveEmbeddingConfig();
  if (!config) return [];
  const controller = new AbortController();
  const cancel = () => controller.abort();
  options.signal?.addEventListener("abort", cancel, { once: true });
  const timer = setTimeout(cancel, 3000);
  try {
    options.signal?.throwIfAborted();
    const vector = await queryVector(config, query, controller.signal);
    const hits = await abortable(withCancellation(window.api.ai, controller.signal, requestId => window.api.semantic.search({ model: config.model, vector, limit, requestId })), controller.signal);
    options.signal?.throwIfAborted();
    return hits.filter(hit => hit.score >= SEMANTIC_MIN_SCORE).map(hit => ({ id: hit.itemId, reviewStatus: hit.reviewStatus, reviewReasons: hit.reviewReasons, title: hit.title || "无标题", snippet: hit.snippet, matchText: hit.snippet }));
  } catch (error) {
    options.signal?.throwIfAborted();
    options.onWarning?.(controller.signal.aborted ? "语义检索等待超过 3 秒，本次使用本地关键词结果。" : "语义检索暂不可用，本次使用本地关键词结果。");
    return [];
  } finally { clearTimeout(timer); options.signal?.removeEventListener("abort", cancel); }
}

/** QA 检索入口：两路并发，RRF 融合 */
export async function hybridSearchItems(
  query: string,
  limit: number,
  options: { signal?: AbortSignal; onWarning?: (message: string) => void } = {},
): Promise<QaSearchHit[]> {
  options.signal?.throwIfAborted();
  const [ftsHits, semanticHits] = await abortable(Promise.all([
    searchByFts(query, limit),
    searchBySemantic(query, limit, options),
  ]), options.signal);
  options.signal?.throwIfAborted();
  if (semanticHits.length === 0) {
    return ftsHits;
  }
  return mergeHybridResults(ftsHits, semanticHits, limit);
}
