import { describe, expect, it } from "vitest";
import type { WikiCatalogEntry } from "@guizhi/shared/types";
import {
  countCatalogByFilter,
  selectVisibleCatalog,
  wikiCompileButtonState,
} from "../../../src/renderer/stores/wiki.store";

describe("wikiCompileButtonState", () => {
  const counts = { ready: 0, review: 0, waiting: 0, upgrade: 0, current: 0, excluded: 0 };

  it("数字只算可执行资料，与全文编译对话框一致", () => {
    expect(wikiCompileButtonState({ ...counts, ready: 3, upgrade: 2 })).toEqual({ pending: 3, actionable: true });
  });

  it("只有待升级、待复核或等待中的资料时仍可打开对话框", () => {
    expect(wikiCompileButtonState({ ...counts, upgrade: 1 })).toEqual({ pending: 0, actionable: true });
    expect(wikiCompileButtonState({ ...counts, waiting: 1 })).toEqual({ pending: 0, actionable: true });
  });

  it("全部已是当前版本时禁用；预览未知时不拦", () => {
    expect(wikiCompileButtonState({ ...counts, current: 5, excluded: 1 })).toEqual({ pending: 0, actionable: false });
    expect(wikiCompileButtonState(null)).toEqual({ pending: 0, actionable: true });
  });
});

function entry(patch: Partial<WikiCatalogEntry>): WikiCatalogEntry {
  return {
    id: patch.id ?? "p1",
    title: patch.title ?? "页面",
    normalizedTitle: (patch.title ?? "页面").toUpperCase(),
    kind: patch.kind ?? "topic",
    summary: patch.summary ?? "",
    aliasesJson: null,
    manualEditedAt: patch.manualEditedAt ?? null,
    updatedAt: patch.updatedAt ?? 0,
  };
}

const CATALOG: WikiCatalogEntry[] = [
  entry({ id: "a", title: "Core Web Vitals", kind: "concept", updatedAt: 300 }),
  entry({ id: "b", title: "INP", kind: "concept", updatedAt: 200 }),
  entry({ id: "c", title: "内网穿透", kind: "topic", updatedAt: 100 }),
  entry({ id: "d", title: "Tailscale", kind: "entity", updatedAt: 400, manualEditedAt: 999 }),
];

// a 被两页引用，b 被一页引用，c/d 没有入链
const BACKLINKS = { a: 2, b: 1 };

describe("countCatalogByFilter", () => {
  it("类型、手动编辑与孤立页各自计数", () => {
    expect(countCatalogByFilter(CATALOG, BACKLINKS)).toEqual({
      all: 4,
      topic: 1,
      entity: 1,
      concept: 2,
      manual: 1,
      orphan: 2,
    });
  });

  it("没有任何链接时全部页面都算孤立", () => {
    expect(countCatalogByFilter(CATALOG, {}).orphan).toBe(4);
  });
});

describe("selectVisibleCatalog", () => {
  const base = {
    catalog: CATALOG,
    backlinkCounts: BACKLINKS,
    filter: "all" as const,
    sort: "recent" as const,
    searchHitIds: null,
  };

  it("默认按更新时间倒序", () => {
    expect(selectVisibleCatalog(base).map((page) => page.id)).toEqual([
      "d",
      "a",
      "b",
      "c",
    ]);
  });

  it("按被引用数排序，同分退回更新时间", () => {
    expect(
      selectVisibleCatalog({ ...base, sort: "linked" }).map((page) => page.id),
    ).toEqual(["a", "b", "d", "c"]);
  });

  it("孤立页筛选挑出没有入链的页面", () => {
    expect(
      selectVisibleCatalog({ ...base, filter: "orphan" }).map((page) => page.id),
    ).toEqual(["d", "c"]);
  });

  it("按类型与手动编辑筛选", () => {
    expect(
      selectVisibleCatalog({ ...base, filter: "concept" }).map((page) => page.id),
    ).toEqual(["a", "b"]);
    expect(
      selectVisibleCatalog({ ...base, filter: "manual" }).map((page) => page.id),
    ).toEqual(["d"]);
  });

  it("搜索命中保持 bm25 相关度顺序，不被排序轴打散", () => {
    const visible = selectVisibleCatalog({
      ...base,
      sort: "title",
      searchHitIds: ["c", "a"],
    });
    expect(visible.map((page) => page.id)).toEqual(["c", "a"]);
  });

  it("搜索与筛选叠加：命中集合再过一遍筛选轴", () => {
    const visible = selectVisibleCatalog({
      ...base,
      filter: "concept",
      searchHitIds: ["c", "a", "b"],
    });
    expect(visible.map((page) => page.id)).toEqual(["a", "b"]);
  });

  it("搜索无命中时给空列表，而不是退回全量", () => {
    expect(selectVisibleCatalog({ ...base, searchHitIds: [] })).toEqual([]);
  });
});
