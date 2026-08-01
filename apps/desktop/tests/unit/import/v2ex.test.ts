import { describe, expect, it, vi } from "vitest";
import type { AIClientConfig } from "@guizhi/core";
import { detectForumPlatform } from "@guizhi/shared/utils/forum-platforms";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";

// v2ex → safe-fetch → network-proxy 引用 electron，单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { fetchV2exThread, type ForumThread } from "../../../src/main/services/import/v2ex";
import { extractForumPost } from "../../../src/main/services/import/forum-post";
import {
  generateForumSummary,
  needsAiTitle,
  sanitizeForumSummary,
  splitReplyChunks,
} from "../../../src/main/services/import/forum-summary";

const CONFIG: AIClientConfig = {
  provider: "openai",
  apiProtocol: "openai",
  apiKey: "sk-test",
  apiUrl: "https://api.openai.com",
  model: "main-model",
};

/** 接口返回形态取自 www.v2ex.com/api/topics/show.json 实际响应 */
function topicPayload(overrides: Record<string, unknown> = {}) {
  return [
    {
      id: 1227616,
      title: "外面访问家里局域网最优雅的方式是？",
      content: "家里的设备有\r\n1. 群辉 nas\r\n\r\n可以接受花点钱。",
      created: 1784163702,
      replies: 3,
      url: "https://www.v2ex.com/t/1227616",
      member: { username: "yitwotre" },
      node: { name: "qna", title: "问与答" },
      ...overrides,
    },
  ];
}

function repliesPayload() {
  return [
    {
      content: "zerotier 试试",
      created: 1784163822,
      member: { username: "wowo243" },
    },
    {
      content: "搞个网关 sslvpn 拨回去。",
      created: 1784163871,
      member: { username: "onlychen" },
    },
    // 空回复应被丢弃，不占楼层素材
    { content: "   ", created: 1784163900, member: { username: "spammer" } },
  ];
}

/** 按 URL 分派的假接口 */
function fakeFetchJson(
  topics: unknown,
  replies: unknown,
  onCall?: (url: string) => void,
) {
  return async <T,>(url: string): Promise<T> => {
    onCall?.(url);
    if (url.includes("/topics/show.json")) {
      return topics as T;
    }
    if (url.includes("/replies/show.json")) {
      if (replies instanceof Error) {
        throw replies;
      }
      return replies as T;
    }
    throw new Error(`未预期的请求: ${url}`);
  };
}

describe("detectForumPlatform", () => {
  it("识别 V2EX 帖子链接并解出帖子 id（含回复锚点）", () => {
    expect(detectForumPlatform("https://www.v2ex.com/t/1227616#reply107")).toEqual(
      { platform: "v2ex", topicId: "1227616" },
    );
    expect(detectForumPlatform("https://v2ex.com/t/1227616")).toEqual({
      platform: "v2ex",
      topicId: "1227616",
    });
    expect(detectForumPlatform("https://www.v2ex.com/t/1227616?p=2")).toEqual({
      platform: "v2ex",
      topicId: "1227616",
    });
  });

  it("非帖子页与后缀碰撞域名一律退回通用抓取", () => {
    expect(detectForumPlatform("https://www.v2ex.com/go/qna")).toBeNull();
    expect(detectForumPlatform("https://www.v2ex.com/")).toBeNull();
    // fakev2ex.com 不是 v2ex.com 的子域
    expect(detectForumPlatform("https://fakev2ex.com/t/1227616")).toBeNull();
    expect(detectForumPlatform("https://example.com/t/1227616")).toBeNull();
    expect(detectForumPlatform("不是链接")).toBeNull();
  });
});

