import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { AskSessionDB } from "@guizhi/db/ask-session";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe("AskSessionDB", () => {
  let db: DatabaseAdapter.Database;
  let sessions: AskSessionDB;

  beforeEach(() => {
    db = createTestDb();
    sessions = new AskSessionDB(db);
  });

  it("save 创建会话并可完整读回消息 JSON", () => {
    const messagesJson = JSON.stringify([
      { id: "m1", question: "归知是什么？", answer: "本地优先知识库", status: "done" },
    ]);
    const saved = sessions.save({
      id: "s1",
      title: "归知是什么？",
      messagesJson,
    });

    expect(saved.title).toBe("归知是什么？");
    const loaded = sessions.get("s1");
    expect(loaded?.messagesJson).toBe(messagesJson);
    expect(loaded?.createdAt).toBeGreaterThan(0);
  });

  it("save 对已有会话是 upsert：更新内容并保留 created_at", async () => {
    const first = sessions.save({ id: "s1", title: "旧标题", messagesJson: "[]" });
    await sleep(5);
    const second = sessions.save({
      id: "s1",
      title: "新标题",
      messagesJson: '[{"id":"m1"}]',
    });

    expect(second.createdAt).toBe(first.createdAt);
    expect(second.updatedAt).toBeGreaterThanOrEqual(first.updatedAt);
    expect(sessions.list()).toHaveLength(1);
    expect(sessions.get("s1")?.title).toBe("新标题");
  });

  it("list 按更新时间倒序返回元数据（不含消息体）", async () => {
    sessions.save({ id: "old", title: "旧会话", messagesJson: "[]" });
    await sleep(5);
    sessions.save({ id: "new", title: "新会话", messagesJson: "[]" });

    const metas = sessions.list();
    expect(metas.map((meta) => meta.id)).toEqual(["new", "old"]);
    expect(
      (metas[0] as unknown as Record<string, unknown>).messagesJson,
    ).toBeUndefined();
  });

  it("delete 移除会话", () => {
    sessions.save({ id: "s1", title: "t", messagesJson: "[]" });
    expect(sessions.delete("s1")).toBe(true);
    expect(sessions.delete("s1")).toBe(false);
    expect(sessions.get("s1")).toBeNull();
  });
});
