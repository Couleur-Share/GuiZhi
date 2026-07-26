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

  it("getBacklinkCounts 只列有入链的页面，孤立页靠缺省判定", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    const electronId = wiki.findPageIdByNormalizedTitle("ELECTRON")!;

    const counts = wiki.getBacklinkCounts();
    expect(counts[electronId]).toBe(1);
    // 知识管理页没有入链——界面据此把它归进「孤立页」
    expect(counts[kmId]).toBeUndefined();
  });

  it("目录带上手动编辑标记（侧栏据此筛选）", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    expect(
      wiki.getCatalog().every((entry) => entry.manualEditedAt === null),
    ).toBe(true);

    wiki.updatePageBody({ pageId: kmId, body: "手写正文", linkTargets: [] });
    const edited = wiki.getCatalog().find((entry) => entry.id === kmId)!;
    expect(edited.manualEditedAt).toBeTruthy();
  });

  it("再次编译按 normalized_title 更新既有页（id 不变）", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const before = wiki.getCatalog();
    const kmId = before.find(
      (entry) => entry.normalizedTitle === "知识管理",
    )!.id;

    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-2",
        contextPageIds: [kmId],
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
    expect(km.id).toBe(kmId);
    expect(km.summary).toBe("更新后的摘要");
    expect(km.kind).toBe("concept");
    expect(wiki.getPage(kmId)!.page.body).toBe("更新后的正文");
    expect(wiki.listIngestions()[0].contentHash).toBe("hash-2");

    // 出链已替换为空
    const electron = after.find(
      (entry) => entry.normalizedTitle === "ELECTRON",
    )!;
    expect(wiki.getPage(electron.id)!.backlinks).toHaveLength(0);
  });

  it("未进入本轮上下文的页面保留原正文，只更新元数据", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    const originalBody = wiki.getPage(kmId)!.page.body;

    // contextPageIds 为空：模型只看到目录里的标题和摘要，
    // 它"更新"出来的正文是凭空编的，覆盖上去等于丢掉原页内容
    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-3",
        contextPageIds: [],
        pages: [
          {
            title: "知识管理",
            normalizedTitle: "知识管理",
            kind: "concept",
            summary: "凭空编的摘要",
            body: "凭空编的正文",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );

    expect(wiki.getPage(kmId)!.page.body).toBe(originalBody);
    // 摘要与类型仍会更新——它们本来就只依赖标题层面的信息
    expect(wiki.getPage(kmId)!.page.summary).toBe("凭空编的摘要");
  });

  it("保留正文的页面，出链也一并保留（链接表不能与正文脱节）", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    const electronId = wiki.findPageIdByNormalizedTitle("ELECTRON")!;
    expect(wiki.getPage(electronId)!.backlinks.map((e) => e.id)).toEqual([kmId]);

    // linkTargets 是从被丢弃的草稿正文解析出来的，
    // 拿它重建出链会让正文里的 [[ELECTRON]] 在图谱和反向链接里凭空消失
    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-4",
        contextPageIds: [],
        pages: [
          {
            title: "知识管理",
            normalizedTitle: "知识管理",
            kind: "topic",
            summary: "只更新摘要",
            body: "凭空编的正文，没有任何链接",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );

    expect(wiki.getPage(kmId)!.page.body).toContain("[[ELECTRON]]");
    expect(wiki.getPage(electronId)!.backlinks.map((e) => e.id)).toEqual([kmId]);
  });

  it("正文没被覆盖时不留空快照", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    expect(wiki.listRevisions(kmId)).toHaveLength(0);

    // 只更新摘要的轮次也存快照的话，10 格历史会被一串
    // 内容完全相同的版本占满，真正想回滚的那一版反被挤出去
    for (let round = 0; round < 3; round++) {
      wiki.applyCompilation(
        compilationInput(itemId, {
          contentHash: `meta-${round}`,
          contextPageIds: [],
          pages: [
            {
              title: "知识管理",
              normalizedTitle: "知识管理",
              kind: "topic",
              summary: `第 ${round} 轮摘要`,
              body: "凭空编的正文",
              aliasesJson: null,
              linkTargets: [],
            },
          ],
        }),
      );
    }

    expect(wiki.listRevisions(kmId)).toHaveLength(0);
  });

  it("覆盖前存快照，可回滚到上一版", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    const originalBody = wiki.getPage(kmId)!.page.body;

    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-2",
        contextPageIds: [kmId],
        pages: [
          {
            title: "知识管理",
            normalizedTitle: "知识管理",
            kind: "topic",
            summary: "新摘要",
            body: "被覆盖后的正文",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );
    expect(wiki.getPage(kmId)!.page.body).toBe("被覆盖后的正文");

    const revisions = wiki.listRevisions(kmId);
    expect(revisions.length).toBeGreaterThan(0);
    expect(revisions[0].body).toBe(originalBody);

    expect(wiki.restoreRevision(revisions[0].id)).toBe(true);
    expect(wiki.getPage(kmId)!.page.body).toBe(originalBody);
  });

  it("每页历史版本不超过保留上限", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;

    for (let round = 0; round < 15; round++) {
      wiki.applyCompilation(
        compilationInput(itemId, {
          contentHash: `hash-${round}`,
          contextPageIds: [kmId],
          pages: [
            {
              title: "知识管理",
              normalizedTitle: "知识管理",
              kind: "topic",
              summary: `第 ${round} 轮`,
              body: `第 ${round} 轮正文`,
              aliasesJson: null,
              linkTargets: [],
            },
          ],
        }),
      );
    }

    expect(wiki.listRevisions(kmId)).toHaveLength(10);
  });

  it("编译失败记录退避，成功后清零", () => {
    const first = wiki.recordCompilationFailure(itemId, "hash-x", 1_000);
    expect(first).toBe(1);
    let ingestion = wiki.listIngestions()[0];
    expect(ingestion).toMatchObject({
      failureCount: 1,
      nextAttemptAt: 1_000,
      // 空 promptVersion 表示尚未成功编译过，指纹判定仍视其为待编译
      promptVersion: "",
    });

    expect(wiki.recordCompilationFailure(itemId, "hash-x", 2_000)).toBe(2);
    expect(wiki.listIngestions()[0].failureCount).toBe(2);

    wiki.applyCompilation(compilationInput(itemId, { contentHash: "hash-x" }));
    ingestion = wiki.listIngestions()[0];
    expect(ingestion.failureCount).toBe(0);
    expect(ingestion.nextAttemptAt).toBeNull();
    expect(ingestion.promptVersion).toBe("wiki-compile-v1");
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

  it("getStatus 不把编译失败算成已编译", () => {
    // 失败也会写 wiki_ingestions 行（prompt_version 为空标记从未成功）。
    // 算进「已编译」的话，整轮全失败会被显示成「已编译 1/1」圆满完成
    wiki.recordCompilationFailure(itemId, "hash-x", 1_000);
    expect(wiki.getStatus()).toMatchObject({
      compiledItemCount: 0,
      eligibleItemCount: 1,
    });

    wiki.applyCompilation(compilationInput(itemId, { contentHash: "hash-x" }));
    expect(wiki.getStatus().compiledItemCount).toBe(1);
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

  it("searchPages 让中文提问也能命中页面", () => {
    wiki.applyCompilation(
      compilationInput(itemId, {
        pages: [
          {
            title: "音视频转写",
            normalizedTitle: "音视频转写",
            kind: "topic",
            summary: "把语音转成文字稿的完整链路",
            body: "先下载音轨，再转码，最后送给转写模型。",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );

    // 问句里没有「音视频转写」这五个字，旧的内存子串计分完全命不中
    const hits = wiki.searchPages("视频是怎么被转成文字的", 5);
    expect(hits.map((hit) => hit.title)).toContain("音视频转写");

    expect(wiki.searchPages("完全无关的主题", 5)).toEqual([]);
  });

  it("正文回滚后索引跟着回滚", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;

    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-9",
        contextPageIds: [kmId],
        pages: [
          {
            title: "知识管理",
            normalizedTitle: "知识管理",
            kind: "topic",
            summary: "换了内容",
            body: "现在讲的是量子力学",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );
    expect(wiki.searchPages("量子力学", 5)).toHaveLength(1);

    const revision = wiki.listRevisions(kmId)[0];
    expect(wiki.restoreRevision(revision.id)).toBe(true);
    // 索引不跟着回滚的话，搜「量子力学」还能搜到一个正文里已经没有它的页面
    expect(wiki.searchPages("量子力学", 5)).toHaveLength(0);
  });

  it("clearAll 后索引一并清空", () => {
    wiki.applyCompilation(compilationInput(itemId));
    expect(wiki.searchPages("知识管理", 5).length).toBeGreaterThan(0);
    wiki.clearAll();
    expect(wiki.searchPages("知识管理", 5)).toEqual([]);
  });

  it("backfillMissingFtsRows 补齐老库里没有索引的页面", () => {
    wiki.applyCompilation(compilationInput(itemId));
    // 模拟本表加入之前建的库：页面在，索引行不在
    db.run("DELETE FROM wiki_fts");
    expect(wiki.searchPages("知识管理", 5)).toEqual([]);

    expect(wiki.backfillMissingFtsRows()).toBe(2);
    expect(wiki.searchPages("知识管理", 5).length).toBeGreaterThan(0);
    // 已补齐后重复调用不再重写
    expect(wiki.backfillMissingFtsRows()).toBe(0);
  });

  it("手动编辑的正文挡得住下一轮编译", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;

    expect(
      wiki.updatePageBody({
        pageId: kmId,
        body: "我自己写的正文，参考 [[ELECTRON]]",
        linkTargets: ["ELECTRON"],
      }),
    ).toBe(true);
    expect(wiki.getPage(kmId)!.page.manualEditedAt).toBeTruthy();

    // 即便这一页进了本轮上下文，也不该被覆盖——否则改完活不过下一轮
    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-manual",
        contextPageIds: [kmId],
        pages: [
          {
            title: "知识管理",
            normalizedTitle: "知识管理",
            kind: "topic",
            summary: "模型重写的摘要",
            body: "模型重写的正文",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );
    expect(wiki.getPage(kmId)!.page.body).toBe("我自己写的正文，参考 [[ELECTRON]]");
    // 摘要仍然跟着更新——它不依赖正文
    expect(wiki.getPage(kmId)!.page.summary).toBe("模型重写的摘要");
  });

  it("手动编辑会按新正文重建出链", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    const electronId = wiki.findPageIdByNormalizedTitle("ELECTRON")!;
    expect(wiki.getPage(electronId)!.backlinks).toHaveLength(1);

    wiki.updatePageBody({ pageId: kmId, body: "去掉链接的正文", linkTargets: [] });
    expect(wiki.getPage(electronId)!.backlinks).toHaveLength(0);
  });

  it("交回自动编译后重新接受覆盖", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    wiki.updatePageBody({ pageId: kmId, body: "手写正文", linkTargets: [] });

    wiki.updatePageBody({
      pageId: kmId,
      body: "手写正文",
      linkTargets: [],
      releaseToAuto: true,
    });
    expect(wiki.getPage(kmId)!.page.manualEditedAt).toBeNull();

    wiki.applyCompilation(
      compilationInput(itemId, {
        contentHash: "hash-after-release",
        contextPageIds: [kmId],
        pages: [
          {
            title: "知识管理",
            normalizedTitle: "知识管理",
            kind: "topic",
            summary: "s",
            body: "重新由模型接管",
            aliasesJson: null,
            linkTargets: [],
          },
        ],
      }),
    );
    expect(wiki.getPage(kmId)!.page.body).toBe("重新由模型接管");
  });

  it("deletePage 只删这一页，索引与链接一并清掉", () => {
    wiki.applyCompilation(compilationInput(itemId));
    const kmId = wiki.findPageIdByNormalizedTitle("知识管理")!;
    const electronId = wiki.findPageIdByNormalizedTitle("ELECTRON")!;

    expect(wiki.deletePage(kmId)).toBe(true);
    expect(wiki.getPage(kmId)).toBeNull();
    // 另一页还在，指向被删页的链接由外键级联清掉
    expect(wiki.getPage(electronId)).not.toBeNull();
    expect(wiki.getPage(electronId)!.backlinks).toHaveLength(0);
    expect(wiki.searchPages("知识管理", 5)).toEqual([]);

    expect(wiki.deletePage("not-exist")).toBe(false);
  });

  it("getGraph 超出上限时按连接度截断并报总数", () => {
    // 造 5 个页面，其中一个被其余全部指向
    const pages = ["A", "B", "C", "D", "HUB"].map((title) => ({
      title,
      normalizedTitle: title,
      kind: "topic" as const,
      summary: "",
      body: title === "HUB" ? "" : "指向 [[HUB]]",
      aliasesJson: null,
      linkTargets: title === "HUB" ? [] : ["HUB"],
    }));
    wiki.applyCompilation(compilationInput(itemId, { pages }));

    const full = wiki.getGraph();
    expect(full.totalNodes).toBe(5);
    expect(full.nodes).toHaveLength(5);

    const capped = wiki.getGraph(2);
    expect(capped.totalNodes).toBe(5);
    expect(capped.nodes).toHaveLength(2);
    // 连接度最高的 HUB 必须留下
    expect(capped.nodes.map((node) => node.title)).toContain("HUB");
    // 边的两端都要在可见集合里，否则 force-graph 会因找不到节点报错
    const visible = new Set(capped.nodes.map((node) => node.id));
    for (const link of capped.links) {
      expect(visible.has(link.source)).toBe(true);
      expect(visible.has(link.target)).toBe(true);
    }
  });

  it("findPageIdByNormalizedTitle 精确定位", () => {
    wiki.applyCompilation(compilationInput(itemId));
    expect(wiki.findPageIdByNormalizedTitle("知识管理")).toBeTruthy();
    expect(wiki.findPageIdByNormalizedTitle("不存在")).toBeNull();
  });
});
