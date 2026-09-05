import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  const webContentsHandlers = new Map<string, (...args: unknown[]) => void>();
  const windowHandlers = new Map<string, (...args: unknown[]) => void>();
  const debuggerMessage = {
    listener: null as
      | null
      | ((
          event: unknown,
          method: string,
          params: Record<string, unknown>,
        ) => void),
  };
  const webContents = {
    setWindowOpenHandler: vi.fn(),
    on: vi.fn((event: string, handler: (...args: unknown[]) => void) => {
      webContentsHandlers.set(event, handler);
    }),
    once: vi.fn((event: string, handler: (...args: unknown[]) => void) => {
      webContentsHandlers.set(event, handler);
    }),
    removeListener: vi.fn(),
    getUserAgent: vi.fn(() => "Mozilla/5.0 Electron/33.0 GuiZhi/0.18"),
    setUserAgent: vi.fn(),
    isDestroyed: vi.fn(() => false),
    getURL: vi.fn(() => "about:blank"),
    loadURL: vi.fn().mockResolvedValue(undefined),
    executeJavaScript: vi.fn().mockResolvedValue(undefined),
    debugger: {
      attach: vi.fn(),
      detach: vi.fn(),
      removeListener: vi.fn(),
      on: vi.fn((_event: string, listener: typeof debuggerMessage.listener) => {
        debuggerMessage.listener = listener;
      }),
      sendCommand: vi.fn().mockImplementation(async (command: string) =>
        command === "Network.getResponseBody"
          ? {
              body: JSON.stringify({ aweme_list: [{ aweme_id: "fixture" }] }),
              base64Encoded: false,
            }
          : undefined,
      ),
    },
  };
  const browserWindow = {
    webContents,
    removeMenu: vi.fn(),
    once: vi.fn((event: string, handler: (...args: unknown[]) => void) => {
      windowHandlers.set(event, handler);
    }),
    removeListener: vi.fn(),
    show: vi.fn(),
    center: vi.fn(),
    isDestroyed: vi.fn(() => false),
    destroy: vi.fn(),
    getSize: vi.fn(() => [1040, 760]),
    setSkipTaskbar: vi.fn(),
    setOpacity: vi.fn(),
    setPosition: vi.fn(),
    showInactive: vi.fn(),
  };
  const beforeRequest = {
    listener: null as
      | null
      | ((
          details: { url: string; resourceType: string },
          callback: (result: { cancel: boolean }) => void,
        ) => void),
  };
  const targetSession = {
    webRequest: {
      onBeforeRequest: vi.fn(
        (_filter: unknown, listener: typeof beforeRequest.listener) => {
          beforeRequest.listener = listener;
        },
      ),
    },
    cookies: { get: vi.fn().mockResolvedValue([]) },
    clearStorageData: vi.fn().mockResolvedValue(undefined),
    clearCache: vi.fn().mockResolvedValue(undefined),
    clearAuthCache: vi.fn().mockResolvedValue(undefined),
    closeAllConnections: vi.fn().mockResolvedValue(undefined),
    on: vi.fn(),
    removeListener: vi.fn(),
  };
  return {
    webContentsHandlers,
    windowHandlers,
    debuggerMessage,
    webContents,
    browserWindow,
    beforeRequest,
    targetSession,
    BrowserWindow: vi.fn(() => browserWindow),
    fromPartition: vi.fn(() => targetSession),
    applyProxy: vi.fn().mockResolvedValue(undefined),
  };
});

vi.mock("electron", () => ({
  BrowserWindow: mocks.BrowserWindow,
  session: { fromPartition: mocks.fromPartition },
  screen: { getAllDisplays: () => [{ bounds: { x: 0, y: 0, width: 1920, height: 1080 } }] },
}));

vi.mock("../../../src/main/services/network-proxy", () => ({
  applyElectronSessionProxy: mocks.applyProxy,
}));

import {
  clearElectronCaptureSessions,
  createElectronCaptureContext,
} from "../../../src/main/services/platform-capture/electron-capture-runtime";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.beforeRequest.listener = null;
  mocks.webContentsHandlers.clear();
  mocks.windowHandlers.clear();
  mocks.debuggerMessage.listener = null;
});

