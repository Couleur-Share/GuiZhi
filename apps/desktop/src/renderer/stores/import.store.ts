import { create } from "zustand";
import type { EnqueueImportInput, ImportTask } from "@guizhi/shared/types";
import { useKnowledgeStore } from "./knowledge.store";

/** 侧栏筛选组的取值；active 合并 pending 与 processing */
export type ImportFilter =
  | "all"
  | "active"
  | "completed"
  | "duplicate"
  | "failed"
  | "canceled";

export type ImportCounts = Record<ImportFilter, number>;

interface ImportState {
  tasks: ImportTask[];
  /**
   * 首次加载是否已结束（无论成败）。
   *
   * 队列为空既可能是「真没有任务」也可能是「还没读出来」，界面必须先能
   * 区分这两者：否则空队列时会先铺一整屏引导页，再被列表换掉。
   */
  hasLoaded: boolean;
  /** 进行中任务数（rail 角标） */
  activeCount: number;
  filter: ImportFilter;
  setFilter: (filter: ImportFilter) => void;
  query: string;
  setQuery: (query: string) => void;
  selectionIds: string[];
  toggleSelection: (id: string) => void;
  /** Shift 点选：从上一个选中项到 id 之间的可见任务全选 */
  rangeSelectTo: (id: string) => void;
  selectVisible: () => void;
  clearSelection: () => void;
  fetchTasks: () => Promise<void>;
  enqueue: (inputs: EnqueueImportInput[]) => Promise<ImportTask[]>;
  cancelTask: (id: string) => Promise<void>;
  retryTask: (id: string, forceDuplicate?: boolean) => Promise<void>;
  /** 删除单条已结束的任务（失败任务不进「清理已完成」，需要单独的出口） */
  removeTask: (id: string) => Promise<void>;
  /** 批量：逐条走 IPC，结束后只刷新一次列表 */
  cancelTasks: (ids: string[]) => Promise<void>;
  retryTasks: (ids: string[]) => Promise<void>;
  removeTasks: (ids: string[]) => Promise<void>;
  clearFinished: () => Promise<void>;
  /** 订阅主进程任务变更（App 挂载时调用一次） */
  subscribeChanges: () => () => void;
}

function isActive(task: ImportTask): boolean {
  return task.status === "pending" || task.status === "processing";
}

function countActive(tasks: ImportTask[]): number {
  return tasks.filter(isActive).length;
}

/** 侧栏筛选组的各档计数 */
export function countByFilter(tasks: ImportTask[]): ImportCounts {
  const counts: ImportCounts = {
    all: tasks.length,
    active: 0,
    completed: 0,
    duplicate: 0,
    failed: 0,
    canceled: 0,
  };
  for (const task of tasks) {
    if (isActive(task)) {
      counts.active += 1;
    } else if (task.status === "completed") {
      counts.completed += 1;
    } else if (task.status === "duplicate") {
      counts.duplicate += 1;
    } else if (task.status === "failed") {
      counts.failed += 1;
    } else if (task.status === "canceled") {
      counts.canceled += 1;
    }
  }
  return counts;
}

function matchesFilter(task: ImportTask, filter: ImportFilter): boolean {
  switch (filter) {
    case "all":
      return true;
    case "active":
      return isActive(task);
    default:
      return task.status === filter;
  }
}

/**
 * 列表可见范围。搜索同时匹配显示名与原始输入——采集成功后显示名会换成
 * 真实标题，只匹配显示名的话，用手里的链接反而搜不到那条任务。
 */
export function filterTasks(
  tasks: ImportTask[],
  filter: ImportFilter,
  query: string,
): ImportTask[] {
  const keyword = query.trim().toLowerCase();
  return tasks.filter((task) => {
    if (!matchesFilter(task, filter)) {
      return false;
    }
    if (!keyword) {
      return true;
    }
    return (
      task.displayName.toLowerCase().includes(keyword) ||
      task.sourceInput.toLowerCase().includes(keyword)
    );
  });
}

