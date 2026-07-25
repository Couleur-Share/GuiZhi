import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  Collection,
  CreateCollectionInput,
  CreateKnowledgeItemInput,
  CreateTagInput,
  KnowledgeCounts,
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
  setStatus: (ids: string[], status: KnowledgeItemStatus): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_SET_STATUS, ids, status),
  moveToTrash: (ids: string[]): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_MOVE_TO_TRASH, ids),
  restore: (ids: string[]): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_RESTORE, ids),
  deleteForever: (ids: string[]): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_DELETE_FOREVER, ids),
  emptyTrash: (): Promise<number> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_EMPTY_TRASH),
  counts: (): Promise<KnowledgeCounts> =>
    ipcRenderer.invoke(IPC_CHANNELS.KNOWLEDGE_COUNTS),
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
