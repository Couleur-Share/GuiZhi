import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  BackgroundJobFailureInput,
  BackgroundJobSyncInput,
} from "@guizhi/shared/types";
import type { BackgroundJobRuntime } from "../services/background-jobs";

function requireId(value: unknown): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error("后台任务 id 不能为空");
  }
  return value;
}

export function registerBackgroundJobIPC(runtime: BackgroundJobRuntime): void {
  ipcMain.handle(IPC_CHANNELS.BACKGROUND_JOB_LIST, () => runtime.list());
  ipcMain.handle(
    IPC_CHANNELS.BACKGROUND_JOB_SYNC_RENDERER,
    (_event, input: BackgroundJobSyncInput) => {
      if (
        !input ||
        typeof input.wikiEnabled !== "boolean" ||
        typeof input.semanticEnabled !== "boolean"
      ) {
        throw new Error("后台任务同步参数不合法");
      }
      return runtime.syncRendererJobs(input);
    },
  );
  ipcMain.handle(IPC_CHANNELS.BACKGROUND_JOB_RENEW, (_event, id: unknown) =>
    runtime.renewRendererJob(requireId(id)),
  );
  ipcMain.handle(IPC_CHANNELS.BACKGROUND_JOB_COMPLETE, (_event, id: unknown) =>
    runtime.completeRendererJob(requireId(id)),
  );
  ipcMain.handle(
    IPC_CHANNELS.BACKGROUND_JOB_FAIL,
    (_event, input: BackgroundJobFailureInput) => {
      if (!input || typeof input.error !== "string") {
        throw new Error("后台任务失败参数不合法");
      }
      return runtime.failRendererJob(
        requireId(input.id),
        input.error,
        input.pause === true,
      );
    },
  );
  ipcMain.handle(IPC_CHANNELS.BACKGROUND_JOB_PAUSE, (_event, id: unknown) =>
    runtime.setPaused(requireId(id), true),
  );
  ipcMain.handle(IPC_CHANNELS.BACKGROUND_JOB_RESUME, (_event, id: unknown) =>
    runtime.setPaused(requireId(id), false),
  );
}
