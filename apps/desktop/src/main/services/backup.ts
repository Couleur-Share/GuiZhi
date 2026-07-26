/**
 * 本地备份服务。
 *
 * 底层是 node-sqlite3-wasm（无 better-sqlite3 的 backup API、无 WAL），
 * 在线一致性备份采用 SQL 标准的 `VACUUM INTO`：对打开中的库生成
 * 紧凑一致的单文件副本，落在 %userData%/backups 下。
 *
 * 恢复流程（backup.ipc.ts 编排）：校验备份文件 → 关闭数据库 →
 * 当前库先存 pre-restore 快照 → 文件覆盖 → 应用重启。
 */
import fs from "fs";
import path from "path";
import Database from "../database/sqlite";
import { getDatabase } from "../database";
import { getBackupsDir, getDatabasePath } from "../runtime-paths";
import { getSchemaVersion, SCHEMA_VERSION } from "@guizhi/db";
import type {
  BackupCreateResult,
  BackupFileInfo,
  BackupKind,
} from "@guizhi/shared/types";
import { BACKUP_KINDS } from "@guizhi/shared/types";

const BACKUP_FILE_PATTERN =
  /^knowledge-(manual|auto|pre-update|pre-restore)-(\d{8})-(\d{6})(?:-(\d+))?\.db$/;

/**
 * 机器生成的快照类备份各自保留几份。
 *
 * 每次更新留一份 pre-update、每次恢复留一份 pre-restore，而它们都是整库副本；
 * 不设上限的话，一年下来这些文件能占掉几十倍于知识库本身的磁盘。
 * manual 是用户显式创建的，列表里能看见也能删，不在这里动。
 */
const SNAPSHOT_KEEP_COUNT = 3;

/** 必须存在的核心表（恢复前校验备份文件确实是归知知识库） */
const REQUIRED_TABLES = ["settings", "knowledge_items", "collections", "tags"];

const AUTO_BACKUP_FIRST_CHECK_MS = 3 * 60 * 1000;
const AUTO_BACKUP_CHECK_INTERVAL_MS = 60 * 60 * 1000;
const LAST_AUTO_BACKUP_SETTING_KEY = "backupLastAutoAt";

