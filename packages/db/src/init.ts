import Database from "./adapter";
import path from "path";
import fs from "fs";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "./schema";
import { runMigrations } from "./migrations";
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

/** 干净关闭标记：存在且够新就说明上次是正常退出 */
function getCleanShutdownMarkerPath(dbPath: string): string {
  return `${dbPath}.clean`;
}

/** 兜底全量校验的间隔：正常退出也每周查一次，防的是外部原因的坏块 */
const INTEGRITY_RECHECK_INTERVAL_MS = 7 * 24 * 60 * 60 * 1000;

/**
 * 上次正常退出时记下的「最近一次真正校验」时刻；读完即删。
 *
 * 崩溃或断电时没人写标记，下次启动自然读不到，于是照常全量校验。
 */
function consumeCleanShutdownMarker(dbPath: string): number | null {
  const markerPath = getCleanShutdownMarkerPath(dbPath);
  let raw: string;
  try {
    raw = fs.readFileSync(markerPath, "utf8");
  } catch {
    return null;
  } finally {
    fs.rmSync(markerPath, { force: true });
  }
  const checkedAt = Number.parseInt(raw, 10);
  return Number.isFinite(checkedAt) ? checkedAt : null;
}

function writeCleanShutdownMarker(dbPath: string, checkedAt: number): void {
  try {
    fs.writeFileSync(getCleanShutdownMarkerPath(dbPath), String(checkedAt));
  } catch (error) {
    // 写不进去只是下次多校验一遍，不该拦住退出
    console.warn("[DB] 未能写入干净关闭标记:", error);
  }
}

/**
 * 启动完整性检查。
 *
 * 这一步在窗口创建之前同步执行，`quick_check` 的耗时与库体积成正比——
 * 实测 2 万条（364MB）要 920ms，用户看到的是「点了图标半天没反应」。
 *
 * 所以只在上次没能正常退出时才查：正常退出会在 closeDatabase 里留下标记，
 * SQLite 的事务保证此时文件必然自洽，再查一遍纯属白等。标记超过一周
 * 仍会强制查一次——磁盘坏块不挑应用退没退干净。
 */