describe("fetchV2exThread", () => {
  it("解析主楼与回复，过滤空回复并编号楼层", async () => {
    const urls: string[] = [];
    const thread = await fetchV2exThread("1227616", {
      fetchJson: fakeFetchJson(topicPayload(), repliesPayload(), (url) =>
        urls.push(url),
      ),
    });

    expect(urls).toEqual([
      "https://www.v2ex.com/api/topics/show.json?id=1227616",
      "https://www.v2ex.com/api/replies/show.json?topic_id=1227616",
    ]);
    expect(thread.title).toBe("外面访问家里局域网最优雅的方式是？");
    expect(thread.author).toBe("yitwotre");
    expect(thread.node).toBe("问与答");
    expect(thread.replyCount).toBe(3);
    expect(thread.webpageUrl).toBe("https://www.v2ex.com/t/1227616");
    // \r\n 统一为 \n
    expect(thread.content).toContain("家里的设备有\n1. 群辉 nas");
    expect(thread.replies).toHaveLength(2);
    expect(thread.replies[0]).toMatchObject({
      floor: 1,
      author: "wowo243",
      content: "zerotier 试试",
    });
    expect(thread.replies[1].floor).toBe(2);
  });

  it("帖子不存在时接口返回空数组，应给出可读错误", async () => {
    await expect(
      fetchV2exThread("999999999", { fetchJson: fakeFetchJson([], []) }),
    ).rejects.toThrow(/帖子不存在/);
  });

  it("限流的 HTTP 403 翻译成用户能理解的说法", async () => {
    const fetchJson = vi.fn(async () => {
      throw new Error("HTTP 403");
    });

    await expect(
      fetchV2exThread("1227616", { fetchJson: fetchJson as never }),
    ).rejects.toThrow(/访问受限/);
    // 4xx 重试只会更快撞上限，必须一次就放弃
    expect(fetchJson).toHaveBeenCalledTimes(1);
  });

  it("Cloudflare 522 这类瞬时故障退避重试，成功后照常返回", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    let topicCalls = 0;
    const thread = await fetchV2exThread("1227616", {
      retryDelaysMs: [0, 0],
      fetchJson: async <T,>(url: string): Promise<T> => {
        if (url.includes("/topics/show.json")) {
          topicCalls += 1;
          if (topicCalls < 3) {
            throw new Error("HTTP 522");
          }
          return topicPayload() as T;
        }
        return repliesPayload() as T;
      },
    });

    expect(topicCalls).toBe(3);
    expect(thread.title).toBe("外面访问家里局域网最优雅的方式是？");
    expect(thread.replies).toHaveLength(2);
    warn.mockRestore();
  });

  it("重试耗尽后说清是对方故障，而不是甩一个 HTTP 522", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const fetchJson = vi.fn(async () => {
      throw new Error("HTTP 522");
    });

    await expect(
      fetchV2exThread("1227616", {
        retryDelaysMs: [0, 0],
        fetchJson: fetchJson as never,
      }),
    ).rejects.toThrow("V2EX 服务器暂时无响应（HTTP 522），已自动重试仍未成功，稍后再试");
    expect(fetchJson).toHaveBeenCalledTimes(3);
    warn.mockRestore();
  });

  it("连接超时同样重试，并给出可读说法", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const fetchJson = vi.fn(async () => {
      throw new Error("请求超时");
    });

    await expect(
      fetchV2exThread("1227616", {
        retryDelaysMs: [0],
        fetchJson: fetchJson as never,
      }),
    ).rejects.toThrow(/连接 V2EX 超时/);
    expect(fetchJson).toHaveBeenCalledTimes(2);
    warn.mockRestore();
  });

  it("取消要立刻生效，不等退避走完", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const controller = new AbortController();
    const fetchJson = vi.fn(async () => {
      controller.abort();
      throw new Error("HTTP 522");
    });

    await expect(
      fetchV2exThread(
        "1227616",
        { retryDelaysMs: [60_000], fetchJson: fetchJson as never },
        controller.signal,
      ),
    ).rejects.toThrow("已取消");
    expect(fetchJson).toHaveBeenCalledTimes(1);
    warn.mockRestore();
  });

  it("回复抓取失败时保留主楼，不让整条采集失败", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const thread = await fetchV2exThread("1227616", {
      retryDelaysMs: [],
      fetchJson: fakeFetchJson(topicPayload(), new Error("HTTP 500")),
    });

    expect(thread.title).toBe("外面访问家里局域网最优雅的方式是？");
    expect(thread.replies).toEqual([]);
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });
});

