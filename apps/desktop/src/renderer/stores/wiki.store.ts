import { create } from "zustand";
import type {
  WikiBacklinkCounts,
  WikiCatalogEntry,
  WikiCompilationStatus,
  WikiGraph,
  WikiPageDetail,
  WikiPageRevision,
} from "@guizhi/shared/types";
import type { WikiCompilePreview } from "@guizhi/shared/types/wiki-compiler";
import {
  buildLinkResolver,
  cleanWikiLinks,
  normalizeWikiTitle,
  parseAliases,
} from "../services/knowledge-ai/wiki-compile";
import { describeLoadError } from "./load-error";

/**
 * 页面写操作的结果。
 *
 * 原来只返回 boolean：DB 抛异常时整条链路没人 catch，界面上连「保存失败」
 * 都不弹，用户手写的正文就那么没了着落。现在异常收进 error 一并交给调用方。
 * error 为空的 false 表示目标不存在这类无异常的失败。
 */
export interface WikiMutationResult {
  ok: boolean;
  error?: string;
}

function describeMutationError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export type WikiCompileCounts = WikiCompilePreview["counts"];

/**
 * 工具栏编译按钮与全文编译对话框共用预览口径：数字只算「可执行」，
 * 只要还有待升级、待复核或等待中的资料就允许打开对话框；预览未知时不拦。
 */
export function wikiCompileButtonState(counts: WikiCompileCounts | null): {
  pending: number;
  actionable: boolean;
} {
  if (!counts) return { pending: 0, actionable: true };
  return {
    pending: counts.ready,
    actionable: counts.ready + counts.upgrade + counts.review + counts.waiting > 0,
  };
}

export type WikiViewMode = "catalog" | "graph";

/**
 * 目录筛选轴。
 *
 * orphan（孤立页）是这里唯一一个不看字段而看关系的档：没有任何页面链向它，
 * 说明它游离在知识网络之外——对一个靠互链组织的 Wiki 来说，这是最该被
 * 看见的一类页面，之前完全没有入口。
 */
export type WikiCatalogFilter =
  | "all"
  | "topic"
  | "entity"
  | "concept"
  | "manual"
  | "orphan";

export type WikiCatalogSort = "recent" | "linked" | "title";

export type WikiCatalogCounts = Record<WikiCatalogFilter, number>;

/** 侧栏筛选轴的各档计数 */
export function countCatalogByFilter(
  catalog: WikiCatalogEntry[],
  backlinkCounts: WikiBacklinkCounts,
): WikiCatalogCounts {
  const counts: WikiCatalogCounts = {
    all: catalog.length,
    topic: 0,
    entity: 0,
    concept: 0,
    manual: 0,
    orphan: 0,
  };
  for (const entry of catalog) {
    counts[entry.kind] += 1;
    if (entry.manualEditedAt) {
      counts.manual += 1;
    }
    if (!backlinkCounts[entry.id]) {
      counts.orphan += 1;
    }
  }
  return counts;
}

function matchesCatalogFilter(
  entry: WikiCatalogEntry,
  filter: WikiCatalogFilter,
  backlinkCounts: WikiBacklinkCounts,
): boolean {
  switch (filter) {
    case "all":
      return true;
    case "manual":
      return entry.manualEditedAt !== null;
    case "orphan":
      return !backlinkCounts[entry.id];
    default:
      return entry.kind === filter;
  }
}

/**
 * 目录列的可见范围。
 *
 * 搜索命中（FTS，覆盖正文）与筛选/排序是叠加关系：命中集合先限定范围，
 * 再套用当前筛选轴。搜索按 bm25 排序，这时不再套用排序轴——把相关度
 * 最高的结果按标题重排等于把搜索结果打散。
 */
export function selectVisibleCatalog(params: {
  catalog: WikiCatalogEntry[];
  backlinkCounts: WikiBacklinkCounts;
  filter: WikiCatalogFilter;
  sort: WikiCatalogSort;
  /** FTS 命中的页面 id（按相关度排序）；null 表示当前没有搜索 */
  searchHitIds: string[] | null;
}): WikiCatalogEntry[] {
  const { catalog, backlinkCounts, filter, sort, searchHitIds } = params;
  const matching = catalog.filter((entry) =>
    matchesCatalogFilter(entry, filter, backlinkCounts),
  );

  if (searchHitIds) {
    const rank = new Map(searchHitIds.map((id, index) => [id, index]));
    return matching
      .filter((entry) => rank.has(entry.id))
      .sort((left, right) => rank.get(left.id)! - rank.get(right.id)!);
  }

  const sorted = [...matching];
  if (sort === "title") {
    sorted.sort((left, right) => left.title.localeCompare(right.title));
  } else if (sort === "linked") {
    sorted.sort(
      (left, right) =>
        (backlinkCounts[right.id] ?? 0) - (backlinkCounts[left.id] ?? 0) ||
        right.updatedAt - left.updatedAt,
    );
  } else {
    sorted.sort((left, right) => right.updatedAt - left.updatedAt);
  }
  return sorted;
}

