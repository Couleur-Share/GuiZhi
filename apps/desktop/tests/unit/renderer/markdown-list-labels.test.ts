import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import { describe, expect, it } from "vitest";
import { remarkGfmPlugins } from "../../../src/renderer/utils/remark-gfm-plugins";

const render = (content: string) =>
  renderToStaticMarkup(
    createElement(ReactMarkdown, {
      remarkPlugins: remarkGfmPlugins,
      children: content,
    }),
  );

describe("中文 Markdown 强调", () => {
  it("兼容已保存总结的共识和分歧，保留后续链接和强调", () => {
    const html = render(
      "- **共识：**若只看推荐集中度，[作品](https://example.com)最突出。\n- **分歧：**有人认为悲剧蕴含力量，*也有异议*。",
    );
    expect(html).toContain("<strong>共识：</strong>若只看推荐集中度");
    expect(html).toContain("<strong>分歧：</strong>有人认为悲剧蕴含力量");
    expect(html).toContain('<a href="https://example.com">作品</a>');
    expect(html).toContain("<em>也有异议</em>");
    expect(html).not.toContain("**");
  });

  it.each([
    "- **共识：** 正文",
    "- **共识**：正文",
    "1. **共识：**正文",
    "- 父项\n  - **共识：**正文",
  ])("保留标准写法并支持嵌套和有序列表：%s", (source) => {
    expect(render(source)).toContain("<strong>共识");
    expect(render(source)).not.toContain("**");
  });

  it.each([
    "- `**共识：**正文`",
    "```md\n- **共识：**正文\n```",
    "- \\*\\*共识：\\*\\*正文",
    "- **共识：\\**正文",
    "- **共识： **正文",
  ])("保留代码、转义及无效空格边界：%s", (source) => {
    expect(render(source)).not.toContain("<strong>");
  });

  it.each([
    ["普通段落 **共识：**正文", "共识："],
    ["- 说明 **共识：**正文", "共识："],
    ["这是**“已知问题”**的说明。", "“已知问题”"],
    ["建议**工具（推荐）**适用。", "工具（推荐）"],
    ["**这个结论。**后续解释", "这个结论。"],
  ])("支持段落和中文标点：%s", (source, label) => {
    expect(render(source)).toContain(`<strong>${label}</strong>`);
    expect(render(source)).not.toContain("**");
  });

  it("保留 GFM 表格、双波浪删除线及单波浪范围", () => {
    const html = render(
      "| 项目 | 范围 |\n| --- | --- |\n| ~~旧值~~ | 300~500、3~4周 |",
    );
    expect(html).toContain("<table>");
    expect(html).toContain("<del>旧值</del>");
    expect(html).toContain("300~500、3~4周");
  });
});
