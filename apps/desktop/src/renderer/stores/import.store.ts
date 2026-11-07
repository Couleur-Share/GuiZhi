import { create } from "zustand";
import type {
  EnqueueImportInput,
  ImportCaptureStrategy,
  ImportQueueState,
  ImportTask,
  ImportTaskListQuery,
  ImportTaskClearQuery,
  ImportTaskOrigin,
} from "@guizhi/shared/types";
import { translate } from "../i18n";
import { useKnowledgeStore } from "./knowledge.store";
import {
  reportOperationError,
  runGuardedMutation,
} from "./operation-error.store";
import { describeLoadError } from "./load-error";
import { useTagStore } from "./tag.store";

/**
 * 撤销窗口内已从界面隐藏、尚未提交给主进程的移除。
 * 刷新、翻页和主进程推送都必须绕开它，否则被删的行会在撤销窗口内闪回来。
 */
const pendingRemovals = new Map<string, ImportTask>();
/** 与撤销提示条的停留时长一致 */
const REMOVAL_UNDO_WINDOW_MS = 8000;
/** 尚未提交的移除；退出页面前统一提交，避免关窗口把「已删除」的行又带回来 */
const activeRemovals = new Set<() => Promise<void>>();

export interface ScheduledRemoval {
  count: number;
  /** 窗口内撤销：任务原样回到列表 */
  undo: () => void;
  /** 立即提交（窗口结束时自动调用） */
  commit: () => Promise<void>;
}

if (typeof window !== "undefined") {
  window.addEventListener("beforeunload", () => {
    for (const commit of [...activeRemovals]) void commit();
  });
}

/** 从各档计数里扣掉一批任务（仅用于撤销窗口内的乐观更新） */
function subtractCounts(counts: ImportCounts, removed: ImportTask[]): ImportCounts {
  const hidden = countByFilter(removed);
  const next = { ...counts };
  for (const key of Object.keys(next) as ImportFilter[]) {
    next[key] = Math.max(0, next[key] - hidden[key]);
  }
  return next;
}

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
        get().tasks.find((task) => task.id === id)?.displayName ??
        pendingRemovals.get(id)?.displayName ??
        id;
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
  total: number;
  counts: ImportCounts;
  nextCursor: string | null;
  isLoadingMore: boolean;
  /** 调度器状态和任务列表分开：暂停并不改变 pending 任务的持久化状态。 */
  queueState: ImportQueueState;
  origin: ImportTaskOrigin | "all";
  setOrigin: (origin: ImportTaskOrigin | "all") => void;
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
  loadMore: () => Promise<void>;
  toggleQueuePaused: () => Promise<void>;
  enqueue: (inputs: EnqueueImportInput[]) => Promise<ImportTask[]>;
  cancelTask: (id: string) => Promise<void>;
  retryTask: (
    id: string,
    options?: boolean | {
      verifyWeb?: boolean;
      forceDuplicate?: boolean;
      captureStrategy?: ImportCaptureStrategy;
      commentLimit?: 0 | 10 | 20 | 50;
    },
  ) => Promise<boolean>;
  /**
   * 从列表删除已结束的任务（失败任务不进「清理已完成」，需要单独的出口）。
   *
   * 先在界面隐藏，窗口结束才真正提交，窗口内可撤销。任务记录只是队列的流水，
   * 不含已入库的条目，值得撤销但不值得为它加软删除字段。
   */
  scheduleRemoveTasks: (ids: string[], delayMs?: number) => ScheduledRemoval | null;
  /** 批量：逐条走 IPC，结束后只刷新一次列表 */
  cancelTasks: (ids: string[]) => Promise<void>;
  retryTasks: (ids: string[]) => Promise<void>;
  clearFinished: () => Promise<void>;
  previewClearTerminal: (query: ImportTaskClearQuery) => Promise<number>;
  clearTerminal: (query: ImportTaskClearQuery) => Promise<number>;
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

function fallbackQueueState(tasks: ImportTask[]): ImportQueueState {
  return {
    paused: false,
    runningCount: tasks.filter((task) => task.status === "processing").length,
    pendingCount: tasks.filter((task) => task.status === "pending").length,
    concurrency: 2,
  };
}

function toServerStatus(filter: ImportFilter): ImportTaskListQuery["status"] {
  return filter;
}

function mergeActiveFirst(active: ImportTask[], entries: ImportTask[]): ImportTask[] {
  const seen = new Set<string>();
  return [...active, ...entries].filter((task) => {
    if (seen.has(task.id)) return false;
    seen.add(task.id);
    return true;
  });
}

