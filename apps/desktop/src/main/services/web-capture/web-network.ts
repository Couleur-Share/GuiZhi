import * as http from "node:http";
import * as https from "node:https";
import { brotliDecompressSync, gunzipSync, inflateSync } from "node:zlib";
import { HttpProxyAgent } from "http-proxy-agent";
import { HttpsProxyAgent } from "https-proxy-agent";
import { SocksProxyAgent } from "socks-proxy-agent";
import {
  getActiveNetworkProxySettings,
  getHttpRequestAgent,
  resolveManualProxy,
} from "../network-proxy";
import { assertSafeTarget } from "../import/safe-fetch";
import { resolvePublicAddress } from "../net-safety";
import { canonicalWebUrl } from "@guizhi/shared/utils/web-scope";
import type { WebConnectionPool } from "./web-connection-pool";
import { webTextHeaders } from "./web-request-identity";
import type { WebTrace } from "./web-trace";

export const WEB_RESPONSE_LIMIT = 10 * 1024 * 1024;
export interface WebNetworkRequest {
  url: string;
  method?: string;
  headers?: Record<string, string>;
  body?: string;
}
export interface WebNetworkResponse {
  status: number;
  headers: Record<string, string>;
  body: string;
}

/** 调用方可选提供的上下文；全部缺省时与引入这些选项之前的行为一致。 */
export interface WebNetworkOptions {
  /** 一次采集内的连接复用范围；缺省沿用逐请求建连的原行为。 */
  pool?: WebConnectionPool;
  /** 诊断追踪（默认关闭），以及调用方已知的资源类型与槽位排队时间。 */
  trace?: WebTrace;
  resourceType?: string;
  gateWaitMs?: number;
}

/** 一次采集内各阶段（静态抓取、渲染子请求）共享的网络上下文。 */
export type WebCaptureNet = Pick<WebNetworkOptions, "pool" | "trace">;

export async function proxyAgent(
  url: URL,
  pool?: WebConnectionPool,
): Promise<http.Agent | undefined> {
  if (getActiveNetworkProxySettings().mode !== "system") {
    const manual = pool ? resolveManualProxy(url) : null;
    // SOCKS 代理的 keepAlive 行为未验证，沿用共享 agent 的原行为。
    if (pool && manual && !manual.socks)
      return pool.agentFor(manual.proxyUrl, url.protocol === "https:");
    return getHttpRequestAgent(url) as http.Agent | undefined;
  }
  const { session } = await import("electron");
  const proxy = (await session.defaultSession.resolveProxy(url.href))
    .split(";")[0]
    .trim();
  if (proxy === "DIRECT") return undefined;
  const match = /^(PROXY|HTTPS|SOCKS|SOCKS5)\s+([^\s]+)$/.exec(proxy);
  if (!match) throw new Error("无法解析系统代理配置，未回退直连");
  if (match[1].startsWith("SOCKS"))
    return new SocksProxyAgent(
      `socks5h://${match[2]}`,
    ) as unknown as http.Agent;
  const address = `${match[1] === "HTTPS" ? "https" : "http"}://${match[2]}`;
  // 系统代理原先每个请求新建 agent；有连接范围时按代理地址复用，随采集结束销毁。
  if (pool) return pool.agentFor(address, url.protocol === "https:");
  return (
    url.protocol === "https:"
      ? new HttpsProxyAgent(address)
      : new HttpProxyAgent(address)
  ) as http.Agent;
}

/** 单次尝试的观测状态：供复用连接的重试判断与追踪使用。 */
interface AttemptState {
  reused: boolean;
  responded: boolean;
  socketAt: number;
  bytes: number;
}

/** 追踪用的失败归类：优先错误码，其次错误名；本模块自造的超时错误无码，按消息归为 TIMEOUT。 */
function errorLabel(error: unknown): string {
  const failure = error as NodeJS.ErrnoException | undefined;
  if (failure?.code) return String(failure.code).slice(0, 32);
  if (failure?.name && failure.name !== "Error")
    return failure.name.slice(0, 32);
  return /超时/.test(failure?.message ?? "") ? "TIMEOUT" : "Error";
}

const IDEMPOTENT_METHODS = ["GET", "HEAD", "OPTIONS"];
/** 复用的连接被服务端关闭时，Node 报 ECONNRESET（或写入时报 EPIPE）。 */
const isStaleSocketError = (error: unknown): boolean =>
  ["ECONNRESET", "EPIPE"].includes(
    (error as NodeJS.ErrnoException | undefined)?.code ?? "",
  );

