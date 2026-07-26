import { describe, expect, it, vi } from "vitest";

// transcribe 依赖 network-proxy（引用 electron），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  buildTranscriptionsEndpoint,
  cleanTranscriptText,
} from "../../../src/main/services/media/transcribe";

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

describe("cleanTranscriptText", () => {
  it("折叠 VAD 段边界叠出的双标点，句末标点优先", () => {
    // 实测样本：前一段收尾的逗号撞上后一段起头的句号
    expect(cleanTranscriptText("所谓的高手，。 都是只有一个")).toBe(
      "所谓的高手。都是只有一个",
    );
    expect(cleanTranscriptText("好的。，接着说")).toBe("好的。接着说");
    expect(cleanTranscriptText("这样，，然后呢")).toBe("这样，然后呢");
    expect(cleanTranscriptText("真的吗？。")).toBe("真的吗？");
  });

  it("去掉 CJK 之间的切段空格，中日韩都适用", () => {
    expect(cleanTranscriptText("我刚才说的四个字 足够 好谢谢")).toBe(
      "我刚才说的四个字足够好谢谢",
    );
    expect(cleanTranscriptText("うちの中学は弁当制で持っ ていけない")).toBe(
      "うちの中学は弁当制で持っていけない",
    );
    expect(cleanTranscriptText("呢几个字 都表达唔到")).toBe(
      "呢几个字都表达唔到",
    );
  });

  it("不动 ASCII 文本的空格与标点", () => {
    const english =
      "the tribal chieftain called for the boy and presented him with 50 pieces of gold.";
    expect(cleanTranscriptText(english)).toBe(english);
    // 中英混排时西文词两侧的空格是必需的，不能当噪音删掉
    expect(cleanTranscriptText("这是 GitHub 上的仓库。")).toBe(
      "这是 GitHub 上的仓库。",
    );
  });

  it("对已排版规整的云端结果是幂等的", () => {
    const clean = "今天天气很好，我们去公园吧。你觉得呢？";
    expect(cleanTranscriptText(clean)).toBe(clean);
    expect(cleanTranscriptText(cleanTranscriptText(clean))).toBe(clean);
  });

  it("首尾空白被清掉，空输入得到空串", () => {
    expect(cleanTranscriptText("  文字稿正文  ")).toBe("文字稿正文");
    expect(cleanTranscriptText("   ")).toBe("");
  });
});
