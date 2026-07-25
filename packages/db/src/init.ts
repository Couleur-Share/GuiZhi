import Database from "./adapter";
import path from "path";
import fs from "fs";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "./schema";
import {
  acquireDatabaseClientLease,
  type DatabaseClientLease,
} from "./database-client-lock";

const QUICK_CHECK_OK = "ok";
const DATABASE_BUSY_TIMEOUT_MS = 5_000;
const QUICK_CHECK_DATABASE_HEADER = /^\*{3} in database .+ \*{3}$/;
const FREELIST_MISMATCH = /^Freelist: size is \d+ but should be \d+$/;
const INDEX_ENTRY_MISMATCH = /^wrong # of entries in index (.+)$/;

function getQuickCheckDiagnostics(probe: Database.Database): string[] {
  const rows = probe.pragma("quick_check");
  if (!Array.isArray(rows)) {
    throw new Error("SQLite quick check returned an invalid result");
  }
  return rows.flatMap((row) => {
    if (!row || typeof row !== "object" || Array.isArray(row)) {
      throw new Error("SQLite quick check returned an invalid row");
    }
    return Object.values(row as Record<string, unknown>).flatMap((value) => {
      if (typeof value !== "string") {
        throw new Error("SQLite quick check returned a non-text diagnostic");
      }
      return value
        .split("\n")
        .map((line) => line.trim())
        .filter(
          (line) => Boolean(line) && !QUICK_CHECK_DATABASE_HEADER.test(line),
        );
    });
  });
}

function isHealthyQuickCheck(diagnostics: string[]): boolean {
  return diagnostics.length === 1 && diagnostics[0] === QUICK_CHECK_OK;
}

function isFreelistOnlyMismatch(diagnostics: string[]): boolean {
  return (
    diagnostics.length > 0 &&
    diagnostics.every((diagnostic) => FREELIST_MISMATCH.test(diagnostic))
  );
}

function getIndexOnlyMismatchNames(diagnostics: string[]): string[] | null {
  if (diagnostics.length === 0) return null;

  const names = new Set<string>();
  for (const diagnostic of diagnostics) {
    const match = INDEX_ENTRY_MISMATCH.exec(diagnostic);
    const indexName = match?.[1]?.trim();
    if (!indexName) return null;
    names.add(indexName);
  }
  return [...names];
}

function createIntegrityBackup(dbPath: string): string {
  const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
  const backupPath = `${dbPath}.integrity-backup-${timestamp}`;
  fs.copyFileSync(dbPath, backupPath);
  return backupPath;
}

function quoteSqliteIdentifier(identifier: string): string {
  return `"${identifier.replace(/"/g, '""')}"`;
}

function inspectDatabaseIntegrity(dbPath: string): string[] {
  const probe = new Database(dbPath);
  try {
    probe.pragma(`busy_timeout = ${DATABASE_BUSY_TIMEOUT_MS}`);
    return getQuickCheckDiagnostics(probe);
  } finally {
    probe.close();
  }
}

function repairFreelistIntegrity(dbPath: string, diagnostics: string[]): void {
  if (!isFreelistOnlyMismatch(diagnostics)) {
    throw new Error(
      `Database integrity check failed: ${diagnostics.join("; ").slice(0, 500)}`,
    );
  }

  const backupPath = createIntegrityBackup(dbPath);

  const repairDatabase = new Database(dbPath);
  try {
    repairDatabase.pragma(`busy_timeout = ${DATABASE_BUSY_TIMEOUT_MS}`);
    repairDatabase.exec("VACUUM");
  } finally {
    repairDatabase.close();
  }

  const repairedDiagnostics = inspectDatabaseIntegrity(dbPath);
  if (!isHealthyQuickCheck(repairedDiagnostics)) {
    throw new Error(
      `Database integrity repair failed: ${repairedDiagnostics.join("; ").slice(0, 500)}`,
    );
  }
  console.log(
    `[DB] Repaired SQLite freelist metadata; backup=${path.basename(backupPath)}`,
  );
}

