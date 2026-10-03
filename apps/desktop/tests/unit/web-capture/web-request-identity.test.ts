import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  DOCUMENT_ACCEPT,
  WEB_IDENTITY_ENV,
  webDocumentHeaders,
  webTextHeaders,
} from "../../../src/main/services/web-capture/web-request-identity";

const UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) GuiZhi/1.2.3 Chrome/130.0.6723.191 Electron/33.4.11 Safari/537.36";
const electron = vi.hoisted(() => ({
  app: { userAgentFallback: "" } as { userAgentFallback: string } | undefined,
}));
vi.mock("electron", () => electron);
beforeEach(() => {
  electron.app = { userAgentFallback: UA };
});

describe("静态抓取与文本请求的请求身份", () => {
  it("文档请求带渲染窗口同一个 User-Agent 与文档导航 Accept", async () => {
    const headers = await webDocumentHeaders({});
    expect(headers).toEqual({ "user-agent": UA, accept: DOCUMENT_ACCEPT });
    // 自报家门：保留 GuiZhi 记号，不冒用任何爬虫身份。
    expect(headers["user-agent"]).toContain("GuiZhi/");
    expect(headers["user-agent"]).not.toMatch(/bot|spider|crawl/i);
  });

  it("不带 Accept-Language（渲染窗口本来就没有）与 Upgrade-Insecure-Requests（能力声明，有意不发）", async () => {
    const headers = await webDocumentHeaders({});
    expect(Object.keys(headers).sort()).toEqual(["accept", "user-agent"]);
  });

  it("robots.txt 与 sitemap 这类文本请求只带 User-Agent", async () => {
    expect(await webTextHeaders({})).toEqual({ "user-agent": UA });
  });

  it("GUIZHI_WEB_IDENTITY=none 恢复不带身份；其他取值按默认处理", async () => {
    expect(await webDocumentHeaders({ [WEB_IDENTITY_ENV]: "none" })).toEqual({});
    expect(await webTextHeaders({ [WEB_IDENTITY_ENV]: "none" })).toEqual({});
    expect(
      await webDocumentHeaders({ [WEB_IDENTITY_ENV]: "browser" }),
    ).toHaveProperty("user-agent", UA);
  });

  it("默认读取进程环境变量", async () => {
    vi.stubEnv(WEB_IDENTITY_ENV, "none");
    try {
      expect(await webDocumentHeaders()).toEqual({});
    } finally {
      vi.unstubAllEnvs();
    }
    expect(await webDocumentHeaders()).toHaveProperty("user-agent", UA);
  });

  it("取不到 User-Agent（不在 Electron 里或值为空）时不带身份，也不抛错", async () => {
    electron.app = { userAgentFallback: "" };
    expect(await webDocumentHeaders({})).toEqual({});
    expect(await webTextHeaders({})).toEqual({});
    electron.app = undefined;
    expect(await webDocumentHeaders({})).toEqual({});
    expect(await webTextHeaders({})).toEqual({});
  });
});
