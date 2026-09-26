import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WebElectronRenderer } from "../../../src/main/services/web-capture/web-electron-renderer";
import { WebTaskGate } from "../../../src/main/services/web-capture/web-task-gate";

const mocks = vi.hoisted(() => ({
  network: vi.fn(),
  sessions: [] as any[],
  windows: [] as any[],
  flood: false,
  background: false,
}));
vi.mock("../../../src/main/services/web-capture/web-network", () => ({
  webNetworkRequest: mocks.network,
}));
vi.mock("../../../src/main/testing/window-mode", () => ({
  showWindowOffscreen: vi.fn(),
}));
vi.mock("electron", () => ({
  session: {
    fromPartition: vi.fn(() => {
      const handlers = new Map<
        string,
        (request: Request) => Promise<Response>
      >();
      const target = {
        handlers,
        before: null as any,
        setProxy: vi.fn(async () => undefined),
        setPermissionRequestHandler: vi.fn(),
        setPermissionCheckHandler: vi.fn(),
        on: vi.fn(),
        removeListener: vi.fn(),
        closeAllConnections: vi.fn(async () => undefined),
        clearStorageData: vi.fn(async () => undefined),
        clearCache: vi.fn(async () => undefined),
        clearAuthCache: vi.fn(async () => undefined),
        protocol: {
          handle: vi.fn((scheme, action) => handlers.set(scheme, action)),
          unhandle: vi.fn((scheme) => handlers.delete(scheme)),
        },
        webRequest: {
          onBeforeRequest: vi.fn((handler) => {
            target.before = handler;
          }),
        },
      };
      mocks.sessions.push(target);
      return target;
    }),
  },
  BrowserWindow: class {
    destroyed = false;
    url = "";
    workerPolicy = "";
    target: any;
    constructor(options: any) {
      this.target = options.webPreferences.session;
      mocks.windows.push(this);
    }
    webContents = {
      setWebRTCIPHandlingPolicy: vi.fn(),
      setWindowOpenHandler: vi.fn(),
      getURL: () => this.url,
      executeJavaScript: vi.fn(async (script: string) =>
        script.startsWith("Array.from")
          ? []
          : { html: "<html><body>完整正文</body></html>" },
      ),
    };
    async loadURL(url: string) {
      this.url = url;
      let canceled = false;
      this.target.before(
        { url, resourceType: "mainFrame" },
        (result: { cancel: boolean }) => {
          canceled = result.cancel;
        },
      );
      if (canceled) throw new Error("导航被拒绝");
      const response = await this.target.handlers.get(
        new URL(url).protocol.slice(0, -1),
      )(new Request(url));
      this.workerPolicy = response.headers.get("content-security-policy") ?? "";
      if (mocks.background) void this.target.handlers.get("https")(new Request(url + "late")).catch(() => undefined);
      if (mocks.flood) await Promise.allSettled(Array.from({ length: 210 }, (_, index) =>
        this.target.handlers.get("https")(new Request(url + "?asset=" + index))));
      if (response.status === 302)
        await this.loadURL(
          new URL(response.headers.get("location")!, url).href,
        );
    }
    isDestroyed() {
      return this.destroyed;
    }
    destroy() {
      this.destroyed = true;
    }
  },
}));
const request = {
  taskId: "one",
  purpose: "import" as const,
  url: "https://example.com/",
};
let renderer: WebElectronRenderer;
beforeEach(() => {
  vi.clearAllMocks();
  mocks.sessions.length = 0;
  mocks.windows.length = 0;
  mocks.flood = false;
  mocks.background = false;
  mocks.network.mockResolvedValue({
    status: 200,
    headers: { "content-type": "text/html" },
    body: Buffer.from("<p>正文</p>").toString("base64"),
  });
  renderer = new WebElectronRenderer(new WebTaskGate(8));
});
afterEach(() => vi.useRealTimers());
async function render() {
  vi.useFakeTimers();
  const work = renderer.render(request, new AbortController().signal);
  await vi.runAllTimersAsync();
  return work;
}
describe("Electron 页面会话与网络隔离", () => {
  it("快照已取得后，收尾请求报告安全拒绝仍返回失败", async () => {
    vi.useFakeTimers();
    mocks.background = true;
    mocks.network.mockImplementation((request, signal: AbortSignal) => request.url.endsWith("late")
      ? new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(new Error("拒绝内网目标")), { once: true }))
      : Promise.resolve({ status: 200, headers: { "content-type": "text/html" }, body: "" }));
    const rejected = expect(renderer.render(request, new AbortController().signal)).rejects.toThrow("内网");
    await vi.runAllTimersAsync();
    await rejected;
  });
  it("超过请求预算时在等待队列继续增长前取消页面", async () => {
    mocks.flood = true;
    await expect(renderer.render(request, new AbortController().signal)).rejects.toThrow("预算");
    expect(mocks.network.mock.calls.length).toBeLessThanOrEqual(9);
    expect(mocks.windows[0].destroyed).toBe(true);
  });
  it("逐页销毁窗口并清理数据；会话可复用", async () => {
    await render();
    await render();
    expect(mocks.sessions).toHaveLength(1);
    expect(mocks.windows.every((window) => window.destroyed)).toBe(true);
    expect(mocks.sessions[0].clearStorageData).toHaveBeenCalledTimes(2);
    expect(mocks.sessions[0].clearAuthCache).toHaveBeenCalledTimes(2);
    expect(mocks.sessions[0].handlers.size).toBe(0);
    const session = mocks.sessions[0];
    const requestHandlers = session.setPermissionRequestHandler.mock.calls;
    const checkHandlers = session.setPermissionCheckHandler.mock.calls;
    // 复用固定权限策略，不能让持久 Session 保留每页新建的闭包上下文。
    expect(requestHandlers[0][0]).toBe(requestHandlers[1][0]);
    expect(checkHandlers[0][0]).toBe(checkHandlers[1][0]);
    const reply = vi.fn();
    requestHandlers[1][0](null, "notifications", reply, {});
    expect(reply).toHaveBeenCalledWith(false);
    expect(checkHandlers[1][0](null, "notifications", "https://example.com", {})).toBe(false);
    expect(mocks.windows[0].workerPolicy).toContain("worker-src 'none'");
    expect(
      mocks.windows[0].webContents.setWebRTCIPHandlingPolicy,
    ).toHaveBeenCalledWith("disable_non_proxied_udp");
  });
  it("两页并发也不共用页面会话", async () => {
    vi.useFakeTimers();
    const first = renderer.render(request, new AbortController().signal);
    const second = renderer.render(
      { ...request, taskId: "two" },
      new AbortController().signal,
    );
    await vi.runAllTimersAsync();
    await Promise.all([first, second]);
    expect(mocks.sessions).toHaveLength(2);
  });
  it("重定向到范围外时不发送第二跳请求", async () => {
    mocks.network.mockResolvedValue({
      status: 302,
      headers: { location: "https://outside.example/" },
      body: "",
    });
    await expect(
      renderer.render(
        {
          ...request,
          scope: { origin: "https://example.com", directory: "/" },
        },
        new AbortController().signal,
      ),
    ).rejects.toThrow("范围");
    expect(mocks.network).toHaveBeenCalledTimes(1);
    expect(mocks.windows[0].destroyed).toBe(true);
  });
  it("网络安全拒绝保留原因并清理会话", async () => {
    mocks.network.mockRejectedValue(new Error("拒绝内网目标"));
    await expect(
      renderer.render(request, new AbortController().signal),
    ).rejects.toThrow("内网");
    expect(mocks.sessions[0].clearStorageData).toHaveBeenCalled();
  });
  it("取消导航立即销毁窗口并等待请求退出", async () => {
    mocks.network.mockImplementation(
      (_request, signal: AbortSignal) =>
        new Promise((_resolve, reject) => {
          signal.addEventListener(
            "abort",
            () => reject(new Error("请求已取消")),
            { once: true },
          );
        }),
    );
    const controller = new AbortController();
    const work = renderer.render(request, controller.signal);
    const rejected = expect(work).rejects.toThrow("取消");
    await vi.waitFor(() => expect(mocks.network).toHaveBeenCalled());
    controller.abort();
    await rejected;
    expect(mocks.windows[0].destroyed).toBe(true);
    expect(mocks.sessions[0].clearStorageData).toHaveBeenCalled();
  });
});
