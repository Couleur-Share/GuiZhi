import type { AIProtocol, AITransportResponse } from "@guizhi/shared/types";
import {
  buildChatEndpointFromBase,
  buildHeadersForProtocol,
  buildModelsEndpointFromBase,
  resolveAIProtocol,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import type {
  AIConfig,
  AITestResult,
  ChatCompletionRequest,
  ChatCompletionOptions,
  ChatCompletionResponse,
  ChatCompletionResult,
  ChatMessage,
  ChatMessageContent,
  ChatMessageContentPart,
  FetchModelsResult,
  ModelInfo,
  StreamCallbacks,
} from "./ai-types";

export type * from "./ai-types";

export {
  getBaseUrl,
  normalizeApiUrlInput,
} from "@guizhi/shared/utils/ai-protocol";

/**
 * AI Service - Call various AI model APIs
 * Most domestic and international service providers are compatible with OpenAI format
 * AI 服务 - 调用各种 AI 模型 API
 * 大部分国内外服务商都兼容 OpenAI 格式
 */

type AnthropicMessageContentPart =
  | { type: "text"; text: string }
  | {
      type: "image";
      source: {
        type: "base64";
        media_type: string;
        data: string;
      };
    };

interface ResponseLike {
  ok: boolean;
  status: number;
  statusText: string;
  headers: Record<string, string>;
  text: () => Promise<string>;
  json: <T = unknown>() => Promise<T>;
  error?: string;
}

interface StreamState {
  fullContent: string;
  thinkingContent: string;
  buffer: string;
  chunkCount: number;
}

const AI_CONNECTION_TEST_MAX_TOKENS = 8;
const AI_CONNECTION_TEST_TIMEOUT_MS = 12_000;
const AI_CONNECTION_TEST_PROMPT = "Reply with exactly: OK";

function getAITransport() {
  if (typeof window === "undefined") {
    return null;
  }
  return window.api?.ai ?? null;
}

function createResponseLike(response: AITransportResponse): ResponseLike {
  return {
    ok: response.ok,
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
    text: async () => response.body,
    json: async <T = unknown>() => JSON.parse(response.body) as T,
    error: response.error,
  };
}

function createFetchResponseLike(response: Response): ResponseLike {
  return {
    ok: response.ok,
    status: response.status,
    statusText: response.statusText,
    headers: Object.fromEntries(response.headers.entries()),
    text: async () => response.text(),
    json: async <T = unknown>() => response.json() as Promise<T>,
  };
}

async function requestAIEndpoint(request: {
  method: "GET" | "POST";
  url: string;
  headers: Record<string, string>;
  body?: string;
  timeoutMs?: number;
}): Promise<ResponseLike> {
  const transport = getAITransport();
  if (transport) {
    return createResponseLike(await transport.request(request));
  }

  return createFetchResponseLike(
    await fetch(request.url, {
      method: request.method,
      headers: request.headers,
      body: request.body,
    }),
  );
}

function getResponseHeader(
  headers: Record<string, string>,
  name: string,
): string {
  const lowerName = name.toLowerCase();
  const match = Object.entries(headers).find(
    ([key]) => key.toLowerCase() === lowerName,
  );
  return match?.[1] ?? "";
}

function isHtmlErrorPayload(
  text: string,
  headers: Record<string, string>,
): boolean {
  const contentType = getResponseHeader(headers, "content-type").toLowerCase();
  const trimmed = text.trimStart().toLowerCase();
  return (
    contentType.includes("text/html") ||
    trimmed.startsWith("<!doctype html") ||
    trimmed.startsWith("<html")
  );
}

function extractHtmlTitle(text: string): string | null {
  const match = text.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
  return match?.[1]?.replace(/\s+/g, " ").trim() || null;
}

function formatGatewayTimeoutMessage(
  operation: string,
  status: number,
): string {
  return `${operation} gateway timed out (${status}). The provider or proxy did not finish before its own timeout.`;
}

function parseStructuredErrorMessage(text: string): string | null {
  try {
    const errorJson = JSON.parse(text);
    const message =
      errorJson.error?.message ||
      errorJson.error?.status ||
      errorJson.error?.type ||
      errorJson.message ||
      errorJson.detail ||
      (typeof errorJson.error === "string" ? errorJson.error : null);

    if (!message) {
      return null;
    }

    if (errorJson.error?.code) {
      return `${message} (code: ${errorJson.error.code})`;
    }
    if (errorJson.error?.type && errorJson.error.type !== message) {
      return `[${errorJson.error.type}] ${message}`;
    }
    return message;
  } catch {
    return null;
  }
}

async function getFormattedErrorMessageFromResponse(
  response: ResponseLike,
  options: {
    operation?: string;
    fallback?: string;
    maxTextLength?: number;
  } = {},
): Promise<string> {
  const errorText = response.error ?? (await response.text());
  const operation = options.operation ?? "API request";
  const fallback = options.fallback ?? `API 请求失败 (${response.status})`;

  if (response.status === 504) {
    return formatGatewayTimeoutMessage(operation, response.status);
  }

  const structuredMessage = parseStructuredErrorMessage(errorText);
  if (structuredMessage) {
    return structuredMessage;
  }

  if (errorText && isHtmlErrorPayload(errorText, response.headers)) {
    const title = extractHtmlTitle(errorText);
    return title ? `${fallback}: ${title}` : fallback;
  }

  if (errorText) {
    return errorText.slice(0, options.maxTextLength ?? 200);
  }

  return fallback;
}

function createStreamState(): StreamState {
  return {
    fullContent: "",
    thinkingContent: "",
    buffer: "",
    chunkCount: 0,
  };
}

function isGeminiApiHost(apiUrl: string): boolean {
  return apiUrl.includes("generativelanguage.googleapis.com");
}

function isGeminiOpenAICompatEndpoint(endpoint: string): boolean {
  return (
    endpoint.includes("generativelanguage.googleapis.com") &&
    endpoint.includes("/openai/")
  );
}

function yieldToEventLoop() {
  return new Promise<void>((resolve) => {
    if (typeof requestAnimationFrame === "function") {
      requestAnimationFrame(() => resolve());
    } else {
      setTimeout(resolve, 0);
    }
  });
}

async function processStreamTextChunk(
  chunkText: string,
  state: StreamState,
  onStream?: (chunk: string) => void,
  streamCallbacks?: StreamCallbacks,
  options?: {
    flush?: boolean;
    yieldToUi?: boolean;
  },
): Promise<void> {
  state.buffer += chunkText;
  const lines = state.buffer.split("\n");
  state.buffer = options?.flush ? "" : lines.pop() || "";
  let deltasSinceYield = 0;

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed === "data: [DONE]") continue;
    if (!trimmed.startsWith("data: ")) continue;

    try {
      const json = JSON.parse(trimmed.slice(6));
      const delta = json.choices?.[0]?.delta;

      if (!delta) {
        continue;
      }

      state.chunkCount++;
      deltasSinceYield++;

      if (delta.reasoning_content) {
        state.thinkingContent += delta.reasoning_content;
        streamCallbacks?.onThinking?.(delta.reasoning_content);
      }

      if (delta.content) {
        state.fullContent += delta.content;
        onStream?.(delta.content);
        streamCallbacks?.onContent?.(delta.content);
      }

      if (options?.yieldToUi && deltasSinceYield >= 20) {
        deltasSinceYield = 0;
        await yieldToEventLoop();
      }
    } catch {
      // 忽略解析错误 / Ignore parse errors
    }
  }

  if (options?.yieldToUi) {
    await yieldToEventLoop();
  }
}

