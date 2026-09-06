import { describe, expect, it, vi } from "vitest";
import type { ElectronCapturePage } from "../../../src/main/services/platform-capture/electron-capture-runtime";
import {
  captureDouyinDetailPage,
  douyinDetailResponseHtml,
} from "../../../src/main/services/platform-capture/douyin-detail-capture";

const id = "7669754297756737722";
const detail = {
  aweme_id: id,
  desc: "目标作品",
  video: { play_addr: { url_list: ["https://example.com/video.mp4"] } },
};
function createPage() {
  const payloads: unknown[] = [];
  const page = {
    goto: vi.fn().mockResolvedValue(undefined),
    url: () => `https://www.douyin.com/video/${id}`,
    startJsonCapture: vi.fn(() => payloads),
    stopJsonCapture: vi.fn(),
    waitForTimeout: vi.fn().mockResolvedValue(undefined),
    evaluate: vi.fn().mockResolvedValue(false),
  };
  return { page, payloads, typed: page as unknown as ElectronCapturePage };
}

describe("离屏抖音详情采集", () => {
  it("忽略早期空 loader 和推荐作品，匹配实际目标详情", () => {
    const payloads = [
      { loaderData: { page: {} } },
      { aweme_detail: { ...detail, aweme_id: "999999999" } },
    ];
    expect(douyinDetailResponseHtml(payloads, id)).toBeNull();
    expect(
      douyinDetailResponseHtml([...payloads, { aweme_detail: detail }], id),
    ).toContain("目标作品");
    expect(douyinDetailResponseHtml([{ item_list: [detail] }], id)).toContain(
      id,
    );
    expect(
      douyinDetailResponseHtml(
        [
          { aweme_detail: detail },
          {
            unrelated: Array.from({ length: 5000 }, () => ({
              value: "推荐数据",
            })),
          },
        ],
        id,
      ),
    ).toContain(id);
    const cyclic: Record<string, unknown> = {};
    cyclic.self = cyclic;
    expect(douyinDetailResponseHtml([cyclic], id)).toBeNull();
  });
  it("等候延迟到达的目标响应，成功后停止监听", async () => {
    const { page, payloads, typed } = createPage();
    page.waitForTimeout.mockImplementation(async () => {
      payloads.push({ aweme_detail: detail });
    });
    expect(
      await captureDouyinDetailPage(typed, id, new AbortController().signal),
    ).toContain(id);
    expect(page.goto).toHaveBeenCalledWith(
      `https://www.douyin.com/video/${id}`,
      expect.anything(),
    );
    expect(page.waitForTimeout).toHaveBeenCalledTimes(1);
    expect(page.stopJsonCapture).toHaveBeenCalledTimes(1);
  });
  it("等待有上限，空响应不会被误报为已删除", async () => {
    const { page, typed } = createPage();
    await expect(
      captureDouyinDetailPage(typed, id, new AbortController().signal),
    ).rejects.toMatchObject({ code: "platform_changed" });
    expect(page.waitForTimeout).toHaveBeenCalledTimes(40);
    expect(page.stopJsonCapture).toHaveBeenCalledTimes(1);
  });
  it("验证与取消分别返回可识别原因并清理监听", async () => {
    const { page, typed } = createPage();
    page.evaluate.mockResolvedValue(true);
    await expect(
      captureDouyinDetailPage(typed, id, new AbortController().signal),
    ).rejects.toMatchObject({ code: "verification_required" });
    const controller = new AbortController();
    page.waitForTimeout.mockImplementation(async () => {
      controller.abort();
    });
    await expect(
      captureDouyinDetailPage(typed, id, controller.signal),
    ).rejects.toMatchObject({ code: "canceled" });
    expect(page.stopJsonCapture).toHaveBeenCalledTimes(2);
  });
  it("跳转到非官方地址或导航失败也清理监听", async () => {
    const { page, typed } = createPage();
    page.url = () => "https://www.douyin.com.evil.test/video/123";
    await expect(
      captureDouyinDetailPage(typed, id, new AbortController().signal),
    ).rejects.toMatchObject({ code: "platform_changed" });
    page.goto.mockRejectedValue(new Error("页面加载失败"));
    await expect(
      captureDouyinDetailPage(typed, id, new AbortController().signal),
    ).rejects.toThrow("页面加载失败");
    expect(page.stopJsonCapture).toHaveBeenCalledTimes(2);
  });
});
