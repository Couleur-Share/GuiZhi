import { describe, expect, it } from "vitest";

import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";

const FULL_CONTENT = [
  "> 平台：哔哩哔哩 · 作者：UP主 · 时长：12:52",
  "> 简介：文档在公众号【白玩 · 在路上】",
  "> 原标题：什么是Web核心指标？2026前端必会的 LCP/INP/CLS 全解析",
  "> 文字稿来源：发布者字幕（zh-CN）",
  "",
  "## 视频总结",
  "",
  "本视频讲解了核心内容。",
].join("\n");

describe("parseVideoMetaBlock", () => {
  it("解析平台/作者/时长/简介/原标题/文字稿来源，正文剥离元数据块", () => {
    const meta = parseVideoMetaBlock(FULL_CONTENT);
    expect(meta).not.toBeNull();
    expect(meta!.platform).toBe("哔哩哔哩");
    expect(meta!.author).toBe("UP主");
    expect(meta!.duration).toBe("12:52");
    // 简介整行取值，内容中的 · 不参与字段拆分
    expect(meta!.description).toBe("文档在公众号【白玩 · 在路上】");
    expect(meta!.originalTitle).toBe(
      "什么是Web核心指标？2026前端必会的 LCP/INP/CLS 全解析",
    );
    expect(meta!.transcriptSource).toBe("发布者字幕（zh-CN）");
    expect(meta!.body.startsWith("## 视频总结")).toBe(true);
    expect(meta!.body).not.toContain("平台：");
    expect(meta!.body).not.toContain("简介：");
  });

  it("旧格式条目（简介与来源在正文中）仍原样保留正文", () => {
    const legacy = [
      "> 平台：哔哩哔哩 · 作者：UP主 · 时长：12:52",
      "",
      "这是简介",
      "",
      "---",
      "",
      "来源：<https://example.com/v>",
    ].join("\n");
    const meta = parseVideoMetaBlock(legacy);
    expect(meta).not.toBeNull();
    expect(meta!.description).toBeUndefined();
    expect(meta!.body).toContain("这是简介");
    expect(meta!.body).toContain("来源：<https://example.com/v>");
  });

  it("没有原标题行时 originalTitle 为空，部分字段缺失可容忍", () => {
    const meta = parseVideoMetaBlock("> 平台：YouTube · 时长：5:05\n\n正文");
    expect(meta).not.toBeNull();
    expect(meta!.platform).toBe("YouTube");
    expect(meta!.author).toBeUndefined();
    expect(meta!.duration).toBe("5:05");
    expect(meta!.originalTitle).toBeUndefined();
    expect(meta!.body).toBe("正文");
  });

  it("首行不是元数据块时返回 null（普通引用/普通正文不受影响）", () => {
    expect(parseVideoMetaBlock("普通正文")).toBeNull();
    expect(parseVideoMetaBlock("> 这是一段普通引用\n\n正文")).toBeNull();
    expect(parseVideoMetaBlock("")).toBeNull();
  });
});
