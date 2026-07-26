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
  WikiSearchHit,
} from "@guizhi/shared/types";

export const wikiApi = {
  catalog: (): Promise<WikiCatalogEntry[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_CATALOG),
  search: (query: string, limit: number): Promise<WikiSearchHit[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_SEARCH, query, limit),
  updatePage: (input: {
    pageId: string;
    body: string;
    linkTargets: string[];
    releaseToAuto?: boolean;
  }): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_UPDATE_PAGE, input),
  deletePage: (pageId: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.WIKI_DELETE_PAGE, pageId),
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
