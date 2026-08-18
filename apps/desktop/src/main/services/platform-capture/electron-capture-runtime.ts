import {
  BrowserWindow,
  session,
  type BrowserWindow as ElectronBrowserWindow,
  type Session,
} from "electron";
import type {
  NetworkProxySettings,
  PlatformCapturePlatform,
} from "@guizhi/shared/types";
import { isAllowedPlatformUrl } from "@guizhi/shared/utils/platform-capture";
import { applyElectronSessionProxy } from "../network-proxy";

const JSON_RESPONSE_LIMIT = 64;
const JSON_RESPONSE_BYTES_LIMIT = 32 * 1024 * 1024;
const SINGLE_JSON_RESPONSE_BYTES_LIMIT = 10 * 1024 * 1024;

export interface ElectronCaptureCookie {
  name: string;
  value: string;
  domain: string;
  path: string;
}

interface LocatorWaitOptions {
  state?: "visible";
  timeout?: number;
}

interface LocatorClickOptions {
  timeout?: number;
}

interface LocatorQuery {
  kind: "css" | "text" | "role";
  value: string;
  exact?: boolean;
  name?: string;
}

export interface ElectronCaptureLocator {
  first(): ElectronCaptureLocator;
  isVisible(): Promise<boolean>;
  waitFor(options?: LocatorWaitOptions): Promise<void>;
  click(options?: LocatorClickOptions): Promise<void>;
}

export interface ElectronCapturePage {
  setDefaultNavigationTimeout(timeout: number): void;
  setDefaultTimeout(timeout: number): void;
  goto(
    url: string,
    options?: { timeout?: number; waitUntil?: string },
  ): Promise<void>;
  reload(options?: { timeout?: number; waitUntil?: string }): Promise<void>;
  url(): string;
  content(): Promise<string>;
  isClosed(): boolean;
  waitForTimeout(timeout: number): Promise<void>;
  evaluate<T, A = undefined>(
    fn: (arg: A) => T | Promise<T>,
    arg?: A,
  ): Promise<T>;
  locator(selector: string): ElectronCaptureLocator;
  getByRole(
    role: string,
    options?: { name?: string; exact?: boolean },
  ): ElectronCaptureLocator;
  getByText(
    text: string,
    options?: { exact?: boolean },
  ): ElectronCaptureLocator;
  scrollBy(y: number): Promise<void>;
  startJsonCapture(platform: PlatformCapturePlatform): unknown[];
  close(): Promise<void>;
}

export interface ElectronCaptureContext {
  readonly page: ElectronCapturePage;
  readonly browserVersion: string;
  cookies(url?: string): Promise<ElectronCaptureCookie[]>;
  clearStorageData(): Promise<void>;
  close(): Promise<void>;
}

export interface CreateElectronCaptureContextInput {
  platform: PlatformCapturePlatform;
  visible: boolean;
  parent?: ElectronBrowserWindow | null;
  proxy: NetworkProxySettings;
  isAllowedResourceUrl: (url: string) => boolean;
  isAllowedNavigationUrl: (url: string) => boolean;
  shouldBlockRequest: (url: string, resourceType: string) => boolean;
}

function delay(timeout: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, timeout));
}

function withTimeout<T>(
  promise: Promise<T>,
  timeout: number,
  message: string,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(message)), timeout);
    promise.then(
      (value) => {
        clearTimeout(timer);
        resolve(value);
      },
      (error) => {
        clearTimeout(timer);
        reject(error);
      },
    );
  });
}

