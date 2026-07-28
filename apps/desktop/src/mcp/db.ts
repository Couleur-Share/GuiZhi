/**
 * MCP server 的数据库接入：只读打开归知的 knowledge.db。
 *
 * 这个进程与归知应用是两个独立进程，可能同时活着，所以有三条硬约束：
 *
 * 1. **必须用 node-sqlite3-wasm，不能换成 Node 24 自带的 node:sqlite。**
 *    wasm 那份的 VFS 把锁实现成 `mkdir <db>.lock` 目录，而原生 SQLite 用的是
 *    字节范围锁——两套机制互不认识。实测（2026-07）：归知的写事务尚未提交时，
 *    node:sqlite 3ms 就读到了那条未提交的行；以读写方式打开时它还会把归知正在
 *    写的 journal 当成崩溃残留的 hot journal 去回滚，直接摧毁对方的事务。
 * 2. **必须注册客户端租约。** 归知主进程带 `recoverUnregisteredLock: true`
 *    启动（它有 Electron 单实例门兜底），不在 `<db>.clients/` 下登记的话，
 *    归知启动时会把本进程正持有的 `.lock` 目录当成孤儿删掉。
 * 3. **只读打开。** 杜绝写坏用户数据，也不会触发 hot journal 回滚。
 *
 * 并发代价实测可接受：归知每 50ms 一条查询、本进程同时做一次 38ms 的重查询，
 * 归知侧最慢 18ms、中位 3ms、零报错。锁是语句级的，不是连接级的。
 */
import fs from "node:fs";
import path from "node:path";
import {
  acquireDatabaseClientLease,
  getSchemaVersion,
  SCHEMA_VERSION,
  type DatabaseClientLease,
} from "@guizhi/db";
// 从 adapter 模块本身取 default：barrel 的具名重导出丢掉了合并的命名空间，
// 拿不到 `DatabaseAdapter.Database` 这个实例类型
import DatabaseAdapter from "@guizhi/db/adapter";
import { getAppDataPath } from "@guizhi/core";
import {
  getHistoricalDefaultUserDataPath,
  resolveInitialUserDataPath,
} from "../main/data-path";

/**
 * 比归知主进程的 5000 更长。实测归知处于写事务时读者要退避等待约 3 秒，
 * 宁可多等也别把「对方正在写」报成失败。
 */
const BUSY_TIMEOUT_MS = 8000;

export interface KnowledgeDbHandle {
  db: DatabaseAdapter.Database;
  dbPath: string;
  close: () => void;
}

/**
 * 解析 knowledge.db 的绝对路径。
 *
 * 复用归知主进程那套四级优先级（data-path.ts），绝不在这里另写一份：
 * 算错的表现是「MCP 搜不到东西，但归知界面里明明有」，而两边各有一份实现时
 * 这种偏差只会越走越远。
 */
export function resolveKnowledgeDbPath(): string {
  const override = process.env.GUIZHI_DATA_DIR?.trim();
  if (override) {
    return path.join(path.resolve(override), "data", "knowledge.db");
  }

  const appDataPath = getAppDataPath();
  const userDataPath = resolveInitialUserDataPath({
    appDataPath,
    defaultUserDataPath: getHistoricalDefaultUserDataPath(
      appDataPath,
      process.platform,
    ),
    exePath: process.execPath,
    // 打包后本进程由 GuiZhi.exe 以 ELECTRON_RUN_AS_NODE 模式启动，
    // execPath 就是安装目录里的应用本体；开发时它在 node_modules 下。
    isPackaged: !process.execPath.includes("node_modules"),
    platform: process.platform,
  });
  return path.join(userDataPath, "data", "knowledge.db");
}

export function openKnowledgeDbReadOnly(): KnowledgeDbHandle {
  const dbPath = resolveKnowledgeDbPath();
  if (!fs.existsSync(dbPath)) {
    throw new Error(
      `找不到归知的知识库文件：${dbPath}\n` +
        "若归知把数据目录改到了别处，请给 MCP server 设置 GUIZHI_DATA_DIR 环境变量指向该目录（含 data 子目录的那一层）。",
    );
  }

  // 登记租约，让归知知道「这个 .lock 有人正拿着」。不能传
  // recoverUnregisteredLock——那个开关只有带单实例门的主进程配用。
  const lease: DatabaseClientLease = acquireDatabaseClientLease(dbPath);

  let db: DatabaseAdapter.Database;
  try {
    db = new DatabaseAdapter(dbPath, { readOnly: true });
    db.pragma(`busy_timeout = ${BUSY_TIMEOUT_MS}`);
  } catch (error) {
    lease.release();
    throw error;
  }

  // 库比本进程新时直接说清楚，别拿旧的表结构去查：那样报出来的是
  // 看不懂的 SQL 错误。备份校验用的是同一条判据。
  const version = getSchemaVersion(db);
  if (version > SCHEMA_VERSION) {
    db.close();
    lease.release();
    throw new Error(
      `知识库来自更新版本的归知（schema ${version} > ${SCHEMA_VERSION}）。请更新归知后重启 MCP server。`,
    );
  }

  let closed = false;
  return {
    db,
    dbPath,
    close: () => {
      if (closed) {
        return;
      }
      closed = true;
      try {
        db.close();
      } finally {
        lease.release();
      }
    },
  };
}
