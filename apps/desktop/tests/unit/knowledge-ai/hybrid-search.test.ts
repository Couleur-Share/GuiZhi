import { describe, expect, it } from "vitest";
import { mergeHybridResults } from "../../../src/renderer/services/knowledge-ai/hybrid-search";
import type { QaSearchHit } from "../../../src/renderer/services/knowledge-ai/qa";

function hit(id: string, snippet = `snippet-${id}`): QaSearchHit {
  return { id, title: `title-${id}`, snippet };
}

describe("mergeHybridResults（RRF 融合）", () => {
  it("两路都命中的文档排名高于单路命中", () => {
    const fts = [hit("a"), hit("b"), hit("c")];
    const semantic = [hit("d"), hit("b")];

    const merged = mergeHybridResults(fts, semantic, 10);
    // b：FTS 第 2 + 语义第 2；a：仅 FTS 第 1；b 的融合分应最高
    expect(merged[0].id).toBe("b");
    expect(merged.map((entry) => entry.id)).toContain("d");
  });

  it("同一文档去重且元数据优先取 FTS 结果", () => {
    const fts = [hit("a", "FTS 摘录")];
    const semantic = [hit("a", "语义分块节选")];

    const merged = mergeHybridResults(fts, semantic, 10);
    expect(merged).toHaveLength(1);
    expect(merged[0].snippet).toBe("FTS 摘录");
  });

  it("语义独有命中保留其分块节选，limit 生效", () => {
    const fts = [hit("a"), hit("b")];
    const semantic = [hit("x", "向量命中节选")];

    const merged = mergeHybridResults(fts, semantic, 2);
    expect(merged).toHaveLength(2);

    const full = mergeHybridResults(fts, semantic, 10);
    const semanticOnly = full.find((entry) => entry.id === "x");
    expect(semanticOnly?.snippet).toBe("向量命中节选");
  });

  it("语义结果为空时保持 FTS 原序", () => {
    const fts = [hit("a"), hit("b"), hit("c")];
    const merged = mergeHybridResults(fts, [], 10);
    expect(merged.map((entry) => entry.id)).toEqual(["a", "b", "c"]);
  });
});
