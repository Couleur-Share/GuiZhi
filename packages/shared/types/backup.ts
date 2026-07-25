/**
 * 备份 / 导出类型。
 *
 * 备份产物是单个 SQLite 文件（VACUUM INTO 的输出），存放在
 * %userData%/backups 下，文件名 knowledge-{kind}-{timestamp}.db。
 */

/** 备份来源类别：手动 / 定时自动 / 升级前快照 / 恢复前快照 */
export const BACKUP_KINDS = [
  "manual",
  "auto",
  "pre-update",
  "pre-restore",
] as const;

export type BackupKind = (typeof BACKUP_KINDS)[number];

export interface BackupFileInfo {
  fileName: string;
  /** 绝对路径（仅展示用；恢复/删除按 fileName 走白名单目录） */
  path: string;
  kind: BackupKind;
  sizeBytes: number;
  createdAt: number;
}

export interface BackupCreateResult {
  success: boolean;
  backup?: BackupFileInfo;
  error?: string;
}

export interface BackupRestoreResult {
  success: boolean;
  /** 成功后应用将自动重启 */
  relaunching?: boolean;
  error?: string;
}

export interface ExportMarkdownResult {
  success: boolean;
  canceled?: boolean;
  /** 实际写入的条目数 */
  count?: number;
  /** 导出目录绝对路径 */
  dir?: string;
  error?: string;
}