function locatorScript(
  query: LocatorQuery,
  action: "visible" | "click",
): string {
  return `(() => {
    const query = ${JSON.stringify(query)};
    const normalize = (value) => String(value || "").replace(/\\s+/g, " ").trim();
    const visible = (node) => {
      if (!(node instanceof HTMLElement)) return false;
      const style = window.getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
    };
    let nodes = [];
    if (query.kind === "css") {
      let selector = query.value;
      let expected = "";
      let exact = false;
      const textIs = selector.match(/:text-is\\((['"])(.*?)\\1\\)/);
      const hasText = selector.match(/:has-text\\((['"])(.*?)\\1\\)/);
      const match = textIs || hasText;
      if (match) {
        expected = normalize(match[2]);
        exact = Boolean(textIs);
        selector = selector.replace(match[0], "");
      }
      try { nodes = Array.from(document.querySelectorAll(selector)); } catch { nodes = []; }
      if (expected) nodes = nodes.filter((node) => exact
        ? normalize(node.textContent) === expected
        : normalize(node.textContent).includes(expected));
    } else if (query.kind === "role") {
      const selector = query.value === "button" ? "button, [role='button']" : "[role='" + query.value + "']";
      nodes = Array.from(document.querySelectorAll(selector));
      if (query.name) nodes = nodes.filter((node) => {
        const label = normalize(node.getAttribute("aria-label") || node.textContent);
        return query.exact ? label === normalize(query.name) : label.includes(normalize(query.name));
      });
    } else {
      nodes = Array.from(document.querySelectorAll("button, [role='button'], a, p, span, div"));
      nodes = nodes.filter((node) => query.exact
        ? normalize(node.textContent) === normalize(query.value)
        : normalize(node.textContent).includes(normalize(query.value)));
    }
    const node = nodes.find(visible);
    if (!node) return false;
    if (${JSON.stringify(action)} === "click") node.click();
    return true;
  })()`;
}

class ElectronLocator implements ElectronCaptureLocator {
  constructor(
    private readonly page: ElectronPage,
    private readonly query: LocatorQuery,
  ) {}

  first(): ElectronCaptureLocator {
    return this;
  }

  async isVisible(): Promise<boolean> {
    if (this.page.isClosed()) return false;
    return Boolean(
      await this.page.execute<boolean>(locatorScript(this.query, "visible")),
    );
  }

  async waitFor(options: LocatorWaitOptions = {}): Promise<void> {
    const timeout = options.timeout ?? this.page.defaultTimeout;
    const started = Date.now();
    while (Date.now() - started < timeout) {
      if (await this.isVisible()) return;
      await delay(100);
    }
    throw new Error("等待平台登录组件超时");
  }

  async click(options: LocatorClickOptions = {}): Promise<void> {
    await this.waitFor({ state: "visible", timeout: options.timeout });
    const clicked = await this.page.execute<boolean>(
      locatorScript(this.query, "click"),
    );
    if (!clicked) throw new Error("平台登录入口已消失");
  }
}

class ElectronPage implements ElectronCapturePage {
  defaultTimeout = 45_000;
  private navigationTimeout = 45_000;
  private jsonPayloads: unknown[] | null = null;
  private capturedBytes = 0;
  private debuggerAttached = false;
  private pendingResponses = new Map<
    string,
    { url: string; mimeType: string }
  >();

  constructor(private readonly window: ElectronBrowserWindow) {}

  setDefaultNavigationTimeout(timeout: number): void {
    this.navigationTimeout = timeout;
  }

  setDefaultTimeout(timeout: number): void {
    this.defaultTimeout = timeout;
  }

  async goto(url: string, options: { timeout?: number } = {}): Promise<void> {
    if (this.isClosed()) throw new Error("登录窗口已关闭");
    const contents = this.window.webContents;
    const ready = new Promise<void>((resolve, reject) => {
      const onReady = () => {
        cleanup();
        resolve();
      };
      const onFailure = (
        _event: unknown,
        code: number,
        description: string,
        _url: string,
        isMainFrame: boolean,
      ) => {
        if (!isMainFrame || code === -3) return;
        cleanup();
        reject(new Error(`页面加载失败：${description}`));
      };
      const onClosed = () => {
        cleanup();
        reject(new Error("Platform login window closed"));
      };
      const cleanup = () => {
        contents.removeListener("dom-ready", onReady);
        contents.removeListener("did-fail-load", onFailure);
        this.window.removeListener("closed", onClosed);
      };
      contents.once("dom-ready", onReady);
      contents.on("did-fail-load", onFailure);
      this.window.once("closed", onClosed);
    });
    void contents.loadURL(url).catch(() => undefined);
    await withTimeout(
      ready,
      options.timeout ?? this.navigationTimeout,
      "页面导航超时",
    );
  }

  async reload(options: { timeout?: number } = {}): Promise<void> {
    await this.goto(this.url(), options);
  }

  url(): string {
    return this.isClosed() ? "" : this.window.webContents.getURL();
  }

  async content(): Promise<string> {
    return this.evaluate(() => document.documentElement.outerHTML);
  }

  isClosed(): boolean {
    return this.window.isDestroyed() || this.window.webContents.isDestroyed();
  }

  waitForTimeout(timeout: number): Promise<void> {
    return delay(timeout);
  }