export const useImportStore = create<ImportState>()((set, get) => ({
  tasks: [],
  hasLoaded: false,
  activeCount: 0,
  filter: "all",
  query: "",
  selectionIds: [],

  setFilter: (filter) => set({ filter, selectionIds: [] }),
  setQuery: (query) => set({ query }),

  toggleSelection: (id) =>
    set((state) => ({
      selectionIds: state.selectionIds.includes(id)
        ? state.selectionIds.filter((candidate) => candidate !== id)
        : [...state.selectionIds, id],
    })),

  rangeSelectTo: (id) =>
    set((state) => {
      const visible = filterTasks(state.tasks, state.filter, state.query);
      const anchorId = state.selectionIds[state.selectionIds.length - 1];
      const target = visible.findIndex((task) => task.id === id);
      const anchor = visible.findIndex((task) => task.id === anchorId);
      if (target < 0 || anchor < 0) {
        return { selectionIds: [id] };
      }
      const [from, to] = anchor <= target ? [anchor, target] : [target, anchor];
      const range = visible.slice(from, to + 1).map((task) => task.id);
      const merged = new Set([...state.selectionIds, ...range]);
      return { selectionIds: [...merged] };
    }),

  selectVisible: () =>
    set((state) => ({
      selectionIds: filterTasks(state.tasks, state.filter, state.query).map(
        (task) => task.id,
      ),
    })),

  clearSelection: () => set({ selectionIds: [] }),

  fetchTasks: async () => {
    try {
      const tasks = await window.api.import.list();
      const alive = new Set(tasks.map((task) => task.id));
      set((state) => ({
        tasks,
        activeCount: countActive(tasks),
        // 清理/删除之后选中集合里可能留着已经不存在的 id
        selectionIds: state.selectionIds.filter((id) => alive.has(id)),
      }));
    } catch (error) {
      console.error("加载导入任务失败:", error);
    } finally {
      // 失败也要放行：否则界面会一直停在加载态，用户连空态引导都看不到
      set({ hasLoaded: true });
    }
  },

  enqueue: async (inputs) => {
    const created = await window.api.import.enqueue(inputs);
    await get().fetchTasks();
    return created;
  },

  cancelTask: async (id) => {
    await window.api.import.cancel(id);
    await get().fetchTasks();
  },

  retryTask: async (id, forceDuplicate) => {
    await window.api.import.retry(
      id,
      forceDuplicate !== undefined ? { forceDuplicate } : undefined,
    );
    await get().fetchTasks();
  },

  removeTask: async (id) => {
    await window.api.import.remove(id);
    await get().fetchTasks();
  },

  cancelTasks: async (ids) => {
    for (const id of ids) {
      await window.api.import.cancel(id);
    }
    set({ selectionIds: [] });
    await get().fetchTasks();
  },

  retryTasks: async (ids) => {
    for (const id of ids) {
      await window.api.import.retry(id);
    }
    set({ selectionIds: [] });
    await get().fetchTasks();
  },

  removeTasks: async (ids) => {
    for (const id of ids) {
      await window.api.import.remove(id);
    }
    set({ selectionIds: [] });
    await get().fetchTasks();
  },

  clearFinished: async () => {
    await window.api.import.clearFinished();
    await get().fetchTasks();
  },

  subscribeChanges: () => {
    const handleChanged = (task: ImportTask) => {
      set((state) => {
        const index = state.tasks.findIndex(
          (candidate) => candidate.id === task.id,
        );
        const tasks =
          index >= 0
            ? state.tasks.map((candidate) =>
                candidate.id === task.id ? task : candidate,
              )
            : [task, ...state.tasks];
        return { tasks, activeCount: countActive(tasks) };
      });

      // 新条目入库后刷新知识库视图与计数，并调度一轮语义索引
      if (task.status === "completed") {
        void useKnowledgeStore.getState().refreshAll();
        void import("./semantic.store").then(({ useSemanticStore }) =>
          useSemanticStore.getState().scheduleIndexing(),
        );
      }
    };

    window.api.on?.("import:changed", handleChanged);
    return () => {
      window.api.off?.("import:changed", handleChanged);
    };
  },
}));
