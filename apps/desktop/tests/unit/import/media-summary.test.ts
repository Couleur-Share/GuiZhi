import { describe, expect, it, vi } from "vitest";
import type { AIClientConfig } from "@guizhi/core";

import {
  appendOriginalTitleNote,
  hasMediaSummarySection,
  mediaSummaryHeading,
  upsertMediaSummarySection,
} from "@guizhi/shared/utils/media-summary";
import {
  generateMediaSummary,
  sanitizeMediaSummary,
} from "../../../src/main/services/media/media-summary";

const CONFIG: AIClientConfig = {
  provider: "openai",
  apiProtocol: "openai",
  apiKey: "sk-test",
  apiUrl: "https://api.openai.com",
  model: "main-model",
};

/** 在线视频条目正文（buildVideoContent 现行形态）：仅元数据引用块 */
const VIDEO_CONTENT = [
  "> 平台：哔哩哔哩 · 作者：UP主 · 时长：5:05",
  "> 简介：这是简介",
].join("\n");

/** 旧版正文形态（存量条目）：简介与来源仍是正文段落 */
const LEGACY_VIDEO_CONTENT = [
  "> 平台：哔哩哔哩 · 作者：UP主 · 时长：5:05",
  "",
  "这是简介",
  "",
  "---",
  "",
  "来源：<https://example.com/v>",
].join("\n");

describe("mediaSummaryHeading / hasMediaSummarySection", () => {
  it("按条目类型选择小节标题", () => {
    expect(mediaSummaryHeading("video")).toBe("## 视频总结");
    expect(mediaSummaryHeading("audio")).toBe("## 音频总结");
  });

  it("检测正文中是否已有总结小节", () => {
    expect(hasMediaSummarySection(VIDEO_CONTENT)).toBe(false);
    const withSummary = upsertMediaSummarySection(
      VIDEO_CONTENT,
      "## 视频总结",
      "- 要点",
    );
    expect(hasMediaSummarySection(withSummary)).toBe(true);
  });
});

