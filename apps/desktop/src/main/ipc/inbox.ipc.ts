import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { InboxOrganizeInput } from "@guizhi/shared/types";
import type { InboxAiClassificationApplyInput } from "@guizhi/shared/types";
import type Database from "../database/sqlite";
import {
  acknowledgeInboxImportWarning,
  applyInboxAiClassification,
  listInboxItems,
  listInboxAiClassificationSources,
  markInboxItemsReviewed,
  organizeInboxItems,
} from "../services/inbox";

function ids(value: unknown): string[] {
  return Array.isArray(value)
    ? [
        ...new Set(
          value.filter(
            (id): id is string => typeof id === "string" && id.length > 0,
          ),
        ),
      ]
    : [];
}

function classificationInput(value: unknown): InboxAiClassificationApplyInput {
  const assignments =
    typeof value === "object" && value !== null
      ? (value as { assignments?: unknown }).assignments
      : null;
  return {
    assignments: Array.isArray(assignments)
      ? assignments.map((assignment) => {
          const row =
            typeof assignment === "object" && assignment !== null
              ? (assignment as Record<string, unknown>)
              : {};
          return {
            itemId: typeof row.itemId === "string" ? row.itemId : "",
            collectionName:
              typeof row.collectionName === "string" ? row.collectionName : "",
          };
        })
      : [],
  };
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
  ipcMain.handle(
    IPC_CHANNELS.INBOX_ACKNOWLEDGE_IMPORT_WARNING,
    (_event, value: unknown) =>
      acknowledgeInboxImportWarning(
        db,
        typeof value === "string" ? value.trim() : "",
      ),
  );
  ipcMain.handle(
    IPC_CHANNELS.INBOX_AI_CLASSIFICATION_SOURCES,
    (_event, value: unknown) =>
      listInboxAiClassificationSources(db, ids(value)),
  );
  ipcMain.handle(
    IPC_CHANNELS.INBOX_APPLY_AI_CLASSIFICATION,
    (_event, value: unknown) =>
      applyInboxAiClassification(db, classificationInput(value)),
  );
}