function countsFromResult(
  counts: Record<ImportTask["status"], number>,
  loaded: ImportTask[],
  degradedCount?: number,
): ImportCounts {
  const raw: ImportCounts = {
    all: Object.values(counts).reduce((sum, value) => sum + value, 0),
    active: counts.pending + counts.processing,
    completed: counts.completed,
    degraded: degradedCount ?? loaded.filter(isDegradedTask).length,
    duplicate: counts.duplicate,
    failed: counts.failed,
    canceled: counts.canceled,
  };
  if (pendingRemovals.size === 0) return raw;
  // 服务端计数仍含撤销窗口内隐藏的任务；loaded 已经排除了它们，所以仅当
  // 降级数取自服务端时才需要扣除。
  const hidden = countByFilter([...pendingRemovals.values()]);
  const next = { ...raw };
  for (const key of Object.keys(next) as ImportFilter[]) {
    if (key === "degraded" && degradedCount === undefined) continue;
    next[key] = Math.max(0, next[key] - hidden[key]);
  }
  return next;
}

function hidePendingRemovals(tasks: ImportTask[]): ImportTask[] {
  return pendingRemovals.size === 0
    ? tasks
    : tasks.filter((task) => !pendingRemovals.has(task.id));
}

/** 撤销窗口内被隐藏、且属于当前筛选范围的任务数（用于修正列表总数） */
function hiddenInView(state: ImportState): number {
  return filterTasks(
    [...pendingRemovals.values()],
    state.filter,
    state.query,
    state.origin,
  ).length;
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
  origin: ImportTaskOrigin | "all" = "all",
): ImportTask[] {
  const keyword = query.trim().toLowerCase();
  return tasks.filter((task) => {
    if ((origin !== "all" && (task.origin ?? "desktop") !== origin) || !matchesFilter(task, filter)) {
      return false;
    }
    if (!keyword) {
      return true;
    }
    return (
      task.displayName.toLowerCase().includes(keyword) ||
      task.sourceInput.toLowerCase().includes(keyword) ||
      (task.error ?? "").toLowerCase().includes(keyword) ||
      (task.warning ?? "").toLowerCase().includes(keyword)
    );
  });
}

// 查询代次同时保护首页和翻页，避免快速切换来源时旧结果覆盖新筛选。
let listRevision = 0;
let fetchRevision = 0;
// IPC 推送可能先于列表请求回执到达；旧快照不能把终态覆盖回处理中。
let taskRevision = 0;

