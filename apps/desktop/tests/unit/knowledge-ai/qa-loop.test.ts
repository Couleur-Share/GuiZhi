import { describe, expect, it } from "vitest";
import type { WikiCatalogEntry, WikiPageDetail } from "@guizhi/shared/types";
import {
  askKnowledgeBase,
  extractReadWindow,
  locateNormalized,
  matchWikiPages,
  QaNoSourceError,
  type QaDeps,
  type QaItemContent,
  type QaSearchHit,
} from "../../../src/renderer/services/knowledge-ai/qa";

const ITEMS: Record<string, QaItemContent> = {
  "item-a": { title: "归知架构", content: "归知采用 Electron + React 构建。" },
  "item-b": { title: "采集管线", content: "导入队列并发为 2，支持断点恢复。" },
};

const HITS: QaSearchHit[] = [
  { id: "item-a", title: "归知架构", snippet: "Electron + React" },
  { id: "item-b", title: "采集管线", snippet: "导入队列" },
];

/** 按脚本依次返回模型输出的假 deps */
function createScriptedDeps(script: string[]): QaDeps & {
  chatCalls: string[];
  chatSignals: (AbortSignal | undefined)[];
  searchCalls: string[];
} {
  let index = 0;
  const chatCalls: string[] = [];
  const chatSignals: (AbortSignal | undefined)[] = [];
  const searchCalls: string[] = [];
  return {
    chatCalls,
    chatSignals,
    searchCalls,
    chat: async (messages, options) => {
      chatCalls.push(messages[messages.length - 1].content);
      chatSignals.push(options.signal);
      const content = script[Math.min(index, script.length - 1)];
      index++;
      return { content, model: "test-model" };
    },
    searchItems: async (query) => {
      searchCalls.push(query);
      return HITS;
    },
    readItem: async (id) => ITEMS[id] ?? null,
  };
}

