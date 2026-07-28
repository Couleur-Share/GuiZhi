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
import { resolveSourcePlatform } from "@guizhi/shared/utils/source-platforms";
import type Database from "./adapter";

export interface Migration {
  /** 唯一名称，写入 schema_migrations 后不可更改 */
  name: string;
  /**
   * 执行期间关闭外键强制（重建表类迁移必须开启）。
   *
   * SQLite 改不了已有的 CHECK / 外键约束，只能「建新表 → 拷数据 → 删旧表 →
   * 改名」。但开着 `foreign_keys` 时 `DROP TABLE` 会先隐式 DELETE 一遍，
   * 把所有 ON DELETE CASCADE 的子表数据（标签关联、来源记录、Wiki 关联、
   * 语义向量）一并清空。而 `PRAGMA foreign_keys` 在事务内是空操作，
   * 必须由执行器在 BEGIN 之前关掉、COMMIT 之后恢复。
   *
   * 开启后本执行器会在提交前跑 `PRAGMA foreign_key_check`，
   * 留下孤儿行就整条回滚。
   */
  foreignKeysOff?: boolean;
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

/**
 * 缺列才加；已存在时静默跳过。
 *
 * 表本身不存在也跳过：迁移在 SCHEMA_TABLES 之后执行，正常库里表一定在；
 * 只建了半个库的单测不该因此炸掉整条迁移链。
 */
export function addColumnIfMissing(
  db: Database.Database,
  table: string,
  column: string,
  definition: string,
): void {
  const rows = db.all(`PRAGMA table_info(${table})`) as ColumnRow[];
  if (rows.length === 0 || rows.some((row) => row.name === column)) {
    return;
  }
  db.run(`ALTER TABLE ${table} ADD COLUMN ${column} ${definition}`);
}

/** 建表语句原文；表不存在时为空串。重建表类迁移靠它判断是否已经做过 */
export function getTableDefinition(
  db: Database.Database,
  table: string,
): string {
  const row = db.get(
    "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?",
    table,
  ) as { sql?: string } | undefined;
  return row?.sql ?? "";
}

/**
 * knowledge_items 的完整定义。重建表时新旧结构必须逐字一致（除 CHECK 之外），
 * 所以这里与 schema.ts 的建表语句共用同一份列清单。
 */
const KNOWLEDGE_ITEMS_COLUMNS = [
  "id",
  "title",
  "content",
  "summary",
  "transcript",
  "item_type",
  "status",
  "collection_id",
  "is_favorite",
  "is_pinned",
  "deleted_at",
  "created_at",
  "updated_at",
] as const;

/** DROP TABLE 会连带删掉表上的索引，重建后要一并补回 */
const KNOWLEDGE_ITEMS_INDEXES = [
  "CREATE INDEX IF NOT EXISTS idx_items_status ON knowledge_items(status)",
  "CREATE INDEX IF NOT EXISTS idx_items_collection ON knowledge_items(collection_id)",
  "CREATE INDEX IF NOT EXISTS idx_items_updated ON knowledge_items(updated_at DESC)",
  "CREATE INDEX IF NOT EXISTS idx_items_deleted ON knowledge_items(deleted_at)",
  "CREATE INDEX IF NOT EXISTS idx_items_favorite ON knowledge_items(is_favorite)",
] as const;

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
  {
    // 论坛帖子采集：item_type 的 CHECK 要放行 'forum'。
    // 老库的 CHECK 写死了七种类型，不重建表的话论坛条目一入库就被约束打回。
    name: "0003-item-type-forum",
    foreignKeysOff: true,
    up: (db) => {
      const definition = getTableDefinition(db, "knowledge_items");
      // 表不存在（单测建了半个库）或已放行则跳过，保持幂等
      if (!definition || definition.includes("'forum'")) {
        return;
      }

      const columns = KNOWLEDGE_ITEMS_COLUMNS.join(", ");
      db.exec(`
        CREATE TABLE knowledge_items_migrate (
          id TEXT PRIMARY KEY,
          title TEXT NOT NULL DEFAULT '',
          content TEXT NOT NULL DEFAULT '',
          summary TEXT,
          transcript TEXT,
          item_type TEXT NOT NULL DEFAULT 'note'
            CHECK(item_type IN ('note','webpage','video','image','audio','document','snippet','forum')),
          status TEXT NOT NULL DEFAULT 'inbox'
            CHECK(status IN ('inbox','ready','archived')),
          collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL,
          is_favorite INTEGER NOT NULL DEFAULT 0,
          is_pinned INTEGER NOT NULL DEFAULT 0,
          deleted_at INTEGER,
          created_at INTEGER NOT NULL,
          updated_at INTEGER NOT NULL
        );
        INSERT INTO knowledge_items_migrate (${columns})
          SELECT ${columns} FROM knowledge_items;
        DROP TABLE knowledge_items;
        ALTER TABLE knowledge_items_migrate RENAME TO knowledge_items;
        ${KNOWLEDGE_ITEMS_INDEXES.join(";\n        ")};
      `);
    },
  },
  {
    // 采集时选定的标签要随任务持久化，重启恢复后仍能打到条目上
    name: "0004-import-task-tags",
    up: (db) => {
      addColumnIfMissing(db, "import_tasks", "tag_names", "TEXT");
    },
  },
  {
    // 失败调用也计入用量：超时与限流同样产生费用
    name: "0005-ai-usage-failed-calls",
    up: (db) => {
      addColumnIfMissing(
        db,
        "ai_usage_daily",
        "failed_calls",
        "INTEGER NOT NULL DEFAULT 0",
      );
    },
  },
  {
    // Wiki 页面可手动编辑：标记过的页面下一轮编译不再覆盖正文
    name: "0006-wiki-manual-edit",
    up: (db) => {
      addColumnIfMissing(db, "wiki_pages", "manual_edited_at", "INTEGER");
    },
  },
  {
    // 导入列表按条目类型显示图标：抽取成功后回写，老库缺这一列
    name: "0007-import-task-item-type",
    up: (db) => {
      addColumnIfMissing(db, "import_tasks", "item_type", "TEXT");
    },
  },
  {
    // 条目状态三态压成两态：inbox / ready 一并并入 active。
    //
    // inbox 从来没有 gate 过任何东西（问答检索、Wiki 编译、语义索引都不看
    // status），也没有任何自动化会把它推进到 ready，所以它只是一个需要人
    // 手动维护、维护了也不产生任何效果的状态。待整理的信号改用
    // 「collection_id IS NULL」表达，见 KnowledgeItemDB.list 的 uncategorized 分支。
    name: "0008-item-status-two-state",
    foreignKeysOff: true,
    up: (db) => {
      const definition = getTableDefinition(db, "knowledge_items");
      // 表不存在（单测建了半个库）或已是两态则跳过，保持幂等
      if (!definition || definition.includes("'active'")) {
        return;
      }

      const columns = KNOWLEDGE_ITEMS_COLUMNS.join(", ");
      // 旧 CHECK 不放行 'active'，没法先 UPDATE 再重建，只能在拷贝时就地折叠
      const selected = KNOWLEDGE_ITEMS_COLUMNS.map((column) =>
        column === "status"
          ? "CASE WHEN status = 'archived' THEN 'archived' ELSE 'active' END"
          : column,
      ).join(", ");
      db.exec(`
        CREATE TABLE knowledge_items_migrate (
          id TEXT PRIMARY KEY,
          title TEXT NOT NULL DEFAULT '',
          content TEXT NOT NULL DEFAULT '',
          summary TEXT,
          transcript TEXT,
          item_type TEXT NOT NULL DEFAULT 'note'
            CHECK(item_type IN ('note','webpage','video','image','audio','document','snippet','forum')),
          status TEXT NOT NULL DEFAULT 'active'
            CHECK(status IN ('active','archived')),
          collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL,
          is_favorite INTEGER NOT NULL DEFAULT 0,
          is_pinned INTEGER NOT NULL DEFAULT 0,
          deleted_at INTEGER,
          created_at INTEGER NOT NULL,
          updated_at INTEGER NOT NULL
        );
        INSERT INTO knowledge_items_migrate (${columns})
          SELECT ${selected} FROM knowledge_items;
        DROP TABLE knowledge_items;
        ALTER TABLE knowledge_items_migrate RENAME TO knowledge_items;
        ${KNOWLEDGE_ITEMS_INDEXES.join(";\n        ")};
      `);
    },
  },
  {
    // 侧栏「平台」分区：把 source_records.platform 补齐。
    //
    // 这一列建表时就在，但采集管线的 INSERT 从来没写过它，全库皆为 NULL；
    // 唯一写过的是旧版 .NET 迁移，落进去的是老应用自己的一套取值。因此这里
    // 不是「只补 NULL」而是**全部重算**：留着老取值会在分区里多出几个用户
    // 认不出来的分组，而这一列此前没有任何读取方，重算不会弄丢任何在用的数据。
    name: "0009-source-platform",
    up: (db) => {
      // 表不存在（只建了半个库的单测）就跳过，与 addColumnIfMissing 同一约定
      if (!getTableDefinition(db, "source_records")) {
        return;
      }
      db.exec(
        "CREATE INDEX IF NOT EXISTS idx_sources_platform ON source_records(platform)",
      );

      const rows = db.all(
        "SELECT id, source_type, source_uri FROM source_records",
      ) as Array<{
        id: string;
        source_type: string;
        source_uri: string | null;
      }>;
      for (const row of rows) {
        db.run(
          "UPDATE source_records SET platform = ? WHERE id = ?",
          resolveSourcePlatform(row.source_type, row.source_uri),
          row.id,
        );
      }
    },
  },
  {
    // 已完成但有缺失的任务要在列表上说出来（转写失败仍会入库并标 completed）
    name: "0010-import-task-warning",
    up: (db) => {
      addColumnIfMissing(db, "import_tasks", "warning", "TEXT");
    },
  },
  {
    // 各阶段耗时与 AI 开销：终态任务此前不留任何耗时痕迹，
    // 「排版慢了八分钟」这类异常只能靠翻数据库发现
    name: "0011-import-task-stage-stats",
    up: (db) => {
      addColumnIfMissing(db, "import_tasks", "stage_stats", "TEXT");
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

interface ForeignKeyViolation {
  table?: string;
  rowid?: number;
}

/** 重建表后校验没有留下悬空引用；有就让整条迁移回滚 */
function assertNoForeignKeyViolations(db: Database.Database): void {
  const violations = db.pragma("foreign_key_check") as ForeignKeyViolation[];
  if (!Array.isArray(violations) || violations.length === 0) {
    return;
  }
  const tables = [...new Set(violations.map((row) => row.table ?? "?"))];
  throw new Error(
    `迁移后存在外键孤儿行（${violations.length} 行，涉及 ${tables.join(", ")}）`,
  );
}

/**
 * 执行单条迁移。
 *
 * foreignKeysOff 的迁移要在事务外先关掉外键强制——`PRAGMA foreign_keys`
 * 在事务内部是空操作，写在 up() 里不起任何作用。
 */
function applyMigration(db: Database.Database, migration: Migration): void {
  const run = db.transaction(() => {
    migration.up(db);
    if (migration.foreignKeysOff) {
      assertNoForeignKeyViolations(db);
    }
    db.run(
      "INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)",
      migration.name,
      Date.now(),
    );
  });

  if (!migration.foreignKeysOff) {
    run();
    return;
  }

  db.pragma("foreign_keys = OFF");
  try {
    run();
  } finally {
    db.pragma("foreign_keys = ON");
  }
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
    applyMigration(db, migration);
    executed.push(migration.name);
  }

  // user_version 是备份/恢复做版本比对的依据（PRAGMA 不接受参数绑定）
  db.exec(`PRAGMA user_version = ${SCHEMA_VERSION}`);
  return executed;
}
