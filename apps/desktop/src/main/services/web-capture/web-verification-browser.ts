import { BrowserWindow, session } from "electron";
import { randomUUID } from "node:crypto";
import type { WebCaptureRequest } from "@guizhi/shared/types";
import { captureRenderedDocument } from "./web-rendered-document";
import { createWebVerificationProxy } from "./web-verification-proxy";
import { webPause, withWebAbort, WebTaskGate } from "./web-task-gate";
import { showWindowOffscreen } from "../../testing/window-mode";
import type { RenderedWebPage } from "./web-electron-renderer";

/** 用户主动发起的验证；通过后直接保存当前 DOM，不再重发无 Cookie 请求。 */
const verificationGate = new WebTaskGate(1);
let verificationSession: Electron.Session | undefined;
let cleanupFailed = false;
async function clearVerificationSession(target: Electron.Session): Promise<void> {
  try {
    await target.closeAllConnections();
    await target.clearStorageData();
    await target.clearCache();
    await target.clearAuthCache();
  } catch (error) {
    cleanupFailed = true;
    throw new Error("网页验证会话清理失败，请重启归知后重试", { cause: error });
  }
}
export function captureVerifiedWebPage(
  request: WebCaptureRequest,
  signal: AbortSignal,
) {
  return verificationGate.run(signal, () => captureOwned(request, signal));
}
async function captureOwned(
  request: WebCaptureRequest,
  signal: AbortSignal,
): Promise<RenderedWebPage> {
  if (cleanupFailed) throw new Error("网页验证会话清理失败，请重启归知后重试");
  const entry = new URL(request.url);
  if (
    entry.protocol !== "https:" ||
    entry.username ||
    entry.password ||
    (entry.port && entry.port !== "443")
  )
    throw new Error("网页验证只支持 HTTPS 标准端口");
  const proxy = await createWebVerificationProxy(signal);
  const target = (verificationSession ??= session.fromPartition(
    `guizhi-verify-${randomUUID()}`,
    { cache: false },
  ));
  let window: BrowserWindow | undefined;
  let status = 0;
  const stop = () => {
    if (window && !window.isDestroyed()) window.destroy();
  };
  const denyDownload = (event: Electron.Event) => event.preventDefault();
  try {
    await withWebAbort(
      target.setProxy({
        proxyRules: `http=127.0.0.1:${proxy.port};https=127.0.0.1:${proxy.port}`,
        proxyBypassRules: "<-loopback>",
      }),
      signal,
    );
    target.setPermissionRequestHandler((_contents, _permission, callback) =>
      callback(false),
    );
    target.setPermissionCheckHandler(() => false);
    target.on("will-download", denyDownload);
    target.webRequest.onBeforeRequest((details, callback) => {
      let allowed = false;
      try {
        const url = new URL(details.url);
        allowed =
          url.protocol === "https:" &&
          !url.username &&
          !url.password &&
          (!url.port || url.port === "443") &&
          (details.resourceType !== "mainFrame" || url.origin === entry.origin);
        if (details.resourceType === "mainFrame") status = 0;
      } catch {
        /* 非 HTTP 资源不允许访问系统能力。 */
      }
      callback({ cancel: !allowed });
    });
    target.webRequest.onCompleted((details) => {
      if (details.resourceType === "mainFrame") status = details.statusCode;
    });
    window = new BrowserWindow({
      width: 1024,
      height: 768,
      show: false,
      title: "完成网页验证后，归知会自动继续采集",
      webPreferences: {
        session: target,
        sandbox: true,
        contextIsolation: true,
        nodeIntegration: false,
        webSecurity: true,
        backgroundThrottling: false,
      },
    });
    window.webContents.setUserAgent(
      window.webContents
        .getUserAgent()
        .replace(/\sElectron\/\S+/i, "")
        .replace(/\sGuiZhi\/\S+/i, ""),
    );
    window.webContents.setWebRTCIPHandlingPolicy("disable_non_proxied_udp");
    window.webContents.on("did-navigate", (_event, _url, responseCode) => {
      status = responseCode;
    });
    window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    signal.addEventListener("abort", stop, { once: true });
    if (signal.aborted) signal.throwIfAborted();
    if (process.env.GUIZHI_WINDOW_MODE === "offscreen")
      showWindowOffscreen(window);
    else window.show();
    await withWebAbort(window.loadURL(entry.href), signal);
    for (;;) {
      if (proxy.error) throw new Error(proxy.error);
      if (window.isDestroyed()) throw new Error("网页验证已取消");
      await webPause(1000, signal);
      if (window.isDestroyed()) throw new Error("网页验证已取消");
      if (window.webContents.isLoading()) continue;
      const state = await withWebAbort(
        window.webContents.executeJavaScript(`(() => ({
        title: document.title, challenge: !!document.querySelector("#challenge-stage, #cf-wrapper, script[src*='/orchestrate/chl_page/']"),
        text: (document.querySelector('article, main, [role="main"]') || document.body)?.innerText?.trim() || ""
      }))()`),
        signal,
      );
      if (
        state.challenge ||
        /^(just a moment|verify you are human|人机验证|安全验证)/i.test(
          state.title,
        ) ||
        status < 200 ||
        status >= 400 ||
        !state.text
      )
        continue;
      const snapshot = await withWebAbort(
        window.webContents.executeJavaScript(
          `(${captureRenderedDocument.toString()})()`,
        ),
        signal,
      );
      if (snapshot.error) throw new Error(snapshot.error);
      if (
        typeof snapshot.html !== "string" ||
        Buffer.byteLength(snapshot.html) > 10 * 1024 * 1024
      )
        throw new Error("验证后 HTML 超过 10 MiB");
      const url = window.webContents.getURL();
      if (new URL(url).origin !== entry.origin)
        throw new Error("验证页面超出来源范围");
      return { html: snapshot.html, url, status, links: [] };
    }
  } catch (error) {
    if (window?.isDestroyed() && !signal.aborted)
      throw new Error("网页验证已取消", { cause: error });
    throw error;
  } finally {
    signal.removeEventListener("abort", stop);
    stop();
    proxy.close();
    target.webRequest.onBeforeRequest(null);
    target.webRequest.onCompleted(null);
    target.removeListener("will-download", denyDownload);
    target.setPermissionRequestHandler(null);
    target.setPermissionCheckHandler(null);
    await clearVerificationSession(target);
  }
}
