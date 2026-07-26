import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  KnowledgeItem,
  KnowledgeItemListEntry,
  UpdateKnowledgeItemInput,
} from "@guizhi/shared/types";

import { useKnowledgeStore } from "../../../src/renderer/stores/knowledge.store";

function makeEntry(
  id: string,
  overrides: Partial<KnowledgeItemListEntry> = {},
): KnowledgeItemListEntry {
  return {
    id,
    title: id,
    snippet: "",
    itemType: "note",
    status: "active",
    collectionId: null,
    isFavorite: false,
    isPinned: false,
    deletedAt: null,
    createdAt: 0,
    updatedAt: 0,
    tags: [],
    ...overrides,
  };
}

function makeItem(id: string, patch: UpdateKnowledgeItemInput): KnowledgeItem {
  return {
    id,
    title: id,
    content: "",
    summary: null,
    transcript: null,
    itemType: "note",
    status: "active",
    collectionId: null,
    isFavorite: patch.isFavorite ?? false,
    isPinned: patch.isPinned ?? false,
    sourceUri: null,
    deletedAt: null,
    createdAt: 0,
    updatedAt: 0,
    tags: [],
  };
}

describe("knowledge.store 收藏 / 置顶", () => {
  let updateCalls: UpdateKnowledgeItemInput[];
  let listCalls: number;

  beforeEach(() => {
    updateCalls = [];
    listCalls = 0;
    window.api.knowledge = {
      ...(window.api.knowledge ?? {}),
      update: async (id: string, patch: UpdateKnowledgeItemInput) => {
        updateCalls.push(patch);
        return makeItem(id, patch);
      },
      list: async () => {
        listCalls += 1;
        return { entries: [], total: 0 };
      },
      counts: async () => ({
        uncategorized: 0,
        all: 0,
        favorites: 0,
        archived: 0,
        trash: 0,
        byCollection: {},
        byTag: {},
      }),
    };
    useKnowledgeStore.setState({
      entries: [makeEntry("A")],
      selectedId: null,
      selectedItem: null,
    });
  });

  it("取消收藏后重取列表：收藏范围里的条目要真的消失", async () => {
    useKnowledgeStore.setState({
      scope: "favorites",
      entries: [makeEntry("A", { isFavorite: true })],
    });

    await useKnowledgeStore.getState().toggleFavorite("A");

    expect(updateCalls).toEqual([{ isFavorite: false }]);
    // 只改本地行的话，条目会留在「收藏」列表里不走
    expect(listCalls).toBeGreaterThan(0);
  });

  it("置顶与收藏走同一套刷新，不再一个重排一个不动", async () => {
    await useKnowledgeStore.getState().togglePinned("A");
    expect(updateCalls).toEqual([{ isPinned: true }]);
    expect(listCalls).toBeGreaterThan(0);
  });

  it("连点两次翻转两次，不会都发同一个值", async () => {
    const store = useKnowledgeStore.getState();
    await Promise.all([store.toggleFavorite("A"), store.toggleFavorite("A")]);

    // 从本地状态读当前值的话，两次都会发 isFavorite: true
    expect(updateCalls).toEqual([{ isFavorite: true }, { isFavorite: false }]);
  });

  it("bulkUpdate 只打一次 IPC，不是逐条循环", async () => {
    const bulkCalls: Array<{ ids: string[]; patch: unknown }> = [];
    window.api.knowledge = {
      ...window.api.knowledge,
      bulkUpdate: async (ids: string[], patch: unknown) => {
        bulkCalls.push({ ids, patch });
        return ids.length;
      },
    };

    await useKnowledgeStore
      .getState()
      .bulkMoveToCollection(["A", "B", "C"], "col-1");

    expect(bulkCalls).toHaveLength(1);
    expect(bulkCalls[0]).toEqual({
      ids: ["A", "B", "C"],
      patch: { collectionId: "col-1" },
    });
    expect(updateCalls).toHaveLength(0);
  });
});

describe("useItemListKeyboard 的守卫", () => {
  it("焦点在输入框里时不劫持方向键", async () => {
    const { useItemListKeyboard } = await import(
      "../../../src/renderer/components/library/use-item-keyboard"
    );
    expect(typeof useItemListKeyboard).toBe("function");
    // 行为本身在组件测试里覆盖；这里只确保模块可独立加载，
    // 避免它被误挂到会引入 electron 依赖的模块图上
    expect(vi.isMockFunction(useItemListKeyboard)).toBe(false);
  });
});
