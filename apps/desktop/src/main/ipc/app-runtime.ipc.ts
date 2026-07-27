import path from "path";
import { app, ipcMain, session } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { getDataDir, getDatabasePath } from "../runtime-paths";
import { logAppError } from "../diagnostic-log";

/** 单条日志的长度上限：报错原文可能是一整页 HTML，别把日志文件撑爆 */
const LOG_MESSAGE_MAX_CHARS = 2000;

function readLogField(value: unknown, max: number): string {
  return typeof value === "string" ? value.trim().slice(0, max) : "";
}

/**
 * 渲染进程的业务失败汇入主进程日志。
 *
 * 渲染侧的 console 只进 DevTools，普通用户看不到；错误提示又是一次性的，
 * 关掉就没了。要让「事后能查」成立，两边的失败必须落到同一个文件里。
 */
function registerErrorLogHandler(): void {
  // 这个注册函数只在启动时跑一次，但 `on` 不像 `handle` 会覆盖，先清一遍更稳
  ipcMain.removeAllListeners(IPC_CHANNELS.LOG_APP_ERROR);
  ipcMain.on(IPC_CHANNELS.LOG_APP_ERROR, (_event, entry: unknown) => {
    if (!entry || typeof entry !== "object") {
      return;
    }
    const source = entry as Record<string, unknown>;
    const message = readLogField(source.message, LOG_MESSAGE_MAX_CHARS);
    if (!message) {
      return;
    }
    logAppError({
      scope: readLogField(source.scope, 64) || "renderer",
      action: readLogField(source.action, 64) || "unknown",
      message,
    });
  });
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

function registerRuntimePathHandler(): void {
  ipcMain.handle(IPC_CHANNELS.APP_GET_RUNTIME_PATHS, async () => ({
    userDataPath: app.getPath("userData"),
    dataDir: getDataDir(),
    databasePath: getDatabasePath(),
    backupsDir: path.join(app.getPath("userData"), "backups"),
    logsDir: path.join(app.getPath("userData"), "logs"),
  }));
}

/** Register app cache, runtime-path and error-log handlers. */
export function registerAppRuntimeIPC(): void {
  registerCacheHandlers();
  registerRuntimePathHandler();
  registerErrorLogHandler();
}
