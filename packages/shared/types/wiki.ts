/**
 * Wiki 模块类型（ADR 0023：LLM 从知识条目编译出的派生知识页网络）。
 * 派生数据纪律：由 AI 全权维护、可随时全量重建，绝不反向修改来源条目。
 */

export const WIKI_PAGE_KINDS = ["topic", "entity", "concept"] as const;

export type WikiPageKind = (typeof WIKI_PAGE_KINDS)[number];

export interface WikiPage {
  id: string;
  /** 显示标题 */
  title: string;
  /** 规范化标题（唯一）：[[链接]] 解析与页面去重的锚点；改名等于新页面 */
  normalizedTitle: string;
  kind: WikiPageKind;
  /** 一行摘要（列表与检索用） */
  summary: string;
  /** Markdown 正文，内含 [[目标页标题]] 形式的页间链接 */
  body: string;
  /** 别名列表（JSON 字符串数组） */
  aliasesJson: string | null;
  /** 生成出处 */
  provider: string;
  model: string;
  promptVersion: string;
  generatedAt: number;
  /**
   * 用户手动改过正文的时刻；非空时编译不再覆盖 body。
   *
   * 派生数据纪律的例外：AI 编出来的页面允许人工修正，但修正必须挡得住
   * 下一轮编译，否则改完等于白改。清空该标记即交回自动编译。
   */
  manualEditedAt: number | null;
  createdAt: number;
  updatedAt: number;
}

/** 目录条目（列表/检索/链接解析用的轻量投影） */
export interface WikiCatalogEntry {
  id: string;
  title: string;
  normalizedTitle: string;
  kind: WikiPageKind;
  summary: string;
  aliasesJson: string | null;
  updatedAt: number;
}

/** 页面全文检索命中（问答检索用；按 bm25 排序，标题权重最高） */
export interface WikiSearchHit {
  id: string;
  title: string;
  kind: WikiPageKind;
  summary: string;
}

/** 页面来源条目引用（页面 → 原文跳转） */
export interface WikiSourceRef {
  itemId: string;
  title: string;
}

/** 页面详情：本体 + 反向链接 + 来源条目 */
export interface WikiPageDetail {
  page: WikiPage;
  backlinks: WikiCatalogEntry[];
  sources: WikiSourceRef[];
}

/** 待编译条目投影 */
export interface WikiCompilableItem {
  id: string;
  title: string;
  content: string;
}

/** 编译指纹（素材哈希 + 提示词版本；模型仅作出处不触发重编） */
export interface WikiIngestion {
  itemId: string;
  contentHash: string;
  model: string;
  /** 空串表示上次尝试失败，尚未成功编译过 */
  promptVersion: string;
  /** 连续失败次数；超过上限后不再自动重试 */
  failureCount: number;
  /** 早于该时刻不再尝试（指数退避） */
  nextAttemptAt: number | null;
  updatedAt: number;
}

/** 页面被整体覆盖前的快照 */
export interface WikiPageRevision {
  id: string;
  pageId: string;
  title: string;
  kind: WikiPageKind;
  summary: string;
  body: string;
  aliasesJson: string | null;
  model: string;
  promptVersion: string;
  createdAt: number;
}

/** 编译状态快照（Wiki 页头展示） */
export interface WikiCompilationStatus {
  pageCount: number;
  compiledItemCount: number;
  eligibleItemCount: number;
}

/** 关系图谱节点（页面投影） */
export interface WikiGraphNode {
  id: string;
  title: string;
  kind: WikiPageKind;
}

/** 关系图谱边（source/target 命名对齐 force-graph 的约定） */
export interface WikiGraphLink {
  source: string;
  target: string;
}

export interface WikiGraph {
  nodes: WikiGraphNode[];
  links: WikiGraphLink[];
  /** 库中页面总数；大于 nodes.length 时说明图被截断了 */
  totalNodes: number;
}

/** 单条目编译结果落库输入（main 侧单事务写四表） */
export interface WikiApplyCompilationInput {
  itemId: string;
  contentHash: string;
  provider: string;
  model: string;
  promptVersion: string;
  /**
   * 本次 prompt 里附了完整正文的既有页面 id。
   *
   * 只有这些页允许整体覆盖 body——模型只看到目录里的标题和摘要时，
   * 它"更新"出来的正文是凭空编的，覆盖上去等于把原页内容丢掉。
   */
  contextPageIds?: string[];
  pages: {
    title: string;
    normalizedTitle: string;
    kind: WikiPageKind;
    summary: string;
    body: string;
    aliasesJson: string | null;
    /** 出链目标（规范化标题，已按白名单清洗） */
    linkTargets: string[];
  }[];
}
