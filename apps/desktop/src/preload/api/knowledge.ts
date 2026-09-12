import type { SaveKnowledgeDraftInput, SaveKnowledgeDraftResult } from "@guizhi/shared/types/knowledge-draft";
import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  BulkUpdateKnowledgeItemsInput,
  Collection,
  CreateCollectionInput,
  CreateKnowledgeItemInput,
  CreateTagInput,
  KnowledgeCounts,
  KnowledgeFacetCountsQuery,
  KnowledgeItem,
  KnowledgeItemListResult,
  KnowledgeItemQuery,
  KnowledgeItemStatus,
  Tag,
  UpdateCollectionInput,
  UpdateKnowledgeItemInput,
  UpdateTagInput,
} from "@guizhi/shared/types";

export const knowledgeApi = {
  selection: (input: import("@guizhi/shared/types/knowledge-batch").KnowledgeSelectionCommand): Promise<{ ok: boolean; error?: string; ids?: string[]; selectionId?: string; versions?: { id: string; title: string; content: string; transcript: string | null; reviewJson: string; capturedAt: number }[]; results?: import("@guizhi/shared/types/knowledge-batch").KnowledgeBatchResult[] }> => ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_SELECTION, input),
  saveDraft: (input: SaveKnowledgeDraftInput): Promise<SaveKnowledgeDraftResult> => ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_SAVE_DRAFT, input),
  list: (query: KnowledgeItemQuery): Promise<KnowledgeItemListResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_LIST, query),
  get: (id: string): Promise<KnowledgeItem | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_GET, id),
  create: (input: CreateKnowledgeItemInput): Promise<KnowledgeItem> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_CREATE, input),
  update: (
    id: string,
    input: UpdateKnowledgeItemInput,
  ): Promise<KnowledgeItem | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_UPDATE, id, input),
  bulkUpdate: (
    ids: string[],
    input: BulkUpdateKnowledgeItemsInput,
  ): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_BULK_UPDATE, ids, input),
  setStatus: (ids: string[], status: KnowledgeItemStatus): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_SET_STATUS, ids, status),
  moveToTrash: (ids: string[]): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_MOVE_TO_TRASH, ids),
  restore: (ids: string[]): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_RESTORE, ids),
  deleteForever: (ids: string[], options?: { clearEvidence?: boolean }): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_DELETE_FOREVER, ids, options),
  emptyTrash: (options?: { clearEvidence?: boolean }): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_EMPTY_TRASH, options),
  counts: (query?: KnowledgeFacetCountsQuery): Promise<KnowledgeCounts> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_COUNTS, query),
};

export const collectionApi = {
  list: (): Promise<Collection[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.COLLECTION_LIST),
  create: (input: CreateCollectionInput): Promise<Collection> =>
    ipcRenderer.invoke(IPC_CHANNELS.COLLECTION_CREATE, input),
  update: (
    id: string,
    input: UpdateCollectionInput,
  ): Promise<Collection | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.COLLECTION_UPDATE, id, input),
  delete: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.COLLECTION_DELETE, id),
};

export const tagApi = {
  list: (): Promise<Tag[]> => ipcRenderer.invoke(IPC_CHANNELS.TAG_LIST),
  create: (input: CreateTagInput): Promise<Tag> =>
    ipcRenderer.invoke(IPC_CHANNELS.TAG_CREATE, input),
  update: (id: string, input: UpdateTagInput): Promise<Tag | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.TAG_UPDATE, id, input),
  delete: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.TAG_DELETE, id),
};
