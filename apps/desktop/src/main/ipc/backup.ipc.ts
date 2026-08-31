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
  pruneBackupsOfKind,
} from "../services/backup";
import {
  exportKnowledgeToMarkdown,
  sanitizeFileName,
} from "../services/export-markdown";
import type { BackgroundJobRuntime } from "../services/background-jobs";
import { previewRepositoryRestore } from "../services/backup-repository-preview";
import {
  getBackupRendererSettings,
  getBackupRepository,
  setBackupRendererSettings,
} from "../services/backup-repository-runtime";
import {
  applyPreparedRepositoryRestore,
  prepareRepositoryRestore,
} from "../services/backup-repository-restore";
import {
  getConfigDir,
  getDatabasePath,
  getImagesDir,
  getVideosDir,
} from "../runtime-paths";

const RESTORE_RELAUNCH_DELAY_MS = 800;

function formatExportTimestamp(date = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

/** 另存为对话框的默认落点；取不到下载目录时只给文件名，交给系统决定 */
function resolveDefaultExportPath(fileName: string): string {
  try {
    return path.join(app.getPath("downloads"), fileName);
  } catch {
    return fileName;
  }
}

function findBackupByFileName(fileName: unknown): BackupFileInfo | null {
  if (typeof fileName !== "string" || path.basename(fileName) !== fileName) {
    return null;
  }
  return (
    listBackups().find((backup) => backup.fileName === fileName) ?? null
  );
}

function readBooleanSetting(
  db: Database.Database,
  key: string,
  fallback: boolean,
): boolean {
  const row = db.get("SELECT value FROM settings WHERE key = ?", key) as
    | { value: string }
    | undefined;
  if (!row) return fallback;
  try {
    return JSON.parse(row.value) === true;
  } catch {
    return row.value === "true";
  }
}

function readNumberSetting(
  db: Database.Database,
  key: string,
  fallback: number,
): number {
  const row = db.get("SELECT value FROM settings WHERE key = ?", key) as
    | { value: string }
    | undefined;
  const value = row ? Number(JSON.parse(row.value)) : fallback;
  return Number.isFinite(value) ? value : fallback;
}

function syncRepositoryBackupJob(
  db: Database.Database,
  runtime: BackgroundJobRuntime,
): void {
  const repository = getBackupRepository();
  const status = repository.status();
  const intervalHours = Math.min(
    Math.max(Math.round(readNumberSetting(db, "backupIntervalHours", 24)), 1),
    168,
  );
  const enabled =
    status.initialized &&
    status.automaticAccessAvailable &&
    readBooleanSetting(db, "backgroundTasksEnabled", false) &&
    readBooleanSetting(db, "backupAutoEnabled", true);
  const current = runtime
    .list()
    .find((job) => job.kind === "backup" && job.scopeId === "repository");
  const preservedNextRunAt =
    current?.state === "scheduled" || current?.state === "retry_wait"
      ? current.nextRunAt
      : null;
  runtime.schedule("repository-auto-backup", {
    kind: "backup",
    scopeId: "repository",
    intervalMinutes: intervalHours * 60,
    nextRunAt:
      preservedNextRunAt ?? Date.now() + intervalHours * 60 * 60_000,
    enabled,
  });
}

export function registerBackupIPC(
  db: Database.Database,
  backgroundJobs: BackgroundJobRuntime,
): void {
  const repository = getBackupRepository();
  backgroundJobs.registerHandler("backup", async () => {
    const result = repository.createSnapshot({
      db,
      appVersion: app.getVersion(),
      kind: "auto",
      request: { rendererSettings: getBackupRendererSettings() },
    });
    if (!result.success) throw new Error(result.error || "完整备份失败");
    repository.pruneAutoSnapshots(
      readNumberSetting(db, "backupKeepCount", 10),
    );
  });
  syncRepositoryBackupJob(db, backgroundJobs);

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
    IPC_CHANNELS.BACKUP_REPOSITORY_STATUS,
    (): BackupRepositoryStatus => repository.status(),
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_INIT,
    (_event, password: string): BackupRepositoryInitResult => {
      try {
        const status = repository.initialize(password);
        syncRepositoryBackupJob(db, backgroundJobs);
        return { success: true, ...status };
      } catch (error) {
        return {
          success: false,
          ...repository.status(),
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_CHANGE_PASSWORD,
    (_event, request: BackupPasswordChangeRequest) => {
      try {
        repository.changePassword(request.currentPassword, request.nextPassword);
        return { success: true };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_SYNC_RENDERER_SETTINGS,
    (_event, settings: Record<string, unknown>) => {
      if (!settings || typeof settings !== "object" || Array.isArray(settings)) {
        return false;
      }
      setBackupRendererSettings(settings);
      syncRepositoryBackupJob(db, backgroundJobs);
      return true;
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_CREATE_SNAPSHOT,
    (_event, request?: RepositorySnapshotRequest): RepositorySnapshotResult =>
      repository.createSnapshot({
        db,
        appVersion: app.getVersion(),
        kind: "manual",
        request: {
          ...request,
          rendererSettings:
            request?.rendererSettings ?? getBackupRendererSettings(),
        },
      }),
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_LIST,
    (): BackupFileInfo[] => repository.listSnapshots(),
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_PREVIEW,
    (
      _event,
      input: { snapshotId?: unknown; recoveryPassword?: unknown },
    ): BackupRestorePreview =>
      previewRepositoryRestore(
        repository,
        typeof input?.snapshotId === "string" ? input.snapshotId : "",
        typeof input?.recoveryPassword === "string"
          ? input.recoveryPassword
          : undefined,
      ),
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_DELETE,
    (_event, snapshotId: string) => {
      try {
        return { success: true, removedObjects: repository.deleteSnapshot(snapshotId) };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_EXPORT_PORTABLE,
    async (
      event,
      input: { snapshotId?: unknown; recoveryPassword?: unknown },
    ): Promise<PortableBackupExportResult> => {
      const snapshotId =
        typeof input?.snapshotId === "string" ? input.snapshotId : "";
      const owner = BrowserWindow.fromWebContents(event.sender) ?? undefined;
      const saveOptions = {
        title: "导出归知便携备份",
        defaultPath: resolveDefaultExportPath(
          `GuiZhi-${formatExportTimestamp()}.guizhi-backup`,
        ),
        filters: [{ name: "GuiZhi Backup", extensions: ["guizhi-backup"] }],
      };
      const selected = owner
        ? await dialog.showSaveDialog(owner, saveOptions)
        : await dialog.showSaveDialog(saveOptions);
      if (selected.canceled || !selected.filePath) {
        return { success: false, canceled: true };
      }
      try {
        await repository.exportPortable(
          snapshotId,
          selected.filePath,
          typeof input?.recoveryPassword === "string"
            ? input.recoveryPassword
            : undefined,
        );
        return { success: true, filePath: selected.filePath };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_RESTORE,
    (
      _event,
      input: { snapshotId?: unknown; recoveryPassword?: unknown },
    ): BackupRestoreResult => {
      const snapshotId =
        typeof input?.snapshotId === "string" ? input.snapshotId : "";
      const recoveryPassword =
        typeof input?.recoveryPassword === "string"
          ? input.recoveryPassword
          : undefined;
      if (countActiveImportTasks(db) > 0) {
        return {
          success: false,
          error: "有导入任务正在进行，请等待完成或取消后再恢复",
        };
      }
      const runningJobs = db.get(
        "SELECT COUNT(*) AS count FROM background_jobs WHERE state = 'running'",
      ) as { count: number };
      if (runningJobs.count > 0) {
        return {
          success: false,
          error: "有后台写任务正在执行，请等待本轮完成后再恢复",
        };
      }
      const preview = previewRepositoryRestore(
        repository,
        snapshotId,
        recoveryPassword,
      );
      if (!preview.success) {
        return { success: false, error: preview.error || "恢复预检失败" };
      }
      const targets = {
        databasePath: getDatabasePath(),
        imagesDir: getImagesDir(),
        videosDir: getVideosDir(),
        configDir: getConfigDir(),
      };
      let prepared;
      try {
        prepared = prepareRepositoryRestore({
          repository,
          snapshotId,
          recoveryPassword,
          liveDb: db,
          targets,
          currentRendererSettings: getBackupRendererSettings(),
        });
        createBackup(db, "pre-restore");
        pruneBackupsOfKind("pre-restore", 3);
      } catch (error) {
        return {
          success: false,
          error: `恢复准备失败：${error instanceof Error ? error.message : String(error)}`,
        };
      }

      const scheduleRelaunch = (delayMs: number) => {
        setTimeout(() => {
          app.relaunch();
          app.quit();
        }, delayMs);
      };
      try {
        backgroundJobs.stop();
        closeDatabase();
        applyPreparedRepositoryRestore(prepared, targets);
      } catch (error) {
        scheduleRelaunch(RESTORE_RELAUNCH_DELAY_MS * 3);
        return {
          success: false,
          relaunching: true,
          error: `恢复交换失败并已回滚：${error instanceof Error ? error.message : String(error)}`,
        };
      }
      scheduleRelaunch(RESTORE_RELAUNCH_DELAY_MS);
      return { success: true, relaunching: true };
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.BACKUP_REPOSITORY_CONSUME_RENDERER_SETTINGS,
    (): Record<string, unknown> | null => {
      const pendingPath = path.join(
        getConfigDir(),
        "pending-renderer-settings.json",
      );
      if (!fs.existsSync(pendingPath)) return null;
      try {
        const parsed = JSON.parse(fs.readFileSync(pendingPath, "utf8")) as unknown;
        fs.rmSync(pendingPath, { force: true });
        return parsed && typeof parsed === "object" && !Array.isArray(parsed)
          ? (parsed as Record<string, unknown>)
          : null;
      } catch (error) {
        console.warn("[backup] 读取恢复后的 Renderer 设置失败:", error);
        return null;
      }
    },
  );

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

  ipcMain.handle(
    IPC_CHANNELS.EXPORT_AI_HANDOFF,
    async (
      event,
      request: ExportAiHandoffRequest,
    ): Promise<ExportAiHandoffResult> => {
      if (typeof request?.text !== "string" || !request.text.trim()) {
        return { success: false, error: "交接稿内容为空" };
      }

      const owner = BrowserWindow.fromWebContents(event.sender) ?? undefined;
      const fileName = `${sanitizeFileName(request.title ?? "") || "guizhi-note"}.md`;
      const options = {
        title: "另存为 AI 交接稿",
        // 取不到下载目录时只给文件名，让系统自己决定落在哪儿
        defaultPath: resolveDefaultExportPath(fileName),
        filters: [{ name: "Markdown", extensions: ["md"] }],
      };
      const result = owner
        ? await dialog.showSaveDialog(owner, options)
        : await dialog.showSaveDialog(options);
      if (result.canceled || !result.filePath) {
        return { success: false, canceled: true };
      }

      try {
        fs.writeFileSync(result.filePath, request.text, "utf8");
      } catch (error) {
        return {
          success: false,
          error: `写入文件失败：${error instanceof Error ? error.message : String(error)}`,
        };
      }
      return { success: true, filePath: result.filePath };
    },
  );
}
