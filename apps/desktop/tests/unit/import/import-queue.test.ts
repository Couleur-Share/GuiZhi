import { describe, expect, it, vi } from "vitest";
import { randomUUID } from "crypto";
import type {
  EnqueueImportInput,
  ImportTask,
} from "@guizhi/shared/types";
import {
  ImportQueue,
  type ImportPersistence,
  type ImportTaskStore,
} from "../../../src/main/services/import/import-queue";
import type { ExtractedContent } from "../../../src/main/services/import/connectors";

/** 内存版任务存储：模拟 ImportTaskDB 行为 */
function createMemoryStore(): ImportTaskStore & { rows: Map<string, ImportTask & { forceDuplicate: boolean }> } {
  const rows = new Map<string, ImportTask & { forceDuplicate: boolean }>();
  return {
    rows,
    create(input: EnqueueImportInput) {
      const now = Date.now();
      const task: ImportTask & { forceDuplicate: boolean } = {
        id: randomUUID(),
        sourceKind: input.kind,
        sourceInput: input.input,
        displayName: input.input.slice(0, 60),
        status: "pending",
        stage: null,
        error: null,
        resultItemId: null,
        duplicateItemId: null,
        collectionId: input.collectionId ?? null,
        forceDuplicate: input.forceDuplicate ?? false,
        createdAt: now,
        updatedAt: now,
      };
      rows.set(task.id, task);
      return { ...task };
    },
    get(id) {
      const row = rows.get(id);
      return row ? { ...row } : null;
    },
    isForceDuplicate(id) {
      return rows.get(id)?.forceDuplicate ?? false;
    },
    list() {
      return [...rows.values()].map((row) => ({ ...row }));
    },
    update(id, patch) {
      const row = rows.get(id);
      if (!row) {
        return null;
      }
      if (patch.status !== undefined) row.status = patch.status;
      if (patch.stage !== undefined) row.stage = patch.stage;
      if (patch.error !== undefined) row.error = patch.error;
      if (patch.resultItemId !== undefined) row.resultItemId = patch.resultItemId;
      if (patch.duplicateItemId !== undefined)
        row.duplicateItemId = patch.duplicateItemId;
      if (patch.forceDuplicate !== undefined)
        row.forceDuplicate = patch.forceDuplicate;
      row.updatedAt = Date.now();
      return { ...row };
    },
    resetProcessingToPending() {
      let changed = 0;
      for (const row of rows.values()) {
        if (row.status === "processing") {
          row.status = "pending";
          row.stage = null;
          changed++;
        }
      }
      return changed;
    },
  };
}

function fakeExtracted(content: string): ExtractedContent {
  return { title: content.slice(0, 20), content, itemType: "note", sourceUri: null };
}

interface Harness {
  store: ReturnType<typeof createMemoryStore>;
  queue: ImportQueue;
  savedItems: string[];
  events: ImportTask[];
}

function createHarness(options?: {
  extract?: (
    kind: ImportTask["sourceKind"],
    input: string,
    signal: AbortSignal,
  ) => Promise<ExtractedContent>;
  findDuplicate?: ImportPersistence["findDuplicate"];
  concurrency?: number;
}): Harness {
  const store = createMemoryStore();
  const savedItems: string[] = [];
  const events: ImportTask[] = [];
  const queue = new ImportQueue({
    store,
    persistence: {
      findDuplicate: options?.findDuplicate ?? (() => null),
      saveItem({ extracted }) {
        const id = `item-${savedItems.length + 1}`;
        savedItems.push(extracted.content);
        return id;
      },
    },
    extract:
      options?.extract ?? (async (_kind, input) => fakeExtracted(input)),
    onTaskChanged: (task) => events.push(task),
    concurrency: options?.concurrency,
  });
  return { store, queue, savedItems, events };
}

