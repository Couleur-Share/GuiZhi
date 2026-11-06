import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WebElectronRenderer } from "../../../src/main/services/web-capture/web-electron-renderer";
import { WebTaskGate } from "../../../src/main/services/web-capture/web-task-gate";
import { WebConnectionPool } from "../../../src/main/services/web-capture/web-connection-pool";
import type { WebTrace } from "../../../src/main/services/web-capture/web-trace";

const mocks = vi.hoisted(() => ({
  network: vi.fn(),
  sessions: [] as any[],
  windows: [] as any[],
  flood: false,
  // 洪泛请求的 Accept；默认不带，用来区分“图片洪泛”（占位图本地应答）与普通洪泛（占请求预算）。
  floodAccept: undefined as string | undefined,
  background: false,
  // 后台请求拿到的应答，供断言占位图应答的内容。
  backgroundResponses: [] as Response[],
  // 页面载入后发起的导航，按顺序由窗口替身触发；默认没有。
  pageNavigations: [] as Array<{ url: string; isMainFrame?: boolean }>,
  // 上述导航的处置记录：是否被监听取消。
  navigationLog: [] as Array<{ url: string; isMainFrame: boolean; prevented: boolean }>,
  // 后台请求的 Accept；沉降等待按它区分样式/图片/文档类与其余请求。
  backgroundAccept: undefined as string | undefined,
  snapshot: {} as Record<string, unknown>,
  // 沉降探针的页面侧返回值：默认是已静止、正文充足的页面。
  probe: {
    install: true as unknown,
    sample: { domQuietMs: 1000, textLength: 5000 } as unknown,
    sampleFails: false,
  },
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
        webRequest: { onBeforeRequest: vi.fn() },
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
    // webContents.on 注册的导航事件监听（同一事件只保留最后一个，足够本测试使用）。
    listeners = new Map<string, (event: any) => void>();
    constructor(options: any) {
      this.target = options.webPreferences.session;
      mocks.windows.push(this);
    }
    webContents = {
      on: vi.fn((event: string, listener: (event: any) => void) => {
        this.listeners.set(event, listener);
      }),
      once: vi.fn((event: string, listener: (event: any) => void) => {
        this.listeners.set(event, (payload: any) => {
          this.listeners.delete(event);
          listener(payload);
        });
      }),
      setWebRTCIPHandlingPolicy: vi.fn(),
      setWindowOpenHandler: vi.fn(),
      getURL: () => this.url,
      executeJavaScript: vi.fn(async (script: string) => {
        if (script.startsWith("Array.from")) return [];
        if (script.includes("guizhi.settle")) {
          if (script.includes("MutationObserver")) return mocks.probe.install;
          if (mocks.probe.sampleFails) throw new Error("Object has been destroyed");
          return mocks.probe.sample;
        }
        if (script.includes("scrollBy")) return undefined;
        return mocks.snapshot;
      }),
    };
    /** 触发导航事件，返回监听是否调用了 preventDefault（与真实 Electron 一致：被取消则导航中止）。 */
    emitNavigation(event: string, url: string, isMainFrame = true): boolean {
      let prevented = false;
      this.listeners.get(event)?.({
        url,
        isMainFrame,
        isSameDocument: false,
        preventDefault: () => {
          prevented = true;
        },
      });
      return prevented;
    }
    /** 页面脚本、点击或 meta 刷新发起的导航：先过 will-frame-navigate，放行才取页面。 */
    async navigateFromPage(navigation: { url: string; isMainFrame?: boolean }) {
      const isMainFrame = navigation.isMainFrame ?? true;
      const prevented = this.emitNavigation("will-frame-navigate", navigation.url, isMainFrame);
      mocks.navigationLog.push({ url: navigation.url, isMainFrame, prevented });
      if (prevented) return;
      if (!isMainFrame || !/^https?:/.test(navigation.url)) return;
      this.url = navigation.url;
      // 取页失败时 Chromium 展示错误页，窗口继续存在；失败由 protocol.handle 的 serve 记账。
      await this.target.handlers
        .get(new URL(navigation.url).protocol.slice(0, -1))(
          new Request(navigation.url, { headers: { accept: "text/html,application/xhtml+xml,*/*;q=0.8" } }),
        )
        .catch(() => undefined);
    }
    async loadURL(url: string) {
      this.url = url;
      const response = await this.target.handlers.get(
        new URL(url).protocol.slice(0, -1),
      )(new Request(url, { headers: { accept: "text/html,application/xhtml+xml,*/*;q=0.8" } }));
      this.workerPolicy = response.headers.get("content-security-policy") ?? "";
      // 主文档解析完成：Chromium 此时触发 dom-ready（主框架 DOMContentLoaded）。
      this.listeners.get("dom-ready")?.({});
      if (mocks.background) {
        const headers = mocks.backgroundAccept ? { accept: mocks.backgroundAccept } : undefined;
        void this.target.handlers.get("https")(new Request(url + "late", { headers }))
          .then((late: Response) => mocks.backgroundResponses.push(late), () => undefined);
      }
      if (mocks.flood) await Promise.allSettled(Array.from({ length: 210 }, (_, index) =>
        this.target.handlers.get("https")(new Request(url + "?asset=" + index,
          mocks.floodAccept ? { headers: { accept: mocks.floodAccept } } : undefined))));
      if (response.status === 302) {
        const next = new URL(response.headers.get("location")!, url).href;
        // 服务端重定向：Chromium 在取下一跳之前触发 will-redirect，被取消则整个导航中止。
        if (this.emitNavigation("will-redirect", next)) throw new Error("ERR_ABORTED (-3)");
        await this.loadURL(next);
      }
      // 页面载入后由脚本/点击/meta 刷新发起的主框架导航。
      for (const navigation of mocks.pageNavigations.splice(0))
        await this.navigateFromPage(navigation);
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
  mocks.floodAccept = undefined;
  mocks.background = false;
  mocks.backgroundResponses.length = 0;
  mocks.pageNavigations.length = 0;
  mocks.navigationLog.length = 0;
  mocks.backgroundAccept = undefined;
  mocks.snapshot = { html: "<html><body>完整正文</body></html>" };
  mocks.probe.install = true;
  mocks.probe.sample = { domQuietMs: 1000, textLength: 5000 };
  mocks.probe.sampleFails = false;
  mocks.network.mockResolvedValue({
    status: 200,
    headers: { "content-type": "text/html" },
    body: Buffer.from("<p>正文</p>").toString("base64"),
  });
  renderer = new WebElectronRenderer(new WebTaskGate(8));
});
afterEach(() => vi.useRealTimers());
async function render(input: Parameters<WebElectronRenderer["render"]>[0] = request) {
  vi.useFakeTimers();
  const work = renderer.render(input, new AbortController().signal);
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
  describe("惰性图片以占位图本地应答", () => {
    const IMAGE_ACCEPT = "image/jxl,image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8";
    afterEach(() => vi.unstubAllEnvs());

    it("图片请求不出网，也不占请求预算：210 个图片请求不会触发预算失败", async () => {
      mocks.flood = true;
      mocks.floodAccept = IMAGE_ACCEPT;
      await expect(render()).resolves.toMatchObject({ status: 200 });
      // 只有主文档出网。
      expect(mocks.network).toHaveBeenCalledTimes(1);
    });

    it("应答是一张有效的 GIF，且不缓存", async () => {
      mocks.background = true;
      mocks.backgroundAccept = IMAGE_ACCEPT;
      await render();
      expect(mocks.network).toHaveBeenCalledTimes(1);
      const [answer] = mocks.backgroundResponses;
      expect(answer.status).toBe(200);
      expect(answer.headers.get("content-type")).toBe("image/gif");
      expect(answer.headers.get("cache-control")).toBe("no-store");
      expect(Buffer.from(await answer.arrayBuffer()).subarray(0, 6).toString("ascii")).toBe("GIF89a");
    });

    it("样式表与其他请求照常出网", async () => {
      mocks.background = true;
      mocks.backgroundAccept = "text/css,*/*;q=0.1";
      await render();
      expect(mocks.network).toHaveBeenCalledTimes(2);
    });

    it("GUIZHI_WEB_INERT_RESOURCES=load 恢复真实加载：图片请求出网并占请求预算", async () => {
      vi.stubEnv("GUIZHI_WEB_INERT_RESOURCES", "load");
      mocks.flood = true;
      mocks.floodAccept = IMAGE_ACCEPT;
      await expect(renderer.render(request, new AbortController().signal)).rejects.toThrow("预算");
    });
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
  describe("主框架导航的范围防护", () => {
    const scoped = {
      ...request,
      url: "https://example.com/docs/",
      scope: { origin: "https://example.com", directory: "/docs/" },
    };
    const okPage = {
      status: 200,
      headers: { "content-type": "text/html" },
      body: Buffer.from("<p>正文</p>").toString("base64"),
    };

    it("在窗口上注册 will-frame-navigate 与 will-redirect，覆盖脚本跳转、点击、meta 刷新与重定向", async () => {
      await render(scoped);
      expect(
        mocks.windows[0].webContents.on.mock.calls.map(([event]: [string]) => event),
      ).toEqual(["will-frame-navigate", "will-redirect"]);
    });

    it("页面把主框架导航到范围外：取消导航、不请求目标页面，采集以越界失败", async () => {
      mocks.pageNavigations.push({ url: "https://example.com/other/b" });
      await expect(
        renderer.render(scoped, new AbortController().signal),
      ).rejects.toMatchObject({
        message: "导航超出允许范围",
        cause: { webCaptureCode: "security" },
      });
      // 只取过入口页面；越界目标没有被请求，更没有被执行。
      expect(mocks.network).toHaveBeenCalledTimes(1);
      expect(mocks.windows[0].destroyed).toBe(true);
    });

    it("跨站的主框架导航同样被取消", async () => {
      mocks.pageNavigations.push({ url: "https://elsewhere.example/x" });
      await expect(
        renderer.render(scoped, new AbortController().signal),
      ).rejects.toThrow("导航超出允许范围");
      expect(mocks.network).toHaveBeenCalledTimes(1);
    });

    it("范围内的导航放行，最终网址与主文档响应随之更新", async () => {
      mocks.pageNavigations.push({ url: "https://example.com/docs/next" });
      const page = await render(scoped);
      expect(page.url).toBe("https://example.com/docs/next");
      expect(mocks.network).toHaveBeenCalledTimes(2);
    });

    it("范围内的导航取页失败：按真实原因终止，而不是采到浏览器错误页", async () => {
      mocks.pageNavigations.push({ url: "https://example.com/docs/next" });
      mocks.network.mockImplementation(async (incoming: { url: string }) => {
        if (incoming.url.endsWith("/docs/next")) throw new Error("连接中断");
        return okPage;
      });
      await expect(
        renderer.render(scoped, new AbortController().signal),
      ).rejects.toThrow("连接中断");
    });

    it("目录范围只约束主框架：范围外的同源子框架与非 http(s) 的主框架导航都不终止采集", async () => {
      mocks.pageNavigations.push(
        { url: "https://example.com/widget", isMainFrame: false },
        // 应用唤起类跳转：外部协议由会话的权限处理器拒绝，不在这里终止采集。
        { url: "weixin://dl/business", isMainFrame: true },
      );
      const page = await render(scoped);
      expect(page.url).toBe("https://example.com/docs/");
      expect(mocks.network).toHaveBeenCalledTimes(1);
      expect(mocks.navigationLog.map((entry) => entry.prevented)).toEqual([false, false]);
    });

    it("没有目录范围时不拦截主框架导航", async () => {
      mocks.pageNavigations.push({ url: "https://elsewhere.example/page" });
      const page = await render();
      expect(page.url).toBe("https://elsewhere.example/page");
    });

    it("webRequest 监听只取消 WebSocket 与非 http(s) 请求，http(s) 交给 protocol.handle", async () => {
      await render();
      const hook = mocks.sessions[0].webRequest.onBeforeRequest.mock.calls[0][0];
      const cancelled = (url: string, resourceType: string) => {
        let cancel: boolean | undefined;
        hook({ url, resourceType }, (result: { cancel: boolean }) => {
          cancel = result.cancel;
        });
        return cancel;
      };
      expect(cancelled("ws://127.0.0.1:8080/ws", "webSocket")).toBe(true);
      expect(cancelled("wss://example.com/ws", "webSocket")).toBe(true);
      expect(cancelled("https://example.com/ws", "webSocket")).toBe(true);
      expect(cancelled("ftp://example.com/f", "mainFrame")).toBe(true);
      expect(cancelled("file:///C:/Windows/win.ini", "subFrame")).toBe(true);
      // http(s) 的内网/范围判断不在这里：由 serve 内的出口校验与导航事件负责。
      expect(cancelled("https://example.com/a.png", "image")).toBe(false);
      expect(cancelled("http://example.com/", "mainFrame")).toBe(false);
      // 渲染结束后监听被摘除。
      expect(mocks.sessions[0].webRequest.onBeforeRequest).toHaveBeenLastCalledWith(null);
    });
  });
  describe("跨源子框架", () => {
    const frames = [
      // 视频嵌入、广告、统计：跨源
      { url: "https://www.youtube.com/embed/abc", isMainFrame: false },
      { url: "http://ads.example/slot", isMainFrame: false },
      // 同源子框架照常加载；about:blank 不产生网络请求
      { url: "https://example.com/widget", isMainFrame: false },
      { url: "about:blank", isMainFrame: false },
    ];

    it("取消跨源子框架，放行同源与非 http(s) 子框架，采集照常完成并在追踪里记数", async () => {
      vi.useFakeTimers();
      mocks.pageNavigations.push(...frames);
      const { trace, fields } = recordingTrace();
      const work = renderer.render(request, new AbortController().signal, { trace });
      await vi.runAllTimersAsync();
      await work;
      expect(mocks.navigationLog.map((entry) => entry.prevented)).toEqual([true, true, false, false]);
      expect(fields).toContainEqual({ blockedFrames: 2 });
      // 被取消的子框架从未走到出口：只取过入口页面。
      expect(mocks.network).toHaveBeenCalledTimes(1);
    });

    it("没有被取消的子框架时，追踪里不出现 blockedFrames", async () => {
      vi.useFakeTimers();
      mocks.pageNavigations.push(frames[2], frames[3]);
      const { trace, fields } = recordingTrace();
      const work = renderer.render(request, new AbortController().signal, { trace });
      await vi.runAllTimersAsync();
      await work;
      expect(fields.some((patch) => "blockedFrames" in patch)).toBe(false);
    });

    it("子框架重定向到跨源同样被取消（will-redirect）", async () => {
      const window = () => mocks.windows[0];
      mocks.network.mockResolvedValue({
        status: 200,
        headers: { "content-type": "text/html" },
        body: Buffer.from("<p>正文</p>").toString("base64"),
      });
      await render();
      expect(
        window().emitNavigation("will-redirect", "https://tracker.example/pixel", false),
      ).toBe(true);
      expect(
        window().emitNavigation("will-redirect", "https://example.com/next", false),
      ).toBe(false);
    });

    it("GUIZHI_WEB_SUBFRAMES=allow 恢复加载全部子框架", async () => {
      vi.stubEnv("GUIZHI_WEB_SUBFRAMES", "allow");
      try {
        mocks.pageNavigations.push(...frames);
        await render();
        expect(mocks.navigationLog.every((entry) => !entry.prevented)).toBe(true);
      } finally {
        vi.unstubAllEnvs();
      }
    });
  });
  it("请求预算用尽时，错误里写明请求数最多的主机，便于判断是页面本身还是第三方内容", async () => {
    mocks.flood = true;
    await expect(
      renderer.render(request, new AbortController().signal),
    ).rejects.toThrow("页面网络预算已用尽（超过 200 个请求，主要来源：example.com ×201）");
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

  function recordingTrace() {
    const marks: string[] = [];
    const fields: Record<string, unknown>[] = [];
    const trace: WebTrace = {
      enabled: true,
      mark: (name) => marks.push(name),
      set: (patch) => fields.push(patch),
      egress: () => undefined,
      flush: async () => undefined,
    };
    return { trace, marks, fields };
  }
  it("快照展开了 shadow root 时，追踪记录宿主数；没有展开则不记录", async () => {
    vi.useFakeTimers();
    mocks.snapshot = {
      html: "<html><body>完整正文</body></html>",
      flattenedShadowRoots: 3,
    };
    const first = recordingTrace();
    const work = renderer.render(request, new AbortController().signal, {
      trace: first.trace,
    });
    await vi.runAllTimersAsync();
    await work;
    expect(first.fields).toContainEqual({ shadowRoots: 3 });

    mocks.snapshot = { html: "<html><body>完整正文</body></html>" };
    const second = recordingTrace();
    const again = renderer.render(request, new AbortController().signal, {
      trace: second.trace,
    });
    await vi.runAllTimersAsync();
    await again;
    expect(second.fields.some((patch) => "shadowRoots" in patch)).toBe(false);
  });
  it("追踪按渲染流程依次记录页面阶段", async () => {
    vi.useFakeTimers();
    const { trace, marks } = recordingTrace();
    const work = renderer.render(request, new AbortController().signal, {
      trace,
    });
    await vi.runAllTimersAsync();
    await work;
    expect(marks).toEqual([
      "renderGate",
      "slot",
      "window",
      "load",
      "settle",
      "snapshot",
      "links",
      "drain",
      "cleanup",
    ]);
  });
  it("追踪开启时记录主框架 DOMContentLoaded 的时刻；追踪关闭时不注册监听", async () => {
    vi.useFakeTimers();
    const { trace, fields } = recordingTrace();
    const work = renderer.render(request, new AbortController().signal, { trace });
    await vi.runAllTimersAsync();
    await work;
    expect(fields).toContainEqual({ domReadyMs: expect.any(Number) });
    await render();
    expect(mocks.windows[1].webContents.once).not.toHaveBeenCalled();
  });
  it("渲染失败时追踪记录 error 阶段，随后仍记录收尾", async () => {
    mocks.network.mockRejectedValue(new Error("拒绝内网目标"));
    const { trace, marks } = recordingTrace();
    await expect(
      renderer.render(request, new AbortController().signal, { trace }),
    ).rejects.toThrow("内网");
    expect(marks).toEqual([
      "renderGate",
      "slot",
      "window",
      "error",
      "drain",
      "cleanup",
    ]);
  });
  it("子请求带上连接范围、资源类型与槽位等待时间", async () => {
    vi.useFakeTimers();
    const pool = new WebConnectionPool();
    const { trace } = recordingTrace();
    const work = renderer.render(request, new AbortController().signal, {
      pool,
      trace,
    });
    await vi.runAllTimersAsync();
    await work;
    pool.close();
    expect(mocks.network.mock.calls[0][2]).toEqual({
      pool,
      trace,
      resourceType: "document",
      gateWaitMs: expect.any(Number),
    });
  });
  it("未传追踪与连接范围时沿用原行为：无连接范围、追踪为空实现", async () => {
    await render();
    const options = mocks.network.mock.calls[0][2];
    expect(options.pool).toBeUndefined();
    expect(options.trace.enabled).toBe(false);
    // 追踪关闭时不向出口传资源类型。
    expect(options.resourceType).toBeUndefined();
  });
  it("追踪开启但请求没有 Accept 时，资源类型归入 other", async () => {
    vi.useFakeTimers();
    const original = globalThis.Request;
    // 换成不带请求头的构造，模拟没有 Accept 的请求。
    globalThis.Request = class extends original {
      constructor(input: RequestInfo | URL, init?: RequestInit) {
        super(input, { ...init, headers: undefined });
      }
    } as typeof Request;
    try {
      const { trace } = recordingTrace();
      const work = renderer.render(request, new AbortController().signal, { trace });
      await vi.runAllTimersAsync();
      await work;
    } finally {
      globalThis.Request = original;
    }
    expect(mocks.network.mock.calls[0][2].resourceType).toBe("other");
  });

  describe("沉降等待", () => {
    // 沉降等待关心真实网络活动的类别；占位图本地应答（默认）下图片请求根本不会登记，
    // 所以这里恢复真实加载，才能继续覆盖“图片在途不阻止沉降”的语义。
    beforeEach(() => vi.stubEnv("GUIZHI_WEB_INERT_RESOURCES", "load"));
    afterEach(() => vi.unstubAllEnvs());
    const okResponse = {
      status: 200,
      headers: { "content-type": "text/html" },
      body: Buffer.from("<p>正文</p>").toString("base64"),
    };
    const settleOf = (fields: Record<string, unknown>[]) =>
      fields.find((patch) => "settle" in patch)?.settle;
    const scriptsOf = (index = 0) =>
      mocks.windows[index].webContents.executeJavaScript.mock.calls.map(
        ([script]: [string]) => script,
      );

    /** 假定时器下完整渲染一次，返回虚拟耗时；performance.now 改接虚拟时钟，保证静止判定可复现。 */
    async function renderTimed() {
      vi.useFakeTimers();
      const clock = vi
        .spyOn(performance, "now")
        .mockImplementation(() => Date.now());
      try {
        const { trace, fields } = recordingTrace();
        const startedAt = Date.now();
        const work = renderer.render(request, new AbortController().signal, {
          trace,
        });
        await vi.runAllTimersAsync();
        await work;
        return { elapsed: Date.now() - startedAt, fields };
      } finally {
        clock.mockRestore();
      }
    }

    it("默认事件驱动：页面静止即提前结束，仍滚动三次并记录沉降结果", async () => {
      const { elapsed, fields } = await renderTimed();
      expect(settleOf(fields)).toMatchObject({ reason: "quiet", initialMs: 300 });
      // 旧版固定等待为 1800 ms。
      expect(elapsed).toBe(600);
      expect(scriptsOf().filter((script) => script.includes("scrollBy"))).toHaveLength(3);
    });

    it("GUIZHI_WEB_SETTLE=fixed 回退为固定等待：不向页面注入探针", async () => {
      vi.stubEnv("GUIZHI_WEB_SETTLE", "fixed");
      try {
        const { elapsed, fields } = await renderTimed();
        expect(settleOf(fields)).toMatchObject({ reason: "fixed", initialMs: 1200 });
        expect(elapsed).toBe(1800);
        expect(scriptsOf().some((script) => script.includes("guizhi.settle"))).toBe(false);
      } finally {
        vi.unstubAllEnvs();
      }
    });

    it("页面探针无法安装或读取时退回固定等待，记为 blind，采集照常完成", async () => {
      mocks.probe.install = false;
      const installFailed = await renderTimed();
      expect(settleOf(installFailed.fields)).toMatchObject({ reason: "blind", initialMs: 1200 });
      expect(installFailed.elapsed).toBe(1800);

      mocks.probe.install = true;
      mocks.probe.sampleFails = true;
      const readFailed = await renderTimed();
      expect(settleOf(readFailed.fields)).toMatchObject({ reason: "blind", initialMs: 1200 });
      expect(readFailed.elapsed).toBe(1800);
    });

    it("子请求进行期间不提前结束，结束后再静止 300 ms 才退出", async () => {
      mocks.background = true;
      mocks.network.mockImplementation((incoming) =>
        incoming.url.endsWith("late")
          ? new Promise((resolve) => setTimeout(() => resolve(okResponse), 600))
          : Promise.resolve(okResponse),
      );
      const { elapsed, fields } = await renderTimed();
      // 请求在 600 ms 结束，其后 300 ms 静止，在 900 ms 的轮询上退出。
      expect(settleOf(fields)).toMatchObject({ reason: "quiet", initialMs: 900 });
      expect(elapsed).toBe(1200);
    });

    /** 后台请求 late 一直不结束，直到页面被取消。 */
    const hangingLate = () =>
      mocks.network.mockImplementation((incoming, signal: AbortSignal) =>
        incoming.url.endsWith("late")
          ? new Promise((_resolve, reject) =>
              signal.addEventListener("abort", () => reject(new Error("请求已取消")), {
                once: true,
              }),
            )
          : Promise.resolve(okResponse),
      );

    it.each([
      ["image", "image/jxl,image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"],
      ["style", "text/css,*/*;q=0.1"],
      // 子框架与 <link rel=prefetch> 都用 Chromium 的导航式 Accept。
      [
        "document",
        "text/html,application/xhtml+xml,application/xml;q=0.9,image/jxl,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
      ],
    ])("在途的是%s请求（不改动主框架 DOM）时不阻止提前结束；诊断里仍能看到它", async (kind, accept) => {
      mocks.background = true;
      mocks.backgroundAccept = accept;
      hangingLate();
      const { elapsed, fields } = await renderTimed();
      expect(settleOf(fields)).toMatchObject({
        reason: "quiet",
        initialMs: 300,
        last: { inflight: { [kind]: 1 } },
      });
      expect(elapsed).toBe(600);
    });

    it("在途的是脚本、XHR/fetch 等（Accept 为 */*）请求时同样阻止提前结束", async () => {
      mocks.background = true;
      mocks.backgroundAccept = "*/*";
      mocks.network.mockImplementation((incoming) =>
        incoming.url.endsWith("late")
          ? new Promise((resolve) => setTimeout(() => resolve(okResponse), 600))
          : Promise.resolve(okResponse),
      );
      const { fields } = await renderTimed();
      expect(settleOf(fields)).toMatchObject({ reason: "quiet", initialMs: 900 });
    });

    it("XHR 取 HTML 片段（Accept 为 text/html, */*）不是文档：它在途时同样阻止提前结束", async () => {
      mocks.background = true;
      mocks.backgroundAccept = "text/html, */*; q=0.01";
      mocks.network.mockImplementation((incoming) =>
        incoming.url.endsWith("late")
          ? new Promise((resolve) => setTimeout(() => resolve(okResponse), 600))
          : Promise.resolve(okResponse),
      );
      const { fields } = await renderTimed();
      expect(settleOf(fields)).toMatchObject({ reason: "quiet", initialMs: 900 });
    });

    it("子请求一直不结束时等满首段，总等待不超过旧版", async () => {
      mocks.background = true;
      hangingLate();
      const { elapsed, fields } = await renderTimed();
      expect(settleOf(fields)).toMatchObject({ reason: "cap", initialMs: 1200 });
      expect(elapsed).toBe(1800);
    });

    it("等满首段时，追踪里记录是哪个请求拖住了静止（类别、origin、在途时长）", async () => {
      mocks.background = true;
      hangingLate();
      const { fields } = await renderTimed();
      const last = (settleOf(fields) as { last: { blockers: Record<string, unknown>[] } }).last;
      expect(last.blockers).toEqual([
        { kind: "other", origin: "https://example.com", ageMs: expect.any(Number) },
      ]);
      expect(last.blockers[0].ageMs as number).toBeGreaterThanOrEqual(1000);
    });

    it("沉降等待期间取消：立即失败并销毁窗口", async () => {
      vi.useFakeTimers();
      const controller = new AbortController();
      const work = renderer.render(request, controller.signal);
      const rejected = expect(work).rejects.toThrow("取消");
      await vi.advanceTimersByTimeAsync(150);
      controller.abort();
      await vi.runAllTimersAsync();
      await rejected;
      expect(mocks.windows[0].destroyed).toBe(true);
      expect(mocks.sessions[0].clearStorageData).toHaveBeenCalled();
    });
  });
});
