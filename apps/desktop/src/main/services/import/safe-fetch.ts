/**
 * 采集链路的出站 HTTP：SSRF 防护（每跳重定向都校验）、代理 agent、
 * 超时与大小上限。网页采集把响应读成 HTML 文本，媒体采集流式落盘。
 *
 * 复用 image.ipc.ts 同款的 http/https + 代理 agent 模式。
 */
import { randomUUID } from "crypto";
import fs from "fs";
import * as http from "http";
import * as https from "https";
import * as nodeNet from "net";
import os from "os";
import path from "path";
import {
  getHttpRequestAgent,
  hasAnyProxyConfigured,
} from "../network-proxy";
import {
  isBlockedHostname,
  isPrivateAddress,
  resolvePublicAddress,
  type ResolvedAddress,
} from "../net-safety";

/** 无数据往来的最长等待；流式下载期间只要还在收字节就不会触发 */
const IDLE_TIMEOUT_MS = 30_000;
const HTML_MAX_BYTES = 10 * 1024 * 1024;
/** 论坛接口一次会吐出整帖回复，千楼长帖约 1~2MB */
const JSON_MAX_BYTES = 8 * 1024 * 1024;
const MAX_REDIRECTS = 5;
const DEFAULT_USER_AGENT =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36 GuiZhi/0.3";
const HTML_ACCEPT =
  "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8";
const JSON_ACCEPT = "application/json,text/plain;q=0.9,*/*;q=0.8";

export interface SafeRequestOptions {
  signal?: AbortSignal;
  /** 覆盖默认 UA（抖音分享页只在移动端 UA 下服务端渲染） */
  userAgent?: string;
  accept?: string;
  referer?: string;
  /** 原样写入 Cookie 头（NGA guestJs 握手） */
  cookie?: string;
}

export interface FetchHtmlResult {
  finalUrl: string;
  html: string;
  contentType: string;
}

/**
 * 校验目标并返回要钉扎的 IP。
 *
 * 走代理时不做 DNS 解析也不钉扎：目标域名由代理解析，本地解析结果与实际
 * 连接无关，钉扎反而会绕过代理按域名分流的规则。但「不解析域名」不等于
 * 「什么都不查」——URL 里直接写的字面 IP 与 DNS 无关，必须照常拦下，
 * 否则配了手动代理之后 http://192.168.1.1/ 和云元数据地址会被原样转发。
 */
export async function assertSafeTarget(
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

  // 方括号写法的 IPv6 字面量在 URL.hostname 里带着括号
  const literalHost = host.startsWith("[") && host.endsWith("]")
    ? host.slice(1, -1)
    : host;
  const isLiteralIp = nodeNet.isIP(literalHost) !== 0;

  if (viaProxy) {
    if (isLiteralIp && isPrivateAddress(literalHost)) {
      throw new Error("不允许访问内网地址");
    }
    return null;
  }

  // Clash / Surge 的 fake-ip 池就在 198.18/15，本地 DNS 在系统代理下拿到的
  // 全是这个段；一律拒绝会让网页导入不可用。但这只在确实存在代理配置时
  // 才该放行——没有代理却解析到 198.18，那就是一个真实的内网地址。
  try {
    return await resolvePublicAddress(literalHost, {
      allowProxyCompatibilityAddress: hasAnyProxyConfigured(),
    });
  } catch (error) {
    // resolvePublicAddress 的消息是英文的，而这里抛出的错误会原样进
    // 导入任务列表；翻成用户能据此行动的说法
    const message = error instanceof Error ? error.message : String(error);
    if (message.includes("internal network") || message.includes("local network")) {
      throw new Error("不允许访问内网地址", { cause: error });
    }
    if (message.includes("resolve")) {
      throw new Error(`无法解析域名：${literalHost}`, { cause: error });
    }
    throw error;
  }
}

/**
 * 发起单次请求，响应头到达即 resolve，响应体留给调用方消费。
 *
 * 响应头之后再发生的超时 / 取消 / 连接错误会转投到响应流上，
 * 让正在读流的一方拿到 error，而不是变成无人处理的请求错误。
 */
function openRequest(
  targetUrl: string,
  pinnedAddress: ResolvedAddress | null,
  options: SafeRequestOptions,
): Promise<http.IncomingMessage> {
  return new Promise((resolve, reject) => {
    const parsed = new URL(targetUrl);
    const isHttps = parsed.protocol === "https:";
    const requestModule = isHttps ? https : http;
    let response: http.IncomingMessage | null = null;

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
          "User-Agent": options.userAgent ?? DEFAULT_USER_AGENT,
          Accept: options.accept ?? HTML_ACCEPT,
          "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
          ...(options.referer ? { Referer: options.referer } : {}),
          ...(options.cookie ? { Cookie: options.cookie } : {}),
        },
        agent: getHttpRequestAgent(parsed),
        timeout: IDLE_TIMEOUT_MS,
      },
      (incoming) => {
        response = incoming;
        resolve(incoming);
      },
    );

    const fail = (error: Error) => {
      if (response) {
        response.destroy(error);
      } else {
        reject(error);
      }
    };

    request.on("timeout", () => request.destroy(new Error("请求超时")));
    request.on("error", fail);

    const signal = options.signal;
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

