import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { ArticleAskApi } from "@guizhi/shared/types/article-ask";
export const articleAskApi: ArticleAskApi = {
  context: input => ipcRenderer.invoke(IPC_CHANNELS.ARTICLE_ASK_CONTEXT, input),
  search: input => ipcRenderer.invoke(IPC_CHANNELS.ARTICLE_ASK_SEARCH, input),
  cancelSearch: id => ipcRenderer.invoke(IPC_CHANNELS.ARTICLE_ASK_CANCEL, id),
};
