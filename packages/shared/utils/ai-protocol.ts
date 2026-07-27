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

/**
 * 文生图端点：OpenAI 兼容协议走 /v1/images/generations。
 *
 * Gemini 的图像生成在 `models/{model}:generateContent` 上（模型名要进路径，
 * 见 buildGeminiImageEndpoint），Anthropic 压根没有文生图 API；
 * 两者都返回空串，由调用方给出可读提示而不是让用户撞一个 404。
 */
export function buildImagesEndpointFromBase(
  resolved: ResolvedAIProtocolBase,
): string {
  const baseUrl = resolved.baseUrl.replace(/\/$/, "");
  if (!baseUrl) return "";
  if (resolved.explicit) return baseUrl;
  if (resolved.protocol !== "openai") return "";
  return baseUrl.match(/\/v\d+$/)
    ? `${baseUrl}/images/generations`
    : `${baseUrl}/v1/images/generations`;
}

/**
 * Gemini 生图端点。
 *
 * 走原生 generateContent 而不是 OpenAI 兼容层：只有原生请求体里的
 * `generationConfig.imageConfig.aspectRatio` 能拿到真正的 16:9，
 * 兼容层没有对应参数，出来的是方图。
 */
export function buildGeminiImageEndpoint(
  resolved: ResolvedAIProtocolBase,
  model: string,
): string {
  const baseUrl = resolved.baseUrl.replace(/\/$/, "");
  if (!baseUrl) return "";
  if (resolved.explicit) return baseUrl;
  const nativeBase = baseUrl.replace(/\/openai$/, "");
  const versioned = nativeBase.match(/\/v\d+(?:beta)?$/)
    ? nativeBase
    : `${nativeBase}/v1beta`;
  return `${versioned}/models/${encodeURIComponent(model)}:generateContent`;
}

/**
 * 转写端点：只有 OpenAI 兼容协议提供 multipart 的 /audio/transcriptions。
 *
 * Anthropic 没有转写 API；Gemini 的音频要走 generateContent 内联，
 * 与 multipart 上传完全不同。两者都返回空串，由调用方给出可读提示——
 * 此前一律拼 /v1/audio/transcriptions，用户只会撞上一个莫名的 404。
 */
export function buildTranscriptionsEndpointFromBase(
  resolved: ResolvedAIProtocolBase,
): string {
  const baseUrl = resolved.baseUrl.replace(/\/$/, "");
  if (!baseUrl) return "";
  if (resolved.explicit) return baseUrl;
  if (resolved.protocol !== "openai") return "";
  return baseUrl.match(/\/v\d+$/)
    ? `${baseUrl}/audio/transcriptions`
    : `${baseUrl}/v1/audio/transcriptions`;
}

/** OpenAI 风格的多模态消息片段（各协议的共同输入形态） */
export type MultimodalPart =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } };

/**
 * 把 OpenAI 风格的多模态内容转成 Anthropic 的 content 块。
 *
 * Anthropic 不认 image_url，图片必须拆成 base64 的 source 对象。
 */
export function toAnthropicContentParts(
  parts: MultimodalPart[],
): Array<Record<string, unknown>> {
  return parts.flatMap((part): Array<Record<string, unknown>> => {
    if (part.type === "text") {
      return [{ type: "text", text: part.text }];
    }
    const match = part.image_url.url.match(/^data:(.+?);base64,(.+)$/);
    if (!match) {
      return [];
    }
    return [
      {
        type: "image",
        source: { type: "base64", media_type: match[1], data: match[2] },
      },
    ];
  });
}

/**
 * 从各协议的对话响应里提取纯文本。
 *
 * OpenAI / Gemini 兼容层是 `choices[0].message.content`，
 * Anthropic 是顶层的 `content` 块数组——两者结构不同，取错就会
 * 得到「响应缺少文本内容」这种看不出原因的报错。
 */
export function extractTextFromChatResponse(
  payload: unknown,
  protocol: AIProtocol,
): string | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }

  const flattenParts = (value: unknown): string | null => {
    if (typeof value === "string") {
      return value;
    }
    if (Array.isArray(value)) {
      return value
        .map((part) =>
          part && typeof part === "object" && "text" in part
            ? String((part as { text: unknown }).text ?? "")
            : "",
        )
        .join("");
    }
    return null;
  };

  if (protocol === "anthropic") {
    return flattenParts((payload as { content?: unknown }).content);
  }

  const choices = (payload as { choices?: unknown }).choices;
  if (!Array.isArray(choices) || choices.length === 0) {
    return null;
  }
  const message = (choices[0] as { message?: { content?: unknown } })?.message;
  return flattenParts(message?.content);
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
