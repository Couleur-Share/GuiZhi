/**
 * 采集 / 导入管线类型。
 */

export const IMPORT_SOURCE_KINDS = ["text", "file", "url"] as const;

export type ImportSourceKind = (typeof IMPORT_SOURCE_KINDS)[number];

export const IMPORT_TASK_STATUSES = [
  "pending",
  "processing",
  "completed",
  "failed",
  "canceled",
  "duplicate",
] as const;

export type ImportTaskStatus = (typeof IMPORT_TASK_STATUSES)[number];

export const IMPORT_STAGES = ["fetching", "extracting", "saving"] as const;

export type ImportStage = (typeof IMPORT_STAGES)[number];

export interface ImportTask {
  id: string;
  sourceKind: ImportSourceKind;
  /** 原始输入：文本内容 / 文件绝对路径 / URL */
  sourceInput: string;
  /** 列表显示名（文本首行 / 文件名 / URL） */
  displayName: string;
  status: ImportTaskStatus;
  stage?: ImportStage | null;
  error?: string | null;
  /** 成功入库的条目 id */
  resultItemId?: string | null;
  /** 去重命中的已有条目 id */
  duplicateItemId?: string | null;
  collectionId?: string | null;
  createdAt: number;
  updatedAt: number;
}

export interface EnqueueImportInput {
  kind: ImportSourceKind;
  /** 文本内容 / 文件绝对路径 / URL */
  input: string;
  collectionId?: string | null;
  /** 跳过去重强制创建副本（「仍要创建副本」） */
  forceDuplicate?: boolean;
}
