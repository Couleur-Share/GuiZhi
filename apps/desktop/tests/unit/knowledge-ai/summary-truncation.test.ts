import { beforeEach, describe, expect, it, vi } from "vitest";
import { generateSummary } from "../../../src/renderer/services/knowledge-ai/summarize";
import { SUMMARY_CHUNK_SIZE } from "../../../src/renderer/services/knowledge-ai/prompts";

const { runScenarioChat } = vi.hoisted(() => ({ runScenarioChat: vi.fn() }));
vi.mock("../../../src/renderer/services/knowledge-ai/ai-invoke", () => ({
  runScenarioChat,
  AiNotConfiguredError: class AiNotConfiguredError extends Error {},
}));

const LONG_TEXT = "内".repeat(SUMMARY_CHUNK_SIZE + 100);

beforeEach(() => {
  runScenarioChat.mockReset();
});

describe("generateSummary 的截断标记", () => {
  it("正常收尾不标截断", async () => {
    runScenarioChat.mockResolvedValue({
      content: "- 要点",
      model: "m",
      finishReason: "stop",
    });

    await expect(generateSummary("标题", "短正文")).resolves.toEqual({
      text: "- 要点",
      truncated: false,
    });
  });

  it("单发撞上 max_tokens → 内容照常返回，但标为可能不完整", async () => {
    runScenarioChat.mockResolvedValue({
      content: "- 要点一\n- 要点二写到一半",
      model: "m",
      finishReason: "length",
    });

    const result = await generateSummary("标题", "短正文");
    expect(result.text).toContain("要点一");
    expect(result.truncated).toBe(true);
  });

  it("map 阶段某一片段被截断 → 即使 reduce 正常收尾也要标注", async () => {
    // 片段要点残缺，汇总出来的摘要同样缺内容，只看最后一次调用会漏报
    runScenarioChat
      .mockResolvedValueOnce({
        content: "- 片段一要点",
        model: "m",
        finishReason: "length",
      })
      .mockResolvedValueOnce({
        content: "- 片段二要点",
        model: "m",
        finishReason: "stop",
      })
      .mockResolvedValueOnce({
        content: "- 汇总要点",
        model: "m",
        finishReason: "stop",
      });

    const result = await generateSummary("标题", LONG_TEXT);
    expect(runScenarioChat).toHaveBeenCalledTimes(3);
    expect(result.text).toBe("- 汇总要点");
    expect(result.truncated).toBe(true);
  });
});
