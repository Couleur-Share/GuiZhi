// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseHTML } from "linkedom";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { themeTestPage } from "./themed-reading-test-fixtures";

describe("内容关系阅读组件", () => {
  it("正文完整回填，目录默认收起，离线仍然无脚本", () => {
    const page = themeTestPage({ assets: [] });
    page.design.html = '<article class="reading-page reading-discussion"><header><div data-source-block="b0"></div></header><details class="reading-contents"><summary>文章目录</summary><nav><a href="#content">继续阅读</a></nav></details><section id="content" class="reading-checklist"><div data-source-block="b1"></div></section></article>';
    const doc = parseHTML(themedReadingDocument(page)).document;
    expect(doc.querySelector("details").hasAttribute("open")).toBe(false);
    expect([...doc.querySelectorAll("[data-source-block]")].map(node => node.textContent)).toEqual(page.source.blocks.map(block => block.text));
    expect(doc.querySelector("style").textContent).toContain(".reading-page .reading-checklist");
    expect(doc.querySelector("script")).toBeNull();
    expect(doc.querySelector("a").getAttribute("href")).toBe("#content");
  });

  it("模型误把正文放进辅助目录时强制展开，原有自由设计不注入组件样式", () => {
    const page = themeTestPage();
    expect(parseHTML(themedReadingDocument(page)).document.querySelector("style").textContent).not.toContain("--reader-panel-surface");
    page.design.html = '<article class="reading-page"><details class="reading-contents"><summary>文章目录</summary><div data-source-block="b0"></div><div data-source-block="b1"></div></details></article>';
    expect(parseHTML(themedReadingDocument(page)).document.querySelector("details").hasAttribute("open")).toBe(true);
  });
});
