/** 只清理归知明确标记为可丢弃的备份，未知旧目录与恢复副本不按年龄猜测。 */
import fs from "fs";
import path from "path";
import { randomUUID } from "crypto";
import type { FunasrInstallState } from "./funasr-paths";
import {
  assertDirectoryIdentity,
  assertPlainFunasrTree,
  readDirectoryIdentity,
  type DirectoryIdentity,
} from "./funasr-update-paths";

type BackupPhase =
  "copying" | "ready" | "updating" | "recovery-required" | "disposable";
interface BackupManifest {
  schema: 2;
  kind: "guizhi-funasr-update";
  directory: string;
  ownerPid: number;
  createdAt: string;
  phase: BackupPhase;
  original: FunasrInstallState;
  targetVersion: string;
  rootIdentity: DirectoryIdentity;
  directoryIdentity: DirectoryIdentity;
}
export interface FunasrBackup {
  root: string;
  directory: string;
  manifest: BackupManifest;
}
const MANIFEST = "update.json";
const BACKUP_NAME = /^update-backup-[a-zA-Z0-9]{6}$/;

/** 同目录临时文件 + rename；断电留下坏/旧记录时宁可保留。 */
export async function markFunasrBackup(
  backup: FunasrBackup,
  phase: BackupPhase,
): Promise<void> {
  await assertBackupIdentity(backup);
  const next = { ...backup.manifest, phase };
  // 随机名字 + 独占创建：绝不截断现有临时文件、符号链接或硬链接目标。
  const temporary = path.join(backup.directory, `.update-${randomUUID()}.tmp`);
  await fs.promises.writeFile(temporary, `${JSON.stringify(next, null, 2)}\n`, {
    flush: true,
    flag: "wx",
  });
  await assertBackupIdentity(backup);
  await fs.promises.rename(temporary, path.join(backup.directory, MANIFEST));
  backup.manifest = next;
}

export async function createFunasrBackup(
  root: string,
  original: FunasrInstallState,
  targetVersion: string,
): Promise<FunasrBackup> {
  const rootIdentity = await readDirectoryIdentity(root);
  const directory = await fs.promises.mkdtemp(
    path.join(root, "update-backup-"),
  );
  const backup: FunasrBackup = {
    root,
    directory,
    manifest: {
      schema: 2,
      kind: "guizhi-funasr-update",
      directory: path.basename(directory),
      ownerPid: process.pid,
      createdAt: new Date().toISOString(),
      phase: "copying",
      original,
      targetVersion,
      rootIdentity,
      directoryIdentity: await readDirectoryIdentity(directory),
    },
  };
  await markFunasrBackup(backup, "copying");
  return backup;
}

async function assertBackupIdentity(backup: FunasrBackup): Promise<void> {
  await assertBackupPath(backup.root, backup.directory);
  await assertDirectoryIdentity(backup.root, backup.manifest.rootIdentity);
  await assertDirectoryIdentity(
    backup.directory,
    backup.manifest.directoryIdentity,
  );
}

async function assertBackupContents(backup: FunasrBackup): Promise<void> {
  const allowed = new Set(["env", "tmp", "failed-env", MANIFEST]);
  for (const entry of await fs.promises.readdir(backup.directory, {
    withFileTypes: true,
  })) {
    if (
      !allowed.has(entry.name) ||
      (entry.name === MANIFEST ? !entry.isFile() : !entry.isDirectory())
    ) {
      throw new Error(`备份含有非预期内容，保留整个目录：${entry.name}`);
    }
  }
  await assertPlainFunasrTree(backup.directory);
}

async function assertBackupPath(
  root: string,
  directory: string,
): Promise<void> {
  if (
    path.dirname(path.resolve(directory)) !== path.resolve(root) ||
    !BACKUP_NAME.test(path.basename(directory))
  ) {
    throw new Error("备份目录越界，拒绝清理");
  }
  const stat = await fs.promises.lstat(directory);
  if (!stat.isDirectory() || stat.isSymbolicLink())
    throw new Error("备份目录是链接，拒绝清理");
  const realRoot = await fs.promises.realpath(root);
  if (
    (await fs.promises.realpath(directory)) !==
    path.join(realRoot, path.basename(directory))
  ) {
    throw new Error("备份真实路径越界，拒绝清理");
  }
}

