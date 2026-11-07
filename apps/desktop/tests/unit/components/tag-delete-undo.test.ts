import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Tag } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { useTagStore } from "../../../src/renderer/stores/tag.store";
import { useKnowledgeStore } from "../../../src/renderer/stores/knowledge.store";

function tag(id: string): Tag {
  return { id, name: id, colorKey: "blue", itemCount: 0, createdAt: 1, updatedAt: 1 } as unknown as Tag;
}

const all = [tag("甲"), tag("乙")];

describe("标签删除：先隐藏、窗口内可撤销、之后才提交", () => {
  let refreshAll: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.useFakeTimers();
    installWindowMocks({ api: { tag: {} } });
    window.api.tag.list = vi.fn().mockResolvedValue(all);
    window.api.tag.delete = vi.fn().mockResolvedValue(true);
    refreshAll = vi.fn().mockResolvedValue(undefined);
    useKnowledgeStore.setState({ refreshAll } as never);
    useTagStore.setState({ tags: [...all] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("立即从列表消失，但窗口内不调用主进程", () => {
    const deletion = useTagStore.getState().scheduleDeleteTag("甲");
    expect(deletion).not.toBeNull();
    expect(useTagStore.getState().tags.map((c) => c.id)).toEqual(["乙"]);
    expect(window.api.tag.delete).not.toHaveBeenCalled();
    deletion?.undo();
  });

  it("窗口内撤销：标签回来，且从未提交", async () => {
    const deletion = useTagStore.getState().scheduleDeleteTag("甲");
    deletion?.undo();
    await vi.advanceTimersByTimeAsync(0);
    expect(useTagStore.getState().tags.map((c) => c.id)).toEqual(["甲", "乙"]);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(window.api.tag.delete).not.toHaveBeenCalled();
  });

  it("窗口结束后才提交，并刷新列表与条目计数", async () => {
    window.api.tag.list = vi.fn().mockResolvedValue([tag("乙")]);
    useTagStore.getState().scheduleDeleteTag("甲");
    await vi.advanceTimersByTimeAsync(7_999);
    expect(window.api.tag.delete).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(window.api.tag.delete).toHaveBeenCalledWith("甲");
    expect(useTagStore.getState().tags.map((c) => c.id)).toEqual(["乙"]);
    expect(refreshAll).toHaveBeenCalled();
  });

  it("窗口内重新拉取列表时，待删标签不会闪回", async () => {
    const deletion = useTagStore.getState().scheduleDeleteTag("甲");
    await useTagStore.getState().fetchTags();
    expect(useTagStore.getState().tags.map((c) => c.id)).toEqual(["乙"]);
    deletion?.undo();
  });

  it("提交失败时标签回到列表", async () => {
    window.api.tag.delete = vi.fn().mockResolvedValue(false);
    useTagStore.getState().scheduleDeleteTag("甲");
    await vi.advanceTimersByTimeAsync(8_000);
    expect(useTagStore.getState().tags.map((c) => c.id)).toEqual(["甲", "乙"]);
  });

  it("同一标签重复删除只排一次；未知标签返回 null", () => {
    const first = useTagStore.getState().scheduleDeleteTag("甲");
    expect(useTagStore.getState().scheduleDeleteTag("甲")).toBeNull();
    expect(useTagStore.getState().scheduleDeleteTag("不存在")).toBeNull();
    first?.undo();
  });
});
