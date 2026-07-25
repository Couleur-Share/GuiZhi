import { create } from "zustand";
import type {
  CreateKnowledgeItemInput,
  KnowledgeCounts,
  KnowledgeItem,
  KnowledgeItemListEntry,
  KnowledgeItemStatus,
  KnowledgeScope,
  KnowledgeSortField,
  KnowledgeSortOrder,
  UpdateKnowledgeItemInput,
} from "@guizhi/shared/types";
import { useSettingsStore } from "./settings.store";
import { useTagStore } from "./tag.store";

const AUTO_SAVE_DEBOUNCE_MS = 800;

/** 可本地编辑并防抖持久化的字段 */
type EditablePatch = Pick<
  UpdateKnowledgeItemInput,
  "title" | "content" | "collectionId" | "tagNames"
>;

let saveTimer: ReturnType<typeof setTimeout> | null = null;
let pendingPatch: EditablePatch = {};
let pendingItemId: string | null = null;

interface KnowledgeState {
  // ── 导航 ──
  scope: KnowledgeScope;
  collectionId: string | null;
  tagId: string | null;
  searchQuery: string;
  // ── 排序（搜索态下由相关度接管） ──
  sortBy: KnowledgeSortField;
  sortOrder: KnowledgeSortOrder;
  // ── 列表 ──
  entries: KnowledgeItemListEntry[];
  total: number;
  isLoading: boolean;
  // ── 计数 ──
  counts: KnowledgeCounts | null;
  // ── 详情 ──
  selectedId: string | null;
  selectedItem: KnowledgeItem | null;
  isSaving: boolean;
  /** 有未落盘的编辑（autoSave 关闭时由保存按钮 / Ctrl+S 落盘） */
  hasUnsavedChanges: boolean;
  // ── 批量多选 ──
  selectionIds: string[];
  selectionAnchorId: string | null;

  setScope: (scope: KnowledgeScope) => void;
  selectCollection: (collectionId: string | null) => void;
  selectTag: (tagId: string | null) => void;
  setSearchQuery: (query: string) => void;
  setSort: (sortBy: KnowledgeSortField, sortOrder: KnowledgeSortOrder) => void;

  /** Ctrl/Cmd+点击：切换单条选中态 */
  toggleSelection: (id: string) => void;
  /** Shift+点击：从锚点到目标的连续范围选择 */
  rangeSelectTo: (id: string) => void;
  /** 覆盖式设置选中集合（表格视图的「全选本页」） */
  setSelection: (ids: string[]) => void;
  clearSelection: () => void;
  bulkMoveToCollection: (
    ids: string[],
    collectionId: string | null,
  ) => Promise<void>;

  fetchList: () => Promise<void>;
  refreshCounts: () => Promise<void>;
  refreshAll: () => Promise<void>;

  selectItem: (id: string | null) => Promise<void>;
  createItem: (input?: CreateKnowledgeItemInput) => Promise<KnowledgeItem>;
  /** 本地即时更新选中条目并防抖持久化 */
  updateSelected: (patch: EditablePatch) => void;
  /** 立即落盘（Ctrl+S / 切换条目 / 关闭前） */
  flushPendingSave: () => Promise<void>;

  /** 外部（AI 服务等）直接持久化后的条目写回同步 */
  applyServerItem: (item: KnowledgeItem) => void;

  /** 直接覆盖某条目的标签（列表右键编辑用；详情页走 updateSelected 的防抖保存） */
  setItemTags: (id: string, tagNames: string[]) => Promise<void>;
  setStatus: (ids: string[], status: KnowledgeItemStatus) => Promise<void>;
  toggleFavorite: (id: string) => Promise<void>;
  togglePinned: (id: string) => Promise<void>;
  moveToTrash: (ids: string[]) => Promise<void>;
  restoreItems: (ids: string[]) => Promise<void>;
  deleteForever: (ids: string[]) => Promise<void>;
  emptyTrash: () => Promise<void>;
}

