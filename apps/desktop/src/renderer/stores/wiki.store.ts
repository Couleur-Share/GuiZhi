import { create } from "zustand";
import type {
  WikiCatalogEntry,
  WikiCompilationStatus,
  WikiGraph,
  WikiPageDetail,
  WikiPageRevision,
} from "@guizhi/shared/types";
import {
  buildLinkResolver,
  cleanWikiLinks,
  compilePendingItems,
  normalizeWikiTitle,
  parseAliases,
} from "../services/knowledge-ai/wiki-compile";
import { AiNotConfiguredError } from "../services/knowledge-ai/ai-invoke";

export interface WikiCompileProgress {
  currentTitle: string;
  current: number;
  total: number;
}

export type WikiViewMode = "catalog" | "graph";

interface WikiState {
  catalog: WikiCatalogEntry[];
  status: WikiCompilationStatus | null;
  selectedPageId: string | null;
  pageDetail: WikiPageDetail | null;
  /** 当前页的历史版本（每次被编译覆盖前的快照，最多 10 份） */
  pageRevisions: WikiPageRevision[];
  isLoading: boolean;
  isCompiling: boolean;
  compileProgress: WikiCompileProgress | null;
  /** 编译结果提示（完成/停止/失败后一次性展示） */
  compileNotice: {
    kind: "done" | "cancelled" | "error" | "not-configured";
    message: string;
  } | null;
  /** 目录视图 / 关系图谱 */
  viewMode: WikiViewMode;
  graph: WikiGraph | null;

  refresh: () => Promise<void>;
  selectPage: (id: string | null) => Promise<void>;
  /** 把当前页回滚到最近一次被覆盖前的内容 */
  restorePreviousRevision: () => Promise<boolean>;
  /** 手动改写当前页正文；落标记后下一轮编译不再覆盖 */
  savePageBody: (body: string, releaseToAuto?: boolean) => Promise<boolean>;
  /** 删除单个页面（清理残留知识不必再清空全库重编） */
  deletePage: (pageId: string) => Promise<boolean>;
  /** 按 [[链接]] 目标跳转：标题精确匹配优先，其次别名 */
  openByLinkTarget: (target: string) => Promise<boolean>;
  compileNow: () => Promise<void>;
  /** 中断在途编译：已落库的条目保留，本轮剩余条目下轮续跑 */
  cancelCompile: () => void;
  rebuildAll: () => Promise<void>;
  dismissNotice: () => void;
  setViewMode: (mode: WikiViewMode) => void;
  loadGraph: () => Promise<void>;
}

let compileAbort: AbortController | null = null;

