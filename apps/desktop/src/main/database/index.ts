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
  return dbInit(dbPath, hooks);
}
