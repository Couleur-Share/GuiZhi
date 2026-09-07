import { sanitizeSnapshot } from "./web-capture/snapshot-sanitize";
import fs from "node:fs";
import path from "node:path";
import { randomUUID } from "node:crypto";
import Database from "../database/sqlite";
import { getSchemaVersion, SCHEMA_VERSION } from "@guizhi/db";
import type { BackupRepository } from "./backup-repository";

const CONFIG_FILES = new Set([
  "ai-models.json",
  "illustration-styles.json",
  "shortcuts.json",
  "shortcut-mode.json",
  "mcp.json",
]);

const MACHINE_BOUND_RENDERER_KEYS = new Set([
  "dataPath",
  "launchAtStartup",
  "minimizeOnLaunch",
  "closeAction",
  "backgroundImageFileName",
  "ytDlpPath",
  "ffmpegPath",
  "backgroundTasksEnabled",
]);

const MACHINE_BOUND_DB_KEYS = [
  "launchAtStartup",
  "minimizeOnLaunch",
  "backgroundImageFileName",
  "ytDlpPath",
  "ffmpegPath",
  "backgroundTasksEnabled",
] as const;

export interface PreparedRepositoryRestore {
  stageDir: string;
  databasePath: string;
  imagesDir: string;
  videosDir: string;
  configDir: string;
  pendingRendererSettingsPath: string | null;
}

export interface RepositoryRestoreTargets {
  databasePath: string;
  imagesDir: string;
  videosDir: string;
  configDir: string;
}

function assertSafeAssetPath(logicalPath: string): void {
  if (
    !/^data\/assets\/(images|videos)\/[A-Za-z0-9_.-]+$/.test(logicalPath) ||
    logicalPath.includes("/../")
  ) {
    throw new Error(`快照包含不安全的媒体路径: ${logicalPath}`);
  }
}

function mergeRendererSettings(
  restored: Record<string, unknown>,
  current: Record<string, unknown> | undefined,
): Record<string, unknown> {
  const merged = { ...restored };
  for (const key of MACHINE_BOUND_RENDERER_KEYS) {
    if (current && Object.prototype.hasOwnProperty.call(current, key)) {
      merged[key] = current[key];
    } else {
      delete merged[key];
    }
  }
  return merged;
}

function copyMachineSettings(
  liveDb: Database.Database,
  stagedDatabasePath: string,
): void {
  const staged = new Database(stagedDatabasePath);
  try {
    for (const key of MACHINE_BOUND_DB_KEYS) {
      const row = liveDb.get("SELECT value FROM settings WHERE key = ?", key) as
        | { value: string }
        | undefined;
      if (row) {
        staged.run(
          "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
          key,
          row.value,
        );
      } else {
        staged.run("DELETE FROM settings WHERE key = ?", key);
      }
    }
    if (staged.get("SELECT name FROM sqlite_master WHERE type='table' AND name='web_source_versions'")) {
      for (const row of staged.all("SELECT payload FROM web_source_versions") as {payload:string}[]) {
        const version = JSON.parse(row.payload);
        if (version.snapshot) sanitizeSnapshot(version.snapshot);
      }
    }
    const check = staged.pragma("quick_check") as Array<Record<string, unknown>>;
    if (check.length !== 1 || Object.values(check[0])[0] !== "ok") {
      throw new Error("恢复数据库 quick_check 未通过");
    }
    const version = getSchemaVersion(staged);
    if (version > SCHEMA_VERSION) {
      throw new Error(`快照数据结构 v${version} 高于当前支持的 v${SCHEMA_VERSION}`);
    }
  } finally {
    staged.close();
  }
}

