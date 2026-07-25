import { app, ipcMain } from "electron";
import fs from "fs";
import os from "os";
import path from "path";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import {
  countLegacyItems,
  isTargetDatabaseEmpty,
  migrateLegacyDatabase,
} from "@guizhi/db";
import Database from "../database/sqlite";

/** 旧版 .NET 归知与新版共用 %APPDATA%\GuiZhi 目录，旧库固定在 data\guizhi.db。 */
function getDefaultLegacyDbPath(): string {
  return path.join(app.getPath("userData"), "data", "guizhi.db");
}

/**
 * 在旧库的临时拷贝上执行操作（绝不触碰原文件）。
 *
 * 旧库是 WAL 日志模式，而 node-sqlite3-wasm（Emscripten 构建）不支持 WAL：
 * 拷贝后把文件头 bytes 18/19（read/write format version）从 2 降为 1
 * 即可按回滚日志模式打开。前提是没有未合并的 -wal 文件——
 * 旧版应用正常退出时会 checkpoint，若发现残留 -wal 则拒绝迁移并提示。
 */
function withLegacyCopy<T>(
  sourcePath: string,
  callback: (legacy: InstanceType<typeof Database>) => T,
): T {
  const walPath = `${sourcePath}-wal`;
  if (fs.existsSync(walPath) && fs.statSync(walPath).size > 0) {
    throw new Error(
      "旧版数据库有未合并的写入日志。请先打开一次旧版归知并正常退出，再重试迁移。",
    );
  }

  const tempCopy = path.join(
    os.tmpdir(),
    `guizhi-legacy-migration-${Date.now()}.db`,
  );
  fs.copyFileSync(sourcePath, tempCopy);
  try {
    const fd = fs.openSync(tempCopy, "r+");
    try {
      const versionBytes = Buffer.alloc(2);
      fs.readSync(fd, versionBytes, 0, 2, 18);
      if (versionBytes[0] === 2 || versionBytes[1] === 2) {
        fs.writeSync(fd, Buffer.from([1, 1]), 0, 2, 18);
      }
    } finally {
      fs.closeSync(fd);
    }

    const legacy = new Database(tempCopy);
    try {
      return callback(legacy);
    } finally {
      legacy.close();
    }
  } finally {
    fs.rmSync(tempCopy, { force: true });
  }
}

export function registerMigrationIPC(db: Database.Database): void {
  ipcMain.handle(IPC_CHANNELS.MIGRATION_DETECT_LEGACY, () => {
    const legacyPath = getDefaultLegacyDbPath();
    if (!fs.existsSync(legacyPath) || !isTargetDatabaseEmpty(db)) {
      return null;
    }
    try {
      const itemCount = withLegacyCopy(legacyPath, countLegacyItems);
      return itemCount > 0 ? { path: legacyPath, itemCount } : null;
    } catch (error) {
      console.error("旧版数据库探测失败:", error);
      return null;
    }
  });

  ipcMain.handle(
    IPC_CHANNELS.MIGRATION_RUN_LEGACY,
    (_event, sourcePath?: string) => {
      const legacyPath = sourcePath || getDefaultLegacyDbPath();
      if (!fs.existsSync(legacyPath)) {
        throw new Error(`找不到旧版数据库文件：${legacyPath}`);
      }
      const stats = withLegacyCopy(legacyPath, (legacy) =>
        migrateLegacyDatabase(db, legacy),
      );
      console.log("旧版数据迁移完成:", JSON.stringify(stats));
      return stats;
    },
  );
}
