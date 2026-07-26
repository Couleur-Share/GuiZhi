import { ipcMain } from "electron";
import Database from "../database/sqlite";
import type { ImportTask } from "@guizhi/shared/types";
import { registerSettingsIPC } from "./settings.ipc";
import { registerImageIPC } from "./image.ipc";
import { registerAIIPC } from "./ai.ipc";
import { registerSecurityIPC } from "./security.ipc";
import { registerKnowledgeIPC } from "./knowledge.ipc";
import { registerImportIPC } from "./import.ipc";
import { registerWikiIPC } from "./wiki.ipc";
import { registerMigrationIPC } from "./migration.ipc";
import { registerBackupIPC } from "./backup.ipc";
import { registerAskIPC } from "./ask.ipc";
import { registerSemanticIPC } from "./semantic.ipc";
import { registerMediaIPC } from "./media.ipc";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";

const REBINDABLE_DB_CHANNELS = [
  IPC_CHANNELS.KNOWLEDGE_LIST,
  IPC_CHANNELS.KNOWLEDGE_GET,
  IPC_CHANNELS.KNOWLEDGE_CREATE,
  IPC_CHANNELS.KNOWLEDGE_UPDATE,
  IPC_CHANNELS.KNOWLEDGE_SET_STATUS,
  IPC_CHANNELS.KNOWLEDGE_MOVE_TO_TRASH,
  IPC_CHANNELS.KNOWLEDGE_RESTORE,
  IPC_CHANNELS.KNOWLEDGE_DELETE_FOREVER,
  IPC_CHANNELS.KNOWLEDGE_EMPTY_TRASH,
  IPC_CHANNELS.KNOWLEDGE_COUNTS,
  IPC_CHANNELS.COLLECTION_LIST,
  IPC_CHANNELS.COLLECTION_CREATE,
  IPC_CHANNELS.COLLECTION_UPDATE,
  IPC_CHANNELS.COLLECTION_DELETE,
  IPC_CHANNELS.TAG_LIST,
  IPC_CHANNELS.TAG_CREATE,
  IPC_CHANNELS.TAG_UPDATE,
  IPC_CHANNELS.TAG_DELETE,
  IPC_CHANNELS.IMPORT_ENQUEUE,
  IPC_CHANNELS.IMPORT_LIST,
  IPC_CHANNELS.IMPORT_CANCEL,
  IPC_CHANNELS.IMPORT_RETRY,
  IPC_CHANNELS.IMPORT_REMOVE,
  IPC_CHANNELS.IMPORT_CLEAR_FINISHED,
  IPC_CHANNELS.WIKI_CATALOG,
  IPC_CHANNELS.WIKI_GET_PAGE,
  IPC_CHANNELS.WIKI_APPLY_COMPILATION,
  IPC_CHANNELS.WIKI_LIST_COMPILABLE,
  IPC_CHANNELS.WIKI_LIST_INGESTIONS,
  IPC_CHANNELS.WIKI_STATUS,
  IPC_CHANNELS.WIKI_CLEAR,
  IPC_CHANNELS.WIKI_GRAPH,
  IPC_CHANNELS.MIGRATION_DETECT_LEGACY,
  IPC_CHANNELS.MIGRATION_RUN_LEGACY,
  IPC_CHANNELS.BACKUP_CREATE,
  IPC_CHANNELS.BACKUP_LIST,
  IPC_CHANNELS.BACKUP_DELETE,
  IPC_CHANNELS.BACKUP_RESTORE,
  IPC_CHANNELS.EXPORT_MARKDOWN,
  IPC_CHANNELS.ASK_SESSION_LIST,
  IPC_CHANNELS.ASK_SESSION_GET,
  IPC_CHANNELS.ASK_SESSION_SAVE,
  IPC_CHANNELS.ASK_SESSION_DELETE,
  IPC_CHANNELS.SEMANTIC_STATUS,
  IPC_CHANNELS.AI_USAGE_RECORD,
  IPC_CHANNELS.AI_USAGE_SUMMARY,
  IPC_CHANNELS.AI_USAGE_CLEAR,
  IPC_CHANNELS.WIKI_RECORD_FAILURE,
  IPC_CHANNELS.WIKI_LIST_REVISIONS,
  IPC_CHANNELS.WIKI_RESTORE_REVISION,
  IPC_CHANNELS.SEMANTIC_LIST_PENDING,
  IPC_CHANNELS.SEMANTIC_APPLY_EMBEDDINGS,
  IPC_CHANNELS.SEMANTIC_SEARCH,
  IPC_CHANNELS.SEMANTIC_CLEAR,
  IPC_CHANNELS.MEDIA_TRANSCRIBE,
  IPC_CHANNELS.MEDIA_SUMMARIZE,
  IPC_CHANNELS.MEDIA_TEST_TRANSCRIPTION,
  IPC_CHANNELS.YTDLP_STATUS,
  IPC_CHANNELS.YTDLP_INSTALL,
  IPC_CHANNELS.YTDLP_REMOVE,
  IPC_CHANNELS.YTDLP_PICK_BINARY,
  IPC_CHANNELS.FFMPEG_STATUS,
  IPC_CHANNELS.FFMPEG_INSTALL,
  IPC_CHANNELS.FFMPEG_REMOVE,
  IPC_CHANNELS.FFMPEG_PICK_BINARY,
  IPC_CHANNELS.FUNASR_STATUS,
  IPC_CHANNELS.FUNASR_INSTALL,
  IPC_CHANNELS.FUNASR_UNINSTALL,
  IPC_CHANNELS.SETTINGS_GET,
  IPC_CHANNELS.SETTINGS_SET,
  IPC_CHANNELS.SECURITY_SET_MASTER_PASSWORD,
  IPC_CHANNELS.SECURITY_CHANGE_MASTER_PASSWORD,
  IPC_CHANNELS.SECURITY_UNLOCK,
  IPC_CHANNELS.SECURITY_STATUS,
  IPC_CHANNELS.SECURITY_LOCK,
] as const;

function resetAllRegisteredIpcHandlers(): void {
  for (const channel of REBINDABLE_DB_CHANNELS) {
    ipcMain.removeHandler(channel);
  }
}

function registerIpcGroup(label: string, register: () => void): void {
  try {
    register();
  } catch (error) {
    console.error(`[ipc] Failed to register ${label} handlers:`, error);
    throw error;
  }
}

export interface RegisterAllIpcOptions {
  broadcastImportChanged: (task: ImportTask) => void;
}

/**
 * Register all IPC handlers
 * 注册所有 IPC 处理器
 */
export function registerAllIPC(
  db: Database.Database,
  _setDbRef: (db: Database.Database) => void,
  options: RegisterAllIpcOptions,
): void {
  resetAllRegisteredIpcHandlers();

  registerIpcGroup("knowledge", () => registerKnowledgeIPC(db));
  registerIpcGroup("import", () =>
    registerImportIPC(db, options.broadcastImportChanged),
  );
  registerIpcGroup("wiki", () => registerWikiIPC(db));
  registerIpcGroup("migration", () => registerMigrationIPC(db));
  registerIpcGroup("backup", () => registerBackupIPC(db));
  registerIpcGroup("ask", () => registerAskIPC(db));
  registerIpcGroup("semantic", () => registerSemanticIPC(db));
  registerIpcGroup("media", () => registerMediaIPC(db));
  registerIpcGroup("settings", () => registerSettingsIPC(db));
  registerIpcGroup("security", () => registerSecurityIPC(db));
  registerIpcGroup("image", () => registerImageIPC());
  registerIpcGroup("ai", () => registerAIIPC(db));
}
