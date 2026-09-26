import { BrowserWindow, session, type Session } from "electron";
import { randomUUID } from "node:crypto";
import type { WebCaptureRequest } from "@guizhi/shared/types";
import { inWebScope } from "@guizhi/shared/utils/web-scope";
import { showWindowOffscreen } from "../../testing/window-mode";
import { webNetworkRequest } from "./web-network";
import { webCaptureError } from "./web-error";
import { captureRenderedDocument } from "./web-rendered-document";
import {
  WebTaskGate,
  withWebAbort,
  webPause,
  webAbortError,
} from "./web-task-gate";

interface Slot {
  session: Session;
  busy: boolean;
  broken: boolean;
}

// Session 比单页存活更久；回调必须在模块作用域创建，避免 V8 的共享闭包
// 上下文把最后一页的窗口、请求、响应表和 AbortController 一起保留。
const denyPermissionRequest: NonNullable<
  Parameters<Session["setPermissionRequestHandler"]>[0]
> = (_contents, _permission, callback) => callback(false);
const denyPermissionCheck = () => false;
export interface RenderedWebPage {
  html: string;
  url: string;
  status: number;
  links: string[];
}

async function limitedBody(request: Request): Promise<string | undefined> {
  if (["GET", "HEAD"].includes(request.method) || !request.body)
    return undefined;
  const reader = request.body.getReader();
  const chunks: Buffer[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > 1024 * 1024) throw new Error("网页请求体超过 1 MiB");
      chunks.push(Buffer.from(value));
    }
  } finally {
    await reader.cancel().catch(() => undefined);
  }
  return Buffer.concat(chunks, size).toString("base64");
}

/** 最多两个内存会话，清理成功后才复用，避免每页累积一个 Electron Session。 */
export class WebElectronRenderer {
  private slots: Slot[] = [];
  private pages = new WebTaskGate(2);
  constructor(private readonly network: WebTaskGate) {}

  render(
    request: WebCaptureRequest,
    signal: AbortSignal,
  ): Promise<RenderedWebPage> {
    return this.pages.run(signal, () => this.renderOwned(request, signal));
  }

