import { create } from "zustand";
import type {
  Collection,
  CreateCollectionInput,
  UpdateCollectionInput,
} from "@guizhi/shared/types";
import { translate } from "../i18n";
import { useKnowledgeStore } from "./knowledge.store";
import { runGuardedMutation } from "./operation-error.store";

/**
 * 撤销窗口内已从界面隐藏、尚未提交给主进程的合集删除。
 * 重新拉取列表时必须绕开它，否则被删的合集会在窗口内闪回来。
 */
const pendingDeletions = new Map<string, Collection>();
/** 与撤销提示条的停留时长一致 */
const DELETION_UNDO_WINDOW_MS = 8000;
/** 尚未提交的删除；退出页面前统一提交，避免关窗口把「已删除」的合集又带回来 */
const activeDeletions = new Set<() => Promise<void>>();

if (typeof window !== "undefined") {
  window.addEventListener("beforeunload", () => {
    for (const commit of [...activeDeletions]) void commit();
  });
}

export interface ScheduledDeletion {
  /** 窗口内撤销：合集及其条目归属原样回来 */
  undo: () => void;
  /** 立即提交（窗口结束时自动调用） */
  commit: () => Promise<void>;
}

function hidePendingDeletions(collections: Collection[]): Collection[] {
  return pendingDeletions.size === 0
    ? collections
    : collections.filter((collection) => !pendingDeletions.has(collection.id));
}

interface CollectionState {
  collections: Collection[];
  isLoading: boolean;
  fetchCollections: () => Promise<void>;
  createCollection: (input: CreateCollectionInput) => Promise<Collection>;
  updateCollection: (
    id: string,
    input: UpdateCollectionInput,
  ) => Promise<Collection | null>;
  /**
   * 删除合集（其中条目保留为未归合集）：先从界面隐藏，窗口内可撤销，之后才提交。
   * 合集不存在或已在撤销窗口内时返回 null。
   */
  scheduleDeleteCollection: (
    id: string,
    delayMs?: number,
  ) => ScheduledDeletion | null;
}

export const useCollectionStore = create<CollectionState>()((set, get) => ({
  collections: [],
  isLoading: false,

  fetchCollections: async () => {
    set({ isLoading: true });
    try {
      const collections = await window.api.collection.list();
      set({ collections: hidePendingDeletions(collections) });
    } catch (error) {
      console.error("加载集合列表失败:", error);
    } finally {
      set({ isLoading: false });
    }
  },

  createCollection: async (input) => {
    const created = await window.api.collection.create(input);
    await get().fetchCollections();
    return created;
  },

  updateCollection: async (id, input) => {
    const updated = await window.api.collection.update(id, input);
    await get().fetchCollections();
    return updated;
  },

  scheduleDeleteCollection: (id, delayMs = DELETION_UNDO_WINDOW_MS) => {
    const target = get().collections.find((collection) => collection.id === id);
    if (!target || pendingDeletions.has(id)) return null;
    pendingDeletions.set(id, target);
    set((state) => ({ collections: hidePendingDeletions(state.collections) }));

    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const release = () => {
      pendingDeletions.delete(id);
      activeDeletions.delete(commit);
    };
    const commit = async (): Promise<void> => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      // 提交期间保持隐藏，避免提交中的刷新让被删的合集闪回
      await runGuardedMutation(
        "library.deleteCollection",
        translate("library.deleteCollection", "删除合集"),
        async () => {
          const removed = await window.api.collection.delete(id);
          if (!removed) throw new Error("合集已不存在，请刷新列表");
        },
      );
      release();
      // 提交失败的合集会随这次刷新回到列表，并已有原因提示
      await get().fetchCollections();
      await useKnowledgeStore.getState().refreshAll();
    };
    timer = setTimeout(() => void commit(), delayMs);
    activeDeletions.add(commit);
    return {
      commit,
      undo: () => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        release();
        void get().fetchCollections();
      },
    };
  },
}));