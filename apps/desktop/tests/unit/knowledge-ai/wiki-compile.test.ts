import { describe, expect, it } from "vitest";
import type { WikiCatalogEntry } from "@guizhi/shared/types";
import {
  buildLinkResolver,
  cleanWikiLinks,
  normalizeWikiTitle,
  parseWikiResponse,
  rankCandidates,
  sanitizePages,
} from "../../../src/renderer/services/knowledge-ai/wiki-compile";
import { preprocessWikiLinks } from "../../../src/renderer/components/wiki/WikiMarkdown";

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