describe("upsertMediaSummarySection", () => {
  it("插入到元数据引用块之后；正文再无其他内容时不补 --- 锚点", () => {
    const summary = "本视频讲解了核心内容。\n\n**一、要点**\n- 细节";
    const result = upsertMediaSummarySection(
      VIDEO_CONTENT,
      "## 视频总结",
      summary,
    );
    const metaIdx = result.indexOf("> 平台：");
    const headingIdx = result.indexOf("## 视频总结");
    const summaryIdx = result.indexOf("本视频讲解了核心内容。");
    expect(metaIdx).toBe(0);
    expect(headingIdx).toBeGreaterThan(result.indexOf("> 简介：这是简介"));
    expect(summaryIdx).toBeGreaterThan(headingIdx);
    expect(result).not.toContain("---");
  });

  it("旧格式正文：插入到元数据块与旧段落之间，并补 --- 结束锚点", () => {
    const summary = "本视频讲解了核心内容。";
    const result = upsertMediaSummarySection(
      LEGACY_VIDEO_CONTENT,
      "## 视频总结",
      summary,
    );
    const summaryIdx = result.indexOf("本视频讲解了核心内容。");
    const descIdx = result.indexOf("这是简介");
    const sourceIdx = result.indexOf("来源：<");
    expect(result.indexOf("## 视频总结")).toBeGreaterThan(
      result.indexOf("> 平台："),
    );
    expect(descIdx).toBeGreaterThan(summaryIdx);
    expect(sourceIdx).toBeGreaterThan(descIdx);
    // 小节后有确定性的结束锚点（--- 分隔线），后续段落不会被并入小节
    expect(result.slice(summaryIdx, descIdx)).toContain("---");
  });

  it("已有总结小节时原位替换，其余内容不动", () => {
    const first = upsertMediaSummarySection(
      LEGACY_VIDEO_CONTENT,
      "## 视频总结",
      "旧总结第一行\n- 旧要点",
    );
    const second = upsertMediaSummarySection(first, "## 视频总结", "新总结");
    expect(second).not.toContain("旧总结第一行");
    expect(second).not.toContain("旧要点");
    expect(second).toContain("新总结");
    expect(second.match(/## 视频总结/g)).toHaveLength(1);
    expect(second).toContain("这是简介");
    expect(second).toContain("来源：<https://example.com/v>");
  });

  it("跨类型标题也按同一小节替换（音频标题替换视频标题）", () => {
    const first = upsertMediaSummarySection(
      VIDEO_CONTENT,
      "## 视频总结",
      "旧总结",
    );
    const second = upsertMediaSummarySection(first, "## 音频总结", "新总结");
    expect(second).not.toContain("## 视频总结");
    expect(second.match(/## 音频总结/g)).toHaveLength(1);
    expect(second).toContain("新总结");
  });

  it("本地媒体条目：插入到资产引用段之后", () => {
    const content = [
      "[a.mp4](local-video://abc.mp4)",
      "",
      "> 视频文件已导入本地资产库，可在详情页播放。",
    ].join("\n");
    const result = upsertMediaSummarySection(content, "## 视频总结", "- 要点");
    expect(result.startsWith("[a.mp4](local-video://abc.mp4)")).toBe(true);
    expect(result.indexOf("## 视频总结")).toBeLessThan(
      result.indexOf("> 视频文件已导入"),
    );
  });

  it("无元数据头的正文置顶插入；空正文只保留总结小节", () => {
    const result = upsertMediaSummarySection("普通正文", "## 视频总结", "- 要点");
    expect(result.startsWith("## 视频总结")).toBe(true);
    expect(result.endsWith("普通正文")).toBe(true);

    expect(upsertMediaSummarySection("", "## 视频总结", "- 要点")).toBe(
      "## 视频总结\n\n- 要点",
    );
  });
});

describe("appendOriginalTitleNote", () => {
  it("追加到开头元数据引用块的末尾", () => {
    const result = appendOriginalTitleNote(VIDEO_CONTENT, "平台原标题");
    const lines = result.split("\n");
    expect(lines[0]).toContain("> 平台：");
    expect(lines[1]).toBe("> 简介：这是简介");
    expect(lines[2]).toBe("> 原标题：平台原标题");
  });

  it("已有原标题记录时不重复追加", () => {
    const once = appendOriginalTitleNote(VIDEO_CONTENT, "第一次标题");
    const twice = appendOriginalTitleNote(once, "第二次标题");
    expect(twice).toBe(once);
    expect(twice.match(/> 原标题：/g)).toHaveLength(1);
  });

  it("开头不是引用块或原标题为空时原样返回", () => {
    expect(appendOriginalTitleNote("普通正文", "标题")).toBe("普通正文");
    expect(appendOriginalTitleNote(VIDEO_CONTENT, "  ")).toBe(VIDEO_CONTENT);
  });
});

describe("sanitizeMediaSummary", () => {
  it("剥离包裹的代码围栏", () => {
    expect(sanitizeMediaSummary("```markdown\n**内容**\n```")).toBe("**内容**");
  });

  it("# 级标题降级为加粗行，删除 --- 分隔线", () => {
    const raw = "## 一、简介\n- 要点\n\n---\n\n### 二、细节\n- 更多";
    expect(sanitizeMediaSummary(raw)).toBe(
      "**一、简介**\n- 要点\n\n**二、细节**\n- 更多",
    );
  });

  it("丢弃冗余的「视频总结」总标题行", () => {
    expect(sanitizeMediaSummary("**视频总结**\n\n正文第一段")).toBe(
      "正文第一段",
    );
    expect(sanitizeMediaSummary("# 视频总结\n正文")).toBe("正文");
  });
});

describe("generateMediaSummary", () => {
  it("短文字稿单发：标题/简介/文字稿进入 user 消息，拆出 AI 标题", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        _messages: { role: string; content: string }[],
      ) => ({ content: "标题：《AI 拟定标题》\n\n**一、要点**\n- 内容" }),
    );
    const result = await generateMediaSummary(
      { title: "测试视频", context: "简介文字", transcript: "口播原文。" },
      CONFIG,
      { chat: chat as never },
    );
    expect(chat).toHaveBeenCalledTimes(1);
    const [, messages] = chat.mock.calls[0];
    expect(messages[0].content).toContain("视频内容总结助手");
    expect(messages[1].content).toContain("标题：《测试视频》");
    expect(messages[1].content).toContain("简介：简介文字");
    expect(messages[1].content).toContain("口播原文。");
    // 书名号被归一化剥掉
    expect(result.title).toBe("AI 拟定标题");
    expect(result.summary).toBe("**一、要点**\n- 内容");
  });

  it("模型未按协议输出标题行 → title 为 null，整段视为总结", async () => {
    const chat = vi.fn(async () => ({ content: "**一、要点**\n- 内容" }));
    const result = await generateMediaSummary(
      { title: "测试视频", transcript: "口播原文。" },
      CONFIG,
      { chat: chat as never },
    );
    expect(result.title).toBeNull();
    expect(result.summary).toBe("**一、要点**\n- 内容");
  });

  it("长文字稿 map-reduce：逐片段提笔记后综合，标题来自综合输出", async () => {
    // 101 字一句 × 250 句 ≈ 2.5 万字 → 3 块
    const transcript = `${"内容".repeat(50)}。`.repeat(250);
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => {
        const isMap = messages[0].content.includes("文字稿的一个片段");
        return {
          content: isMap
            ? "**片段小节**\n- 片段要点"
            : "标题：综合标题\n\n**综合总结**\n- 最终要点",
        };
      },
    );
    const result = await generateMediaSummary(
      { title: "长视频", transcript },
      CONFIG,
      { chat: chat as never },
    );
    expect(chat).toHaveBeenCalledTimes(4);
    const reduceUser = chat.mock.calls[3][1][1].content;
    expect(reduceUser).toContain("【片段 1 笔记】");
    expect(reduceUser).toContain("【片段 3 笔记】");
    expect(result.title).toBe("综合标题");
    expect(result.summary).toBe("**综合总结**\n- 最终要点");
  });

  it("空文字稿或模型空输出时抛错", async () => {
    await expect(
      generateMediaSummary({ title: "t", transcript: "  " }, CONFIG),
    ).rejects.toThrow("文字稿为空");

    const emptyChat = vi.fn(async () => ({ content: "  " }));
    await expect(
      generateMediaSummary({ title: "t", transcript: "有内容" }, CONFIG, {
        chat: emptyChat as never,
      }),
    ).rejects.toThrow("输出为空");

    // 输出只有标题行没有正文：清洗后为空同样视为失败
    const titleOnlyChat = vi.fn(async () => ({ content: "标题：只有标题" }));
    await expect(
      generateMediaSummary({ title: "t", transcript: "有内容" }, CONFIG, {
        chat: titleOnlyChat as never,
      }),
    ).rejects.toThrow("有效的内容总结");
  });
});
