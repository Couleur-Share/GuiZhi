/**
 * AI 请求的传输层。
 *
 * 桌面端的请求发生在主进程（绕开 CORS、复用代理配置），渲染进程只通过 IPC
 * 转交参数；纯 web 构建没有这条通道，回退到 fetch。两条路径包成同一个
 * ResponseLike，上层不必区分。
 */
import type { AITransportResponse } from "@guizhi/shared/types";
import type { ResponseLike } from "./ai-types";

export function getAITransport() {
  if (typeof window === "undefined") {
    return null;
  }
  return window.api?.ai ?? null;
}

export type AITransport = NonNullable<ReturnType<typeof getAITransport>>;

export function createResponseLike(
  response: AITransportResponse,
): ResponseLike {
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

export function createFetchResponseLike(response: Response): ResponseLike {
  return {
    ok: response.ok,
    status: response.status,
    statusText: response.statusText,
    headers: Object.fromEntries(response.headers.entries()),
    text: async () => response.text(),
    json: async <T = unknown>() => response.json() as Promise<T>,
  };
}

/**
 * 把渲染进程的 AbortSignal 接到主进程的在途请求上。
 *
 * 请求实际发生在主进程，signal 过不去，只能带一个 requestId 过去，
 * abort 时再用同一个 id 发取消指令。主进程中断后返回的是一个 error 响应
 * 而不是 reject，所以这里统一按 AbortError 抛出，交给上层识别为「已停止」。
 */
export async function withCancellation<T>(
  transport: AITransport,
  signal: AbortSignal | undefined,
  run: (requestId: string) => Promise<T>,
): Promise<T> {
  const requestId = transport.createRequestId();
  if (signal?.aborted) {
    throw new DOMException("已取消", "AbortError");
  }
  const onAbort = () => transport.cancel(requestId);
  signal?.addEventListener("abort", onAbort, { once: true });
  try {
    const result = await run(requestId);
    if (signal?.aborted) {
      throw new DOMException("已取消", "AbortError");
    }
    return result;
  } finally {
    signal?.removeEventListener("abort", onAbort);
  }
}

export async function requestAIEndpoint(request: {
  method: "GET" | "POST";
  url: string;
  headers: Record<string, string>;
  body?: string;
  timeoutMs?: number;
  signal?: AbortSignal;
}): Promise<ResponseLike> {
  const { signal, ...payload } = request;
  const transport = getAITransport();
  if (transport) {
    return createResponseLike(
      await withCancellation(transport, signal, (requestId) =>
        transport.request({ ...payload, requestId }),
      ),
    );
  }

  return createFetchResponseLike(
    await fetch(payload.url, {
      method: payload.method,
      headers: payload.headers,
      body: payload.body,
      signal,
    }),
  );
}
