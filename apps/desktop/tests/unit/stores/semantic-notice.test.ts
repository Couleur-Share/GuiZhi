import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SemanticIndexRunResult } from "../../../src/renderer/services/knowledge-ai/semantic-index";

/** 本轮索引的结果，逐用例设定 */
let runResult: SemanticIndexRunResult = { skipped: true, indexed: 0, failed: 0 };

vi.mock("../../../src/renderer/services/knowledge-ai/semantic-index", () => ({
  runSemanticIndexing: vi.fn(async () => runResult),
}));

vi.mock("../../../src/renderer/services/knowledge-ai/embeddings", () => ({
  resolveEmbeddingConfig: () => ({
    apiUrl: "https://example.test/v1",
    apiKey: "test-key",
    model: "text-embedding-3-small",
  }),
  isSemanticConfigured: () => true,
}));

import { useSemanticStore } from "../../../src/renderer/stores/semantic.store";

beforeEach(() => {
  runResult = { skipped: true, indexed: 0, failed: 0 };
  window.api.semantic = {
    status: async () => ({
      indexedItems: 15,
      eligibleItems: 15,
      totalChunks: 42,
      lastSearchMs: null,
      lastScannedChunks: null,
    }),
  };
  useSemanticStore.setState({ isIndexing: false, indexedThisRun: 0, notice: null });
});

describe("semantic.store 索引回执", () => {
  it("后台轮次不留回执：用户没点过任何东西，不该在切到问答页时收到 toast", async () => {
    await useSemanticStore.getState().runIndexing(true);
    expect(useSemanticStore.getState().notice).toBeNull();

    // 后台轮次失败同样静默，等下一轮
    runResult = { skipped: false, indexed: 0, failed: 2, lastError: "HTTP 401" };
    await useSemanticStore.getState().runIndexing(true);
    expect(useSemanticStore.getState().notice).toBeNull();
  });

  it("手动轮次留下回执", async () => {
    await useSemanticStore.getState().runIndexing();
    expect(useSemanticStore.getState().notice).toMatchObject({ kind: "nothing" });

    runResult = { skipped: false, indexed: 3, failed: 0 };
    await useSemanticStore.getState().runIndexing();
    expect(useSemanticStore.getState().notice).toMatchObject({
      kind: "done",
      indexed: 3,
    });
  });

  it("同一条回执只能被取走一次", async () => {
    runResult = { skipped: false, indexed: 3, failed: 0 };
    await useSemanticStore.getState().runIndexing();

    expect(useSemanticStore.getState().consumeNotice()).toMatchObject({
      kind: "done",
    });
    expect(useSemanticStore.getState().consumeNotice()).toBeNull();
    expect(useSemanticStore.getState().notice).toBeNull();
  });
});
