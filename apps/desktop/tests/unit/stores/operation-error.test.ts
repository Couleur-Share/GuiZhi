import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  runGuardedMutation,
  useOperationErrorStore,
} from "../../../src/renderer/stores/operation-error.store";

beforeEach(() => {
  useOperationErrorStore.setState({ latest: null });
  vi.spyOn(console, "error").mockImplementation(() => {});
});

describe("runGuardedMutation", () => {
  it("成功时返回 true 且不投错误", async () => {
    const ok = await runGuardedMutation("k", "批量更新", async () => {});

    expect(ok).toBe(true);
    expect(useOperationErrorStore.getState().latest).toBeNull();
  });

  /**
   * 这是这条通道存在的理由：变更方法几乎全被 `void mutate(...)` 调用，
   * 一旦向外抛就是无人处理的 rejection，界面上什么都不会发生。
   */
  it("失败时不向外抛，返回 false 并把原因投进通道", async () => {
    const ok = await runGuardedMutation("library.actionBulkUpdate", "批量更新", () =>
      Promise.reject(new Error("SQLITE_BUSY: database is locked")),
    );

    expect(ok).toBe(false);
    expect(useOperationErrorStore.getState().latest).toMatchObject({
      actionKey: "library.actionBulkUpdate",
      actionFallback: "批量更新",
      message: "SQLITE_BUSY: database is locked",
    });
  });

  it("非 Error 抛出物也转成可读文本", async () => {
    await runGuardedMutation("k", "更新状态", () => Promise.reject("boom"));

    expect(useOperationErrorStore.getState().latest?.message).toBe("boom");
  });

  it("同一个错误连发两次也要各提示一次（id 递增）", async () => {
    await runGuardedMutation("k", "更新状态", () =>
      Promise.reject(new Error("same")),
    );
    const first = useOperationErrorStore.getState().latest;
    await runGuardedMutation("k", "更新状态", () =>
      Promise.reject(new Error("same")),
    );
    const second = useOperationErrorStore.getState().latest;

    expect(second?.id).toBeGreaterThan(first!.id);
  });
});
