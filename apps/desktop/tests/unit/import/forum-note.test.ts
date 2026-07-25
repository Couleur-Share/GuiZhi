import { describe, expect, it } from "vitest";
import {
  parseForumReplies,
  splitForumNoteSections,
  upsertForumSummarySection,
} from "@guizhi/shared/utils/forum-note";

/** 采集端 buildForumEntry 的现行输出形态 */
const FORUM_CONTENT = [
  "> 平台：V2EX · 作者：yitwotre · 节点：问与答 · 107 条回复",
  "> 发布：2026-07-16",
  "",
  "## 讨论总结",
  "",
  "**ZeroTier**",
  "- 多人推荐，延迟 4ms",
  "",
  "## 正文",
  "",
  "家里的设备有群辉 nas。",
  "",
  "## 讨论（2 条）",
  "",
  "**1 楼 · wowo243**",
  "",
  "zerotier 试试",
  "",
  "**2 楼 · onlychen**",
  "",
  "搞个网关 sslvpn 拨回去。",
].join("\n");

describe("splitForumNoteSections", () => {
  it("拆出总结 / 主楼 / 回复三段，且都不含元数据引用块", () => {
    const sections = splitForumNoteSections(FORUM_CONTENT);

    expect(sections.summary).toBe("**ZeroTier**\n- 多人推荐，延迟 4ms");
    expect(sections.body).toBe("家里的设备有群辉 nas。");
    expect(sections.replies).toContain("**1 楼 · wowo243**");
    expect(sections.replies).toContain("搞个网关 sslvpn 拨回去。");

    for (const section of Object.values(sections)) {
      expect(section).not.toContain("平台：V2EX");
      expect(section).not.toContain("发布：2026-07-16");
    }
  });

  it("「## 讨论总结」不会被当成回复小节的标题", () => {
    const sections = splitForumNoteSections(FORUM_CONTENT);
    expect(sections.summary).not.toContain("楼 ·");
    expect(sections.replies).not.toContain("ZeroTier");
  });

  it("未生成总结时，状态注记归入主楼段而不是丢掉", () => {
    const content = [
      "> 平台：V2EX · 作者：someone · 1 条回复",
      "> 发布：2026-07-16",
      "",
      "> 未配置文本模型，讨论总结未生成；原始讨论已完整入库。",
      "",
      "## 正文",
      "",
      "主楼内容",
      "",
      "## 讨论（1 条）",
      "",
      "**1 楼 · a**",
      "",
      "回复内容",
    ].join("\n");

    const sections = splitForumNoteSections(content);

    expect(sections.summary).toBe("");
    expect(sections.body).toContain("未配置文本模型");
    expect(sections.body).toContain("主楼内容");
    expect(sections.replies).toContain("回复内容");
  });

  it("用户编辑掉小节标题后不丢内容，全部归入主楼段", () => {
    const sections = splitForumNoteSections("随手改成一段普通笔记");
    expect(sections.body).toBe("随手改成一段普通笔记");
    expect(sections.summary).toBe("");
    expect(sections.replies).toBe("");
  });

  it("无回复的帖子回复段为空", () => {
    const content = [
      "> 平台：V2EX · 作者：someone · 0 条回复",
      "> 发布：2026-07-16",
      "",
      "## 正文",
      "",
      "只有主楼",
    ].join("\n");

    const sections = splitForumNoteSections(content);
    expect(sections.body).toBe("只有主楼");
    expect(sections.replies).toBe("");
  });
});

describe("parseForumReplies", () => {
  it("从已入库正文还原逐楼回复，供重新生成总结复用", () => {
    expect(parseForumReplies(FORUM_CONTENT)).toEqual([
      { floor: 1, author: "wowo243", content: "zerotier 试试" },
      { floor: 2, author: "onlychen", content: "搞个网关 sslvpn 拨回去。" },
    ]);
  });

  it("多行回复完整保留，不会被下一楼截断", () => {
    const content = [
      "> 平台：V2EX · 作者：a · 2 条回复",
      "> 发布：2026-07-16",
      "",
      "## 讨论（2 条）",
      "",
      "**1 楼 · hnbcinfo**",
      "",
      "easytier，我用了三年。",
      "",
      "中继换了个 12 块钱半年的 LXC。",
      "",
      "**2 楼 · me221**",
      "",
      "Netbird、Tailscale",
    ].join("\n");

    const replies = parseForumReplies(content);
    expect(replies).toHaveLength(2);
    expect(replies[0].content).toBe(
      "easytier，我用了三年。\n\n中继换了个 12 块钱半年的 LXC。",
    );
    expect(replies[1].content).toBe("Netbird、Tailscale");
  });

  it("没有讨论段时返回空数组", () => {
    expect(parseForumReplies("> 平台：V2EX · 作者：a · 0 条回复\n\n## 正文\n\n只有主楼")).toEqual([]);
  });
});

describe("upsertForumSummarySection", () => {
  it("已有总结小节时原位替换，不动主楼与讨论", () => {
    const result = upsertForumSummarySection(FORUM_CONTENT, "### 新方案\n- 新要点");

    expect(result).toContain("### 新方案");
    expect(result).not.toContain("ZeroTier");
    const sections = splitForumNoteSections(result);
    expect(sections.summary).toBe("### 新方案\n- 新要点");
    expect(sections.body).toBe("家里的设备有群辉 nas。");
    expect(sections.replies).toContain("**1 楼 · wowo243**");
  });

  it("采集时没生成总结的条目：插到元数据之后，并清掉过时的状态注记", () => {
    const content = [
      "> 平台：V2EX · 作者：a · 1 条回复",
      "> 发布：2026-07-16",
      "",
      "> 未配置文本模型，讨论总结未生成；原始讨论已完整入库。",
      "",
      "## 正文",
      "",
      "主楼",
      "",
      "## 讨论（1 条）",
      "",
      "**1 楼 · b**",
      "",
      "回复",
    ].join("\n");

    const result = upsertForumSummarySection(content, "### 方案\n- 要点");

    expect(result).not.toContain("未配置文本模型");
    // 元数据引用块仍在最前，且总结排在正文之前
    expect(result.startsWith("> 平台：V2EX")).toBe(true);
    expect(result.indexOf("## 讨论总结")).toBeLessThan(
      result.indexOf("## 正文"),
    );
    const sections = splitForumNoteSections(result);
    expect(sections.summary).toBe("### 方案\n- 要点");
    expect(sections.body).toBe("主楼");
    expect(sections.replies).toBe("**1 楼 · b**\n\n回复");
  });

  it("生成失败注记同样会被换掉", () => {
    const content = [
      "> 平台：V2EX · 作者：a · 1 条回复",
      "> 发布：2026-07-16",
      "",
      "> 讨论总结生成失败：模型额度不足。原始讨论已完整入库。",
      "",
      "## 讨论（1 条）",
      "",
      "**1 楼 · b**",
      "",
      "回复",
    ].join("\n");

    const result = upsertForumSummarySection(content, "### 方案\n- 要点");
    expect(result).not.toContain("生成失败");
    expect(splitForumNoteSections(result).summary).toBe("### 方案\n- 要点");
  });

  it("反复重新生成不会累积空行或重复小节", () => {
    let content = FORUM_CONTENT;
    for (let round = 0; round < 3; round++) {
      content = upsertForumSummarySection(content, `### 第 ${round} 版\n- 要点`);
    }

    expect(content.match(/## 讨论总结/g)).toHaveLength(1);
    expect(content).not.toMatch(/\n{3,}/);
    expect(splitForumNoteSections(content).summary).toBe("### 第 2 版\n- 要点");
  });
});
