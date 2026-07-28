import { create } from "zustand";
import type {
  BulkUpdateKnowledgeItemsInput,
  CreateKnowledgeItemInput,
  KnowledgeCounts,
  KnowledgeItem,
  KnowledgeItemListEntry,
  KnowledgeItemStatus,
  KnowledgeScope,
  KnowledgeSortField,
  KnowledgeSortOrder,
  Tag,
  UpdateKnowledgeItemInput,
} from "@guizhi/shared/types";
import { runGuardedMutation } from "./operation-error.store";
import { describeLoadError } from "./load-error";
import { useSettingsStore } from "./settings.store";
import { useTagStore } from "./tag.store";

const AUTO_SAVE_DEBOUNCE_MS = 800;
export const DEFAULT_PAGE_SIZE = 20;
export const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

/** fetchList 的请求序号：只有最后一次发出的请求可以写回结果 */
let listRequestSeq = 0;

/**
 * 可本地编辑并防抖持久化的字段。
 *
 * 分类不在其中：改分类是一次命令，不是正文编辑，走 bulkMoveToCollection
 * 立即落盘并重取列表与计数（详情页 chip 与列表右键菜单共用这一条路）。
 */
type EditablePatch = Pick<
  UpdateKnowledgeItemInput,
  "title" | "content" | "tagNames"
>;

/**
 * 待落盘的编辑，按条目 id 分桶。
 *
 * 用单个 pendingPatch 变量存不住并发：A 的保存请求在途时切到 B 继续输入，
 * A 失败回退会把 B 的正文并进 A 的 patch，下一次落盘就把 B 的内容写进了 A。
 */
const pendingPatches = new Map<string, EditablePatch>();
/** 每个条目最近一次落盘的 Promise，切条目前要等它真正结束 */
const inflightSaves = new Map<string, Promise<void>>();
let saveTimer: ReturnType<typeof setTimeout> | null = null;

/** 仅用于测试：清空跨用例残留的待保存队列 */
export function __resetPendingSaves(): void {
  pendingPatches.clear();
  inflightSaves.clear();
  if (saveTimer) {
    clearTimeout(saveTimer);
    saveTimer = null;
  }
}

/**
 * 本地即时回显的标签列表：已存在的标签沿用原对象，新名字生成临时占位。
 *
 * 不回显的话，标签浮层下一次 onChange 仍然基于旧的 item.tags 做全量覆盖，
 * 防抖窗口内连加两个标签，第一个会被第二个的 patch 覆盖掉。
 */
function reconcileOptimisticTags(existing: Tag[], tagNames: string[]): Tag[] {
  const byName = new Map(existing.map((tag) => [tag.name.toLowerCase(), tag]));
  const now = Date.now();
  return tagNames.map(
    (name) =>
      byName.get(name.toLowerCase()) ?? {
        id: `pending:${name.toLowerCase()}`,
        name,
        colorKey: "gray",
        createdAt: now,
        updatedAt: now,
      },
  );
}

interface KnowledgeState {
  // ── 导航（范围 / 知识库 / 标签 / 平台四者互斥）──
  scope: KnowledgeScope;
  collectionId: string | null;
  tagId: string | null;
  /** 采集来源平台（SourcePlatform） */
  platform: string | null;
  searchQuery: string;
  // ── 排序（搜索态下由相关度接管） ──
  sortBy: KnowledgeSortField;
  sortOrder: KnowledgeSortOrder;
  // ── 列表（服务端分页：entries 只含当前页，total 是过滤后的总数）──
  entries: KnowledgeItemListEntry[];
  total: number;
  page: number;
  pageSize: number;
  isLoading: boolean;
  /** 列表读取失败的原因；为空表示读取正常（列表真的是空的） */
  loadError: string | null;
  // ── 计数 ──
  counts: KnowledgeCounts | null;
  // ── 详情 ──
  selectedId: string | null;
  selectedItem: KnowledgeItem | null;
  isSaving: boolean;
  /** 有未落盘的编辑（autoSave 关闭时由保存按钮 / Ctrl+S 落盘） */
  hasUnsavedChanges: boolean;
  /** 上次保存失败的原因；改动已退回待保存队列，可重试。成功保存后清空 */
  saveError: string | null;
  // ── 批量多选 ──
  selectionIds: string[];
  selectionAnchorId: string | null;

