import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WikiCatalogEntry, WikiCompilableItem } from "@guizhi/shared/types";
import {
  buildLinkResolver,
  cleanWikiLinks,
  compilePendingItems,
  normalizeWikiTitle,
  parseWikiResponse,
  rankCandidates,
  sanitizePages,
} from "../../../src/renderer/services/knowledge-ai/wiki-compile";
import { preprocessWikiLinks } from "../../../src/renderer/components/wiki/WikiMarkdown";
import { installWindowMocks } from "../../helpers/window";

const { runScenarioChat } = vi.hoisted(() => ({ runScenarioChat: vi.fn() }));
vi.mock("../../../src/renderer/services/knowledge-ai/ai-invoke", () => ({
  runScenarioChat,
  AiNotConfiguredError: class AiNotConfiguredError extends Error {},
}));

function catalogEntry(
  overrides: Partial<WikiCatalogEntry> & { title: string },
): WikiCatalogEntry {
  return {
    id: `page-${overrides.title}`,
    normalizedTitle: normalizeWikiTitle(overrides.title),
    kind: "topic",
    summary: "",
    aliasesJson: null,
    updatedAt: 0,
    ...overrides,
  };
}

describe("normalizeWikiTitle", () => {
  it("折叠空白并大写化", () => {
    expect(normalizeWikiTitle("  知识  管理 ")).toBe("知识 管理");
    expect(normalizeWikiTitle("React Hooks")).toBe("REACT HOOKS");
  });
});

describe("parseWikiResponse", () => {
  it("解析规范 JSON 与围栏包裹", () => {
    const raw =
      '好的：\n```json\n{"pages":[{"title":"归知","kind":"entity","summary":"s","aliases":[],"body":"b"}]}\n```';
    const pages = parseWikiResponse(raw);
    expect(pages).toHaveLength(1);
    expect(pages![0].title).toBe("归知");
  });

  it("非法输出返回 null", () => {
    expect(parseWikiResponse("不是 JSON")).toBeNull();
    expect(parseWikiResponse('{"foo":1}')).toBeNull();
  });
});

describe("sanitizePages", () => {
  it("过滤空标题/空正文，批内去重，上限 4 页", () => {
    const pages = sanitizePages([
      { title: "页面A", kind: "topic", summary: "s", body: "内容" },
      { title: "", body: "无标题被过滤" },
      { title: "页面A", body: "重复标题被过滤" },
      { title: "页面B", body: "" },
      { title: "页面C", kind: "entity", summary: "多行\n第二行", body: "c" },
      { title: "页面D", body: "d" },
      { title: "页面E", body: "e" },
      { title: "页面F", body: "f" },
    ]);
    expect(pages.map((page) => page.title)).toEqual([
      "页面A",
      "页面C",
      "页面D",
      "页面E",
    ]);
    // summary 只取首行
    expect(pages[1].summary).toBe("多行");
    expect(pages[1].kind).toBe("entity");
  });

  it("别名净化：去重、剔除与标题同名的、上限 5 个", () => {
    const pages = sanitizePages([
      {
        title: "归知",
        body: "b",
        aliases: ["GuiZhi", "guizhi", "GuiZhi", "归知", "a", "b", "c", "d"],
      },
    ]);
    const aliases = JSON.parse(pages[0].aliasesJson!) as string[];
    expect(aliases).toHaveLength(5);
    expect(aliases).not.toContain("归知");
  });
});

describe("cleanWikiLinks", () => {
  const resolver = buildLinkResolver(
    [
      catalogEntry({ title: "知识管理", aliasesJson: '["KM","PKM"]' }),
      catalogEntry({ title: "Electron" }),
    ],
    [],
  );

  it("白名单内链接保留并归一目标标题（大小写差异的显示文字保留）", () => {
    const { body, targets } = cleanWikiLinks(
      "参考 [[知识管理]] 与 [[electron]]。",
      resolver,
    );
    expect(body).toBe("参考 [[知识管理]] 与 [[Electron|electron]]。");
    expect(targets).toEqual(["知识管理", "ELECTRON"]);
  });

  it("别名链接重写为规范标题（保留显示文字）", () => {
    const { body, targets } = cleanWikiLinks("详见 [[PKM|个人知识管理]]。", resolver);
    expect(body).toBe("详见 [[知识管理|个人知识管理]]。");
    expect(targets).toEqual(["知识管理"]);
  });

  it("白名单外的链接降级为纯文本", () => {
    const { body, targets } = cleanWikiLinks(
      "这个 [[不存在的页面]] 会降级。",
      resolver,
    );
    expect(body).toBe("这个 不存在的页面 会降级。");
    expect(targets).toEqual([]);
  });
});

describe("rankCandidates", () => {
  it("标题/别名命中素材的排前", () => {
    const catalog = [
      catalogEntry({ title: "无关页面" }),
      catalogEntry({ title: "Electron", aliasesJson: null }),
      catalogEntry({ title: "打包工具", aliasesJson: '["electron-builder"]' }),
    ];
    const ranked = rankCandidates(
      catalog,
      "归知使用 Electron 与 electron-builder 打包",
    );
    expect(ranked[0].entry.title).toBe("Electron");
    expect(ranked[0].score).toBe(10);
    expect(ranked[1].entry.title).toBe("打包工具");
    expect(ranked[1].score).toBe(5);
    expect(ranked[2].score).toBe(0);
  });
});