function finalizeStreamState(
  state: StreamState,
  streamCallbacks?: StreamCallbacks,
): ChatCompletionResult {
  streamCallbacks?.onComplete?.(
    state.fullContent,
    state.thinkingContent || undefined,
  );

  return {
    content: state.fullContent,
    thinkingContent: state.thinkingContent || undefined,
  };
}

function normalizeAssistantContent(content: ChatMessageContent): string {
  if (typeof content === "string") {
    return content;
  }

  return content
    .filter(
      (part): part is Extract<ChatMessageContentPart, { type: "text" }> =>
        part.type === "text",
    )
    .map((part) => part.text)
    .join("");
}

function toAnthropicMessageContent(
  content: ChatMessageContent,
): string | AnthropicMessageContentPart[] {
  if (typeof content === "string") {
    return content;
  }

  const parts = content.flatMap((part): AnthropicMessageContentPart[] => {
    if (part.type === "text") {
      return [{ type: "text", text: part.text }];
    }

    if (part.type === "image_url") {
      const match = part.image_url.url.match(/^data:(.+?);base64,(.+)$/);
      if (!match) {
        return [];
      }

      return [
        {
          type: "image",
          source: {
            type: "base64",
            media_type: match[1],
            data: match[2],
          },
        },
      ];
    }

    return [];
  });

  return parts.length > 0 ? parts : "";
}

