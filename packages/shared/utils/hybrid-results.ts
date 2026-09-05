export interface QaSearchHit { id: string; title: string; snippet: string; matchText?: string; }
const RRF_K = 60;
/** 语义命中的最低余弦相似度，低于此值多为噪声 */


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

