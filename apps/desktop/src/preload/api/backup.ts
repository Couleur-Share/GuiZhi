import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  BackupPasswordChangeRequest,
  BackupRepositoryInitResult,
  BackupRepositoryStatus,
  BackupRestorePreview,
  BackupCreateResult,
  BackupFileInfo,
  BackupRestoreResult,
  ExportAiHandoffRequest,
  ExportAiHandoffResult,
  ExportMarkdownResult,
  PortableBackupExportResult,
  RepositorySnapshotRequest,
  RepositorySnapshotResult,
} from "@guizhi/shared/types";

export interface BackupDeleteResult {
  success: boolean;
  error?: string;
}

export const backupApi = {
  create: (): Promise<BackupCreateResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_CREATE),
  list: (): Promise<BackupFileInfo[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_LIST),
  delete: (fileName: string): Promise<BackupDeleteResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_DELETE, fileName),
  restore: (fileName: string): Promise<BackupRestoreResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_RESTORE, fileName),
  repositoryStatus: (): Promise<BackupRepositoryStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_STATUS),
  initializeRepository: (password: string): Promise<BackupRepositoryInitResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_INIT, password),
  changeRepositoryPassword: (request: BackupPasswordChangeRequest) =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_CHANGE_PASSWORD, request) as Promise<{
      success: boolean;
      error?: string;
    }>,
  createRepositorySnapshot: (
    request?: RepositorySnapshotRequest,
  ): Promise<RepositorySnapshotResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_CREATE_SNAPSHOT, request),
  listRepositorySnapshots: (): Promise<BackupFileInfo[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_LIST),
  previewRepositoryRestore: (input: {
    snapshotId: string;
    recoveryPassword?: string;
  }): Promise<BackupRestorePreview> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_PREVIEW, input),
  deleteRepositorySnapshot: (snapshotId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_DELETE, snapshotId) as Promise<{
      success: boolean;
      removedObjects?: number;
      error?: string;
    }>,
  exportPortable: (input: {
    snapshotId: string;
    recoveryPassword?: string;
  }): Promise<PortableBackupExportResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_EXPORT_PORTABLE, input),
  restoreRepositorySnapshot: (input: {
    snapshotId: string;
    recoveryPassword?: string;
  }): Promise<BackupRestoreResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_REPOSITORY_RESTORE, input),
  consumeRestoredRendererSettings: (): Promise<Record<string, unknown> | null> =>
    ipcRenderer.invoke(
      IPC_CHANNELS.BACKUP_REPOSITORY_CONSUME_RENDERER_SETTINGS,
    ),
  syncRendererSettings: (settings: Record<string, unknown>): Promise<boolean> =>
    ipcRenderer.invoke(
      IPC_CHANNELS.BACKUP_REPOSITORY_SYNC_RENDERER_SETTINGS,
      settings,
    ),
  exportMarkdown: (): Promise<ExportMarkdownResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.EXPORT_MARKDOWN),
  exportAiHandoff: (
    request: ExportAiHandoffRequest,
  ): Promise<ExportAiHandoffResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.EXPORT_AI_HANDOFF, request),
};
