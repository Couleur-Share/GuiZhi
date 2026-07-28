import { describe, expect, it, vi } from "vitest";
import { extractUsageFromChatResponse } from "@guizhi/shared/utils/ai-protocol";

vi.mock("electron", () => ({ app: {}, session: { defaultSession: {} } }));

describe("extractUsageFromChatResponse", () => {
  it("openai 系读 prompt_tokens / completion_tokens", () => {
    expect(
      extractUsageFromChatResponse(
        { usage: { prompt_tokens: 120, completion_tokens: 45 } },
        "openai",
      ),
    ).toEqual({ promptTokens: 120, completionTokens: 45 });
  });

  it("anthropic 读 input_tokens / output_tokens", () => {
    expect(
      extractUsageFromChatResponse(
        { usage: { input_tokens: 200, output_tokens: 60 } },
        "anthropic",
      ),
    ).toEqual({ promptTokens: 200, completionTokens: 60 });
    // 用错协议就会取到 undefined——恒为 0 在面板上和「接口不回报」分不开
    expect(
      extractUsageFromChatResponse(
        { usage: { input_tokens: 200, output_tokens: 60 } },
        "openai",
      ),
    ).toBeUndefined();
  });

  it("gemini 原生的 usageMetadata 也认", () => {
    expect(
      extractUsageFromChatResponse(
        { usageMetadata: { promptTokenCount: 30, candidatesTokenCount: 12 } },
        "gemini",
      ),
    ).toEqual({ promptTokens: 30, completionTokens: 12 });
  });

  it("接口不回报用量时返回 undefined，只留调用次数", () => {
    expect(extractUsageFromChatResponse({}, "openai")).toBeUndefined();
    expect(extractUsageFromChatResponse(null, "openai")).toBeUndefined();
    expect(
      extractUsageFromChatResponse({ usage: "nonsense" }, "openai"),
    ).toBeUndefined();
  });

  it("只回报一半时另一半按 0 计，不整条丢弃", () => {
    expect(
      extractUsageFromChatResponse({ usage: { prompt_tokens: 88 } }, "openai"),
    ).toEqual({ promptTokens: 88, completionTokens: 0 });
  });
});

describe("recordMainAiUsage", () => {
  it("数据库未初始化时静默跳过，既不抛错也不刷日志", async () => {
    const { recordMainAiUsage } = await import(
      "../../../src/main/services/ai-usage"
    );
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    // 单测与备份恢复期间都是这个状态：没什么可记的，但绝不能连累主流程
    expect(() =>
      recordMainAiUsage({ scenario: "illustration", model: "gpt-image-2" }),
    ).not.toThrow();
    expect(warn).not.toHaveBeenCalled();

    warn.mockRestore();
  });
});
