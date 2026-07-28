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
  // 单调递增，避免同毫秒创建的任务排序不稳定
  let sequence = 0;
  return {
    rows,
    create(input: EnqueueImportInput) {
      const now = Date.now() + sequence++;
      const task: ImportTask & { forceDuplicate: boolean } = {
        id: randomUUID(),
        sourceKind: input.kind,
        sourceInput: input.input,
        displayName: input.input.slice(0, 60),
        status: "pending",
        stage: null,
        error: null,
        warning: null,
        itemType: null,
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
    // 与 ImportTaskDB.list 一致：最近 limit 条、按创建时间倒序。
    // 这个窗口正是 recover() 不能用 list() 的原因，替身必须如实模拟。
    list(limit = 200) {
      return [...rows.values()]
        .sort((left, right) => right.createdAt - left.createdAt)
        .slice(0, limit)
        .map((row) => ({ ...row }));
    },
    listByStatus(statuses) {
      return [...rows.values()]
        .filter((row) => statuses.includes(row.status))
        .sort((left, right) => left.createdAt - right.createdAt)
        .map((row) => ({ ...row }));
    },
    update(id, patch) {
      const row = rows.get(id);
      if (!row) {
        return null;
      }
      if (patch.status !== undefined) row.status = patch.status;
      if (patch.stage !== undefined) row.stage = patch.stage;
      if (patch.error !== undefined) row.error = patch.error;
      if (patch.warning !== undefined) row.warning = patch.warning;
      // 与 ImportTaskDB.update 一致：空标题不覆盖既有显示名
      if (patch.displayName?.trim()) row.displayName = patch.displayName.trim();
      if (patch.itemType !== undefined) row.itemType = patch.itemType;
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

  it("抽取成功后回写真实标题与条目类型", async () => {
    const harness = createHarness({
      extract: async () => ({
        title: "为什么 SQLite 不适合做队列",
        content: "正文",
        itemType: "forum",
        sourceUri: "https://www.v2ex.com/t/1223399",
      }),
    });
    const [task] = harness.queue.enqueue([
      { kind: "url", input: "https://www.v2ex.com/t/1223399#reply147" },
    ]);
    // 建任务时显示名只能是原始链接，一列长得一样的 URL 分不清采的是什么
    expect(task.displayName).toContain("v2ex.com");
    await harness.queue.drain();

    const finished = harness.store.get(task.id)!;
    expect(finished.displayName).toBe("为什么 SQLite 不适合做队列");
    expect(finished.itemType).toBe("forum");
  });

  it("重复任务同样拿得到标题：回写发生在去重判定之前", async () => {
    const harness = createHarness({
      findDuplicate: () => "existing-item",
      extract: async () => ({
        title: "已经采过的那篇",
        content: "正文",
        itemType: "webpage",
        sourceUri: "https://example.com/a",
      }),
    });
    const [task] = harness.queue.enqueue([
      { kind: "url", input: "https://example.com/a" },
    ]);
    await harness.queue.drain();

    const finished = harness.store.get(task.id)!;
    expect(finished.status).toBe("duplicate");
    expect(finished.displayName).toBe("已经采过的那篇");
    expect(finished.itemType).toBe("webpage");
  });

  it("入库但内容有缺失：任务仍标 completed，降级原因写在 warning 上", async () => {
    const harness = createHarness({
      extract: async () => ({
        title: "某条抖音视频",
        content: "> 平台：抖音\n\n> 文字稿生成失败：本地转写服务启动失败",
        itemType: "video",
        sourceUri: "https://v.douyin.com/abc/",
        warningReason: "文字稿生成失败：本地转写服务启动失败",
      }),
    });
    const [task] = harness.queue.enqueue([
      { kind: "url", input: "https://v.douyin.com/abc/" },
    ]);
    await harness.queue.drain();

    const finished = harness.store.get(task.id)!;
    // 条目本身有价值（元数据齐全），不该按失败处理丢掉
    expect(finished.status).toBe("completed");
    expect(finished.resultItemId).toBe("item-1");
    // 但「已完成」三个字必须带上下文，否则用户点开才发现是个空壳
    expect(finished.warning).toContain("文字稿生成失败");
    expect(finished.error).toBeNull();
  });

  it("重试清空上一轮的降级原因，否则旧提示会赖在成功的任务上", async () => {
    let warned = true;
    const harness = createHarness({
      extract: async () => ({
        title: "某条抖音视频",
        content: "正文",
        itemType: "video",
        sourceUri: "https://v.douyin.com/abc/",
        ...(warned ? { warningReason: "文字稿生成失败：服务忙" } : {}),
      }),
    });
    const [task] = harness.queue.enqueue([
      { kind: "url", input: "https://v.douyin.com/abc/" },
    ]);
    await harness.queue.drain();
    expect(harness.store.get(task.id)!.warning).toBeTruthy();

    warned = false;
    harness.queue.retry(task.id, { forceDuplicate: true });
    await harness.queue.drain();
    expect(harness.store.get(task.id)!.warning).toBeNull();
  });

  it("抽取没给标题时保留原始显示名，不写成空", async () => {
    const harness = createHarness({
      extract: async () => ({
        title: "   ",
        content: "正文",
        itemType: "note",
        sourceUri: null,
      }),
    });
    const [task] = harness.queue.enqueue([
      { kind: "url", input: "https://example.com/b" },
    ]);
    await harness.queue.drain();

    expect(harness.store.get(task.id)!.displayName).toBe(
      "https://example.com/b",
    );
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

  it("重启恢复：待处理任务超过 list() 的 200 条窗口时也不丢", async () => {
    const harness = createHarness({ concurrency: 8 });
    const tasks = harness.queue.enqueue(
      Array.from({ length: 250 }, (_, index) => ({
        kind: "text" as const,
        input: `任务 ${index}`,
      })),
    );
    await harness.queue.drain();

    // 全部复位为 pending，模拟上次退出时这 250 条都还没跑完
    for (const task of tasks) {
      harness.store.update(task.id, { status: "pending", resultItemId: null });
    }
    harness.savedItems.length = 0;

    harness.queue.recover();
    await harness.queue.drain();

    const unfinished = tasks.filter(
      (task) => harness.store.get(task.id)!.status !== "completed",
    );
    expect(unfinished).toHaveLength(0);
    expect(harness.savedItems).toHaveLength(250);
  });

  it("抽取降级：标记 failed 并透出原因，不入库", async () => {
    const harness = createHarness({
      extract: async () => ({
        title: "https://example.com/gone",
        content: "",
        itemType: "webpage" as const,
        sourceUri: null,
        degradedReason: "网页抓取失败：HTTP 403",
      }),
    });
    const [task] = harness.queue.enqueue([
      { kind: "url", input: "https://example.com/gone" },
    ]);
    await harness.queue.drain();

    const finished = harness.store.get(task.id)!;
    expect(finished.status).toBe("failed");
    expect(finished.error).toBe("网页抓取失败：HTTP 403");
    // 不入库是关键：空壳条目会占住该链接的 normalized_uri，让重试永远判重
    expect(finished.resultItemId).toBeNull();
    expect(harness.savedItems).toHaveLength(0);
  });
});
