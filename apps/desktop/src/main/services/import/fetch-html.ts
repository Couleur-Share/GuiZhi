/**
 * 抓取网页 HTML：SSRF 防护（每跳重定向都校验）、超时、大小上限。
 * 复用 image.ipc.ts 同款的 http/https + 代理 agent 模式。
 */
import * as http from "http";
import * as https from "https";
import { getHttpRequestAgent } from "../network-proxy";
import {
  isBlockedHostname,
  resolvePublicAddress,
  type ResolvedAddress,
} from "../net-safety";

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

/**
 * 校验目标并返回要钉扎的 IP。
 *
 * 走代理时返回 null：目标域名由代理解析，本地解析结果与实际连接无关，
 * 钉扎反而会绕过代理按域名分流的规则。
 */
async function assertSafeTarget(
  parsed: URL,
  viaProxy: boolean,
): Promise<ResolvedAddress | null> {
  if (!["http:", "https:"].includes(parsed.protocol)) {
    throw new Error(`不支持的协议: ${parsed.protocol}`);
  }
  const host = parsed.hostname.toLowerCase();
  if (isBlockedHostname(host)) {
    throw new Error("不允许访问本地网络地址");
  }
  if (viaProxy) {
    return null;
  }
  // Clash / Surge 的 fake-ip 池就在 198.18/15，国内用户开着系统代理时
  // 本地 DNS 拿到的全是这个段；一律拒绝会让网页导入完全不可用。
  return await resolvePublicAddress(host, {
    allowProxyCompatibilityAddress: true,
  });
}

function requestOnce(
  targetUrl: string,
  signal: AbortSignal | undefined,
  pinnedAddress: ResolvedAddress | null,
): Promise<{ statusCode: number; headers: http.IncomingHttpHeaders; body: Buffer; }> {
  return new Promise((resolve, reject) => {
    const parsed = new URL(targetUrl);
    const isHttps = parsed.protocol === "https:";
    const requestModule = isHttps ? https : http;
    const request = requestModule.request(
      {
        protocol: parsed.protocol,
        // 钉扎到刚校验过的 IP，堵住「校验后 DNS 再变」的 rebinding 窗口。
        // servername 保 SNI、Host 头保虚拟主机，站点侧行为不变。
        hostname: pinnedAddress ? pinnedAddress.address : parsed.hostname,
        family: pinnedAddress?.family,
        servername: isHttps ? parsed.hostname : undefined,
        port: parsed.port ? Number(parsed.port) : isHttps ? 443 : 80,
        path: `${parsed.pathname}${parsed.search}`,
        method: "GET",
        headers: {
          Host: parsed.host,
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
    const viaProxy = getHttpRequestAgent(parsed) !== undefined;
    const pinnedAddress = await assertSafeTarget(parsed, viaProxy);

    const response = await requestOnce(currentUrl, signal, pinnedAddress);

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
