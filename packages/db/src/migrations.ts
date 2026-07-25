/**
 * Schema 迁移执行器。
 *
 * 新表靠 `CREATE TABLE IF NOT EXISTS` 就能覆盖新老库，但**列级变更不行**：
 * 给既有表加一列时，`IF NOT EXISTS` 会整条跳过，新列只对全新安装生效，
 * 老用户升级后一读新列就 `no such column`。这里补上真正的执行器。
 *
 * 约定：
 * - 每条迁移必须幂等。老库可能已经有目标列（历史上直接改过 schema.ts），
 *   所以统一用 `addColumnIfMissing` 之类的守卫，而不是裸 ALTER。
 * - 迁移只做结构演进与数据修复，不做业务逻辑。
 * - 追加迁移只能加在数组末尾，`name` 一旦发布不可更改。
 */
import type Database from "./adapter";

export interface Migration {
  /** 唯一名称，写入 schema_migrations 后不可更改 */
  name: string;
  up: (db: Database.Database) => void;
}

interface ColumnRow {
  name: string;
}

/** 表是否已有该列（迁移幂等的基础） */
export function hasColumn(
  db: Database.Database,
  table: string,
  column: string,
): boolean {
  const rows = db.all(`PRAGMA table_info(${table})`) as ColumnRow[];
  return rows.some((row) => row.name === column);
}

/** 缺列才加；已存在时静默跳过 */
export function addColumnIfMissing(
  db: Database.Database,
  table: string,
  column: string,
  definition: string,
): void {
  if (hasColumn(db, table, column)) {
    return;
  }
  db.run(`ALTER TABLE ${table} ADD COLUMN ${column} ${definition}`);
}

/**
 * 迁移清单。
 *
 * 目前只有一条基线：v0.4.1 之前没有执行器，所有结构都由 schema.ts 的
 * CREATE 语句给出，这条迁移只是把版本戳落到位，让后续迁移有起点。
 */
export const MIGRATIONS: Migration[] = [
  {
    name: "0001-baseline",
    up: () => {
      // schema.ts 的 CREATE TABLE IF NOT EXISTS 已经建好全部结构
    },
  },
  {
    // Wiki 编译失败退避：老库的 wiki_ingestions 没有这两列
    name: "0002-wiki-ingestion-backoff",
    up: (db) => {
      addColumnIfMissing(
        db,
        "wiki_ingestions",
        "failure_count",
        "INTEGER NOT NULL DEFAULT 0",
      );
      addColumnIfMissing(db, "wiki_ingestions", "next_attempt_at", "INTEGER");
    },
  },
];

/** 当前代码期望的 schema 版本（= 迁移条数），写入 PRAGMA user_version */
export const SCHEMA_VERSION = MIGRATIONS.length;

export function getSchemaVersion(db: Database.Database): number {
  const row = db.get("PRAGMA user_version") as
    | { user_version?: number }
    | undefined;
  return row?.user_version ?? 0;
}

/**
 * 执行所有未应用的迁移。
 *
 * 每条迁移单独一个事务：一条失败时前面成功的保持已应用状态，
 * 重启后从失败那条继续，而不是整批回滚后反复重跑。
 */
export function runMigrations(db: Database.Database): string[] {
  db.exec(`
    CREATE TABLE IF NOT EXISTS schema_migrations (
      name TEXT PRIMARY KEY,
      applied_at INTEGER NOT NULL
    )
  `);

  const appliedRows = db.all(
    "SELECT name FROM schema_migrations",
  ) as { name: string }[];
  const applied = new Set(appliedRows.map((row) => row.name));

  const executed: string[] = [];
  for (const migration of MIGRATIONS) {
    if (applied.has(migration.name)) {
      continue;
    }
    const run = db.transaction(() => {
      migration.up(db);
      db.run(
        "INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)",
        migration.name,
        Date.now(),
      );
    });
    run();
    executed.push(migration.name);
  }

  // user_version 是备份/恢复做版本比对的依据（PRAGMA 不接受参数绑定）
  db.exec(`PRAGMA user_version = ${SCHEMA_VERSION}`);
  return executed;
}
