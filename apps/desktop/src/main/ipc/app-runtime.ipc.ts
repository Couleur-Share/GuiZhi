import fs from "fs";
import path from "path";
import { app, ipcMain, session } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { getDataDir, getDatabasePath } from "../runtime-paths";

const AUTO_SYNC_PROVIDERS = new Set(["webdav", "s3", "self-hosted"]);
const AUTO_SYNC_REASONS = new Set(["startup", "startup-resume", "interval"]);
const AUTO_SYNC_STATUSES = new Set(["success", "failed", "skipped"]);

interface AutoSyncLogRecord {
  id?: string;
  provider: unknown;
  reason: unknown;
  status: unknown;
  startedAt: string;
  finishedAt: string;
  message: string;
  localChanged?: boolean;
}

function getAutoSyncLogPath(): string {
  return path.join(app.getPath("userData"), "logs", "auto-sync.jsonl");
}

function ensureAutoSyncLogFile(): string {
  const filePath = getAutoSyncLogPath();
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  if (!fs.existsSync(filePath)) {
    fs.writeFileSync(filePath, "", "utf8");
  }
  return filePath;
}

export function sanitizeAutoSyncLogMessage(value: unknown): string {
  return String(value ?? "")
    .replace(/https?:\/\/[^\s]+/gi, "[url]")
    .replace(/\b[\w.%+-]+@[\w.-]+\.[A-Za-z]{2,}\b/g, "[email]")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 500);
}

function normalizeAutoSyncLogEntry(entry: unknown): AutoSyncLogRecord | null {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
    return null;
  }

  const record = entry as Record<string, unknown>;
  if (
    !AUTO_SYNC_PROVIDERS.has(String(record.provider)) ||
    !AUTO_SYNC_REASONS.has(String(record.reason)) ||
    !AUTO_SYNC_STATUSES.has(String(record.status)) ||
    typeof record.startedAt !== "string" ||
    typeof record.finishedAt !== "string"
  ) {
    return null;
  }

  return {
    id: typeof record.id === "string" ? record.id : undefined,
    provider: record.provider,
    reason: record.reason,
    status: record.status,
    startedAt: record.startedAt,
    finishedAt: record.finishedAt,
    message: sanitizeAutoSyncLogMessage(record.message),
    localChanged:
      typeof record.localChanged === "boolean"
        ? record.localChanged
        : undefined,
  };
}

function registerCacheHandlers(): void {
  ipcMain.handle(IPC_CHANNELS.APP_GET_CACHE_SIZE, async () => ({
    size: await session.defaultSession.getCacheSize(),
  }));
  ipcMain.handle(IPC_CHANNELS.APP_CLEAR_CACHE, async () => {
    await session.defaultSession.clearCache();
    return { success: true };
  });
}

function registerAutoSyncLogHandler(): void {
  ipcMain.handle(
    IPC_CHANNELS.APP_APPEND_AUTO_SYNC_LOG,
    async (_event, entry: unknown) => {
      const logRecord = normalizeAutoSyncLogEntry(entry);
      if (!logRecord) {
        return { success: false, error: "Invalid automatic sync log entry" };
      }

      fs.appendFileSync(
        ensureAutoSyncLogFile(),
        `${JSON.stringify(logRecord)}\n`,
        "utf8",
      );
      return { success: true, path: getAutoSyncLogPath() };
    },
  );
}

function registerRuntimePathHandler(): void {
  ipcMain.handle(IPC_CHANNELS.APP_GET_RUNTIME_PATHS, async () => ({
    userDataPath: app.getPath("userData"),
    dataDir: getDataDir(),
    databasePath: getDatabasePath(),
    backupsDir: path.join(app.getPath("userData"), "backups"),
    logsDir: path.join(app.getPath("userData"), "logs"),
    autoSyncLogPath: ensureAutoSyncLogFile(),
  }));
}

/** Register app cache, runtime-path, and privacy-scrubbed sync-log handlers. */
export function registerAppRuntimeIPC(): void {
  registerCacheHandlers();
  registerAutoSyncLogHandler();
  registerRuntimePathHandler();
}
