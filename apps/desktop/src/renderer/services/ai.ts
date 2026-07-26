import {
  buildChatEndpointFromBase,
  buildHeadersForProtocol,
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
  ResponseLike,
  StreamCallbacks,
} from "./ai-types";
import { getFormattedErrorMessageFromResponse } from "./ai-errors";
import {
  createStreamState,
  finalizeStreamState,
  handleStreamResponse,
  processStreamTextChunk,
} from "./ai-stream";
import {
  createFetchResponseLike,
  createResponseLike,
  getAITransport,
  withCancellation,
} from "./ai-transport";

export type * from "./ai-types";

export {
  getBaseUrl,
  normalizeApiUrlInput,
} from "@guizhi/shared/utils/ai-protocol";

// ai.ts 是 AI 能力的公开入口，拆分出去的模块仍从这里透出
export { fetchAvailableModels, getApiEndpointPreview } from "./ai-models";

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

const AI_CONNECTION_TEST_MAX_TOKENS = 8;
const AI_CONNECTION_TEST_TIMEOUT_MS = 12_000;
const AI_CONNECTION_TEST_PROMPT = "Reply with exactly: OK";

function normalizeAssistantContent(content: ChatMessageContent): string {
  if (typeof content === "string") {
    return content;
  }
  // provider 在没有正文时回 content: null 很常见，按空串处理，别让它撞成 TypeError
  if (!Array.isArray(content)) {
    return "";
  }

  return content
    .filter(
      (part): part is Extract<ChatMessageContentPart, { type: "text" }> =>
        part.type === "text",
    )
    .map((part) => part.text)
    .join("");
}

/**
 * 空正文一律按失败处理。
 *
 * 思考类模型会把 max_tokens 预算整个花在推理上：HTTP 200、choices 齐全，
 * 但 `finish_reason` 是 length、`content` 是空字符串，正文全在
 * `reasoning_content` 里且被截断。沿用「有 choices 就算成功」的判断，
 * 上层会把空结果当成有效输出静默落库（摘要就被写成空串），
 * 界面上既看不到结果也看不到报错。
 */
function assertNonEmptyContent(
  content: string,
  context: {
    finishReason?: string;
    hasThinking: boolean;
    maxTokens: number;
    allowEmpty?: boolean;
  },
): void {
  if (context.allowEmpty || content.trim()) {
    return;
  }

  if (context.finishReason === "length") {
    throw new Error(
      context.hasThinking
        ? `模型把 ${context.maxTokens} token 的输出预算全用在思考过程上（finish_reason=length），没有产出正文。请为该场景改用非思考模型。`
        : `模型输出在 ${context.maxTokens} token 上限处被截断，没有产出正文。`,
    );
  }

  throw new Error(
    `模型返回了空正文（finish_reason=${context.finishReason ?? "unknown"}）`,
  );
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
  const useMaxCompletionTokens =
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
      throw new Error(await getFormattedErrorMessageFromResponse(response));
    }

    const data = await response.json<{
      content?: Array<{ type?: string; text?: string }>;
      stop_reason?: string;
    }>();
    const content = (data.content || [])
      .filter((item) => item?.type === "text" && typeof item.text === "string")
      .map((item) => item.text)
      .join("");
    const finishReason =
      data.stop_reason === "max_tokens" ? "length" : data.stop_reason;

    assertNonEmptyContent(content, {
      finishReason,
      hasThinking: false,
      maxTokens: mergedParams.maxTokens,
      allowEmpty: options?.allowEmptyContent,
    });

    return {
      content,
      finishReason,
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

      const response = await withCancellation(
        transport,
        options?.signal,
        (requestId) =>
          transport.requestStream(
            {
              method: "POST",
              url: endpoint,
              headers,
              body: requestBody,
              timeoutMs: options?.timeoutMs,
              requestId,
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
          ),
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
      const response = await withCancellation(
        transport,
        options?.signal,
        (requestId) =>
          transport.request({
            method: "POST",
            url: endpoint,
            headers,
            body: requestBody,
            timeoutMs: options?.timeoutMs,
            requestId,
          }),
      );
      return { response: createResponseLike(response) };
    }

    const response = await fetch(endpoint, {
      method: "POST",
      headers,
      body: requestBody,
      signal: options?.signal,
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
      const errorMessage = await getFormattedErrorMessageFromResponse(response);

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
          throw new Error(await getFormattedErrorMessageFromResponse(response));
        }
      } else if (isThinkingParamError) {
        console.warn(
          `[AI Service] enable_thinking parameter error detected: "${errorMessage}". Retrying with enable_thinking=false...`,
        );

        body.enable_thinking = false;
        requestResult = await sendRequest();
        response = requestResult.response;

        if (response && !response.ok) {
          throw new Error(await getFormattedErrorMessageFromResponse(response));
        }
      } else {
        throw new Error(errorMessage);
      }
    }

    if (requestResult.streamResult) {
      // 流式没有 finish_reason 可读，只能凭「有没有攒到正文」判断
      assertNonEmptyContent(requestResult.streamResult.content, {
        hasThinking: Boolean(requestResult.streamResult.thinkingContent),
        maxTokens: mergedParams.maxTokens,
        allowEmpty: options?.allowEmptyContent,
      });
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

    const choice = data.choices[0];
    const message = choice.message;
    const content = normalizeAssistantContent(message.content);

    assertNonEmptyContent(content, {
      finishReason: choice.finish_reason,
      hasThinking: Boolean(message.reasoning_content),
      maxTokens: mergedParams.maxTokens,
      allowEmpty: options?.allowEmptyContent,
    });

    return {
      content,
      thinkingContent: message.reasoning_content,
      finishReason: choice.finish_reason,
      usage: data.usage
        ? {
            promptTokens: data.usage.prompt_tokens ?? 0,
            completionTokens: data.usage.completion_tokens ?? 0,
          }
        : undefined,
    };
  } catch (error) {
    if (error instanceof Error) {
      throw error;
    }
    throw new Error("网络请求失败，请检查网络连接", { cause: error });
    // Network request failed, please check network connection
  }
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
        // 探针只有几个 token，思考类模型必然答不出正文；这里验的是连通性
        allowEmptyContent: true,
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
