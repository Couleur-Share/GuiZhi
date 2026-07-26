import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import type { ImportTask } from "@guizhi/shared/types";
import { createImportService } from "../../../src/main/services/import/import-service";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("ImportService（真实 DB 集成）", () => {
  let db: DatabaseAdapter.Database;
  let events: ImportTask[];
  let service: ReturnType<typeof createImportService>;

  beforeEach(() => {
    db = createTestDb();
    events = [];
    service = createImportService(db, (task) => events.push(task));
  });

  it("文本导入端到端：入库为收件箱条目并写来源记录", async () => {
    service.queue.enqueue([
      { kind: "text", input: "会议纪要\n下周一发布 v0.3" },
    ]);
    await service.queue.drain();

    const [task] = service.taskDb.list();
    expect(task.status).toBe("completed");
    expect(task.resultItemId).toBeTruthy();

    const items = new KnowledgeItemDB(db);
    const item = items.get(task.resultItemId!);
    expect(item?.title).toBe("会议纪要");
    expect(item?.status).toBe("inbox");
    expect(item?.itemType).toBe("note");

    const source = db.get(
      "SELECT * FROM source_records WHERE item_id = ?",
      task.resultItemId,
    ) as { source_type: string; content_hash: string } | undefined;
    expect(source?.source_type).toBe("text");
    expect(source?.content_hash).toBeTruthy();
  });

  it("相同文本再次导入：内容哈希去重命中", async () => {
    service.queue.enqueue([{ kind: "text", input: "重复的内容" }]);
    await service.queue.drain();
    const first = service.taskDb.list()[0];
    expect(first.status).toBe("completed");

    service.queue.enqueue([{ kind: "text", input: "重复的内容" }]);
    await service.queue.drain();

    const second = service.taskDb
      .list()
      .find((candidate) => candidate.id !== first.id)!;
    expect(second.status).toBe("duplicate");
    expect(second.duplicateItemId).toBe(first.resultItemId);
  });

  it("原条目移入回收站后重新导入不算重复", async () => {
    service.queue.enqueue([{ kind: "text", input: "先删再导" }]);
    await service.queue.drain();
    const first = service.taskDb.list()[0];

    const items = new KnowledgeItemDB(db);
    items.moveToTrash([first.resultItemId!]);

    service.queue.enqueue([{ kind: "text", input: "先删再导" }]);
    await service.queue.drain();

    const second = service.taskDb
      .list()
      .find((candidate) => candidate.id !== first.id)!;
    expect(second.status).toBe("completed");
    expect(second.resultItemId).not.toBe(first.resultItemId);
  });

  it("采集时选的标签直接落到入库条目上", async () => {
    service.queue.enqueue([
      { kind: "text", input: "带标签的内容", tagNames: ["读书", "待读"] },
    ]);
    await service.queue.drain();

    const [task] = service.taskDb.list();
    expect(task.status).toBe("completed");

    // 标签要穿过「任务持久化 → 队列调度 → saveItem」整条链路
    const items = new KnowledgeItemDB(db);
    const names = items.get(task.resultItemId!)!.tags.map((tag) => tag.name);
    expect(new Set(names)).toEqual(new Set(["读书", "待读"]));
  });

  it("重启恢复后仍能打上采集时选的标签", async () => {
    // 任务入队但不跑完，模拟「下载途中退出应用」
    const [pending] = service.queue.enqueue([
      { kind: "text", input: "重启前入队", tagNames: ["稍后读"] },
    ]);
    await service.queue.drain();

    // 标签存在任务行里，而不只是活在渲染进程的内存里
    expect(service.taskDb.get(pending.id)!.tagNames).toEqual(["稍后读"]);

    const items = new KnowledgeItemDB(db);
    const itemId = service.taskDb.get(pending.id)!.resultItemId!;
    expect(items.get(itemId)!.tags.map((tag) => tag.name)).toEqual(["稍后读"]);
  });

  it("指定知识库：条目归入目标 collection", async () => {
    db.run(
      "INSERT INTO collections (id, name, sort_order, created_at, updated_at) VALUES ('c1', '技术', 0, 1, 1)",
    );
    service.queue.enqueue([
      { kind: "text", input: "归类内容", collectionId: "c1" },
    ]);
    await service.queue.drain();

    const [task] = service.taskDb.list();
    const items = new KnowledgeItemDB(db);
    expect(items.get(task.resultItemId!)?.collectionId).toBe("c1");
  });
});
