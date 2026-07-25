import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { WikiDB } from "@guizhi/db/wiki";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import type { WikiApplyCompilationInput } from "@guizhi/shared/types";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

function compilationInput(
  itemId: string,
  overrides?: Partial<WikiApplyCompilationInput>,
): WikiApplyCompilationInput {
  return {
    itemId,
    contentHash: "hash-1",
    provider: "guizhi",
    model: "test-model",
    promptVersion: "wiki-compile-v1",
    pages: [
      {
        title: "知识管理",
        normalizedTitle: "知识管理",
        kind: "topic",
        summary: "关于知识管理",
        body: "正文，参考 [[ELECTRON]]",
        aliasesJson: '["KM"]',
        linkTargets: ["ELECTRON"],
      },
      {
        title: "Electron",
        normalizedTitle: "ELECTRON",
        kind: "entity",
        summary: "桌面框架",
        body: "Electron 正文",
        aliasesJson: null,
        linkTargets: [],
      },
    ],
    ...overrides,
  };
}

describe("WikiDB", () => {
  let db: DatabaseAdapter.Database;
  let wiki: WikiDB;
  let itemId: string;

  beforeEach(() => {
    db = createTestDb();
    wiki = new WikiDB(db);
    const items = new KnowledgeItemDB(db);
    itemId = items.create({ title: "来源条目", content: "内容" }).id;
  });

  it("applyCompilation 建页、建链接、记来源与指纹", () => {
    wiki.applyCompilation(compilationInput(itemId));

    const catalog = wiki.getCatalog();
    expect(catalog).toHaveLength(2);

    const km = catalog.find((entry) => entry.normalizedTitle === "知识管理")!;
    const detail = wiki.getPage(km.id)!;
    expect(detail.page.body).toContain("[[ELECTRON]]");
    expect(detail.sources).toHaveLength(1);
    expect(detail.sources[0].itemId).toBe(itemId);

    // 出链物化：Electron 页有来自知识管理页的反向链接
    const electron = catalog.find(
      (entry) => entry.normalizedTitle === "ELECTRON",
    )!;
    const electronDetail = wiki.getPage(electron.id)!;
    expect(electronDetail.backlinks.map((entry) => entry.id)).toEqual([km.id]);

    const ingestions = wiki.listIngestions();
    expect(ingestions).toHaveLength(1);
    expect(ingestions[0]).toMatchObject({ itemId, contentHash: "hash-1" });
  });

  it("再次编译按 normalized_title 更新既有页（id 不变）", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const before = wiki.getCatalog();

    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-2",
        pages: [
          {
            title: "知识管理",
            normalizedTitle: "知识管理",
            kind: "concept",
            summary: "更新后的摘要",
            body: "更新后的正文",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );

    const after = wiki.getCatalog();
    expect(after).toHaveLength(2);
    const km = after.find((entry) => entry.normalizedTitle === "知识管理")!;
    expect(km.id).toBe(
      before.find((entry) => entry.normalizedTitle === "知识管理")!.id,
    );
    expect(km.summary).toBe("更新后的摘要");
    expect(km.kind).toBe("concept");
    expect(wiki.listIngestions()[0].contentHash).toBe("hash-2");

    // 出链已替换为空
    const electron = after.find(
      (entry) => entry.normalizedTitle === "ELECTRON",
    )!;
    expect(wiki.getPage(electron.id)!.backlinks).toHaveLength(0);
  });

  it("getStatus 统计页面与已编译条目", () => {
    expect(wiki.getStatus()).toMatchObject({
      pageCount: 0,
      compiledItemCount: 0,
      eligibleItemCount: 1,
    });
    wiki.applyCompilation(compilationInput(itemId));
    expect(wiki.getStatus()).toMatchObject({
      pageCount: 2,
      compiledItemCount: 1,
      eligibleItemCount: 1,
    });
  });

  it("回收站条目的来源不展示、指纹计数排除", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const items = new KnowledgeItemDB(db);
    items.moveToTrash([itemId]);

    const catalog = wiki.getCatalog();
    const km = catalog.find((entry) => entry.normalizedTitle === "知识管理")!;
    expect(wiki.getPage(km.id)!.sources).toHaveLength(0);
    expect(wiki.getStatus().compiledItemCount).toBe(0);
  });

  it("clearAll 清空四表", () => {
    wiki.applyCompilation(compilationInput(itemId));
    wiki.clearAll();
    expect(wiki.getCatalog()).toHaveLength(0);
    expect(wiki.listIngestions()).toHaveLength(0);
    expect(wiki.getStatus().pageCount).toBe(0);
  });

  it("findPageIdByNormalizedTitle 精确定位", () => {
    wiki.applyCompilation(compilationInput(itemId));
    expect(wiki.findPageIdByNormalizedTitle("知识管理")).toBeTruthy();
    expect(wiki.findPageIdByNormalizedTitle("不存在")).toBeNull();
  });
});
