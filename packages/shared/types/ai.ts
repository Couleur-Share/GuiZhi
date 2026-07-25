export interface AITransportRequest {
  /** 取消用的标识；流式与非流式都需要，缺省则该请求不可中断 */
  requestId?: string;
  method: "GET" | "POST";
  url: string;
  headers?: Record<string, string>;
  body?: string;
  timeoutMs?: number;
}

export type AIProtocol = "openai" | "gemini" | "anthropic";

export interface AITransportResponse {
  ok: boolean;
  status: number;
  statusText: string;
  body: string;
  headers: Record<string, string>;
  error?: string;
}

export interface AITransportStreamChunk {
  requestId: string;
  chunk: string;
}

export interface AITransportStreamError {
  requestId: string;
  error: string;
}