describe("ImportQueue", () => {
  it("任务成功走完：pending → processing → completed 且写入条目", async () => {
    const harness = createHarness();
    const [task] = harness.queue.enqueue([{ kind: "text", input: "第一条" }]);
    await harness.queue.drain();

    const finished = harness.store.get(task.id)!;
    expect(finished.status).toBe("completed");
    expect(finished.resultItemId).toBe("item-1");
    expect(harness.savedItems).toEqual(["第一条"]);
    // 状态流转事件按序广播
    const statuses = harness.events
      .filter((event) => event.id === task.id)
      .map((event) => event.status);
    expect(statuses[0]).toBe("pending");
    expect(statuses[statuses.length - 1]).toBe("completed");
  });

  it("并发不超过上限", async () => {
    let running = 0;
    let peak = 0;
    const harness = createHarness({
      concurrency: 2,
      extract: async (_kind, input) => {
        running++;
        peak = Math.max(peak, running);
        await new Promise((resolve) => setTimeout(resolve, 20));
        running--;
        return fakeExtracted(input);
      },
    });
    harness.queue.enqueue(
      ["a", "b", "c", "d", "e"].map((input) => ({
        kind: "text" as const,
        input,
      })),
    );
    await harness.queue.drain();

    expect(peak).toBe(2);
    expect(harness.savedItems).toHaveLength(5);
  });

  it("去重命中：标记 duplicate 并携带已有条目 id，不入库", async () => {
    const harness = createHarness({
      findDuplicate: () => "existing-item",
    });
    const [task] = harness.queue.enqueue([{ kind: "text", input: "重复内容" }]);
    await harness.queue.drain();

    const finished = harness.store.get(task.id)!;
    expect(finished.status).toBe("duplicate");
    expect(finished.duplicateItemId).toBe("existing-item");
    expect(harness.savedItems).toHaveLength(0);
  });

  it("仍要创建副本：retry(forceDuplicate) 跳过去重", async () => {
    const harness = createHarness({
      findDuplicate: () => "existing-item",
    });
    const [task] = harness.queue.enqueue([{ kind: "text", input: "重复内容" }]);
    await harness.queue.drain();
    expect(harness.store.get(task.id)!.status).toBe("duplicate");

    harness.queue.retry(task.id, { forceDuplicate: true });
    await harness.queue.drain();

    const finished = harness.store.get(task.id)!;
    expect(finished.status).toBe("completed");
    expect(harness.savedItems).toEqual(["重复内容"]);
  });

  it("失败任务记录错误信息，可重试成功", async () => {
    let attempts = 0;
    const harness = createHarness({
      extract: async (_kind, input) => {
        attempts++;
        if (attempts === 1) {
          throw new Error("网络错误");
        }
        return fakeExtracted(input);
      },
    });
    const [task] = harness.queue.enqueue([{ kind: "text", input: "内容" }]);
    await harness.queue.drain();
    expect(harness.store.get(task.id)!.status).toBe("failed");
    expect(harness.store.get(task.id)!.error).toBe("网络错误");

    harness.queue.retry(task.id);
    await harness.queue.drain();
    expect(harness.store.get(task.id)!.status).toBe("completed");
  });

  it("取消运行中的任务", async () => {
    const harness = createHarness({
      extract: async (_kind, input, signal) => {
        await new Promise((resolve, reject) => {
          const timer = setTimeout(resolve, 1000);
          signal.addEventListener("abort", () => {
            clearTimeout(timer);
            reject(new Error("已取消"));
          });
        });
        return fakeExtracted(input);
      },
    });
    const [task] = harness.queue.enqueue([{ kind: "text", input: "长任务" }]);
    await vi.waitFor(() => {
      expect(harness.store.get(task.id)!.status).toBe("processing");
    });

    harness.queue.cancel(task.id);
    await harness.queue.drain();

    expect(harness.store.get(task.id)!.status).toBe("canceled");
    expect(harness.savedItems).toHaveLength(0);
  });

  it("取消排队中的任务", async () => {
    const harness = createHarness({
      concurrency: 1,
      extract: async (_kind, input) => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return fakeExtracted(input);
      },
    });
    const [first, second] = harness.queue.enqueue([
      { kind: "text", input: "第一个" },
      { kind: "text", input: "第二个" },
    ]);

    harness.queue.cancel(second.id);
    await harness.queue.drain();

    expect(harness.store.get(first.id)!.status).toBe("completed");
    expect(harness.store.get(second.id)!.status).toBe("canceled");
  });

  it("重启恢复：processing 复位为 pending 并重新执行", async () => {
    const store = createMemoryStore();
    // 模拟上次异常退出遗留的状态
    const stale = store.create({ kind: "text", input: "遗留任务" });
    store.update(stale.id, { status: "processing", stage: "fetching" });

    const savedItems: string[] = [];
    const queue = new ImportQueue({
      store,
      persistence: {
        findDuplicate: () => null,
        saveItem({ extracted }) {
          savedItems.push(extracted.content);
          return "item-recovered";
        },
      },
      extract: async (_kind, input) => fakeExtracted(input),
      onTaskChanged: () => {},
    });

    queue.recover();
    await queue.drain();

    expect(store.get(stale.id)!.status).toBe("completed");
    expect(savedItems).toEqual(["遗留任务"]);
  });
});
