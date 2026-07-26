/**
 * 混合检索：FTS 关键词 + embedding 语义，RRF（Reciprocal Rank Fusion）融合。
 * embedding 未配置或调用失败时静默退化为纯 FTS（保持既有行为）。
 */
import type { QaSearchHit } from "./qa";
import { embedTexts, resolveEmbeddingConfig } from "./embeddings";

const RRF_K = 60;
/** 语义命中的最低余弦相似度，低于此值多为噪声 */
const SEMANTIC_MIN_SCORE = 0.25;

/**
 * RRF 融合两路排名：score(doc) = Σ 1/(k + rank)。
 * 元数据（标题/摘录）优先取 FTS 结果，语义独有命中用其分块节选。
 *
 * matchText 单独并进来：两路都命中时元数据归 FTS，但 FTS 的 snippet 是
 * 文档开头、当不了定位线索，语义那份命中片段不能跟着一起被丢掉。
 */
export function mergeHybridResults(
  ftsHits: QaSearchHit[],
  semanticHits: QaSearchHit[],
  limit: number,
): QaSearchHit[] {
  const scores = new Map<string, number>();
  const meta = new Map<string, QaSearchHit>();
  const matchTexts = new Map<string, string>();

  const accumulate = (hits: QaSearchHit[], preferMeta: boolean) => {
    hits.forEach((hit, rank) => {
      scores.set(hit.id, (scores.get(hit.id) ?? 0) + 1 / (RRF_K + rank + 1));
      if (preferMeta || !meta.has(hit.id)) {
        meta.set(hit.id, hit);
      }
      if (hit.matchText && !matchTexts.has(hit.id)) {
        matchTexts.set(hit.id, hit.matchText);
      }
    });
  };
  accumulate(ftsHits, true);
  accumulate(semanticHits, false);

  return [...scores.entries()]
    .sort((left, right) => right[1] - left[1])
    .slice(0, Math.max(1, limit))
    .map(([id]) => {
      const hit = meta.get(id);
      if (!hit) {
        return null;
      }
      const matchText = matchTexts.get(id);
      return matchText ? { ...hit, matchText } : hit;
    })
    .filter((hit): hit is QaSearchHit => hit !== null);
}

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
