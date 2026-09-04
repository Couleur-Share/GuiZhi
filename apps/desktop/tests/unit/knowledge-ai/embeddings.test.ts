import { describe, expect, it } from "vitest";
import {
  buildEmbeddingsEndpointFromBase,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import {
  l2Normalize,
  parseEmbeddingsResponse,
  resolveEmbeddingConfig,
} from "../../../src/renderer/services/knowledge-ai/embeddings";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";

describe("resolveEmbeddingConfig", () => {
  it("供应商禁用后不再调用其旗下 embedding 模型", () => {
    const previous = useSettingsStore.getState();
    const provider = {
      id: "provider-disabled",
      name: "已停用供应商",
      provider: "custom",
      apiProtocol: "openai" as const,
      apiKey: "sk-provider",
      apiUrl: "https://old.example.com/v1",
      enabled: false,
    };
    const model = {
      id: "embedding-disabled-provider",
      providerId: provider.id,
      provider: "custom",
      apiProtocol: "openai" as const,
      apiKey: "sk-model",
      apiUrl: provider.apiUrl,
      model: "text-embedding-3-small",
      enabled: true,
      capabilities: { chat: false, embedding: true },
    };

    try {
      useSettingsStore.setState({
        aiProviders: [provider],
        aiModels: [model],
        modelRouteDefaults: { embedding: model.id },
      });
      expect(resolveEmbeddingConfig()).toBeNull();

      useSettingsStore.setState({ aiProviders: [{ ...provider, enabled: true }] });
      expect(resolveEmbeddingConfig()).toMatchObject({
        id: model.id,
        model: model.model,
      });
    } finally {
      useSettingsStore.setState({
        aiProviders: previous.aiProviders,
        aiModels: previous.aiModels,
        modelRouteDefaults: previous.modelRouteDefaults,
      });
    }
  });
});

describe("buildEmbeddingsEndpointFromBase", () => {
  it("openai：无版本段补 /v1/embeddings，有版本段直接拼", () => {
    expect(
      buildEmbeddingsEndpointFromBase(
        resolveProtocolBase("https://api.openai.com", "openai"),
      ),
    ).toBe("https://api.openai.com/v1/embeddings");
    expect(
      buildEmbeddingsEndpointFromBase(
        resolveProtocolBase("https://api.example.com/v1", "openai"),
      ),
    ).toBe("https://api.example.com/v1/embeddings");
  });

  it("# 结尾的显式端点原样使用", () => {
    expect(
      buildEmbeddingsEndpointFromBase(
        resolveProtocolBase("https://gw.internal/custom-embed#", "openai"),
      ),
    ).toBe("https://gw.internal/custom-embed");
  });

  it("gemini 走 OpenAI 兼容层，anthropic 不支持返回空串", () => {
    expect(
      buildEmbeddingsEndpointFromBase(
        resolveProtocolBase(
          "https://generativelanguage.googleapis.com/v1beta",
          "gemini",
        ),
      ),
    ).toBe(
      "https://generativelanguage.googleapis.com/v1beta/openai/embeddings",
    );
    expect(
      buildEmbeddingsEndpointFromBase(
        resolveProtocolBase("https://api.anthropic.com/v1", "anthropic"),
      ),
    ).toBe("");
  });
});

describe("parseEmbeddingsResponse", () => {
  it("按 index 对齐输入顺序", () => {
    const body = JSON.stringify({
      data: [
        { index: 1, embedding: [0, 1] },
        { index: 0, embedding: [1, 0] },
      ],
    });
    expect(parseEmbeddingsResponse(body, 2)).toEqual([
      [1, 0],
      [0, 1],
    ]);
  });

  it("数量或维度不一致时报错", () => {
    expect(() =>
      parseEmbeddingsResponse(JSON.stringify({ data: [] }), 1),
    ).toThrow("数量不匹配");
    const inconsistent = JSON.stringify({
      data: [
        { index: 0, embedding: [1, 0] },
        { index: 1, embedding: [1] },
      ],
    });
    expect(() => parseEmbeddingsResponse(inconsistent, 2)).toThrow(
      "维度不一致",
    );
    expect(() => parseEmbeddingsResponse("not json", 1)).toThrow("JSON");
  });
});

describe("l2Normalize", () => {
  it("归一化后模长为 1，零向量安全返回零", () => {
    const normalized = l2Normalize([3, 4]);
    expect(normalized[0]).toBeCloseTo(0.6, 5);
    expect(normalized[1]).toBeCloseTo(0.8, 5);
    expect(l2Normalize([0, 0])).toEqual([0, 0]);
  });
});
