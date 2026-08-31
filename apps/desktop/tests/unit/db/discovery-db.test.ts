import { describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { DiscoveryDB } from "@guizhi/db/discovery";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import type { PlatformDiscoveryItem } from "@guizhi/shared/types";

function createDb() {
  const db = new DatabaseAdapter(":memory:");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

function item(externalId: string, title = externalId): PlatformDiscoveryItem {
  return {
    platform: "douyin",
    externalId,
    url: `https://www.douyin.com/video/${externalId}`,
    title,
    author: "作者",
    mediaType: "video",
    publishedAt: 100,
  };
}

describe("DiscoveryDB", () => {
  it("保存视图、运行游标与候选状态", () => {
    const store = new DiscoveryDB(createDb());
    const view = store.saveView(
      {
        name: "每日关注",
        platform: "douyin",
        mode: "keyword",
        query: "知识管理",
        intervalMinutes: 1440,
        enabled: true,
      },
      50_000,
      1_000,
    );
    expect(view).toMatchObject({ enabled: true, nextRunAt: 50_000 });

    const run = store.beginRun(view.id, "20", 2_000);
    store.updateRun(run.id, { cursor: "40", pagesScanned: 1 }, false, 3_000);
    expect(store.getResumableRun(view.id)).toMatchObject({ cursor: "40" });

    expect(store.upsertCandidate(view.id, item("1"), 4_000)).toBe(true);
    expect(store.upsertCandidate(view.id, item("1"), 5_000)).toBe(false);
    expect(store.listCandidates(view.id, "new")).toHaveLength(1);
    expect(store.setCandidateState("douyin", "1", "dismissed")).toBe(true);
    expect(store.listCandidates(view.id)[0]).toMatchObject({ state: "dismissed" });
  });

  it("平台 ID 变化但内容哈希相同时更新同一候选", () => {
    const store = new DiscoveryDB(createDb());
    const firstView = store.saveView(
      { name: "A", platform: "douyin", mode: "keyword", query: "a" },
      null,
      1,
    );
    const secondView = store.saveView(
      { name: "B", platform: "douyin", mode: "keyword", query: "b" },
      null,
      2,
    );
    expect(store.upsertCandidate(firstView.id, item("old", "相同作品"), 10)).toBe(true);
    expect(store.upsertCandidate(secondView.id, item("new", "相同作品"), 20)).toBe(false);
    expect(store.listCandidates(firstView.id)).toHaveLength(0);
    expect(store.listCandidates(secondView.id)[0]).toMatchObject({ externalId: "new" });
  });

  it("只清理 90 天前已忽略候选，保留未处理候选", () => {
    const store = new DiscoveryDB(createDb());
    const view = store.saveView(
      { name: "A", platform: "douyin", mode: "keyword", query: "a" },
      null,
      1,
    );
    store.upsertCandidate(view.id, item("old"), 1);
    store.setCandidateState("douyin", "old", "dismissed");
    store.upsertCandidate(view.id, item("new"), 1);
    store.prune(91 * 24 * 60 * 60_000);
    expect(store.listCandidates(view.id).map((entry) => entry.externalId)).toEqual(["new"]);
  });
});
