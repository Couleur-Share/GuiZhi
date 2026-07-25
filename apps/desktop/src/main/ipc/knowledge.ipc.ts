import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import { CollectionDB, KnowledgeItemDB, TagDB } from "@guizhi/db";
import type Database from "../database/sqlite";
import type {
  CreateCollectionInput,
  CreateKnowledgeItemInput,
  CreateTagInput,
  KnowledgeItemQuery,
  KnowledgeItemStatus,
  UpdateCollectionInput,
  UpdateKnowledgeItemInput,
  UpdateTagInput,
} from "@guizhi/shared/types";

function normalizeIds(ids: unknown): string[] {
  if (!Array.isArray(ids)) {
    return [];
  }
  return ids.filter(
    (id): id is string => typeof id === "string" && id.length > 0,
  );
}

/**
 * 注册知识域 IPC（条目 / 集合 / 标签）。
 */
export function registerKnowledgeIPC(db: Database.Database): void {
  const items = new KnowledgeItemDB(db);
  const collections = new CollectionDB(db);
  const tags = new TagDB(db);

  // ── 条目 ──────────────────────────────────────────────────────────────────
  ipcMain.handle(
    IPC_CHANNELS.KNOWLEDGE_LIST,
    (_event, query: KnowledgeItemQuery) => items.list(query),
  );
  ipcMain.handle(IPC_CHANNELS.KNOWLEDGE_GET, (_event, id: string) =>
    items.get(id),
  );
  ipcMain.handle(
    IPC_CHANNELS.KNOWLEDGE_CREATE,
    (_event, input: CreateKnowledgeItemInput) => items.create(input ?? {}),
  );
  ipcMain.handle(
    IPC_CHANNELS.KNOWLEDGE_UPDATE,
    (_event, id: string, input: UpdateKnowledgeItemInput) =>
      items.update(id, input ?? {}),
  );
  ipcMain.handle(
    IPC_CHANNELS.KNOWLEDGE_SET_STATUS,
    (_event, ids: unknown, status: KnowledgeItemStatus) =>
      items.setStatus(normalizeIds(ids), status),
  );
  ipcMain.handle(IPC_CHANNELS.KNOWLEDGE_MOVE_TO_TRASH, (_event, ids: unknown) =>
    items.moveToTrash(normalizeIds(ids)),
  );
  ipcMain.handle(IPC_CHANNELS.KNOWLEDGE_RESTORE, (_event, ids: unknown) =>
    items.restore(normalizeIds(ids)),
  );
  ipcMain.handle(
    IPC_CHANNELS.KNOWLEDGE_DELETE_FOREVER,
    (_event, ids: unknown) => items.deleteForever(normalizeIds(ids)),
  );
  ipcMain.handle(IPC_CHANNELS.KNOWLEDGE_EMPTY_TRASH, () => items.emptyTrash());
  ipcMain.handle(IPC_CHANNELS.KNOWLEDGE_COUNTS, () => items.counts());

  // ── 集合 ──────────────────────────────────────────────────────────────────
  ipcMain.handle(IPC_CHANNELS.COLLECTION_LIST, () => collections.list());
  ipcMain.handle(
    IPC_CHANNELS.COLLECTION_CREATE,
    (_event, input: CreateCollectionInput) => collections.create(input),
  );
  ipcMain.handle(
    IPC_CHANNELS.COLLECTION_UPDATE,
    (_event, id: string, input: UpdateCollectionInput) =>
      collections.update(id, input ?? {}),
  );
  ipcMain.handle(IPC_CHANNELS.COLLECTION_DELETE, (_event, id: string) =>
    collections.delete(id),
  );

  // ── 标签 ──────────────────────────────────────────────────────────────────
  ipcMain.handle(IPC_CHANNELS.TAG_LIST, () => tags.list());
  ipcMain.handle(IPC_CHANNELS.TAG_CREATE, (_event, input: CreateTagInput) =>
    tags.create(input),
  );
  ipcMain.handle(
    IPC_CHANNELS.TAG_UPDATE,
    (_event, id: string, input: UpdateTagInput) => tags.update(id, input ?? {}),
  );
  ipcMain.handle(IPC_CHANNELS.TAG_DELETE, (_event, id: string) =>
    tags.delete(id),
  );
}