export const useKnowledgeStore = create<KnowledgeState>()((set, get) => {
  const buildQuery = () => {
    const state = get();
    return {
      scope: state.scope,
      collectionId: state.collectionId ?? undefined,
      tagId: state.tagId ?? undefined,
      search: state.searchQuery.trim() || undefined,
      sortBy: state.sortBy,
      sortOrder: state.sortOrder,
    };
  };

  const scheduleSave = (itemId: string) => {
    pendingItemId = itemId;
    set({ hasUnsavedChanges: true });
    if (saveTimer) {
      clearTimeout(saveTimer);
      saveTimer = null;
    }
    // autoSave 关闭时不排定时器；改动保留在 pendingPatch，
    // 由 Ctrl+S / 保存按钮 / 切换条目时的 flushPendingSave 落盘
    if (!useSettingsStore.getState().autoSave) {
      return;
    }
    saveTimer = setTimeout(() => {
      void get().flushPendingSave();
    }, AUTO_SAVE_DEBOUNCE_MS);
  };

  const applyItemToList = (item: KnowledgeItem) => {
    set((state) => ({
      entries: state.entries.map((entry) =>
        entry.id === item.id
          ? {
              ...entry,
              title: item.title,
              status: item.status,
              collectionId: item.collectionId,
              isFavorite: item.isFavorite,
              isPinned: item.isPinned,
              updatedAt: item.updatedAt,
              tags: item.tags,
            }
          : entry,
      ),
    }));
  };

  return {
    scope: "all",
    collectionId: null,
    tagId: null,
    searchQuery: "",
    sortBy: "updatedAt",
    sortOrder: "desc",
    entries: [],
    total: 0,
    isLoading: false,
    counts: null,
    selectedId: null,
    selectedItem: null,
    isSaving: false,
    hasUnsavedChanges: false,
    selectionIds: [],
    selectionAnchorId: null,

    setScope: (scope) => {
      void get().flushPendingSave();
      set({
        scope,
        collectionId: null,
        tagId: null,
        selectedId: null,
        selectedItem: null,
        selectionIds: [],
        selectionAnchorId: null,
      });
      void get().fetchList();
    },
    selectCollection: (collectionId) => {
      void get().flushPendingSave();
      set({
        scope: "all",
        collectionId,
        tagId: null,
        selectedId: null,
        selectedItem: null,
        selectionIds: [],
        selectionAnchorId: null,
      });
      void get().fetchList();
    },
    selectTag: (tagId) => {
      void get().flushPendingSave();
      set({
        scope: "all",
        collectionId: null,
        tagId,
        selectedId: null,
        selectedItem: null,
        selectionIds: [],
        selectionAnchorId: null,
      });
      void get().fetchList();
    },
    setSearchQuery: (query) => {
      set({ searchQuery: query, selectionIds: [], selectionAnchorId: null });
      void get().fetchList();
    },

    setSort: (sortBy, sortOrder) => {
      set({ sortBy, sortOrder });
      void get().fetchList();
    },

    toggleSelection: (id) => {
      set((state) => {
        const exists = state.selectionIds.includes(id);
        return {
          selectionIds: exists
            ? state.selectionIds.filter((candidate) => candidate !== id)
            : [...state.selectionIds, id],
          selectionAnchorId: id,
        };
      });
    },

    rangeSelectTo: (id) => {
      set((state) => {
        const anchorId =
          state.selectionAnchorId ?? state.selectedId ?? state.entries[0]?.id;
        if (!anchorId) {
          return { selectionIds: [id], selectionAnchorId: id };
        }
        const ids = state.entries.map((entry) => entry.id);
        const anchorIndex = ids.indexOf(anchorId);
        const targetIndex = ids.indexOf(id);
        if (anchorIndex === -1 || targetIndex === -1) {
          return { selectionIds: [id], selectionAnchorId: id };
        }
        const [from, to] =
          anchorIndex <= targetIndex
            ? [anchorIndex, targetIndex]
            : [targetIndex, anchorIndex];
        return {
          selectionIds: ids.slice(from, to + 1),
          selectionAnchorId: anchorId,
        };
      });
    },

    setSelection: (ids) => {
      set({
        selectionIds: ids,
        selectionAnchorId: ids[ids.length - 1] ?? null,
      });
    },

    clearSelection: () => {
      set({ selectionIds: [], selectionAnchorId: null });
    },

    bulkMoveToCollection: async (ids, collectionId) => {
      for (const id of ids) {
        await window.api.knowledge.update(id, { collectionId });
      }
      await get().refreshAll();
    },

    fetchList: async () => {
      set({ isLoading: true });
      try {
        const result = await window.api.knowledge.list(buildQuery());
        set((state) => ({
          entries: result.entries,
          total: result.total,
          // 清掉已不在当前列表中的选中项（被移出视图 / 删除）
          selectionIds: state.selectionIds.filter((id) =>
            result.entries.some((entry) => entry.id === id),
          ),
        }));
      } catch (error) {
        console.error("加载知识条目列表失败:", error);
      } finally {
        set({ isLoading: false });
      }
    },

    refreshCounts: async () => {
      try {
        const counts = await window.api.knowledge.counts();
        set({ counts });
      } catch (error) {
        console.error("加载侧栏计数失败:", error);
      }
    },

    refreshAll: async () => {
      await Promise.all([get().fetchList(), get().refreshCounts()]);
    },

    selectItem: async (id) => {
      await get().flushPendingSave();
      if (!id) {
        set({ selectedId: null, selectedItem: null });
        return;
      }
      set({ selectedId: id });
      try {
        const item = await window.api.knowledge.get(id);
        // 异步返回时用户可能已切换选择
        if (get().selectedId === id) {
          set({ selectedItem: item });
        }
      } catch (error) {
        console.error("加载条目详情失败:", error);
      }
    },

    createItem: async (input) => {
      await get().flushPendingSave();
      const created = await window.api.knowledge.create(input ?? {});
      await get().refreshAll();
      set({ selectedId: created.id, selectedItem: created });
      return created;
    },

    updateSelected: (patch) => {
      const current = get().selectedItem;
      if (!current) {
        return;
      }
      const next: KnowledgeItem = {
        ...current,
        title: patch.title !== undefined ? patch.title : current.title,
        content: patch.content !== undefined ? patch.content : current.content,
        collectionId:
          patch.collectionId !== undefined
            ? patch.collectionId
            : current.collectionId,
      };
      set({ selectedItem: next });
      pendingPatch = { ...pendingPatch, ...patch };
      scheduleSave(current.id);
    },

    flushPendingSave: async () => {
      if (saveTimer) {
        clearTimeout(saveTimer);
        saveTimer = null;
      }
      const itemId = pendingItemId;
      const patch = pendingPatch;
      pendingItemId = null;
      pendingPatch = {};
      set({ hasUnsavedChanges: false });
      if (!itemId || Object.keys(patch).length === 0) {
        return;
      }
      set({ isSaving: true });
      try {
        const updated = await window.api.knowledge.update(itemId, patch);
        if (updated) {
          applyItemToList(updated);
          if (get().selectedId === updated.id) {
            // 保留用户可能仍在输入的本地内容，仅同步服务器权威字段
            set((state) => ({
              selectedItem: state.selectedItem
                ? {
                    ...state.selectedItem,
                    updatedAt: updated.updatedAt,
                    tags: updated.tags,
                  }
                : updated,
            }));
          }
        }
      } catch (error) {
        console.error("保存条目失败:", error);
      } finally {
        set({ isSaving: false });
      }
    },

    applyServerItem: (item) => {
      applyItemToList(item);
      if (get().selectedId === item.id) {
        set({ selectedItem: item });
      }
    },

    setItemTags: async (id, tagNames) => {
      const updated = await window.api.knowledge.update(id, { tagNames });
      if (!updated) {
        return;
      }
      applyItemToList(updated);
      if (get().selectedId === id) {
        set({ selectedItem: updated });
      }
      // 可能新建了标签，侧栏标签列表要跟上
      await useTagStore.getState().fetchTags();
    },

    setStatus: async (ids, status) => {
      await window.api.knowledge.setStatus(ids, status);
      const { selectedId } = get();
      await get().refreshAll();
      if (selectedId && ids.includes(selectedId)) {
        await get().selectItem(selectedId);
      }
    },

    toggleFavorite: async (id) => {
      const entry = get().entries.find((candidate) => candidate.id === id);
      const selected = get().selectedItem;
      const currentValue =
        selected?.id === id ? selected.isFavorite : entry?.isFavorite ?? false;
      const updated = await window.api.knowledge.update(id, {
        isFavorite: !currentValue,
      });
      if (updated) {
        applyItemToList(updated);
        if (get().selectedId === id) {
          set({ selectedItem: updated });
        }
      }
      await get().refreshCounts();
    },

    togglePinned: async (id) => {
      const entry = get().entries.find((candidate) => candidate.id === id);
      const selected = get().selectedItem;
      const currentValue =
        selected?.id === id ? selected.isPinned : entry?.isPinned ?? false;
      const updated = await window.api.knowledge.update(id, {
        isPinned: !currentValue,
      });
      if (updated) {
        if (get().selectedId === id) {
          set({ selectedItem: updated });
        }
        await get().fetchList();
      }
    },

    moveToTrash: async (ids) => {
      await window.api.knowledge.moveToTrash(ids);
      if (ids.includes(get().selectedId ?? "")) {
        set({ selectedId: null, selectedItem: null });
      }
      await get().refreshAll();
    },

    restoreItems: async (ids) => {
      await window.api.knowledge.restore(ids);
      if (ids.includes(get().selectedId ?? "")) {
        set({ selectedId: null, selectedItem: null });
      }
      await get().refreshAll();
    },

    deleteForever: async (ids) => {
      await window.api.knowledge.deleteForever(ids);
      if (ids.includes(get().selectedId ?? "")) {
        set({ selectedId: null, selectedItem: null });
      }
      await get().refreshAll();
    },

    emptyTrash: async () => {
      await window.api.knowledge.emptyTrash();
      set({ selectedId: null, selectedItem: null });
      await get().refreshAll();
    },
  };
});
