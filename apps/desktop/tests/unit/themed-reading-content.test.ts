// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { buildThemedReadingSource, themedReadingContent } from "../../src/main/services/themed-reading/content";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { themeTestPage } from "./themed-reading-test-fixtures";
import { parseHTML } from "linkedom";

const item = (content: string, itemType: KnowledgeItem["itemType"] = "note"): KnowledgeItem => ({
  id: "item-1", title: "啤酒知识", content, itemType, status: "active", isFavorite: false, isPinned: false,
  createdAt: 1, updatedAt: 1, tags: [],
});

describe("主题页正文快照", () => {
  it("完整解析中文强调、GFM表格、引用链接、嵌套列表、代码和原始折叠标签", async () => {
    const source = await buildThemedReadingSource(item([
      "# 生啤与熟啤", "", "中文**重点**，保质期300~500天、3~4周。[原文][ref]", "",
      "| 类型 | 温度 |", "| --- | --- |", "| 熟啤 | 60–70°C |", "",
      "- 外层", "  - 内层", "", "```ts", "const exact = '<script>alert(1)</script>';", "```", "",
      "<details><summary>展开说明</summary>", "", "限定条件不能遗漏。", "", "</details>", "",
      "[ref]: https://example.com/source", "", "![酒花](local-image://flower.png)",
    ].join("\n")), "body");
    const rendered = source.blocks.map((block) => block.html).join("");
    expect(rendered).toContain("<strong>重点</strong>");
    expect(rendered).toContain("300~500");
    expect(rendered).not.toContain("<del>");
    expect(rendered).toContain("<table>");
    expect(rendered).toContain("href=\"https://example.com/source\"");
    expect(rendered).toContain("local-image://flower.png");
    expect(rendered).toContain("&lt;script&gt;");
    expect(rendered).toContain("<details>");
    expect(source.blocks.find((block) => block.html.startsWith("<details>"))?.text).toContain("限定条件不能遗漏");
  });

  it("长文尾部和跨块引用定义不被截断", async () => {
    const content = Array.from({ length: 300 }, (_, index) => `## 第${index}节\n\n${"正文信息。".repeat(100)}[来源][link]`).join("\n\n") + "\n\n最后一句必须保留。\n\n[link]: https://example.com/reference";
    const source = await buildThemedReadingSource(item(content), "body");
    expect(source.content).toBe(content);
    expect(source.blocks.at(-1).text).toContain("最后一句必须保留");
    expect(source.blocks.map((block) => block.html).join("").match(/https:\/\/example.com\/reference/g)).toHaveLength(300);
  });

  it("内容指纹不受标签收藏更新时间影响，但正文、标题和来源改变时更新", async () => {
    const original = item("固定正文");
    const first = await buildThemedReadingSource(original, "body");
    const updated = await buildThemedReadingSource({ ...original, isFavorite: true, updatedAt: 900, tags: [{ id: "tag", name: "新标签", colorKey: "red", createdAt: 1, updatedAt: 1 }] }, "body");
    expect(updated.fingerprint).toBe(first.fingerprint);
    for (const patch of [{ content: "新正文" }, { title: "新标题" }, { sourceUri: "https://example.com" }]) {
      expect((await buildThemedReadingSource({ ...original, ...patch }, "body")).fingerprint).not.toBe(first.fingerprint);
    }
  });

  it("按当前来源提取论坛主楼、讨论总结与图片文案", () => {
    const forum = item("## 讨论总结\n\n总结限定条件\n\n## 正文\n\n主楼正文\n\n## 讨论（1 条）\n\n### 1 楼 · 用户\n\n不应进入正文", "forum");
    expect(themedReadingContent(forum, "body")).toBe("主楼正文");
    expect(themedReadingContent(forum, "summary")).toBe("总结限定条件");
    expect(themedReadingContent(item("图片文案\n\n![图片](local-image://one.png)\n\n## 图中文字\n\nOCR文字", "image"), "body")).toBe("图片文案");
    expect(() => themedReadingContent(item("普通正文"), "summary")).toThrow("讨论总结");
  });

  it.each(["video", "audio"] as const)("%s冻结的是当前正文总结，元数据和文字稿不混入", async (type) => {
    const media = item("> 平台：抖音 · 作者：测试作者 · 时长：4:44\n> 原标题：来源标题\n\n## 视频总结\n\n总结正文，包含限定条件。", type);
    media.transcript = "这个文字稿不进入当前正文主题页";
    media.summary = "这个摘要字段也不是正在阅读的正文";
    const source = await buildThemedReadingSource(media, "body");
    expect(source.content).toBe("## 视频总结\n\n总结正文，包含限定条件。");
    expect(source.blocks.map((block) => block.text).join("")).not.toMatch(/测试作者|来源标题|文字稿|摘要字段/);
  });

  it("公众号标准正文和普通笔记保留普通引用，不把它们误当媒体元数据", async () => {
    const webpage = item("> 平台：这里是文章中的引文\n\n公众号正文尾部", "webpage");
    webpage.sourceUri = "https://mp.weixin.qq.com/s/article";
    const source = await buildThemedReadingSource(webpage, "body");
    expect(source.content).toBe(webpage.content);
    expect(source.sourceUri).toBe(webpage.sourceUri);
    expect(source.blocks.map((block) => block.html).join("")).toContain("<blockquote>");
  });

  it("组装时再次清理仍保留列表次序、完整表格、代码字面量、链接查询参数和文字尾部", async () => {
    const content = "1. 第一步\n2. 第二步\n\n| 列A | 列B |\n| --- | --- |\n| 值一 | 值二 |\n\n```html\n<script>这是代码，不是脚本</script>\n```\n\n[带参数原文](https://example.com/article?a=1&b=2)\n\n[实体字面量](https://example.com/article?a=1&amp;copy;=2)\n\n最后一个限定条件，绝不能遗漏。";
    const source = await buildThemedReadingSource(item(content), "body");
    const page = themeTestPage({ source, assets: [], design: { direction: "测试", html: source.blocks.map((block) => `<div data-source-block="${block.id}"></div>`).join(""), css: "", assets: [] } });
    const html = themedReadingDocument(page);
    expect(html.indexOf("第一步")).toBeLessThan(html.indexOf("第二步"));
    expect(html).toContain("值一");
    expect(html).toContain("值二");
    expect(html).toContain("&lt;script&gt;这是代码，不是脚本&lt;/script&gt;");
    expect(html).toContain("https://example.com/article?a=1&amp;b=2");
    expect(parseHTML(html).document.querySelectorAll("a")[1].getAttribute("href")).toBe("https://example.com/article?a=1&copy;=2");
    expect(html).toContain("最后一个限定条件，绝不能遗漏。");
    expect(html).not.toContain("<script>");
    expect(source.content).toBe(content);
  });
});
