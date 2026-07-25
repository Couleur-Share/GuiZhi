// Database adapter
export { default as DatabaseAdapter } from "./adapter";
export type { default as Database } from "./adapter";

// Schema
export { SCHEMA_TABLES, SCHEMA_INDEXES, SCHEMA } from "./schema";
export {
  addColumnIfMissing,
  getSchemaVersion,
  hasColumn,
  runMigrations,
  MIGRATIONS,
  SCHEMA_VERSION,
} from "./migrations";
export type { Migration } from "./migrations";

// Initialization
export { initDatabase, getDatabase, closeDatabase, db } from "./init";
export type { InitDatabaseHooks } from "./init";
export {
  acquireDatabaseClientLease,
  inspectDatabaseClientLock,
  recoverDatabaseClientLock,
} from "./database-client-lock";
export type {
  DatabaseClientLease,
  DatabaseClientLeaseOptions,
  DatabaseLockInspection,
  DatabaseLockRecoveryReason,
  DatabaseLockRecoveryResult,
} from "./database-client-lock";

// Knowledge domain DAOs
export { KnowledgeItemDB, makeSnippet } from "./knowledge";
export { CollectionDB } from "./collection";
export { TagDB } from "./tag";
export { ImportTaskDB } from "./import-task";
export { WikiDB } from "./wiki";
export { AskSessionDB } from "./ask-session";
export { AIUsageDB, toLocalDay } from "./ai-usage";
export { SemanticIndexDB, vectorToBlob, blobToVector } from "./semantic";
export type { SemanticChunkRecord, SemanticItemState } from "./semantic";
export {
  migrateLegacyDatabase,
  isTargetDatabaseEmpty,
  countLegacyItems,
} from "./legacy-migration";
export type { LegacyMigrationStats } from "./legacy-migration";
export { segmentTextForFts, buildFtsMatchQuery } from "./fts";
