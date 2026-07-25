/**
 * 同步传输类型（PromptHub 遗留的 WebDAV / S3 通道公共定义）。
 *
 * 归知的本地备份 / 恢复 / 导出见 types/backup.ts；
 * 远端同步快照格式在同步功能立项时再定义。
 */

export type SyncDirection = "push" | "pull";
export type SyncMode = "merge" | "replace" | "bidirectional";

export interface SyncCapabilities {
  incremental: boolean;
  bidirectional: boolean;
  media: boolean;
  encryption: boolean;
  manifest: boolean;
}

export type SyncErrorCode =
  | "SYNC_AUTH_FAILED"
  | "SYNC_CONNECTIVITY_FAILED"
  | "SYNC_REMOTE_NOT_FOUND"
  | "SYNC_PAYLOAD_INVALID"
  | "SYNC_PROVIDER_UNSUPPORTED";

export interface SyncWarning {
  code: string;
  message: string;
}

export interface SyncEvent {
  stage: string;
  message: string;
}

export interface SyncMediaFiles {
  images?: Record<string, string>;
  videos?: Record<string, string>;
}
