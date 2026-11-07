import { describe, expect, it } from "vitest";
import type { SemanticVectorCache } from "../../../src/main/services/semantic-vector-cache";
import { ExactSemanticSearchBackend } from "../../../src/main/services/semantic-search-backend";

function makeCache(): SemanticVectorCache {
  return {
    model: "model-a",
    dims: 2,
    vectors: new Float32Array([
      1, 0,
      0.8, 0.2,
      0, 1,
      -1, 0,
    ]),
    itemIds: ["a", "a", "b", "c"],
    chunkIndexes: [0, 1, 0, 0],
  };
}

describe("ExactSemanticSearchBackend", () => {
  it("按条目取最高分并稳定返回 top-k", async () => {
    const backend = new ExactSemanticSearchBackend(makeCache());
    await expect(backend.search(new Float32Array([1, 0]), 2)).resolves.toEqual([
      { itemId: "a", chunkIndex: 0, score: 1 },
      { itemId: "b", chunkIndex: 0, score: 0 },
    ]);
    await expect(backend.search(new Float32Array([1, 0, 0]), 2)).resolves.toEqual([]);
  });

  it("已取消的检索立即拒绝", async () => {
    const backend = new ExactSemanticSearchBackend(makeCache());
    const controller = new AbortController();
    controller.abort();
    await expect(backend.search(new Float32Array([1, 0]), 2, controller.signal)).rejects.toThrow();
  });
});
