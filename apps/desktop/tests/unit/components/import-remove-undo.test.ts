import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImportTask, ImportTaskListResult } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { useImportStore } from "../../../src/renderer/stores/import.store";

function failedTask(id: string): ImportTask {
  return {
    id,
    origin: "desktop",
    sourceKind: "text",
    sourceInput: id,
    displayName: id,
    status: "failed",
    error: "网络错误",
    createdAt: 1,
    updatedAt: 1,
    captureStrategy: "standard",
    commentLimit: 0,
  };
}

function page(entries: ImportTask[]): ImportTaskListResult {
  return {
    entries,
    active: [],
    nextCursor: null,
    total: entries.length,
    counts: {
      failed: entries.length,
      completed: 0,
      duplicate: 0,
      canceled: 0,
      pending: 0,
      processing: 0,
    },
  };
}

const all = [failedTask("甲"), failedTask("乙"), failedTask("丙")];

describe("导入列表删除：先隐藏、窗口内可撤销、之后才提交", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    installWindowMocks({ api: { import: {} } });
    window.api.import.getQueueState = vi
      .fn()
      .mockResolvedValue({ paused: false, runningCount: 0, pendingCount: 0, concurrency: 2 });
    window.api.import.remove = vi.fn().mockResolvedValue(undefined);
    window.api.import.list = vi.fn().mockResolvedValue(page(all));
    useImportStore.setState({
      tasks: [...all],
      total: 3,
      counts: { all: 3, active: 0, completed: 0, degraded: 0, duplicate: 0, failed: 3, canceled: 0 },
      selectionIds: ["甲", "乙"],
      filter: "all",
      query: "",
      origin: "all",
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("立即从列表、选中集合和计数里消失，但窗口内不调用主进程", () => {
    const removal = useImportStore.getState().scheduleRemoveTasks(["甲", "乙"]);
    expect(removal?.count).toBe(2);

    const state = useImportStore.getState();
    expect(state.tasks.map((task) => task.id)).toEqual(["丙"]);
    expect(state.selectionIds).toEqual([]);
    expect(state.total).toBe(1);
    expect(state.counts.failed).toBe(1);
    expect(window.api.import.remove).not.toHaveBeenCalled();

    removal?.undo();
  });

  it("撤销后原样回到列表，且从未提交过删除", async () => {
    const removal = useImportStore.getState().scheduleRemoveTasks(["甲"])!;
    removal.undo();
    await vi.advanceTimersByTimeAsync(10_000);

    expect(window.api.import.remove).not.toHaveBeenCalled();
    expect(useImportStore.getState().tasks.map((task) => task.id)).toEqual(["甲", "乙", "丙"]);
  });

  it("窗口结束自动提交，只删被选中的任务", async () => {
    useImportStore.getState().scheduleRemoveTasks(["甲"]);
    await vi.advanceTimersByTimeAsync(8_000);

    expect(window.api.import.remove).toHaveBeenCalledTimes(1);
    expect(window.api.import.remove).toHaveBeenCalledWith("甲");
  });

  it("窗口内刷新列表或收到主进程推送，被删的行也不会闪回", async () => {
    const removal = useImportStore.getState().scheduleRemoveTasks(["甲"])!;
    const unsubscribe = useImportStore.getState().subscribeChanges();
    try {
      await useImportStore.getState().fetchTasks();
      expect(useImportStore.getState().tasks.map((task) => task.id)).toEqual(["乙", "丙"]);
      // 服务端计数仍含「甲」，界面需要扣掉
      expect(useImportStore.getState().counts.failed).toBe(2);

      const listener = vi.mocked(window.api.on).mock.calls.find((call) => call[0] === "import:changed")![1];
      listener({ ...failedTask("甲"), updatedAt: 2 });
      expect(useImportStore.getState().tasks.map((task) => task.id)).toEqual(["乙", "丙"]);
    } finally {
      unsubscribe();
      removal.undo();
    }
  });

  it("提交时逐条记账：失败的任务回到列表，成功的不再出现", async () => {
    window.api.import.remove = vi
      .fn()
      .mockImplementation(async (id: string) => {
        if (id === "乙") throw new Error("数据库忙");
      });
    // 提交后列表只剩失败的那条（服务端已删掉「甲」）
    window.api.import.list = vi.fn().mockResolvedValue(page([failedTask("乙"), failedTask("丙")]));

    const removal = useImportStore.getState().scheduleRemoveTasks(["甲", "乙"])!;
    await removal.commit();

    expect(window.api.import.remove).toHaveBeenCalledTimes(2);
    expect(useImportStore.getState().tasks.map((task) => task.id)).toEqual(["乙", "丙"]);
  });

  it("运行中的任务不能被删，也不会重复登记同一条", () => {
    useImportStore.setState({
      tasks: [{ ...failedTask("运行中"), status: "processing" as const }, ...all],
    });
    expect(useImportStore.getState().scheduleRemoveTasks(["运行中"])).toBeNull();

    const first = useImportStore.getState().scheduleRemoveTasks(["甲"]);
    expect(first?.count).toBe(1);
    expect(useImportStore.getState().scheduleRemoveTasks(["甲"])).toBeNull();
    first?.undo();
  });
});
