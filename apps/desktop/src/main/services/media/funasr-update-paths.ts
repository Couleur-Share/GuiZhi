/** 更新维护的文件系统边界；路径字符串相同不代表仍是原来的目录。 */
import fs from "fs";
import path from "path";

export interface DirectoryIdentity {
  device: string;
  inode: string;
  birthtime: string;
}

export async function readDirectoryIdentity(
  directory: string,
): Promise<DirectoryIdentity> {
  const stat = await fs.promises.lstat(directory, { bigint: true });
  if (!stat.isDirectory() || stat.isSymbolicLink() || stat.ino === 0n) {
    throw new Error(`不是可验证身份的普通目录，保留文件：${directory}`);
  }
  return {
    device: String(stat.dev),
    inode: String(stat.ino),
    birthtime: String(stat.birthtimeNs),
  };
}

export async function assertDirectoryIdentity(
  directory: string,
  expected: DirectoryIdentity,
): Promise<void> {
  const actual = await readDirectoryIdentity(directory);
  if (
    !expected ||
    actual.device !== expected.device ||
    actual.inode !== expected.inode ||
    actual.birthtime !== expected.birthtime
  ) {
    throw new Error(`目录身份发生变化，保留文件：${directory}`);
  }
}

/** 拒绝目录连接、符号链接和硬链接；遇到不认识的节点就停，不递归到目标。 */
export async function assertPlainFunasrTree(directory: string): Promise<void> {
  const pending = [directory];
  while (pending.length) {
    const current = pending.pop()!;
    const identity = await readDirectoryIdentity(current);
    for (const entry of await fs.promises.readdir(current, {
      withFileTypes: true,
    })) {
      const file = path.join(current, entry.name);
      const stat = await fs.promises.lstat(file);
      if (
        stat.isSymbolicLink() ||
        (!stat.isDirectory() && (!stat.isFile() || stat.nlink !== 1))
      ) {
        throw new Error(`发现链接或异常文件，保留整个目录：${file}`);
      }
      if (stat.isDirectory()) pending.push(file);
    }
    await assertDirectoryIdentity(current, identity);
  }
}
