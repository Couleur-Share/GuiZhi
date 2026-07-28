import { ipcMain } from "electron";
import * as dns from "dns/promises";
import * as nodeNet from "net";

import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  AITransportRequest,
  AITransportResponse,
  AIUsageRecordInput,
} from "@guizhi/shared/types";
import { AIUsageDB } from "@guizhi/db";
import type Database from "../database/sqlite";
import { fetchWithNetworkProxy } from "../services/network-proxy";
import { isForbiddenAIEndpointAddress, isPrivateAddress } from "../services/net-safety";
import { assertEndpointAllowed } from "../services/ai-endpoint-guard";

/**
 * 校验 AI 接口地址。
 *
 * 这条通道此前把渲染进程给的 URL 原样交给 fetch，等于一个不设防的
 * HTTP 代理：响应体（含全部响应头）会完整回传，可以用来探测内网。
 *
 * 但它和网页抓取不同——本地 Ollama / LM Studio、局域网推理服务都是常规
 * 用法，不能套用「一律禁私网」。这里挡的是没有任何合法 AI 用途的目标：
 * 非 http(s) 协议，以及 link-local（云元数据）、组播、保留段地址。
 * 域名同样解析后校验，避免用一个指向 169.254.169.254 的域名绕过。
 */
async function assertSafeAIEndpoint(rawUrl: string): Promise<void> {
  let parsed: URL;
  try {
    parsed = new URL(rawUrl);
  } catch {
    throw new Error(`无效的接口地址: ${rawUrl}`);
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error(`不支持的协议: ${parsed.protocol}`);
  }

  const host = parsed.hostname.toLowerCase().replace(/^\[|\]$/g, "");
  const addresses = nodeNet.isIP(host)
    ? [host]
    : (await dns.lookup(host, { all: true })).map((entry) => entry.address);

  for (const address of addresses) {
    if (isForbiddenAIEndpointAddress(address)) {
      throw new Error(`不允许访问该地址: ${parsed.host}`);
    }
  }

  // 已配置端点与回环放行，未知目标限速——挡住「注入后静默扫内网」
  assertEndpointAllowed(rawUrl);

  // 私网端点是合法用法，但值得留痕——排查「AI 请求打到奇怪地方」时用得上
  if (addresses.some(isPrivateAddress)) {
    console.log(`[ai] 请求私网端点: ${parsed.protocol}//${parsed.host}`);
  }
}

function normalizeHeaders(headers?: Record<string, string>): HeadersInit | undefined {
  if (!headers) {
    return undefined;
  }
  return Object.fromEntries(
    Object.entries(headers).filter(([, value]) => value != null),
  );
}

function headersToObject(headers: Headers): Record<string, string> {
  const result: Record<string, string> = {};
  headers.forEach((value, key) => {
    result[key] = value;
  });
  return result;
}

function toErrorResponse(error: unknown): AITransportResponse {
  return {
    ok: false,
    status: 0,
    statusText: "",
    body: "",
    headers: {},
    error: error instanceof Error ? error.message : "Unknown error",
  };
}

async function requestToResponse(response: Response): Promise<AITransportResponse> {
  return {
    ok: response.ok,
    status: response.status,
    statusText: response.statusText,
    body: await response.text(),
    headers: headersToObject(response.headers),
  };
}

/** 在途请求：requestId → controller，供渲染进程按 id 中断 */
const inflightRequests = new Map<string, AbortController>();

function trackRequest(
  requestId: string | undefined,
  controller: AbortController,
): () => void {
  if (!requestId) {
    return () => {};
  }
  inflightRequests.set(requestId, controller);
  return () => {
    if (inflightRequests.get(requestId) === controller) {
      inflightRequests.delete(requestId);
    }
  };
}

interface StartedRequest {
  response: Response;
  /** 响应体读完后调用，撤掉超时计时器 */
  finish: () => void;
}

