import type { SaveKnowledgeDraftResult } from "@guizhi/shared/types/knowledge-draft";
import { mergeDraftTags } from "@guizhi/shared/types/knowledge-draft";
import { create } from "zustand";
import type {
  BulkUpdateKnowledgeItemsInput,
  CreateKnowledgeItemInput,
  KnowledgeCounts,
  KnowledgeFacetCountsQuery,
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
/** refreshCounts 同样可能乱序返回；不能让旧筛选的数字覆盖新筛选。 */
let countsRequestSeq = 0;

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

/** 可保存/恢复的知识库筛选快照。搜索词刻意不写入，避免打开视图时带回过期的临时检索。 */
export interface LibraryFacetFilters {
  scope: KnowledgeScope;
  collectionId: string | null;
  tagId: string | null;
  platform: string | null;
}

/**
 * 待落盘的编辑，按条目 id 分桶。
 *
 * 用单个 pendingPatch 变量存不住并发：A 的保存请求在途时切到 B 继续输入，
 * A 失败回退会把 B 的正文并进 A 的 patch，下一次落盘就把 B 的内容写进了 A。
 */
const pendingPatches = new Map<string, EditablePatch>();
/** 每个条目最近一次落盘的 Promise，切条目前要等它真正结束 */
const inflightSaves = new Map<string, Promise<boolean>>();
const savingPatches = new Map<string, EditablePatch>();
const draftBases = new Map<string, EditablePatch>();
let flushRun: Promise<boolean> | null = null;
let detailRequestSeq = 0;
const fieldsOf = (item: KnowledgeItem): EditablePatch => ({ title: item.title, content: item.content, tagNames: item.tags.map(tag => tag.name) });
let saveTimer: ReturnType<typeof setTimeout> | null = null;

/** 仅用于测试：清空跨用例残留的待保存队列 */
export function __resetPendingSaves(): void {
  pendingPatches.clear();
  savingPatches.clear(); draftBases.clear(); flushRun = null; detailRequestSeq++;
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
  // ── 筛选（范围 + 知识库 + 标签 + 平台可组合）──
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
  /** 已访问页的起始 keyset 游标；第 1 页固定为 null。 */
  pageCursors: Record<number, string | null>;
  isLoading: boolean;
  /** 列表读取失败的原因；为空表示读取正常（列表真的是空的） */
  loadError: string | null;
  // ── 计数 ──
  counts: KnowledgeCounts | null;
  // ── 详情 ──
  selectedId: string | null;
  selectedItem: KnowledgeItem | null;
  detailLoading: boolean;
  detailError: string | null;
  saveConflict: { itemId: string; result: SaveKnowledgeDraftResult } | null;
  resolveSaveConflict: (choice: "local" | "server") => Promise<boolean>;
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
  /** 清空知识库 / 标签 / 平台三类细分条件，保留当前范围与搜索词 */
  clearFacetFilters: () => void;
  /** 智能视图恢复筛选快照；与点击侧栏同样会重置分页和多选 */
  applyFacetFilters: (filters: LibraryFacetFilters) => void;
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

  selectItem: (id: string | null) => Promise<boolean>;
  createItem: (input?: CreateKnowledgeItemInput) => Promise<KnowledgeItem>;
  /** 本地即时更新选中条目并防抖持久化 */
  updateSelected: (patch: EditablePatch) => void;
  /** 立即落盘（Ctrl+S / 切换条目 / 关闭前） */
  flushPendingSave: () => Promise<boolean>;

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
  deleteForever: (ids: string[], options?: { clearEvidence?: boolean }) => Promise<boolean>;
  emptyTrash: (options?: { clearEvidence?: boolean }) => Promise<boolean>;
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
      ...(state.page === 1
        ? { cursor: null, offset: 0 }
        : typeof state.pageCursors[state.page] === "string"
          ? { cursor: state.pageCursors[state.page] }
          : { offset: (state.page - 1) * state.pageSize }),
    };
  };

  const buildFacetCountsQuery = (): KnowledgeFacetCountsQuery => {
    const state = get();
    return {
      scope: state.scope,
      collectionId: state.collectionId ?? undefined,
      tagId: state.tagId ?? undefined,
      platform: state.platform ?? undefined,
      search: state.searchQuery.trim() || undefined,
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

  const mergeServerItem = (item: KnowledgeItem): KnowledgeItem => {
    const patch = { ...savingPatches.get(item.id), ...pendingPatches.get(item.id) };
    const base = draftBases.get(item.id);
    return { ...item, ...("title" in patch ? { title: patch.title } : {}),
      ...("content" in patch ? { content: patch.content } : {}),
      ...(patch.tagNames ? { tags: reconcileOptimisticTags(item.tags, mergeDraftTags(base?.tagNames ?? [], patch.tagNames, item.tags.map(tag => tag.name))) } : {}) };
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
          set({ selectedItem: mergeServerItem(updated) });
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
  const persistPatch = async (itemId: string): Promise<boolean> => {
    const patch = pendingPatches.get(itemId);
    pendingPatches.delete(itemId);
    if (!patch || !Object.keys(patch).length) return true;
    savingPatches.set(itemId, patch);
    set({ isSaving: true, saveError: null });
    try {
      // 老运行时和测试替身保留 update 兼容；桌面始终经事务草稿接口保存。
      const result: SaveKnowledgeDraftResult = window.api.knowledge.saveDraft
        ? await window.api.knowledge.saveDraft({ id: itemId, requestId: crypto.randomUUID(), base: draftBases.get(itemId) ?? {}, patch })
        : { ok: true, item: await window.api.knowledge.update(itemId, patch) };
      if (!result.ok || !result.item) {
        if (result.conflicts?.length) set({ saveConflict: { itemId, result } });
        throw new Error(result.error || "条目不存在或已被删除");
      }
      const updated = result.item;
      const later = pendingPatches.get(itemId), previousBase = draftBases.get(itemId) ?? {};
      const nextBase = fieldsOf(updated);
      if (later) {
        if (later.tagNames) later.tagNames = mergeDraftTags(patch.tagNames ?? previousBase.tagNames ?? [], later.tagNames, updated.tags.map(t => t.name));
        for (const field of ["title", "content"] as const) {
          if (later[field] !== undefined && patch[field] === undefined) nextBase[field] = previousBase[field];
        }
      }
      draftBases.set(itemId, nextBase);
      savingPatches.delete(itemId);
      applyItemToList(updated);
      if (get().selectedId === itemId) set({ selectedItem: mergeServerItem(updated) });
      if (get().saveConflict?.itemId === itemId) set({ saveConflict: null });
      if (patch.tagNames) { await useTagStore.getState().fetchTags(); await get().refreshCounts(); }
      return true;
    } catch (error) {
      pendingPatches.set(itemId, { ...patch, ...pendingPatches.get(itemId) });
      set({ saveError: error instanceof Error ? error.message : String(error) });
      console.error("保存条目失败:", error);
      return false;
    } finally {
      savingPatches.delete(itemId);
      if (!pendingPatches.has(itemId)) draftBases.delete(itemId);
      set({ isSaving: false, hasUnsavedChanges: pendingPatches.size > 0 });
    }
  };

  /**
   * 更新筛选条件。范围与三个 facet 是正交的：例如“收藏的抖音条目”与
   * “工作库里的某标签”都是重度用户每天会用到的查询，不能再互相清空。
   */
  const updateFilters = async (target: Partial<LibraryFacetFilters>) => {
    if ((pendingPatches.size || flushRun !== null) && !(await get().flushPendingSave())) return;
    detailRequestSeq++;
    set({
      ...(target.scope !== undefined ? { scope: target.scope } : {}),
      ...(target.collectionId !== undefined
        ? { collectionId: target.collectionId }
        : {}),
      ...(target.tagId !== undefined ? { tagId: target.tagId } : {}),
      ...(target.platform !== undefined ? { platform: target.platform } : {}),
      page: 1,
      pageCursors: { 1: null },
      selectedId: null,
      selectedItem: null,
      selectionIds: [],
      selectionAnchorId: null,
    });
    void get().refreshAll();
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
    pageCursors: { 1: null },
    isLoading: false,
    loadError: null,
    counts: null,
    selectedId: null,
    selectedItem: null,
    detailLoading: false, detailError: null, saveConflict: null,
    resolveSaveConflict: async choice => {
      const conflict = get().saveConflict;
      if (!conflict?.result.item) return false;
      const item = conflict.result.item;
      const pending = pendingPatches.get(item.id);
      // 冲突确认只改变正文选择；标签必须先按旧基线重放增删，再采用新基线。
      if (pending?.tagNames) pending.tagNames = mergeDraftTags(draftBases.get(item.id)?.tagNames ?? [], pending.tagNames, item.tags.map(tag => tag.name));
      draftBases.set(item.id, fieldsOf(item));
      if (choice === "server") {
        const remaining = { ...pendingPatches.get(item.id) };
        for (const field of conflict.result.conflicts ?? []) delete remaining[field];
        if (Object.keys(remaining).length) pendingPatches.set(item.id, remaining);
        else { pendingPatches.delete(item.id); draftBases.delete(item.id); }
      }
      set({ saveConflict: null, saveError: null, hasUnsavedChanges: pendingPatches.size > 0,
        ...(get().selectedId === item.id ? { selectedItem: mergeServerItem(item) } : {}) });
      return get().flushPendingSave();
    },
    isSaving: false,
    hasUnsavedChanges: false,
    saveError: null,
    selectionIds: [],
    selectionAnchorId: null,

    setScope: (scope) => updateFilters({ scope }),
    selectCollection: (collectionId) =>
      updateFilters({
        collectionId:
          collectionId && get().collectionId === collectionId
            ? null
            : collectionId,
      }),
    selectTag: (tagId) =>
      updateFilters({
        tagId: tagId && get().tagId === tagId ? null : tagId,
      }),
    selectPlatform: (platform) =>
      updateFilters({
        platform: platform && get().platform === platform ? null : platform,
      }),
    clearFacetFilters: () =>
      updateFilters({ collectionId: null, tagId: null, platform: null }),
    applyFacetFilters: (filters) => updateFilters(filters),
    setSearchQuery: (query) => {
      set({
        searchQuery: query,
        page: 1,
        pageCursors: { 1: null },
        selectionIds: [],
        selectionAnchorId: null,
      });
      void get().refreshAll();
    },

    setSort: (sortBy, sortOrder) => {
      set({ sortBy, sortOrder, page: 1, pageCursors: { 1: null } });
      void get().fetchList();
    },

    setPage: (page) => {
      set({ page: Math.max(1, page) });
      void get().fetchList();
    },

    setPageSize: (pageSize) => {
      // 换每页条数后停留在原页码会越过总页数，统一回到第一页
      set({
        pageSize: Math.max(1, pageSize),
        page: 1,
        pageCursors: { 1: null },
      });
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
      if (window.api.knowledge.selection) {
        const { useLibraryWorkflowStore } = await import("./library-workflow.store");
        await useLibraryWorkflowStore.getState().execute(ids, { kind: "update", patch });
        if (patch.addTagNames?.length) await useTagStore.getState().fetchTags();
        return;
      }
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
          pageCursors: result.nextCursor
            ? { ...current.pageCursors, [current.page + 1]: result.nextCursor }
            : Object.fromEntries(
                Object.entries(current.pageCursors).filter(
                  ([page]) => Number(page) <= current.page,
                ),
              ),
          // 翻页保留冻结的 ID；筛选变化才清空。
          selectionIds: current.selectionIds,
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
      const requestId = ++countsRequestSeq;
      try {
        const counts = await window.api.knowledge.counts(
          buildFacetCountsQuery(),
        );
        if (requestId === countsRequestSeq) {
          set({ counts });
        }
      } catch (error) {
        if (requestId === countsRequestSeq) {
          console.error("加载侧栏计数失败:", error);
        }
      }
    },

    refreshAll: async () => {
      await Promise.all([get().fetchList(), get().refreshCounts()]);
    },

    selectItem: async (id) => {
      const seq = ++detailRequestSeq;
      if (!(await get().flushPendingSave()) || seq !== detailRequestSeq) return false;
      set({ selectedId: id, selectedItem: null, detailLoading: Boolean(id), detailError: null });
      if (!id) return true;
      try {
        const item = await window.api.knowledge.get(id);
        if (seq !== detailRequestSeq) return false;
        if (!item) throw new Error("条目不存在或已被删除");
        set({ selectedItem: mergeServerItem(item), detailLoading: false });
        return true;
      } catch (error) {
        if (seq === detailRequestSeq) set({ detailLoading: false, detailError: describeLoadError(error) });
        console.error("加载条目详情失败:", error);
        return false;
      }
    },

    createItem: async (input) => {
      const seq = ++detailRequestSeq;
      if (!(await get().flushPendingSave())) throw new Error("请先保存当前草稿");
      const created = await window.api.knowledge.create(input ?? {});
      await get().refreshAll();
      if (seq === detailRequestSeq && await get().flushPendingSave() && seq === detailRequestSeq) set({ selectedId: created.id, selectedItem: created, detailLoading: false, detailError: null });
      return created;
    },

    updateSelected: (patch) => {
      const current = get().selectedItem;
      if (!current) {
        return;
      }
      if (get().selectedId !== current.id) return;
      if (!draftBases.has(current.id)) draftBases.set(current.id, fieldsOf(current));
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
      if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; }
      if (flushRun !== null) return flushRun;
      const run = async (): Promise<boolean> => {
        // 顺序清空各桶；在途期间的新输入留在下一次循环，失败不自动无限重试。
        while (pendingPatches.size) {
          const ids = [...pendingPatches.keys()];
          for (const id of ids) {
            const saving = persistPatch(id); inflightSaves.set(id, saving);
            const ok = await saving; inflightSaves.delete(id);
            if (!ok) return false;
          }
        }
        set({ hasUnsavedChanges: false }); return true;
      };
      flushRun = run();
      try { return await flushRun; } finally { flushRun = null; }
    },

    applyServerItem: (item) => {
      applyItemToList(item);
      if (get().selectedId === item.id) {
        set({ selectedItem: mergeServerItem(item) });
      }
    },

    setItemTags: async (id, tagNames) => {
      if (get().selectedItem?.id === id && get().selectedId === id) {
        get().updateSelected({ tagNames }); await get().flushPendingSave(); return;
      }
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
            set({ selectedItem: mergeServerItem(updated) });
          }
          // 可能新建了标签，侧栏的标签列表与计数都要跟上
          await useTagStore.getState().fetchTags();
          await get().refreshCounts();
        },
      );
    },

    setStatus: async (ids, status) => {
      if (window.api.knowledge.selection) {
        const { useLibraryWorkflowStore } = await import("./library-workflow.store");
        await useLibraryWorkflowStore.getState().execute(ids, { kind: "status", status }); return;
      }
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
    moveToTrash: async (ids) => {
      if (window.api.knowledge.selection) {
        const { useLibraryWorkflowStore } = await import("./library-workflow.store");
        return useLibraryWorkflowStore.getState().execute(ids, { kind: "trash" });
      }
      return runGuardedMutation(
        "library.actionMoveToTrash",
        "移到回收站",
        async () => {
          if (!(await get().flushPendingSave())) throw new Error(get().saveError || "请先保存草稿");
          await window.api.knowledge.moveToTrash(ids);
          if (ids.includes(get().selectedId ?? "")) {
            set({ selectedId: null, selectedItem: null });
          }
          await get().refreshAll();
        },
      );
    },

    restoreItems: async (ids) => {
      if (window.api.knowledge.selection) {
        const { useLibraryWorkflowStore } = await import("./library-workflow.store");
        await useLibraryWorkflowStore.getState().execute(ids, { kind: "restore" }); return;
      }
      await runGuardedMutation(
        "library.actionRestore",
        "恢复条目",
        async () => {
          await window.api.knowledge.restore(ids);
          if (ids.includes(get().selectedId ?? "")) {
            set({ selectedId: null, selectedItem: null });
          }
          await get().refreshAll();
        },
      );
    },

    deleteForever: async (ids, options) => {
      if (window.api.knowledge.selection) {
        const { useLibraryWorkflowStore } = await import("./library-workflow.store");
        return useLibraryWorkflowStore.getState().execute(ids, { kind: "delete", clearEvidence: options?.clearEvidence });
      }
      return runGuardedMutation(
        "library.actionDeleteForever",
        "彻底删除",
        async () => {
          if (!(await get().flushPendingSave())) throw new Error(get().saveError || "请先保存草稿");
          await window.api.knowledge.deleteForever(ids, options);
          if (ids.includes(get().selectedId ?? "")) {
            set({ selectedId: null, selectedItem: null });
          }
          await get().refreshAll();
        },
      );
    },

    emptyTrash: async (options) => {
      if (window.api.knowledge.selection) {
        const result = await window.api.knowledge.selection({ action: "freeze", query: { scope: "trash" } });
        if (!result.ok || !result.ids) return runGuardedMutation("library.emptyTrash", "清空回收站", async () => { throw new Error(result.error || "读取回收站失败"); });
        return get().deleteForever(result.ids, options);
      }
      return runGuardedMutation(
        "library.actionEmptyTrash",
        "清空回收站",
        async () => {
          if (!(await get().flushPendingSave())) throw new Error(get().saveError || "请先保存草稿");
          await window.api.knowledge.emptyTrash(options);
          set({ selectedId: null, selectedItem: null });
          await get().refreshAll();
        },
      );
    },
  };
});