export const useWikiStore = create<WikiState>()((set, get) => ({
  catalog: [],
  status: null,
  selectedPageId: null,
  pageDetail: null,
  pageRevisions: [],
  isLoading: false,
  isCompiling: false,
  compileProgress: null,
  compileNotice: null,
  viewMode: "catalog",
  graph: null,

  setViewMode: (mode) => {
    set({ viewMode: mode });
    if (mode === "graph") {
      void get().loadGraph();
    }
  },

  loadGraph: async () => {
    try {
      const graph = await window.api.wiki.graph();
      set({ graph });
    } catch (error) {
      console.error("加载 Wiki 图谱失败:", error);
    }
  },

  refresh: async () => {
    set({ isLoading: true });
    try {
      const [catalog, status] = await Promise.all([
        window.api.wiki.catalog(),
        window.api.wiki.status(),
      ]);
      set({ catalog, status });
      // 图谱视图打开时同步刷新节点与链接
      if (get().viewMode === "graph") {
        void get().loadGraph();
      }
      // 选中页可能已被重建清掉，同步详情
      const selectedId = get().selectedPageId;
      if (selectedId) {
        const detail = await window.api.wiki.getPage(selectedId);
        set(
          detail
            ? { pageDetail: detail }
            : { selectedPageId: null, pageDetail: null },
        );
      }
    } catch (error) {
      console.error("加载 Wiki 目录失败:", error);
    } finally {
      set({ isLoading: false });
    }
  },

  selectPage: async (id) => {
    if (!id) {
      set({ selectedPageId: null, pageDetail: null, pageRevisions: [] });
      return;
    }
    set({ selectedPageId: id });
    const [detail, revisions] = await Promise.all([
      window.api.wiki.getPage(id),
      window.api.wiki.listRevisions(id),
    ]);
    // 防串页：等待期间用户可能又点了别的页
    if (get().selectedPageId === id) {
      set({ pageDetail: detail, pageRevisions: revisions });
    }
  },

  restorePreviousRevision: async () => {
    const pageId = get().selectedPageId;
    const latest = get().pageRevisions[0];
    if (!pageId || !latest) {
      return false;
    }
    const restored = await window.api.wiki.restoreRevision(latest.id);
    if (restored) {
      // 回滚本身也会存一份快照，重新拉详情与版本列表
      await get().selectPage(pageId);
      await get().refresh();
    }
    return restored;
  },

  savePageBody: async (body, releaseToAuto) => {
    const pageId = get().selectedPageId;
    if (!pageId) {
      return false;
    }
    // 出链按新正文重建，否则图谱与反向链接停留在改动之前
    const resolver = buildLinkResolver(get().catalog, []);
    const { targets } = cleanWikiLinks(body, resolver);
    const saved = await window.api.wiki.updatePage({
      pageId,
      body,
      linkTargets: targets,
      releaseToAuto,
    });
    if (saved) {
      await get().selectPage(pageId);
      await get().refresh();
    }
    return saved;
  },

  deletePage: async (pageId) => {
    const removed = await window.api.wiki.deletePage(pageId);
    if (removed) {
      if (get().selectedPageId === pageId) {
        set({ selectedPageId: null, pageDetail: null, pageRevisions: [] });
      }
      await get().refresh();
    }
    return removed;
  },

  openByLinkTarget: async (target) => {
    const normalized = normalizeWikiTitle(target);
    const catalog = get().catalog;
    const byTitle = catalog.find(
      (entry) => entry.normalizedTitle === normalized,
    );
    if (byTitle) {
      await get().selectPage(byTitle.id);
      return true;
    }
    for (const entry of catalog) {
      for (const alias of parseAliases(entry.aliasesJson)) {
        if (normalizeWikiTitle(alias) === normalized) {
          await get().selectPage(entry.id);
          return true;
        }
      }
    }
    return false;
  },

  compileNow: async () => {
    if (get().isCompiling) {
      return;
    }
    set({ isCompiling: true, compileProgress: null, compileNotice: null });
    const controller = new AbortController();
    compileAbort = controller;
    try {
      const result = await compilePendingItems((currentTitle, current, total) => {
        set({ compileProgress: { currentTitle, current, total } });
      }, controller.signal);
      set({
        compileNotice: controller.signal.aborted
          ? { kind: "cancelled", message: `${result.compiled}/${result.pending}` }
          : result.pending === 0
            ? { kind: "done", message: "" }
            : {
                kind: "done",
                message: `${result.compiled}/${result.pending}`,
              },
      });
    } catch (error) {
      if (controller.signal.aborted) {
        // 中断落在在途请求上时抛的是 AbortError，不是失败
        set({ compileNotice: { kind: "cancelled", message: "" } });
      } else if (error instanceof AiNotConfiguredError) {
        set({ compileNotice: { kind: "not-configured", message: "" } });
      } else {
        set({
          compileNotice: {
            kind: "error",
            message: error instanceof Error ? error.message : String(error),
          },
        });
      }
    } finally {
      compileAbort = null;
      set({ isCompiling: false, compileProgress: null });
      await get().refresh();
    }
  },

  cancelCompile: () => {
    compileAbort?.abort();
  },

  rebuildAll: async () => {
    if (get().isCompiling) {
      return;
    }
    await window.api.wiki.clear();
    set({ selectedPageId: null, pageDetail: null });
    await get().compileNow();
  },

  dismissNotice: () => set({ compileNotice: null }),
}));

/** 后台自动编译（App 定时器调用）：静默执行，不打扰当前视图。 */
export async function runBackgroundWikiCompile(): Promise<void> {
  const store = useWikiStore.getState();
  if (store.isCompiling) {
    return;
  }
  useWikiStore.setState({ isCompiling: true });
  try {
    await compilePendingItems();
  } catch {
    // 后台任务纪律：失败静默，等下轮（AI 未配置 / 网络抖动都不打扰用户）
  } finally {
    useWikiStore.setState({ isCompiling: false });
    // 仅当用户正停留在 Wiki 模块时刷新视图
    const { useUIStore } = await import("./ui.store");
    if (useUIStore.getState().appModule === "wiki") {
      await useWikiStore.getState().refresh();
    }
  }
}
