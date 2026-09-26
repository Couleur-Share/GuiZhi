import { describe, expect, it } from "vitest";
import {
  assessStaticPage,
  staticPageLinks,
} from "../../../src/main/services/web-capture/web-static-route";
const prose = "这是包含代码、链接和表格说明的完整中文正文。".repeat(30);
const doc = (body: string, head = "") =>
  `<html><head>${head}</head><body>${body}</body></html>`;
const scripts = '<script src="/app.js"></script>';
describe("静态到 Electron 的结构判断", () => {
  it("短静态页和 JSON-LD 页面无需为了长度启动浏览器", () => {
    expect(assessStaticPage(doc("<h1>短文</h1><p>正文</p>"), 200).route).toBe(
      "static",
    );
    expect(
      assessStaticPage(
        doc('<p>正文</p><script type="application/ld+json">{}</script>'),
        200,
      ).route,
    ).toBe("static");
  });
  it("有脚本的服务端正文有明确区域时可走静态", () => {
    expect(
      assessStaticPage(
        doc(`<main><h1>正文</h1><p>${prose}</p></main>${scripts}`),
        200,
      ).route,
    ).toBe("static");
  });
  it.each([
    `<main></main>${scripts}`,
    `<main><p>摘要</p></main>${scripts}`,
    `<main><p>${prose}</p><p>Loading content...</p></main>${scripts}`,
    `<main><p>${prose}</p><div aria-busy="true"></div></main>`,
    `<main hidden><p>${prose}</p></main>`,
    `<div style="display: none"><main><p>${prose}</p></main></div>`,
    `<main aria-busy="true"><p>${prose}</p></main>`,
    `<main><p>${prose}</p><article-reader></article-reader></main>`,
    `<article>${prose}</article><article>${prose}</article>${scripts}`,
    `<div>${prose}</div>${scripts}`,
    `<main><p>${prose}</p><pre><a href="#line1"></a>value = 1</pre></main>`,
  ])("不把部分正文或未解析组件判作完整：%s", (body) => {
    expect(assessStaticPage(doc(body), 200).route).toBe("render");
  });
  it("不把代码中的 Loading 字符串视为页面占位", () => {
    expect(
      assessStaticPage(
        doc(
          `<main><p>${prose}</p><pre><span>Loading...</span></pre></main>${scripts}`,
        ),
        200,
      ).route,
    ).toBe("static");
  });
  it("HTTP 错误、验证码和登录不会触发自动重试", () => {
    for (const status of [401, 403, 404, 429, 500])
      expect(assessStaticPage("", status).route).toBe("terminal");
    expect(
      assessStaticPage(doc(scripts, "<title>Just a moment...</title>"), 200)
        .route,
    ).toBe("terminal");
    expect(
      assessStaticPage(
        doc('<input type="password">', "<title>登录</title>"),
        200,
      ).route,
    ).toBe("terminal");
  });
  it("延迟跳转需要浏览器，静态链接解析为绝对 HTTP 地址", () => {
    expect(
      assessStaticPage(
        doc("<p>跳转</p>", '<meta http-equiv="refresh" content="1;url=/next">'),
        200,
      ).route,
    ).toBe("render");
    expect(
      staticPageLinks(
        doc(
          '<a href="../page#part">正文</a><a href="javascript:alert(1)">按钮</a>',
        ),
        "https://example.com/docs/start",
      ),
    ).toEqual(["https://example.com/page#part"]);
  });
});