async function getErrorMessageFromResponse(
  response: ResponseLike,
): Promise<string> {
  return getFormattedErrorMessageFromResponse(response);
}

/**
 * 调用 AI 模型进行对话（支持流式输出和思考模型）
 * Call AI model for chat (supports streaming and thinking models)
 */
export async function chatCompletion(
  config: AIConfig,
  messages: ChatMessage[],
  options?: ChatCompletionOptions,
): Promise<ChatCompletionResult> {
  const { provider, apiKey, apiUrl, model, chatParams } = config;
  const providerId = provider?.toLowerCase() || "";
  const protocol = resolveAIProtocol(config);
  const isGemini = protocol === "gemini";
  const isAnthropic = protocol === "anthropic";
  const normalizedModel = isGemini ? model.replace(/^models\//, "") : model;

  if (!apiKey) {
    throw new Error("API Key is not configured");
  }

  if (!apiUrl) {
    throw new Error("API URL is not configured");
  }

  if (!model) {
    throw new Error("No model selected");
  }

  const endpoint = buildChatEndpointFromBase(
    resolveProtocolBase(apiUrl, protocol),
  );

  // 合并参数：config.chatParams < options（options 优先级更高）
  // Merge parameters: config.chatParams < options (options takes precedence)
  const mergedParams = {
    temperature: options?.temperature ?? chatParams?.temperature ?? 0.7,
    maxTokens: options?.maxTokens ?? chatParams?.maxTokens ?? 2048,
    topP: options?.topP ?? chatParams?.topP,
    topK: options?.topK ?? chatParams?.topK,
    frequencyPenalty: options?.frequencyPenalty ?? chatParams?.frequencyPenalty,
    presencePenalty: options?.presencePenalty ?? chatParams?.presencePenalty,
    stream: options?.stream ?? chatParams?.stream ?? false,
    enableThinking:
      options?.enableThinking ?? chatParams?.enableThinking ?? false,
  };

  if (isAnthropic) {
    mergedParams.stream = false;
  }

  // 构建请求头 / Build request headers
  const headers = buildHeadersForProtocol(protocol, apiKey, {
    accept: mergedParams.stream ? "text/event-stream" : "application/json",
  });

  // 检测是否为需要 max_completion_tokens 的新模型
  // Detect if it's a new model that requires max_completion_tokens
  // Updated for Issue #21: Support automatic fallback/retry for token parameters
  const modelLower = model.toLowerCase();
  let useMaxCompletionTokens =
    modelLower.includes("o1") ||
    modelLower.includes("o3") ||
    modelLower.includes("gpt-4o") ||
    modelLower.includes("gpt-4.5") ||
    /gpt-[5-9]/.test(modelLower) || // Matches gpt-5, gpt-5.2, gpt-6, etc.
    providerId.includes("openai");

  // 构建请求体 / Build request body
  const body: ChatCompletionRequest = {
    model: normalizedModel,
    messages,
    temperature: mergedParams.temperature,
    stream: mergedParams.stream,
  };

  if (isAnthropic) {
    const anthropicMessages = messages
      .filter((message) => message.role !== "system")
      .map((message) => ({
        role: message.role === "assistant" ? "assistant" : "user",
        content: toAnthropicMessageContent(message.content),
      }));

    const anthropicBody: Record<string, unknown> = {
      model,
      max_tokens: mergedParams.maxTokens,
      messages: anthropicMessages,
      stream: false,
    };

    const systemMessage = messages.find((message) => message.role === "system");
    if (systemMessage) {
      anthropicBody.system = normalizeAssistantContent(systemMessage.content);
    }

    const requestBody = JSON.stringify(anthropicBody);
    const transport = getAITransport();
    const response = transport
      ? createResponseLike(
          await transport.request({
            method: "POST",
            url: endpoint,
            headers,
            body: requestBody,
            timeoutMs: options?.timeoutMs,
          }),
        )
      : createFetchResponseLike(
          await fetch(endpoint, {
            method: "POST",
            headers,
            body: requestBody,
          }),
        );

    if (!response.ok) {
      throw new Error(await getErrorMessageFromResponse(response));
    }

    const data = await response.json<{
      content?: Array<{ type?: string; text?: string }>;
    }>();
    const content = (data.content || [])
      .filter((item) => item?.type === "text" && typeof item.text === "string")
      .map((item) => item.text)
      .join("");

    if (!content) {
      throw new Error("AI returned an unexpected response format");
    }

    return {
      content,
    };
  }

  // 根据模型类型选择正确的 token 限制参数
  // Choose the correct token limit parameter based on model type
  if (useMaxCompletionTokens) {
    body.max_completion_tokens = mergedParams.maxTokens;
  } else {
    body.max_tokens = mergedParams.maxTokens;
  }

  // 添加可选参数 / Add optional parameters
  if (mergedParams.topP !== undefined) {
    body.top_p = mergedParams.topP;
  }
  if (mergedParams.topK !== undefined) {
    body.top_k = mergedParams.topK;
  }
  if (!isGemini && mergedParams.frequencyPenalty !== undefined) {
    body.frequency_penalty = mergedParams.frequencyPenalty;
  }
  if (!isGemini && mergedParams.presencePenalty !== undefined) {
    body.presence_penalty = mergedParams.presencePenalty;
  }

  // 检测是否为 Qwen 模型 / Detect if Qwen model
  const isQwen =
    providerId.includes("qwen") ||
    providerId.includes("dashscope") ||
    model.toLowerCase().includes("qwen");

  // 处理思考模式 / Handle thinking mode
  // 只有在流式模式下才能启用思考，非流式必须禁用
  if (isQwen) {
    if (mergedParams.stream && mergedParams.enableThinking) {
      body.enable_thinking = true;
    } else {
      body.enable_thinking = false;
    }
  } else if (mergedParams.enableThinking) {
    // 其他支持思考的模型（如 DeepSeek）
    body.enable_thinking = true;
  }

  // 处理自定义参数 / Handle custom parameters
  const customParams = chatParams?.customParams;
  if (customParams && typeof customParams === "object") {
    const bodyAny = body as unknown as Record<string, unknown>;
    for (const [key, value] of Object.entries(customParams)) {
      if (key && value !== undefined && value !== "") {
        bodyAny[key] = value;
      }
    }
  }

  // 处理输出格式 / Handle response format (Issue #38)
  if (options?.responseFormat && options.responseFormat.type !== "text") {
    if (options.responseFormat.type === "json_object") {
      body.response_format = { type: "json_object" };
    } else if (
      options.responseFormat.type === "json_schema" &&
      options.responseFormat.jsonSchema
    ) {
      body.response_format = {
        type: "json_schema",
        json_schema: {
          name: options.responseFormat.jsonSchema.name,
          strict: options.responseFormat.jsonSchema.strict ?? true,
          schema: options.responseFormat.jsonSchema.schema,
        },
      };
    }
  }

  const transport = getAITransport();

  const sendRequest = async (): Promise<{
    streamResult?: ChatCompletionResult;
    response?: ResponseLike;
  }> => {
    const requestBody = JSON.stringify(body);

    if (mergedParams.stream && transport) {
      const streamState = createStreamState();
      let streamError: string | null = null;

      const response = await transport.requestStream(
        {
          method: "POST",
          url: endpoint,
          headers,
          body: requestBody,
          timeoutMs: options?.timeoutMs,
        },
        {
          onChunk: (chunk) => {
            void processStreamTextChunk(
              chunk,
              streamState,
              options?.onStream,
              options?.streamCallbacks,
            );
          },
          onError: (error) => {
            streamError = error;
          },
        },
      );

      if (!response.ok) {
        return { response: createResponseLike(response) };
      }

      if (streamError) {
        throw new Error(streamError);
      }

      await processStreamTextChunk(
        "",
        streamState,
        options?.onStream,
        options?.streamCallbacks,
        { flush: true },
      );

      return {
        streamResult: finalizeStreamState(
          streamState,
          options?.streamCallbacks,
        ),
      };
    }

    if (transport) {
      const response = await transport.request({
        method: "POST",
        url: endpoint,
        headers,
        body: requestBody,
        timeoutMs: options?.timeoutMs,
      });
      return { response: createResponseLike(response) };
    }

    const response = await fetch(endpoint, {
      method: "POST",
      headers,
      body: requestBody,
    });

    if (mergedParams.stream) {
      return {
        streamResult: await handleStreamResponse(
          response,
          options?.onStream,
          options?.streamCallbacks,
        ),
      };
    }

    return { response: createFetchResponseLike(response) };
  };

  try {
    let requestResult = await sendRequest();
    let response = requestResult.response;

    if (response && !response.ok) {
      const errorMessage = await getErrorMessageFromResponse(response);

      // Check for token parameter compatibility issues (Issue #21)
      // 检查 Token 参数兼容性问题
      const isTokenParamError =
        errorMessage.includes("'max_tokens' is not supported") ||
        errorMessage.includes("'max_completion_tokens' is not supported") ||
        errorMessage.includes("Use 'max_completion_tokens' instead") ||
        errorMessage.includes("Use 'max_tokens' instead");

      // Check for enable_thinking compatibility issues (Issue #9)
      // 检查 enable_thinking 参数兼容性问题 (Issue #9)
      const isThinkingParamError =
        errorMessage.includes("enable_thinking must be set to false") ||
        errorMessage.includes("enable_thinking only support stream") ||
        errorMessage.includes("parameter.enable_thinking");

      if (isTokenParamError) {
        console.warn(
          `[AI Service] Token parameter mismatch detected: "${errorMessage}". Retrying with alternative parameter...`,
        );

        if (useMaxCompletionTokens) {
          delete body.max_completion_tokens;
          body.max_tokens = mergedParams.maxTokens;
        } else {
          delete body.max_tokens;
          body.max_completion_tokens = mergedParams.maxTokens;
        }

        requestResult = await sendRequest();
        response = requestResult.response;

        if (response && !response.ok) {
          throw new Error(await getErrorMessageFromResponse(response));
        }
      } else if (isThinkingParamError) {
        console.warn(
          `[AI Service] enable_thinking parameter error detected: "${errorMessage}". Retrying with enable_thinking=false...`,
        );

        body.enable_thinking = false;
        requestResult = await sendRequest();
        response = requestResult.response;

        if (response && !response.ok) {
          throw new Error(await getErrorMessageFromResponse(response));
        }
      } else {
        throw new Error(errorMessage);
      }
    }

    if (requestResult.streamResult) {
      return requestResult.streamResult;
    }

    if (!response) {
      throw new Error("AI 返回结果为空");
    }

    // 非流式响应 / Non-streaming response
    const data: ChatCompletionResponse = await response.json();

    if (!data.choices || data.choices.length === 0) {
      throw new Error("AI 返回结果为空");
      // AI returned empty result
    }

    const message = data.choices[0].message;
    return {
      content: normalizeAssistantContent(message.content),
      thinkingContent: message.reasoning_content,
    };
  } catch (error) {
    if (error instanceof Error) {
      throw error;
    }
    throw new Error("网络请求失败，请检查网络连接");
    // Network request failed, please check network connection
  }
}

/**
 * 处理流式响应
 * Handle streaming response
 */
async function handleStreamResponse(
  response: Response,
  onStream?: (chunk: string) => void,
  streamCallbacks?: StreamCallbacks,
): Promise<ChatCompletionResult> {
  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error("无法读取响应流");
    // Cannot read response stream
  }

  const decoder = new TextDecoder();
  const state = createStreamState();

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }

      await processStreamTextChunk(
        decoder.decode(value, { stream: true }),
        state,
        onStream,
        streamCallbacks,
        { yieldToUi: true },
      );
    }

    await processStreamTextChunk(
      decoder.decode(),
      state,
      onStream,
      streamCallbacks,
      { flush: true, yieldToUi: true },
    );
  } finally {
    reader.releaseLock();
  }

  return finalizeStreamState(state, streamCallbacks);
}

