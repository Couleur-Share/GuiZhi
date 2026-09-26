import { app } from "electron";
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import type { WebRuntimeStatus } from "@guizhi/shared/types";
import { runtimeFile, verifyRuntimeFiles } from "./web-runtime-integrity";

export { runtimeFile } from "./web-runtime-integrity";

export interface WebRuntimeManifest {
  protocol: 1;
  version: string;
  target: string;
  python: string;
  /** 仅兼容旧开发运行包；当前采集由 Electron 渲染。 */
  browser?: string;
  renderer?: "electron";
  files: Record<string, string>;
  workerHashes?: Record<string, string>;
}
let integrityFailure: string | undefined;
export function webRuntimeRoot(): string {
  return app.isPackaged
    ? path.join(process.resourcesPath, "crawl4ai")
    : path.join(developmentResources(), "crawl4ai");
}
function developmentResources(): string {
  const base = app.getAppPath();
  for (const candidate of [
    path.join(base, "resources"),
    path.resolve(base, "../../resources"),
  ])
    if (existsSync(path.join(candidate, "crawl4ai-worker", "worker.py")))
      return candidate;
  return path.join(base, "resources");
}
export function workerRoot(): string {
  return app.isPackaged
    ? path.join(process.resourcesPath, "crawl4ai-worker")
    : path.join(developmentResources(), "crawl4ai-worker");
}
export async function webRuntimeStatus(
  running = false,
): Promise<WebRuntimeStatus> {
  const target = `${process.platform}-${process.platform === "win32" ? "x64" : process.arch}`;
  let supported = [
    "win32-x64",
    "darwin-x64",
    "darwin-arm64",
    "linux-x64",
  ].includes(target);
  let reason: string | undefined;
  if (
    process.platform === "win32" &&
    Number(os.release().split(".")[2]) < 22000
  )
    supported = false;
  if (process.platform === "darwin" && Number(os.release().split(".")[0]) < 23)
    supported = false;
  if (process.platform === "linux") {
    const release = await fs
      .readFile("/etc/os-release", "utf8")
      .catch(() => "");
    const id = /^ID=["']?([^"'\n]+)/m.exec(release)?.[1];
    const version = Number(/^VERSION_ID=["']?([\d.]+)/m.exec(release)?.[1]);
    supported =
      supported &&
      ((id === "ubuntu" && version >= 22.04) ||
        (id === "debian" && version >= 12));
  }
  if (!supported)
    reason =
      "网页组件需要 Windows 11、macOS 14 或 Ubuntu 22.04 / Debian 12 及以上受支持架构";
  const status: WebRuntimeStatus = {
    supported,
    available: false,
    running,
    version: "0.9.3",
    runtimeTarget: target,
    reason,
  };
  if (!supported) return status;
  if (integrityFailure)
    return { ...status, reason: integrityFailure, repairRequired: true };
  try {
    const root = webRuntimeRoot();
    const manifest: WebRuntimeManifest = JSON.parse(
      await fs.readFile(path.join(root, "manifest.json"), "utf8"),
    );
    if (
      manifest.protocol !== 1 ||
      manifest.version !== "0.9.3" ||
      manifest.target !== target
    )
      throw new Error("组件版本或架构不匹配");
    for (const file of [manifest.python]) {
      if (!manifest.files[file])
        throw new Error("组件清单缺少可执行文件校验值");
      await fs.access(runtimeFile(root, file));
    }
    return { ...status, available: true };
  } catch {
    return {
      ...status,
      reason: app.isPackaged
        ? "随包网页组件缺失或清单无效；请重新安装当前归知版本修复组件"
        : "开发环境网页组件缺失或清单无效；请在仓库根目录运行 python scripts/build-crawl4ai.py --target " +
          target,
      repairRequired: true,
    };
  }
}
export async function verifyWebRuntime(): Promise<WebRuntimeManifest> {
  try {
    return await verifyFiles();
  } catch (error) {
    integrityFailure =
      error instanceof Error ? error.message : "组件文件校验失败";
    throw error;
  }
}
async function verifyFiles(): Promise<WebRuntimeManifest> {
  const root = webRuntimeRoot();
  const manifest: WebRuntimeManifest = JSON.parse(
    await fs.readFile(path.join(root, "manifest.json"), "utf8"),
  );
  await verifyRuntimeFiles(root, manifest.files);
  if (app.isPackaged) {
    if (!manifest.workerHashes) throw new Error("组件清单缺少 worker 校验值");
    for (const entry of await fs.readdir(workerRoot()))
      if (!manifest.workerHashes[entry])
        throw new Error("worker 包含未登记文件，请重新安装当前版本");
    for (const [relative, expected] of Object.entries(manifest.workerHashes)) {
      const data = await fs.readFile(runtimeFile(workerRoot(), relative));
      if (createHash("sha256").update(data).digest("hex") !== expected)
        throw new Error(`worker 校验失败：${relative}；请重新安装当前归知版本`);
    }
  }
  return manifest;
}
