import { describe, expect, it, vi } from "vitest";
import type { SemanticVectorCache } from "../../../src/main/services/semantic-vector-cache";
import {
  ExactSemanticSearchBackend,
  HNSW_CHUNK_THRESHOLD,
  HNSW_VECTOR_BYTES_THRESHOLD,
  HNSW_WARM_MEDIAN_THRESHOLD_MS,
  resetSemanticBackendCompatibilityForTests,
  resolveSemanticSearchBackend,
  shouldUseHnsw,
} from "../../../src/main/services/semantic-search-backend";

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

describe("SemanticSearchBackend", () => {
  it("按 chunks、缓存字节或热查询中位耗时选择 ANN", () => {
    expect(shouldUseHnsw({ chunkCount: HNSW_CHUNK_THRESHOLD - 1, vectorBytes: 1, warmMedianMs: 1 })).toBe(false);
    expect(shouldUseHnsw({ chunkCount: HNSW_CHUNK_THRESHOLD, vectorBytes: 1, warmMedianMs: null })).toBe(true);
    expect(shouldUseHnsw({ chunkCount: 1, vectorBytes: HNSW_VECTOR_BYTES_THRESHOLD + 1, warmMedianMs: null })).toBe(true);
    expect(shouldUseHnsw({ chunkCount: 1, vectorBytes: 1, warmMedianMs: HNSW_WARM_MEDIAN_THRESHOLD_MS + 1 })).toBe(true);
  });

  it("精确后端按条目取最高分并稳定返回 top-k", async () => {
    const backend = new ExactSemanticSearchBackend(makeCache());
    await expect(backend.search(new Float32Array([1, 0]), 2)).resolves.toEqual([
      { itemId: "a", chunkIndex: 0, score: 1 },
      { itemId: "b", chunkIndex: 0, score: 0 },
    ]);
    await expect(backend.search(new Float32Array([1, 0, 0]), 2)).resolves.toEqual([]);
  });

  it("当前平台无法加载 WASM HNSW 时透明回退，并缓存兼容性结论", async () => {
    resetSemanticBackendCompatibilityForTests();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const input = {
      cache: makeCache(),
      model: "model-a",
      generation: "g1",
      rootDir: "unused-in-fallback",
      warmMedianMs: HNSW_WARM_MEDIAN_THRESHOLD_MS + 1,
    };
    const first = await resolveSemanticSearchBackend(input);
    const second = await resolveSemanticSearchBackend({ ...input, generation: "g2" });
    expect(first.name).toBe("exact");
    expect(second.name).toBe("exact");
    expect(
      warn.mock.calls.filter(([message]) => String(message).includes("保留精确扫描")),
    ).toHaveLength(1);
    warn.mockRestore();
  });
});
