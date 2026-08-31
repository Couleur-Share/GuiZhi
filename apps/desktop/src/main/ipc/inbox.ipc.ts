import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { InboxOrganizeInput } from "@guizhi/shared/types";
import type Database from "../database/sqlite";
import {
  listInboxItems,
  markInboxItemsReviewed,
  organizeInboxItems,
} from "../services/inbox";

function ids(value: unknown): string[] {
  return Array.isArray(value)
    ? [...new Set(value.filter((id): id is string => typeof id === "string" && id.length > 0))]
    : [];
}

export function registerInboxIPC(db: Database.Database): void {
  ipcMain.handle(IPC_CHANNELS.INBOX_LIST, () => listInboxItems(db));
  ipcMain.handle(
    IPC_CHANNELS.INBOX_ORGANIZE,
    (_event, input: InboxOrganizeInput) =>
      organizeInboxItems(db, { ...input, itemIds: ids(input?.itemIds) }),
  );
  ipcMain.handle(IPC_CHANNELS.INBOX_MARK_REVIEWED, (_event, value: unknown) =>
    markInboxItemsReviewed(db, ids(value)),
  );
}
