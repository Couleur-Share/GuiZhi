import { describe, expect, it, vi } from "vitest";

// transcribe 依赖 network-proxy（引用 electron），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { buildTranscriptionsEndpoint } from "../../../src/main/services/media/transcribe";

describe("buildTranscriptionsEndpoint", () => {
  it("无版本段的 base 补 /v1/audio/transcriptions", () => {
    expect(buildTranscriptionsEndpoint({ apiUrl: "https://api.openai.com" })).toBe(
      "https://api.openai.com/v1/audio/transcriptions",
    );
  });

  it("已带版本段的 base 直接拼接", () => {
    expect(
      buildTranscriptionsEndpoint({ apiUrl: "https://gw.example.com/v1" }),
    ).toBe("https://gw.example.com/v1/audio/transcriptions");
  });

  it("剥离 chat 端点后缀与显式 # 标记", () => {
    expect(
      buildTranscriptionsEndpoint({
        apiUrl: "https://api.openai.com/v1/chat/completions",
      }),
    ).toBe("https://api.openai.com/v1/audio/transcriptions");
    expect(
      buildTranscriptionsEndpoint({ apiUrl: "https://gw.example.com/v1#" }),
    ).toBe("https://gw.example.com/v1");
  });

  it("不支持转写的协议返回空串，由调用方给出可读提示", () => {
    // Anthropic 没有转写 API，Gemini 的音频要走 generateContent 内联；
    // 此前一律拼 /v1/audio/transcriptions，用户只会撞上莫名的 404
    expect(
      buildTranscriptionsEndpoint({
        apiUrl: "https://api.anthropic.com",
        apiProtocol: "anthropic",
      }),
    ).toBe("");
    expect(
      buildTranscriptionsEndpoint({
        apiUrl: "https://generativelanguage.googleapis.com",
        apiProtocol: "gemini",
      }),
    ).toBe("");
  });

  it("按 provider / URL 推断协议", () => {
    expect(
      buildTranscriptionsEndpoint({
        apiUrl: "https://api.anthropic.com",
        provider: "anthropic",
      }),
    ).toBe("");
    expect(
      buildTranscriptionsEndpoint({ apiUrl: "https://api.anthropic.com" }),
    ).toBe("");
  });
});