function formatBackupTimestamp(date = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

function parseBackupKind(fileName: string): BackupKind | null {
  const match = BACKUP_FILE_PATTERN.exec(fileName);
  if (!match) {
    return null;
  }
  const kind = match[1] as BackupKind;
  return BACKUP_KINDS.includes(kind) ? kind : null;
}

/** 生成不冲突的备份文件名（同一秒内多次备份时追加序号） */
function resolveBackupFileName(backupsDir: string, kind: BackupKind): string {
  const base = `knowledge-${kind}-${formatBackupTimestamp()}`;
  let fileName = `${base}.db`;
  let counter = 1;
  while (fs.existsSync(path.join(backupsDir, fileName))) {
    fileName = `${base}-${counter}.db`;
    counter += 1;
  }
  return fileName;
}

/**
 * 从文件名解析创建时间。
 *
 * 不用 mtime：把备份目录整体复制或同步一次，mtime 会全部刷新且顺序打乱，
 * 而清理正是按这个顺序决定删谁的——那会删错文件。文件名里的时间戳才是可靠的。
 */
function parseBackupTimestamp(fileName: string): number | null {
  const match = BACKUP_FILE_PATTERN.exec(fileName);
  if (!match) {
    return null;
  }
  const [, , date, time, sequence] = match;
  const parsed = new Date(
    Number(date.slice(0, 4)),
    Number(date.slice(4, 6)) - 1,
    Number(date.slice(6, 8)),
    Number(time.slice(0, 2)),
    Number(time.slice(2, 4)),
    Number(time.slice(4, 6)),
  ).getTime();
  // 同一秒内的多份靠序号区分先后
  return Number.isNaN(parsed) ? null : parsed + Number(sequence ?? 0);
}

function toBackupFileInfo(
  backupsDir: string,
  fileName: string,
  kind: BackupKind,
): BackupFileInfo {
  const fullPath = path.join(backupsDir, fileName);
  const stat = fs.statSync(fullPath);
  return {
    fileName,
    path: fullPath,
    kind,
    sizeBytes: stat.size,
    createdAt: parseBackupTimestamp(fileName) ?? Math.round(stat.mtimeMs),
  };
}

/**
 * 对打开中的数据库执行 VACUUM INTO 生成备份文件。
 * 失败时抛错并清理半成品文件。
 */
export function createBackup(
  db: Database.Database,
  kind: BackupKind,
  backupsDir = getBackupsDir(),
): BackupFileInfo {
  fs.mkdirSync(backupsDir, { recursive: true });
  const fileName = resolveBackupFileName(backupsDir, kind);
  const fullPath = path.join(backupsDir, fileName);
  try {
    db.run("VACUUM INTO ?", fullPath);
  } catch (error) {
    fs.rmSync(fullPath, { force: true });
    throw error;
  }
  return toBackupFileInfo(backupsDir, fileName, kind);
}

/** 不抛错版本（升级前快照等“尽力而为”场景使用） */
export function createBackupSafe(kind: BackupKind): BackupCreateResult {
  try {
    const backup = createBackup(getDatabase(), kind);
    console.log(`[backup] 已创建 ${kind} 备份: ${backup.fileName}`);
    if (kind === "pre-update" || kind === "pre-restore") {
      pruneBackupsOfKind(kind, SNAPSHOT_KEEP_COUNT);
    }
    return { success: true, backup };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.warn(`[backup] 创建 ${kind} 备份失败:`, message);
    return { success: false, error: message };
  }
}

export function listBackups(backupsDir = getBackupsDir()): BackupFileInfo[] {
  if (!fs.existsSync(backupsDir)) {
    return [];
  }
  const backups: BackupFileInfo[] = [];
  for (const fileName of fs.readdirSync(backupsDir)) {
    const kind = parseBackupKind(fileName);
    if (!kind) {
      continue;
    }
    try {
      backups.push(toBackupFileInfo(backupsDir, fileName, kind));
    } catch {
      // 文件在扫描间隙被移除，跳过
    }
  }
  backups.sort((left, right) => right.createdAt - left.createdAt);
  return backups;
}

/** 仅允许删除备份目录内、命名符合规范的文件（防路径穿越） */
export function deleteBackup(
  fileName: string,
  backupsDir = getBackupsDir(),
): boolean {
  if (path.basename(fileName) !== fileName || !parseBackupKind(fileName)) {
    return false;
  }
  const fullPath = path.join(backupsDir, fileName);
  if (!fs.existsSync(fullPath)) {
    return false;
  }
  fs.rmSync(fullPath);
  return true;
}

/** 按类别保留最近 keepCount 份，其余删除；返回删除数量 */
export function pruneBackupsOfKind(
  kind: BackupKind,
  keepCount: number,
  backupsDir = getBackupsDir(),
): number {
  const excess = listBackups(backupsDir)
    .filter((backup) => backup.kind === kind)
    .slice(Math.max(1, keepCount));
  for (const backup of excess) {
    fs.rmSync(backup.path, { force: true });
  }
  return excess.length;
}

/** 自动备份只保留最近 keepCount 份；手动备份不受影响 */
export function pruneAutoBackups(
  keepCount: number,
  backupsDir = getBackupsDir(),
): number {
  return pruneBackupsOfKind("auto", keepCount, backupsDir);
}

/** 恢复前校验：能只读打开、quick_check 通过、含知识库核心表 */
export function validateBackupFile(filePath: string): {
  ok: boolean;
  error?: string;
} {
  if (!fs.existsSync(filePath) || fs.statSync(filePath).size === 0) {
    return { ok: false, error: "备份文件不存在或为空" };
  }
  let probe: Database.Database | null = null;
  try {
    probe = new Database(filePath, { readOnly: true });
    const diagnostics = probe.pragma("quick_check");
    const healthy =
      Array.isArray(diagnostics) &&
      diagnostics.length === 1 &&
      Object.values(diagnostics[0] as Record<string, unknown>)[0] === "ok";
    if (!healthy) {
      return { ok: false, error: "备份文件完整性校验未通过" };
    }
    for (const table of REQUIRED_TABLES) {
      const row = probe.get(
        "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?",
        table,
      );
      if (!row) {
        return { ok: false, error: `备份文件缺少数据表: ${table}` };
      }
    }

    // 来自更新版本的备份可能带着本版本读不懂的结构，恢复后只会更难排查。
    // 反向（旧备份）没问题：恢复后迁移执行器会把结构补齐。
    const backupVersion = getSchemaVersion(probe);
    if (backupVersion > SCHEMA_VERSION) {
      return {
        ok: false,
        error: `备份来自更新版本的归知（数据结构 v${backupVersion}，当前支持 v${SCHEMA_VERSION}），请先升级应用`,
      };
    }
    return { ok: true };
  } catch (error) {
    return {
      ok: false,
      error: `备份文件无法读取: ${error instanceof Error ? error.message : String(error)}`,
    };
  } finally {
    try {
      probe?.close();
    } catch {
      // 只读探针关闭失败不影响校验结论
    }
  }
}

/**
 * 恢复的文件交换步骤（调用方必须先 closeDatabase）：
 * 1. 当前库存为 pre-restore 快照；2. 备份换上主库；3. 清理旁车文件。
 *
 * 第 2 步先拷到同目录的 `.incoming` 再原子改名。直接覆盖主库的话，拷贝中途
 * 失败（磁盘满、杀软占用）会把主库截成半个文件，重启时 quick_check 不通过、
 * 应用直接起不来——而恢复入口在应用里，用户没有自救路径。
 */
export function performRestoreSwap(options: {
  databasePath: string;
  backupFilePath: string;
  backupsDir: string;
}): { preRestoreFileName: string | null } {
  const { databasePath, backupFilePath, backupsDir } = options;

  let preRestoreFileName: string | null = null;
  let preRestorePath: string | null = null;
  if (fs.existsSync(databasePath)) {
    fs.mkdirSync(backupsDir, { recursive: true });
    preRestoreFileName = resolveBackupFileName(backupsDir, "pre-restore");
    preRestorePath = path.join(backupsDir, preRestoreFileName);
    fs.copyFileSync(databasePath, preRestorePath);
    pruneBackupsOfKind("pre-restore", SNAPSHOT_KEEP_COUNT, backupsDir);
  }

  const incomingPath = `${databasePath}.incoming`;
  try {
    fs.rmSync(incomingPath, { force: true });
    fs.copyFileSync(backupFilePath, incomingPath);
    fs.renameSync(incomingPath, databasePath);
  } catch (error) {
    fs.rmSync(incomingPath, { force: true });
    // 无论失败在改名前还是改名中，都用刚存的快照兜一次底，
    // 保证留在原地的始终是一个能打开的库
    if (preRestorePath && fs.existsSync(preRestorePath)) {
      try {
        fs.copyFileSync(preRestorePath, databasePath);
      } catch (rollbackError) {
        console.error("[backup] 恢复失败后回滚主库也失败:", rollbackError);
      }
    }
    throw error;
  }

  for (const suffix of ["-wal", "-shm", "-journal"]) {
    fs.rmSync(`${databasePath}${suffix}`, { force: true });
  }
  // 干净关闭标记是给换下去的那个库开的，留着会让恢复出来的库跳过完整性校验
  fs.rmSync(`${databasePath}.clean`, { force: true });
  return { preRestoreFileName };
}

/** 有导入任务在跑时禁止恢复（避免换库瞬间的写入冲突） */
export function countActiveImportTasks(db: Database.Database): number {
  const row = db.get(
    "SELECT COUNT(*) AS count FROM import_tasks WHERE status IN ('pending', 'processing')",
  ) as { count: number } | undefined;
  return row?.count ?? 0;
}

// ── 自动备份调度 ────────────────────────────────────────────────────────────

interface AutoBackupSettings {
  enabled: boolean;
  intervalHours: number;
  keepCount: number;
}

function readSettingValue(db: Database.Database, key: string): unknown {
  const row = db.get("SELECT value FROM settings WHERE key = ?", key) as
    | { value: string }
    | undefined;
  if (!row) {
    return undefined;
  }
  try {
    return JSON.parse(row.value);
  } catch {
    return row.value;
  }
}

function clampNumber(value: unknown, fallback: number, min: number, max: number): number {
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(parsed)) {
    return fallback;
  }
  return Math.min(Math.max(Math.round(parsed), min), max);
}

