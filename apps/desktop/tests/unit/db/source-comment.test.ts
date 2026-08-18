import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SourceCommentDB } from "@guizhi/db/source-comment";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";

describe("SourceCommentDB", () => {
  let db: DatabaseAdapter.Database;
  let comments: SourceCommentDB;

  beforeEach(() => {
    db = new DatabaseAdapter(":memory:");
    db.pragma("foreign_keys = ON");
    db.exec(SCHEMA_TABLES);
    db.exec(SCHEMA_INDEXES);
    const now = Date.now();
    db.run(
      "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
      "item-1", "测试", "正文", now, now,
    );
    comments = new SourceCommentDB(db);
  });

  it("按条目、平台和评论 ID 更新而不是重复追加", () => {
    comments.upsertMany([{
      itemId: "item-1", platform: "douyin", externalId: "comment-1",
      authorName: "甲", content: "旧内容", likeCount: 1,
    }]);
    comments.upsertMany([{
      itemId: "item-1", platform: "douyin", externalId: "comment-1",
      authorName: "甲", content: "新内容", likeCount: 8,
    }]);
    expect(comments.list("item-1")).toHaveLength(1);
    expect(comments.list("item-1")[0]).toMatchObject({ content: "新内容", likeCount: 8 });
  });

  it("条目删除时评论级联删除", () => {
    comments.upsertMany([{
      itemId: "item-1", platform: "xiaohongshu", externalId: "comment-1",
      authorName: "乙", content: "评论",
    }]);
    db.run("DELETE FROM knowledge_items WHERE id = ?", "item-1");
    expect(comments.list("item-1")).toEqual([]);
  });
});
