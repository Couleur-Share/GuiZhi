import { describe, expect, it, vi } from "vitest";
vi.mock("electron", () => ({ session: { defaultSession: {} }, app: {} }));
vi.mock("../../../src/main/services/import/platform-parse-log", () => ({
  logPlatformStructureMissing: vi.fn(),
}));
import {
  fetchDouyinAweme,
  parseDouyinRouterData,
} from "../../../src/main/services/import/douyin";

const id = "7669754297756737722";
const item = {
  aweme_id: id,
  desc: "CI/CD 与手动发版",
  video: {
    duration: 193000,
    play_addr: { url_list: ["https://example.com/playwm/video"] },
  },
};
const wrap = (value: unknown) =>
  `<script>window._ROUTER_DATA = ${JSON.stringify({ loaderData: { page: value } })}</script>`;
// 2026-09-06 真实页面形态的最小夹具；不保存 webId、分享跟踪参数或 Cookie。
const shell = wrap({
  itemId: id,
  isVideoOptimize: true,
  commonContext: { renderInSSR: 1 },
});
const complete = wrap({ videoInfoRes: { item_list: [item] } });

describe("抖音分享页兼容回退", () => {
  it("缺少 videoInfoRes 是结构缺失，不能判定作品已删除", () => {
    expect(() => parseDouyinRouterData(shell, id)).toThrow(
      "[structure_missing]",
    );
  });
  it("短链保留原任务，回退时只传解析出的目标 ID，并贯穿取消信号", async () => {
    const signal = new AbortController().signal;
    const captureDetail = vi.fn().mockResolvedValue(complete);
    const fetchPage = vi
      .fn()
      .mockResolvedValue({
        html: shell,
        finalUrl: `https://www.iesdouyin.com/share/video/${id}/?region=CN`,
      });
    const result = await fetchDouyinAweme(
      "https://v.douyin.com/eP6DBnoYQXc/",
      signal,
      { fetchPage, captureDetail },
    );
    expect(result).toMatchObject({
      awemeId: id,
      title: "CI/CD 与手动发版",
      durationSeconds: 193,
      webpageUrl: `https://www.douyin.com/video/${id}`,
      playUrl: "https://example.com/play/video",
    });
    expect(captureDetail).toHaveBeenCalledTimes(1);
    expect(captureDetail).toHaveBeenCalledWith(id, signal);
    expect(fetchPage).toHaveBeenCalledTimes(2);
  });
  it("SSR 可用时不打开浏览器", async () => {
    const captureDetail = vi.fn();
    await fetchDouyinAweme(`https://www.douyin.com/video/${id}`, undefined, {
      fetchPage: async () => ({ html: complete, finalUrl: "" }),
      captureDetail,
    });
    expect(captureDetail).not.toHaveBeenCalled();
  });
  it("平台明确给出不可用原因时不再尝试浏览器", async () => {
    const captureDetail = vi.fn();
    await expect(
      fetchDouyinAweme(`https://www.douyin.com/video/${id}`, undefined, {
        fetchPage: async () => ({
          html: wrap({
            videoInfoRes: {
              item_list: [],
              filter_list: [{ detail_msg: "该作品已被作者删除" }],
            },
          }),
          finalUrl: "",
        }),
        captureDetail,
      }),
    ).rejects.toThrow("[note_unavailable] 该作品已被作者删除");
    expect(captureDetail).not.toHaveBeenCalled();
  });
  it("抓取被取消或网络失败时不启动浏览器回退", async () => {
    const captureDetail = vi.fn();
    const controller = new AbortController();
    controller.abort();
    await expect(
      fetchDouyinAweme(
        `https://www.douyin.com/video/${id}`,
        controller.signal,
        {
          fetchPage: async () => ({ html: shell, finalUrl: "" }),
          captureDetail,
        },
      ),
    ).rejects.toThrow("已取消");
    await expect(
      fetchDouyinAweme(`https://www.douyin.com/video/${id}`, undefined, {
        fetchPage: async () => {
          throw new Error("连接超时");
        },
        captureDetail,
      }),
    ).rejects.toThrow("连接超时");
    expect(captureDetail).not.toHaveBeenCalled();
  });
  it("返回其他作品时拒绝导入，多个作品时按目标 ID 选择", () => {
    const other = { ...item, aweme_id: "7669754297756737723" };
    expect(() =>
      parseDouyinRouterData(wrap({ videoInfoRes: { item_list: [other] } }), id),
    ).toThrow("[structure_missing]");
    expect(
      parseDouyinRouterData(
        wrap({ videoInfoRes: { item_list: [other, item] } }),
        id,
      ).awemeId,
    ).toBe(id);
  });
});
