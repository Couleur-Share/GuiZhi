import { ipcRenderer } from "electron";

import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  AITransportRequest,
  AITransportResponse,
  AITransportStreamChunk,
  AITransportStreamError,
  AIUsageRecordInput,
  AIUsageSummary,
} from "@guizhi/shared/types";

function createRequestId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `ai-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export const aiApi = {
  /** 生成一个可用于 cancel 的请求 id */
  createRequestId,
  /** 中断在途请求；未带 requestId 发出的请求不可中断 */
  cancel: (requestId: string): void => {
    ipcRenderer.send(IPC_CHANNELS.AI_HTTP_CANCEL, requestId);
  },
  request: (request: AITransportRequest): Promise<AITransportResponse> =>
    ipcRenderer.invoke(IPC_CHANNELS.AI_HTTP_REQUEST, request),
  recordUsage: (entry: AIUsageRecordInput): Promise<void> =>
    ipcRenderer.invoke(IPC_CHANNELS.AI_USAGE_RECORD, entry),
  usageSummary: (days: number): Promise<AIUsageSummary> =>
    ipcRenderer.invoke(IPC_CHANNELS.AI_USAGE_SUMMARY, days),
  clearUsage: (): Promise<void> =>
    ipcRenderer.invoke(IPC_CHANNELS.AI_USAGE_CLEAR),
  requestStream: async (
    request: AITransportRequest,
    handlers?: {
      onChunk?: (chunk: string) => void;
      onError?: (error: string) => void;
    },
  ): Promise<AITransportResponse> => {
    const requestId = request.requestId || createRequestId();

    const chunkListener = (
      _event: Electron.IpcRendererEvent,
      payload: AITransportStreamChunk,
    ) => {
      if (payload.requestId !== requestId) {
        return;
      }
      handlers?.onChunk?.(payload.chunk);
    };

    const errorListener = (
      _event: Electron.IpcRendererEvent,
      payload: AITransportStreamError,
    ) => {
      if (payload.requestId !== requestId) {
        return;
      }
      handlers?.onError?.(payload.error);
    };

    ipcRenderer.on(IPC_CHANNELS.AI_HTTP_STREAM_CHUNK, chunkListener);
    ipcRenderer.on(IPC_CHANNELS.AI_HTTP_STREAM_ERROR, errorListener);

    try {
      return await ipcRenderer.invoke(IPC_CHANNELS.AI_HTTP_STREAM, {
        ...request,
        requestId,
      });
    } finally {
      ipcRenderer.removeListener(IPC_CHANNELS.AI_HTTP_STREAM_CHUNK, chunkListener);
      ipcRenderer.removeListener(IPC_CHANNELS.AI_HTTP_STREAM_ERROR, errorListener);
    }
  },
};
