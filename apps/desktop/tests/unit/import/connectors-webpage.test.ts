// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { extractContent } from "../../../src/main/services/import/connectors";
import { fetchHtml } from "../../../src/main/services/import/safe-fetch";

const extract = vi.hoisted(() => vi.fn());
vi.mock("../../../src/main/services/import/safe-fetch", () => ({ fetchHtml: vi.fn() }));
vi.mock("../../../src/main/services/web-capture/web-node-extractor", () => ({ webNodeExtractor: () => ({ extract }) }));
// 只验证普通网页分支；视频分支的依赖很重，与本用例无关。
vi.mock("../../../src/main/services/import/video-url", () => ({ detectVideoPlatform: () => null, extractVideoUrl: vi.fn() }));

const url = "https://example.com/blog/post";
const article = (body: string) =>
  `<html><head><title>兼容采集</title></head><body><article>${"<p>这是一段公开文章的正文内容，足够长，能被识别为正文。</p>".repeat(4)}${body}</article></body></html>`;

describe("兼容网页采集与网页采集共用正文提取", () => {
  it("交给共享提取线程，沿用其绝对地址与代码块规则，并保留来源行", async () => {
    const { extractWebArticle } = await import("../../../src/main/services/web-capture/web-extract-article");
    extract.mockImplementation(async (html: string, finalUrl: string, status: number) => extractWebArticle(html, finalUrl, status).result);
    vi.mocked(fetchHtml).mockResolvedValue({
      finalUrl: url,
      contentType: "text/html",
      html: article('<p><a href="../about">关于</a></p><pre><code class="language-py">x = 1 * 2</code></pre>'),
    });
    const result = await extractContent("url", url);
    expect(extract).toHaveBeenCalledWith(expect.any(String), url, 200, expect.any(AbortSignal));
    expect(result.title).toBe("兼容采集");
    expect(result.content).toContain("[关于](https://example.com/about)");
    expect(result.content).toContain("```py\nx = 1 * 2\n```");
    expect(result.content.endsWith(`\n\n---\n\n来源：<${url}>`)).toBe(true);
    expect(result.sourceUri).toBe(url);
    expect(result.degradedReason).toBeUndefined();
  });

  it("没有正文时给出可理解的原因，不入库空壳", async () => {
    extract.mockResolvedValue({ title: "空页", markdown: "", error: { code: "empty", message: "网页没有可保存的正文", retryable: false } });
    vi.mocked(fetchHtml).mockResolvedValue({ finalUrl: url, contentType: "text/html", html: "<html><body></body></html>" });
    const result = await extractContent("url", url);
    expect(result.content).toBe("");
    expect(result.degradedReason).toBe("未能抽取网页正文（页面可能由脚本动态渲染，或需要登录后才能访问）");
  });

  it("提取线程失败时说明原因，可重试", async () => {
    extract.mockRejectedValue(new Error("正文提取超时"));
    vi.mocked(fetchHtml).mockResolvedValue({ finalUrl: url, contentType: "text/html", html: article("") });
    const result = await extractContent("url", url);
    expect(result.content).toBe("");
    expect(result.degradedReason).toBe("网页正文提取失败：正文提取超时");
  });
});