function buildThread(overrides: Partial<ForumThread> = {}): ForumThread {
  return {
    platform: "v2ex",
    topicId: "1227616",
    title: "外面访问家里局域网最优雅的方式是？",
    author: "yitwotre",
    node: "问与答",
    createdAt: new Date(2026, 6, 16, 9, 1).getTime(),
    replyCount: 2,
    content: "家里的设备有群辉 nas。",
    replies: [
      {
        floor: 1,
        author: "wowo243",
        content: "zerotier 试试",
        createdAt: 1784163822000,
      },
      {
        floor: 2,
        author: "onlychen",
        content: "搞个网关 sslvpn 拨回去。",
        createdAt: 1784163871000,
      },
    ],
    webpageUrl: "https://www.v2ex.com/t/1227616",
    ...overrides,
  };
}

const TARGET = { platform: "v2ex", topicId: "1227616" } as const;

describe("extractForumPost", () => {
  it("组装元数据、讨论总结、主楼与逐楼回复，类型为 forum", async () => {
    const entry = await extractForumPost(TARGET, {
      fetchThread: async () => buildThread(),
      getSummaryConfig: () => CONFIG,
      summarize: async () => ({
        summary: "**ZeroTier**\n- 多人推荐",
        title: null,
      }),
    });

    expect(entry.itemType).toBe("forum");
    expect(entry.title).toBe("外面访问家里局域网最优雅的方式是？");
    expect(entry.sourceUri).toBe("https://www.v2ex.com/t/1227616");
    expect(entry.degradedReason).toBeUndefined();
    // 元数据引用块沿用「平台：」开头，详情页来源 chip 才认得
    expect(entry.content).toContain(
      "> 平台：V2EX · 作者：yitwotre · 节点：问与答 · 2 条回复",
    );
    expect(entry.content).toContain("> 发布：2026-07-16");
    expect(entry.content).toContain("## 讨论总结");
    expect(entry.content).toContain("**ZeroTier**");
    expect(entry.content).toContain("## 正文");
    expect(entry.content).toContain("家里的设备有群辉 nas。");
    expect(entry.content).toContain("## 讨论（2 条）");
    expect(entry.content).toContain("### 1 楼 · wowo243");
    expect(entry.content).toContain("zerotier 试试");
    expect(entry.content).toContain("### 2 楼 · onlychen");
  });

  it("元数据两行紧邻，能被来源 chip 的解析整块剥离", async () => {
    const entry = await extractForumPost(TARGET, {
      fetchThread: async () => buildThread(),
      getSummaryConfig: () => CONFIG,
      summarize: async () => ({
        summary: "**ZeroTier**\n- 多人推荐",
        title: null,
      }),
    });

    // 中间夹空行会让「发布」这行漏进正文，必须紧邻
    expect(entry.content).toMatch(
      /^> 平台：V2EX[^\n]*\n> 发布：2026-07-16\n/,
    );
    const meta = parseVideoMetaBlock(entry.content);
    expect(meta?.platform).toBe("V2EX");
    expect(meta?.author).toBe("yitwotre");
    expect(meta?.body).not.toContain("发布：");

    const sections = splitForumNoteSections(entry.content);
    expect(sections.body).toBe("家里的设备有群辉 nas。");
    expect(sections.summary).toBe("**ZeroTier**\n- 多人推荐");
    expect(sections.replies).toContain("### 1 楼 · wowo243");
  });

  it("模型重拟标题时替换原标题，原标题记进元数据引用块", async () => {
    const entry = await extractForumPost(TARGET, {
      fetchThread: async () => buildThread({ title: "求推荐" }),
      getSummaryConfig: () => CONFIG,
      summarize: async () => ({
        summary: "### ZeroTier\n- 多人推荐",
        title: "内网穿透方案选型与实测对比",
      }),
    });

    expect(entry.title).toBe("内网穿透方案选型与实测对比");
    // 原标题仍在元数据块里：来源 chip 显示得出，全文检索也找得到
    const meta = parseVideoMetaBlock(entry.content);
    expect(meta?.originalTitle).toBe("求推荐");
    expect(meta?.platform).toBe("V2EX");
    expect(meta?.body).not.toContain("原标题：");
    expect(splitForumNoteSections(entry.content).summary).toBe(
      "### ZeroTier\n- 多人推荐",
    );
  });

  it("未配置文本模型时如实注明，讨论照常入库", async () => {
    const entry = await extractForumPost(TARGET, {
      fetchThread: async () => buildThread(),
      getSummaryConfig: () => null,
    });

    expect(entry.content).toContain("> 未配置文本模型，讨论总结未生成");
    expect(entry.content).not.toContain("## 讨论总结");
    expect(entry.content).toContain("### 1 楼 · wowo243");
    expect(entry.degradedReason).toBeUndefined();
  });

  it("总结失败不阻断采集，原因写进正文", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const entry = await extractForumPost(TARGET, {
      fetchThread: async () => buildThread(),
      getSummaryConfig: () => CONFIG,
      summarize: async () => {
        throw new Error("模型额度不足");
      },
    });

    expect(entry.content).toContain("> 讨论总结生成失败：模型额度不足");
    expect(entry.content).toContain("### 1 楼 · wowo243");
    expect(entry.degradedReason).toBeUndefined();
    warn.mockRestore();
  });

  it("只有主楼没有回复时不写讨论小节，也不调用模型", async () => {
    const summarize = vi.fn();
    const entry = await extractForumPost(TARGET, {
      fetchThread: async () => buildThread({ replies: [], replyCount: 0 }),
      getSummaryConfig: () => CONFIG,
      summarize,
    });

    expect(summarize).not.toHaveBeenCalled();
    expect(entry.content).not.toContain("## 讨论总结");
    expect(entry.content).not.toContain("## 讨论（");
    expect(entry.content).toContain("## 正文");
  });

  it("抓取失败返回 degradedReason 而非抛错（队列据此标失败且不入库）", async () => {
    const entry = await extractForumPost(TARGET, {
      fetchThread: async () => {
        throw new Error("帖子不存在、已被删除或需要登录后才能查看");
      },
    });

    expect(entry.degradedReason).toBe(
      "论坛帖子抓取失败：帖子不存在、已被删除或需要登录后才能查看",
    );
    expect(entry.sourceUri).toBeNull();
    expect(entry.content).toBe("");
  });

  it("取消要原样抛出，不能降级成失败任务", async () => {
    await expect(
      extractForumPost(TARGET, {
        fetchThread: async () => {
          throw new Error("已取消");
        },
      }),
    ).rejects.toThrow("已取消");
  });

  it("按阶段上报进度", async () => {
    const stages: string[] = [];
    await extractForumPost(TARGET, {
      fetchThread: async () => buildThread(),
      getSummaryConfig: () => CONFIG,
      summarize: async () => ({ summary: "总结", title: null }),
      onStage: (stage) => stages.push(stage),
    });

    expect(stages).toEqual(["forum-replies", "summarizing"]);
  });
});

