import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { ImportTaskDB } from "@guizhi/db/import-task";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("ImportTaskDB", () => {
  let db: DatabaseAdapter.Database;
  let tasks: ImportTaskDB;

  beforeEach(() => {
    db = createTestDb();
    tasks = new ImportTaskDB(db);
  });

  it("创建任务：文本取首行做显示名", () => {
    const task = tasks.create({
      kind: "text",
      input: "第一行标题\n第二行内容",
    });
    expect(task.status).toBe("pending");
    expect(task.displayName).toBe("第一行标题");
    expect(task.sourceKind).toBe("text");
  });

  it("创建任务：文件取文件名做显示名", () => {
    const task = tasks.create({
      kind: "file",
      input: "C:\\Users\\me\\docs\\读书笔记.md",
    });
    expect(task.displayName).toBe("读书笔记.md");
  });

  it("阶段统计往返：JSON 存取不丢字段", () => {
    const task = tasks.create({ kind: "url", input: "https://example.com" });
    const updated = tasks.update(task.id, {
      stageStats: [
        { stage: "transcribing", ms: 126_000 },
        {
          stage: "formatting",
          ms: 493_000,
          calls: 7,
          failedCalls: 2,
          promptTokens: 9_500,
          completionTokens: 57_900,
          models: ["qwen3.5-flash"],
        },
      ],
    })!;

    expect(updated.stageStats).toHaveLength(2);
    expect(updated.stageStats![1]).toEqual({
      stage: "formatting",
      ms: 493_000,
      calls: 7,
      failedCalls: 2,
      promptTokens: 9_500,
      completionTokens: 57_900,
      models: ["qwen3.5-flash"],
    });
    expect(tasks.get(task.id)!.stageStats).toEqual(updated.stageStats);
  });

  it("阶段统计：新建为空、传 null 清空、不传则保留", () => {
    const task = tasks.create({ kind: "url", input: "https://example.com" });
    expect(task.stageStats).toBeNull();

    tasks.update(task.id, { stageStats: [{ stage: "fetching", ms: 100 }] });
    // 不带这个字段的更新（改状态、写标题）不该把统计抹掉
    expect(tasks.update(task.id, { status: "failed" })!.stageStats).toEqual([
      { stage: "fetching", ms: 100 },
    ]);
    // 重试走的是显式 null
    expect(tasks.update(task.id, { stageStats: null })!.stageStats).toBeNull();
  });

  it("坏掉的统计不连累整行：读不出来当没有，而不是让列表报错", () => {
    const task = tasks.create({ kind: "url", input: "https://example.com" });
    db.run("UPDATE import_tasks SET stage_stats = ? WHERE id = ?", "{坏", task.id);
    expect(tasks.get(task.id)!.stageStats).toBeNull();
    expect(tasks.get(task.id)!.status).toBe("pending");
  });

  it("更新状态与去重结果", () => {
    const task = tasks.create({ kind: "url", input: "https://example.com" });
    const updated = tasks.update(task.id, {
      status: "duplicate",
      duplicateItemId: "item-1",
    })!;
    expect(updated.status).toBe("duplicate");
    expect(updated.duplicateItemId).toBe("item-1");
  });

  it("回写真实标题与条目类型", () => {
    const task = tasks.create({
      kind: "url",
      input: "https://www.v2ex.com/t/1223399#reply147",
    });
    expect(task.displayName).toBe("https://www.v2ex.com/t/1223399#reply147");
    expect(task.itemType).toBeNull();

    const updated = tasks.update(task.id, {
      displayName: "为什么 SQLite 不适合做队列",
      itemType: "forum",
      status: "completed",
    })!;
    expect(updated.displayName).toBe("为什么 SQLite 不适合做队列");
    expect(updated.itemType).toBe("forum");
  });

  it("空标题不覆盖既有显示名", () => {
    const task = tasks.create({ kind: "url", input: "https://example.com" });
    // 抽取降级时 title 可能是空串，覆盖过去列表上就只剩一行空白
    tasks.update(task.id, { displayName: "   " });
    expect(tasks.get(task.id)!.displayName).toBe("https://example.com");
  });

  it("forceDuplicate 标志读写", () => {
    const task = tasks.create({
      kind: "text",
      input: "内容",
      forceDuplicate: true,
    });
    expect(tasks.isForceDuplicate(task.id)).toBe(true);

    tasks.update(task.id, { forceDuplicate: false });
    expect(tasks.isForceDuplicate(task.id)).toBe(false);
  });

  it("resetProcessingToPending 只复位 processing", () => {
    const running = tasks.create({ kind: "text", input: "运行中" });
    const done = tasks.create({ kind: "text", input: "已完成" });
    tasks.update(running.id, { status: "processing", stage: "fetching" });
    tasks.update(done.id, { status: "completed" });

    const changed = tasks.resetProcessingToPending();
    expect(changed).toBe(1);
    expect(tasks.get(running.id)!.status).toBe("pending");
    expect(tasks.get(running.id)!.stage).toBeNull();
    expect(tasks.get(done.id)!.status).toBe("completed");
  });

  it("采集时选定的标签随任务持久化", () => {
    const task = tasks.create({
      kind: "url",
      input: "https://example.com",
      tagNames: ["读书", "待读"],
    });
    // 重启恢复后要还能读出来，所以走一次 get 而不是直接看返回值
    expect(tasks.get(task.id)!.tagNames).toEqual(["读书", "待读"]);
    expect(tasks.create({ kind: "text", input: "无标签" }).tagNames).toEqual([]);
  });

  it("remove 删得掉失败任务，删不掉进行中的", () => {
    const failed = tasks.create({ kind: "text", input: "失败" });
    const running = tasks.create({ kind: "text", input: "进行中" });
    tasks.update(failed.id, { status: "failed" });
    tasks.update(running.id, { status: "processing" });

    // failed 有意不进「清理已完成」，所以必须有单独的删除出口
    expect(tasks.remove(failed.id)).toBe(true);
    expect(tasks.get(failed.id)).toBeNull();
    // 正在跑的任务删掉会让队列失去落点
    expect(tasks.remove(running.id)).toBe(false);
    expect(tasks.get(running.id)).not.toBeNull();
  });

  it("clearFinished 清掉终态任务，保留进行中的与失败的", () => {
    const pending = tasks.create({ kind: "text", input: "待处理" });
    const failed = tasks.create({ kind: "text", input: "失败" });
    const duplicate = tasks.create({ kind: "text", input: "重复" });
    const completed = tasks.create({ kind: "text", input: "已完成" });
    tasks.update(failed.id, { status: "failed", error: "err" });
    tasks.update(duplicate.id, { status: "duplicate" });
    tasks.update(completed.id, { status: "completed" });

    const removed = tasks.clearFinished();
    expect(removed).toBe(2);
    // failed 必须留下：它保存着原始输入与失败原因，是用户唯一的重试入口
    const remainingIds = tasks.list().map((task) => task.id);
    expect(remainingIds).toHaveLength(2);
    expect(remainingIds).toContain(pending.id);
    expect(remainingIds).toContain(failed.id);
  });

  it("listByStatus 按状态过滤", () => {
    tasks.create({ kind: "text", input: "甲" });
    const second = tasks.create({ kind: "text", input: "乙" });
    tasks.update(second.id, { status: "failed" });

    expect(tasks.listByStatus(["pending"])).toHaveLength(1);
    expect(tasks.listByStatus(["failed"])).toHaveLength(1);
    expect(tasks.listByStatus(["pending", "failed"])).toHaveLength(2);
  });
});