/**
 * 跟随重定向直到拿到最终响应，每一跳都重新做安全校验。
 * 消费在 consume 回调内完成，确保底层请求的错误仍能传到读流的一方。
 *
 * acceptStatuses：除 2xx 外也交给 consume 读 body 的状态码。
 * NGA 的 guestJs 挑战就是 403 正文里下发 cookie 值，丢掉 body 就没法握手。
 */
async function requestFollowingRedirects<T>(
  rawUrl: string,
  options: SafeRequestOptions & { acceptStatuses?: readonly number[] },
  consume: (
    response: http.IncomingMessage,
    finalUrl: string,
    statusCode: number,
  ) => Promise<T>,
): Promise<T> {
  let currentUrl = rawUrl;
  const acceptStatuses = options.acceptStatuses ?? [];

  for (let redirect = 0; redirect <= MAX_REDIRECTS; redirect++) {
    const parsed = new URL(currentUrl);
    const viaProxy = getHttpRequestAgent(parsed) !== undefined;
    const pinnedAddress = await assertSafeTarget(parsed, viaProxy);
    const response = await openRequest(currentUrl, pinnedAddress, options);
    const statusCode = response.statusCode ?? 0;

    if (statusCode >= 300 && statusCode < 400 && response.headers.location) {
      response.resume();
      currentUrl = new URL(response.headers.location, currentUrl).toString();
      continue;
    }
    const accepted =
      (statusCode >= 200 && statusCode < 300) ||
      acceptStatuses.includes(statusCode);
    if (!accepted) {
      response.resume();
      throw new Error(`HTTP ${statusCode}`);
    }
    return await consume(response, currentUrl, statusCode);
  }

  throw new Error("重定向次数过多");
}

/** 读取响应体；超过上限即中止 */
function readBody(
  response: http.IncomingMessage,
  maxBytes: number,
  onOverflow: string,
): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let total = 0;
    response.on("data", (chunk: Buffer) => {
      total += chunk.length;
      if (total > maxBytes) {
        response.destroy(new Error(onOverflow));
        return;
      }
      chunks.push(chunk);
    });
    response.on("end", () => resolve(Buffer.concat(chunks)));
    response.on("error", reject);
    response.on("aborted", () => reject(new Error("连接被中断")));
  });
}

export async function fetchHtml(
  rawUrl: string,
  signal?: AbortSignal,
  options: Omit<SafeRequestOptions, "signal"> = {},
): Promise<FetchHtmlResult> {
  return requestFollowingRedirects(
    rawUrl,
    { ...options, signal },
    async (response, finalUrl) => {
      const contentType = String(response.headers["content-type"] ?? "");
      if (contentType && !/text\/html|application\/xhtml/i.test(contentType)) {
        response.resume();
        throw new Error(`不是网页内容: ${contentType.split(";")[0]}`);
      }
      const body = await readBody(
        response,
        HTML_MAX_BYTES,
        "页面超过大小上限",
      );
      return { finalUrl, html: decodeHtmlBody(body, contentType), contentType };
    },
  );
}

export interface FetchRawTextResult {
  status: number;
  text: string;
  contentType: string;
  finalUrl: string;
  /** Set-Cookie 头拆成的 name=value（NGA 握手要 ngaPassportUid 等） */
  setCookies: string[];
}

export interface FetchRawTextOptions extends Omit<SafeRequestOptions, "signal"> {
  signal?: AbortSignal;
  /** 除 2xx 外也读 body 的状态码（NGA guestJs 挑战是 403） */
  acceptStatuses?: readonly number[];
  maxBytes?: number;
}

/**
 * 抓取任意文本响应。NGA 的 lite=js 既不是纯 JSON 也不是 HTML：
 * 正文是 `window.script_muti_get_var_store={...}`，编码常是 GBK，
 * 且 guestJs 握手必须读 403 body。
 */
export async function fetchRawText(
  rawUrl: string,
  options: FetchRawTextOptions = {},
): Promise<FetchRawTextResult> {
  const { signal, acceptStatuses, maxBytes, ...requestOptions } = options;
  return requestFollowingRedirects(
    rawUrl,
    { accept: "*/*", ...requestOptions, signal, acceptStatuses },
    async (response, finalUrl, statusCode) => {
      const contentType = String(response.headers["content-type"] ?? "");
      const body = await readBody(
        response,
        maxBytes ?? JSON_MAX_BYTES,
        "接口响应超过大小上限",
      );
      const rawCookies = response.headers["set-cookie"];
      const setCookies = (Array.isArray(rawCookies)
        ? rawCookies
        : rawCookies
          ? [rawCookies]
          : []
      )
        .map((line) => line.split(";")[0]?.trim())
        .filter((part): part is string => Boolean(part) && part.includes("="));
      return {
        status: statusCode,
        text: decodeTextBody(body, contentType),
        contentType,
        finalUrl,
        setCookies,
      };
    },
  );
}

