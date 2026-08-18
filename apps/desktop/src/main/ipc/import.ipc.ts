import { BrowserWindow, dialog, ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import {
  isCommentLimit,
  isImportCaptureStrategy,
  type EnqueueImportInput,
  type ImportTask,
} from "@guizhi/shared/types";
import { detectPlatformCapturePlatform } from "@guizhi/shared/utils/platform-capture";
import type Database from "../database/sqlite";
import {
  createImportService,
  type ImportService,
} from "../services/import/import-service";

let service: ImportService | null = null;

function validInput(value: unknown): value is EnqueueImportInput {
  if (!value || typeof value !== "object") return false;
  const input = value as EnqueueImportInput;
  if (!["text", "file", "url"].includes(input.kind) || typeof input.input !== "string" || !input.input.trim()) return false;
  if (input.captureStrategy !== undefined && !isImportCaptureStrategy(input.captureStrategy)) return false;
  if (input.commentLimit !== undefined && !isCommentLimit(input.commentLimit)) return false;
  if (
    input.captureStrategy === "authenticated" &&
    (input.kind !== "url" || !detectPlatformCapturePlatform(input.input))
  ) return false;
  if (
    (input.commentLimit ?? 0) > 0 &&
    (input.captureStrategy !== "authenticated" ||
      input.kind !== "url" ||
      !detectPlatformCapturePlatform(input.input))
  ) return false;
  return true;
}

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
    (_event, inputs: EnqueueImportInput[]) => {
      if (!Array.isArray(inputs) || inputs.some((input) => !validInput(input))) {
        throw new Error("导入参数不合法");
      }
      const safe = inputs;
      const authenticatedCount = safe.filter(
        (input) => input.captureStrategy === "authenticated",
      ).length;
      if (authenticatedCount > 50) {
        throw new Error("单次最多创建 50 个登录态采集任务");
      }
      return service!.queue.enqueue(safe);
    },
  );
  ipcMain.handle(IPC_CHANNELS.IMPORT_LIST, () => service!.taskDb.list());
  ipcMain.handle(IPC_CHANNELS.IMPORT_QUEUE_STATE, () => service!.queue.getState());
  ipcMain.handle(IPC_CHANNELS.IMPORT_PAUSE, () => service!.queue.pause());
  ipcMain.handle(IPC_CHANNELS.IMPORT_RESUME, () => service!.queue.resume());
  ipcMain.handle(IPC_CHANNELS.IMPORT_CANCEL, (_event, id: string) =>
    service!.queue.cancel(id),
  );
  ipcMain.handle(
    IPC_CHANNELS.IMPORT_RETRY,
    (
      _event,
      id: string,
      options?: {
        forceDuplicate?: boolean;
        captureStrategy?: ImportTask["captureStrategy"];
        commentLimit?: ImportTask["commentLimit"];
      },
    ) => {
      if (typeof id !== "string" || !id) throw new Error("任务 ID 不合法");
      if (
        options?.captureStrategy !== undefined &&
        !isImportCaptureStrategy(options.captureStrategy)
      ) throw new Error("采集策略不合法");
      if (
        options?.commentLimit !== undefined &&
        !isCommentLimit(options.commentLimit)
      ) throw new Error("评论数量不合法");
      if (
        options?.forceDuplicate !== undefined &&
        typeof options.forceDuplicate !== "boolean"
      ) throw new Error("重复导入参数不合法");
      const task = service!.taskDb.get(id);
      if (!task) return null;
      const requestedStrategy = isImportCaptureStrategy(options?.captureStrategy)
        ? options.captureStrategy
        : undefined;
      if (
        requestedStrategy === "authenticated" &&
        (task.sourceKind !== "url" || !detectPlatformCapturePlatform(task.sourceInput))
      ) {
        throw new Error("该任务不支持登录态采集");
      }
      const requestedCommentLimit = isCommentLimit(options?.commentLimit)
        ? options.commentLimit
        : undefined;
      const resultingStrategy = requestedStrategy ?? task.captureStrategy;
      if (
        (requestedCommentLimit ?? 0) > 0 &&
        (resultingStrategy !== "authenticated" ||
          task.sourceKind !== "url" ||
          !detectPlatformCapturePlatform(task.sourceInput))
      ) {
        throw new Error("热门评论只支持小红书、抖音或 LINUX DO 登录态任务");
      }
      const safe = {
        ...(typeof options?.forceDuplicate === "boolean"
          ? { forceDuplicate: options.forceDuplicate }
          : {}),
        ...(requestedStrategy
          ? { captureStrategy: requestedStrategy }
          : {}),
        ...(requestedCommentLimit !== undefined
          ? { commentLimit: requestedCommentLimit }
          : {}),
      };
      return service!.queue.retry(id, safe);
    },
  );
  ipcMain.handle(IPC_CHANNELS.IMPORT_REMOVE, (_event, id: string) =>
    service!.taskDb.remove(id),
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
