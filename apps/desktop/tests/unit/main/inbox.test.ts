import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import {
  listInboxItems,
  markInboxItemsReviewed,
  organizeInboxItems,
} from "../../../src/main/services/inbox";

describe("处理中心聚合", () => {
  let db: DatabaseAdapter.Database;

  beforeEach(() => {
    db = new DatabaseAdapter(":memory:");
    db.exec(SCHEMA_TABLES);
    db.exec(SCHEMA_INDEXES);
    const now = Date.now();
    db.run(
      "INSERT INTO collections (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)",
      "collection-1",
      "收件箱",
      now,
      now,
    );
    db.run(
      "INSERT INTO knowledge_items (id, title, content, review_status, review_reasons, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
      "review-1",
      "需要复核",
      "正文",
      "needs_review",
      JSON.stringify(["转写失败"]),
      now,
      now,
    );
    db.run(
      "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
      "unclassified-1",
      "未分类",
      "正文",
      now - 1,
      now - 1,
    );
    db.run(
      `INSERT INTO import_tasks
       (id, source_kind, source_input, display_name, status, error, capture_strategy, comment_limit, force_duplicate, created_at, updated_at)
       VALUES (?, 'url', ?, ?, 'failed', ?, 'standard', 0, 0, ?, ?)`,
      "task-1",
      "https://example.com",
      "失败任务",
      "网络失败",
      now,
      now,
    );
    db.run(
      `INSERT INTO platform_discovery_views
       (id, name, platform, mode, query, interval_minutes, enabled, state, created_at, updated_at)
       VALUES (?, ?, 'douyin', 'keyword', ?, 1440, 1, 'ready', ?, ?)`,
      "view-1",
      "发现视图",
      "AI",
      now,
      now,
    );
    db.run(
      `INSERT INTO platform_discovery_candidates
       (view_id, platform, external_id, item_json, state, first_seen_at, last_seen_at)
       VALUES (?, 'douyin', ?, ?, 'new', ?, ?)`,
      "view-1",
      "external-1",
      JSON.stringify({ title: "新候选" }),
      now,
      now,
    );
  });

  it("把复核、未分类、导入问题、候选与两个聚合状态统一返回", () => {
    const result = listInboxItems(db);
    expect(result.counts).toMatchObject({
      "review-required": 1,
      unclassified: 1,
      "import-issue": 1,
      "discovery-candidate": 1,
      "semantic-pending": 1,
      "wiki-pending": 1,
    });
    expect(result.items.map((item) => item.kind)).toEqual(
      expect.arrayContaining([
        "review-required",
        "unclassified",
        "import-issue",
        "discovery-candidate",
        "semantic-pending",
        "wiki-pending",
      ]),
    );
  });

  it("批量归知识库、打标签并标记已复核", () => {
    expect(
      organizeInboxItems(db, {
        itemIds: ["review-1", "unclassified-1"],
        collectionId: "collection-1",
        addTagNames: ["待整理"],
      }),
    ).toBe(2);
    expect(markInboxItemsReviewed(db, ["review-1"])).toBe(1);

    const rows = db.all(
      "SELECT id, collection_id, review_status FROM knowledge_items ORDER BY id",
    );
    expect(rows).toEqual([
      { id: "review-1", collection_id: "collection-1", review_status: "clear" },
      { id: "unclassified-1", collection_id: "collection-1", review_status: "clear" },
    ]);
    expect(
      (db.get("SELECT COUNT(*) AS count FROM knowledge_item_tags") as { count: number }).count,
    ).toBe(2);
  });
});