export function readAutoBackupSettings(
  db: Database.Database,
): AutoBackupSettings {
  return {
    enabled: readSettingValue(db, "backupAutoEnabled") !== false,
    intervalHours: clampNumber(
      readSettingValue(db, "backupIntervalHours"),
      24,
      1,
      168,
    ),
    keepCount: clampNumber(readSettingValue(db, "backupKeepCount"), 10, 1, 100),
  };
}

function writeLastAutoBackupAt(db: Database.Database, timestamp: number): void {
  db.run(
    "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
    LAST_AUTO_BACKUP_SETTING_KEY,
    JSON.stringify(timestamp),
  );
}

/**
 * 自动备份的开始 / 结束通知。
 *
 * VACUUM INTO 是同步执行在主进程主线程上的，库越大冻得越久，而定时器是
 * 静默触发的——用户正在打字时界面会毫无征兆地卡住若干秒。把 SQLite 搬到
 * 独立线程是另一码事（node-sqlite3-wasm 是同步 WASM），在那之前至少让这段
 * 停顿有个解释。
 */
export type AutoBackupNotifier = (phase: "start" | "done" | "failed") => void;

let autoBackupNotifier: AutoBackupNotifier | null = null;

export function setAutoBackupNotifier(notifier: AutoBackupNotifier | null): void {
  autoBackupNotifier = notifier;
}

