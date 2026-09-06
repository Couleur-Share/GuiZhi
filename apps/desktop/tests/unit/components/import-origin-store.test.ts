import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ImportTask, ImportTaskListResult } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { filterTasks, useImportStore } from "../../../src/renderer/stores/import.store";

function task(id: string, origin: "mobile" | "desktop"): ImportTask {
  return { id, origin, sourceKind: "text", sourceInput: id, displayName: id,
    status: "failed", error: "网络错误", createdAt: 1, updatedAt: 1,
    captureStrategy: "standard", commentLimit: 0 };
}
function page(entries: ImportTask[], nextCursor: string | null = null): ImportTaskListResult {
  return { entries, active: [], nextCursor, total: entries.length,
    counts: { failed: entries.length, completed: 0, duplicate: 0, canceled: 0, pending: 0, processing: 0 } };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(r => { resolve = r; });
  return { promise, resolve };
}
describe("来源筛选状态与异步边界", () => {
  beforeEach(() => {
    installWindowMocks({ api: { import: {} } });
    useImportStore.setState({ tasks: [], origin: "all", filter: "all", query: "", selectionIds: [], nextCursor: null, isLoadingMore: false });
    window.api.import.getQueueState = vi.fn().mockResolvedValue({ paused: false, runningCount: 2, pendingCount: 1, concurrency: 2 });
  });
  it("来源、状态和错误搜索组合，并只选择当前可见任务", () => {
    const tasks = [task("手机", "mobile"), task("桌面", "desktop")];
    expect(filterTasks(tasks, "failed", "网络", "mobile").map(t => t.id)).toEqual(["手机"]);
    useImportStore.setState({ tasks, origin: "mobile", filter: "failed", query: "网络" });
    useImportStore.getState().selectVisible();
    expect(useImportStore.getState().selectionIds).toEqual(["手机"]);
  });
  it("快速切换来源不被旧请求覆盖，选中项清空，全局角标保留其他来源任务", async () => {
    const old = deferred<ImportTaskListResult>();
    const fresh = deferred<ImportTaskListResult>();
    window.api.import.list = vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
    const first = useImportStore.getState().fetchTasks();
    useImportStore.setState({ selectionIds: ["旧任务"] });
    useImportStore.getState().setOrigin("mobile");
    fresh.resolve(page([task("手机", "mobile")]));
    await vi.waitFor(() => expect(useImportStore.getState().tasks[0]?.id).toBe("手机"));
    old.resolve(page([task("桌面", "desktop")]));
    await first;
    expect(useImportStore.getState().tasks.map(t => t.id)).toEqual(["手机"]);
    expect(useImportStore.getState().selectionIds).toEqual([]);
    expect(useImportStore.getState().activeCount).toBe(3);
    expect(window.api.import.list).toHaveBeenLastCalledWith(expect.objectContaining({ origin: "mobile" }));
  });
  it("旧来源翻页返回时不能混入新来源列表", async () => {
    const more = deferred<ImportTaskListResult>();
    window.api.import.list = vi.fn().mockReturnValueOnce(more.promise).mockResolvedValue(page([task("手机", "mobile")]));
    useImportStore.setState({ tasks: [task("桌面", "desktop")], origin: "desktop", nextCursor: "older" });
    const loading = useImportStore.getState().loadMore();
    useImportStore.getState().setOrigin("mobile");
    await vi.waitFor(() => expect(useImportStore.getState().tasks[0]?.id).toBe("手机"));
    more.resolve(page([task("更早桌面", "desktop")]));
    await loading;
    expect(useImportStore.getState().tasks.map(t => t.id)).toEqual(["手机"]);
    expect(useImportStore.getState().isLoadingMore).toBe(false);
  });
  it("确认清理使用预览时的范围快照", async () => {
    const query = { scope: "filtered", origin: "mobile", status: "failed", query: "网络" } as const;
    window.api.import.previewClearTerminal = vi.fn().mockResolvedValue({ count: 1 });
    window.api.import.clearTerminal = vi.fn().mockResolvedValue({ count: 1 });
    window.api.import.list = vi.fn().mockResolvedValue(page([]));
    await useImportStore.getState().previewClearTerminal(query);
    useImportStore.setState({ origin: "desktop", filter: "all", query: "" });
    await useImportStore.getState().clearTerminal(query);
    expect(window.api.import.previewClearTerminal).toHaveBeenCalledWith(query);
    expect(window.api.import.clearTerminal).toHaveBeenCalledWith(query);
  });
  it("其他来源的实时事件不混入列表，刷新计数不丢失已加载历史", async () => {
    const older = task("历史手机任务", "mobile");
    const first = task("最新手机任务", "mobile");
    useImportStore.setState({ tasks: [first, older], origin: "mobile", nextCursor: "older-page" });
    window.api.import.list = vi.fn().mockResolvedValue(page([first]));
    const unsubscribe = useImportStore.getState().subscribeChanges();
    try {
      const listener = vi.mocked(window.api.on).mock.calls.find(call => call[0] === "import:changed")![1];
      listener({ ...task("桌面新任务", "desktop"), status: "processing" });
      await vi.waitFor(() => expect(window.api.import.list).toHaveBeenCalled(), { timeout: 1000 });
      expect(useImportStore.getState().tasks.map(t => t.id)).toEqual([first.id, older.id]);
      expect(useImportStore.getState().nextCursor).toBe("older-page");
    } finally { unsubscribe(); }
  });
});
