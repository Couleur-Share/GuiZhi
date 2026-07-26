import { beforeEach, describe, expect, it, vi } from "vitest";
import type { KnowledgeItem, UpdateKnowledgeItemInput } from "@guizhi/shared/types";

import {
  __resetPendingSaves,
  useKnowledgeStore,
} from "../../../src/renderer/stores/knowledge.store";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";

function makeItem(id: string, content = ""): KnowledgeItem {
  return {
    id,
    title: id,
    content,
    summary: null,
    transcript: null,
    itemType: "note",
    status: "inbox",
    collectionId: null,
    isFavorite: false,
    isPinned: false,
    sourceUri: null,
    deletedAt: null,
    createdAt: 0,
    updatedAt: 0,
    tags: [],
  };
}

interface UpdateCall {
  id: string;
  patch: UpdateKnowledgeItemInput;
}

function stubUpdate(
  impl: (id: string, patch: UpdateKnowledgeItemInput) => Promise<KnowledgeItem | null>,
): void {
  window.api.knowledge = { ...(window.api.knowledge ?? {}), update: impl };
}

function selectItem(item: KnowledgeItem): void {
  useKnowledgeStore.setState({ selectedId: item.id, selectedItem: item });
}

describe("knowledge.store 待保存队列", () => {
  beforeEach(() => {
    __resetPendingSaves();
    // 关掉 autoSave：由用例显式驱动落盘，等价于 Ctrl+S / 切换条目的路径
    useSettingsStore.setState({ autoSave: false });
    useKnowledgeStore.setState({
      entries: [],
      selectedId: null,
      selectedItem: null,
      isSaving: false,
      hasUnsavedChanges: false,
      saveError: null,
    });
  });

  it("A 的保存失败不会把 B 的编辑写进 A", async () => {
    const calls: UpdateCall[] = [];
    let rejectFirst: ((error: Error) => void) | null = null;

    stubUpdate(async (id, patch) => {
      calls.push({ id, patch });
      if (calls.length === 1) {
        return new Promise<KnowledgeItem>((_resolve, reject) => {
          rejectFirst = reject;
        });
      }
      return makeItem(id, String(patch.content ?? ""));
    });

    selectItem(makeItem("A"));
    useKnowledgeStore.getState().updateSelected({ content: "A 的正文" });
    const inflight = useKnowledgeStore.getState().flushPendingSave();
    await vi.waitFor(() => expect(calls).toHaveLength(1));

    // 保存在途期间视图已经换到 B（ItemDetailModal 关闭 / 切换范围都是这个时序），
    // 用户继续在 B 里输入
    selectItem(makeItem("B"));
    useKnowledgeStore.getState().updateSelected({ content: "B 的正文" });

    rejectFirst!(new Error("数据库被备份流程占用"));
    await inflight;

    // 失败的改动退回 A 自己的队列，B 的照旧；重试时各写各的
    await useKnowledgeStore.getState().flushPendingSave();

    const retried = new Map(
      calls.slice(1).map((call) => [call.id, call.patch.content]),
    );
    expect(retried.get("A")).toBe("A 的正文");
    expect(retried.get("B")).toBe("B 的正文");
  });

  it("保存失败后保留未保存标记与失败原因，成功后清空", async () => {
    let shouldFail = true;
    stubUpdate(async (id, patch) => {
      if (shouldFail) {
        throw new Error("条目已被删除");
      }
      return makeItem(id, String(patch.content ?? ""));
    });

    selectItem(makeItem("A"));
    useKnowledgeStore.getState().updateSelected({ content: "改动" });
    await useKnowledgeStore.getState().flushPendingSave();

    expect(useKnowledgeStore.getState().hasUnsavedChanges).toBe(true);
    expect(useKnowledgeStore.getState().saveError).toContain("条目已被删除");

    shouldFail = false;
    await useKnowledgeStore.getState().flushPendingSave();

    expect(useKnowledgeStore.getState().hasUnsavedChanges).toBe(false);
    expect(useKnowledgeStore.getState().saveError).toBeNull();
  });

  it("flushPendingSave 等待在途保存收尾，不重复下发同一份改动", async () => {
    const calls: UpdateCall[] = [];
    let resolveFirst: ((item: KnowledgeItem) => void) | null = null;

    stubUpdate(async (id, patch) => {
      calls.push({ id, patch });
      if (calls.length === 1) {
        return new Promise<KnowledgeItem>((resolve) => {
          resolveFirst = resolve;
        });
      }
      return makeItem(id, String(patch.content ?? ""));
    });

    selectItem(makeItem("A"));
    useKnowledgeStore.getState().updateSelected({ content: "第一次" });
    const first = useKnowledgeStore.getState().flushPendingSave();
    await vi.waitFor(() => expect(calls).toHaveLength(1));

    const second = useKnowledgeStore.getState().flushPendingSave();
    resolveFirst!(makeItem("A", "第一次"));
    await Promise.all([first, second]);

    // 队列已被第一次取空，第二次不该再发一遍
    expect(calls).toHaveLength(1);
  });

  it("防抖窗口内连加两个标签，第一个不会被覆盖", async () => {
    const calls: UpdateCall[] = [];
    stubUpdate(async (id, patch) => {
      calls.push({ id, patch });
      return { ...makeItem(id), tags: [] };
    });

    selectItem(makeItem("A"));
    const store = useKnowledgeStore.getState();

    // 标签浮层每次都基于当前 item.tags 做全量覆盖，
    // 本地不回显的话第二次的 patch 会把第一个标签挤掉
    const namesAfterFirst = [
      ...(useKnowledgeStore.getState().selectedItem?.tags ?? []).map((t) => t.name),
      "读书",
    ];
    store.updateSelected({ tagNames: namesAfterFirst });

    const namesAfterSecond = [
      ...(useKnowledgeStore.getState().selectedItem?.tags ?? []).map((t) => t.name),
      "笔记",
    ];
    store.updateSelected({ tagNames: namesAfterSecond });

    await useKnowledgeStore.getState().flushPendingSave();

    expect(calls).toHaveLength(1);
    expect(calls[0].patch.tagNames).toEqual(["读书", "笔记"]);
  });
});