/**
 * 抓取 JSON 接口（论坛采集走平台的只读 API）。
 * 与 fetchHtml 共用同一套 SSRF 防护与重定向校验，不要绕开它自己发请求。
 */
export async function fetchJson<T>(
  rawUrl: string,
  signal?: AbortSignal,
  options: Omit<SafeRequestOptions, "signal"> = {},
): Promise<T> {
  return requestFollowingRedirects(
    rawUrl,
    { accept: JSON_ACCEPT, ...options, signal },
    async (response) => {
      const contentType = String(response.headers["content-type"] ?? "");
      // 被登录墙或 WAF 拦下时返回的是 HTML，早点报错比让 JSON.parse 抛乱码强
      if (contentType && !/json|text\/plain/i.test(contentType)) {
        response.resume();
        throw new Error(`接口未返回 JSON: ${contentType.split(";")[0]}`);
      }
      const body = await readBody(
        response,
        JSON_MAX_BYTES,
        "接口响应超过大小上限",
      );
      try {
        return JSON.parse(body.toString("utf8")) as T;
      } catch {
        throw new Error("接口响应不是合法的 JSON");
      }
    },
  );
}

export interface DownloadOptions extends SafeRequestOptions {
  /** 超出即中止下载并清理临时目录 */
  maxBytes: number;
  /** 落盘文件名（含扩展名） */
  fileName: string;
}

/**
 * 流式下载到独立临时目录，返回目录与文件路径。
 * 调用方负责在用完后删除整个目录（失败时本函数已自行清理）。
 */
export async function downloadToTempFile(
  rawUrl: string,
  options: DownloadOptions,
): Promise<{ dir: string; filePath: string }> {
  const dir = path.join(
    os.tmpdir(),
    `guizhi-download-${randomUUID().slice(0, 8)}`,
  );
  const filePath = path.join(dir, options.fileName);
  fs.mkdirSync(dir, { recursive: true });

  try {
    await requestFollowingRedirects(
      rawUrl,
      { accept: "*/*", ...options },
      async (response) => {
        const contentType = String(response.headers["content-type"] ?? "");
        if (/text\/html/i.test(contentType)) {
          response.resume();
          throw new Error("下载地址返回的是网页而非媒体文件（可能已失效）");
        }
        await writeBodyToFile(response, filePath, options.maxBytes);
      },
    );
  } catch (error) {
    fs.rmSync(dir, { recursive: true, force: true });
    throw error;
  }

  return { dir, filePath };
}

function writeBodyToFile(
  response: http.IncomingMessage,
  filePath: string,
  maxBytes: number,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const stream = fs.createWriteStream(filePath);
    let total = 0;
    let failed = false;
    const fail = (error: Error) => {
      if (failed) {
        return;
      }
      failed = true;
      response.destroy();
      stream.destroy();
      reject(error);
    };

    response.on("data", (chunk: Buffer) => {
      total += chunk.length;
      if (total > maxBytes) {
        fail(new Error("文件超过大小上限"));
        return;
      }
      if (!stream.write(chunk)) {
        response.pause();
        stream.once("drain", () => response.resume());
      }
    });
    // end 之后才 stream.end()，让缓冲区落盘完成再兑现（close 会直接丢弃）
    response.on("end", () => {
      if (!failed) {
        stream.end(() => resolve());
      }
    });
    response.on("error", fail);
    response.on("aborted", () => fail(new Error("连接被中断")));
    stream.on("error", fail);
  });
}

/** 按 Content-Type / meta charset 解码（默认 UTF-8，常见 GBK 站点回退处理）。 */
function decodeHtmlBody(body: Buffer, contentType: string): string {
  return decodeTextBody(body, contentType, { peekMeta: true });
}

/**
 * 通用文本解码。NGA lite=js 响应里常带 `"encode":"gbk"`，
 * 按 UTF-8 硬解会得到一串乱码，JSON 解析随之失败。
 */
function decodeTextBody(
  body: Buffer,
  contentType: string,
  options: { peekMeta?: boolean } = {},
): string {
  const headerCharset = /charset=([\w-]+)/i.exec(contentType)?.[1];
  const latinPeek = body.toString("latin1").slice(0, 4096);
  const ngaEncode = /"encode"\s*:\s*"(gbk|gb2312|utf-8|utf8)"/i.exec(
    latinPeek,
  )?.[1];
  let metaCharset: string | undefined;
  if (options.peekMeta) {
    const utf8Peek = body.toString("utf8").slice(0, 2048);
    metaCharset =
      /<meta[^>]+charset=["']?([\w-]+)/i.exec(utf8Peek)?.[1];
  }
  const charset = (
    headerCharset ||
    ngaEncode ||
    metaCharset ||
    "utf-8"
  ).toLowerCase();

  if (charset === "utf-8" || charset === "utf8") {
    return body.toString("utf8");
  }
  const decoderCharset = charset === "gb2312" ? "gbk" : charset;
  try {
    return new TextDecoder(decoderCharset).decode(body);
  } catch {
    return body.toString("utf8");
  }
}
