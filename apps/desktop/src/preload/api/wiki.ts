import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  WikiApplyCompilationInput,
  WikiCatalogEntry,
  WikiCompilableItem,
  WikiCompilationStatus,
  WikiGraph,
  WikiIngestion,
  WikiPageDetail,
  WikiPageRevision,
} from "@guizhi/shared/types";

export const wikiApi = {
  catalog: (): Promise<WikiCatalogEntry[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_CATALOG),
  getPage: (id: string): Promise<WikiPageDetail | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_GET_PAGE, id),
  applyCompilation: (input: WikiApplyCompilationInput): Promise<void> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_APPLY_COMPILATION, input),
  listCompilable: (): Promise<WikiCompilableItem[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_LIST_COMPILABLE),
  listIngestions: (): Promise<WikiIngestion[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_LIST_INGESTIONS),
  status: (): Promise<WikiCompilationStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_STATUS),
  graph: (): Promise<WikiGraph> => ipcRenderer.invoke(IPC_CHANNELS.WIKI_GRAPH),
  clear: (): Promise<void> => ipcRenderer.invoke(IPC_CHANNELS.WIKI_CLEAR),
  recordCompilationFailure: (
    itemId: string,
    contentHash: string,
    nextAttemptAt: number | null,
  ): Promise<number> =>
    ipcRenderer.invoke(
      IPC_CHANNELS.WIKI_RECORD_FAILURE,
      itemId,
      contentHash,
      nextAttemptAt,
    ),
  listRevisions: (pageId: string): Promise<WikiPageRevision[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_LIST_REVISIONS, pageId),
  restoreRevision: (revisionId: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_RESTORE_REVISION, revisionId),
};
