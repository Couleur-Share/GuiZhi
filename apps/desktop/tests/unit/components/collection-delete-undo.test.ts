import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Collection } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { useCollectionStore } from "../../../src/renderer/stores/collection.store";
import { useKnowledgeStore } from "../../../src/renderer/stores/knowledge.store";

function collection(id: string): Collection {
  return { id, name: id, icon: "", sortOrder: 0, createdAt: 1, updatedAt: 1 } as Collection;
}

const all = [collection("甲"), collection("乙")];

describe("合集删除：先隐藏、窗口内可撤销、之后才提交", () => {
  let refreshAll: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.useFakeTimers();
    installWindowMocks({ api: { collection: {} } });
    window.api.collection.list = vi.fn().mockResolvedValue(all);
    window.api.collection.delete = vi.fn().mockResolvedValue(true);
    refreshAll = vi.fn().mockResolvedValue(undefined);
    useKnowledgeStore.setState({ refreshAll } as never);
    useCollectionStore.setState({ collections: [...all] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("立即从列表消失，但窗口内不调用主进程", () => {
    const deletion = useCollectionStore.getState().scheduleDeleteCollection("甲");
    expect(deletion).not.toBeNull();
    expect(useCollectionStore.getState().collections.map((c) => c.id)).toEqual(["乙"]);
    expect(window.api.collection.delete).not.toHaveBeenCalled();
    deletion?.undo();
  });

  it("窗口内撤销：合集回来，且从未提交", async () => {
    const deletion = useCollectionStore.getState().scheduleDeleteCollection("甲");
    deletion?.undo();
    await vi.advanceTimersByTimeAsync(0);
    expect(useCollectionStore.getState().collections.map((c) => c.id)).toEqual(["甲", "乙"]);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(window.api.collection.delete).not.toHaveBeenCalled();
  });

  it("窗口结束后才提交，并刷新列表与条目计数", async () => {
    window.api.collection.list = vi.fn().mockResolvedValue([collection("乙")]);
    useCollectionStore.getState().scheduleDeleteCollection("甲");
    await vi.advanceTimersByTimeAsync(7_999);
    expect(window.api.collection.delete).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(window.api.collection.delete).toHaveBeenCalledWith("甲");
    expect(useCollectionStore.getState().collections.map((c) => c.id)).toEqual(["乙"]);
    expect(refreshAll).toHaveBeenCalled();
  });

  it("窗口内重新拉取列表时，待删合集不会闪回", async () => {
    const deletion = useCollectionStore.getState().scheduleDeleteCollection("甲");
    await useCollectionStore.getState().fetchCollections();
    expect(useCollectionStore.getState().collections.map((c) => c.id)).toEqual(["乙"]);
    deletion?.undo();
  });

  it("提交失败时合集回到列表", async () => {
    window.api.collection.delete = vi.fn().mockResolvedValue(false);
    useCollectionStore.getState().scheduleDeleteCollection("甲");
    await vi.advanceTimersByTimeAsync(8_000);
    expect(useCollectionStore.getState().collections.map((c) => c.id)).toEqual(["甲", "乙"]);
  });

  it("同一合集重复删除只排一次；未知合集返回 null", () => {
    const first = useCollectionStore.getState().scheduleDeleteCollection("甲");
    expect(useCollectionStore.getState().scheduleDeleteCollection("甲")).toBeNull();
    expect(useCollectionStore.getState().scheduleDeleteCollection("不存在")).toBeNull();
    first?.undo();
  });
});
