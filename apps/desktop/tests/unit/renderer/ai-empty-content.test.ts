import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chatCompletion } from "../../../src/renderer/services/ai";

const CONFIG = {
  provider: "custom",
  apiProtocol: "openai" as const,
  apiKey: "test-key",
  apiUrl: "https://example.test/v1",
  model: "reasoning-flash",
};

/** 走 fetch 回退路径：清掉 window.api 后 getAITransport() 返回 null */
let savedApi: unknown;

function mockResponse(payload: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      statusText: "OK",
      headers: new Headers({ "content-type": "application/json" }),
      json: async () => payload,
      text: async () => JSON.stringify(payload),
    }),
  );
}

beforeEach(() => {
  savedApi = window.api;
  (window as unknown as { api: unknown }).api = undefined;
});

afterEach(() => {
  (window as unknown as { api: unknown }).api = savedApi;
  vi.unstubAllGlobals();
});

describe("chatCompletion 的空正文处理", () => {
  it("思考模型耗尽 max_tokens 只回推理内容时报错，而不是返回空串", async () => {
    // 实测形态：HTTP 200、choices 齐全、content 是空字符串，
    // 输出预算全被 reasoning_tokens 吃光。放行会静默写入空摘要。
    mockResponse({
      id: "1",
      choices: [
        {
          index: 0,
          message: {
            role: "assistant",
            content: "",
            reasoning_content: "我们需要提取要点……",
          },
          finish_reason: "length",
        },
      ],
      usage: { prompt_tokens: 3215, completion_tokens: 799, total_tokens: 4014 },
    });

    await expect(
      chatCompletion(CONFIG, [{ role: "user", content: "总结" }], {
        maxTokens: 800,
      }),
    ).rejects.toThrow(/思考过程.*没有产出正文/);
  });

  it("content 为 null 时按空正文报错，不撞成 TypeError", async () => {
    mockResponse({
      id: "1",
      choices: [
        { index: 0, message: { role: "assistant", content: null }, finish_reason: "stop" },
      ],
    });

    await expect(
      chatCompletion(CONFIG, [{ role: "user", content: "总结" }]),
    ).rejects.toThrow(/空正文/);
  });

  it("allowEmptyContent 放行连通性探针", async () => {
    mockResponse({
      id: "1",
      choices: [
        { index: 0, message: { role: "assistant", content: "" }, finish_reason: "length" },
      ],
    });

    const result = await chatCompletion(
      CONFIG,
      [{ role: "user", content: "ping" }],
      { maxTokens: 8, allowEmptyContent: true },
    );
    expect(result.content).toBe("");
    expect(result.finishReason).toBe("length");
  });

  it("正常输出照常返回并带上 finish_reason", async () => {
    mockResponse({
      id: "1",
      choices: [
        {
          index: 0,
          message: { role: "assistant", content: "- 要点一\n- 要点二" },
          finish_reason: "stop",
        },
      ],
      usage: { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120 },
    });

    const result = await chatCompletion(CONFIG, [
      { role: "user", content: "总结" },
    ]);
    expect(result.content).toContain("要点一");
    expect(result.finishReason).toBe("stop");
    expect(result.usage).toEqual({ promptTokens: 100, completionTokens: 20 });
  });
});
