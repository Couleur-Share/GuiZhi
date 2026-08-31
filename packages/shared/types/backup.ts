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

export type BackupFormat =
  | "repository-snapshot"
  | "portable"
  | "legacy-db";

export interface BackupContentSummary {
  itemCount: number;
  assetCount: number;
  configDomains: string[];
  schemaVersion: number;
  appVersion: string;
}

export interface BackupFileInfo {
  fileName: string;
  /** 绝对路径（仅展示用；恢复/删除按 fileName 走白名单目录） */
  path: string;
  kind: BackupKind;
  sizeBytes: number;
  createdAt: number;
  /** v0.20 起的新格式；未迁移的 .db 一律是 legacy-db */
  format: BackupFormat;
  encrypted: boolean;
  validation: "unchecked" | "valid" | "invalid";
  summary?: BackupContentSummary;
}

export interface BackupRepositoryStatus {
  initialized: boolean;
  automaticAccessAvailable: boolean;
  keyStorageBackend: string | null;
  warning?: string;
}

export interface BackupRepositoryInitResult extends BackupRepositoryStatus {
  success: boolean;
  error?: string;
}

export interface RepositorySnapshotRequest {
  /** Renderer 持有的界面偏好；恢复时会排除机器绑定字段。 */
  rendererSettings?: Record<string, unknown>;
  recoveryPassword?: string;
}

export interface RepositorySnapshotResult {
  success: boolean;
  snapshot?: BackupFileInfo;
  reusedObjects?: number;
  createdObjects?: number;
  error?: string;
}

export interface BackupRestorePreview {
  success: boolean;
  snapshot?: BackupFileInfo;
  missingFiles: string[];
  damagedFiles: string[];
  warnings: string[];
  error?: string;
}

export interface PortableBackupExportResult {
  success: boolean;
  canceled?: boolean;
  filePath?: string;
  error?: string;
}

export interface BackupPasswordChangeRequest {
  currentPassword: string;
  nextPassword: string;
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

/** 单条目「AI 交接稿」另存为：正文已由渲染进程序列化好，主进程只负责落盘 */
export interface ExportAiHandoffRequest {
  /** 条目标题，主进程清洗后作为默认文件名 */
  title: string;
  text: string;
}

export interface ExportAiHandoffResult {
  success: boolean;
  canceled?: boolean;
  filePath?: string;
  error?: string;
}