  async evaluate<T, A = undefined>(
    fn: (arg: A) => T | Promise<T>,
    arg?: A,
  ): Promise<T> {
    const source = `(${fn.toString()})(${JSON.stringify(arg)})`;
    return this.execute<T>(source);
  }

  execute<T>(source: string): Promise<T> {
    if (this.isClosed()) return Promise.reject(new Error("登录窗口已关闭"));
    return this.window.webContents.executeJavaScript(
      source,
      true,
    ) as Promise<T>;
  }

  locator(selector: string): ElectronCaptureLocator {
    return new ElectronLocator(this, { kind: "css", value: selector });
  }

  getByRole(
    role: string,
    options: { name?: string; exact?: boolean } = {},
  ): ElectronCaptureLocator {
    return new ElectronLocator(this, {
      kind: "role",
      value: role,
      name: options.name,
      exact: options.exact,
    });
  }

  getByText(
    text: string,
    options: { exact?: boolean } = {},
  ): ElectronCaptureLocator {
    return new ElectronLocator(this, {
      kind: "text",
      value: text,
      exact: options.exact,
    });
  }

  scrollBy(y: number): Promise<void> {
    return this.evaluate(
      (amount) => window.scrollBy({ top: amount, behavior: "instant" }),
      y,
    );
  }

  startJsonCapture(platform: PlatformCapturePlatform): unknown[] {
    if (this.jsonPayloads) return this.jsonPayloads;
    this.jsonPayloads = [];
    const contents = this.window.webContents;
    try {
      contents.debugger.attach("1.3");
      this.debuggerAttached = true;
      void contents.debugger.sendCommand("Network.enable", {
        maxTotalBufferSize: JSON_RESPONSE_BYTES_LIMIT,
        maxResourceBufferSize: SINGLE_JSON_RESPONSE_BYTES_LIMIT,
      });
      contents.debugger.on("message", (_event, method, params) => {
        if (method === "Network.responseReceived") {
          const response = params.response as
            { url?: string; mimeType?: string } | undefined;
          const url = response?.url ?? "";
          const mimeType = response?.mimeType ?? "";
          if (
            url &&
            isAllowedPlatformUrl(platform, url) &&
            /json|javascript/i.test(mimeType)
          ) {
            this.pendingResponses.set(String(params.requestId), {
              url,
              mimeType,
            });
          }
          return;
        }
        if (method !== "Network.loadingFinished") return;
        const requestId = String(params.requestId);
        const pending = this.pendingResponses.get(requestId);
        this.pendingResponses.delete(requestId);
        if (
          !pending ||
          !this.jsonPayloads ||
          this.jsonPayloads.length >= JSON_RESPONSE_LIMIT
        )
          return;
        void contents.debugger
          .sendCommand("Network.getResponseBody", { requestId })
          .then((result) => {
            if (
              !this.jsonPayloads ||
              this.jsonPayloads.length >= JSON_RESPONSE_LIMIT
            )
              return;
            const record = result as { body?: string; base64Encoded?: boolean };
            const buffer = record.base64Encoded
              ? Buffer.from(record.body ?? "", "base64")
              : Buffer.from(record.body ?? "", "utf8");
            if (
              buffer.byteLength > SINGLE_JSON_RESPONSE_BYTES_LIMIT ||
              this.capturedBytes + buffer.byteLength > JSON_RESPONSE_BYTES_LIMIT
            )
              return;
            try {
              this.jsonPayloads.push(JSON.parse(buffer.toString("utf8")));
              this.capturedBytes += buffer.byteLength;
            } catch {
              // 非 JSON 的脚本响应不进入采集结果。
            }
          })
          .catch(() => undefined);
      });
    } catch {
      this.debuggerAttached = false;
    }
    return this.jsonPayloads;
  }

  async close(): Promise<void> {
    if (this.debuggerAttached && !this.window.webContents.isDestroyed()) {
      try {
        this.window.webContents.debugger.detach();
      } catch {
        /* 已关闭 */
      }
    }
    if (!this.window.isDestroyed()) this.window.destroy();
  }
}

class ElectronContext implements ElectronCaptureContext {
  readonly page: ElectronCapturePage;
  readonly browserVersion =
    process.versions.chrome ?? process.versions.electron;

  constructor(
    private readonly window: ElectronBrowserWindow,
    private readonly targetSession: Session,
  ) {
    this.page = new ElectronPage(window);
  }

