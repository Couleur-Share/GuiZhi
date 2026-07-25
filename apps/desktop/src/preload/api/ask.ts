import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  AskSessionMeta,
  AskSessionRecord,
  SaveAskSessionInput,
} from "@guizhi/shared/types";

export const askSessionApi = {
  list: (): Promise<AskSessionMeta[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_LIST),
  get: (id: string): Promise<AskSessionRecord | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_GET, id),
  save: (input: SaveAskSessionInput): Promise<AskSessionRecord> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_SAVE, input),
  delete: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_DELETE, id),
};