/** 全量解密到同一 userData 文件系统上的暂存目录，并再次校验数据库。 */
export function prepareRepositoryRestore(options: {
  repository: BackupRepository;
  snapshotId: string;
  recoveryPassword?: string;
  liveDb: Database.Database;
  targets: RepositoryRestoreTargets;
  currentRendererSettings?: Record<string, unknown>;
}): PreparedRepositoryRestore {
  const { repository, snapshotId, recoveryPassword, liveDb, targets } = options;
  const userDataDir = path.dirname(path.dirname(targets.databasePath));
  const stageDir = fs.mkdtempSync(path.join(userDataDir, ".restore-stage-"));
  const stagedDatabasePath = path.join(stageDir, "data", "knowledge.db");
  const stagedImagesDir = path.join(stageDir, "data", "assets", "images");
  const stagedVideosDir = path.join(stageDir, "data", "assets", "videos");
  const stagedConfigDir = path.join(stageDir, "config");
  let pendingRendererSettingsPath: string | null = null;
  try {
    const manifest = repository.readManifest(snapshotId, recoveryPassword);
    for (const entry of manifest.entries) {
      const plain = repository.readEntry(entry);
      let destination: string;
      if (entry.category === "database") {
        if (entry.logicalPath !== "data/knowledge.db") {
          throw new Error("数据库快照路径不合法");
        }
        destination = stagedDatabasePath;
      } else if (entry.category === "media") {
        assertSafeAssetPath(entry.logicalPath);
        destination = path.join(stageDir, ...entry.logicalPath.split("/"));
      } else if (entry.category === "config") {
        const fileName = path.posix.basename(entry.logicalPath);
        if (!CONFIG_FILES.has(fileName) || entry.logicalPath !== `config/${fileName}`) {
          throw new Error(`配置快照路径不合法: ${entry.logicalPath}`);
        }
        destination = path.join(stagedConfigDir, fileName);
      } else {
        const restored = JSON.parse(plain.toString("utf8")) as Record<string, unknown>;
        const merged = mergeRendererSettings(
          restored,
          options.currentRendererSettings,
        );
        pendingRendererSettingsPath = path.join(
          stagedConfigDir,
          "pending-renderer-settings.json",
        );
        fs.mkdirSync(path.dirname(pendingRendererSettingsPath), {
          recursive: true,
        });
        fs.writeFileSync(
          pendingRendererSettingsPath,
          JSON.stringify(merged),
          "utf8",
        );
        continue;
      }
      fs.mkdirSync(path.dirname(destination), { recursive: true });
      fs.writeFileSync(destination, plain);
    }
    if (!fs.existsSync(stagedDatabasePath)) {
      throw new Error("快照缺少数据库");
    }
    fs.mkdirSync(stagedImagesDir, { recursive: true });
    fs.mkdirSync(stagedVideosDir, { recursive: true });
    fs.mkdirSync(stagedConfigDir, { recursive: true });
    copyMachineSettings(liveDb, stagedDatabasePath);
    return {
      stageDir,
      databasePath: stagedDatabasePath,
      imagesDir: stagedImagesDir,
      videosDir: stagedVideosDir,
      configDir: stagedConfigDir,
      pendingRendererSettingsPath,
    };
  } catch (error) {
    fs.rmSync(stageDir, { recursive: true, force: true });
    throw error;
  }
}

interface MoveRecord {
  target: string;
  rollback: string | null;
}

function replacePath(
  incoming: string,
  target: string,
  rollbackDir: string,
  records: MoveRecord[],
): void {
  fs.mkdirSync(path.dirname(target), { recursive: true });
  let rollback: string | null = null;
  if (fs.existsSync(target)) {
    rollback = path.join(rollbackDir, `${records.length}-${path.basename(target)}`);
    fs.renameSync(target, rollback);
  }
  try {
    fs.renameSync(incoming, target);
    records.push({ target, rollback });
  } catch (error) {
    if (rollback && fs.existsSync(rollback)) fs.renameSync(rollback, target);
    throw error;
  }
}

/**
 * 数据库已关闭后执行同盘交换；任一步失败都逆序恢复已移动的旧路径。
 * pre-restore 数据库快照由调用方在关闭数据库前另行创建。
 */
export function applyPreparedRepositoryRestore(
  prepared: PreparedRepositoryRestore,
  targets: RepositoryRestoreTargets,
): void {
  const userDataDir = path.dirname(path.dirname(targets.databasePath));
  const rollbackDir = path.join(userDataDir, `.restore-rollback-${randomUUID()}`);
  fs.mkdirSync(rollbackDir, { recursive: true });
  const records: MoveRecord[] = [];
  try {
    replacePath(prepared.databasePath, targets.databasePath, rollbackDir, records);
    replacePath(prepared.imagesDir, targets.imagesDir, rollbackDir, records);
    replacePath(prepared.videosDir, targets.videosDir, rollbackDir, records);

    for (const fileName of CONFIG_FILES) {
      const incoming = path.join(prepared.configDir, fileName);
      if (fs.existsSync(incoming)) {
        replacePath(
          incoming,
          path.join(targets.configDir, fileName),
          rollbackDir,
          records,
        );
      }
    }
    const pending = path.join(prepared.configDir, "pending-renderer-settings.json");
    if (fs.existsSync(pending)) {
      replacePath(
        pending,
        path.join(targets.configDir, "pending-renderer-settings.json"),
        rollbackDir,
        records,
      );
    }
    for (const suffix of ["-wal", "-shm", "-journal", ".clean"]) {
      fs.rmSync(`${targets.databasePath}${suffix}`, { force: true });
    }
  } catch (error) {
    for (const record of records.reverse()) {
      fs.rmSync(record.target, { recursive: true, force: true });
      if (record.rollback && fs.existsSync(record.rollback)) {
        fs.renameSync(record.rollback, record.target);
      }
    }
    throw error;
  } finally {
    fs.rmSync(prepared.stageDir, { recursive: true, force: true });
    fs.rmSync(rollbackDir, { recursive: true, force: true });
  }
}
