import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  ApplySemanticEmbeddingsInput,
  PendingSemanticItem,
  SemanticIndexStatus,
  SemanticSearchHit,
} from "@guizhi/shared/types";

export const semanticApi = {
  status: (model: string): Promise<SemanticIndexStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.SEMANTIC_STATUS, model),
  listPending: (params: {
    model: string;
    limit?: number;
  }): Promise<PendingSemanticItem[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.SEMANTIC_LIST_PENDING, params),
  applyEmbeddings: (input: ApplySemanticEmbeddingsInput): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.SEMANTIC_APPLY_EMBEDDINGS, input),
  search: (params: {
    model: string;
    vector: number[];
    limit?: number;
  }): Promise<SemanticSearchHit[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.SEMANTIC_SEARCH, params),
  clear: (): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.SEMANTIC_CLEAR),
};
