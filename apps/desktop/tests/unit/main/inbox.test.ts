import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { ImportTaskDB } from "@guizhi/db";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import {
  acknowledgeInboxImportWarning,
  applyInboxAiClassification,
  listInboxAiClassificationSources,
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

  it("返回主进程可精确判断的待办，Wiki 状态留给编译器补入", () => {
    const result = listInboxItems(db);
    expect(result.counts).toMatchObject({
      "review-required": 1,
      unclassified: 1,
      "import-issue": 1,
      "discovery-candidate": 1,
      "semantic-pending": 1,
      "wiki-pending": 0,
    });
    expect(result.items.map((item) => item.kind)).toEqual(
      expect.arrayContaining([
        "review-required",
        "unclassified",
        "import-issue",
        "discovery-candidate",
        "semantic-pending",
      ]),
    );
    expect(result.items.some((item) => item.kind === "wiki-pending")).toBe(
      false,
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
      {
        id: "unclassified-1",
        collection_id: "collection-1",
        review_status: "clear",
      },
    ]);
    expect(
      (
        db.get("SELECT COUNT(*) AS count FROM knowledge_item_tags") as {
          count: number;
        }
      ).count,
    ).toBe(2);
  });

  it("已知悉的完成任务保留警告原文但不再进入处理中心，新警告会重新出现", () => {
    const now = Date.now();
    db.run(
      `INSERT INTO import_tasks
       (id, source_kind, source_input, display_name, status, warning,
        capture_strategy, comment_limit, force_duplicate, created_at, updated_at)
       VALUES (?, 'url', ?, ?, 'completed', ?, 'standard', 0, 0, ?, ?)`,
      "task-warning",
      "https://example.com/warning",
      "有缺失的导入",
      "1 张附件图下载失败，已保留外链",
      now,
      now,
    );

    expect(
      listInboxItems(db).items.some(
        (item) =>
          item.kind === "import-issue" && item.taskId === "task-warning",
      ),
    ).toBe(true);
    expect(acknowledgeInboxImportWarning(db, "task-warning")).toBe(1);

    const acknowledged = db.get(
      "SELECT warning, warning_acknowledged_at FROM import_tasks WHERE id = ?",
      "task-warning",
    ) as { warning: string; warning_acknowledged_at: number | null };
    expect(acknowledged.warning).toContain("附件图下载失败");
    expect(acknowledged.warning_acknowledged_at).toEqual(expect.any(Number));
    expect(
      listInboxItems(db).items.some(
        (item) =>
          item.kind === "import-issue" && item.taskId === "task-warning",
      ),
    ).toBe(false);
    expect(acknowledgeInboxImportWarning(db, "task-warning")).toBe(0);

    new ImportTaskDB(db).update("task-warning", { warning: "新的采集警告" });
    expect(
      db.get(
        "SELECT warning_acknowledged_at FROM import_tasks WHERE id = ?",
        "task-warning",
      ),
    ).toEqual({ warning_acknowledged_at: null });
    expect(
      listInboxItems(db).items.some(
        (item) =>
          item.kind === "import-issue" && item.taskId === "task-warning",
      ),
    ).toBe(true);
  });

  it("AI 归类优先复用现有知识库，原子创建缺失分类并跳过已变更条目", () => {
    const now = Date.now();
    db.run(
      "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
      "unclassified-2",
      "健康饮食记录",
      "关于均衡饮食与睡眠的笔记",
      now,
      now,
    );
    db.run(
      "INSERT INTO knowledge_items (id, title, content, collection_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
      "already-classified",
      "已手动分类",
      "不应被 AI 覆盖",
      "collection-1",
      now,
      now,
    );

    expect(
      listInboxAiClassificationSources(db, [
        "review-1",
        "unclassified-1",
        "unclassified-2",
        "already-classified",
      ]).map((item) => item.itemId),
    ).toEqual(["unclassified-1", "unclassified-2"]);

    const result = applyInboxAiClassification(db, {
      assignments: [
        { itemId: "unclassified-1", collectionName: " 收件箱 " },
        { itemId: "unclassified-2", collectionName: "健康养护" },
        { itemId: "already-classified", collectionName: "健康养护" },
      ],
    });

    expect(result).toEqual({
      classified: 2,
      skipped: 1,
      createdCollectionNames: ["健康养护"],
    });
    const collections = db.all(
      "SELECT id, name FROM collections ORDER BY created_at, id",
    ) as Array<{ id: string; name: string }>;
    expect(collections.map((collection) => collection.name)).toEqual([
      "收件箱",
      "健康养护",
    ]);
    const collectionByName = new Map(
      collections.map((collection) => [collection.name, collection.id]),
    );
    expect(
      db.all(
        "SELECT id, collection_id FROM knowledge_items WHERE id IN ('unclassified-1','unclassified-2','already-classified') ORDER BY id",
      ),
    ).toEqual([
      { id: "already-classified", collection_id: "collection-1" },
      {
        id: "unclassified-1",
        collection_id: collectionByName.get("收件箱"),
      },
      {
        id: "unclassified-2",
        collection_id: collectionByName.get("健康养护"),
      },
    ]);
  });
});
