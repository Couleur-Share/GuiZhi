/**
 * 模型列表拉取与解析。
 *
 * 各家 /models 的响应形态不同：OpenAI 与 Anthropic 都是 `data[]` 但字段不同，
 * Gemini 是 `models[]` 且 id 带 `models/` 前缀，还有中转站直接返回裸数组。
 * 这里把四种形态归一成 ModelInfo。
 */
import type { AIProtocol } from "@guizhi/shared/types";
import {
  buildChatEndpointFromBase,
  buildHeadersForProtocol,
  buildModelsEndpointFromBase,
  resolveAIProtocol,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import type { FetchModelsResult, ModelInfo } from "./ai-types";
import {
  createFetchResponseLike,
  createResponseLike,
  getAITransport,
} from "./ai-transport";
// ============ 获取模型列表 ============
// ============ Get Model List ============

interface AnthropicModelsPayload {
  data?: Array<{
    id?: string;
    display_name?: string;
    created_at?: string;
  }>;
}

interface OpenAIModelsPayload {
  data?: Array<{
    id?: string;
    owned_by?: string;
    created?: number;
  }>;
}

interface GeminiModelsPayload {
  models?: Array<{
    name?: string;
    displayName?: string;
    description?: string;
  }>;
}

interface ArrayModelPayloadItem {
  id?: string;
  model?: string;
  name?: string;
}

/**
 * Get complete API endpoint preview (for display)
 * 如果用户输入以 # 结尾，则不自动填充后续路径
 * 如果用户没有输入 /v1，会自动补全
 * 对于 Gemini API，使用 OpenAI 兼容端点
 * Get complete API endpoint preview (for display)
 * If the input ends with #, do not auto-fill the subsequent path
 * Auto-complete /v1 if user didn't input it
 * Use OpenAI-compatible endpoint for Gemini API
 */
export function getApiEndpointPreview(
  apiUrl: string,
  protocol: AIProtocol = "openai",
): string {
  if (!apiUrl) return "";
  return buildChatEndpointFromBase(resolveProtocolBase(apiUrl, protocol));
}

/**
 * Fetch available model list from API
 * 从 API 获取可用模型列表
 */
export async function fetchAvailableModels(
  apiUrl: string,
  apiKey: string,
  apiProtocol: AIProtocol = "openai",
): Promise<FetchModelsResult> {
  if (!apiKey || !apiUrl) {
    return {
      success: false,
      models: [],
      error: "Please fill in API Key and API URL first",
    };
    // 请先填写 API Key 和 API 地址
  }

  try {
    const endpoint = buildModelsEndpointFromBase(
      resolveProtocolBase(apiUrl, apiProtocol),
    );
    const resolvedProtocol = resolveAIProtocol({
      apiProtocol,
      provider: "",
      apiUrl,
    });
    const headers = buildHeadersForProtocol(resolvedProtocol, apiKey, {
      accept: "application/json",
      useNativeGeminiAuth: resolvedProtocol === "gemini",
    });

    const transport = getAITransport();
    const response = transport
      ? createResponseLike(
          await transport.request({
            method: "GET",
            url: endpoint,
            headers,
            timeoutMs: 12_000,
          }),
        )
      : createFetchResponseLike(
          await fetch(endpoint, {
            method: "GET",
            headers,
          }),
        );

    if (!response.ok) {
      const errorText = response.error ?? (await response.text());
      const reason =
        response.status === 401 || response.status === 403
          ? "auth"
          : response.status === 0 && /timeout/i.test(errorText)
            ? "network"
            : response.status === 404 ||
                response.status === 405 ||
                response.status === 501
              ? "unsupported"
              : "http";
      return {
        success: false,
        models: [],
        error:
          response.status === 0
            ? errorText.substring(0, 120)
            : `获取模型列表失败: ${response.status} - ${errorText.substring(0, 100)}`,
        reason,
        endpoint,
        status: response.status,
        // Failed to get model list
      };
    }

    const data = await response.json<
      | AnthropicModelsPayload
      | OpenAIModelsPayload
      | GeminiModelsPayload
      | ArrayModelPayloadItem[]
    >();

    if (
      apiProtocol === "anthropic" &&
      "data" in data &&
      Array.isArray(data.data)
    ) {
      const models = data.data
        .filter((m: { id?: string }) => typeof m.id === "string")
        .map(
          (m: { id: string; display_name?: string; created_at?: string }) => ({
            id: m.id,
            name: m.display_name || m.id,
            owned_by: "Anthropic",
            created: m.created_at ? Date.parse(m.created_at) : undefined,
          }),
        )
        .sort((a: ModelInfo, b: ModelInfo) => a.id.localeCompare(b.id));

      return { success: true, models };
    }

    // OpenAI 格式的响应
    // OpenAI format response
    if ("data" in data && Array.isArray(data.data)) {
      const models = data.data
        .filter((m: { id?: string }) => m.id) // 过滤掉没有 id 的 / Filter out those without id
        .map((m: { id: string; owned_by?: string; created?: number }) => ({
          id: m.id,
          name: m.id,
          owned_by: m.owned_by,
          created: m.created,
        }))
        .sort((a: ModelInfo, b: ModelInfo) => a.id.localeCompare(b.id));

      return { success: true, models };
    }

    // Gemini 格式的响应 / Gemini format response
    if ("models" in data && Array.isArray(data.models)) {
      const models = data.models
        .filter((m: { name?: string }) => m.name)
        .map(
          (m: { name: string; displayName?: string; description?: string }) => {
            // Gemini returns "models/gemini-pro", we need "gemini-pro" for OpenAI compatible endpoint
            const id = m.name.replace(/^models\//, "");
            return {
              id: id,
              name: m.displayName ? `${m.displayName} (${id})` : id,
              owned_by: "Google",
              description: m.description,
            };
          },
        )
        .sort((a: ModelInfo, b: ModelInfo) => a.id.localeCompare(b.id));

      return { success: true, models };
    }

    // 某些 API 直接返回数组
    // Some APIs return array directly
    if (Array.isArray(data)) {
      const models = data
        .filter((m: { id?: string; model?: string }) => m.id || m.model)
        .map((m: { id?: string; model?: string; name?: string }) => ({
          id: m.id || m.model || "",
          name: m.name || m.id || m.model,
        }));
      return { success: true, models };
    }

    return {
      success: false,
      models: [],
      error: "无法解析模型列表响应",
      reason: "unsupported",
      endpoint,
    };
    // Cannot parse model list response
  } catch (error) {
    const message = error instanceof Error ? error.message : "获取模型列表失败";
    return {
      success: false,
      models: [],
      error: message,
      reason:
        message.toLowerCase().includes("failed to fetch") ||
        message.toLowerCase().includes("network")
          ? "network"
          : "http",
      // Failed to get model list
    };
  }
}