import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type {
  InboxAiClassificationApplyInput,
  InboxAiClassificationApplyResult,
  InboxAiClassificationSource,
  InboxListResult,
  InboxOrganizeInput,
} from "@guizhi/shared/types";

export const inboxApi = {
  list: (): Promise<InboxListResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_LIST),
  organize: (input: InboxOrganizeInput): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_ORGANIZE, input),
  markReviewed: (itemIds: string[]): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_MARK_REVIEWED, itemIds),
  acknowledgeImportWarning: (taskId: string): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_ACKNOWLEDGE_IMPORT_WARNING, taskId),
  aiClassificationSources: (
    itemIds: string[],
  ): Promise<InboxAiClassificationSource[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_AI_CLASSIFICATION_SOURCES, itemIds),
  applyAiClassification: (
    input: InboxAiClassificationApplyInput,
  ): Promise<InboxAiClassificationApplyResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.INBOX_APPLY_AI_CLASSIFICATION, input),
};
