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
    mocks.debuggerMessage.listener?.({}, "Network.responseReceived", {
      requestId: "request-1",
      response: {
        url: "https://www.douyin.com/aweme/v1/web/detail/",
        mimeType: "application/json",
      },
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
});