/** 一次请求一份公网解析结果；重定向由调用方重新走此入口。代理失败不降级直连。 */
export async function webNetworkRequest(
  input: WebNetworkRequest,
  signal: AbortSignal,
  opts: WebNetworkOptions = {},
): Promise<WebNetworkResponse> {
  const { trace } = opts;
  const tracing = trace?.enabled === true;
  const enteredAt = tracing ? performance.now() : 0;
  const url = new URL(canonicalWebUrl(input.url));
  const method = input.method ?? "GET";
  if (!["GET", "HEAD", "POST", "OPTIONS"].includes(method))
    throw new Error("网页请求方法不受支持");
  const pool = opts.pool?.enabled ? opts.pool : undefined;
  const agent = await proxyAgent(url, pool);
  await assertSafeTarget(url, true);
  const host = url.hostname.replace(/^\[|\]$/g, "");
  // 实际直连时不允许 fake-IP，即使系统配置了代理但该 URL 被规则设为 DIRECT。
  const pinned = agent ? null : await resolvePublicAddress(host);
  signal.throwIfAborted();
  const body = input.body ? Buffer.from(input.body, "base64") : undefined;
  if (body && body.length > 1024 * 1024)
    throw new Error("网页请求体超过 1 MiB");
  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(input.headers ?? {})) {
    if (
      !/^(host|connection|content-length|proxy-.*|authorization|upgrade|transfer-encoding)$/i.test(
        key,
      ) &&
      !/[\r\n]/.test(value)
    )
      headers[key] = value;
  }
  headers.host = url.host;
  headers["accept-encoding"] = "gzip, deflate, br";
  // 复用连接可能已被服务端关闭。对幂等请求，仅当复用的连接在收到响应前被重置时重试 1 次
  //（Node 文档给出的做法）；新建连接上的失败、POST、已开始响应的请求都不重试。
  // 取消与超时的错误不是 ECONNRESET/EPIPE，自然不会进入重试。
  const canRetry =
    pool !== undefined &&
    agent !== undefined &&
    IDEMPOTENT_METHODS.includes(method);
  const observe = tracing || canRetry;
  const startedAt = tracing ? performance.now() : 0;
  const send = (attempt: AttemptState) =>
    new Promise<WebNetworkResponse>((resolve, reject) => {
      const request = (url.protocol === "https:" ? https : http).request(
        {
          protocol: url.protocol,
          hostname: pinned?.address ?? host,
          family: pinned?.family,
          servername: url.protocol === "https:" ? host : undefined,
          port: url.port || undefined,
          path: url.pathname + url.search,
          method,
          headers,
          agent,
          signal,
          timeout: 30_000,
        },
        (response) => {
          attempt.responded = true;
          const chunks: Buffer[] = [];
          let size = 0;
          response.on("data", (chunk: Buffer) => {
            size += chunk.length;
            if (size > WEB_RESPONSE_LIMIT)
              response.destroy(new Error("网页响应超过 10 MiB"));
            else chunks.push(chunk);
          });
          response.on("error", reject);
          response.on("end", () => {
            try {
              let data = Buffer.concat(chunks);
              const encoding = response.headers["content-encoding"];
              const options = { maxOutputLength: WEB_RESPONSE_LIMIT };
              if (encoding === "gzip") data = gunzipSync(data, options);
              else if (encoding === "br")
                data = brotliDecompressSync(data, options);
              else if (encoding === "deflate")
                data = inflateSync(data, options);
              else if (encoding && encoding !== "identity")
                throw new Error("不支持的响应压缩格式");
              if (data.length > WEB_RESPONSE_LIMIT)
                throw new Error("解压后的网页超过 10 MiB");
              const output: Record<string, string> = {};
              for (const [key, value] of Object.entries(response.headers)) {
                if (
                  value &&
                  !/^(content-encoding|content-length|transfer-encoding|connection|alt-svc)$/i.test(
                    key,
                  )
                )
                  output[key] = Array.isArray(value) ? value.join("\n") : value;
              }
              attempt.bytes = data.length;
              resolve({
                status: response.statusCode ?? 502,
                headers: output,
                body: data.toString("base64"),
              });
            } catch (error) {
              reject(error);
            }
          });
        },
      );
      if (observe)
        request.on("socket", () => {
          attempt.socketAt = performance.now();
          attempt.reused = request.reusedSocket === true;
        });
      request.on("timeout", () => request.destroy(new Error("网页请求超时")));
      request.on("error", reject);
      request.end(body);
    });
  let retried = false;
  for (;;) {
    const attempt: AttemptState = {
      reused: false,
      responded: false,
      socketAt: 0,
      bytes: 0,
    };
    const attemptAt = tracing ? performance.now() : 0;
    // 追踪只在开启时记录；耗时从首次发出请求算起，重试时包含失败的那一次。
    const record = (failure?: unknown) => {
      if (!trace?.enabled) return;
      trace.set({ proxy: getActiveNetworkProxySettings().mode });
      trace.egress({
        type: opts.resourceType ?? "other",
        method,
        origin: url.origin,
        responded: attempt.responded,
        prepMs: startedAt - enteredAt,
        gateWaitMs: opts.gateWaitMs ?? 0,
        socketMs: attempt.socketAt ? attempt.socketAt - attemptAt : 0,
        requestMs: performance.now() - startedAt,
        reused: attempt.reused,
        retried,
        failed: failure !== undefined,
        ...(failure !== undefined ? { error: errorLabel(failure) } : {}),
        bytes: attempt.bytes,
      });
    };
    try {
      const response = await send(attempt);
      record();
      return response;
    } catch (error) {
      if (
        canRetry &&
        !retried &&
        attempt.reused &&
        !attempt.responded &&
        isStaleSocketError(error)
      ) {
        retried = true;
        continue;
      }
      record(error ?? "unknown");
      throw error;
    }
  }
}

export async function webTextRequest(
  raw: string,
  signal: AbortSignal,
  allowed: (url: string) => boolean,
): Promise<{ status: number; text: string; url: string }> {
  let url = canonicalWebUrl(raw);
  // robots.txt、sitemap 与渲染窗口同一身份，否则按客户端特征拒绝请求的站点会让批次因“规则读取失败”暂停。
  const headers = await webTextHeaders();
  for (let hop = 0; hop <= 5; hop++) {
    if (!allowed(url)) throw new Error("重定向超出允许范围");
    const response = await webNetworkRequest({ url, headers }, signal);
    if (
      [301, 302, 303, 307, 308].includes(response.status) &&
      response.headers.location
    ) {
      url = canonicalWebUrl(new URL(response.headers.location, url).href);
      continue;
    }
    return {
      status: response.status,
      text: Buffer.from(response.body, "base64").toString("utf8"),
      url,
    };
  }
  throw new Error("网页重定向超过 5 次");
}
