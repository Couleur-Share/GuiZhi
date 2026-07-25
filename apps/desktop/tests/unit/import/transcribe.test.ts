import { describe, expect, it, vi } from "vitest";

// transcribe 依赖 network-proxy（引用 electron），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { buildTranscriptionsEndpoint } from "../../../src/main/services/media/transcribe";

describe("buildTranscriptionsEndpoint", () => {
  it("无版本段的 base 补 /v1/audio/transcriptions", () => {
    expect(buildTranscriptionsEndpoint("https://api.openai.com")).toBe(
      "https://api.openai.com/v1/audio/transcriptions",
    );
  });

  it("已带版本段的 base 直接拼接", () => {
    expect(buildTranscriptionsEndpoint("https://gw.example.com/v1")).toBe(
      "https://gw.example.com/v1/audio/transcriptions",
    );
  });

  it("剥离 chat 端点后缀与显式 # 标记", () => {
    expect(
      buildTranscriptionsEndpoint("https://api.openai.com/v1/chat/completions"),
    ).toBe("https://api.openai.com/v1/audio/transcriptions");
    expect(buildTranscriptionsEndpoint("https://gw.example.com/v1#")).toBe(
      "https://gw.example.com/v1/audio/transcriptions",
    );
  });
});