describe("splitReplyChunks", () => {
  it("按回复边界切块，不把一条回复劈成两半", () => {
    const replies = Array.from({ length: 6 }, (_, index) => ({
      floor: index + 1,
      author: `u${index}`,
      content: "x".repeat(40),
      createdAt: 0,
    }));

    const chunks = splitReplyChunks(replies, 100);

    expect(chunks.length).toBeGreaterThan(1);
    expect(chunks.join("\n").split("\n")).toHaveLength(6);
    for (const chunk of chunks) {
      for (const line of chunk.split("\n")) {
        expect(line).toMatch(/^\d+ 楼 u\d+：x+$/);
      }
    }
  });

  it("单条超长回复自成一块", () => {
    const chunks = splitReplyChunks(
      [{ floor: 1, author: "u", content: "x".repeat(500), createdAt: 0 }],
      100,
    );
    expect(chunks).toHaveLength(1);
  });
});

describe("generateForumSummary", () => {
  it("短帖单发，素材含主楼与全部回复", async () => {
    const chat = vi.fn(async () => ({
      content: "**ZeroTier**\n- 多人推荐",
      finishReason: "stop" as const,
    }));

    const summary = await generateForumSummary(
      {
        title: "外面访问家里局域网最优雅的方式是？",
        content: "家里有群辉 nas。",
        replies: buildThread().replies,
      },
      CONFIG,
      { chat: chat as never },
    );

    // 独占一行的加粗小标题在清洗阶段升为 ###
    expect(summary?.summary).toBe("### ZeroTier\n- 多人推荐");
    expect(chat).toHaveBeenCalledTimes(1);
    const userMessage = chat.mock.calls[0][1][1].content;
    expect(userMessage).toContain("家里有群辉 nas。");
    expect(userMessage).toContain("1 楼 wowo243：zerotier 试试");
  });

  it("没有回复时不调用模型", async () => {
    const chat = vi.fn();
    const summary = await generateForumSummary(
      { title: "标题", content: "正文", replies: [] },
      CONFIG,
      { chat: chat as never },
    );

    expect(summary).toBeNull();
    expect(chat).not.toHaveBeenCalled();
  });

  it("长帖走 map-reduce：分块提要点后再综合", async () => {
    const replies = Array.from({ length: 60 }, (_, index) => ({
      floor: index + 1,
      author: `user${index}`,
      content: "这是一条足够长的回复内容，用来把素材撑过单发上限。".repeat(20),
      createdAt: 0,
    }));
    const chat = vi.fn(async () => ({
      content: "**方案**\n- 要点",
      finishReason: "stop" as const,
    }));

    const summary = await generateForumSummary(
      { title: "长帖", content: "主楼", replies },
      CONFIG,
      { chat: chat as never },
    );

    expect(summary?.summary).toBe("### 方案\n- 要点");
    // 至少一轮 map 加一次 reduce
    expect(chat.mock.calls.length).toBeGreaterThan(1);
    const lastUserMessage =
      chat.mock.calls[chat.mock.calls.length - 1][1][1].content;
    expect(lastUserMessage).toContain("要点笔记");
  });

  it("模型输出的 ## 标题降为 ###，避免与正文小节锚点撞车", async () => {
    const chat = vi.fn(async () => ({
      content: "## 方案一\n内容\n\n---\n\n更多",
      finishReason: "stop" as const,
    }));

    const summary = await generateForumSummary(
      { title: "标题", content: "正文", replies: buildThread().replies },
      CONFIG,
      { chat: chat as never },
    );

    expect(summary?.summary).toContain("### 方案一");
    expect(summary?.summary).not.toMatch(/^#{1,2}\s/m);
    expect(summary?.summary).not.toMatch(/^-{3,}$/m);
  });

  it("原标题说得清内容时不要拟题指令，也不动标题", async () => {
    const chat = vi.fn(async () => ({
      content: "**方案**\n- 要点",
      finishReason: "stop" as const,
    }));

    const summary = await generateForumSummary(
      {
        title: "外面访问家里局域网最优雅的方式是？",
        content: "主楼",
        replies: buildThread().replies,
      },
      CONFIG,
      { chat: chat as never },
    );

    expect(summary?.title).toBeNull();
    expect(chat.mock.calls[0][1][0].content).not.toContain("「标题：」");
  });

  it("弱标题时在同一次请求里拟题，不额外发一次调用", async () => {
    const chat = vi.fn(async () => ({
      content: "标题：内网穿透方案选型与实测对比\n\n**方案**\n- 要点",
      finishReason: "stop" as const,
    }));

    const summary = await generateForumSummary(
      { title: "求推荐", content: "主楼", replies: buildThread().replies },
      CONFIG,
      { chat: chat as never },
    );

    expect(chat).toHaveBeenCalledTimes(1);
    expect(chat.mock.calls[0][1][0].content).toContain("「标题：」");
    expect(summary?.title).toBe("内网穿透方案选型与实测对比");
    // 标题行不能漏进总结正文
    expect(summary?.summary).toBe("### 方案\n- 要点");
  });

  it("要了标题但模型没按协议输出时，整段仍当总结正文", async () => {
    const chat = vi.fn(async () => ({
      content: "**方案**\n- 要点",
      finishReason: "stop" as const,
    }));

    const summary = await generateForumSummary(
      { title: "求推荐", content: "主楼", replies: buildThread().replies },
      CONFIG,
      { chat: chat as never },
    );

    expect(summary?.title).toBeNull();
    expect(summary?.summary).toBe("### 方案\n- 要点");
  });
});

describe("needsAiTitle", () => {
  it("说得清内容的标题一律不动", () => {
    for (const title of [
      "外面访问家里局域网最优雅的方式是？",
      "如果有家族遗传脱发应尽早使用非那雄胺",
      "求推荐一个适合小团队的项目管理工具",
      "有人用过 Obsidian 的同步服务吗",
      "大家都用什么记笔记？",
    ]) {
      expect(needsAiTitle(title)).toBe(false);
    }
  });

  it("只剩套话、过短或抓取兜底的标题才重拟", () => {
    for (const title of [
      "求推荐",
      "问个问题",
      "急！在线等",
      "有人遇到过吗",
      "",
      "   ",
      "V2EX 帖子 1227616",
    ]) {
      expect(needsAiTitle(title)).toBe(true);
    }
  });
});

describe("sanitizeForumSummary", () => {
  it("独占一行的加粗小标题转成 ###（否则会和下一行正文渲染成同一段）", () => {
    const result = sanitizeForumSummary(
      "**基于 IPv6 的 DDNS 直连**\n多人推荐此方案，速度最快。",
    );
    expect(result).toBe("### 基于 IPv6 的 DDNS 直连\n多人推荐此方案，速度最快。");
  });

  it("带尾冒号与编号的加粗标题一并归一（冒号在 ** 内外都算）", () => {
    expect(sanitizeForumSummary("**共识与分歧：**")).toBe("### 共识与分歧");
    expect(sanitizeForumSummary("**共识与分歧**：")).toBe("### 共识与分歧");
    expect(sanitizeForumSummary("**一、方案对比**")).toBe("### 一、方案对比");
  });

  it("列表项里的加粗是内容不是标题，必须原样保留", () => {
    const input = "### 组网工具\n- **ZeroTier**：延迟 4ms\n  - **Tailscale**：会冲突";
    expect(sanitizeForumSummary(input)).toBe(input);
  });

  it("# 与 #### 都拉平到 ###，标题里的加粗标记去掉", () => {
    expect(sanitizeForumSummary("# 方案")).toBe("### 方案");
    expect(sanitizeForumSummary("#### **方案**")).toBe("### 方案");
  });

  it("删分隔线、去冗余总标题、折叠多余空行", () => {
    const result = sanitizeForumSummary(
      "## 讨论总结\n\n概述一句。\n\n\n\n---\n\n### 方案\n- 要点",
    );
    expect(result).toBe("概述一句。\n\n### 方案\n- 要点");
  });

  it("规范化后的 ### 标题不会被详情页误判成回复小节", () => {
    const summary = sanitizeForumSummary("**讨论**\n- 要点");
    const content = [
      "> 平台：V2EX · 作者：a · 1 条回复",
      "> 发布：2026-07-16",
      "",
      "## 讨论总结",
      "",
      summary,
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

    const sections = splitForumNoteSections(content);
    expect(sections.summary).toBe("### 讨论\n- 要点");
    expect(sections.body).toBe("主楼");
    expect(sections.replies).toBe("**1 楼 · b**\n\n回复");
  });
});
