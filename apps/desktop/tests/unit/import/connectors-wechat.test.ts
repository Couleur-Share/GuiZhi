import { describe, expect, it, vi } from "vitest";
import { extractContent } from "../../../src/main/services/import/connectors";
import { fetchHtml } from "../../../src/main/services/import/safe-fetch";
vi.mock("../../../src/main/services/import/safe-fetch", () => ({ fetchHtml: vi.fn() }));
vi.mock("../../../src/main/services/web-capture/snapshot-assets",()=>({
  collectSnapshotAssets: vi.fn(async()=>({assets:[{fileName:"wechat-"+"a".repeat(64)+".jpg",sha256:"a".repeat(64),sourceUrl:"https://mmbiz.qpic.cn/example.jpg",bytes:10}],failures:[]})),
  releaseSnapshotAssets:vi.fn(),
}));
const url = "https://mp.weixin.qq.com/s/example";
describe("公众号兼容网页采集", () => {
  it("保留初始隐藏的正文与懒加载图片，排除页面导航", async () => {
    vi.mocked(fetchHtml).mockResolvedValue({ finalUrl: url, contentType: "text/html", html:
      '<html><head><title></title></head><body><h1 id="activity-name">冬游黄山</h1><nav>分享收藏导航</nav><div id="js_content" style="visibility: hidden; opacity: 0"><p>黄山两次旅行的正文和路线。</p><img data-src="https://mmbiz.qpic.cn/example.jpg"><p>最后一段冬季注意事项。</p></div></body></html>' });
    const captureWebpage = vi.fn();
    const result = await extractContent("url", url, undefined, { captureWebpage });
    expect(captureWebpage).not.toHaveBeenCalled();
    expect(result.title).toBe("冬游黄山");
    expect(result.content).toContain("黄山两次旅行的正文和路线。");
    expect(result.content).toContain("最后一段冬季注意事项。");
    expect(result.content).toContain("local-image://wechat-");
    expect(result.webCapture.snapshot.html).toContain("local-image://wechat-");
    expect(result.content).not.toContain("分享收藏导航");
    expect(result.degradedReason).toBeUndefined();
  });
  it("验证页不作为成功正文保存", async () => {
    vi.mocked(fetchHtml).mockResolvedValue({ finalUrl: url, contentType: "text/html", html:
      '<html><body><h2>环境异常</h2><p>当前环境异常，完成验证后即可继续访问。</p></body></html>' });
    await expect(extractContent("url", url)).rejects.toThrow("未获取到公众号正文");
  });
  it("其他站点不将同名隐藏容器当作公众号正文", async () => {
    vi.mocked(fetchHtml).mockResolvedValue({ finalUrl: "https://example.com/article", contentType: "text/html", html:
      '<html><head><title>正常文章</title></head><body><article><p>这里是公开的文章正文，包含足够的中文内容。</p></article><div id="js_content" style="display:none">隐藏干扰内容</div></body></html>' });
    const result = await extractContent("url", "https://example.com/article");
    expect(result.content).not.toContain("隐藏干扰内容");
  });
});