/** FTS 一次最多取回的页面数（目录列展示用，问答检索另有自己的上限） */
const CATALOG_SEARCH_LIMIT = 100;

interface WikiState {
  catalog: WikiCatalogEntry[];
  backlinkCounts: WikiBacklinkCounts;
  status: WikiCompilationStatus | null;
  catalogFilter: WikiCatalogFilter;
  catalogSort: WikiCatalogSort;
  searchQuery: string;
  /** FTS 命中的页面 id（按相关度）；null 表示当前没有搜索 */
  searchHitIds: string[] | null;
  isSearching: boolean;
  selectedPageId: string | null;
  pageDetail: WikiPageDetail | null;
  /** 当前页的历史版本（每次被编译覆盖前的快照，最多 10 份） */
  pageRevisions: WikiPageRevision[];
  /**
   * 首次加载是否已结束（无论成败）。
   *
   * 目录为空既可能是「真没有页面」也可能是「还没读出来」，界面必须先能
   * 区分这两者：否则每次进 Wiki 都先铺一屏「Wiki 还是空的」，再被两栏
   * 布局整块顶掉，顶栏的状态行与重建按钮也跟着横向弹一下。
   */
  hasLoaded: boolean;
  /** 目录读取失败的原因；为空表示读取正常（Wiki 真的是空的） */
  loadError: string | null;
  /** 搜索失败的原因；不区分的话失败会显示成「无结果」，最容易误导 */
  searchError: string | null;
  /** 后台自动编译轮次进行中（手动编译的进度在编译对话框里） */
  isCompiling: boolean;
  /** 全文编译预览的分类计数；读取失败为 null */
  compileCounts: WikiCompileCounts | null;
  /** 目录视图 / 关系图谱 */
  viewMode: WikiViewMode;
  graph: WikiGraph | null;

  setCatalogFilter: (filter: WikiCatalogFilter) => void;
  setCatalogSort: (sort: WikiCatalogSort) => void;
  /** 目录列搜索：走 wiki_fts（覆盖正文），空串清空命中集合 */
  runSearch: (query: string) => Promise<void>;
  refresh: () => Promise<void>;
  selectPage: (id: string | null) => Promise<void>;
  /** 把当前页回滚到最近一次被覆盖前的内容 */
  restorePreviousRevision: () => Promise<WikiMutationResult>;
  /** 手动改写当前页正文；落标记后下一轮编译不再覆盖 */
  savePageBody: (
    body: string,
    releaseToAuto?: boolean,
  ) => Promise<WikiMutationResult>;
  /** 删除单个页面（清理残留知识不必再清空全库重编） */
  deletePage: (pageId: string) => Promise<WikiMutationResult>;
  /** 按 [[链接]] 目标跳转：标题精确匹配优先，其次别名 */
  openByLinkTarget: (target: string) => Promise<boolean>;
  /** 打开全文编译对话框：预览范围、启动、暂停与继续都在对话框里完成 */
  openCompileDialog: () => void;
  setViewMode: (mode: WikiViewMode) => void;
  loadGraph: () => Promise<void>;
}

