import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  BackupCreateResult,
  BackupFileInfo,
  BackupRestoreResult,
  ExportMarkdownResult,
} from "@guizhi/shared/types";

export const backupApi = {
  create: (): Promise<BackupCreateResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_CREATE),
  list: (): Promise<BackupFileInfo[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_LIST),
  delete: (fileName: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_DELETE, fileName),
  restore: (fileName: string): Promise<BackupRestoreResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKUP_RESTORE, fileName),
  exportMarkdown: (): Promise<ExportMarkdownResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.EXPORT_MARKDOWN),
};