describe("compilePendingItems", () => {
  function compilableItem(id: string): WikiCompilableItem {
    return { id, title: `条目${id}`, content: `正文${id}` };
  }

  function installWikiApi(items: WikiCompilableItem[]) {
    installWindowMocks({
      api: {
        wiki: {
          listCompilable: vi.fn().mockResolvedValue(items),
          listIngestions: vi.fn().mockResolvedValue([]),
          catalog: vi.fn().mockResolvedValue([]),
          getPage: vi.fn().mockResolvedValue(null),
          applyCompilation: vi.fn().mockResolvedValue(undefined),
          recordCompilationFailure: vi.fn().mockResolvedValue(undefined),
        },
      },
    });
  }

  const onePage = {
    content: '{"pages":[{"title":"页面一","kind":"topic","summary":"s","body":"正文"}]}',
    model: "test-model",
  };

  beforeEach(() => {
    runScenarioChat.mockReset();
  });

  it("放宽单请求超时并把取消信号交给模型调用", async () => {
    // 主进程 30s 兜底会把这条最重的生成掐断在半路（v0.7 实测踩到）
    installWikiApi([compilableItem("a")]);
    runScenarioChat.mockResolvedValue(onePage);
    const controller = new AbortController();

    const result = await compilePendingItems(undefined, controller.signal);

    expect(result).toEqual({
      compiled: 1,
      pending: 1,
      skipped: 0,
      failures: [],
    });
    const options = runScenarioChat.mock.calls[0][2];
    expect(options.timeoutMs).toBeGreaterThan(30_000);
    expect(options.signal).toBe(controller.signal);
  });

  it("上一次被 max_tokens 截断 → 重试抬上限并要求更少页面，而不是再喊一遍格式", async () => {
    // 半截 JSON 不能拼接续写（模型会重写并漂移），只能丢弃重来；
    // 而重来时喊「只输出 JSON」对长度问题毫无作用
    installWikiApi([compilableItem("a")]);
    runScenarioChat
      .mockResolvedValueOnce({
        content: '{"pages":[{"title":"页面一","kind":"topic","summary":"s","body":"正文被切断',
        model: "test-model",
        finishReason: "length",
      })
      .mockResolvedValueOnce(onePage);

    const result = await compilePendingItems();

    expect(runScenarioChat).toHaveBeenCalledTimes(2);
    const retryPrompt = runScenarioChat.mock.calls[1][1][1].content;
    expect(retryPrompt).toContain("被截断");
    expect(retryPrompt).toContain("1~2 个页面");
    expect(retryPrompt).not.toContain("无法解析");
    expect(runScenarioChat.mock.calls[1][2].maxTokens).toBeGreaterThan(
      runScenarioChat.mock.calls[0][2].maxTokens,
    );
    expect(result.compiled).toBe(1);
  });

  it("输出完整但解析失败 → 重试仍用格式纠错话术", async () => {
    installWikiApi([compilableItem("a")]);
    runScenarioChat
      .mockResolvedValueOnce({
        content: "抱歉，我不能这样输出。",
        model: "test-model",
        finishReason: "stop",
      })
      .mockResolvedValueOnce(onePage);

    await compilePendingItems();

    const retryPrompt = runScenarioChat.mock.calls[1][1][1].content;
    expect(retryPrompt).toContain("无法解析");
    expect(retryPrompt).not.toContain("被截断");
    // 输出没到上限，抬预算解决不了问题，不该改动
    expect(runScenarioChat.mock.calls[1][2].maxTokens).toBe(
      runScenarioChat.mock.calls[0][2].maxTokens,
    );
  });

  it("中途取消 → 停在当前条目，已编译的保留，剩余留到下轮", async () => {
    installWikiApi([compilableItem("a"), compilableItem("b")]);
    const controller = new AbortController();
    runScenarioChat.mockImplementation(async () => {
      controller.abort();
      return onePage;
    });

    const result = await compilePendingItems(undefined, controller.signal);

    expect(runScenarioChat).toHaveBeenCalledTimes(1);
    expect(result).toEqual({
      compiled: 1,
      pending: 2,
      skipped: 0,
      failures: [],
    });
  });

  /**
   * 只回一个 skipped 计数的话，界面上就是「编译完成（1/2）」，
   * 用户得自己做减法，而且完全不知道那一条为什么没编出来。
   */
  it("条目级失败带回原因：格式错与被截断要分得开", async () => {
    installWikiApi([compilableItem("a")]);
    runScenarioChat.mockResolvedValue({
      content: "抱歉，我不能这样输出。",
      model: "test-model",
      finishReason: "stop",
    });

    const result = await compilePendingItems();

    expect(result.compiled).toBe(0);
    expect(result.skipped).toBe(1);
    expect(result.failures).toEqual([
      { title: "条目a", reason: "模型输出不是可解析的 JSON" },
    ]);
  });

  it("被 max_tokens 截断的失败原因指向长度而不是格式", async () => {
    installWikiApi([compilableItem("a")]);
    runScenarioChat.mockResolvedValue({
      content: '{"pages":[{"title":"页面一","kind":"topic","summary":"s","body":"被切断',
      model: "test-model",
      finishReason: "length",
    });

    const result = await compilePendingItems();

    expect(result.failures).toEqual([
      { title: "条目a", reason: "模型输出被 max_tokens 截断，JSON 不完整" },
    ]);
  });
});

describe("preprocessWikiLinks", () => {
  it("转换 wikilink 为 fragment 链接", () => {
    expect(preprocessWikiLinks("见 [[知识管理]]")).toBe(
      `见 [知识管理](#wiki=${encodeURIComponent("知识管理")})`,
    );
    expect(preprocessWikiLinks("见 [[知识管理|KM]]")).toBe(
      `见 [KM](#wiki=${encodeURIComponent("知识管理")})`,
    );
  });

  it("普通 markdown 不受影响", () => {
    const text = "普通 [链接](https://example.com) 与 `code`";
    expect(preprocessWikiLinks(text)).toBe(text);
  });
});
