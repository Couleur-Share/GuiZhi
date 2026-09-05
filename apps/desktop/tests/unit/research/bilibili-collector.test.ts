import { describe, expect, it, vi } from "vitest";
import {
  BilibiliResearchCollector,
  parseBilibiliSearchResponse,
} from "../../../src/main/services/research/collectors";

function searchInput() {
  return {
    topic: "本地知识库",
    rangeFrom: 1_700_000_000_000,
    rangeTo: 1_800_000_000_000,
    cursor: null,
    limit: 20,
    signal: new AbortController().signal,
  };
}

describe("Bilibili research collector fixture", () => {
  it("清理标题 HTML，映射 BVID、日期与互动字段", () => {
    const page = parseBilibiliSearchResponse(
      {
        code: 0,
        data: {
          page: 1,
          numPages: 2,
          result: [
            {
              bvid: "BV1fixture",
              title: "<em class=\"keyword\">本地 AI</em> 知识库",
              author: "作者",
              description: "  一段\n简介  ",
              pubdate: 1_720_000_000,
              play: "1,234",
              video_review: 56,
              review: 7,
              like: 89,
              favorites: 12,
            },
          ],
        },
      },
      { rangeFrom: 1_700_000_000_000, rangeTo: 1_730_000_000_000 },
      1,
    );
    expect(page).toMatchObject({ cursor: "2", hasMore: true });
    expect(page.items[0]).toMatchObject({
      externalId: "BV1fixture",
      url: "https://www.bilibili.com/video/BV1fixture",
      title: "本地 AI 知识库",
      snippet: "一段 简介",
      publishedAt: 1_720_000_000_000,
      dateConfidence: "high",
      engagement: {
        views: 1234,
        danmaku: 56,
        comments: 7,
        likes: 89,
        favorites: 12,
      },
    });
  });

  it("保留窗口外及未知日期候选，覆盖统计分别记录", () => {
    const page = parseBilibiliSearchResponse(
      {
        code: 0,
        data: {
          result: [
            { bvid: "BVold", title: "旧视频", pubdate: 100 },
            { bvid: "BVunknown", title: "未知日期" },
          ],
        },
      },
      { rangeFrom: 1_000_000, rangeTo: 2_000_000 },
    );
    expect(page.items.map((item) => item.externalId)).toEqual(["BVold", "BVunknown"]);
    expect(page.items[1].dateConfidence).toBe("low");
    expect(page.inWindowCount).toBe(0);
    expect(page.unknownDateCount).toBe(1);
  });

  it("使用 WBI 搜索入口，并在 HTTP 412 时补匿名访客 Cookie 重试", async () => {
    const requestJson = vi
      .fn()
      .mockRejectedValueOnce(new Error("HTTP 412"))
      .mockResolvedValueOnce({
        code: 0,
        data: { b_3: "visitor-3", b_4: "visitor-4" },
      })
      .mockResolvedValueOnce({ code: 0, data: { result: [] } });
    const collector = new BilibiliResearchCollector(requestJson as never);

    await expect(collector.search(searchInput())).resolves.toMatchObject({
      items: [],
      hasMore: false,
    });

    expect(requestJson.mock.calls[0][0]).toContain(
      "/x/web-interface/wbi/search/type",
    );
    expect(requestJson.mock.calls[1][0]).toBe(
      "https://api.bilibili.com/x/frontend/finger/spi",
    );
    expect(requestJson.mock.calls[2][2]).toMatchObject({
      cookie: "buvid3=visitor-3; buvid4=visitor-4",
      userAgent: expect.stringContaining("Chrome/126.0.0.0"),
    });
  });

  it("访客重试仍被 412 拦截时返回可行动诊断", async () => {
    const requestJson = vi
      .fn()
      .mockRejectedValueOnce(new Error("HTTP 412"))
      .mockResolvedValueOnce({ code: 0, data: { b_3: "visitor-3" } })
      .mockRejectedValueOnce(new Error("HTTP 412"));
    const collector = new BilibiliResearchCollector(requestJson as never);

    await expect(collector.search(searchInput())).rejects.toThrow(
      "B 站搜索触发访问保护，请稍后重试",
    );
  });
});
