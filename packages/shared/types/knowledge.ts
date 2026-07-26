/**
 * 知识域类型：条目 / 集合 / 标签。
 * 时间戳统一为 Unix 毫秒（UTC）。
 */

export const KNOWLEDGE_ITEM_TYPES = [
  "note",
  "webpage",
  "video",
  "image",
  "audio",
  "document",
  "snippet",
  "forum",
] as const;

export type KnowledgeItemType = (typeof KNOWLEDGE_ITEM_TYPES)[number];

/**
 * 条目状态只有「活跃」与「归档」两态。
 *
 * v0.6 之前还有一个 inbox 态，本意是「待整理」，但它不 gate 任何消费路径
 * （问答检索、Wiki 编译、语义索引一律无视 status），也没有任何自动化会推进它，
 * 于是永远停在 inbox——「收件箱」和「全部」显示的是同一批条目。
 * 待整理的真实信号是「还没归到任何知识库」，见 scope 的 uncategorized。
 */
export const KNOWLEDGE_ITEM_STATUSES = ["active", "archived"] as const;

export type KnowledgeItemStatus = (typeof KNOWLEDGE_ITEM_STATUSES)[number];

export const TAG_COLOR_KEYS = [
  "red",
  "orange",
  "amber",
  "green",
  "teal",
  "blue",
  "indigo",
  "purple",
  "pink",
  "gray",
] as const;

export type TagColorKey = (typeof TAG_COLOR_KEYS)[number];

export interface Tag {
  id: string;
  name: string;
  colorKey: TagColorKey;
  /** 关联的未删除条目数（列表查询时附带） */
  itemCount?: number;
  createdAt: number;
  updatedAt: number;
}

export interface Collection {
  id: string;
  name: string;
  icon?: string | null;
  sortOrder: number;
  /** 集合内未删除条目数（列表查询时附带） */
  itemCount?: number;
  createdAt: number;
  updatedAt: number;
}

export interface KnowledgeItem {
  id: string;
  title: string;
  content: string;
  summary?: string | null;
  transcript?: string | null;
  itemType: KnowledgeItemType;
  status: KnowledgeItemStatus;
  collectionId?: string | null;
  isFavorite: boolean;
  isPinned: boolean;
  /** 最近一次采集的来源 URI（source_records 联查带出；手工条目为 null） */
  sourceUri?: string | null;
  /** 软删除时间；null 表示未在回收站 */
  deletedAt?: number | null;
  createdAt: number;
  updatedAt: number;
  tags: Tag[];
}

/** 列表轻量投影（不含完整正文） */
export interface KnowledgeItemListEntry {
  id: string;
  title: string;
  /** 正文前段纯文本摘要 */
  snippet: string;
  itemType: KnowledgeItemType;
  status: KnowledgeItemStatus;
  collectionId?: string | null;
  isFavorite: boolean;
  isPinned: boolean;
  deletedAt?: number | null;
  createdAt: number;
  updatedAt: number;
  tags: Tag[];
}

/** 侧栏导航范围。uncategorized = 未归入任何知识库，即待整理队列 */
export const KNOWLEDGE_SCOPES = [
  "uncategorized",
  "all",
  "favorites",
  "archived",
  "trash",
] as const;

export type KnowledgeScope = (typeof KNOWLEDGE_SCOPES)[number];

/** 列表排序字段（置顶条目始终在最前） */
export const KNOWLEDGE_SORT_FIELDS = [
  "updatedAt",
  "createdAt",
  "title",
] as const;

export type KnowledgeSortField = (typeof KNOWLEDGE_SORT_FIELDS)[number];

export type KnowledgeSortOrder = "asc" | "desc";

export interface KnowledgeItemQuery {
  scope: KnowledgeScope;
  collectionId?: string;
  tagId?: string;
  /** 全文搜索关键词；非空时结果按相关性排序 */
  search?: string;
  /**
   * 搜索串的编译方式，默认 phrase（片段 AND、中文整段相邻）。
   * 自然语言问句要用 recall，否则中文长句会被编译成一个
   * 要求逐字连续出现的 phrase，必然零命中。
   */
  searchMode?: "phrase" | "recall";
  /** scope=all 时把归档条目一并纳入（AI 问答检索：归档 ≠ 移出知识） */
  includeArchived?: boolean;
  /** 默认 updatedAt；search 非空时被相关度排序覆盖 */
  sortBy?: KnowledgeSortField;
  /** 默认 desc；search 非空时被相关度排序覆盖 */
  sortOrder?: KnowledgeSortOrder;
  limit?: number;
  offset?: number;
}

/**
 * 批量修改多个条目。
 *
 * 标签用「追加 / 移除」而不是整体替换：批量打标签时各条目原有的标签不一样，
 * 替换语义会把它们一起抹掉。
 */
export interface BulkUpdateKnowledgeItemsInput {
  collectionId?: string | null;
  isFavorite?: boolean;
  isPinned?: boolean;
  status?: KnowledgeItemStatus;
  addTagNames?: string[];
  removeTagNames?: string[];
}

export interface KnowledgeItemListResult {
  entries: KnowledgeItemListEntry[];
  total: number;
}

export interface CreateKnowledgeItemInput {
  title?: string;
  content?: string;
  /** 口播转写稿（在线视频导入时随条目一并写入） */
  transcript?: string | null;
  itemType?: KnowledgeItemType;
  status?: KnowledgeItemStatus;
  collectionId?: string | null;
  tagNames?: string[];
}

export interface UpdateKnowledgeItemInput {
  title?: string;
  content?: string;
  summary?: string | null;
  transcript?: string | null;
  itemType?: KnowledgeItemType;
  status?: KnowledgeItemStatus;
  collectionId?: string | null;
  isFavorite?: boolean;
  isPinned?: boolean;
  /** 全量替换标签（按名称，缺失的自动创建） */
  tagNames?: string[];
}

/** 侧栏计数（未删除条目；除 trash 外都排除归档，与点进去看到的列表同口径） */
export interface KnowledgeCounts {
  uncategorized: number;
  all: number;
  favorites: number;
  archived: number;
  trash: number;
  byCollection: Record<string, number>;
  byTag: Record<string, number>;
}

export interface CreateCollectionInput {
  name: string;
  icon?: string | null;
}

export interface UpdateCollectionInput {
  name?: string;
  icon?: string | null;
  sortOrder?: number;
}

export interface CreateTagInput {
  name: string;
  colorKey?: TagColorKey;
}

export interface UpdateTagInput {
  name?: string;
  colorKey?: TagColorKey;
}
