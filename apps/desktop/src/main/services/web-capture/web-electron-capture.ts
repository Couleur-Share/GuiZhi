import type {
  ImportStage,
  WebCaptureRequest,
  WebCaptureResult,
} from "@guizhi/shared/types";
import { canonicalWebUrl, inWebScope } from "@guizhi/shared/utils/web-scope";
import { WebHtmlExtractor } from "./web-html-extractor";
import {
  WebElectronRenderer,
  type RenderedWebPage,
} from "./web-electron-renderer";
import { WebTaskGate, withWebAbort, webAbortError } from "./web-task-gate";
import { webNetworkRequest, type WebCaptureNet } from "./web-network";
import { WebConnectionPool } from "./web-connection-pool";
import { NOOP_WEB_TRACE, type WebTrace } from "./web-trace";
import { assessStaticPage, staticPageLinks, isWebVerificationPage } from "./web-static-route";
import { WEB_VERIFICATION_REQUIRED } from "@guizhi/shared/utils/web-verification";
import { captureWithFlareSolverr, getFlareSolverrSettings } from "./flaresolverr";
import { webCaptureError } from "./web-error";

async function solvePage(request: WebCaptureRequest, signal: AbortSignal, settings: ReturnType<typeof getFlareSolverrSettings>) {
  try { return await captureWithFlareSolverr(request, signal, settings); }
  catch (error) {
    const failure = webCaptureError(error);
    if (failure.code === "security" || failure.code === "canceled") throw error;
    throw new Error(`${WEB_VERIFICATION_REQUIRED}：${failure.message}。可重试自动验证或在采集设置中关闭后备服务后手动验证。`, { cause: error });
  }
}

export class ElectronWebCapture {
  private extractor = new WebHtmlExtractor();
  private network = new WebTaskGate(8);
  private renderer = new WebElectronRenderer(this.network);
  private pages = new WebTaskGate(2);
  private lifetime = new AbortController();
  private operations = new Set<Promise<unknown>>();
  private closing?: Promise<void>;
  get running(): boolean {
    return this.operations.size > 0 || this.extractor.running;
  }

  async capture(
    request: WebCaptureRequest,
    signal: AbortSignal,
    stage?: (stage: ImportStage) => void,
    trace: WebTrace = NOOP_WEB_TRACE,
  ): Promise<WebCaptureResult> {
    while (this.closing !== undefined) await withWebAbort(this.closing, signal);
    const combined = AbortSignal.any([signal, this.lifetime.signal]);
    // 本次采集独占的连接范围：静态抓取与渲染子请求共用，采集结束（含失败/取消）立即销毁，不跨采集共享。
    const net: WebCaptureNet = { pool: new WebConnectionPool(), trace };
    const work = this.pages.run(combined, async () => {
      trace.mark("pageGate");
      const entryUrl = canonicalWebUrl(request.url);
      const solver = getFlareSolverrSettings();
      stage?.(request.interactiveVerification ? "web-verifying" : "fetching");
      let page = request.interactiveVerification && solver.enabled
        ? await solvePage({ ...request, url: entryUrl }, combined, solver)
        : request.interactiveVerification
        ? await (await import("./web-verification-browser")).captureVerifiedWebPage({ ...request, url: entryUrl }, combined)
        : await this.staticPage({ ...request, url: entryUrl }, combined, net);
      trace.mark(request.interactiveVerification ? "verify" : "static");
      let engine = request.interactiveVerification ? solver.enabled ? "flaresolverr" : "verified-electron" : "static";
      if (!page) {
        page = await this.renderer.render(
          { ...request, url: entryUrl },
          combined,
          net,
        );
        engine = "electron";
      }
      if (isWebVerificationPage(page.html) && solver.enabled) {
        stage?.("web-verifying");
        page = await solvePage({ ...request, url: page.url }, combined, solver);
        engine = "flaresolverr";
        trace.mark("solver");
      }
      if (isWebVerificationPage(page.html)) throw new Error(
        `${WEB_VERIFICATION_REQUIRED}：网站需要网页验证，点击「验证并自动采集」后完成验证。`,
        { cause: { webCaptureCode: "captcha" } },
      );
      stage?.("extracting");
      let result = await this.extractor.extract(
        page.html,
        page.url,
        page.status,
        combined,
      );
      trace.mark("extract");
      if (engine === "static" && result.error?.code === "empty") {
        trace.set({ fallback: "empty" });
        stage?.("fetching");
        page = await this.renderer.render(
          { ...request, url: entryUrl },
          combined,
          net,
        );
        engine = "electron";
        if (isWebVerificationPage(page.html) && solver.enabled) {
          stage?.("web-verifying");
          page = await solvePage({ ...request, url: page.url }, combined, solver);
          engine = "flaresolverr";
          trace.mark("solver");
        }
        if (isWebVerificationPage(page.html)) throw new Error(
          `${WEB_VERIFICATION_REQUIRED}：网站需要网页验证，点击「验证并自动采集」后完成验证。`,
          { cause: { webCaptureCode: "captcha" } },
        );
        stage?.("extracting");
        result = await this.extractor.extract(
          page.html,
          page.url,
          page.status,
          combined,
        );
      }
      if (combined.aborted) throw webAbortError(combined);
      trace.set({ engine });
      return {
        ...result,
        taskId: request.taskId,
        entryUrl,
        finalUrl: page.url,
        links: page.links,
        capturedAt: Date.now(),
        engineVersion: `crawl4ai/0.9.3-${engine}`,
      };
    });
    this.operations.add(work);
    try {
      return await work;
    } finally {
      this.operations.delete(work);
      // work 结束时静态请求与渲染子请求都已收尾，此时销毁连接不会打断进行中的请求。
      net.pool?.close();
    }
  }

