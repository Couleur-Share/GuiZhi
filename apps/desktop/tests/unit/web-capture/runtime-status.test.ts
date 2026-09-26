import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const fixture = vi.hoisted(() => ({ root: "" }));
vi.mock("electron", () => ({
  app: { isPackaged: false, getAppPath: () => fixture.root },
}));
vi.mock("node:os", async (original) => ({
  ...(await original<typeof import("node:os")>()),
  default: {
    ...(await original<typeof import("node:os")>()).default,
    release: () => "10.0.26100",
  },
}));
let runtime: string;
let manifest: Record<string, unknown>;
beforeEach(async () => {
  vi.resetModules();
  fixture.root = await fs.mkdtemp(
    path.join(os.tmpdir(), "guizhi-runtime-status-"),
  );
  runtime = path.join(fixture.root, "resources/crawl4ai");
  await fs.mkdir(path.join(runtime, "python"), { recursive: true });
  await fs.writeFile(path.join(runtime, "python/python.exe"), "python");
  manifest = {
    protocol: 1,
    version: "0.9.3",
    target: "win32-x64",
    renderer: "electron",
    python: "python/python.exe",
    files: {
      "python/python.exe": createHash("sha256").update("python").digest("hex"),
    },
  };
  await fs.writeFile(
    path.join(runtime, "manifest.json"),
    JSON.stringify(manifest),
  );
});
afterEach(async () => {
  await fs.rm(fixture.root, { recursive: true, force: true });
});

describe.skipIf(process.platform !== "win32")(
  "无独立 Chromium 的 Windows 组件",
  () => {
    it("组件状态可用，完整校验通过", async () => {
      const { webRuntimeStatus, verifyWebRuntime } =
        await import("../../../src/main/services/web-capture/web-runtime");
      expect(await webRuntimeStatus()).toMatchObject({
        available: true,
        supported: true,
      });
      expect(await verifyWebRuntime()).toMatchObject({ renderer: "electron" });
    });
    it("Python 缺失仍报告修复，不把无浏览器当成放宽完整性校验", async () => {
      await fs.unlink(path.join(runtime, "python/python.exe"));
      const { webRuntimeStatus } =
        await import("../../../src/main/services/web-capture/web-runtime");
      expect(await webRuntimeStatus()).toMatchObject({
        available: false,
        repairRequired: true,
      });
    });
  },
);