  private async renderOwned(
    request: WebCaptureRequest,
    signal: AbortSignal,
  ): Promise<RenderedWebPage> {
    let slot = this.slots.find((item) => !item.busy && !item.broken);
    if (!slot && this.slots.length < 2) {
      slot = {
        session: session.fromPartition(`guizhi-web-${randomUUID()}`, {
          cache: false,
        }),
        busy: false,
        broken: false,
      };
      this.slots.push(slot);
    }
    if (!slot) throw new Error("网页会话清理失败，请重启归知后重试");
    slot.busy = true;
    const target = slot.session;
    const controller = new AbortController();
    const combined = AbortSignal.any([signal, controller.signal]);
    const responses = new Map<
      string,
      { status: number; contentType: string }
    >();
    const navigations = new Set([request.url]);
    const pending = new Set<Promise<Response>>();
    let requests = 0,
      bytes = 0;
    let fatal: Error | undefined;
    let failure: unknown;
    let result: RenderedWebPage | undefined;
    let window: BrowserWindow | undefined;
    const abortWindow = () => {
      if (window && !window.isDestroyed()) window.destroy();
    };
    const denyDownload = (event: Electron.Event) => event.preventDefault();
    const serve = (incoming: Request): Promise<Response> => {
      // 入队前计数，避免页面一次发起大量请求时先堆满等待队列。
      if (++requests > 200) {
        fatal ??= new Error("页面网络预算已用尽");
        controller.abort();
        return Promise.reject(fatal);
      }
      const work = this.network
        .run(combined, async () => {
          const response = await webNetworkRequest(
            {
              url: incoming.url,
              method: incoming.method,
              headers: Object.fromEntries(incoming.headers.entries()),
              body: await limitedBody(incoming),
            },
            combined,
          );
          const data = Buffer.from(response.body, "base64");
          bytes += data.length;
          if (bytes > 50 * 1024 * 1024)
            throw new Error("页面网络响应总量超过 50 MiB");
          responses.set(incoming.url, {
            status: response.status,
            contentType: response.headers["content-type"] ?? "",
          });
          const headers = new Headers();
          for (const [key, value] of Object.entries(response.headers))
            for (const part of value.split("\n")) headers.append(key, part);
          // 禁止网页创建 Service Worker/其他 Worker 绕过页面请求的生命周期。
          headers.append("content-security-policy", "worker-src 'none'");
          return new Response(
            [204, 205, 304].includes(response.status)
              ? null
              : new Uint8Array(data),
            { status: response.status, headers },
          );
        })
        .catch((error) => {
          const failure =
            error instanceof Error ? error : new Error("网页网络请求失败");
          if (
            navigations.has(incoming.url) ||
            ["security", "incomplete"].includes(webCaptureError(failure).code)
          ) {
            fatal ??= failure;
            controller.abort();
          }
          throw failure;
        });
      pending.add(work);
      void work.then(
        () => pending.delete(work),
        () => pending.delete(work),
      );
      return work;
    };
    try {
      // 所有未被安全 HTTP 出口接管的请求也只能到黑洞代理，禁止隐式直连。
      await withWebAbort(
        target.setProxy({
          proxyRules: "http=127.0.0.1:9;https=127.0.0.1:9",
          proxyBypassRules: "<-loopback>",
        }),
        combined,
      );
      target.setPermissionRequestHandler(denyPermissionRequest);
      target.setPermissionCheckHandler(denyPermissionCheck);
      target.on("will-download", denyDownload);
      target.webRequest.onBeforeRequest((details, callback) => {
        const main = details.resourceType === "mainFrame";
        if (main) navigations.add(details.url);
        const allowed =
          /^https?:\/\//i.test(details.url) &&
          details.resourceType !== "webSocket" &&
          (!main || !request.scope || inWebScope(details.url, request.scope));
        if (!allowed && main) {
          fatal ??= new Error("导航超出允许范围", {
            cause: { webCaptureCode: "security" },
          });
          controller.abort();
        }
        callback({ cancel: !allowed });
      });
      target.protocol.handle("http", serve);
      target.protocol.handle("https", serve);
      if (combined.aborted) throw webAbortError(combined);
      window = new BrowserWindow({
        width: 1024,
        height: 768,
        x: -10000,
        y: -10000,
        show: false,
        skipTaskbar: true,
        webPreferences: {
          session: target,
          sandbox: true,
          contextIsolation: true,
          nodeIntegration: false,
          webSecurity: true,
          backgroundThrottling: false,
        },
      });
      window.webContents.setWebRTCIPHandlingPolicy("disable_non_proxied_udp");
      window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
      combined.addEventListener("abort", abortWindow, { once: true });
      await withWebAbort(window.loadURL(request.url), combined);
      showWindowOffscreen(window);
      await webPause(1200, combined);
      for (let i = 0; i < 3; i++) {
        await withWebAbort(
          window.webContents.executeJavaScript(
            "window.scrollBy(0, Math.min(window.innerHeight, 1000))",
          ),
          combined,
        );
        await webPause(200, combined);
      }
      const snapshot = await withWebAbort(
        window.webContents.executeJavaScript(
          `(${captureRenderedDocument.toString()})()`,
        ),
        combined,
      );
      if (snapshot.error)
        throw new Error(snapshot.error, {
          cause: { webCaptureCode: "incomplete" },
        });
      if (
        typeof snapshot.html !== "string" ||
        Buffer.byteLength(snapshot.html) > 10 * 1024 * 1024
      )
        throw new Error("渲染后 HTML 超过 10 MiB");
      const url = window.webContents.getURL();
      if (request.scope && !inWebScope(url, request.scope))
        throw new Error("最终网址超出目录范围");
      const response = responses.get(url);
      if (!response) throw new Error("未取得网页主文档响应");
      if (/application\/(?:pdf|json|octet-stream)/i.test(response.contentType))
        throw new Error("入口返回非网页内容", {
          cause: { webCaptureCode: "incomplete" },
        });
      const links = await withWebAbort(
        window.webContents.executeJavaScript(
          "Array.from(document.querySelectorAll('a[href]')).slice(0, 2000).map(a => a.href).filter(url => /^https?:/.test(url))",
        ),
        combined,
      );
      result = { html: snapshot.html, url, status: response.status, links };
    } catch (error) {
      failure = fatal ?? error;
    } finally {
      combined.removeEventListener("abort", abortWindow);
      controller.abort();
      abortWindow();
      await Promise.allSettled([...pending]);
      target.protocol.unhandle("http");
      target.protocol.unhandle("https");
      target.webRequest.onBeforeRequest(null);
      target.removeListener("will-download", denyDownload);
      try {
        await target.closeAllConnections();
        await target.clearStorageData();
        await target.clearCache();
        await target.clearAuthCache();
      } catch {
        slot.broken = true;
      }
      slot.busy = false;
      if (slot.broken) failure ??= new Error("网页会话清理失败，请重启归知后重试");
    }
    // 清理期间仍在收尾的请求也可能报告安全拒绝，不能把先取得的快照当作成功。
    if (fatal) throw fatal;
    if (failure) throw failure;
    return result!;
  }
}
