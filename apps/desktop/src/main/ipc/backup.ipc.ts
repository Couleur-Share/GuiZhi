/**
 * 备份 / 恢复 / 导出 IPC。
 *
 * 恢复是破坏性操作：仅接受备份目录内的规范命名文件，
 * 有导入任务在跑时拒绝执行，成功后自动重启应用完成换库。
 */
import { app, dialog, ipcMain, BrowserWindow } from "electron";
import fs from "fs";
import path from "path";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type {
  BackupCreateResult,
  BackupFileInfo,
  BackupRestoreResult,
  ExportMarkdownResult,
} from "@guizhi/shared/types";
import Database from "../database/sqlite";
import { closeDatabase } from "../database";
import type { BackupDeleteResult } from "../services/backup";
import {
  countActiveImportTasks,
  createBackup,
  deleteBackup,
  getDefaultRestorePaths,
  listBackups,
  performRestoreSwap,
  validateBackupFile,
} from "../services/backup";
import { exportKnowledgeToMarkdown } from "../services/export-markdown";

const RESTORE_RELAUNCH_DELAY_MS = 800;

function formatExportTimestamp(date = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

function findBackupByFileName(fileName: unknown): BackupFileInfo | null {
  if (typeof fileName !== "string" || path.basename(fileName) !== fileName) {
    return null;
  }
  return (
    listBackups().find((backup) => backup.fileName === fileName) ?? null
  );
}

export function registerBackupIPC(db: Database.Database): void {
  ipcMain.handle(IPC_CHANNELS.BACKUP_CREATE, (): BackupCreateResult => {
    try {
      const backup = createBackup(db, "manual");
      console.log(`[backup] 手动备份完成: ${backup.fileName}`);
      return { success: true, backup };
    } catch (error) {
      return {
        success: false,
        error: error instanceof Error ? error.message : String(error),
      };
    }
  });

  ipcMain.handle(IPC_CHANNELS.BACKUP_LIST, (): BackupFileInfo[] => {
    return listBackups();
  });

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_DELETE,
    (_event, fileName: string): BackupDeleteResult => {
      return deleteBackup(fileName);
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_RESTORE,
    (_event, fileName: string): BackupRestoreResult => {
      const backup = findBackupByFileName(fileName);
      if (!backup) {
        return { success: false, error: "找不到指定的备份文件" };
      }

      if (countActiveImportTasks(db) > 0) {
        return {
          success: false,
          error: "有导入任务正在进行，请等待完成或取消后再恢复",
        };
      }

      const validation = validateBackupFile(backup.path);
      if (!validation.ok) {
        return { success: false, error: validation.error };
      }

      const { databasePath, backupsDir } = getDefaultRestorePaths();
      const scheduleRelaunch = (delayMs: number) => {
        setTimeout(() => {
          app.relaunch();
          app.quit();
        }, delayMs);
      };

      try {
        closeDatabase();
        performRestoreSwap({
          databasePath,
          backupFilePath: backup.path,
          backupsDir,
        });
      } catch (error) {
        // 数据库已关闭，无论交换是否完成都必须重启恢复运行状态。
        // performRestoreSwap 失败时会用 pre-restore 快照回滚主库，
        // 这里把快照位置一并告诉用户——万一自动回滚也失败了，
        // 「去哪找那份快照」不该只写在源码注释里。
        console.error("[backup] 恢复过程中出错，应用将重启:", error);
        scheduleRelaunch(RESTORE_RELAUNCH_DELAY_MS * 3);
        const reason = error instanceof Error ? error.message : String(error);
        return {
          success: false,
          relaunching: true,
          error: `恢复失败：${reason}。已尝试用换库前的 pre-restore 快照回滚，快照保存在 ${backupsDir}`,
        };
      }

      console.log(`[backup] 已从备份恢复: ${backup.fileName}，即将重启应用`);
      scheduleRelaunch(RESTORE_RELAUNCH_DELAY_MS);
      return { success: true, relaunching: true };
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.EXPORT_MARKDOWN,
    async (event): Promise<ExportMarkdownResult> => {
      const owner = BrowserWindow.fromWebContents(event.sender) ?? undefined;
      const result = owner
        ? await dialog.showOpenDialog(owner, {
            title: "选择导出目录",
            properties: ["openDirectory", "createDirectory"],
          })
        : await dialog.showOpenDialog({
            title: "选择导出目录",
            properties: ["openDirectory", "createDirectory"],
          });
      if (result.canceled || result.filePaths.length === 0) {
        return { success: false, canceled: true };
      }

      const exportDir = path.join(
        result.filePaths[0],
        `GuiZhi-Export-${formatExportTimestamp()}`,
      );
      try {
        fs.mkdirSync(exportDir, { recursive: true });
        const stats = exportKnowledgeToMarkdown(db, exportDir);
        console.log(
          `[export] Markdown 导出完成: ${stats.count} 条 + ${stats.assetCount} 个资产 → ${exportDir}`,
        );
        return { success: true, count: stats.count, dir: exportDir };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );
}