export const useWikiStore = create<WikiState>()((set, get) => ({
  catalog: [],
  backlinkCounts: {},
  status: null,
  catalogFilter: "all",
  catalogSort: "recent",
  searchQuery: "",
  searchHitIds: null,
  isSearching: false,
  selectedPageId: null,
  pageDetail: null,
  pageRevisions: [],
  hasLoaded: false,
  loadError: null,
  searchError: null,
  isCompiling: false,
  compileCounts: null,
  viewMode: "catalog",
  graph: null,

  setCatalogFilter: (filter) => set({ catalogFilter: filter }),
  setCatalogSort: (sort) => set({ catalogSort: sort }),

  runSearch: async (query) => {
    const trimmed = query.trim();
    set({ searchQuery: query });
    if (!trimmed) {
      set({ searchHitIds: null, isSearching: false, searchError: null });
      return;
    }
    set({ isSearching: true, searchError: null });
    try {
      const hits = await window.api.wiki.search(trimmed, CATALOG_SEARCH_LIMIT);
      // 输入很快时前一次请求可能后到，只认最后一次输入对应的结果
      if (get().searchQuery.trim() === trimmed) {
        set({ searchHitIds: hits.map((hit) => hit.id) });
      }
    } catch (error) {
      console.error("Wiki 搜索失败:", error);
      // 只清空命中集合的话，搜索失败会被画成「无结果」——这是最误导的一种
      if (get().searchQuery.trim() === trimmed) {
        set({ searchHitIds: [], searchError: describeLoadError(error) });
      }
    } finally {
      if (get().searchQuery.trim() === trimmed) {
        set({ isSearching: false });
      }
    }
  },

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
    set({ loadError: null });
    // 预览要逐条算指纹，大库较慢；与目录并行读取，不挡住目录渲染
    void import("../services/knowledge-ai/wiki-v2")
      .then(({ wikiPreview }) => wikiPreview())
      .then((preview) => set({ compileCounts: preview.counts }))
      .catch(() => set({ compileCounts: null }));
    try {
      const [catalog, status, backlinkCounts] = await Promise.all([
        window.api.wiki.catalog(),
        window.api.wiki.status(),
        window.api.wiki.backlinkCounts(),
      ]);
      set({ catalog, status, backlinkCounts });
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
      // 不记下来就会渲染成「Wiki 还是空的」，用户以为页面被清空了
      set({ loadError: describeLoadError(error) });
    } finally {
      // 失败也要放行：否则界面会一直停在加载态，用户连空态引导都看不到
      set({ hasLoaded: true });
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
      return { ok: false };
    }
    try {
      const restored = await window.api.wiki.restoreRevision(latest.id);
      if (restored) {
        // 回滚本身也会存一份快照，重新拉详情与版本列表
        await get().selectPage(pageId);
        await get().refresh();
      }
      return { ok: restored };
    } catch (error) {
      return { ok: false, error: describeMutationError(error) };
    }
  },

  savePageBody: async (body, releaseToAuto) => {
    const pageId = get().selectedPageId;
    if (!pageId) {
      return { ok: false };
    }
    // 出链按新正文重建，否则图谱与反向链接停留在改动之前
    const resolver = buildLinkResolver(get().catalog, []);
    const { targets } = cleanWikiLinks(body, resolver);
    try {
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
      return { ok: saved };
    } catch (error) {
      return { ok: false, error: describeMutationError(error) };
    }
  },

  deletePage: async (pageId) => {
    try {
      const removed = await window.api.wiki.deletePage(pageId);
      if (removed) {
        if (get().selectedPageId === pageId) {
          set({ selectedPageId: null, pageDetail: null, pageRevisions: [] });
        }
        await get().refresh();
      }
      return { ok: removed };
    } catch (error) {
      return { ok: false, error: describeMutationError(error) };
    }
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

  openCompileDialog: () => {
    window.dispatchEvent(new Event("wiki-compile-preview"));
  },
}));

/** 后台自动编译：静默执行；统一调度器可要求把失败抛回以进入持久化退避。 */
export async function runBackgroundWikiCompile(options?: {
  rethrow?: boolean;
}): Promise<void> {
  const store = useWikiStore.getState();
  if (store.isCompiling) {
    return;
  }
  useWikiStore.setState({ isCompiling: true });
  try {
    const { compileReadyWiki } = await import("../services/knowledge-ai/wiki-v2");
    const result = await compileReadyWiki();
    // 后台轮次不弹提示，但逐条失败要留痕，否则「为什么这条一直没进 Wiki」无从查起
    for (const failure of result.failures) {
      window.api?.log?.appError({
        scope: "wiki-compile",
        action: "后台 Wiki 编译",
        message: `${failure.title}：${failure.reason}`,
      });
    }
  } catch (error) {
    // 后台任务纪律：失败静默，等下轮（AI 未配置 / 网络抖动都不打扰用户），
    // 但同样记进日志
    window.api?.log?.appError({
      scope: "wiki-compile",
      action: "后台 Wiki 编译",
      message: error instanceof Error ? error.message : String(error),
    });
    if (options?.rethrow) {
      throw error;
    }
  } finally {
    useWikiStore.setState({ isCompiling: false });
    // 仅当用户正停留在 Wiki 模块时刷新视图
    const { useUIStore } = await import("./ui.store");
    if (useUIStore.getState().appModule === "wiki") {
      await useWikiStore.getState().refresh();
    }
  }
}