async function performRequest(
  request: AITransportRequest,
  controller: AbortController,
): Promise<StartedRequest> {
  await assertSafeAIEndpoint(request.url);

  const timeoutMs =
    typeof request.timeoutMs === "number" && request.timeoutMs > 0
      ? request.timeoutMs
      : 30_000;
  const timeoutId = setTimeout(() => {
    controller.abort(new Error(`AI 请求超时（${timeoutMs}ms）`));
  }, timeoutMs);

  try {
    const response = await fetchWithNetworkProxy(request.url, {
      method: request.method,
      headers: normalizeHeaders(request.headers),
      body: request.body,
      signal: controller.signal,
    });
    // 计时器要留到响应体读完才撤：fetch resolve 只代表响应头到达，
    // provider 发完 200 头就卡住时（中转站限流排队很常见），
    // 后面的 response.text() / 流读取没有任何超时保护，会永久挂起。
    return { response, finish: () => clearTimeout(timeoutId) };
  } catch (error) {
    clearTimeout(timeoutId);
    throw error;
  }
}

export function registerAIIPC(db: Database.Database): void {
  const usage = new AIUsageDB(db);

  // 渲染进程点「停止」时按 requestId 中断在途请求
  ipcMain.on(IPC_CHANNELS.AI_HTTP_CANCEL, (_event, requestId: unknown) => {
    if (typeof requestId !== "string") {
      return;
    }
    inflightRequests.get(requestId)?.abort(new Error("已取消"));
  });

  ipcMain.handle(
    IPC_CHANNELS.AI_USAGE_RECORD,
    (_event, entry: AIUsageRecordInput) => {
      if (
        !entry ||
        typeof entry.scenario !== "string" ||
        typeof entry.model !== "string"
      ) {
        return;
      }
      usage.record({
        scenario: entry.scenario,
        model: entry.model,
        promptTokens: Number(entry.promptTokens) || 0,
        completionTokens: Number(entry.completionTokens) || 0,
        // 渲染进程一直在传 failed，这里此前漏接了：failed_calls 列、
        // DAO 的入参、迁移 0005 三样都齐备，唯独统计出来恒为 0
        failed: entry.failed === true,
      });
    },
  );

  ipcMain.handle(IPC_CHANNELS.AI_USAGE_SUMMARY, (_event, days: unknown) =>
    usage.summary(Math.min(Math.max(1, Number(days) || 30), 90)),
  );

  ipcMain.handle(IPC_CHANNELS.AI_USAGE_CLEAR, () => usage.clear());

  ipcMain.handle(
    IPC_CHANNELS.AI_HTTP_REQUEST,
    async (_event, request: AITransportRequest): Promise<AITransportResponse> => {
      const controller = new AbortController();
      const release = trackRequest(request.requestId, controller);
      try {
        const { response, finish } = await performRequest(request, controller);
        try {
          return await requestToResponse(response);
        } finally {
          finish();
        }
      } catch (error) {
        return toErrorResponse(error);
      } finally {
        release();
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.AI_HTTP_STREAM,
    async (event, request: AITransportRequest): Promise<AITransportResponse> => {
      const controller = new AbortController();
      const release = trackRequest(request.requestId, controller);
      let finishTimeout: (() => void) | null = null;
      try {
        const started = await performRequest(request, controller);
        const response = started.response;
        finishTimeout = started.finish;
        if (!response.ok || !response.body) {
          try {
            return await requestToResponse(response);
          } finally {
            finishTimeout();
          }
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();

        try {
          while (true) {
            const { done, value } = await reader.read();
            if (done) {
              break;
            }

            const chunk = decoder.decode(value, { stream: true });
            if (chunk) {
              event.sender.send(IPC_CHANNELS.AI_HTTP_STREAM_CHUNK, {
                requestId: request.requestId,
                chunk,
              });
            }
          }

          const tail = decoder.decode();
          if (tail) {
            event.sender.send(IPC_CHANNELS.AI_HTTP_STREAM_CHUNK, {
              requestId: request.requestId,
              chunk: tail,
            });
          }
        } catch (error) {
          const message =
            error instanceof Error ? error.message : "Unknown stream error";
          event.sender.send(IPC_CHANNELS.AI_HTTP_STREAM_ERROR, {
            requestId: request.requestId,
            error: message,
          });
          return toErrorResponse(error);
        } finally {
          reader.releaseLock();
        }

        return {
          ok: true,
          status: response.status,
          statusText: response.statusText,
          body: "",
          headers: headersToObject(response.headers),
        };
      } catch (error) {
        const message =
          error instanceof Error ? error.message : "Unknown stream error";
        event.sender.send(IPC_CHANNELS.AI_HTTP_STREAM_ERROR, {
          requestId: request.requestId,
          error: message,
        });
        return toErrorResponse(error);
      } finally {
        finishTimeout?.();
        release();
      }
    },
  );
}
