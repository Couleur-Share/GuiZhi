import { BrowserWindow, dialog, ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { EnqueueImportInput, ImportTask } from "@guizhi/shared/types";
import type Database from "../database/sqlite";
import {
  createImportService,
  type ImportService,
} from "../services/import/import-service";

let service: ImportService | null = null;

/**
 * 注册导入管线 IPC 并启动队列（含上次退出的恢复）。
 */
export function registerImportIPC(
  db: Database.Database,
  broadcast: (task: ImportTask) => void,
): void {
  service = createImportService(db, broadcast);

  ipcMain.handle(
    IPC_CHANNELS.IMPORT_ENQUEUE,
    (_event, inputs: EnqueueImportInput[]) =>
      service!.queue.enqueue(Array.isArray(inputs) ? inputs : []),
  );
  ipcMain.handle(IPC_CHANNELS.IMPORT_LIST, () => service!.taskDb.list());
  ipcMain.handle(IPC_CHANNELS.IMPORT_CANCEL, (_event, id: string) =>
    service!.queue.cancel(id),
  );
  ipcMain.handle(
    IPC_CHANNELS.IMPORT_RETRY,
    (_event, id: string, options?: { forceDuplicate?: boolean }) =>
      service!.queue.retry(id, options),
  );
  ipcMain.handle(IPC_CHANNELS.IMPORT_CLEAR_FINISHED, () =>
    service!.taskDb.clearFinished(),
  );

  ipcMain.handle("dialog:selectImportFiles", async () => {
    const result = await dialog.showOpenDialog(
      BrowserWindow.getFocusedWindow() ?? undefined!,
      {
        properties: ["openFile", "multiSelections"],
        title: "选择要导入的文件",
        filters: [
          {
            name: "支持的文件",
            extensions: [
              "txt", "md", "markdown",
              "png", "jpg", "jpeg", "webp", "gif", "bmp",
              "mp3", "wav", "m4a", "aac", "ogg", "flac",
              "mp4", "webm", "mkv", "mov", "avi",
            ],
          },
          { name: "文本文件", extensions: ["txt", "md", "markdown"] },
          { name: "图片", extensions: ["png", "jpg", "jpeg", "webp", "gif", "bmp"] },
          { name: "音频", extensions: ["mp3", "wav", "m4a", "aac", "ogg", "flac"] },
          { name: "视频", extensions: ["mp4", "webm", "mkv", "mov", "avi"] },
          { name: "All Files", extensions: ["*"] },
        ],
      },
    );
    return result.canceled ? [] : result.filePaths;
  });

  // 主窗口就绪后恢复上次遗留的任务
  service.queue.recover();
}
