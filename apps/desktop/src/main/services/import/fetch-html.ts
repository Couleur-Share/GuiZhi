/**
 * 抓取网页 HTML：SSRF 防护（每跳重定向都校验）、超时、大小上限。
 * 复用 image.ipc.ts 同款的 http/https + 代理 agent 模式。
 */
import * as http from "http";
import * as https from "https";
import { getHttpRequestAgent } from "../network-proxy";
import { isBlockedHostname, resolvePublicAddress } from "../net-safety";

const FETCH_TIMEOUT_MS = 30_000;
const FETCH_MAX_BYTES = 10 * 1024 * 1024;
const FETCH_MAX_REDIRECTS = 5;
const USER_AGENT =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36 GuiZhi/0.3";

export interface FetchHtmlResult {
  finalUrl: string;
  html: string;
  contentType: string;
}

async function assertSafeTarget(parsed: URL): Promise<void> {
  if (!["http:", "https:"].includes(parsed.protocol)) {
    throw new Error(`不支持的协议: ${parsed.protocol}`);
  }
  const host = parsed.hostname.toLowerCase();
  if (isBlockedHostname(host)) {
    throw new Error("不允许访问本地网络地址");
  }
  await resolvePublicAddress(host);
}

function requestOnce(
  targetUrl: string,
  signal: AbortSignal | undefined,
): Promise<{ statusCode: number; headers: http.IncomingHttpHeaders; body: Buffer; }> {
  return new Promise((resolve, reject) => {
    const parsed = new URL(targetUrl);
    const requestModule = parsed.protocol === "https:" ? https : http;
    const request = requestModule.request(
      {
        protocol: parsed.protocol,
        hostname: parsed.hostname,
        port: parsed.port || undefined,
        path: `${parsed.pathname}${parsed.search}`,
        method: "GET",
        headers: {
          "User-Agent": USER_AGENT,
          Accept:
            "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
          "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
        },
        agent: getHttpRequestAgent(parsed),
        timeout: FETCH_TIMEOUT_MS,
      },
      (response) => {
        const chunks: Buffer[] = [];
        let total = 0;
        response.on("data", (chunk: Buffer) => {
          total += chunk.length;
          if (total > FETCH_MAX_BYTES) {
            request.destroy(new Error("页面超过大小上限"));
            return;
          }
          chunks.push(chunk);
        });
        response.on("end", () => {
          resolve({
            statusCode: response.statusCode ?? 0,
            headers: response.headers,
            body: Buffer.concat(chunks),
          });
        });
        response.on("error", reject);
      },
    );

    request.on("timeout", () => {
      request.destroy(new Error("请求超时"));
    });
    request.on("error", reject);
    if (signal) {
      const onAbort = () => request.destroy(new Error("已取消"));
      if (signal.aborted) {
        onAbort();
        return;
      }
      signal.addEventListener("abort", onAbort, { once: true });
      request.on("close", () => signal.removeEventListener("abort", onAbort));
    }
    request.end();
  });
}

export async function fetchHtml(
  rawUrl: string,
  signal?: AbortSignal,
): Promise<FetchHtmlResult> {
  let currentUrl = rawUrl;

  for (let redirect = 0; redirect <= FETCH_MAX_REDIRECTS; redirect++) {
    const parsed = new URL(currentUrl);
    await assertSafeTarget(parsed);

    const response = await requestOnce(currentUrl, signal);

    if (
      response.statusCode >= 300 &&
      response.statusCode < 400 &&
      response.headers.location
    ) {
      currentUrl = new URL(response.headers.location, currentUrl).toString();
      continue;
    }

    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw new Error(`HTTP ${response.statusCode}`);
    }

    const contentType = String(response.headers["content-type"] ?? "");
    if (contentType && !/text\/html|application\/xhtml/i.test(contentType)) {
      throw new Error(`不是网页内容: ${contentType.split(";")[0]}`);
    }

    return {
      finalUrl: currentUrl,
      html: decodeHtmlBody(response.body, contentType),
      contentType,
    };
  }

  throw new Error("重定向次数过多");
}

/** 按 Content-Type / meta charset 解码（默认 UTF-8，常见 GBK 站点回退处理）。 */
function decodeHtmlBody(body: Buffer, contentType: string): string {
  const headerCharset = /charset=([\w-]+)/i.exec(contentType)?.[1];
  const utf8Text = body.toString("utf8");
  const metaCharset =
    /<meta[^>]+charset=["']?([\w-]+)/i.exec(utf8Text.slice(0, 2048))?.[1];
  const charset = (headerCharset || metaCharset || "utf-8").toLowerCase();

  if (charset === "utf-8" || charset === "utf8") {
    return utf8Text;
  }
  try {
    return new TextDecoder(charset).decode(body);
  } catch {
    return utf8Text;
  }
}
