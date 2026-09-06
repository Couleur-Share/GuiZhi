import * as http from "node:http";
import * as https from "node:https";
import { brotliDecompressSync, gunzipSync, inflateSync } from "node:zlib";
import { HttpProxyAgent } from "http-proxy-agent";
import { HttpsProxyAgent } from "https-proxy-agent";
import { SocksProxyAgent } from "socks-proxy-agent";
import {
  getActiveNetworkProxySettings,
  getHttpRequestAgent,
} from "../network-proxy";
import { assertSafeTarget } from "../import/safe-fetch";
import { resolvePublicAddress } from "../net-safety";
import { canonicalWebUrl } from "@guizhi/shared/utils/web-scope";

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

async function proxyAgent(url: URL): Promise<http.Agent | undefined> {
  if (getActiveNetworkProxySettings().mode !== "system")
    return getHttpRequestAgent(url) as http.Agent | undefined;
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
  return (
    url.protocol === "https:"
      ? new HttpsProxyAgent(address)
      : new HttpProxyAgent(address)
  ) as http.Agent;
}

/** 一次请求一份公网解析结果；重定向由调用方重新走此入口。代理失败不降级直连。 */
export async function webNetworkRequest(
  input: WebNetworkRequest,
  signal: AbortSignal,
): Promise<WebNetworkResponse> {
  const url = new URL(canonicalWebUrl(input.url));
  const method = input.method ?? "GET";
  if (!["GET", "HEAD", "POST", "OPTIONS"].includes(method))
    throw new Error("网页请求方法不受支持");
  const agent = await proxyAgent(url);
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
  return new Promise((resolve, reject) => {
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
            else if (encoding === "deflate") data = inflateSync(data, options);
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
    request.on("timeout", () => request.destroy(new Error("网页请求超时")));
    request.on("error", reject);
    request.end(body);
  });
}

export async function webTextRequest(
  raw: string,
  signal: AbortSignal,
  allowed: (url: string) => boolean,
): Promise<{ status: number; text: string; url: string }> {
  let url = canonicalWebUrl(raw);
  for (let hop = 0; hop <= 5; hop++) {
    if (!allowed(url)) throw new Error("重定向超出允许范围");
    const response = await webNetworkRequest({ url }, signal);
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
