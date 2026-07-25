/**
 * yt-dlp 工具管理：应用内一键安装（官方源 + 镜像回退、走网络代理）、
 * 状态探测与移除。托管副本位于 %userData%/tools/。
 *
 * 解析时的生效顺序：自定义路径 → 内置托管版 → 系统 PATH。
 */
import { spawn } from "child_process";
import fs from "fs";
import path from "path";
import type { YtDlpDownloadProgress, YtDlpStatus } from "@guizhi/shared/types";
import { getToolsDir } from "../../runtime-paths";
import { downloadToFile } from "./tool-download";

const VERSION_PROBE_TIMEOUT_MS = 10_000;

export function getManagedBinaryName(
  platform: NodeJS.Platform = process.platform,
): string {
  return platform === "win32" ? "yt-dlp.exe" : "yt-dlp";
}

export function getManagedYtDlpPath(
  platform: NodeJS.Platform = process.platform,
): string {
  return path.join(getToolsDir(), getManagedBinaryName(platform));
}

/** 官方 latest 资产 + 镜像加速源（与应用更新器同一组镜像） */
export function getYtDlpDownloadUrls(
  platform: NodeJS.Platform = process.platform,
): string[] {
  const asset =
    platform === "win32"
      ? "yt-dlp.exe"
      : platform === "darwin"
        ? "yt-dlp_macos"
        : "yt-dlp_linux";
  const official = `https://github.com/yt-dlp/yt-dlp/releases/latest/download/${asset}`;
  return [
    official,
    `https://ghfast.top/${official}`,
    `https://gh-proxy.com/${official}`,
    `https://hub.gitmirror.com/${official}`,
  ];
}

export type VersionProbe = (executable: string) => Promise<string | null>;

/** 运行 `--version` 探测可执行性；失败 / 超时 / 不存在均返回 null */
export const probeYtDlpVersion: VersionProbe = (executable) =>
  new Promise((resolve) => {
    let child: ReturnType<typeof spawn>;
    try {
      child = spawn(executable, ["--version"], { windowsHide: true });
    } catch {
      resolve(null);
      return;
    }
    let stdout = "";
    let settled = false;
    const finish = (value: string | null) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        resolve(value);
      }
    };
    const timer = setTimeout(() => {
      child.kill();
      finish(null);
    }, VERSION_PROBE_TIMEOUT_MS);

    child.stdout?.on("data", (chunk: Buffer) => {
      stdout += chunk.toString("utf8");
    });
    child.on("error", () => finish(null));
    child.on("close", (code) => {
      const version = stdout.trim().split(/\r?\n/)[0]?.trim();
      finish(code === 0 && version ? version : null);
    });
  });

/** 解析时的生效可执行文件：自定义路径 → 内置托管版 → 系统 PATH */
export function resolveYtDlpExecutable(
  configuredPath: string | null,
  managedPath = getManagedYtDlpPath(),
): string {
  const custom = configuredPath?.trim();
  if (custom) {
    return custom;
  }
  if (fs.existsSync(managedPath)) {
    return managedPath;
  }
  return "yt-dlp";
}

export async function getYtDlpStatus(
  configuredPath: string | null,
  options?: { probe?: VersionProbe; managedPath?: string },
): Promise<YtDlpStatus> {
  const probe = options?.probe ?? probeYtDlpVersion;
  const managedPath = options?.managedPath ?? getManagedYtDlpPath();

  const custom = configuredPath?.trim();
  if (custom) {
    const version = await probe(custom);
    return {
      installed: version !== null,
      source: version !== null ? "custom" : null,
      version: version ?? undefined,
      path: custom,
      managedPath,
    };
  }

  if (fs.existsSync(managedPath)) {
    const version = await probe(managedPath);
    if (version) {
      return {
        installed: true,
        source: "managed",
        version,
        path: managedPath,
        managedPath,
      };
    }
    // 托管副本损坏：继续探测系统 PATH
  }

  const pathVersion = await probe("yt-dlp");
  if (pathVersion) {
    return {
      installed: true,
      source: "path",
      version: pathVersion,
      path: "yt-dlp",
      managedPath,
    };
  }

  return { installed: false, source: null, managedPath };
}

let installInFlight = false;

/**
 * 下载并安装托管版 yt-dlp：逐源尝试 → 临时文件校验可执行 → 原子替换。
 * 返回安装的版本号；全部下载源失败时抛错。
 */
export async function installYtDlp(
  onProgress?: (progress: YtDlpDownloadProgress) => void,
): Promise<{ version: string }> {
  if (installInFlight) {
    throw new Error("已有安装任务进行中");
  }
  installInFlight = true;
  try {
    const managedPath = getManagedYtDlpPath();
    const toolsDir = path.dirname(managedPath);
    fs.mkdirSync(toolsDir, { recursive: true });
    // Windows 的 spawn 要求 .exe 扩展名，临时文件保持可探测的命名
    const tempPath = path.join(
      toolsDir,
      process.platform === "win32" ? "yt-dlp.partial.exe" : "yt-dlp.partial",
    );

    const failures: string[] = [];
    for (const url of getYtDlpDownloadUrls()) {
      try {
        console.log(`[ytdlp] 开始下载: ${url}`);
        await downloadToFile(url, tempPath, onProgress);
        if (process.platform !== "win32") {
          fs.chmodSync(tempPath, 0o755);
        }
        const version = await probeYtDlpVersion(tempPath);
        if (!version) {
          throw new Error("下载的文件无法运行");
        }
        fs.rmSync(managedPath, { force: true });
        fs.renameSync(tempPath, managedPath);
        console.log(`[ytdlp] 安装完成 v${version} → ${managedPath}`);
        return { version };
      } catch (error) {
        fs.rmSync(tempPath, { force: true });
        const host = (() => {
          try {
            return new URL(url).hostname;
          } catch {
            return url;
          }
        })();
        failures.push(
          `${host}: ${error instanceof Error ? error.message : String(error)}`,
        );
        console.warn(`[ytdlp] 下载源失败（${host}），尝试下一个`);
      }
    }
    throw new Error(`所有下载源均失败——${failures.join("；")}`);
  } finally {
    installInFlight = false;
  }
}

export function removeManagedYtDlp(): boolean {
  const managedPath = getManagedYtDlpPath();
  if (!fs.existsSync(managedPath)) {
    return false;
  }
  fs.rmSync(managedPath);
  console.log(`[ytdlp] 已移除托管版: ${managedPath}`);
  return true;
}