/** 到期则执行一轮自动备份 + 清理；返回是否实际执行了备份 */
export function maybeRunAutoBackup(
  db: Database.Database,
  backupsDir = getBackupsDir(),
): boolean {
  const settings = readAutoBackupSettings(db);
  if (!settings.enabled) {
    return false;
  }
  const lastAt = clampNumber(
    readSettingValue(db, LAST_AUTO_BACKUP_SETTING_KEY),
    0,
    0,
    Number.MAX_SAFE_INTEGER,
  );
  const dueAt = lastAt + settings.intervalHours * 60 * 60 * 1000;
  if (Date.now() < dueAt) {
    return false;
  }
  // 通知要赶在 VACUUM 之前发出去，否则渲染进程要等主线程空下来才收得到
  autoBackupNotifier?.("start");
  try {
    const backup = createBackup(db, "auto", backupsDir);
    writeLastAutoBackupAt(db, Date.now());
    const pruned = pruneAutoBackups(settings.keepCount, backupsDir);
    console.log(
      `[backup] 自动备份完成: ${backup.fileName}（清理 ${pruned} 份过期自动备份）`,
    );
    autoBackupNotifier?.("done");
    return true;
  } catch (error) {
    console.warn("[backup] 自动备份失败:", error);
    autoBackupNotifier?.("failed");
    return false;
  }
}

/** 启动自动备份调度：启动后延迟首查，其后每小时检查一次到期情况 */
export function startBackupScheduler(db: Database.Database): () => void {
  const firstTimer = setTimeout(() => {
    maybeRunAutoBackup(db);
  }, AUTO_BACKUP_FIRST_CHECK_MS);
  const intervalTimer = setInterval(() => {
    maybeRunAutoBackup(db);
  }, AUTO_BACKUP_CHECK_INTERVAL_MS);

  return () => {
    clearTimeout(firstTimer);
    clearInterval(intervalTimer);
  };
}

export function getDefaultRestorePaths(): {
  databasePath: string;
  backupsDir: string;
} {
  return { databasePath: getDatabasePath(), backupsDir: getBackupsDir() };
}
