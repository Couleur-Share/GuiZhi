/**
 * 正文提取 worker 线程入口（单独打包为 out/web-extract-worker/web-extract-worker.js）。
 * 解析不可信 HTML 前先关闭本线程的网络能力；页面串行处理，异常只回报类型名。
 */
import { isMainThread, parentPort } from "node:worker_threads";
import dgram from "node:dgram";
import dns from "node:dns";
import http from "node:http";
import http2 from "node:http2";
import https from "node:https";
import net from "node:net";
import tls from "node:tls";
import { extractWebArticle } from "./web-extract-article";

export interface WebExtractRequest {
  v: 1;
  id: string;
  html: string;
  url: string;
  status: number;
}

const DENIED = "正文提取线程不得发起网络请求";

/** 只作用于本线程：worker 线程有独立的内置模块实例，主进程的网络不受影响。 */
export function forbidNetwork(): void {
  const deny = () => {
    throw new Error(DENIED);
  };
  const replace = (target: object, names: string[]) => {
    for (const name of names)
      if (typeof (target as Record<string, unknown>)[name] === "function")
        (target as Record<string, unknown>)[name] = deny;
  };
  // TCP 与 TLS 最终都经 net.Socket#connect；http(s) 客户端与内置 fetch 也落在这里。
  replace(net.Socket.prototype, ["connect"]);
  replace(net, ["connect", "createConnection"]);
  replace(tls, ["connect"]);
  replace(http, ["request", "get"]);
  replace(https, ["request", "get"]);
  replace(http2, ["connect"]);
  replace(dgram, ["createSocket"]);
  const lookups = ["lookup", "lookupService", "resolve", "resolve4", "resolve6", "resolveAny", "resolveCname", "resolveMx", "resolveNs", "resolveTxt", "resolveSrv", "resolvePtr", "resolveNaptr", "resolveSoa", "resolveCaa", "reverse"];
  replace(dns, lookups);
  replace(dns.promises, lookups);
  replace(globalThis, ["fetch", "WebSocket", "EventSource"]);
}

if (!isMainThread && parentPort) {
  const port = parentPort;
  forbidNetwork();
  port.on("message", (message: Partial<WebExtractRequest>) => {
    const id = typeof message?.id === "string" ? message.id : "";
    try {
      if (message?.v !== 1 || typeof message.html !== "string" || typeof message.url !== "string")
        throw new TypeError("正文提取请求无效");
      const { result } = extractWebArticle(message.html, message.url, Number(message.status) || 0);
      port.postMessage({ v: 1, id, result });
    } catch (error) {
      port.postMessage({ v: 1, id, error: error instanceof Error ? error.name : "Error" });
    }
  });
}
