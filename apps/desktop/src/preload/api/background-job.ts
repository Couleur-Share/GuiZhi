import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  BackgroundJob,
  BackgroundJobFailureInput,
  BackgroundJobSyncInput,
} from "@guizhi/shared/types";

export const backgroundJobApi = {
  list: (): Promise<BackgroundJob[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKGROUND_JOB_LIST),
  syncRenderer: (input: BackgroundJobSyncInput): Promise<BackgroundJob[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKGROUND_JOB_SYNC_RENDERER, input),
  renew: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKGROUND_JOB_RENEW, id),
  complete: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKGROUND_JOB_COMPLETE, id),
  fail: (input: BackgroundJobFailureInput): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKGROUND_JOB_FAIL, input),
  pause: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKGROUND_JOB_PAUSE, id),
  resume: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.BACKGROUND_JOB_RESUME, id),
};
