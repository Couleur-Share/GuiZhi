import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  AskSessionFilter,
  AskSessionMeta,
  AskSessionRecord,
  SaveAskSessionInput,
} from "@guizhi/shared/types";

export const askSessionApi = {
  query: (input: import("@guizhi/shared/types/ask").AskSessionQuery): Promise<import("@guizhi/shared/types/ask").AskSessionPage> => ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_QUERY, input),
  updateMeta: (id: string, patch: { title?: string; pinned?: boolean }): Promise<AskSessionRecord> => ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_META, id, patch),
  list: (filter?: AskSessionFilter): Promise<AskSessionMeta[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_LIST, filter),
  get: (id: string): Promise<AskSessionRecord | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_GET, id),
  save: (input: SaveAskSessionInput): Promise<AskSessionRecord> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_SAVE, input),
  delete: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.ASK_SESSION_DELETE, id),
};
