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

/**
 * 是否为 Qwen 系模型。判定与渲染进程的 `renderer/services/ai.ts` 保持一致：
 * 中转站的 provider 一律是 custom，所以模型名是唯一可靠的线索。
 */
function isQwenModel(config: AIClientConfig): boolean {
  const provider = config.provider?.toLowerCase() ?? "";
  return (
    provider.includes("qwen") ||
    provider.includes("dashscope") ||
    config.model.toLowerCase().includes("qwen")
  );
}

/**
 * 端点不认识 `enable_thinking` 的判定。
 *
 * 只认「报错文本里点名了这个参数」：官方 OpenAI 对未知字段回
 * `Unrecognized request argument supplied: enable_thinking`，各家中转站措辞不一
 * 但都会带上字段名。按状态码判会把限流、余额不足一并卷进来，白重发一次。
 */
function mentionsThinkingParam(errorText: string): boolean {
  return errorText.toLowerCase().includes("enable_thinking");
}

/** 从错误响应体里抠出 provider 给的说明；抠不出就报状态码 */
function describeChatError(status: number, errorText: string): string {
  try {
    const errorJson = JSON.parse(errorText) as Record<string, unknown>;
    const inner = errorJson.error as Record<string, unknown> | undefined;
    const message =
      (inner?.message as string) ?? (errorJson.message as string) ?? "";
    if (message) {
      return message;
    }
  } catch {
    // 非 JSON 响应体，回落到状态码
  }
  return `AI API request failed (${status})`;
}

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

  // Qwen3 系是混合推理模型，默认开思考。主进程这几条链路（文字稿排版、内容总结、
  // 拟题）一律非流式，思考过程既读不到也用不上，只是白烧 token 和时间——实测给
  // 1600 字文字稿补标点分段，思维链能写到两万字、单块 77 秒（还撞到过 300 秒不
  // 返回），关掉后 10 秒，输出反而分得更细。渲染进程那份客户端早就这么做了，
  // 这份从 fork 剥出来的精简实现一直漏着。
  if (!isAnthropic && isQwenModel(config)) {
    body.enable_thinking = false;
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

  const send = () =>
    fetch(endpoint, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
      signal: controller.signal,
    });

  try {
    let response = await send();

    if (!response.ok) {
      let errorText = await response.text().catch(() => "");
      // 端点不认识 enable_thinking 时摘掉重发一次：这个参数只对混合推理模型有意义，
      // 不认识它的端点不该因此整条链路失败。重发的请求不带该字段，不会再撞同一堵墙
      if (
        body.enable_thinking !== undefined &&
        mentionsThinkingParam(errorText)
      ) {
        console.warn(
          `[ai] 端点拒绝 enable_thinking，摘掉后重发一次：${errorText.slice(0, 200)}`,
        );
        delete body.enable_thinking;
        response = await send();
        errorText = response.ok ? "" : await response.text().catch(() => "");
      }
      if (!response.ok) {
        throw new Error(describeChatError(response.status, errorText));
      }
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
