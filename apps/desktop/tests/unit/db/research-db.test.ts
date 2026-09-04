import { describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { ResearchDB } from "@guizhi/db/research";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";

function createDb() {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("ResearchDB", () => {
  it("按研究精确去重候选，并级联删除研究数据", () => {
    const db = createDb();
    const store = new ResearchDB(db);
    const run = store.create(
      { topic: "本地 AI", dayRange: 30, depth: "quick", sources: ["douyin", "bilibili"] },
      1_000,
      2_000,
      500,
    );
    const candidate = {
      source: "douyin" as const,
      externalId: "d-1",
      url: "https://www.douyin.com/video/d-1?utm_source=test",
      title: "本地 AI 知识库",
      author: "作者",
      mediaType: "video" as const,
      discoveryMethod: "fixture",
    };
    expect(store.upsertCandidate(run.id, candidate, "https://www.douyin.com/video/d-1", 600)).toBe(true);
    expect(store.upsertCandidate(run.id, { ...candidate, title: "另一个标题" }, "https://www.douyin.com/video/d-1", 700)).toBe(false);
    expect(store.getDetail(run.id)?.run.candidateCount).toBe(1);
    expect(store.delete(run.id)).toBe(true);
    expect(db.get("SELECT COUNT(*) AS count FROM research_candidates")).toEqual({ count: 0 });
    expect(db.get("SELECT COUNT(*) AS count FROM research_source_runs")).toEqual({ count: 0 });
    db.close();
  });

  it("恢复中断状态、保留成功报告，并且删除研究不删除已保存知识", () => {
    const db = createDb();
    const store = new ResearchDB(db);
    const run = store.create(
      { topic: "知识管理", dayRange: 7, depth: "deep", sources: ["xiaohongshu"] },
      1,
      2,
      10,
    );
    store.saveReport(run.id, "旧报告 [R1]", "v1", 11);
    store.beginReport(run.id, 12);
    const item = new KnowledgeItemDB(db).create({ title: "报告", content: "正文", itemType: "note" });
    store.setSavedItem(run.id, item.id, 13);

    store.recoverInterrupted(20);
    expect(store.get(run.id)).toMatchObject({
      status: "canceled",
      reportStatus: "failed",
      reportMarkdown: "旧报告 [R1]",
      savedItemId: item.id,
    });
    store.delete(run.id);
    expect(new KnowledgeItemDB(db).get(item.id)?.title).toBe("报告");
    db.close();
  });
});
