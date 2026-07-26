import type { ReactNode } from "react";
import {
  CircleHelpIcon,
  FileTextIcon,
  GlobeIcon,
  ImageIcon,
  FileIcon,
  MessagesSquareIcon,
  MusicIcon,
  ScissorsIcon,
  VideoIcon,
} from "lucide-react";
import type {
  KnowledgeItemStatus,
  KnowledgeItemType,
  TagColorKey,
} from "@guizhi/shared/types";

export interface ItemTypeMeta {
  labelKey: string;
  fallback: string;
  icon: ReactNode;
}

export interface ItemStatusMeta {
  labelKey: string;
  fallback: string;
}

export const ITEM_TYPE_META: Record<KnowledgeItemType, ItemTypeMeta> = {
  note: {
    labelKey: "library.typeNote",
    fallback: "笔记",
    icon: <FileTextIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
  webpage: {
    labelKey: "library.typeWebpage",
    fallback: "网页",
    icon: <GlobeIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
  video: {
    labelKey: "library.typeVideo",
    fallback: "视频",
    icon: <VideoIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
  image: {
    labelKey: "library.typeImage",
    fallback: "图片",
    icon: <ImageIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
  audio: {
    labelKey: "library.typeAudio",
    fallback: "音频",
    icon: <MusicIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
  document: {
    labelKey: "library.typeDocument",
    fallback: "文档",
    icon: <FileIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
  snippet: {
    labelKey: "library.typeSnippet",
    fallback: "片段",
    icon: <ScissorsIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
  forum: {
    labelKey: "library.typeForum",
    fallback: "论坛",
    icon: <MessagesSquareIcon className="h-3.5 w-3.5" aria-hidden="true" />,
  },
};

export const ITEM_STATUS_META: Record<KnowledgeItemStatus, ItemStatusMeta> = {
  active: { labelKey: "library.statusActive", fallback: "活跃" },
  archived: { labelKey: "library.statusArchived", fallback: "归档" },
};

const UNKNOWN_ITEM_TYPE_META: ItemTypeMeta = {
  labelKey: "library.typeUnknown",
  fallback: "未知类型",
  icon: <CircleHelpIcon className="h-3.5 w-3.5" aria-hidden="true" />,
};

const UNKNOWN_ITEM_STATUS_META: ItemStatusMeta = {
  labelKey: "library.statusUnknown",
  fallback: "未知状态",
};

/**
 * 条目类型 / 状态查表。
 *
 * 形参写成 string 而不是联合类型，是因为这两个值直接来自数据库：
 * 新版本写入的条目类型（例如 v0.7.0 新增的 forum）在旧版本里查不到，
 * 类型系统认为不可能，运行时却真会发生。早先没有兜底时，
 * 一条这样的条目就足以让整个知识库列表抛异常、整个界面变空白。
 */
export function getItemTypeMeta(itemType: string): ItemTypeMeta {
  return Object.hasOwn(ITEM_TYPE_META, itemType)
    ? (ITEM_TYPE_META as Record<string, ItemTypeMeta>)[itemType]
    : UNKNOWN_ITEM_TYPE_META;
}

export function getItemStatusMeta(status: string): ItemStatusMeta {
  return Object.hasOwn(ITEM_STATUS_META, status)
    ? (ITEM_STATUS_META as Record<string, ItemStatusMeta>)[status]
    : UNKNOWN_ITEM_STATUS_META;
}

/** 标签色板：浅色背景 + 深色文字，深色模式自动适配 */
export const TAG_COLOR_CLASSES: Record<TagColorKey, string> = {
  red: "bg-red-500/15 text-red-700 dark:text-red-300",
  orange: "bg-orange-500/15 text-orange-700 dark:text-orange-300",
  amber: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
  green: "bg-green-500/15 text-green-700 dark:text-green-300",
  teal: "bg-teal-500/15 text-teal-700 dark:text-teal-300",
  blue: "bg-blue-500/15 text-blue-700 dark:text-blue-300",
  indigo: "bg-indigo-500/15 text-indigo-700 dark:text-indigo-300",
  purple: "bg-purple-500/15 text-purple-700 dark:text-purple-300",
  pink: "bg-pink-500/15 text-pink-700 dark:text-pink-300",
  gray: "bg-muted text-muted-foreground",
};

export const TAG_DOT_CLASSES: Record<TagColorKey, string> = {
  red: "bg-red-500",
  orange: "bg-orange-500",
  amber: "bg-amber-500",
  green: "bg-green-500",
  teal: "bg-teal-500",
  blue: "bg-blue-500",
  indigo: "bg-indigo-500",
  purple: "bg-purple-500",
  pink: "bg-pink-500",
  gray: "bg-gray-400",
};

export function formatItemTime(timestamp: number): string {
  const date = new Date(timestamp);
  const now = new Date();
  const sameDay = date.toDateString() === now.toDateString();
  if (sameDay) {
    return date.toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    });
  }
  const sameYear = date.getFullYear() === now.getFullYear();
  return date.toLocaleDateString(undefined, {
    year: sameYear ? undefined : "numeric",
    month: "short",
    day: "numeric",
  });
}
