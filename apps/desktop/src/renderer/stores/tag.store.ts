import { create } from "zustand";
import type { CreateTagInput, Tag, UpdateTagInput } from "@guizhi/shared/types";
import { translate } from "../i18n";
import { useKnowledgeStore } from "./knowledge.store";
import { runGuardedMutation } from "./operation-error.store";

/**
 * 撤销窗口内已从界面隐藏、尚未提交给主进程的标签删除。
 * 重新拉取列表时必须绕开它，否则被删的标签会在窗口内闪回来。
 */
const pendingDeletions = new Map<string, Tag>();
/** 与撤销提示条的停留时长一致 */
const DELETION_UNDO_WINDOW_MS = 8000;
/** 尚未提交的删除；退出页面前统一提交，避免关窗口把「已删除」的标签又带回来 */
const activeDeletions = new Set<() => Promise<void>>();

if (typeof window !== "undefined") {
  window.addEventListener("beforeunload", () => {
    for (const commit of [...activeDeletions]) void commit();
  });
}

export interface ScheduledTagDeletion {
  /** 窗口内撤销：标签及其与条目的关联原样回来 */
  undo: () => void;
  /** 立即提交（窗口结束时自动调用） */
  commit: () => Promise<void>;
}

function hidePendingDeletions(tags: Tag[]): Tag[] {
  return pendingDeletions.size === 0
    ? tags
    : tags.filter((tag) => !pendingDeletions.has(tag.id));
}

interface TagState {
  tags: Tag[];
  isLoading: boolean;
  fetchTags: () => Promise<void>;
  createTag: (input: CreateTagInput) => Promise<Tag>;
  updateTag: (id: string, input: UpdateTagInput) => Promise<Tag | null>;
  /**
   * 删除标签（条目保留，只摘掉标签）：先从界面隐藏，窗口内可撤销，之后才提交。
   * 标签不存在或已在撤销窗口内时返回 null。
   */
  scheduleDeleteTag: (id: string, delayMs?: number) => ScheduledTagDeletion | null;
}

export const useTagStore = create<TagState>()((set, get) => ({
  tags: [],
  isLoading: false,

  fetchTags: async () => {
    // 独立渲染/测试或降级运行时可能尚未注入知识域 IPC；采集弹窗仍可
    // 手工输入新标签，不应为“没有候选建议”刷一条控制台错误。
    if (!window.api?.tag?.list) {
      return;
    }
    set({ isLoading: true });
    try {
      const tags = await window.api.tag.list();
      set({ tags: hidePendingDeletions(tags) });
    } catch (error) {
      console.error("加载标签列表失败:", error);
    } finally {
      set({ isLoading: false });
    }
  },

  createTag: async (input) => {
    const created = await window.api.tag.create(input);
    await get().fetchTags();
    return created;
  },

  updateTag: async (id, input) => {
    const updated = await window.api.tag.update(id, input);
    await get().fetchTags();
    return updated;
  },

  scheduleDeleteTag: (id, delayMs = DELETION_UNDO_WINDOW_MS) => {
    const target = get().tags.find((tag) => tag.id === id);
    if (!target || pendingDeletions.has(id)) return null;
    pendingDeletions.set(id, target);
    set((state) => ({ tags: hidePendingDeletions(state.tags) }));

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
      // 提交期间保持隐藏，避免提交中的刷新让被删的标签闪回
      await runGuardedMutation(
        "library.deleteTag",
        translate("library.deleteTag", "删除标签"),
        async () => {
          const removed = await window.api.tag.delete(id);
          if (!removed) throw new Error("标签已不存在，请刷新列表");
        },
      );
      release();
      // 提交失败的标签会随这次刷新回到列表，并已有原因提示
      await get().fetchTags();
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
        void get().fetchTags();
      },
    };
  },
}));