describe("askKnowledgeBase - Agent 循环", () => {
  it("search → read → answer 完整路径，引用对齐", async () => {
    const deps = createScriptedDeps([
      '{"action":"search","query":"架构"}',
      '{"action":"read","target":1}',
      '{"action":"answer","text":"归知基于 Electron 构建 [1]。"}',
    ]);
    const steps: string[] = [];
    const answer = await askKnowledgeBase(
      "归知用什么技术栈？",
      undefined,
      deps,
      (step) => steps.push(step),
    );

    expect(answer.usedFallback).toBe(false);
    expect(answer.text).toContain("Electron");
    expect(answer.sources).toHaveLength(1);
    expect(answer.sources[0]).toMatchObject({
      ordinal: 1,
      kind: "item",
      refId: "item-a",
    });
    expect(steps.some((step) => step.startsWith("检索："))).toBe(true);
    expect(steps.some((step) => step.startsWith("阅读："))).toBe(true);
  });

  it("signal 透传到每一次对话调用（否则「停止」中断不了在途请求）", async () => {
    const deps = createScriptedDeps([
      '{"action":"search","query":"架构"}',
      '{"action":"answer","text":"归知基于 Electron 构建 [1]。"}',
    ]);
    const controller = new AbortController();

    await askKnowledgeBase(
      "归知用什么技术栈？",
      undefined,
      deps,
      undefined,
      controller.signal,
    );

    expect(deps.chatSignals.length).toBeGreaterThan(0);
    for (const signal of deps.chatSignals) {
      expect(signal).toBe(controller.signal);
    }
  });

  it("单发兜底管线同样带上 signal", async () => {
    // 协议连续失败会退回单发管线
    const deps = createScriptedDeps(["这不是 JSON"]);

    await askKnowledgeBase(
      "归知用什么技术栈？",
      undefined,
      deps,
      undefined,
      new AbortController().signal,
    );

    expect(deps.chatSignals.at(-1)).toBeInstanceOf(AbortSignal);
  });

  it("回答未标注引用时退回全部已读资料", async () => {
    const deps = createScriptedDeps([
      '{"action":"search","query":"架构"}',
      '{"action":"read","target":1}',
      '{"action":"read","target":2}',
      '{"action":"answer","text":"回答没有标注任何引用。"}',
    ]);
    const answer = await askKnowledgeBase("问题", undefined, deps);
    expect(answer.sources.map((source) => source.ordinal)).toEqual([1, 2]);
  });

  it("未读先答被纠正后仍能完成", async () => {
    const deps = createScriptedDeps([
      '{"action":"answer","text":"我直接回答"}',
      '{"action":"search","query":"架构"}',
      '{"action":"read","target":1}',
      '{"action":"answer","text":"读过资料后的回答 [1]"}',
    ]);
    const answer = await askKnowledgeBase("问题", undefined, deps);
    expect(answer.usedFallback).toBe(false);
    expect(answer.text).toContain("读过资料后的回答");
  });

  it("协议持续失败回退单发管线", async () => {
    // 前 3 次输出无法解析（超过违规上限 2），触发回退；
    // 回退管线的 chat 调用返回同一脚本的后续内容
    const deps = createScriptedDeps([
      "我不会输出 JSON",
      "还是不会",
      "就是不输出",
      "根据资料，答案是 Electron [1]。",
    ]);
    const steps: string[] = [];
    const answer = await askKnowledgeBase("问题", undefined, deps, (step) =>
      steps.push(step),
    );

    expect(answer.usedFallback).toBe(true);
    expect(answer.sources).toHaveLength(1);
    expect(answer.sources[0].refId).toBe("item-a");
    expect(steps).toContain("智能检索未成功，改用单次检索…");
  });

  it("兜底管线零命中抛 QaNoSourceError", async () => {
    const deps: QaDeps = {
      chat: async () => ({ content: "不是 JSON", model: "m" }),
      searchItems: async () => [],
      readItem: async () => null,
    };
    await expect(
      askKnowledgeBase("问题", undefined, deps),
    ).rejects.toBeInstanceOf(QaNoSourceError);
  });

  it("多轮历史传入提示词", async () => {
    const deps = createScriptedDeps([
      '{"action":"search","query":"架构"}',
      '{"action":"read","target":1}',
      '{"action":"answer","text":"追问的回答 [1]"}',
    ]);
    await askKnowledgeBase(
      "那性能呢？",
      [{ question: "归知是什么？", answer: "一个知识库应用" }],
      deps,
    );
    expect(deps.chatCalls[0]).toContain("此前对话：");
    expect(deps.chatCalls[0]).toContain("归知是什么？");
  });

  it("Wiki 路径：search 命中页面，read 注册出链与来源为新资源", async () => {
    const wikiCatalog: WikiCatalogEntry[] = [
      {
        id: "wiki-1",
        title: "知识管理",
        normalizedTitle: "知识管理",
        kind: "topic",
        summary: "知识管理总览",
        aliasesJson: null,
        updatedAt: 0,
      },
      {
        id: "wiki-2",
        title: "采集工作流",
        normalizedTitle: "采集工作流",
        kind: "concept",
        summary: "",
        aliasesJson: null,
        updatedAt: 0,
      },
    ];
    const wikiDetail: WikiPageDetail = {
      page: {
        id: "wiki-1",
        title: "知识管理",
        normalizedTitle: "知识管理",
        kind: "topic",
        summary: "知识管理总览",
        body: "总览正文，见 [[采集工作流]]。",
        aliasesJson: null,
        provider: "guizhi",
        model: "m",
        promptVersion: "wiki-compile-v1",
        generatedAt: 0,
        createdAt: 0,
        updatedAt: 0,
      },
      backlinks: [],
      sources: [{ itemId: "item-a", title: "归知架构" }],
    };

    const base = createScriptedDeps([
      '{"action":"search","query":"知识管理"}',
      '{"action":"read","target":1}',
      '{"action":"answer","text":"基于 Wiki 的回答 [1]"}',
    ]);
    const deps: QaDeps = {
      ...base,
      getWikiCatalog: async () => wikiCatalog,
      readWikiPage: async (id) => (id === "wiki-1" ? wikiDetail : null),
    };

    const answer = await askKnowledgeBase("知识管理是什么？", undefined, deps);
    expect(answer.usedFallback).toBe(false);
    expect(answer.sources).toEqual([
      { ordinal: 1, kind: "wiki", refId: "wiki-1", title: "知识管理" },
    ]);

    // 阅读轨迹注册了出链页面与来源条目（[1] wiki + [2..n] 搜索命中的条目 + 关联资源）
    const readTranscript = base.chatCalls[2];
    expect(readTranscript).toContain("关联页面（可继续 read）");
    expect(readTranscript).toContain("《采集工作流》");
    expect(readTranscript).toContain("来源条目（原文，可继续 read）");
  });
});

describe("matchWikiPages", () => {
  const catalog: WikiCatalogEntry[] = [
    {
      id: "w1",
      title: "Electron",
      normalizedTitle: "ELECTRON",
      kind: "entity",
      summary: "跨平台桌面框架",
      aliasesJson: '["电子框架"]',
      updatedAt: 0,
    },
    {
      id: "w2",
      title: "知识管理",
      normalizedTitle: "知识管理",
      kind: "topic",
      summary: "PKM 方法论",
      aliasesJson: null,
      updatedAt: 0,
    },
  ];

  it("标题包含于查询时命中", () => {
    expect(matchWikiPages(catalog, "Electron 是什么").map((e) => e.id)).toEqual([
      "w1",
    ]);
  });

  it("别名命中", () => {
    expect(matchWikiPages(catalog, "电子框架").map((e) => e.id)).toEqual([
      "w1",
    ]);
  });

  it("摘要 token 命中", () => {
    expect(matchWikiPages(catalog, "PKM").map((e) => e.id)).toEqual(["w2"]);
  });

  it("无命中返回空", () => {
    expect(matchWikiPages(catalog, "完全无关的词汇")).toEqual([]);
  });
});

