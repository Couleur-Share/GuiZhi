// @vitest-environment node
import { beforeEach, expect, it, vi } from "vitest";
const { search, capture } = vi.hoisted(() => ({ search: vi.fn(), capture: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/search-service", () => ({ searchReadingWeb: search }));
vi.mock("../../src/main/services/web-capture/web-capture", () => ({ captureWebPage: capture }));
import { searchArticleWeb } from "../../src/main/services/article-ask-search";
beforeEach(() => { search.mockReset(); capture.mockReset(); });
it("去重且每轮最多使用五份有效正文", async () => {
  search.mockResolvedValue(Array.from({ length: 8 }, (_, i) => ({ title: `资料${i}`, url: `https://example.com/${i}`, text: "有效正文".repeat(100) })));
  const result = await searchArticleWeb(["概念", "前提"], new AbortController().signal);
  expect(search).toHaveBeenCalledTimes(2); expect(result.sources).toHaveLength(5); expect(capture).not.toHaveBeenCalled();
  expect(search).toHaveBeenCalledWith("概念", expect.any(AbortSignal), "qa");
});
it("摘要不足时最多补抓三页，无正文不伪造来源", async () => {
  search.mockResolvedValue(Array.from({ length: 6 }, (_, i) => ({ title: "标题", url: `https://example.com/${i}` })));
  capture.mockResolvedValue({ complete: false, markdown: "", error: { message: "失败" } });
  const result = await searchArticleWeb(["词"], new AbortController().signal);
  expect(capture).toHaveBeenCalledTimes(3); expect(result.sources).toEqual([]); expect(result.warnings?.join()).toContain("未完成联网查证");
});
it("部分搜索失败仍保留有效资料并返回警告", async () => {
  search.mockRejectedValueOnce(new Error("HTTP 402：额度不足")).mockResolvedValueOnce([{ title: "资料", url: "https://example.com/a", text: "事实内容".repeat(100) }]);
  const result = await searchArticleWeb(["词一", "词二"], new AbortController().signal);
  expect(result.sources).toHaveLength(1); expect(result.warnings?.join()).toContain("402");
});
it("主动取消不会继续抓取或降级生成", async () => {
  const controller = new AbortController(); controller.abort();
  await expect(searchArticleWeb(["词"], controller.signal)).rejects.toThrow();
  expect(search).not.toHaveBeenCalled();
});
