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

export const KNOWLEDGE_ITEM_STATUSES = ["inbox", "ready", "archived"] as const;

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

/** 侧栏导航范围 */
export const KNOWLEDGE_SCOPES = [
  "inbox",
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
  /** scope=all 时把归档条目一并纳入（AI 问答检索：归档 ≠ 移出知识） */
  includeArchived?: boolean;
  /** 默认 updatedAt；search 非空时被相关度排序覆盖 */
  sortBy?: KnowledgeSortField;
  /** 默认 desc；search 非空时被相关度排序覆盖 */
  sortOrder?: KnowledgeSortOrder;
  limit?: number;
  offset?: number;
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

/** 侧栏计数（未删除条目） */
export interface KnowledgeCounts {
  inbox: number;
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