describe("Electron 平台采集运行器", () => {
  it("使用平台独立持久化会话和受限的归知子窗口", async () => {
    const parent = { id: 1 } as never;
    await createElectronCaptureContext({
      platform: "xiaohongshu",
      visible: true,
      parent,
      proxy: {
        mode: "system",
        protocol: "http",
        host: "",
        port: 7890,
        username: "",
        password: "",
        bypass: "<local>",
      },
      isAllowedResourceUrl: (url) => url.endsWith("xiaohongshu.com/app.js"),
      isAllowedNavigationUrl: (url) =>
        url === "https://www.xiaohongshu.com/explore",
      shouldBlockRequest: (_url, resourceType) => resourceType === "media",
    });

    expect(mocks.fromPartition).toHaveBeenCalledWith(
      "persist:guizhi-platform-capture-xiaohongshu",
      { cache: true },
    );
    expect(mocks.BrowserWindow).toHaveBeenCalledWith(
      expect.objectContaining({
        parent,
        modal: false,
        show: false,
        skipTaskbar: true,
        autoHideMenuBar: true,
        webPreferences: expect.objectContaining({
          nodeIntegration: false,
          contextIsolation: true,
          sandbox: true,
          webSecurity: true,
          session: mocks.targetSession,
        }),
      }),
    );
    expect(mocks.webContents.setWindowOpenHandler).toHaveBeenCalled();
    expect(mocks.browserWindow.center).toHaveBeenCalled();
    expect(mocks.webContents.setUserAgent).toHaveBeenCalledWith("Mozilla/5.0");
  });

  it("资源请求与顶层导航分别执行白名单，并阻止下载和新窗口", async () => {
    await createElectronCaptureContext({
      platform: "douyin",
      visible: true,
      proxy: {
        mode: "direct",
        protocol: "http",
        host: "",
        port: 7890,
        username: "",
        password: "",
        bypass: "<local>",
      },
      isAllowedResourceUrl: (url) => url.startsWith("https://www.douyin.com/"),
      isAllowedNavigationUrl: (url) => url === "https://www.douyin.com/",
      shouldBlockRequest: (_url, resourceType) => resourceType === "media",
    });

    const decide = (url: string, resourceType: string) =>
      new Promise<{ cancel: boolean }>((resolve) => {
        mocks.beforeRequest.listener?.({ url, resourceType }, resolve);
      });
    await expect(decide("https://evil.test/app.js", "script")).resolves.toEqual(
      { cancel: true },
    );
    await expect(
      decide("https://www.douyin.com/app.js", "script"),
    ).resolves.toEqual({ cancel: false });
    await expect(
      decide("https://www.douyin.com/video.mp4", "media"),
    ).resolves.toEqual({ cancel: true });

    const navigationEvent = { preventDefault: vi.fn() };
    mocks.webContentsHandlers.get("will-navigate")?.(
      navigationEvent,
      "https://cdn.example/app.js",
    );
    expect(navigationEvent.preventDefault).toHaveBeenCalled();
    expect(mocks.webContents.setWindowOpenHandler.mock.calls[0][0]()).toEqual({
      action: "deny",
    });
  });

  it("清除数据时同时清理全部平台 partition", async () => {
    await clearElectronCaptureSessions();
    expect(mocks.fromPartition).toHaveBeenCalledWith(
      "persist:guizhi-platform-capture-xiaohongshu",
      { cache: true },
    );
    expect(mocks.fromPartition).toHaveBeenCalledWith(
      "persist:guizhi-platform-capture-douyin",
      { cache: true },
    );
    expect(mocks.fromPartition).toHaveBeenCalledWith(
      "persist:guizhi-platform-capture-linuxdo",
      { cache: true },
    );
    expect(mocks.targetSession.clearStorageData).toHaveBeenCalledTimes(3);
    expect(mocks.targetSession.closeAllConnections).toHaveBeenCalledTimes(3);
  });

  it("通过内置 WebContents 调试通道读取页面自身的结构化响应", async () => {
    const context = await createElectronCaptureContext({
      platform: "douyin",
      visible: false,
      proxy: {
        mode: "system",
        protocol: "http",
        host: "",
        port: 7890,
        username: "",
        password: "",
        bypass: "<local>",
      },
      isAllowedResourceUrl: () => true,
      isAllowedNavigationUrl: () => true,
      shouldBlockRequest: () => false,
    });
    const payloads = context.page.startJsonCapture("douyin");
    expect(mocks.browserWindow.showInactive).toHaveBeenCalledOnce();
    expect(mocks.browserWindow.setPosition).toHaveBeenCalledWith(-1440, -1160);
    expect(mocks.browserWindow.setOpacity).toHaveBeenCalledWith(0);
    mocks.debuggerMessage.listener?.({}, "Network.responseReceived", {
      requestId: "request-1",
      response: {
        url: "https://www.douyin.com/aweme/v1/web/detail/",
        mimeType: "text/plain",
      },
      type: "Fetch",
    });
    mocks.debuggerMessage.listener?.({}, "Network.loadingFinished", {
      requestId: "request-1",
    });
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(payloads).toEqual([{ aweme_list: [{ aweme_id: "fixture" }] }]);
    expect(mocks.webContents.debugger.sendCommand).toHaveBeenCalledWith(
      "Network.getResponseBody",
      { requestId: "request-1" },
    );
  });

  it("大型 SPA 主框架完成导航后即可进入后续发现轮询", async () => {
    mocks.webContents.loadURL.mockImplementationOnce(() => new Promise(() => {}));
    const context = await createElectronCaptureContext({
      platform: "xiaohongshu",
      visible: false,
      proxy: {
        mode: "system",
        protocol: "http",
        host: "",
        port: 7890,
        username: "",
        password: "",
        bypass: "<local>",
      },
      isAllowedResourceUrl: () => true,
      isAllowedNavigationUrl: () => true,
      shouldBlockRequest: () => false,
    });
    const navigation = context.page.goto(
      "https://www.xiaohongshu.com/search_result?keyword=test",
    );

    // 上一页迟到的 DOM 事件不能将本次导航误报为成功。
    const settled = vi.fn();
    void navigation.then(settled);
    mocks.webContentsHandlers.get("dom-ready")?.();
    await Promise.resolve();
    expect(settled).not.toHaveBeenCalled();

    mocks.webContentsHandlers.get("did-navigate")?.(
      {},
      "https://www.xiaohongshu.com/search_result?keyword=test",
    );

    await expect(navigation).resolves.toBeUndefined();
    expect(mocks.webContents.removeListener).toHaveBeenCalledWith(
      "did-navigate",
      expect.any(Function),
    );
  });

  it("搜索采集只读取本次关键词的响应，忽略推荐流、旧词和重定向响应", async () => {
    const context = await createElectronCaptureContext({
      platform: "douyin", visible: false,
      proxy: { mode: "system", protocol: "http", host: "", port: 7890, username: "", password: "", bypass: "<local>" },
      isAllowedResourceUrl: () => true, isAllowedNavigationUrl: () => true, shouldBlockRequest: () => false,
    });
    const payloads = context.page.startJsonCapture("douyin", { keyword: "GPT-6" });
    const emit = (method: string, params: Record<string, unknown>) => mocks.debuggerMessage.listener?.({}, method, params);
    const base = "https://www.douyin.com/aweme/v1/web/";
    for (const [id, route] of [["feed", "tab/feed/"], ["old", "general/search/single/?keyword=GPT-5"], ["match", "general/search/single/?keyword=GPT-6"]]) {
      emit("Network.requestWillBeSent", { requestId: id, request: { url: base + route } });
      emit("Network.responseReceived", { requestId: id, response: { url: base + route, mimeType: "application/json" }, type: "Fetch" });
      emit("Network.loadingFinished", { requestId: id });
    }
    emit("Network.requestWillBeSent", { requestId: "redirect", request: { url: `${base}general/search/single/?keyword=GPT-6` } });
    emit("Network.requestWillBeSent", { requestId: "redirect", request: { url: `${base}tab/feed/` } });
    emit("Network.responseReceived", { requestId: "redirect", response: { url: `${base}tab/feed/`, mimeType: "application/json" }, type: "Fetch" });
    emit("Network.loadingFinished", { requestId: "redirect" });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(payloads).toHaveLength(1);
    expect(mocks.webContents.debugger.sendCommand.mock.calls.filter(([command]) => command === "Network.getResponseBody")).toEqual([["Network.getResponseBody", { requestId: "match" }]]);
  });

  it("导航立即失败时保留原因，超时后移除监听器", async () => {
    const context = await createElectronCaptureContext({
      platform: "xiaohongshu", visible: false,
      proxy: { mode: "direct", protocol: "http", host: "", port: 7890, username: "", password: "", bypass: "<local>" },
      isAllowedResourceUrl: () => true, isAllowedNavigationUrl: () => true, shouldBlockRequest: () => false,
    });
    mocks.webContents.loadURL.mockRejectedValueOnce(new Error("ERR_PROXY_CONNECTION_FAILED"));
    await expect(context.page.goto("https://www.xiaohongshu.com/search_result")).rejects.toThrow("ERR_PROXY_CONNECTION_FAILED");
    mocks.webContents.loadURL.mockImplementationOnce(() => new Promise(() => {}));
    mocks.webContents.removeListener.mockClear();
    await expect(context.page.goto("https://www.xiaohongshu.com/search_result", { timeout: 5 })).rejects.toThrow("页面导航超时");
    expect(mocks.webContents.removeListener).toHaveBeenCalledWith("did-navigate", expect.any(Function));
  });
  it("暂停采集会释放调试监听，旧响应不会污染恢复后的采集", async () => {
    const context = await createElectronCaptureContext({
      platform: "douyin", visible: false,
      proxy: { mode: "system", protocol: "http", host: "", port: 7890, username: "", password: "", bypass: "<local>" },
      isAllowedResourceUrl: () => true, isAllowedNavigationUrl: () => true, shouldBlockRequest: () => false,
    });
    context.page.startJsonCapture("douyin");
    let resolveBody!: (value: unknown) => void;
    mocks.webContents.debugger.sendCommand.mockImplementationOnce(() => new Promise((resolve) => { resolveBody = resolve; }));
    const oldListener = mocks.debuggerMessage.listener!;
    oldListener({}, "Network.responseReceived", { requestId: "old", type: "Fetch", response: { url: "https://www.douyin.com/aweme/v1/web/detail/", mimeType: "application/json" } });
    oldListener({}, "Network.loadingFinished", { requestId: "old" });
    context.page.stopJsonCapture();
    expect(mocks.webContents.debugger.detach).toHaveBeenCalledOnce();
    expect(mocks.webContents.debugger.removeListener).toHaveBeenCalledWith("message", oldListener);
    const current = context.page.startJsonCapture("douyin", { keyword: "GPT-6" });
    resolveBody({ body: JSON.stringify({ data: [{ aweme_id: "old" }] }) });
    await Promise.resolve();
    expect(current).toEqual([]);
    await context.close();
  });
});
