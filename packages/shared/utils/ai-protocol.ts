import type { AIProtocol } from "../types/ai";

export interface AIProtocolConfig {
  apiProtocol?: AIProtocol;
  provider?: string;
  apiUrl?: string;
}

export interface ResolvedAIProtocolBase {
  protocol: AIProtocol;
  explicit: boolean;
  baseUrl: string;
}

const ENDPOINT_SUFFIXES = [
  "/chat/completions",
  "/completions",
  "/models",
  "/embeddings",
  "/images/generations",
] as const;

export function resolveAIProtocol(config: AIProtocolConfig): AIProtocol {
  if (
    config.apiProtocol === "openai" ||
    config.apiProtocol === "gemini" ||
    config.apiProtocol === "anthropic"
  ) {
    return config.apiProtocol;
  }

  const provider = config.provider?.toLowerCase() || "";
  const apiUrl = config.apiUrl?.toLowerCase() || "";
  if (provider === "anthropic" || apiUrl.includes("api.anthropic.com")) {
    return "anthropic";
  }
  if (
    provider === "google" ||
    provider === "gemini" ||
    apiUrl.includes("generativelanguage.googleapis.com")
  ) {
    return "gemini";
  }
  return "openai";
}

export function getBaseUrl(apiUrl: string): string {
  if (!apiUrl) return "";
  let url = apiUrl.trim();
  if (url.endsWith("#")) return url.slice(0, -1);
  if (url.endsWith("/")) url = url.slice(0, -1);
  for (const suffix of ENDPOINT_SUFFIXES) {
    if (url.endsWith(suffix)) {
      return url.slice(0, -suffix.length);
    }
  }
  return url;
}

export function normalizeApiUrlInput(apiUrl: string): string {
  if (!apiUrl) return "";
  const trimmed = apiUrl.trim();
  const explicit = trimmed.endsWith("#");
  const rawValue = explicit ? trimmed.slice(0, -1) : trimmed;
  const normalized = getBaseUrl(rawValue);
  if (!normalized) return explicit ? "#" : "";
  return explicit ? `${normalized}#` : normalized;
}

export function resolveProtocolBase(
  apiUrl: string,
  protocol: AIProtocol,
): ResolvedAIProtocolBase {
  const trimmed = apiUrl.trim();
  const explicit = trimmed.endsWith("#");
  const rawValue = explicit ? trimmed.slice(0, -1) : trimmed;
  return { protocol, explicit, baseUrl: getBaseUrl(rawValue) };
}

export function buildChatEndpointFromBase(
  resolved: ResolvedAIProtocolBase,
): string {
  const baseUrl = resolved.baseUrl.replace(/\/$/, "");
  if (!baseUrl || resolved.explicit) return baseUrl;
  if (resolved.protocol === "gemini") return buildGeminiChatEndpoint(baseUrl);
  if (resolved.protocol === "anthropic") {
    if (baseUrl.endsWith("/messages")) return baseUrl;
    return baseUrl.match(/\/v\d+$/)
      ? `${baseUrl}/messages`
      : `${baseUrl}/v1/messages`;
  }
  return baseUrl.match(/\/v\d+$/)
    ? `${baseUrl}/chat/completions`
    : `${baseUrl}/v1/chat/completions`;
}

function buildGeminiChatEndpoint(baseUrl: string): string {
  if (baseUrl.endsWith("/openai")) return `${baseUrl}/chat/completions`;
  if (baseUrl.match(/\/v\d+(?:beta)?$/)) {
    return `${baseUrl}/openai/chat/completions`;
  }
  return `${baseUrl}/v1beta/openai/chat/completions`;
}

/**
 * Embeddings 端点：openai 走 /v1/embeddings；gemini 走 OpenAI 兼容层；
 * anthropic 无 embeddings API，返回空串由调用方报「不支持」。
 */
export function buildEmbeddingsEndpointFromBase(
  resolved: ResolvedAIProtocolBase,
): string {
  const baseUrl = resolved.baseUrl.replace(/\/$/, "");
  if (!baseUrl || resolved.explicit) return baseUrl;
  if (resolved.protocol === "anthropic") return "";
  if (resolved.protocol === "gemini") {
    if (baseUrl.endsWith("/openai")) return `${baseUrl}/embeddings`;
    if (baseUrl.match(/\/v\d+(?:beta)?$/)) {
      return `${baseUrl}/openai/embeddings`;
    }
    return `${baseUrl}/v1beta/openai/embeddings`;
  }
  return baseUrl.match(/\/v\d+$/)
    ? `${baseUrl}/embeddings`
    : `${baseUrl}/v1/embeddings`;
}

export function buildModelsEndpointFromBase(
  resolved: ResolvedAIProtocolBase,
): string {
  const baseUrl = resolved.baseUrl.replace(/\/$/, "");
  if (!baseUrl || resolved.explicit) return baseUrl;
  if (resolved.protocol === "gemini") {
    const geminiBaseUrl = baseUrl.replace(/\/openai$/, "");
    return geminiBaseUrl.match(/\/v\d+(?:beta)?$/)
      ? `${geminiBaseUrl}/models`
      : `${geminiBaseUrl}/v1beta/models`;
  }
  const protocolBase = baseUrl.replace(/\/messages$/, "");
  return protocolBase.match(/\/v\d+$/)
    ? `${protocolBase}/models`
    : `${protocolBase}/v1/models`;
}

export function buildHeadersForProtocol(
  protocol: AIProtocol,
  apiKey: string,
  options?: {
    accept?: string;
    contentType?: boolean;
    useNativeGeminiAuth?: boolean;
  },
): Record<string, string> {
  const headers: Record<string, string> = {};
  if (options?.contentType !== false)
    headers["Content-Type"] = "application/json";
  if (options?.accept) headers.Accept = options.accept;
  if (protocol === "anthropic") {
    headers["x-api-key"] = apiKey;
    headers["anthropic-version"] = "2023-06-01";
  } else if (protocol === "gemini" && options?.useNativeGeminiAuth) {
    headers["x-goog-api-key"] = apiKey;
  } else {
    headers.Authorization = `Bearer ${apiKey}`;
  }
  return headers;
}
