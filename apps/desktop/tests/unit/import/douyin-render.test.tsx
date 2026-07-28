import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";

// douyin → safe-fetch → network-proxy 引用 electron，单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { buildImageNoteEntry } from "../../../src/main/services/import/image-note-entry";
import {
  douyinImageNoteSource,
  type DouyinAweme,
} from "../../../src/main/services/import/douyin";
import { MarkdownBody } from "../../../src/renderer/components/library/MarkdownPreview";

/**
 * 主进程生成的正文最终要经渲染进程显示，两边的契约只有跑一遍真渲染器才能验。
 * 这里把 Markdown 交给条目详情页用的那个组件，再还原成用户看到的一行一行。
 */
function renderedBlocks(markdown: string): string[] {
  const html = renderToStaticMarkup(<MarkdownBody content={markdown} />);
  return html
    .replace(/<\/(p|li|h[1-6]|blockquote|pre|div)>/g, "\u0000")
    .replace(/<br\s*\/?>/g, "\u0000")
    .replace(/<[^>]+>/g, "")
    .split("\u0000")
    .map((block) =>
      block
        .replace(/&quot;/g, '"')
        .replace(/&#x27;/g, "'")
        .replace(/&lt;/g, "<")
        .replace(/&gt;/g, ">")
        .replace(/&amp;/g, "&")
        // 浏览器会把块内换行折叠成空格，比对前先对齐这个行为
        .replace(/\s+/g, " ")
        .trim(),
    )
    .filter(Boolean);
}

const DESC = [
  "我用这套方法做了一个生产级 RAG 系统。",
  "不用 LangChain，从零开始手写了一个混合检索 RAG 系统。",
  "1. 评测先行。 没有评测数据，所有优化都是盲猜。",
  "2. 从简单开始。 先跑通最基础的向量检索。",
  "写在最后",
  "如果你也在做 RAG 系统，欢迎交流。#RAG #AI编程",
].join("\n");

const AWEME: DouyinAweme = {
  awemeId: "7648655441894886691",
  kind: "note",
  title: "我用这套方法做了一个生产级 RAG 系统。",
  description: DESC,
  author: "mHe",
  durationSeconds: 0,
  playUrl: null,
  imageMirrors: [["https://p1.douyinpic.com/a.webp"]],
  webpageUrl: "https://www.douyin.com/note/7648655441894886691",
};

async function noteContent(): Promise<string> {
  const entry = await buildImageNoteEntry(douyinImageNoteSource(AWEME), {
    downloadImage: async () => ({ dir: "", filePath: "/tmp/image.webp" }),
    saveAsset: async () => "asset.webp",
    getOcrConfig: () => null,
    // 不读真实 ai-config.json，单测不该发出网络请求
    getTitleConfig: () => null,
  });
  return entry.content;
}

describe("正文里的本地资产图", () => {
  /**
   * 这条路上有两道闸门：rehype-sanitize 的协议白名单，以及 react-markdown 自带的
   * urlTransform。任何一道漏配，图片都会变成 src 为空的破图，且不报错。
   */
  it("local-image 地址不被剥掉，并渲染成可点击放大的缩略图", () => {
    const html = renderToStaticMarkup(
      <MarkdownBody content="![图 1](local-image://asset.webp)" />,
    );
    expect(html).toContain('src="local-image://asset.webp"');
    expect(html).toContain("cursor-zoom-in");
  });

  it("非白名单协议仍然被挡住", () => {
    const html = renderToStaticMarkup(
      <MarkdownBody content="![x](javascript:alert(1))" />,
    );
    expect(html).not.toContain("javascript:");
  });
});

describe("抖音图文正文的渲染保真", () => {
  it("原文每一行都独立成行，序号不被列表语义吃掉", async () => {
    const blocks = renderedBlocks(await noteContent());
    for (const line of DESC.split("\n")) {
      expect(blocks).toContain(line);
    }
    expect(blocks[0]).toContain("平台：抖音 · 作者：mHe · 图文 1 张");
  });

  it("对照：纯文本原样当 Markdown 会被渲染坏（本用例锁住这个回归）", () => {
    const blocks = renderedBlocks(DESC);
    // 单换行并成一段
    expect(blocks).toContain(
      "我用这套方法做了一个生产级 RAG 系统。 不用 LangChain，从零开始手写了一个混合检索 RAG 系统。",
    );
    // 序号被有序列表语义吞掉（列表符号还被 .prose 的样式隐藏了）
    expect(blocks).not.toContain("1. 评测先行。 没有评测数据，所有优化都是盲猜。");
    // 列表后面没有空行的段落被吸进最后一个列表项
    expect(blocks).not.toContain("写在最后");
  });
});