  private async staticPage(
    request: WebCaptureRequest,
    signal: AbortSignal,
    net: WebCaptureNet,
  ): Promise<RenderedWebPage | null> {
    let url = request.url;
    for (let hop = 0; hop <= 5; hop++) {
      if (request.scope && !inWebScope(url, request.scope))
        throw new Error("重定向超出目录范围");
      const response = await this.network.run(signal, (waitedMs) =>
        webNetworkRequest({ url }, signal, {
          ...net,
          resourceType: "document",
          gateWaitMs: waitedMs,
        }),
      );
      if (
        [301, 302, 303, 307, 308].includes(response.status) &&
        response.headers.location
      ) {
        url = canonicalWebUrl(new URL(response.headers.location, url).href);
        continue;
      }
      const contentType = response.headers["content-type"] ?? "";
      if (/application\/(?:pdf|json|octet-stream)/i.test(contentType))
        throw new Error("入口返回非网页内容", {
          cause: { webCaptureCode: "incomplete" },
        });
      let html: string;
      try {
        const charset =
          /charset=["']?([^;\s"']+)/i.exec(contentType)?.[1] ?? "utf-8";
        html = new TextDecoder(charset, { fatal: true }).decode(
          Buffer.from(response.body, "base64"),
        );
      } catch {
        return null;
      }
      const decision = assessStaticPage(html, response.status);
      net.trace?.set({
        route: {
          decision: decision.route,
          reasons: decision.reasons,
          textChars: decision.textChars,
          semanticRegions: decision.semanticRegions,
          hasScripts: decision.hasScripts,
        },
      });
      if (decision.route === "render") return null;
      return {
        html,
        url,
        status: response.status,
        links: staticPageLinks(html, url),
      };
    }
    throw new Error("网页重定向超过 5 次");
  }

  close(): Promise<void> {
    if (this.closing !== undefined) return this.closing;
    this.lifetime.abort();
    this.closing = (async () => {
      await Promise.allSettled([...this.operations]);
      await this.extractor.close();
    })().finally(() => {
      this.lifetime = new AbortController();
      this.closing = undefined;
    });
    return this.closing;
  }
}
