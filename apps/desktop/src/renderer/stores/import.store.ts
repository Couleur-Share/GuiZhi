import { create } from "zustand";
import type {
  EnqueueImportInput,
  ImportCaptureStrategy,
  ImportQueueState,
  ImportTask,
} from "@guizhi/shared/types";
import { useKnowledgeStore } from "./knowledge.store";
import {
  reportOperationError,
  runGuardedMutation,
} from "./operation-error.store";
import { describeLoadError } from "./load-error";
import { useTagStore } from "./tag.store";

/**
 * 批量操作逐条独立执行。
 *
 * 此前是一个裸 for 循环：选中 20 条点「重试失败」，第 3 条抛异常，
 * 后 17 条一声不响地不执行，用户还以为都重试了。改成失败只记账、不中断，
 * 结束后一次性把逐条原因报出来。
 */
async function runTaskBatch(
  get: () => ImportState,
  ids: string[],
  actionKey: string,
  actionFallback: string,
  run: (id: string) => Promise<unknown>,
): Promise<void> {
  const failures: string[] = [];
  for (const id of ids) {
    try {
      await run(id);
    } catch (error) {
      const name =
        get().tasks.find((task) => task.id === id)?.displayName ?? id;
      failures.push(
        `${name}：${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }
  if (failures.length > 0) {
    reportOperationError(
      actionKey,
      actionFallback,
      new Error(failures.join("\n")),
    );
  }
}

/**
 * 侧栏筛选组的取值。
 *
 * active 合并 pending 与 processing；degraded 是 completed 的**子集**而不是
 * 另一档状态——那些任务确实入了库，只是内容有缺失，所以两档会重复计数。
 */
export type ImportFilter =
  | "all"
  | "active"
  | "completed"
  | "degraded"
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
  /** 队列读取失败的原因；为空表示读取正常（队列真的是空的） */
  loadError: string | null;
  /** 进行中任务数（rail 角标） */
  activeCount: number;
  /** 调度器状态和任务列表分开：暂停并不改变 pending 任务的持久化状态。 */
  queueState: ImportQueueState;
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
  toggleQueuePaused: () => Promise<void>;
  enqueue: (inputs: EnqueueImportInput[]) => Promise<ImportTask[]>;
  cancelTask: (id: string) => Promise<void>;
  retryTask: (
    id: string,
    options?: boolean | {
      forceDuplicate?: boolean;
      captureStrategy?: ImportCaptureStrategy;
      commentLimit?: 0 | 10 | 20 | 50;
    },
  ) => Promise<void>;
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

/** 入库了但内容有缺失（转写失败等）；列表上不能只给一枚绿色的「已完成」 */
export function isDegradedTask(task: ImportTask): boolean {
  return task.status === "completed" && Boolean(task.warning);
}

function countActive(tasks: ImportTask[]): number {
  return tasks.filter(isActive).length;
}

function fallbackQueueState(tasks: ImportTask[]): ImportQueueState {
  return {
    paused: false,
    runningCount: tasks.filter((task) => task.status === "processing").length,
    pendingCount: tasks.filter((task) => task.status === "pending").length,
    concurrency: 2,
  };
}

/** 侧栏筛选组的各档计数 */
export function countByFilter(tasks: ImportTask[]): ImportCounts {
  const counts: ImportCounts = {
    all: tasks.length,
    active: 0,
    completed: 0,
    degraded: 0,
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
    // 有意与 completed 重复计数：降级的任务同样是完成了的
    if (isDegradedTask(task)) {
      counts.degraded += 1;
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
    case "degraded":
      return isDegradedTask(task);
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
  loadError: null,
  activeCount: 0,
  queueState: { paused: false, runningCount: 0, pendingCount: 0, concurrency: 2 },
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
    set({ loadError: null });
    try {
      const tasks = await window.api.import.list();
      // 老的测试桩和升级中的 preload 可能还没有这个方法；列表仍要能读出来。
      const queueState = window.api.import.getQueueState
        ? await window.api.import.getQueueState()
        : fallbackQueueState(tasks);
      const alive = new Set(tasks.map((task) => task.id));
      set((state) => ({
        tasks,
        activeCount: countActive(tasks),
        queueState,
        // 清理/删除之后选中集合里可能留着已经不存在的 id
        selectionIds: state.selectionIds.filter((id) => alive.has(id)),
      }));
    } catch (error) {
      console.error("加载导入任务失败:", error);
      // 不记下来就会铺出「还没有导入任务」的新手引导页，掩盖了读取失败
      set({ loadError: describeLoadError(error) });
    } finally {
      // 失败也要放行：否则界面会一直停在加载态，用户连空态引导都看不到
      set({ hasLoaded: true });
    }
  },

  toggleQueuePaused: async () => {
    await runGuardedMutation(
      "imports.actionToggleQueue",
      "切换队列状态",
      async () => {
        const queueState = get().queueState.paused
          ? await window.api.import.resume()
          : await window.api.import.pause();
        set({ queueState });
      },
    );
  },

  enqueue: async (inputs) => {
    const created = await window.api.import.enqueue(inputs);
    await get().fetchTasks();
    return created;
  },

  cancelTask: async (id) => {
    await runGuardedMutation("imports.actionCancel", "取消任务", async () => {
      await window.api.import.cancel(id);
      await get().fetchTasks();
    });
  },

  retryTask: async (id, options) => {
    await runGuardedMutation("imports.actionRetry", "重试任务", async () => {
      const retryOptions = typeof options === "boolean"
        ? { forceDuplicate: options }
        : options;
      await window.api.import.retry(
        id,
        retryOptions,
      );
      await get().fetchTasks();
    });
  },

  removeTask: async (id) => {
    await runGuardedMutation("imports.actionRemove", "移除任务", async () => {
      await window.api.import.remove(id);
      await get().fetchTasks();
    });
  },

  cancelTasks: async (ids) => {
    await runTaskBatch(get, ids, "imports.actionCancel", "取消任务", (id) =>
      window.api.import.cancel(id),
    );
    set({ selectionIds: [] });
    await get().fetchTasks();
  },

  retryTasks: async (ids) => {
    await runTaskBatch(get, ids, "imports.actionRetry", "重试任务", (id) =>
      window.api.import.retry(id),
    );
    set({ selectionIds: [] });
    await get().fetchTasks();
  },

  removeTasks: async (ids) => {
    await runTaskBatch(get, ids, "imports.actionRemove", "移除任务", (id) =>
      window.api.import.remove(id),
    );
    set({ selectionIds: [] });
    await get().fetchTasks();
  },

  clearFinished: async () => {
    await runGuardedMutation(
      "imports.actionClearFinished",
      "清理已完成任务",
      async () => {
        await window.api.import.clearFinished();
        await get().fetchTasks();
      },
    );
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
        return {
          tasks,
          activeCount: countActive(tasks),
          queueState: {
            ...state.queueState,
            runningCount: tasks.filter((task) => task.status === "processing").length,
            pendingCount: tasks.filter((task) => task.status === "pending").length,
          },
        };
      });

      // 新条目入库后刷新知识库视图与计数，并调度一轮语义索引
      if (task.status === "completed") {
        void useKnowledgeStore.getState().refreshAll();
        // 采集弹窗里现敲的标签同样是入库时顺手建出来的，
        // 不重取一次列表，侧栏的「标签」分区就少那一行
        if (task.tagNames?.length) {
          void useTagStore.getState().fetchTags();
        }
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
