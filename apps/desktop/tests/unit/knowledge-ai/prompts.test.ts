import { describe, expect, it } from "vitest";
import {
  MAX_CONTENT_LENGTH,
  SUMMARY_CHUNK_SIZE,
  SUMMARY_MAX_CHUNKS,
  buildUserPrompt,
  chunkContent,
  truncateText,
} from "../../../src/renderer/services/knowledge-ai/prompts";
import { parseTagResponse } from "../../../src/renderer/services/knowledge-ai/suggest-tags";

describe("truncateText", () => {
  it("短文本原样返回", () => {
    expect(truncateText("你好", 100)).toBe("你好");
  });

  it("超长截断并附标注", () => {
    const result = truncateText("a".repeat(200), 100);
    expect(result).toHaveLength(100 + "…（已截断）".length);
    expect(result.endsWith("…（已截断）")).toBe(true);
  });

  it("不切断代理对（emoji）", () => {
    const text = `${"a".repeat(99)}😀更多内容`;
    const result = truncateText(text, 100);
    // 😀 是代理对，第 100 个 code unit 是高位代理，应整体舍弃
    expect(result.startsWith("a".repeat(99))).toBe(true);
    expect(result).not.toContain("\ud83d");
  });
});

describe("buildUserPrompt", () => {
  it("组装标题与正文，超长正文截断", () => {
    const prompt = buildUserPrompt("测试标题", "b".repeat(10000));
    expect(prompt).toContain("标题：测试标题");
    expect(prompt).toContain("…（已截断）");
    expect(prompt.length).toBeLessThan(MAX_CONTENT_LENGTH + 100);
  });
});

describe("chunkContent", () => {
  it("短文单块", () => {
    expect(chunkContent("短内容")).toEqual(["短内容"]);
  });

  it("长文按块切分", () => {
    const content = "x".repeat(SUMMARY_CHUNK_SIZE * 2 + 100);
    const chunks = chunkContent(content);
    expect(chunks).toHaveLength(3);
    expect(chunks[0]).toHaveLength(SUMMARY_CHUNK_SIZE);
    expect(chunks[2]).toHaveLength(100);
  });

  it("超出最大块数的部分截断", () => {
    const content = "x".repeat(SUMMARY_CHUNK_SIZE * (SUMMARY_MAX_CHUNKS + 3));
    expect(chunkContent(content)).toHaveLength(SUMMARY_MAX_CHUNKS);
  });
});

describe("parseTagResponse", () => {
  it("解析中文逗号分隔标签", () => {
    expect(parseTagResponse("知识管理，效率工具，笔记")).toEqual([
      "知识管理",
      "效率工具",
      "笔记",
    ]);
  });

  it("容忍编号、井号与引号污染", () => {
    expect(parseTagResponse('1. #前端\n2. "React"\n3. 性能优化')).toEqual([
      "前端",
      "React",
      "性能优化",
    ]);
  });

  it("去重并过滤超长项", () => {
    expect(
      parseTagResponse("工具，工具，这是一个特别长的不合规标签内容啊"),
    ).toEqual(["工具"]);
  });
});
