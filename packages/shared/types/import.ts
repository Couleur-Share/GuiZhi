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

/**
 * 导入子阶段。
 *
 * 视频链路会串起元数据解析、音频下载、转码、转写、排版、总结六步，
 * 最长可达几十分钟；此前它们全部落在 fetching 一个状态里，界面从头到尾
 * 只显示「抓取中」，用户无从判断是在正常工作还是卡死了。
 */
export const IMPORT_STAGES = [
  "fetching",
  "extracting",
  "saving",
  // ── 在线视频子阶段 ──
  "video-metadata",
  "video-audio",
  "transcoding",
  "transcribing",
  "formatting",
  "summarizing",
] as const;

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
