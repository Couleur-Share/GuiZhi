import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { EnqueueImportInput, ImportTask } from "@guizhi/shared/types";

export const importApi = {
  enqueue: (inputs: EnqueueImportInput[]): Promise<ImportTask[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_ENQUEUE, inputs),
  list: (): Promise<ImportTask[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_LIST),
  cancel: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_CANCEL, id),
  retry: (
    id: string,
    options?: { forceDuplicate?: boolean },
  ): Promise<ImportTask | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_RETRY, id, options),
  remove: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_REMOVE, id),
  clearFinished: (): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.IMPORT_CLEAR_FINISHED),
  selectFiles: (): Promise<string[]> =>
    ipcRenderer.invoke("dialog:selectImportFiles"),
};
