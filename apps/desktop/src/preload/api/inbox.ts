import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { InboxListResult, InboxOrganizeInput } from "@guizhi/shared/types";

export const inboxApi = {
  list: (): Promise<InboxListResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_LIST),
  organize: (input: InboxOrganizeInput): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_ORGANIZE, input),
  markReviewed: (itemIds: string[]): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_MARK_REVIEWED, itemIds),
};
