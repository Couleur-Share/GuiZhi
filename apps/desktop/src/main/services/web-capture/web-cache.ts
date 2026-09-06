import fs from "node:fs/promises";
import path from "node:path";

/** 只回收已退出的归知实例留下的、有本组件标记的目录；未知目录留存。 */
export async function cleanAbandonedWebCaches(parent: string): Promise<void> {
  const root = await fs.realpath(parent);
  for (const entry of await fs.readdir(root, { withFileTypes: true })) {
    if (!entry.isDirectory() || !/^owned-[a-zA-Z0-9]+$/.test(entry.name))
      continue;
    const directory = path.join(root, entry.name);
    try {
      if ((await fs.realpath(directory)) !== directory) continue;
      const marker = JSON.parse(
        await fs.readFile(path.join(directory, "owner.json"), "utf8"),
      );
      if (
        marker.component !== "guizhi-web-capture" ||
        !Number.isSafeInteger(marker.pid) ||
        marker.pid <= 0 ||
        !Number.isFinite(marker.createdAt)
      )
        continue;
      // PID 仍存在（包括已复用的 PID）就保留，不能推断它已经可以删除。
      try {
        process.kill(marker.pid, 0);
        continue;
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== "ESRCH") continue;
      }
      if (Date.now() - marker.createdAt < 60_000) continue;
      await fs.rm(directory, { recursive: true, force: true });
    } catch {
      /* 归属、权限或目录状态不确定时保留。 */
    }
  }
}
