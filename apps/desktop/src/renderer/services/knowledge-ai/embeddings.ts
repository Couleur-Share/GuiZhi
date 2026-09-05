/**
 * Embedding API 调用：按 embedding 模型路由解析配置，
 * 经主进程 AI HTTP 代理发起 OpenAI 兼容的 /embeddings 请求。
 */
import {
  buildEmbeddingsEndpointFromBase,
  buildHeadersForProtocol,
  resolveAIProtocol,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import { useSettingsStore } from "../../stores/settings.store";
import {
  isConfiguredModel,
  resolveRouteModel,
  toAIConfig,
} from "../ai-defaults";
import type { AIConfig } from "../ai";
import { recordAiUsage } from "./ai-invoke";

const EMBEDDINGS_TIMEOUT_MS = 60_000;

/** 解析 embedding 路由对应的模型配置；未配置返回 null（语义检索静默降级） */
export function resolveEmbeddingConfig(): AIConfig | null {
  const state = useSettingsStore.getState();
  const model = resolveRouteModel(
    state.aiModels,
    state.modelRouteDefaults,
    "embedding",
    state.aiProviders,
  );
  return isConfiguredModel(model) ? toAIConfig(model) : null;
}

export function isSemanticConfigured(): boolean {
  return resolveEmbeddingConfig() !== null;
}

export { l2Normalize, parseEmbeddingsResponse } from "@guizhi/shared/utils/embedding-values";
import { l2Normalize, parseEmbeddingsResponse } from "@guizhi/shared/utils/embedding-values";

/** 批量嵌入文本，返回 L2 归一化后的向量（与输入同序） */
export async function embedTexts(
  config: AIConfig,
  texts: string[],
): Promise<number[][]> {
  if (texts.length === 0) {
    return [];
  }
  const protocol = resolveAIProtocol(config);
  const resolved = resolveProtocolBase(config.apiUrl, protocol);
  const endpoint = buildEmbeddingsEndpointFromBase(resolved);
  if (!endpoint) {
    throw new Error(`当前协议不支持 embeddings 接口: ${protocol}`);
  }

  let response;
  try {
    response = await window.api.ai.request({
      method: "POST",
      url: endpoint,
      headers: buildHeadersForProtocol(protocol, config.apiKey),
      body: JSON.stringify({ model: config.model, input: texts }),
      timeoutMs: EMBEDDINGS_TIMEOUT_MS,
    });
  } catch (error) {
    recordAiUsage({ scenario: "embedding", model: config.model, failed: true });
    throw error;
  }
  if (!response.ok) {
    recordAiUsage({ scenario: "embedding", model: config.model, failed: true });
    const detail = (response.error || response.body || "").slice(0, 300);
    throw new Error(`Embeddings 请求失败 (HTTP ${response.status}): ${detail}`);
  }

  // 一次全库索引会打成百上千次 embeddings，此前这些消耗在用量面板上是 0
  recordAiUsage({
    scenario: "embedding",
    model: config.model,
    promptTokens: readPromptTokens(response.body),
  });

  return parseEmbeddingsResponse(response.body, texts.length).map(l2Normalize);
}

/** embeddings 响应里的 usage.prompt_tokens；provider 不回报时按 0 计 */
function readPromptTokens(body: string): number {
  try {
    const parsed: unknown = JSON.parse(body);
    const usage = (parsed as { usage?: { prompt_tokens?: unknown } })?.usage;
    return Math.max(0, Math.trunc(Number(usage?.prompt_tokens) || 0));
  } catch {
    return 0;
  }
}