function ensureDatabaseIntegrity(dbPath: string): void {
  const previousCheckAt = consumeCleanShutdownMarker(dbPath);
  if (!fs.existsSync(dbPath) || fs.statSync(dbPath).size === 0) {
    return;
  }
  if (
    previousCheckAt !== null &&
    Date.now() - previousCheckAt < INTEGRITY_RECHECK_INTERVAL_MS
  ) {
    // 上次正常退出，且距上次校验不到一周：SQLite 的事务保证文件自洽
    lastIntegrityCheckAt = previousCheckAt;
    return;
  }

  const startedAt = Date.now();
  const diagnostics = inspectDatabaseIntegrity(dbPath);
  lastIntegrityCheckAt = startedAt;
  const elapsed = Date.now() - startedAt;
  if (elapsed > 1000) {
    console.warn(
      `[DB] 启动完整性检查耗时 ${elapsed}ms（此期间主进程无响应）`,
    );
  }
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

/** 统计信息过期到这个倍数就重新 ANALYZE */
const STATS_DRIFT_FACTOR = 3;

/**
 * 让查询规划器拿到真实的选择度。
 *
 * 没有 sqlite_stat1 时 SQLite 只能按「等值约束 = 高选择度」的经验值估算，
 * 于是列表查询会挑中 idx_items_deleted——deleted_at IS NULL 被当成等值，
 * 而它其实匹配几乎所有行，接着还要对全部匹配行做一次临时 B 树排序。
 * 有了统计信息，规划器改走 idx_items_pinned_updated 顺序扫描并在 LIMIT
 * 处提前收工。实测 2 万条列表首屏 272ms → 16ms。
 *
 * ANALYZE 本身在 2 万条上约 40ms，只在没跑过或行数漂移过大时执行。
 */
function ensureQueryPlannerStats(database: Database.Database): void {
  const hasStatsTable = database.get(
    "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_stat1'",
  );
  if (hasStatsTable) {
    const recorded = database.get(
      "SELECT stat FROM sqlite_stat1 WHERE tbl = 'knowledge_items' LIMIT 1",
    ) as { stat: string } | undefined;
    // 每条 stat 的首个数字都是 ANALYZE 当时的表行数
    const analyzedRows = Number.parseInt(recorded?.stat ?? "", 10);
    if (Number.isFinite(analyzedRows)) {
      const actual = (
        database.get("SELECT COUNT(*) AS count FROM knowledge_items") as {
          count: number;
        }
      ).count;
      const drifted =
        actual > Math.max(1, analyzedRows) * STATS_DRIFT_FACTOR ||
        actual * STATS_DRIFT_FACTOR < analyzedRows;
      if (!drifted) return;
    }
  }
  const startedAt = Date.now();
  database.exec("ANALYZE");
  console.log(`[DB] 已刷新查询规划统计信息（${Date.now() - startedAt}ms）`);
}

/**
 * Hook functions that allow the host application to inject environment-specific
 * behaviour into the database initialization process.
 */
export interface InitDatabaseHooks {
  /** Called under the database lease, before any schema creation or migration. */
  beforeSchemaUpgrade?: (database: Database.Database) => void;
  /**
   * Recover a legacy lock without lease metadata. Only hosts with an external
   * single-instance guarantee may enable this; shared callers default to false.
   */
  recoverUnregisteredLock?: boolean;
}

let db: Database.Database | null = null;
let dbClientLease: DatabaseClientLease | null = null;
/** 最近一次真正跑过 quick_check 的时刻，正常退出时写进标记文件 */
let lastIntegrityCheckAt = 0;
let openDatabasePath: string | null = null;

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

  const initStartedAt = Date.now();
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

    // 这里不设 journal_mode = WAL。node-sqlite3-wasm 的 Emscripten 构建没有
    // WAL 需要的共享内存 VFS，实测（2026-07）执行该 PRAGMA 既不报错也不生效，
    // 返回值仍是 delete。所以库跑在 rollback journal 下：写事务期间读被阻塞。
    // 谁要再加这一行，先确认 `PRAGMA journal_mode` 的返回值真的变了。

    hooks?.beforeSchemaUpgrade?.(db);

    // Create tables only (indexes come after migrations)
    db.exec(SCHEMA_TABLES);
  } catch (error) {
    resetFailedDatabaseInitialization();
    throw error;
  }

  try {
    // 结构演进：新表由 SCHEMA_TABLES 覆盖，列级变更走迁移执行器
    const executed = runMigrations(db);
    if (executed.length > 0) {
      console.log(`[DB] 已应用 schema 迁移: ${executed.join(", ")}`);
    }
    // Now that all columns exist, create indexes + FTS
    if (SCHEMA_INDEXES.trim().length > 0) {
      db.exec(SCHEMA_INDEXES);
    }
    ensureQueryPlannerStats(db);
  } catch (error) {
    console.error("Database migration failed:", error);
    resetFailedDatabaseInitialization();
    throw error;
  }

  openDatabasePath = dbPath;
  console.log(
    `Database initialized at: ${dbPath} (${Date.now() - initStartedAt}ms)`,
  );
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
 * 拿数据库，没有就返回 null。
 *
 * 给「有则记一笔、没有就算了」的旁路用（用量记账是第一个）。这类调用方
 * 必须把「库还没初始化」与「写入失败」分开：前者在单测和备份恢复期间都是
 * 常态，走 getDatabase() 会抛，而按错误文本去认它太脆。
 */
export function tryGetDatabase(): Database.Database | null {
  return db;
}

/**
 * Close database connection
 */
export function closeDatabase(): void {
  const databaseToClose = db;
  const dbPath = openDatabasePath;
  db = null;
  openDatabasePath = null;
  try {
    databaseToClose?.close();
    // 关闭成功才留标记：close 抛错说明状态不明，下次照常全量校验
    if (dbPath) {
      writeCleanShutdownMarker(dbPath, lastIntegrityCheckAt);
    }
  } finally {
    dbClientLease?.release();
    dbClientLease = null;
  }
}

export { db };
