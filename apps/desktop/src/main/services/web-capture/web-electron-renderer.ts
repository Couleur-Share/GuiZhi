import { BrowserWindow, session, type Session } from "electron";
import { randomUUID } from "node:crypto";
import type { WebCaptureRequest } from "@guizhi/shared/types";
import { inWebScope } from "@guizhi/shared/utils/web-scope";
import { showWindowOffscreen } from "../../testing/window-mode";
import { webNetworkRequest, type WebCaptureNet } from "./web-network";
import { webCaptureError } from "./web-error";
import { captureRenderedDocument } from "./web-rendered-document";
import {
  inertImageResponse,
  inertResourceMode,
  isInertImageRequest,
} from "./web-inert-resources";
import { WebNetworkActivity } from "./web-network-activity";
import { pageSettleEnv, settleRenderedPage } from "./web-settle";
import { shouldBlockSubframe } from "./web-subframe-policy";
import { NOOP_WEB_TRACE, classifyAccept } from "./web-trace";
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

/** 单页请求数上限；超过即终止采集，防止页面无限发请求。 */
const MAX_REQUESTS = 200;

/** 请求数最多的前三个主机，如“www.youtube.com ×94、www.gstatic.com ×43”。只含主机，不含路径与参数。 */
function summarizeHosts(hosts: Map<string, number>): string {
  return [...hosts]
    .sort((left, right) => right[1] - left[1])
    .slice(0, 3)
    .map(([host, count]) => `${host} ×${count}`)
    .join("、");
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
    options: WebCaptureNet = {},
  ): Promise<RenderedWebPage> {
    return this.pages.run(signal, () => {
      options.trace?.mark("renderGate");
      return this.renderOwned(request, signal, options);
    });
  }

  private async renderOwned(
    request: WebCaptureRequest,
    signal: AbortSignal,
    options: WebCaptureNet,
  ): Promise<RenderedWebPage> {
    const trace = options.trace ?? NOOP_WEB_TRACE;
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
    trace.mark("slot");
    const target = slot.session;
    const controller = new AbortController();
    const combined = AbortSignal.any([signal, controller.signal]);
    const responses = new Map<
      string,
      { status: number; contentType: string }
    >();
    const navigations = new Set([request.url]);
    const pending = new Set<Promise<Response>>();
    // 子请求活动（样式表、图片、文档类预取/子框架不算）供沉降等待判断“网络是否已静止”。
    const activity = new WebNetworkActivity();
    let requests = 0,
      bytes = 0;
    // 按主机统计请求数：预算用尽时据此说明是谁在消耗，区分页面本身与第三方内容。
    const hosts = new Map<string, number>();
    // 被取消的跨源子框架数，写入追踪便于判断页面是否受此策略影响。
    let blockedFrames = 0;
    // 惰性图片：以占位图本地应答的请求数（见 web-inert-resources.ts）。
    const inertImages = inertResourceMode() === "stub";
    let inertStubs = 0;
    let fatal: Error | undefined;
    let failure: unknown;
    let result: RenderedWebPage | undefined;
    let window: BrowserWindow | undefined;
    const abortWindow = () => {
      if (window && !window.isDestroyed()) window.destroy();
    };
    const denyDownload = (event: Electron.Event) => event.preventDefault();
    const serve = (incoming: Request): Promise<Response> => {
      // protocol.handle 接管后拿不到 webRequest 的资源类型，只能按 Accept 粗分；
      // 沉降等待与追踪共用这一分类。
      const kind = classifyAccept(incoming.headers.get("accept"));
      // 惰性图片不出网（理由见 web-inert-resources.ts），因此也不占请求数预算与网络槽位。
      if (inertImages && isInertImageRequest(incoming.method, kind)) {
        inertStubs++;
        return Promise.resolve(inertImageResponse(incoming.headers));
      }
      const host = new URL(incoming.url).host;
      hosts.set(host, (hosts.get(host) ?? 0) + 1);
      // 入队前计数，避免页面一次发起大量请求时先堆满等待队列。
      if (++requests > MAX_REQUESTS) {
        fatal ??= new Error(
          `页面网络预算已用尽（超过 ${MAX_REQUESTS} 个请求，主要来源：${summarizeHosts(hosts)}）`,
        );
        controller.abort();
        return Promise.reject(fatal);
      }
      const work = this.network
        .run(combined, async (waitedMs) => {
          const response = await webNetworkRequest(
            {
              url: incoming.url,
              method: incoming.method,
              headers: Object.fromEntries(incoming.headers.entries()),
              body: await limitedBody(incoming),
            },
            combined,
            {
              pool: options.pool,
              trace,
              resourceType: trace.enabled ? kind : undefined,
              gateWaitMs: waitedMs,
            },
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
      const ended = activity.begin(
        kind,
        trace.enabled ? new URL(incoming.url).origin : undefined,
      );
      const finished = () => {
        pending.delete(work);
        ended();
      };
      void work.then(finished, finished);
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
      // 被下面的 protocol.handle 接管的 http(s) 请求（含主框架导航）也会先经过该监听
      // （Electron 45 实测），所以它必须放行 http(s)，由协议处理器统一做出口检查。
      // WebSocket 与非 http(s) 请求不经协议处理器，在这里一律取消；黑洞代理是第二层兜底
      // （监听失效时 ws/wss 也到不了本机或外网）。
      target.webRequest.onBeforeRequest((details, callback) => {
        callback({
          cancel:
            !/^https?:\/\//i.test(details.url) ||
            details.resourceType === "webSocket",
        });
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
      // 导航检查放在导航事件上：它先于取页面触发，被取消的目标不会被请求，更不会执行。
      // 事件不对程序化的 loadURL 触发（入口已由调用方校验），但对脚本跳转、点击、meta 刷新、
      // 子框架加载与服务端重定向（含入口自身的重定向）都会触发。
      const pageContents = window.webContents;
      const guardNavigation = (
        event: Electron.Event<{ url: string; isMainFrame: boolean }>,
      ) => {
        if (!/^https?:\/\//i.test(event.url)) return;
        if (!event.isMainFrame) {
          // 子框架：取消跨源的，理由见 web-subframe-policy.ts；这不是错误，采集照常进行。
          if (shouldBlockSubframe(event.url, pageContents.getURL())) {
            event.preventDefault();
            blockedFrames++;
          }
          return;
        }
        // 主框架：越界即取消并终止采集。
        if (request.scope && !inWebScope(event.url, request.scope)) {
          event.preventDefault();
          fatal ??= new Error("导航超出允许范围", {
            cause: { webCaptureCode: "security" },
          });
          controller.abort();
          return;
        }
        // 主框架导航取页失败必须终止采集，不能把浏览器错误页当作正文。
        navigations.add(event.url);
      };
      pageContents.on("will-frame-navigate", guardNavigation);
      pageContents.on("will-redirect", guardNavigation);
      if (trace.enabled) {
        // 诊断：主框架 DOMContentLoaded 的时刻，对照 load 阶段可看出子资源把 load 拖后了多少。
        const windowAt = performance.now();
        pageContents.once("dom-ready", () =>
          trace.set({ domReadyMs: Math.round(performance.now() - windowAt) }),
        );
      }
      combined.addEventListener("abort", abortWindow, { once: true });
      trace.mark("window");
      await withWebAbort(window.loadURL(request.url), combined);
      trace.mark("load");
      showWindowOffscreen(window);
      // 事件驱动沉降：网络、DOM、正文都静止即提前结束，上限与原先的固定等待相同。
      trace.set({
        settle: await settleRenderedPage(
          pageSettleEnv({
            evaluate: (script) =>
              withWebAbort(pageContents.executeJavaScript(script), combined),
            pause: (ms) => webPause(ms, combined),
            networkQuietMs: () => activity.quietMs(),
            inflight: () => activity.inflight(),
            blockers: () => activity.blockers(),
          }),
          combined,
        ),
      });
      trace.mark("settle");
      const snapshot = await withWebAbort(
        window.webContents.executeJavaScript(
          `(${captureRenderedDocument.toString()})()`,
        ),
        combined,
      );
      trace.mark("snapshot");
      if (snapshot.flattenedShadowRoots)
        trace.set({ shadowRoots: snapshot.flattenedShadowRoots });
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
      trace.mark("links");
      result = { html: snapshot.html, url, status: response.status, links };
    } catch (error) {
      failure = fatal ?? error;
      trace.mark("error");
    } finally {
      combined.removeEventListener("abort", abortWindow);
      controller.abort();
      abortWindow();
      if (blockedFrames) trace.set({ blockedFrames });
      if (inertStubs) trace.set({ inertStubs });
      await Promise.allSettled([...pending]);
      trace.mark("drain");
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
      trace.mark("cleanup");
      slot.busy = false;
      if (slot.broken) failure ??= new Error("网页会话清理失败，请重启归知后重试");
    }
    // 清理期间仍在收尾的请求也可能报告安全拒绝，不能把先取得的快照当作成功。
    if (fatal) throw fatal;
    if (failure) throw failure;
    return result!;
  }
}
