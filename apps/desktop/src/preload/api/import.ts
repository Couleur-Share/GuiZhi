import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { EnqueueImportInput, ImportQueueState, ImportTask } from "@guizhi/shared/types";

export const importApi = {
  enqueue: (inputs: EnqueueImportInput[]): Promise<ImportTask[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_ENQUEUE, inputs),
  list: (): Promise<ImportTask[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_LIST),
  getQueueState: (): Promise<ImportQueueState> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_QUEUE_STATE),
  pause: (): Promise<ImportQueueState> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_PAUSE),
  resume: (): Promise<ImportQueueState> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_RESUME),
  cancel: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_CANCEL, id),
  retry: (
    id: string,
    options?: {
      forceDuplicate?: boolean;
      captureStrategy?: ImportTask["captureStrategy"];
      commentLimit?: ImportTask["commentLimit"];
    },
  ): Promise<ImportTask | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_RETRY, id, options),
  remove: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_REMOVE, id),
  clearFinished: (): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_CLEAR_FINISHED),
  selectFiles: (): Promise<string[]> =>
    ipcRenderer.invoke("dialog:selectImportFiles"),
};
