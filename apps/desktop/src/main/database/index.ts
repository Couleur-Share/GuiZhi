/**
 * Desktop-specific database initialization.
 *
 * Re-exports the shared adapter from @guizhi's db package and resolves the
 * database path via runtime-paths.
 */
import {
  DatabaseAdapter,
  initDatabase as dbInit,
  getDatabase,
  closeDatabase,
  KnowledgeItemDB,
} from "@guizhi/db";
import type { InitDatabaseHooks } from "@guizhi/db";
import { getDatabasePath } from "../runtime-paths";

// ── Re-exports from @guizhi/db ────────────────────────────────────────────
export { getDatabase, closeDatabase };
export { DatabaseAdapter } from "@guizhi/db";
export type { Database } from "@guizhi/db";
export { SCHEMA_TABLES, SCHEMA_INDEXES, SCHEMA } from "@guizhi/db";

/**
 * Initialize database with desktop-specific path resolution.
 */
export function initDatabase(): DatabaseAdapter.Database {
  const dbPath = getDatabasePath();
  const hooks: InitDatabaseHooks = {
    // Main-process initialization runs only after Electron's single-instance gate.
    recoverUnregisteredLock: true,
  };
  const db = dbInit(dbPath, hooks);

  // 老库里回收站条目没有 FTS 行（旧版本软删时会移出索引），补上后回收站可搜索。
  // 无缺失时只花一次 NOT EXISTS 查询。
  const backfilled = new KnowledgeItemDB(db).backfillMissingFtsRows();
  if (backfilled > 0) {
    console.log(`[db] 补齐 ${backfilled} 条缺失的全文索引`);
  }

  return db;
}
