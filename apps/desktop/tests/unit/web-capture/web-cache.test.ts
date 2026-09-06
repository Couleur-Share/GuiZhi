import { expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { cleanAbandonedWebCaches } from "../../../src/main/services/web-capture/web-cache";
it("只清理已退出且归属明确的缓存，保留未知目录、活跃实例和越界链接", async () => {
  const parent = await fs.mkdtemp(path.join(os.tmpdir(), "guizhi-cache-test-"));
  const outside = await fs.mkdtemp(
    path.join(os.tmpdir(), "guizhi-cache-outside-"),
  );
  const kill = vi.spyOn(process, "kill").mockImplementation(((pid: number) => {
    if (pid === 424242)
      throw Object.assign(new Error("gone"), { code: "ESRCH" });
    return true;
  }) as typeof process.kill);
  try {
    for (const [name, pid] of [
      ["owned-dead", 424242],
      ["owned-alive", process.pid],
      ["owned-unknown", null],
    ] as const) {
      const directory = path.join(parent, name);
      await fs.mkdir(directory);
      if (pid)
        await fs.writeFile(
          path.join(directory, "owner.json"),
          JSON.stringify({
            component: "guizhi-web-capture",
            pid,
            createdAt: 1,
          }),
        );
    }
    await fs.writeFile(path.join(outside, "keep.txt"), "不能删除");
    await fs.symlink(
      outside,
      path.join(parent, "owned-link"),
      process.platform === "win32" ? "junction" : "dir",
    );
    await cleanAbandonedWebCaches(parent);
    expect((await fs.readdir(parent)).sort()).toEqual([
      "owned-alive",
      "owned-link",
      "owned-unknown",
    ]);
    expect(await fs.readFile(path.join(outside, "keep.txt"), "utf8")).toBe(
      "不能删除",
    );
  } finally {
    kill.mockRestore();
    await fs.rm(parent, { recursive: true, force: true });
    await fs.rm(outside, { recursive: true, force: true });
  }
});
