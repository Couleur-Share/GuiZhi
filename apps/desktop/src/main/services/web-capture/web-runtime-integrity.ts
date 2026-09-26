import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";

export function runtimeFile(root: string, relative: string): string {
  const full = path.resolve(root, relative);
  if (
    !relative ||
    path.isAbsolute(relative) ||
    !full.startsWith(path.resolve(root) + path.sep)
  )
    throw new Error("组件清单路径越界");
  return full;
}

// 限制同时打开的文件和哈希缓冲；失败后停止派发，并等已开始的读取关闭。
async function checkInBatches<T>(
  values: T[],
  check: (value: T) => Promise<void>,
): Promise<void> {
  let next = 0;
  let failed = false;
  let failure: unknown;
  await Promise.all(
    Array.from({ length: Math.min(8, values.length) }, async () => {
      while (!failed && next < values.length) {
        const value = values[next++];
        try {
          await check(value);
        } catch (error) {
          if (!failed) failure = error;
          failed = true;
        }
      }
    }),
  );
  if (failed) throw failure;
}

/** 每次完整验证文件清单；不缓存校验结果，不跳过 Python 或浏览器文件。 */
export async function verifyRuntimeFiles(
  root: string,
  files: Record<string, string>,
) {
  const started = performance.now();
  const rootReal = await fs.realpath(root);
  const entries = await fs.readdir(root, {
    recursive: true,
    withFileTypes: true,
  });
  const listed = performance.now();
  await checkInBatches(
    entries.filter((entry) => !entry.isDirectory()),
    async (entry) => {
      const full = path.join(entry.parentPath, entry.name);
      const relative = path.relative(root, full).replaceAll(path.sep, "/");
      const real = await fs.realpath(full);
      if (!real.startsWith(rootReal + path.sep))
        throw new Error("组件包含越界链接");
      if ((await fs.stat(full)).isDirectory()) return;
      if (relative !== "manifest.json" && !files[relative])
        throw new Error(`组件包含未登记文件：${relative}`);
    },
  );
  const inspected = performance.now();
  let bytes = 0;
  await checkInBatches(Object.entries(files), async ([relative, expected]) => {
    if (!/^[a-f0-9]{64}$/.test(expected)) throw new Error("组件校验值无效");
    const hash = createHash("sha256");
    for await (const chunk of createReadStream(runtimeFile(root, relative))) {
      hash.update(chunk);
      bytes += chunk.length;
    }
    if (hash.digest("hex") !== expected)
      throw new Error(`组件校验失败：${relative}；请重新安装当前归知版本`);
  });
  return {
    files: Object.keys(files).length,
    bytes,
    listMs: listed - started,
    metadataMs: inspected - listed,
    hashMs: performance.now() - inspected,
    totalMs: performance.now() - started,
  };
}
