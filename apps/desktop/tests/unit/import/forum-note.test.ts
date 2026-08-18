import { describe, expect, it } from "vitest";
import {
  filterForumReplies,
  FORUM_SUMMARY_STALE_NOTE,
  formatForumReplyBlock,
  normalizeForumSnippet,
  parseForumReplies,
  replaceForumRepliesSection,
  resolveReplyTargetFloor,
  splitForumNoteSections,
  upsertForumSummarySection,
  type ForumReplyEntry,
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
    expect(
      parseForumReplies(
        "> 平台：V2EX · 作者：a · 0 条回复\n\n## 正文\n\n只有主楼",
      ),
    ).toEqual([]);
  });

  it("认 NGA 楼主精选讨论标题与 ### 楼层头、回复上下文", () => {
    const content = [
      "> 平台：NGA · 作者：a · 2040 条回复（入库保留楼主 1 条）",
      "> 发布：2026-07-16",
      "",
      "## 讨论（楼主 1 条 · 原帖共 2040 条）",
      "",
      "### 12 楼 · a",
      "",
      "> 回复 @lyzlegend：对方在问镜片怎么选",
      "",
      "楼主补充说明",
    ].join("\n");

    expect(parseForumReplies(content)).toEqual([
      {
        floor: 12,
        author: "a",
        content: "楼主补充说明",
        replyTo: {
          author: "lyzlegend",
          snippet: "对方在问镜片怎么选",
        },
      },
    ]);
  });

  it("读库时洗净摘要里残留的 br 标签（旧条目不必重采）", () => {
    const content = [
      "### 6 楼 · a",
      "",
      "> 回复 @牧云吹雪：<br/> <br/> 想看看楼主整个眼镜是什么样的",
      "",
      "楼主回复正文",
    ].join("\n");

    expect(parseForumReplies(content)[0]?.replyTo?.snippet).toBe(
      "想看看楼主整个眼镜是什么样的",
    );
  });
});

describe("normalizeForumSnippet", () => {
  it("把 br 收成空格并压空白", () => {
    expect(normalizeForumSnippet("<br/><br/>大佬写的太好了<br/>下一段")).toBe(
      "大佬写的太好了 下一段",
    );
  });

  it("写入楼层块时也不留下字面量 br", () => {
    const block = formatForumReplyBlock({
      floor: 1,
      author: "a",
      content: "答",
      replyTo: {
        author: "b",
        snippet: "<br/><br/>对方原话",
      },
    });
    expect(block).toContain("> 回复 @b：对方原话");
    expect(block).not.toMatch(/<br/i);
  });

  it("有楼层时写出「（N 楼）」", () => {
    const block = formatForumReplyBlock({
      floor: 12,
      author: "a",
      content: "答",
      replyTo: {
        author: "lyzlegend",
        floor: 8,
        snippet: "对方在问",
      },
    });
    expect(block).toContain("> 回复 @lyzlegend（8 楼）：对方在问");
  });
});

describe("resolveReplyTargetFloor / 回复行楼层解析", () => {
  const replies: ForumReplyEntry[] = [
    { floor: 1, author: "楼主", content: "主楼" },
    { floor: 8, author: "lyzlegend", content: "提问" },
    { floor: 12, author: "楼主", content: "回答" },
    { floor: 20, author: "同名", content: "甲" },
    { floor: 21, author: "同名", content: "乙" },
  ];

  it("解析带楼层的回复行", () => {
    const content = [
      "### 12 楼 · 楼主",
      "",
      "> 回复 @lyzlegend（8 楼）：对方在问镜片怎么选",
      "",
      "楼主补充",
    ].join("\n");
    expect(parseForumReplies(content)[0]?.replyTo).toEqual({
      author: "lyzlegend",
      floor: 8,
      snippet: "对方在问镜片怎么选",
    });
  });

  it("旧格式无楼层仍可解析", () => {
    const content = [
      "### 12 楼 · 楼主",
      "",
      "> 回复 @lyzlegend：对方在问镜片怎么选",
      "",
      "楼主补充",
    ].join("\n");
    expect(parseForumReplies(content)[0]?.replyTo).toEqual({
      author: "lyzlegend",
      snippet: "对方在问镜片怎么选",
    });
  });

  it("优先按写明的楼层跳转；楼不在库里则 null", () => {
    expect(resolveReplyTargetFloor(replies, { author: "x", floor: 8 })).toBe(8);
    expect(
      resolveReplyTargetFloor(replies, { author: "x", floor: 999 }),
    ).toBeNull();
  });

  it("无楼层时按作者唯一匹配；重名或不存在则 null", () => {
    expect(resolveReplyTargetFloor(replies, { author: "lyzlegend" })).toBe(8);
    expect(resolveReplyTargetFloor(replies, { author: "同名" })).toBeNull();
    expect(resolveReplyTargetFloor(replies, { author: "不存在" })).toBeNull();
  });
});

