import path from "path";
import { app, ipcMain, session } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { getDataDir, getDatabasePath } from "../runtime-paths";

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

/** Register app cache and runtime-path handlers. */
export function registerAppRuntimeIPC(): void {
  registerCacheHandlers();
  registerRuntimePathHandler();
}
