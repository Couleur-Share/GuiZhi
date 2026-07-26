import type { AIProtocol } from "@guizhi/shared/types";

export interface ChatImageAttachment {
  name?: string;
  mimeType: string;
  base64: string;
}

export type ChatMessageContentPart =
  | { type: "text"; text: string }
  | {
      type: "image_url";
      image_url: {
        url: string;
        detail?: "auto" | "low" | "high";
      };
    };

export type ChatMessageContent = string | ChatMessageContentPart[];

export interface ChatMessage {
  role: "system" | "user" | "assistant";
  content: ChatMessageContent;
}

export interface ChatCompletionRequest {
  messages: ChatMessage[];
  model: string;
  temperature?: number;
  max_tokens?: number;
  max_completion_tokens?: number;
  top_p?: number;
  top_k?: number;
  frequency_penalty?: number;
  presence_penalty?: number;
  stream?: boolean;
  enable_thinking?: boolean;
  response_format?: {
    type: "text" | "json_object" | "json_schema";
    json_schema?: {
      name: string;
      strict?: boolean;
      schema: Record<string, unknown>;
    };
  };
}

export interface ChatCompletionResponse {
  id: string;
  choices: {
    index: number;
    message: ChatMessage & { reasoning_content?: string };
    finish_reason: string;
    delta?: { content?: string; reasoning_content?: string };
  }[];
  usage?: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
}

export interface ChatParams {
  temperature?: number;
  maxTokens?: number;
  topP?: number;
  topK?: number;
  frequencyPenalty?: number;
  presencePenalty?: number;
  stream?: boolean;
  enableThinking?: boolean;
  customParams?: Record<string, string | number | boolean>;
}

export interface AIConfig {
  id?: string;
  provider: string;
  apiProtocol: AIProtocol;
  apiKey: string;
  apiUrl: string;
  model: string;
  chatParams?: ChatParams;
}

export interface StreamCallbacks {
  onContent?: (chunk: string) => void;
  onThinking?: (chunk: string) => void;
  onComplete?: (fullContent: string, thinkingContent?: string) => void;
}

/**
 * 传输层响应的统一形态。
 *
 * 桌面端经 IPC 拿到的是一个纯数据对象，web 回退路径拿到的是 fetch 的
 * Response；两者包成同一个接口，上层不必区分。
 */
export interface ResponseLike {
  ok: boolean;
  status: number;
  statusText: string;
  headers: Record<string, string>;
  text: () => Promise<string>;
  json: <T = unknown>() => Promise<T>;
  error?: string;
}

export interface ChatCompletionUsage {
  promptTokens: number;
  completionTokens: number;
}

export interface ChatCompletionResult {
  content: string;
  thinkingContent?: string;
  /** openai 系 finish_reason；anthropic 的 max_tokens 归一化为 "length" */
  finishReason?: string;
  /** provider 回报的 token 用量；流式与部分中转站不返回，此时为 undefined */
  usage?: ChatCompletionUsage;
}

export interface ChatCompletionOptions {
  temperature?: number;
  maxTokens?: number;
  topP?: number;
  topK?: number;
  frequencyPenalty?: number;
  presencePenalty?: number;
  stream?: boolean;
  enableThinking?: boolean;
  onStream?: (chunk: string) => void;
  streamCallbacks?: StreamCallbacks;
  responseFormat?: {
    type: "text" | "json_object" | "json_schema";
    jsonSchema?: {
      name: string;
      strict?: boolean;
      schema: Record<string, unknown>;
    };
  };
  timeoutMs?: number;
  /** 中断在途请求；桌面端经 requestId 转达到主进程 */
  signal?: AbortSignal;
  /**
   * 允许模型返回空正文。只有连通性测试用得上——几个 token 的探针本来就不足以
   * 让思考类模型产出正文，但连接确实是通的。业务调用一律按失败处理。
   */
  allowEmptyContent?: boolean;
}

export interface AITestResult {
  id?: string;
  success: boolean;
  response?: string;
  thinkingContent?: string;
  error?: string;
  latency?: number;
  model: string;
  provider: string;
}

export interface ModelInfo {
  id: string;
  name?: string;
  owned_by?: string;
  created?: number;
}

export interface FetchModelsResult {
  success: boolean;
  models: ModelInfo[];
  error?: string;
  reason?: "auth" | "network" | "unsupported" | "http" | "parse";
  endpoint?: string;
  status?: number;
}
