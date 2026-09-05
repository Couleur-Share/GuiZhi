import https from "node:https";
import { resolvePublicAddress } from "../net-safety";
import { getHttpRequestAgent, hasAnyProxyConfigured } from "../network-proxy";
export class CaptureHttpError extends Error {
  constructor(public status: number, public retryAfter: number, message: string) { super(message); }
}
export function captureOrigin(value: string): string {
  const url = new URL(value);
  if (url.protocol !== "https:" || url.username || url.password || url.pathname !== "/" || url.search || url.hash) throw new Error("请输入 HTTPS 服务根地址");
  return url.origin;
}
export async function captureRequest<T>(origin: string, path: string, credential: string | undefined, body: unknown, method: string, signal: AbortSignal): Promise<T> {
  const url = new URL(path, captureOrigin(origin));
  const address = await resolvePublicAddress(url.hostname, { allowProxyCompatibilityAddress: hasAnyProxyConfigured() });
  signal.throwIfAborted();
  return new Promise<T>((resolve, reject) => {
    const payload = body === undefined ? undefined : JSON.stringify(body);
    const request = https.request(url, {
      method, signal, timeout: 15000, family: address.family, agent: getHttpRequestAgent(url),
      // DNS 校验结果固定到本次连接，重定向不跟随，凭证不转发给别的主机。
      lookup: (_host, _options, callback) => callback(null, address.address, address.family),
      headers: { "Content-Type": "application/json", "X-Guizhi-Protocol": "1", ...(payload === undefined ? {} : { "Content-Length": Buffer.byteLength(payload) }), ...(credential ? { Authorization: `Bearer ${credential}` } : {}) },
    }, response => {
      const chunks: Buffer[] = []; let bytes = 0;
      response.on("data", (chunk: Buffer) => { bytes += chunk.length; if (bytes > 4 * 1024 * 1024) response.destroy(new Error("手机收集响应过大")); else chunks.push(chunk); });
      response.on("error", () => reject(new Error("手机收集网络连接中断")));
      response.on("end", () => {
        const status = response.statusCode ?? 500;
        if (status < 200 || status >= 300) {
          const value = response.headers["retry-after"];
          const delay = value ? (/^\d+$/.test(value) ? Number(value) * 1000 : Math.max(0, Date.parse(value) - Date.now())) : 0;
          reject(new CaptureHttpError(status, Number.isFinite(delay) ? delay : 0, `手机收集请求失败（HTTP ${status}）`)); return;
        }
        try { resolve(JSON.parse(Buffer.concat(chunks).toString("utf8")) as T); } catch { reject(new Error("手机收集响应格式无效")); }
      });
    });
    request.on("timeout", () => request.destroy(new Error("手机收集请求超时")));
    request.on("error", () => reject(new Error(signal.aborted ? "手机收集请求已取消" : "手机收集网络不可用")));
    request.end(payload);
  });
}