function repairIndexIntegrity(dbPath: string, diagnostics: string[]): void {
  const indexNames = getIndexOnlyMismatchNames(diagnostics);
  if (!indexNames) {
    throw new Error(
      `Database integrity check failed: ${diagnostics.join("; ").slice(0, 500)}`,
    );
  }

  const backupPath = createIntegrityBackup(dbPath);
  const repairDatabase = new Database(dbPath);
  try {
    repairDatabase.pragma(`busy_timeout = ${DATABASE_BUSY_TIMEOUT_MS}`);
    const repair = repairDatabase.transaction(() => {
      for (const indexName of indexNames) {
        const existing = repairDatabase.get(
          "SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?",
          indexName,
        );
        if (!existing) {
          throw new Error(`SQLite index does not exist: ${indexName}`);
        }
        repairDatabase.exec(`REINDEX ${quoteSqliteIdentifier(indexName)}`);
      }

      const transactionalDiagnostics = getQuickCheckDiagnostics(repairDatabase);
      if (!isHealthyQuickCheck(transactionalDiagnostics)) {
        throw new Error(
          `Database integrity repair failed: ${transactionalDiagnostics.join("; ").slice(0, 500)}`,
        );
      }
    });
    repair();
  } finally {
    repairDatabase.close();
  }

  const repairedDiagnostics = inspectDatabaseIntegrity(dbPath);
  if (!isHealthyQuickCheck(repairedDiagnostics)) {
    throw new Error(
      `Database integrity repair failed: ${repairedDiagnostics.join("; ").slice(0, 500)}`,
    );
  }
  console.log(
    `[DB] Rebuilt SQLite indexes (${indexNames.join(", ")}); backup=${path.basename(backupPath)}`,
  );
}

function ensureDatabaseIntegrity(dbPath: string): void {
  if (!fs.existsSync(dbPath) || fs.statSync(dbPath).size === 0) return;
  const diagnostics = inspectDatabaseIntegrity(dbPath);
  if (isHealthyQuickCheck(diagnostics)) return;
  if (isFreelistOnlyMismatch(diagnostics)) {
    repairFreelistIntegrity(dbPath, diagnostics);
    return;
  }
  if (getIndexOnlyMismatchNames(diagnostics)) {
    repairIndexIntegrity(dbPath, diagnostics);
    return;
  }
  throw new Error(
    `Database integrity check failed: ${diagnostics.join("; ").slice(0, 500)}`,
  );
}

/**
 * Hook functions that allow the host application to inject environment-specific
 * behaviour into the database initialization process.
 */
export interface InitDatabaseHooks {
  /**
   * Recover a legacy lock without lease metadata. Only hosts with an external
   * single-instance guarantee may enable this; shared callers default to false.
   */
  recoverUnregisteredLock?: boolean;
}

let db: Database.Database | null = null;
let dbClientLease: DatabaseClientLease | null = null;

function resetFailedDatabaseInitialization(): void {
  const failedDatabase = db;
  db = null;
  try {
    failedDatabase?.close();
  } catch (error) {
    console.warn("[DB] Failed to close an incomplete database:", error);
  } finally {
    dbClientLease?.release();
    dbClientLease = null;
  }
}

/**
 * Initialize database at the given path, run schema creation and migrations.
 *
 * @param dbPath  Absolute path to the SQLite database file.
 * @param hooks   Optional hooks for environment-specific behaviour.
 */
export function initDatabase(
  dbPath: string,
  hooks?: InitDatabaseHooks,
): Database.Database {
  if (db) return db;

  fs.mkdirSync(path.dirname(dbPath), { recursive: true });
  dbClientLease = acquireDatabaseClientLease(dbPath, {
    recoverUnregisteredLock: hooks?.recoverUnregisteredLock,
  });
  try {
    ensureDatabaseIntegrity(dbPath);
    db = new Database(dbPath);

    // Serialize short cross-process write overlaps before reporting a conflict.
    db.pragma(`busy_timeout = ${DATABASE_BUSY_TIMEOUT_MS}`);

    // Enable foreign key constraints
    db.pragma("foreign_keys = ON");

    // Create tables only (indexes come after migrations)
    db.exec(SCHEMA_TABLES);
  } catch (error) {
    resetFailedDatabaseInitialization();
    throw error;
  }

  // Run all migrations in a single transaction to avoid lock contention.
  // 迁移在单事务中执行；表结构本身走 CREATE IF NOT EXISTS 增量创建。
  const runMigrations = db.transaction(() => {
    db!.exec(`
      CREATE TABLE IF NOT EXISTS schema_migrations (
        name TEXT PRIMARY KEY,
        applied_at INTEGER NOT NULL
      )
    `);
  });

  try {
    runMigrations();
    // Now that all columns exist, create indexes + FTS
    if (SCHEMA_INDEXES.trim().length > 0) {
      db.exec(SCHEMA_INDEXES);
    }
  } catch (error) {
    console.error("Database migration failed:", error);
    resetFailedDatabaseInitialization();
    throw error;
  }

  console.log(`Database initialized at: ${dbPath}`);
  return db;
}

/**
 * Get database instance
 */
export function getDatabase(): Database.Database {
  if (!db) {
    throw new Error("Database not initialized");
  }
  return db;
}

/**
 * Close database connection
 */
export function closeDatabase(): void {
  const databaseToClose = db;
  db = null;
  try {
    databaseToClose?.close();
  } finally {
    dbClientLease?.release();
    dbClientLease = null;
  }
}

export { db };
