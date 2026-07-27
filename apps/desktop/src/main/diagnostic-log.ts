import fs from "fs";
import os from "os";
import path from "path";
import { app } from "electron";

import { getLogsDir } from "./runtime-paths";

/**
 * 诊断日志：启动事件与业务失败。
 *
 * 两个文件，都在 `<userData>/logs/` 下，每行一条 JSON：
 * - `startup.log`：启动阶段与更新检查事件。独立于主进程控制台、跨重启持久化，
 *   便于从用户反馈的日志里诊断问题（如 v0.5.2 Windows 无限重启）。
 * - `error.log`：业务失败。后台自动执行的任务（定时备份、后台 Wiki 编译）
 *   失败时不该弹窗打扰，但也不能什么都不留——「自动备份失败，详见日志」
 *   这句话此前指向一个根本不存在的东西。设置页的「打开日志」通向这里。
 *
 * 日志轮转：超过 512KB 时截断最早一半，保持文件大小可控。
 */

const MAX_LOG_SIZE_BYTES = 512 * 1024;
const STARTUP_LOG_FILE = "startup.log";
const ERROR_LOG_FILE = "error.log";

function getLogFilePath(fileName: string): string {
  return path.join(getLogsDir(), fileName);
}

function ensureLogDir(filePath: string): void {
  const dir = path.dirname(filePath);
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
}

function rotateIfTooLarge(filePath: string): void {
  try {
    if (!fs.existsSync(filePath)) return;
    const stat = fs.statSync(filePath);
    if (stat.size <= MAX_LOG_SIZE_BYTES) return;
    // Keep only the latter half of the file to preserve recent context
    // 仅保留文件后半部分，保留最近的上下文
    const buf = fs.readFileSync(filePath);
    const halfStart = Math.floor(buf.length / 2);
    // Align to next newline so we don't split a record
    // 对齐到下一个换行符，避免切断单条记录
    const nl = buf.indexOf(0x0a, halfStart);
    const sliceStart = nl >= 0 ? nl + 1 : halfStart;
    fs.writeFileSync(filePath, buf.subarray(sliceStart));
  } catch {
    // Best-effort; ignore rotation errors
    // 尽力而为；忽略轮转错误
  }
}

export interface StartupLogEntry {
  event: string;
  [key: string]: unknown;
}

/**
 * Replace the user's home directory prefix in a path with `~` to avoid leaking
 * the OS username (PII) into diagnostic logs shared by users for support.
 * Returns the input unchanged for non-string values or when homedir cannot be
 * resolved. Case-insensitive on Windows/macOS to match filesystem semantics.
 *
 * 将路径中的用户主目录替换为 `~`，避免日志中泄露用户名（PII）。
 * 在 Windows/macOS 上大小写不敏感以匹配文件系统语义。
 */
export function scrubPath(value: unknown): unknown {
  if (typeof value !== "string" || value.length === 0) return value;
  try {
    const home = os.homedir();
    if (!home) return value;
    const caseInsensitive =
      process.platform === "win32" || process.platform === "darwin";
    const candidate = caseInsensitive ? value.toLowerCase() : value;
    const target = caseInsensitive ? home.toLowerCase() : home;
    if (candidate.startsWith(target)) {
      return "~" + value.slice(home.length);
    }
    return value;
  } catch {
    return value;
  }
}

/**
 * 替换字符串中**任意位置**出现的主目录。
 *
 * scrubPath 只认前缀，够用于路径字段；而报错原文里的路径通常夹在句子中间
 * （`EPERM: operation not permitted, unlink 'C:\Users\xxx\...'`），
 * 不做全局替换就等于把用户名写进了会被分享出去的日志。
 */
function scrubMessage(value: string): string {
  try {
    const home = os.homedir();
    if (!home) return value;
    const caseInsensitive =
      process.platform === "win32" || process.platform === "darwin";
    if (!caseInsensitive) {
      return value.split(home).join("~");
    }
    const lowerHome = home.toLowerCase();
    let result = "";
    let cursor = 0;
    const lower = value.toLowerCase();
    for (;;) {
      const at = lower.indexOf(lowerHome, cursor);
      if (at < 0) {
        return result + value.slice(cursor);
      }
      result += value.slice(cursor, at) + "~";
      cursor = at + home.length;
    }
  } catch {
    return value;
  }
}

function appendLogLine(fileName: string, entry: Record<string, unknown>): void {
  try {
    const filePath = getLogFilePath(fileName);
    ensureLogDir(filePath);
    rotateIfTooLarge(filePath);
    const record = {
      ts: new Date().toISOString(),
      pid: process.pid,
      version: app.getVersion(),
      platform: process.platform,
      ...entry,
    };
    fs.appendFileSync(filePath, JSON.stringify(record) + "\n", "utf-8");
  } catch {
    // Swallow — diagnostic logger must not break the app
    // 静默处理 — 诊断日志不能反过来成为崩溃源
  }
}

/**
 * Append a startup event to the log file. Failures are swallowed to avoid
 * turning a diagnostic helper into a crash source.
 *
 * 向日志文件追加一条启动事件。失败时静默处理，避免诊断工具本身成为崩溃源。
 */
export function logStartupEvent(entry: StartupLogEntry): void {
  appendLogLine(STARTUP_LOG_FILE, entry);
}

export interface AppErrorEntry {
  /** 出错的位置，如 backup / wiki-compile / renderer */
  scope: string;
  /** 用户视角的动作名，如「自动备份」 */
  action: string;
  message: string;
  [key: string]: unknown;
}

/** 记一条业务失败。渲染进程经 `log:appError` 频道汇入同一个文件。 */
export function logAppError(entry: AppErrorEntry): void {
  appendLogLine(ERROR_LOG_FILE, {
    ...entry,
    message: scrubMessage(entry.message),
  });
}
