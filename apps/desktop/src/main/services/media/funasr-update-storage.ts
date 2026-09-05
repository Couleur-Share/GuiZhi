/** 更新空间预算：备份、pip 下载/解包和新旧依赖共存均落在引擎所在卷。 */
import fs from "fs";

const GIB = 1024 ** 3;
const RESERVE_BYTES = 512 * 1024 ** 2;

export interface FunasrSpaceBudget {
  backupBytes: number;
  workBytes: number;
}

export function formatFunasrBytes(bytes: number): string {
  return `${(bytes / GIB).toFixed(2)} GiB`;
}

/** 按目标卷分配单位向上取整；不跟随链接，防止环境路径跳到其他卷。 */
export async function measureFunasrEnvironment(
  directory: string,
  blockSize: number,
): Promise<number> {
  const root = await fs.promises.lstat(directory);
  if (!root.isDirectory() || root.isSymbolicLink()) {
    throw new Error("引擎环境不是普通目录，无法安全备份");
  }
  let total = blockSize;
  const pending = [directory];
  while (pending.length) {
    const dir = await fs.promises.opendir(pending.pop()!);
    for await (const entry of dir) {
      const file = `${dir.path}/${entry.name}`;
      const stat = await fs.promises.lstat(file);
      if (stat.isSymbolicLink())
        throw new Error(`引擎环境包含链接，无法估算备份空间：${file}`);
      if (stat.isDirectory()) {
        pending.push(file);
        total += blockSize;
      } else if (stat.isFile()) {
        total += Math.ceil(stat.size / blockSize) * blockSize;
      } else {
        throw new Error(`引擎环境包含不支持的文件：${file}`);
      }
    }
  }
  return total;
}

async function readDiskSpace(root: string) {
  try {
    const disk = await fs.promises.statfs(root);
    const available = disk.bavail * disk.bsize;
    if (!Number.isFinite(available) || available < 0 || !(disk.bsize > 0)) {
      throw new Error("磁盘返回了无效容量");
    }
    return { available, blockSize: disk.bsize };
  } catch (cause) {
    throw new Error(`无法检查引擎磁盘空间，更新尚未开始：${String(cause)}`, {
      cause,
    });
  }
}

export async function assertFunasrUpdateSpace(
  root: string,
  required: number,
): Promise<void> {
  const { available } = await readDiskSpace(root);
  if (available < required) {
    throw new Error(
      `磁盘空间不足，更新尚未开始：${root} 所在磁盘预计需要 ${formatFunasrBytes(required)}，当前可用 ${formatFunasrBytes(available)}。请释放空间后重试。`,
    );
  }
}

export async function planFunasrUpdateSpace(
  root: string,
  env: string,
): Promise<FunasrSpaceBudget> {
  const { blockSize } = await readDiskSpace(root);
  const backupBytes = await measureFunasrEnvironment(env, blockSize);
  // 更新包大小在解析依赖前未知：预留两份环境大小（至少 1 GiB）及 512 MiB 余量。
  // 这是保守预算，不是容量保证；复制结束后再次检查，实际写满仍由事务回滚。
  const workBytes = Math.max(backupBytes * 2, GIB) + RESERVE_BYTES;
  await assertFunasrUpdateSpace(root, backupBytes + workBytes);
  return { backupBytes, workBytes };
}
