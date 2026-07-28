import type { AIProtocol } from "@guizhi/shared/types";
import {
  buildChatEndpointFromBase,
  buildHeadersForProtocol,
  extractUsageFromChatResponse,
  resolveAIProtocol,
  resolveProtocolBase,
  type ChatTokenUsage,
} from "@guizhi/shared/utils/ai-protocol";

export interface AIClientConfig {
  provider?: string;
  apiProtocol?: AIProtocol;
  apiKey: string;
  apiUrl: string;
  model: string;
}

export interface AIChatMessage {
  role: "system" | "user" | "assistant";
  content: string;
}

export interface AIChatResult {
  content: string;
  /** openai 系 finish_reason；anthropic 的 max_tokens 归一化为 "length" */
  finishReason?: string;
  /**
   * 本次调用的 token 用量；接口没回报时为 undefined。
   *
   * 主进程的总结、排版、论坛总结都走这里，而它们是按块发的——一篇长文字稿
   * 就是三十多次调用。不带出用量的话，用量面板只能显示「N 次 · 0」。
   */
  usage?: ChatTokenUsage;
}

const AI_REQUEST_TIMEOUT_MS = 60_000;

export async function chatCompletion(
  config: AIClientConfig,
  messages: AIChatMessage[],
  options?: {
    temperature?: number;
    maxTokens?: number;
    responseFormat?: { type: "text" | "json_object" };
    /** 外部取消信号（与内部超时并联） */
    signal?: AbortSignal;
    /** 请求超时，默认 60 秒 */
    timeoutMs?: number;
  },
): Promise<AIChatResult> {
  if (!config.apiKey) {
    throw new Error("AI API Key is not configured");
  }
  if (!config.apiUrl) {
    throw new Error("AI API URL is not configured");
  }
  if (!config.model) {
    throw new Error("AI model is not configured");
  }

  const protocol = resolveAIProtocol(config);
  const endpoint = buildChatEndpointFromBase(
    resolveProtocolBase(config.apiUrl, protocol),
  );
  const headers = buildHeadersForProtocol(protocol, config.apiKey, {
    accept: "application/json",
  });

  const isGemini = protocol === "gemini";
  const isAnthropic = protocol === "anthropic";
  const model = isGemini ? config.model.replace(/^models\//, "") : config.model;

  const body: Record<string, unknown> = isAnthropic
    ? {
        model,
        max_tokens: options?.maxTokens ?? 4096,
        messages: messages
          .filter((message) => message.role !== "system")
          .map((message) => ({
            role: message.role === "assistant" ? "assistant" : "user",
            content: message.content,
          })),
        stream: false,
      }
    : {
        model,
        messages,
        temperature: options?.temperature ?? 0.3,
        max_tokens: options?.maxTokens ?? 4096,
        stream: false,
      };

  if (!isAnthropic && options?.responseFormat) {
    body.response_format = options.responseFormat;
  }
  if (isAnthropic) {
    const systemMessage = messages.find((message) => message.role === "system");
    if (systemMessage?.content) {
      body.system = systemMessage.content;
    }
  }

  const controller = new AbortController();
  const timeout = setTimeout(
    () => controller.abort(),
    options?.timeoutMs ?? AI_REQUEST_TIMEOUT_MS,
  );
  const externalSignal = options?.signal;
  const abortFromExternal = () => controller.abort(externalSignal?.reason);
  if (externalSignal?.aborted) {
    abortFromExternal();
  }
  externalSignal?.addEventListener("abort", abortFromExternal, { once: true });

  try {
    const response = await fetch(endpoint, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
      signal: controller.signal,
    });

    if (!response.ok) {
      const errorText = await response.text().catch(() => "");
      let errorMessage = `AI API request failed (${response.status})`;
      try {
        const errorJson = JSON.parse(errorText) as Record<string, unknown>;
        const inner = errorJson.error as Record<string, unknown> | undefined;
        errorMessage =
          (inner?.message as string) ??
          (errorJson.message as string) ??
          errorMessage;
      } catch {
        // Use default error message
      }
      throw new Error(errorMessage);
    }

    const json = (await response.json()) as {
      choices?: { message?: { content?: string }; finish_reason?: string }[];
      content?: Array<{ type?: string; text?: string }>;
      stop_reason?: string;
      usage?: Record<string, unknown>;
    };

    const content = isAnthropic
      ? (json.content || [])
          .filter(
            (item) => item?.type === "text" && typeof item.text === "string",
          )
          .map((item) => item.text)
          .join("")
      : json.choices?.[0]?.message?.content;

    if (typeof content !== "string") {
      throw new Error("AI API returned an unexpected response format");
    }

    const rawFinishReason = isAnthropic
      ? json.stop_reason
      : json.choices?.[0]?.finish_reason;
    const finishReason =
      isAnthropic && rawFinishReason === "max_tokens"
        ? "length"
        : (rawFinishReason ?? undefined);

    return {
      content,
      finishReason,
      usage: extractUsageFromChatResponse(json, protocol),
    };
  } finally {
    clearTimeout(timeout);
    externalSignal?.removeEventListener("abort", abortFromExternal);
  }
}
