import { create } from "zustand";
import type { EnqueueImportInput, ImportTask } from "@guizhi/shared/types";
import { useKnowledgeStore } from "./knowledge.store";

interface ImportState {
  tasks: ImportTask[];
  isLoading: boolean;
  /** 进行中任务数（rail 角标） */
  activeCount: number;
  fetchTasks: () => Promise<void>;
  enqueue: (inputs: EnqueueImportInput[]) => Promise<ImportTask[]>;
  cancelTask: (id: string) => Promise<void>;
  retryTask: (id: string, forceDuplicate?: boolean) => Promise<void>;
  clearFinished: () => Promise<void>;
  /** 订阅主进程任务变更（App 挂载时调用一次） */
  subscribeChanges: () => () => void;
}

function countActive(tasks: ImportTask[]): number {
  return tasks.filter(
    (task) => task.status === "pending" || task.status === "processing",
  ).length;
}

export const useImportStore = create<ImportState>()((set, get) => ({
  tasks: [],
  isLoading: false,
  activeCount: 0,

  fetchTasks: async () => {
    set({ isLoading: true });
    try {
      const tasks = await window.api.import.list();
      set({ tasks, activeCount: countActive(tasks) });
    } catch (error) {
      console.error("加载导入任务失败:", error);
    } finally {
      set({ isLoading: false });
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