/**
 * 测试 AI 配置是否可用（带详细结果，支持流式输出）
 * Test AI configuration (with detailed results, supports streaming)
 */
export async function testAIConnection(
  config: AIConfig,
  testPrompt?: string,
  streamCallbacks?: StreamCallbacks,
): Promise<AITestResult> {
  const startTime = Date.now();
  const prompt = testPrompt || AI_CONNECTION_TEST_PROMPT;

  // 连接测试只验证端点和模型能否响应，不能继承长文本生成参数。
  // A connection test is a lightweight probe, not a full generation benchmark.

  try {
    const result = await chatCompletion(
      config,
      [{ role: "user", content: prompt }],
      {
        temperature: 0,
        maxTokens: AI_CONNECTION_TEST_MAX_TOKENS,
        stream: false,
        enableThinking: false,
        streamCallbacks,
        timeoutMs: AI_CONNECTION_TEST_TIMEOUT_MS,
      },
    );

    return {
      id: config.id,
      success: true,
      response: result.content,
      thinkingContent: result.thinkingContent,
      latency: Date.now() - startTime,
      model: config.model,
      provider: config.provider,
    };
  } catch (error) {
    return {
      id: config.id,
      success: false,
      error: error instanceof Error ? error.message : "未知错误",
      latency: Date.now() - startTime,
      model: config.model,
      provider: config.provider,
    };
  }
}

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