async function removeBackup(backup: FunasrBackup): Promise<void> {
  await assertBackupIdentity(backup);
  await assertBackupContents(backup);
  await assertBackupIdentity(backup);
  // 扫描后再次读状态；其他操作若已改成恢复中，不使用旧快照继续删除。
  const marker = path.join(backup.directory, MANIFEST);
  const markerStat = await fs.promises.lstat(marker);
  if (
    !markerStat.isFile() ||
    markerStat.isSymbolicLink() ||
    markerStat.nlink !== 1
  ) {
    throw new Error("备份状态记录异常，保留文件");
  }
  const current = JSON.parse(await fs.promises.readFile(marker, "utf8"));
  if (
    current.phase !== "disposable" ||
    JSON.stringify(current) !== JSON.stringify(backup.manifest)
  ) {
    throw new Error("备份状态发生变化，保留文件");
  }
  try {
    await fs.promises.rm(backup.directory, {
      recursive: true,
      force: true,
      maxRetries: 3,
      retryDelay: 500,
    });
  } catch (cause) {
    // rm 可能先删掉记录、再遇到被占用的依赖文件；重写终态，供下次安全重试。
    try {
      await assertBackupIdentity(backup);
      // 只补回已被删掉的记录；其他操作写入的新记录不能被覆盖。
      await fs.promises.writeFile(
        marker,
        `${JSON.stringify(backup.manifest, null, 2)}\n`,
        { flag: "wx", flush: true },
      );
    } catch {
      /* 无法写回时按未知目录保留，不扩大删除范围。 */
    }
    throw cause;
  }
}

/** 失败返回可展示的警告；清理故障不得把验收成功的升级变成回滚。 */
export async function discardFunasrBackup(
  backup: FunasrBackup,
): Promise<string | undefined> {
  try {
    await assertBackupIdentity(backup);
    await markFunasrBackup(backup, "disposable");
    await removeBackup(backup);
    return undefined;
  } catch (cause) {
    return `备份清理未完成，下次更新会重试：${backup.directory}（${String(cause)}）`;
  }
}

/** 仅在更新维护锁内、创建本次备份之前调用。 */
export async function cleanupFunasrBackups(root: string): Promise<string[]> {
  const warnings: string[] = [];
  for (const entry of await fs.promises.readdir(root, {
    withFileTypes: true,
  })) {
    if (
      !entry.name.startsWith("update-backup-") &&
      !entry.name.startsWith("pip-repair-backup-")
    )
      continue;
    const directory = path.join(root, entry.name);
    let manifest: BackupManifest;
    try {
      await assertBackupPath(root, directory);
      const marker = path.join(directory, MANIFEST);
      const stat = await fs.promises.lstat(marker);
      if (!stat.isFile() || stat.isSymbolicLink())
        throw new Error("非普通记录");
      manifest = JSON.parse(await fs.promises.readFile(marker, "utf8"));
      if (
        manifest.schema !== 2 ||
        manifest.kind !== "guizhi-funasr-update" ||
        manifest.directory !== entry.name ||
        !Number.isSafeInteger(manifest.ownerPid) ||
        manifest.ownerPid <= 0
      ) {
        throw new Error("无法识别记录");
      }
    } catch {
      warnings.push(`保留状态不明的旧备份，请核对后处理：${directory}`);
      continue;
    }
    // 进程退出不能证明备份已不需要；历史中断记录一律留给用户核对。
    if (manifest.phase !== "disposable") {
      warnings.push(`保留使用中或可能需要恢复的备份：${directory}`);
      continue;
    }
    try {
      await removeBackup({ root, directory, manifest });
    } catch (cause) {
      warnings.push(
        `旧备份清理失败，下次更新会重试：${directory}（${String(cause)}）`,
      );
    }
  }
  return warnings;
}

/** 回滚先保存失败现场，恢复复制失败时也不会先删掉现有环境。 */
export async function preserveFunasrRollbackEnvironment(
  backup: FunasrBackup,
  environment: string,
  identity: DirectoryIdentity,
): Promise<void> {
  await assertBackupIdentity(backup);
  if (path.resolve(environment) !== path.resolve(backup.root, "env")) {
    throw new Error("回滚环境路径越界，保留文件");
  }
  await assertDirectoryIdentity(environment, identity);
  const source = path.join(backup.directory, "env");
  await assertPlainFunasrTree(source);
  for (const name of [
    "Scripts/python.exe",
    "Lib/site-packages/funasr/__init__.py",
  ]) {
    if (!(await fs.promises.lstat(path.join(source, name))).isFile()) {
      throw new Error("恢复备份缺少引擎文件，保留当前环境");
    }
  }
  const failed = path.join(backup.directory, "failed-env");
  try {
    await fs.promises.lstat(failed);
    throw new Error("失败环境目录已存在，拒绝覆盖");
  } catch (cause) {
    if ((cause as NodeJS.ErrnoException).code !== "ENOENT") throw cause;
  }
  await assertBackupIdentity(backup);
  await assertDirectoryIdentity(environment, identity);
  await fs.promises.rename(environment, failed);
}
