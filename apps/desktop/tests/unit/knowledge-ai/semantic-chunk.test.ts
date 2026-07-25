import { describe, expect, it } from "vitest";
import {
  buildEmbeddingChunks,
  SEMANTIC_CHUNK_OVERLAP,
  SEMANTIC_CHUNK_SIZE,
  SEMANTIC_MAX_CHUNKS,
} from "../../../src/renderer/services/knowledge-ai/semantic-chunk";

describe("buildEmbeddingChunks", () => {
  it("短内容单块，带标题前缀", () => {
    const chunks = buildEmbeddingChunks("标题", "内容不长", null);
    expect(chunks).toEqual(["《标题》\n内容不长"]);
  });

  it("空内容仅标题时返回标题；全空返回空数组", () => {
    expect(buildEmbeddingChunks("只有标题", "", null)).toEqual(["只有标题"]);
    expect(buildEmbeddingChunks("", "  ", null)).toEqual([]);
  });

  it("转写文本并入正文分块", () => {
    const chunks = buildEmbeddingChunks("t", "正文", "口播内容");
    expect(chunks[0]).toContain("口播内容");
    expect(chunks[0]).toContain("【口播转写稿】");
  });

  it("长内容滑动窗口分块且相邻块有重叠", () => {
    const body = "字".repeat(SEMANTIC_CHUNK_SIZE * 2);
    const chunks = buildEmbeddingChunks("", body, null);
    expect(chunks.length).toBeGreaterThan(1);
    // 相邻块重叠：上一块尾部与下一块头部相同
    const step = SEMANTIC_CHUNK_SIZE - SEMANTIC_CHUNK_OVERLAP;
    expect(chunks[0].slice(step)).toBe(
      chunks[1].slice(0, SEMANTIC_CHUNK_OVERLAP),
    );
  });

  it("超长内容截断到最大块数", () => {
    const body = "长".repeat(SEMANTIC_CHUNK_SIZE * (SEMANTIC_MAX_CHUNKS + 10));
    const chunks = buildEmbeddingChunks("", body, null);
    expect(chunks).toHaveLength(SEMANTIC_MAX_CHUNKS);
  });
});