describe("filterForumReplies", () => {
  const sample: ForumReplyEntry[] = [
    {
      floor: 6,
      author: "楼主",
      content: "整副眼镜的侧视图在附件",
      replyTo: {
        author: "牧云吹雪",
        snippet: "想看看楼主整个眼镜是什么样的",
      },
    },
    {
      floor: 14,
      author: "楼主",
      content: "数码型不推荐一般人随便配",
      replyTo: { author: "FreezeLegend", snippet: "渐进片相关的问题" },
    },
  ];

  it("空关键字返回全部", () => {
    expect(filterForumReplies(sample, "  ")).toEqual(sample);
  });

  it("按楼层号、被回复作者、正文命中", () => {
    expect(filterForumReplies(sample, "6").map((r) => r.floor)).toEqual([6]);
    expect(filterForumReplies(sample, "牧云吹雪").map((r) => r.floor)).toEqual([
      6,
    ]);
    expect(filterForumReplies(sample, "渐进片").map((r) => r.floor)).toEqual([
      14,
    ]);
  });

  it("无命中返回空数组", () => {
    expect(filterForumReplies(sample, "蔡司智锐")).toEqual([]);
  });
});

describe("upsertForumSummarySection", () => {
  it("已有总结小节时原位替换，不动主楼与讨论", () => {
    const result = upsertForumSummarySection(
      FORUM_CONTENT,
      "### 新方案\n- 新要点",
    );

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
      content = upsertForumSummarySection(
        content,
        `### 第 ${round} 版\n- 要点`,
      );
    }

    expect(content.match(/## 讨论总结/g)).toHaveLength(1);
    expect(content).not.toMatch(/\n{3,}/);
    expect(splitForumNoteSections(content).summary).toBe("### 第 2 版\n- 要点");
  });

  it("重新生成会清掉讨论刷新留下的总结过期提示", () => {
    const stale = replaceForumRepliesSection(FORUM_CONTENT, [
      { floor: 3, author: "new", content: "新回复" },
    ]);
    expect(stale).toContain(FORUM_SUMMARY_STALE_NOTE);

    const result = upsertForumSummarySection(
      stale,
      "### 新总结\n- 覆盖最新楼层",
    );
    expect(result).not.toContain(FORUM_SUMMARY_STALE_NOTE);
    expect(splitForumNoteSections(result).summary).toBe(
      "### 新总结\n- 覆盖最新楼层",
    );
  });
});

describe("replaceForumRepliesSection", () => {
  it("只替换讨论段，保留主楼并把已有总结标记为过期", () => {
    const result = replaceForumRepliesSection(FORUM_CONTENT, [
      { floor: 2, author: "onlychen", content: "更新后的二楼" },
      { floor: 3, author: "new-user", content: "新增楼层" },
    ]);
    const sections = splitForumNoteSections(result);

    expect(sections.body).toBe("家里的设备有群辉 nas。");
    expect(sections.summary).toContain("**ZeroTier**");
    expect(sections.summary).toContain(FORUM_SUMMARY_STALE_NOTE);
    expect(sections.replies).not.toContain("zerotier 试试");
    expect(sections.replies).toContain("### 2 楼 · onlychen");
    expect(sections.replies).toContain("新增楼层");
    expect(result).toContain("## 讨论（2 条）");
  });

  it("重复刷新不会累积过期提示", () => {
    const replies = [{ floor: 2, author: "a", content: "最新回复" }];
    const once = replaceForumRepliesSection(FORUM_CONTENT, replies);
    const twice = replaceForumRepliesSection(once, replies);

    expect(twice.match(/讨论内容已刷新/g)).toHaveLength(1);
  });
});
