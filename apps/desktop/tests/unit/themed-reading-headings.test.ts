// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseHTML } from "linkedom";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { buildThemedReadingSource } from "../../src/main/services/themed-reading/content";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { themeTestPage } from "./themed-reading-test-fixtures";

async function rendered(content: string) {
  const source = await buildThemedReadingSource({ title: "测试文章", content, itemType: "note" } as KnowledgeItem, "body");
  const original = JSON.stringify(source);
  const page = themeTestPage({ source, assets: [], design: { direction: "主题", html: source.blocks.map((block) => `<div data-source-block="${block.id}"></div>`).join(""), css: "", assets: [] } });
  const html = themedReadingDocument(page, "heading-fixture");
  expect(JSON.stringify(source)).toBe(original);
  return { source, document: parseHTML(html).document };
}

describe("主题阅读编号章节的目录语义", () => {
  it("将真实啤酒文章的四个整段加粗章节提升为目录可识别标题", async () => {
    const titles = ["一、生啤与熟啤：杀不杀菌", "二、鲜啤：状态而非工艺", "三、精酿：生产规模与理念", "四、三个选购判断问题"];
    const { source, document } = await rendered("## 视频总结\n\n" + titles.map((title) => `**${title}**\n\n正文限定条件。`).join("\n\n"));
    expect([...document.querySelectorAll("h2")].map((heading) => heading.textContent)).toEqual(["视频总结", ...titles]);
    const texts = [...document.querySelectorAll("[data-source-block]")].map((block) => block.textContent.trim());
    expect(texts).toEqual(source.blocks.map((block) => block.text.trim()));
  });

  it("不把普通强调、带后续正文、列表项和过长句子误当章节", async () => {
    const { document } = await rendered(`**核心概念**\n\n**一、重点**后面仍是正文。\n\n- **二、列表项**\n\n**三、${"这是完整的正文句子".repeat(15)}**`);
    expect(document.querySelectorAll("h2")).toHaveLength(0);
    expect(document.querySelectorAll("li")).toHaveLength(1);
  });

  it("保留编号标题中的原链接实体字面量，不影响源正文", async () => {
    const { document } = await rendered("**一、[来源标题](https://example.com/?x=1&amp;copy;=2)**\n\n最后一句。");
    expect(document.querySelector("h2").textContent).toBe("一、来源标题");
    expect(document.querySelector("h2 a").getAttribute("href")).toBe("https://example.com/?x=1&copy;=2");
    expect(document.body.textContent).toContain("最后一句。");
  });

  it("支持明确的中文括号/章节和阿拉伯章节编号，拒绝小数开头的重点句", async () => {
    const { document } = await rendered("**（一）保存方式**\n\n**第二章 风味**\n\n**3. 配料**\n\n**1.5 升是容量**");
    expect([...document.querySelectorAll("h2")].map((heading) => heading.textContent)).toEqual(["（一）保存方式", "第二章 风味", "3. 配料"]);
  });
});
