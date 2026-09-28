import { beforeEach, describe, expect, it, vi } from "vitest";
import { ElectronWebCapture } from "../../../src/main/services/web-capture/web-electron-capture";
const mocks = vi.hoisted(() => ({
  network: vi.fn(),
  render: vi.fn(),
  extract: vi.fn(),
  close: vi.fn(),
  verify: vi.fn(),
  solver: vi.fn(),
  solverSettings: vi.fn(),
}));
vi.mock("../../../src/main/services/web-capture/flaresolverr", () => ({
  captureWithFlareSolverr: mocks.solver,
  getFlareSolverrSettings: mocks.solverSettings,
}));
vi.mock("../../../src/main/services/web-capture/web-verification-browser", () => ({
  captureVerifiedWebPage: mocks.verify,
}));
vi.mock("../../../src/main/services/web-capture/web-network", () => ({
  webNetworkRequest: mocks.network,
}));
vi.mock("../../../src/main/services/web-capture/web-electron-renderer", () => ({
  WebElectronRenderer: class {
    render = mocks.render;
  },
}));
vi.mock("../../../src/main/services/web-capture/web-html-extractor", () => ({
  WebHtmlExtractor: class {
    extract = mocks.extract;
    close = mocks.close;
    running = false;
  },
}));
const request = {
  taskId: "test",
  purpose: "import" as const,
  url: "https://example.com/",
};
const response = (html: string, status = 200) => ({
  status,
  headers: { "content-type": "text/html; charset=utf-8" },
  body: Buffer.from(html).toString("base64"),
});
beforeEach(() => {
  vi.clearAllMocks();
  mocks.solverSettings.mockReturnValue({ enabled: false, connection: "local", port: 8191, sshHost: "" });
  mocks.extract.mockResolvedValue({
    markdown: "正文",
    complete: true,
    paragraphs: [],
  });
  mocks.render.mockResolvedValue({
    html: "<main>动态正文</main>",
    url: request.url,
    status: 200,
    links: [],
  });
  mocks.close.mockResolvedValue(undefined);
});
describe("正式采集分流和安全失败", () => {
  it("启用服务后仅验证页自动转 FlareSolverr，直接提取返回正文", async () => {
    mocks.solverSettings.mockReturnValue({ enabled: true, connection: "ssh", port: 8191, sshHost: "gatewaysentry" });
    mocks.network.mockResolvedValue(response("<title>Just a moment...</title>", 403));
    mocks.solver.mockResolvedValue({ html: "<article>已验证正文</article>", url: request.url, status: 200, links: [] });
    const result = await new ElectronWebCapture().capture(request, new AbortController().signal);
    expect(result.engineVersion).toContain("flaresolverr");
    expect(mocks.network).toHaveBeenCalledTimes(1);
    expect(mocks.extract).toHaveBeenCalledWith("<article>已验证正文</article>", request.url, 200, expect.any(AbortSignal));
    expect(mocks.verify).not.toHaveBeenCalled();
  });
  it("FlareSolverr 失败保留可验证重试，不自动打开无限勾选窗口", async () => {
    mocks.solverSettings.mockReturnValue({ enabled: true, connection: "ssh", port: 8191, sshHost: "gatewaysentry" });
    mocks.network.mockResolvedValue(response("<title>Just a moment...</title>", 403));
    mocks.solver.mockRejectedValue(new Error("FlareSolverr 未能完成请求"));
    await expect(new ElectronWebCapture().capture(request, new AbortController().signal)).rejects.toThrow("web_verification_required");
    expect(mocks.verify).not.toHaveBeenCalled(); expect(mocks.extract).not.toHaveBeenCalled();
  });
  it("启用服务仍不把普通 HTTP 403 当验证页", async () => {
    mocks.solverSettings.mockReturnValue({ enabled: true, connection: "ssh", port: 8191, sshHost: "gatewaysentry" });
    mocks.network.mockResolvedValue(response("Forbidden", 403));
    await new ElectronWebCapture().capture(request, new AbortController().signal);
    expect(mocks.solver).not.toHaveBeenCalled();
  });
  it("403 验证页先识别验证，不交给提取器当成限流", async () => {
    mocks.network.mockResolvedValue(response('<title>Just a moment...</title><div id="challenge-stage">验证</div>', 403));
    await expect(new ElectronWebCapture().capture(request, new AbortController().signal)).rejects.toThrow("web_verification_required");
    expect(mocks.extract).not.toHaveBeenCalled();
    expect(mocks.verify).not.toHaveBeenCalled();
  });
  it("主动验证直接提取已验证窗口，不重新请求静态页面", async () => {
    mocks.verify.mockResolvedValue({ html: "<article>验证后正文</article>", url: request.url, status: 200, links: [] });
    const result = await new ElectronWebCapture().capture({ ...request, interactiveVerification: true }, new AbortController().signal);
    expect(result.engineVersion).toContain("verified-electron");
    expect(mocks.network).not.toHaveBeenCalled();
    expect(mocks.extract).toHaveBeenCalledWith("<article>验证后正文</article>", request.url, 200, expect.any(AbortSignal));
  });
  it("短静态网页直接提取并补齐链接", async () => {
    mocks.network.mockResolvedValue(
      response('<p>正文</p><a href="/next">下一页</a>'),
    );
    const result = await new ElectronWebCapture().capture(
      request,
      new AbortController().signal,
    );
    expect(result.links).toEqual(["https://example.com/next"]);
    expect(result.engineVersion).toContain("static");
    expect(mocks.render).not.toHaveBeenCalled();
  });
  it("动态占位转 Electron；渲染失败不能回退为静态成功", async () => {
    mocks.network.mockResolvedValue(
      response('<main>Loading...</main><script src="/app.js"></script>'),
    );
    mocks.render.mockRejectedValue(new Error("渲染失败"));
    await expect(
      new ElectronWebCapture().capture(request, new AbortController().signal),
    ).rejects.toThrow("渲染失败");
    expect(mocks.extract).not.toHaveBeenCalled();
  });
  it("安全出口拒绝时不启动浏览器", async () => {
    mocks.network.mockRejectedValue(new Error("拒绝内网目标"));
    await expect(
      new ElectronWebCapture().capture(request, new AbortController().signal),
    ).rejects.toThrow("内网");
    expect(mocks.render).not.toHaveBeenCalled();
  });
  it("HTTP 错误保持失败状态，不升级浏览器绕过", async () => {
    mocks.network.mockResolvedValue(response("Forbidden", 403));
    mocks.extract.mockResolvedValue({
      complete: false,
      error: { code: "restricted" },
    });
    const result = await new ElectronWebCapture().capture(
      request,
      new AbortController().signal,
    );
    expect(result.error?.code).toBe("restricted");
    expect(mocks.render).not.toHaveBeenCalled();
  });
  it("重定向每一跳重新请求并在出范围前拒绝", async () => {
    mocks.network.mockResolvedValue({
      status: 302,
      headers: { location: "https://outside.example/" },
      body: "",
    });
    await expect(
      new ElectronWebCapture().capture(
        {
          ...request,
          scope: { origin: "https://example.com", directory: "/" },
        },
        new AbortController().signal,
      ),
    ).rejects.toThrow("范围");
    expect(mocks.network).toHaveBeenCalledTimes(1);
  });
  it("非网页响应明确失败", async () => {
    mocks.network.mockResolvedValue({
      status: 200,
      headers: { "content-type": "application/pdf" },
      body: "",
    });
    await expect(
      new ElectronWebCapture().capture(request, new AbortController().signal),
    ).rejects.toThrow("非网页");
  });
});
