// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseHTML } from "linkedom";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { themeTestPage } from "./themed-reading-test-fixtures";

describe("主题页目录与布局保留", () => {
  it("保存页和离线导出均保留非对称布局、正文限宽并补齐可点击目录", () => {
    const page = themeTestPage({ assets: [] });
    page.design.html = '<main class="chapter"><nav><span>目录</span><a href="#first">生啤</a><a href="#second">继续阅读</a><a href="#missing">原文</a></nav><article class="body"><div id="first" data-source-block="b0"></div><div id="second" data-source-block="b1"></div></article></main>';
    page.design.css = '.chapter{display:grid;grid-template-columns:minmax(10rem,14rem) minmax(0,48rem)}.chapter .body{max-width:48rem}nav a{text-decoration:none}';
    for (const instance of [undefined, "reader"]) {
      const doc = parseHTML(themedReadingDocument(page, instance)).document;
      expect(doc.querySelectorAll("[data-reader-toc-link]")).toHaveLength(2);
      expect(doc.querySelector("nav").getAttribute("aria-label")).toBe("文章目录");
      expect(doc.querySelector("[data-reader-toc-label]").textContent).toBe("目录");
      expect(doc.querySelector("a:last-child").hasAttribute("href")).toBe(false);
      const css = doc.querySelector("style").textContent;
      expect(css).toContain("minmax(0,14rem) minmax(0,48rem)");
      expect(css).toContain("max-width:48rem");
      expect(css).not.toContain("grid-template-columns:repeat(auto-fit,minmax(min(100%,16rem),1fr))!important");
      expect(css).toContain("text-decoration:underline!important");
      expect(css).toContain(":focus-visible");
      expect(doc.querySelectorAll("script").length).toBe(instance ? 1 : 0);
      expect(doc.querySelectorAll("[data-source-block]")[1].textContent).toContain("完整正文与限定条件");
    }
  });
});