export const useImportStore = create<ImportState>()((set, get) => ({
  tasks: [],
  hasLoaded: false,
  loadError: null,
  activeCount: 0,
  total: 0,
  counts: countByFilter([]),
  nextCursor: null,
  isLoadingMore: false,
  queueState: { paused: false, runningCount: 0, pendingCount: 0, concurrency: 2 },
  origin: "all",
  filter: "all",
  query: "",
  selectionIds: [],

  setOrigin: (origin) => {
    listRevision++;
    set({ origin, selectionIds: [], nextCursor: null, isLoadingMore: false });
    void get().fetchTasks();
  },
  setFilter: (filter) => {
    listRevision++;
    set({ filter, selectionIds: [], nextCursor: null, isLoadingMore: false });
    void get().fetchTasks();
  },
  setQuery: (query) => {
    listRevision++;
    set({ query, selectionIds: [], nextCursor: null, isLoadingMore: false });
  },

  toggleSelection: (id) =>
    set((state) => ({
      selectionIds: state.selectionIds.includes(id)
        ? state.selectionIds.filter((candidate) => candidate !== id)
        : [...state.selectionIds, id],
    })),

  rangeSelectTo: (id) =>
    set((state) => {
      const visible = filterTasks(state.tasks, state.filter, state.query, state.origin);
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
      selectionIds: filterTasks(state.tasks, state.filter, state.query, state.origin).map(
        (task) => task.id,
      ),
    })),

  clearSelection: () => set({ selectionIds: [] }),

  fetchTasks: async () => {
    const revision = listRevision, request = ++fetchRevision, changes = taskRevision;
    const current = () => revision === listRevision && request === fetchRevision;
    set({ loadError: null });
    try {
      const response = await window.api.import.list({
        origin: get().origin,
        status: toServerStatus(get().filter),
        query: get().query,
        pageSize: 50,
        cursor: null,
      });
      // v0.20 升级窗口内兼容尚未更新的 preload / 测试桩：无对象结果时按第一页处理。
      const result = Array.isArray(response)
        ? {
            degradedCount: response.filter(isDegradedTask).length,
            entries: response,
            active: response.filter(isActive),
            nextCursor: null,
            total: response.length,
            counts: Object.fromEntries(
              (["pending", "processing", "completed", "failed", "canceled", "duplicate"] as const).map(
                (status) => [
                  status,
                  response.filter((task) => task.status === status).length,
                ],
              ),
            ) as Record<ImportTask["status"], number>,
          }
        : response;
      const tasks = hidePendingRemovals(mergeActiveFirst(result.active, result.entries));
      // 老的测试桩和升级中的 preload 可能还没有这个方法；列表仍要能读出来。
      const queueState = window.api.import.getQueueState
        ? await window.api.import.getQueueState()
        : fallbackQueueState(tasks);
      if (!current()) return;
      if (changes !== taskRevision) return await get().fetchTasks();
      const alive = new Set(tasks.map((task) => task.id));
      set((state) => ({
        tasks,
        isLoadingMore: false,
        activeCount: queueState.runningCount + queueState.pendingCount,
        total: Math.max(0, result.total - hiddenInView(state)),
        counts: countsFromResult(result.counts, tasks, result.degradedCount),
        nextCursor: result.nextCursor,
        queueState,
        // 清理/删除之后选中集合里可能留着已经不存在的 id
        selectionIds: state.selectionIds.filter((id) => alive.has(id)),
      }));
    } catch (error) {
      if (!current()) return;
      console.error("加载采集任务失败:", error);
      // 不记下来就会铺出「还没有导入任务」的新手引导页，掩盖了读取失败
      set({ loadError: describeLoadError(error) });
    } finally {
      // 失败也要放行：否则界面会一直停在加载态，用户连空态引导都看不到
      if (current()) set({ hasLoaded: true });
    }
  },

  loadMore: async () => {
    const cursor = get().nextCursor;
    if (!cursor || get().isLoadingMore) return;
    const revision = listRevision, request = fetchRevision, changes = taskRevision;
    const current = () => revision === listRevision && request === fetchRevision;
    set({ isLoadingMore: true });
    try {
      const result = await window.api.import.list({
        origin: get().origin,
        status: toServerStatus(get().filter),
        query: get().query,
        pageSize: 50,
        cursor,
      });
      if (!current()) return;
      if (changes !== taskRevision) return;
      set((state) => {
        const tasks = hidePendingRemovals(mergeActiveFirst(result.active, [
          ...state.tasks.filter((task) => !isActive(task)),
          ...result.entries,
        ]));
        return {
          tasks,
          counts: countsFromResult(result.counts, tasks, result.degradedCount),
          total: Math.max(0, result.total - hiddenInView(state)),
          nextCursor: result.nextCursor,
        };
      });
    } catch (error) {
      if (!current()) return;
      reportOperationError("imports.actionLoadMore", translate("imports.actionLoadMore", "加载更多任务"), error);
    } finally {
      if (current()) set({ isLoadingMore: false });
    }
  },

  toggleQueuePaused: async () => {
    await runGuardedMutation(
      "imports.actionToggleQueue",
      translate("imports.actionToggleQueue", "切换队列状态"),
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
    await runGuardedMutation("imports.actionCancel", translate("imports.actionCancel", "取消任务"), async () => {
      await window.api.import.cancel(id);
      await get().fetchTasks();
    });
  },

  retryTask: async (id, options) => {
    return runGuardedMutation("imports.actionRetry", translate("imports.actionRetry", "重试任务"), async () => {
      const retryOptions = typeof options === "boolean"
        ? { forceDuplicate: options }
        : options;
      const retried = await window.api.import.retry(
        id,
        retryOptions,
      );
      if (retried === null) throw new Error("任务已不存在，请刷新列表");
      await get().fetchTasks();
    });
  },

  scheduleRemoveTasks: (ids, delayMs = REMOVAL_UNDO_WINDOW_MS) => {
    const targets = new Set(ids);
    const snapshot = get().tasks.filter(
      (task) => targets.has(task.id) && !pendingRemovals.has(task.id) && !isActive(task),
    );
    if (snapshot.length === 0) return null;
    const removeIds = snapshot.map((task) => task.id);
    for (const task of snapshot) pendingRemovals.set(task.id, task);
    set((state) => ({
      tasks: hidePendingRemovals(state.tasks),
      total: Math.max(0, state.total - snapshot.length),
      counts: subtractCounts(state.counts, snapshot),
      selectionIds: state.selectionIds.filter((id) => !targets.has(id)),
    }));

    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const release = () => {
      for (const id of removeIds) pendingRemovals.delete(id);
      activeRemovals.delete(commit);
    };
    const commit = async (): Promise<void> => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      // 提交期间保持隐藏，避免提交中的刷新让被删的行闪回
      await runTaskBatch(get, removeIds, "imports.actionRemove", translate("imports.actionRemove", "移除任务"), (id) =>
        window.api.import.remove(id),
      );
      release();
      // 提交失败的任务会随这次刷新回到列表，并已有逐条原因的提示
      await get().fetchTasks();
      void import("./inbox.store").then(({ useInboxStore }) =>
        useInboxStore.getState().refresh(),
      );
    };
    timer = setTimeout(() => void commit(), delayMs);
    activeRemovals.add(commit);
    return {
      count: removeIds.length,
      commit,
      undo: () => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        release();
        void get().fetchTasks();
      },
    };
  },

  cancelTasks: async (ids) => {
    await runTaskBatch(get, ids, "imports.actionCancel", translate("imports.actionCancel", "取消任务"), (id) =>
      window.api.import.cancel(id),
    );
    set({ selectionIds: [] });
    await get().fetchTasks();
  },

  retryTasks: async (ids) => {
    await runTaskBatch(get, ids, "imports.actionRetry", translate("imports.actionRetry", "重试任务"), (id) =>
      window.api.import.retry(id),
    );
    set({ selectionIds: [] });
    await get().fetchTasks();
  },


  clearFinished: async () => {
    await runGuardedMutation(
      "imports.actionClearFinished",
      translate("imports.actionClearFinished", "清理已完成任务"),
      async () => {
        await window.api.import.clearFinished();
        await get().fetchTasks();
      },
    );
  },

  previewClearTerminal: async (query) => {
    const result = await window.api.import.previewClearTerminal(query);
    return result.count;
  },

  clearTerminal: async (query) => {
    let count = 0;
    const ok = await runGuardedMutation("imports.clearConfirmTitle", translate("imports.clearConfirmTitle", "清理采集任务历史"), async () => {
      count = (await window.api.import.clearTerminal(query)).count;
      set({ selectionIds: [] });
      await get().fetchTasks();
    });
    return ok ? count : 0;
  },

  subscribeChanges: () => {
    let refreshTimer: ReturnType<typeof setTimeout> | undefined;
    const handleChanged = (task: ImportTask) => {
      taskRevision++;
      // 撤销窗口内的任务已从界面隐藏：只更新被保留的快照，不能把它推回列表
      if (pendingRemovals.has(task.id)) {
        pendingRemovals.set(task.id, task);
        return;
      }
      set((state) => {
        const index = state.tasks.findIndex(
          (candidate) => candidate.id === task.id,
        );
        const tasks =
          index >= 0
            ? state.tasks.map((candidate) =>
                candidate.id === task.id ? task : candidate,
              )
            : filterTasks([task], state.filter, state.query, state.origin).length
              ? [task, ...state.tasks] : state.tasks;
        return {
          tasks,
        };
      });

      // 合并密集推送，只更新计数，不把用户已经翻开的历史重置回第一页。
      if (!refreshTimer) refreshTimer = setTimeout(async () => {
        refreshTimer = undefined;
        const revision = listRevision, request = fetchRevision;
        const state = get();
        try {
          const [result, queueState] = await Promise.all([
            window.api.import.list({ origin: state.origin, status: state.filter, query: state.query, pageSize: 20 }),
            window.api.import.getQueueState(),
          ]);
          if (revision !== listRevision || request !== fetchRevision || Array.isArray(result)) return;
          set({ counts: countsFromResult(result.counts, get().tasks, result.degradedCount),
            total: result.total, queueState, activeCount: queueState.runningCount + queueState.pendingCount });
        } catch (error) {
          if (revision === listRevision && request === fetchRevision) set({ loadError: describeLoadError(error) });
        }
      }, 250);

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
      if (
        task.status === "completed" ||
        task.status === "failed" ||
        task.status === "duplicate"
      ) {
        void import("./inbox.store").then(({ useInboxStore }) =>
          useInboxStore.getState().refresh(),
        );
      }
    };

    window.api.on?.("import:changed", handleChanged);
    return () => {
      clearTimeout(refreshTimer);
      window.api.off?.("import:changed", handleChanged);
    };
  },
}));
