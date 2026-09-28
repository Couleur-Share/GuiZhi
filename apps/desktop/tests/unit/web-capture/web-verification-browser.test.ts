// @vitest-environment node
import { EventEmitter } from "node:events";
import { beforeEach, describe, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({
  createProxy: vi.fn(),
  closeProxy: vi.fn(),
  clear: vi.fn(),
  pause: vi.fn(),
  target: undefined as any,
  window: undefined as any,
  canceled: false,
  verified: false,
}));
vi.mock(
  "../../../src/main/services/web-capture/web-verification-proxy",
  () => ({ createWebVerificationProxy: mocks.createProxy }),
);
vi.mock(
  "../../../src/main/services/web-capture/web-task-gate",
  async (importOriginal) => ({
    ...(await importOriginal<object>()),
    webPause: mocks.pause,
  }),
);
vi.mock("electron", () => {
  mocks.target = {
    setProxy: vi.fn().mockResolvedValue(undefined),
    setPermissionRequestHandler: vi.fn(),
    setPermissionCheckHandler: vi.fn(),
    on: vi.fn(),
    removeListener: vi.fn(),
    webRequest: { onBeforeRequest: vi.fn(), onCompleted: vi.fn() },
    closeAllConnections: mocks.clear,
    clearStorageData: mocks.clear,
    clearCache: mocks.clear,
    clearAuthCache: mocks.clear,
  };
  return {
    session: { fromPartition: vi.fn(() => mocks.target) },
    BrowserWindow: class extends EventEmitter {
      destroyed = false;
      webContents = Object.assign(new EventEmitter(), {
        getUserAgent: () => "Chrome/130 GuiZhi/0.25 Electron/33",
        setUserAgent: vi.fn(),
        setWebRTCIPHandlingPolicy: vi.fn(),
        setWindowOpenHandler: vi.fn(),
        isLoading: () => false,
        getURL: () => "https://example.com/article",
        executeJavaScript: vi.fn(async (script) =>
          script.includes("document.title")
            ? {
                title: mocks.verified ? "文章" : "Just a moment...",
                challenge: !mocks.verified,
                text: "正文",
              }
            : { html: "<article>已验证的正文</article>" },
        ),
      });
      constructor() {
        super();
        mocks.window = this;
      }
      isDestroyed() {
        return this.destroyed;
      }
      destroy() {
        this.destroyed = true;
      }
      show() {}
      async loadURL(url: string) {
        if (mocks.canceled) {
          this.destroy();
          throw new Error("窗口关闭");
        }
        this.webContents.emit("did-navigate", {}, url, 403);
      }
    },
  };
});
import { captureVerifiedWebPage } from "../../../src/main/services/web-capture/web-verification-browser";
beforeEach(() => {
  vi.clearAllMocks();
  mocks.canceled = false;
  mocks.verified = false;
  mocks.createProxy.mockResolvedValue({ port: 12345, close: mocks.closeProxy });
  mocks.clear.mockResolvedValue(undefined);
  mocks.pause.mockImplementation(async () => {
    if (mocks.pause.mock.calls.length === 2) {
      mocks.verified = true;
      mocks.window.webContents.emit(
        "did-navigate",
        {},
        "https://example.com/article",
        200,
      );
    }
  });
});
const request = {
  url: "https://example.com/article",
  taskId: "task",
  purpose: "import" as const,
};
describe("网页验证窗口", () => {
  it("403 阶段不提取；主文档导航变为 200 后自动提取并清理会话", async () => {
    const result = await captureVerifiedWebPage(
      request,
      new AbortController().signal,
    );
    expect(result.status).toBe(200);
    expect(result.html).toContain("已验证的正文");
    expect(mocks.pause).toHaveBeenCalledTimes(2);
    expect(mocks.closeProxy).toHaveBeenCalledOnce();
    expect(mocks.clear).toHaveBeenCalledTimes(4);
    expect(mocks.window.webContents.setUserAgent).toHaveBeenCalledWith(
      "Chrome/130",
    );
  });
  it("用户关闭窗口报告取消，仍清理 Cookie 和连接", async () => {
    mocks.canceled = true;
    await expect(
      captureVerifiedWebPage(request, new AbortController().signal),
    ).rejects.toThrow("取消");
    expect(mocks.closeProxy).toHaveBeenCalledOnce();
    expect(mocks.clear).toHaveBeenCalledTimes(4);
  });
  it("不安全的入口不创建验证代理", async () => {
    await expect(
      captureVerifiedWebPage(
        { ...request, url: "http://example.com/" },
        new AbortController().signal,
      ),
    ).rejects.toThrow("HTTPS");
    expect(mocks.createProxy).not.toHaveBeenCalled();
  });
});