  setScope: (scope: KnowledgeScope) => void;
  selectCollection: (collectionId: string | null) => void;
  selectTag: (tagId: string | null) => void;
  selectPlatform: (platform: string | null) => void;
  setSearchQuery: (query: string) => void;
  setSort: (sortBy: KnowledgeSortField, sortOrder: KnowledgeSortOrder) => void;
  setPage: (page: number) => void;
  setPageSize: (pageSize: number) => void;

  /** Ctrl/Cmd+点击：切换单条选中态 */
  toggleSelection: (id: string) => void;
  /** Shift+点击：从锚点到目标的连续范围选择 */
  rangeSelectTo: (id: string) => void;
  /** 覆盖式设置选中集合（表格视图的「全选本页」） */
  setSelection: (ids: string[]) => void;
  clearSelection: () => void;
  /** 一次 IPC 改一批条目（移动 / 收藏 / 置顶 / 加减标签） */
  bulkUpdate: (
    ids: string[],
    patch: BulkUpdateKnowledgeItemsInput,
  ) => Promise<void>;
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
  /** 返回是否移动成功；调用方据此决定要不要弹撤销提示 */
  moveToTrash: (ids: string[]) => Promise<boolean>;
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
      platform: state.platform ?? undefined,
      search: state.searchQuery.trim() || undefined,
      sortBy: state.sortBy,
      sortOrder: state.sortOrder,
      limit: state.pageSize,
      offset: (state.page - 1) * state.pageSize,
    };
  };

  const scheduleSave = () => {
    set({ hasUnsavedChanges: true });
    if (saveTimer) {
      clearTimeout(saveTimer);
      saveTimer = null;
    }
    // autoSave 关闭时不排定时器；改动保留在 pendingPatches，
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

  /**
   * 翻转收藏 / 置顶。
   *
   * 两者原本一个只改本地行、一个整表重取：在「收藏」范围里取消收藏，
   * 条目会留在列表里不走；而置顶会让整个列表重排、滚动位置丢失。
   * 现在统一——本地先回显，再重取列表与计数（两者都会改变过滤与排序结果）。
   * 翻转值以在途请求为准，连点两次不会都发同一个值。
   */
  const pendingFlags = new Map<string, boolean>();
  const toggleFlag = async (
    id: string,
    field: "isFavorite" | "isPinned",
  ): Promise<void> => {
    const key = `${field}:${id}`;
    const state = get();
    const entry = state.entries.find((candidate) => candidate.id === id);
    const selected = state.selectedItem;
    const known =
      selected?.id === id ? selected[field] : (entry?.[field] ?? false);
    const nextValue = !(pendingFlags.get(key) ?? known);
    pendingFlags.set(key, nextValue);

    try {
      const updated = await window.api.knowledge.update(id, {
        [field]: nextValue,
      });
      if (updated) {
        applyItemToList(updated);
        if (get().selectedId === id) {
          set({ selectedItem: updated });
        }
      }
      await get().refreshAll();
    } finally {
      if (pendingFlags.get(key) === nextValue) {
        pendingFlags.delete(key);
      }
    }
  };

  /** 落盘单个条目的待存改动；失败只退回它自己那一桶。 */
  const persistPatch = async (itemId: string): Promise<void> => {
    const patch = pendingPatches.get(itemId);
    pendingPatches.delete(itemId);
    if (!patch || Object.keys(patch).length === 0) {
      return;
    }
    set({ isSaving: true, saveError: null });
    try {
      const updated = await window.api.knowledge.update(itemId, patch);
      if (!updated) {
        throw new Error("条目不存在或已被删除");
      }
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
      // 标签增删会改侧栏的标签列表与按标签计数；标题与正文不会，
      // 不必让每次防抖落盘都多打这两次查询。
      // 列表要重取是因为详情页新建的标签是 update 在 DAO 里顺手建出来的，
      // 不经 tagStore.createTag 那条自带刷新的路径。
      if (patch.tagNames) {
        await useTagStore.getState().fetchTags();
        await get().refreshCounts();
      }
    } catch (error) {
      // 退回本条目自己的桶；await 期间对同一条目的新输入优先
      pendingPatches.set(itemId, { ...patch, ...pendingPatches.get(itemId) });
      set({
        saveError: error instanceof Error ? error.message : String(error),
      });
      console.error("保存条目失败:", error);
    } finally {
      set({ isSaving: false, hasUnsavedChanges: pendingPatches.size > 0 });
    }
  };

  /**
   * 切换导航轴。四条轴互斥：选中其一即清掉其余三条，并回到第一页、
   * 清空详情与多选。缺省的字段一律复位，调用方只需给出自己那一条。
   */
  const navigateTo = (target: {
    scope?: KnowledgeScope;
    collectionId?: string | null;
    tagId?: string | null;
    platform?: string | null;
  }) => {
    void get().flushPendingSave();
    set({
      scope: target.scope ?? "all",
      collectionId: target.collectionId ?? null,
      tagId: target.tagId ?? null,
      platform: target.platform ?? null,
      page: 1,
      selectedId: null,
      selectedItem: null,
      selectionIds: [],
      selectionAnchorId: null,
    });
    void get().fetchList();
  };

  return {
    scope: "all",
    collectionId: null,
    tagId: null,
    platform: null,
    searchQuery: "",
    sortBy: "updatedAt",
    sortOrder: "desc",
    entries: [],
    total: 0,
    page: 1,
    pageSize: DEFAULT_PAGE_SIZE,
    isLoading: false,
    loadError: null,
    counts: null,
    selectedId: null,
    selectedItem: null,
    isSaving: false,
    hasUnsavedChanges: false,
    saveError: null,
    selectionIds: [],
    selectionAnchorId: null,

    setScope: (scope) => navigateTo({ scope }),
    selectCollection: (collectionId) => navigateTo({ collectionId }),
    selectTag: (tagId) => navigateTo({ tagId }),
    selectPlatform: (platform) => navigateTo({ platform }),
    setSearchQuery: (query) => {
      set({
        searchQuery: query,
        page: 1,
        selectionIds: [],
        selectionAnchorId: null,
      });
      void get().fetchList();
    },

    setSort: (sortBy, sortOrder) => {
      set({ sortBy, sortOrder, page: 1 });
      void get().fetchList();
    },

    setPage: (page) => {
      set({ page: Math.max(1, page) });
      void get().fetchList();
    },

    setPageSize: (pageSize) => {
      // 换每页条数后停留在原页码会越过总页数，统一回到第一页
      set({ pageSize: Math.max(1, pageSize), page: 1 });
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

    bulkUpdate: async (ids, patch) => {
      if (ids.length === 0) {
        return;
      }
      await runGuardedMutation(
        "library.actionBulkUpdate",
        "批量更新",
        async () => {
          await window.api.knowledge.bulkUpdate(ids, patch);
          await get().refreshAll();
          // 可能建了新标签，侧栏标签列表要跟上
          if (patch.addTagNames?.length) {
            await useTagStore.getState().fetchTags();
          }
          const { selectedId } = get();
          if (selectedId && ids.includes(selectedId)) {
            await get().selectItem(selectedId);
          }
        },
      );
    },

    bulkMoveToCollection: async (ids, collectionId) => {
      await get().bulkUpdate(ids, { collectionId });
    },

    fetchList: async () => {
      const requestId = ++listRequestSeq;
      set({ isLoading: true, loadError: null });
      try {
        const result = await window.api.knowledge.list(buildQuery());
        // 快速切换范围时，先发出的慢请求可能后到；只认最后一次
        if (requestId !== listRequestSeq) {
          return;
        }

        // 当前页越界（例如删光了最后一页）：回退到最后一页重取
        const state = get();
        const lastPage = Math.max(
          1,
          Math.ceil(result.total / state.pageSize) || 1,
        );
        if (result.entries.length === 0 && state.page > lastPage) {
          set({ page: lastPage });
          await get().fetchList();
          return;
        }

        set((current) => ({
          entries: result.entries,
          total: result.total,
          // 清掉已不在当前页的选中项（被移出视图 / 删除 / 翻页）
          selectionIds: current.selectionIds.filter((id) =>
            result.entries.some((entry) => entry.id === id),
          ),
        }));
      } catch (error) {
        console.error("加载知识条目列表失败:", error);
        // 不记下来的话，读失败会被渲染成「暂无条目」，用户以为条目真没了
        if (requestId === listRequestSeq) {
          set({ loadError: describeLoadError(error) });
        }
      } finally {
        // 只有最后一次请求负责收起加载态，避免先返回的请求提前关掉 Spinner
        if (requestId === listRequestSeq) {
          set({ isLoading: false });
        }
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
        tags:
          patch.tagNames !== undefined
            ? reconcileOptimisticTags(current.tags, patch.tagNames)
            : current.tags,
      };
      set({ selectedItem: next });
      pendingPatches.set(current.id, {
        ...pendingPatches.get(current.id),
        ...patch,
      });
      scheduleSave();
    },

    flushPendingSave: async () => {
      if (saveTimer) {
        clearTimeout(saveTimer);
        saveTimer = null;
      }
      // 先等在途落盘收尾：它可能失败并把改动退回桶里，那一份也要一起处理
      await Promise.allSettled([...inflightSaves.values()]);

      const itemIds = [...pendingPatches.keys()];
      if (itemIds.length === 0) {
        set({ hasUnsavedChanges: false });
        return;
      }

      const runs = itemIds.map((itemId) => {
        const run = persistPatch(itemId);
        inflightSaves.set(itemId, run);
        void run.finally(() => {
          if (inflightSaves.get(itemId) === run) {
            inflightSaves.delete(itemId);
          }
        });
        return run;
      });
      await Promise.allSettled(runs);
    },

    applyServerItem: (item) => {
      applyItemToList(item);
      if (get().selectedId === item.id) {
        set({ selectedItem: item });
      }
    },

    setItemTags: async (id, tagNames) => {
      await runGuardedMutation(
        "library.actionSetTags",
        "更新标签",
        async () => {
          const updated = await window.api.knowledge.update(id, { tagNames });
          if (!updated) {
            return;
          }
          applyItemToList(updated);
          if (get().selectedId === id) {
            set({ selectedItem: updated });
          }
          // 可能新建了标签，侧栏的标签列表与计数都要跟上
          await useTagStore.getState().fetchTags();
          await get().refreshCounts();
        },
      );
    },

    setStatus: async (ids, status) => {
      await runGuardedMutation(
        "library.actionSetStatus",
        "更新状态",
        async () => {
          await window.api.knowledge.setStatus(ids, status);
          const { selectedId } = get();
          await get().refreshAll();
          if (selectedId && ids.includes(selectedId)) {
            await get().selectItem(selectedId);
          }
        },
      );
    },

    toggleFavorite: async (id) => {
      await runGuardedMutation("library.actionSetFlag", "更新标记", () =>
        toggleFlag(id, "isFavorite"),
      );
    },

    togglePinned: async (id) => {
      await runGuardedMutation("library.actionSetFlag", "更新标记", () =>
        toggleFlag(id, "isPinned"),
      );
    },

    // 返回是否真的移动成功：调用方据此决定要不要弹「已移到回收站」的撤销提示，
    // 否则删失败了还会弹一句成功文案，用户以为删掉了
    moveToTrash: async (ids) =>
      runGuardedMutation(
        "library.actionMoveToTrash",
        "移到回收站",
        async () => {
          await window.api.knowledge.moveToTrash(ids);
          if (ids.includes(get().selectedId ?? "")) {
            set({ selectedId: null, selectedItem: null });
          }
          await get().refreshAll();
        },
      ),

    restoreItems: async (ids) => {
      await runGuardedMutation("library.actionRestore", "恢复条目", async () => {
        await window.api.knowledge.restore(ids);
        if (ids.includes(get().selectedId ?? "")) {
          set({ selectedId: null, selectedItem: null });
        }
        await get().refreshAll();
      });
    },

    deleteForever: async (ids) => {
      await runGuardedMutation(
        "library.actionDeleteForever",
        "彻底删除",
        async () => {
          await window.api.knowledge.deleteForever(ids);
          if (ids.includes(get().selectedId ?? "")) {
            set({ selectedId: null, selectedItem: null });
          }
          await get().refreshAll();
        },
      );
    },

    emptyTrash: async () => {
      await runGuardedMutation(
        "library.actionEmptyTrash",
        "清空回收站",
        async () => {
          await window.api.knowledge.emptyTrash();
          set({ selectedId: null, selectedItem: null });
          await get().refreshAll();
        },
      );
    },
  };
});