describe("流式回答", () => {
  /** 把脚本里的输出按块吐给 onDelta，模拟 SSE */
  function createStreamingDeps(script: string[], chunkSize = 7): QaDeps {
    let index = 0;
    return {
      chat: async (_messages, options) => {
        const content = script[Math.min(index, script.length - 1)];
        index++;
        if (options.onDelta) {
          for (let at = 0; at < content.length; at += chunkSize) {
            options.onDelta(content.slice(at, at + chunkSize));
          }
        }
        return { content, model: "test-model" };
      },
      searchItems: async () => HITS,
      readItem: async (id) => ITEMS[id] ?? null,
    };
  }

  it("回答逐字到达，检索轮的动作 JSON 不漏到界面", async () => {
    const deps = createStreamingDeps([
      '{"action":"search","query":"架构"}',
      '{"action":"read","target":1}',
      '{"action":"answer","text":"归知基于 Electron 构建 [1]。"}',
    ]);
    const streamed: string[] = [];
    const answer = await askKnowledgeBase(
      "归知用什么技术栈？",
      undefined,
      deps,
      undefined,
      undefined,
      (text) => streamed.push(text),
    );

    expect(answer.text).toBe("归知基于 Electron 构建 [1]。");
    // 至少分多次到达，且每一次都是「到目前为止的全量文本」
    expect(streamed.length).toBeGreaterThan(1);
    expect(streamed.at(-1)).toBe("归知基于 Electron 构建 [1]。");
    for (const [position, text] of streamed.entries()) {
      expect(answer.text.startsWith(text)).toBe(true);
      if (position > 0) {
        expect(text.length).toBeGreaterThanOrEqual(streamed[position - 1].length);
      }
    }
    // search / read 两轮不该吐出任何内容
    expect(streamed.some((text) => text.includes("action"))).toBe(false);
  });

  it("被判违规的回答轮不会在界面上留下半截内容", async () => {
    // 第一轮直接 answer 但没读过任何资料 → 违规，第二轮才是真回答
    const deps = createStreamingDeps([
      '{"action":"answer","text":"我瞎编的回答"}',
      '{"action":"search","query":"架构"}',
      '{"action":"read","target":1}',
      '{"action":"answer","text":"基于资料的回答 [1]。"}',
    ]);
    const streamed: string[] = [];
    const answer = await askKnowledgeBase(
      "问题",
      undefined,
      deps,
      undefined,
      undefined,
      (text) => streamed.push(text),
    );

    expect(answer.text).toBe("基于资料的回答 [1]。");
    // 每轮换一份新 state，最终文本会把上一轮的半截盖掉
    expect(streamed.at(-1)).toBe("基于资料的回答 [1]。");
  });

  it("不传回调时不启用流式", async () => {
    const seen: (((chunk: string) => void) | undefined)[] = [];
    const deps: QaDeps = {
      chat: async (_messages, options) => {
        seen.push(options.onDelta);
        return {
          content: '{"action":"answer","text":"直接回答"}',
          model: "m",
        };
      },
      searchItems: async () => HITS,
      readItem: async (id) => ITEMS[id] ?? null,
    };
    // 没读过资料会被判违规，这里只关心 onDelta 是否为 undefined
    await askKnowledgeBase("问题", undefined, deps).catch(() => undefined);
    expect(seen.every((callback) => callback === undefined)).toBe(true);
  });
});

describe("locateNormalized / extractReadWindow", () => {
  it("空白被压过的片段也能在原文里定位", () => {
    const haystack = "前言。\n\n  关键结论   在这里。\n后记。";
    expect(locateNormalized(haystack, "关键结论 在这里。")).toBe(
      haystack.indexOf("关键结论"),
    );
  });

  it("找不到时返回 -1，过短的片段不参与定位", () => {
    expect(locateNormalized("一段正文", "不存在")).toBe(-1);
    expect(locateNormalized("一段正文", "正文")).toBe(-1);
  });

  it("短于上限的正文原样返回", () => {
    expect(extractReadWindow("很短的一段", 100, "任意")).toBe("很短的一段");
  });

  it("有命中线索时取包含命中的窗口，而不是文档开头", () => {
    const body = `${"开头填充。".repeat(400)}这里是真正的答案段落。${"结尾填充。".repeat(400)}`;
    const window = extractReadWindow(body, 300, "这里是真正的答案段落。");

    expect(window).toContain("这里是真正的答案段落。");
    expect(window.startsWith("…（前文略）")).toBe(true);
    expect(window).toContain("…（后文略）");
  });

  it("没有线索或定位失败时退回开头截断", () => {
    const body = "起始标记" + "填充。".repeat(500);
    expect(extractReadWindow(body, 200).startsWith("起始标记")).toBe(true);
    expect(
      extractReadWindow(body, 200, "库里根本没有这句话啊").startsWith("起始标记"),
    ).toBe(true);
  });

  it("命中在文档末尾时窗口不越界", () => {
    const body = `${"填充。".repeat(500)}末尾的关键结论。`;
    const window = extractReadWindow(body, 200, "末尾的关键结论。");
    expect(window).toContain("末尾的关键结论。");
    expect(window).not.toContain("…（后文略）");
  });
});
