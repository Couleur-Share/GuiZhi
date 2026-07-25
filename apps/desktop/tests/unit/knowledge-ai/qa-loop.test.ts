import { describe, expect, it } from "vitest";
import type { WikiCatalogEntry, WikiPageDetail } from "@guizhi/shared/types";
import {
  askKnowledgeBase,
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
  searchCalls: string[];
} {
  let index = 0;
  const chatCalls: string[] = [];
  const searchCalls: string[] = [];
  return {
    chatCalls,
    searchCalls,
    chat: async (messages) => {
      chatCalls.push(messages[messages.length - 1].content);
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
