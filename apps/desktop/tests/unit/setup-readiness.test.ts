import { describe, expect, it } from "vitest";
import type { AIModelConfig } from "../../src/renderer/stores/settings.store";
import {
  buildSetupChecklist,
  isCoreTextModelReady,
  isEmbeddingReady,
  isLegacyTextModelReady,
  isTranscriptionReady,
  setupItemSettingsSection,
} from "../../src/renderer/services/setup-readiness";

function chatModel(overrides: Partial<AIModelConfig> = {}): AIModelConfig {
  return {
    id: "chat-1",
    provider: "openai",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl: "https://api.example.com/v1",
    model: "gpt-4o-mini",
    isDefault: true,
    ...overrides,
  };
}

function embeddingModel(overrides: Partial<AIModelConfig> = {}): AIModelConfig {
  return {
    id: "emb-1",
    provider: "openai",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl: "https://api.example.com/v1",
    model: "text-embedding-3-small",
    capabilities: { chat: false, embedding: true },
    ...overrides,
  };
}

function audioModel(overrides: Partial<AIModelConfig> = {}): AIModelConfig {
  return {
    id: "audio-1",
    provider: "openai",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl: "https://api.example.com/v1",
    model: "whisper-1",
    capabilities: { chat: false, audioTranscription: true },
    ...overrides,
  };
}

const emptyLegacy = {
  aiProvider: "",
  aiApiKey: "",
  aiApiUrl: "",
  aiModel: "",
};

describe("setup-readiness", () => {
  it("核心文本：mainText 或 fastText 配置齐全即就绪", () => {
    expect(isCoreTextModelReady([], undefined)).toBe(false);
    expect(isCoreTextModelReady([chatModel()], undefined)).toBe(true);
    expect(
      isCoreTextModelReady([chatModel({ isDefault: false })], {
        fastText: "chat-1",
      }),
    ).toBe(true);
  });

  it("核心文本：legacy 四字段齐全可兜底", () => {
    expect(isLegacyTextModelReady(emptyLegacy)).toBe(false);
    expect(
      isCoreTextModelReady([], undefined, {
        aiProvider: "openai",
        aiApiKey: "sk",
        aiApiUrl: "https://x",
        aiModel: "gpt",
      }),
    ).toBe(true);
  });

  it("转写：FunASR 已装或 audioText 显式路由", () => {
    expect(isTranscriptionReady([], undefined, false)).toBe(false);
    expect(isTranscriptionReady([], undefined, true)).toBe(true);

    const models = [audioModel()];
    // audioText 无显式路由时不回退到候选列表
    expect(isTranscriptionReady(models, undefined, false)).toBe(false);
    expect(
      isTranscriptionReady(models, { audioText: "audio-1" }, false),
    ).toBe(true);
  });

  it("embedding 必须显式能力 + 可解析路由", () => {
    expect(isEmbeddingReady([chatModel()], undefined)).toBe(false);
    expect(isEmbeddingReady([embeddingModel()], undefined)).toBe(true);
  });

  it("buildSetupChecklist 映射四项就绪状态", () => {
    const items = buildSetupChecklist({
      aiModels: [chatModel(), audioModel(), embeddingModel()],
      modelRouteDefaults: { audioText: "audio-1" },
      legacy: emptyLegacy,
      funasrInstalled: false,
      ytdlpInstalled: true,
    });
    expect(items.map((item) => [item.id, item.ready, item.required])).toEqual([
      ["textModel", true, true],
      ["transcription", true, false],
      ["ytdlp", true, false],
      ["embedding", true, false],
    ]);
  });

  it("转写双路径：仅 FunASR、仅云端路由各自算就绪", () => {
    expect(
      buildSetupChecklist({
        aiModels: [],
        modelRouteDefaults: undefined,
        funasrInstalled: true,
        ytdlpInstalled: false,
      }).find((item) => item.id === "transcription")?.ready,
    ).toBe(true);

    expect(
      buildSetupChecklist({
        aiModels: [audioModel()],
        modelRouteDefaults: { audioText: "audio-1" },
        funasrInstalled: false,
        ytdlpInstalled: false,
      }).find((item) => item.id === "transcription")?.ready,
    ).toBe(true);
  });

  it("清单项 CTA 分区", () => {
    expect(setupItemSettingsSection("textModel")).toBe("ai");
    expect(setupItemSettingsSection("embedding")).toBe("ai");
    expect(setupItemSettingsSection("transcription")).toBe("general");
    expect(setupItemSettingsSection("ytdlp")).toBe("general");
  });
});
