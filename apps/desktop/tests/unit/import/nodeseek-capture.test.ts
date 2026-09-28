// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
vi.mock("../../../src/main/services/import/safe-fetch", () => ({
  assertSafeTarget: vi.fn().mockResolvedValue(undefined),
}));
import {
  readNodeseekPage,
  nodeseekVerificationUrl,
} from "../../../src/main/services/platform-capture/nodeseek-capture";
import type { ElectronCapturePage } from "../../../src/main/services/platform-capture/electron-capture-runtime";
import { assertSafeTarget } from "../../../src/main/services/import/safe-fetch";

const url = "https://www.nodeseek.com/post-123-1";
const html =
  '<html><h1><a class="post-title-link" href="/post-123-1">帖子</a></h1><div class="content-item" id="0"><span class="date-created"><time datetime="2026-09-20T10:35:55Z"></time></span><article class="post-content">正文</article></div></html>';
const challenge =
  "<html><head><title>Just a moment...</title></head><body>验证</body></html>";
function mockPage() {
  let now = 0;
  vi.spyOn(Date, "now").mockImplementation(() => now);
  return {
    goto: vi.fn().mockResolvedValue(undefined),
    url: () => url,
    isClosed: () => false,
    content: vi.fn().mockResolvedValue(html),
    waitForTimeout: vi.fn(async (ms: number) => {
      now += ms;
    }),
  };
}
afterEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
});
describe("NodeSeek 浏览器验证生命周期", () => {
  it("目标链接严格校验后归一到主楼", () => {
    expect(nodeseekVerificationUrl(url.replace(/-1$/, "-8"))).toBe(
      url.replace(/-1$/, "-8"),
    );
    expect(() =>
      nodeseekVerificationUrl("https://localhost/post-123-1"),
    ).toThrow();
    expect(() =>
      nodeseekVerificationUrl("https://www.nodeseek.com@evil.test/post-123-1"),
    ).toThrow();
  });
  it("保持同一页面等待用户验证，拿到真正正文才完成", async () => {
    const page = mockPage();
    page.content
      .mockResolvedValueOnce(challenge)
      .mockResolvedValueOnce(challenge);
    expect(
      await readNodeseekPage(
        page as unknown as ElectronCapturePage,
        url,
        new AbortController().signal,
        true,
      ),
    ).toBe(html);
    expect(page.goto).toHaveBeenCalledOnce();
    expect(page.content).toHaveBeenCalledTimes(3);
    expect(assertSafeTarget).toHaveBeenCalledWith(new URL(url), true);
  });
  it("后台遇到验证会停止并返回验证入口，不将验证页入库", async () => {
    const page = mockPage();
    page.content.mockResolvedValue(challenge);
    const blocked = "https://www.nodeseek.com/post-123-3";
    page.url = () => blocked;
    await expect(
      readNodeseekPage(
        page as unknown as ElectronCapturePage,
        blocked,
        new AbortController().signal,
      ),
    ).rejects.toThrow(blocked);
  });
  it("页面关闭与取消不会触发成功", async () => {
    const page = mockPage();
    page.isClosed = () => true;
    await expect(
      readNodeseekPage(
        page as unknown as ElectronCapturePage,
        url,
        new AbortController().signal,
        true,
      ),
    ).rejects.toThrow("验证窗口已关闭");
    const controller = new AbortController();
    controller.abort();
    page.goto.mockClear();
    await expect(
      readNodeseekPage(
        page as unknown as ElectronCapturePage,
        url,
        controller.signal,
        true,
      ),
    ).rejects.toThrow();
    expect(page.goto).not.toHaveBeenCalled();
  });
  it("验证重定向瞬间的执行上下文销毁可恢复", async () => {
    const page = mockPage();
    page.content.mockRejectedValueOnce(
      new Error("Execution context was destroyed"),
    );
    expect(
      await readNodeseekPage(
        page as unknown as ElectronCapturePage,
        url,
        new AbortController().signal,
        true,
      ),
    ).toBe(html);
  });
  it("内网地址拦截直接传播，不继续导航", async () => {
    const page = mockPage();
    vi.mocked(assertSafeTarget).mockRejectedValueOnce(
      new Error("不允许访问内网地址"),
    );
    await expect(
      readNodeseekPage(
        page as unknown as ElectronCapturePage,
        url,
        new AbortController().signal,
      ),
    ).rejects.toThrow("不允许访问内网地址");
    expect(page.goto).not.toHaveBeenCalled();
  });
});