  async cookies(url?: string): Promise<ElectronCaptureCookie[]> {
    const cookies = await this.targetSession.cookies.get(url ? { url } : {});
    return cookies.map((cookie) => ({
      name: cookie.name,
      value: cookie.value,
      domain: cookie.domain ?? "",
      path: cookie.path ?? "/",
    }));
  }

  async clearStorageData(): Promise<void> {
    await this.targetSession.clearStorageData();
    await this.targetSession.clearCache().catch(() => undefined);
    await this.targetSession.clearAuthCache().catch(() => undefined);
  }

  async close(): Promise<void> {
    await this.page.close();
  }
}

function partitionFor(platform: PlatformCapturePlatform): string {
  return `persist:guizhi-platform-capture-${platform}`;
}

export function getElectronCaptureSession(
  platform: PlatformCapturePlatform,
): Session {
  return session.fromPartition(partitionFor(platform), { cache: true });
}

export async function clearElectronCaptureSessions(): Promise<void> {
  for (const platform of ["xiaohongshu", "douyin", "linuxdo"] as const) {
    const targetSession = getElectronCaptureSession(platform);
    await targetSession.clearStorageData();
    await targetSession.clearCache().catch(() => undefined);
    await targetSession.clearAuthCache().catch(() => undefined);
    await targetSession.closeAllConnections().catch(() => undefined);
  }
}

export async function createElectronCaptureContext(
  input: CreateElectronCaptureContextInput,
): Promise<ElectronCaptureContext> {
  const targetSession = getElectronCaptureSession(input.platform);
  await applyElectronSessionProxy(input.proxy, targetSession);

  targetSession.webRequest.onBeforeRequest(
    { urls: ["*://*/*"] },
    (details, callback) => {
      const blocked =
        !input.isAllowedResourceUrl(details.url) ||
        (input.visible &&
          input.shouldBlockRequest(details.url, details.resourceType));
      callback({ cancel: blocked });
    },
  );

  const window = new BrowserWindow({
    width: 1040,
    height: 760,
    minWidth: 760,
    minHeight: 560,
    parent: input.parent ?? undefined,
    // 保持主窗口可操作，用户仍可点击归知里的“取消登录”。窗口仍归属于主窗口，
    // 不会出现在外部浏览器的标签页或任务流中。
    modal: false,
    show: false,
    skipTaskbar: input.visible,
    autoHideMenuBar: true,
    title:
      input.platform === "xiaohongshu"
        ? "小红书官方登录"
        : input.platform === "douyin"
          ? "抖音官方登录"
          : "LINUX DO 验证",
    backgroundColor: "#ffffff",
    webPreferences: {
      session: targetSession,
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      allowRunningInsecureContent: false,
      backgroundThrottling: false,
    },
  });
  window.removeMenu();
  if (input.visible) window.center();
  window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  window.webContents.on("will-navigate", (event, url) => {
    if (!input.isAllowedNavigationUrl(url)) event.preventDefault();
  });
  window.webContents.on("will-redirect", (event, url) => {
    if (!input.isAllowedNavigationUrl(url)) event.preventDefault();
  });
  window.webContents.on("will-frame-navigate", (event) => {
    if (
      !input.isAllowedNavigationUrl(event.url) &&
      !input.isAllowedResourceUrl(event.url)
    ) {
      event.preventDefault();
    }
  });
  window.webContents.on("console-message", (_event, level, message, line, sourceId) => {
    if (level >= 2) {
      console.warn(`[platform-window:${input.platform}] ${message} (${sourceId}:${line})`);
    }
  });
  window.webContents.on("did-fail-load", (_event, errorCode, errorDescription, validatedURL) => {
    console.warn(
      `[platform-window:${input.platform}] 加载失败: ${validatedURL} (${errorDescription}, code=${errorCode})`,
    );
  });
  const userAgent = window.webContents
    .getUserAgent()
    .replace(/\sElectron\/\S+/i, "")
    .replace(/\sGuiZhi\/\S+/i, "");
  window.webContents.setUserAgent(userAgent);
  const preventDownload = (event: Electron.Event) => event.preventDefault();
  targetSession.on("will-download", preventDownload);
  window.once("closed", () => {
    targetSession.removeListener("will-download", preventDownload);
  });
  if (input.visible) window.once("ready-to-show", () => window.show());

  return new ElectronContext(window, targetSession);
}
