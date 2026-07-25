/**
 * ffmpeg 工具管理：应用内一键安装（yt-dlp/FFmpeg-Builds 官方源 + 镜像回退、
 * 走网络代理）、状态探测与移除。托管副本位于 %userData%/tools/。
 *
 * 用途：转写上传前把音频转码为 16kHz 单声道 mp3（体积更小、格式兼容性更好）。
 * 解析时的生效顺序：自定义路径 → 内置托管版 → 系统 PATH。
 */
import { spawn } from "child_process";
import fs from "fs";
import path from "path";
import AdmZip from "adm-zip";
import type { FfmpegDownloadProgress, FfmpegStatus } from "@guizhi/shared/types";
import { getToolsDir } from "../../runtime-paths";
import { downloadToFile } from "./tool-download";

const VERSION_PROBE_TIMEOUT_MS = 10_000;

export function getManagedFfmpegBinaryName(
  platform: NodeJS.Platform = process.platform,
): string {
  return platform === "win32" ? "ffmpeg.exe" : "ffmpeg";
}

export function getManagedFfmpegPath(
  platform: NodeJS.Platform = process.platform,
): string {
  return path.join(getToolsDir(), getManagedFfmpegBinaryName(platform));
}

/**
 * Windows 静态构建 zip（yt-dlp 官方维护的 FFmpeg-Builds），
 * 镜像加速源与 yt-dlp 管理器同一组。其余平台不提供应用内安装。
 */
export function getFfmpegDownloadUrls(
  platform: NodeJS.Platform = process.platform,
): string[] {
  if (platform !== "win32") {
    return [];
  }
  const official =
    "https://github.com/yt-dlp/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip";
  return [
    official,
    `https://ghfast.top/${official}`,
    `https://gh-proxy.com/${official}`,
    `https://hub.gitmirror.com/${official}`,
  ];
}

export type FfmpegVersionProbe = (
  executable: string,
) => Promise<string | null>;

/** 运行 `-version` 探测可执行性；失败 / 超时 / 不存在均返回 null */
export const probeFfmpegVersion: FfmpegVersionProbe = (executable) =>
  new Promise((resolve) => {
    let child: ReturnType<typeof spawn>;
    try {
      child = spawn(executable, ["-version"], { windowsHide: true });
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
      // 首行形如 "ffmpeg version 8.1-essentials_build-www.gyan.dev Copyright ..."
      const match = stdout.match(/^ffmpeg version (\S+)/);
      finish(code === 0 && match ? match[1] : null);
    });
  });

/** 解析时的生效可执行文件：自定义路径 → 内置托管版 → 系统 PATH */
export function resolveFfmpegExecutable(
  configuredPath: string | null,
  managedPath = getManagedFfmpegPath(),
): string {
  const custom = configuredPath?.trim();
  if (custom) {
    return custom;
  }
  if (fs.existsSync(managedPath)) {
    return managedPath;
  }
  return "ffmpeg";
}

export async function getFfmpegStatus(
  configuredPath: string | null,
  options?: { probe?: FfmpegVersionProbe; managedPath?: string },
): Promise<FfmpegStatus> {
  const probe = options?.probe ?? probeFfmpegVersion;
  const managedPath = options?.managedPath ?? getManagedFfmpegPath();

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

  const pathVersion = await probe("ffmpeg");
  if (pathVersion) {
    return {
      installed: true,
      source: "path",
      version: pathVersion,
      path: "ffmpeg",
      managedPath,
    };
  }

  return { installed: false, source: null, managedPath };
}

/** 从下载的 zip 中解出 bin/ffmpeg.exe 到目标路径 */
export function extractFfmpegFromZip(
  zipPath: string,
  targetPath: string,
): void {
  const zip = new AdmZip(zipPath);
  const entry = zip
    .getEntries()
    .find((candidate) =>
      candidate.entryName.replace(/\\/g, "/").endsWith("/bin/ffmpeg.exe"),
    );
  if (!entry) {
    throw new Error("压缩包中未找到 bin/ffmpeg.exe");
  }
  fs.writeFileSync(targetPath, entry.getData());
}

let installInFlight = false;

/**
 * 下载并安装托管版 ffmpeg：逐源尝试 → zip 解出可执行文件 →
 * 校验可运行 → 原子替换。返回版本号；全部下载源失败时抛错。
 */
export async function installFfmpeg(
  onProgress?: (progress: FfmpegDownloadProgress) => void,
): Promise<{ version: string }> {
  if (installInFlight) {
    throw new Error("已有安装任务进行中");
  }
  if (process.platform !== "win32") {
    throw new Error("当前平台请使用系统包管理器安装 ffmpeg");
  }
  installInFlight = true;
  try {
    const managedPath = getManagedFfmpegPath();
    const toolsDir = path.dirname(managedPath);
    fs.mkdirSync(toolsDir, { recursive: true });
    const zipPath = path.join(toolsDir, "ffmpeg.download.zip");
    // Windows 的 spawn 要求 .exe 扩展名，临时文件保持可探测的命名
    const tempPath = path.join(toolsDir, "ffmpeg.partial.exe");

    const failures: string[] = [];
    for (const url of getFfmpegDownloadUrls()) {
      try {
        console.log(`[ffmpeg] 开始下载: ${url}`);
        await downloadToFile(url, zipPath, onProgress);
        extractFfmpegFromZip(zipPath, tempPath);
        const version = await probeFfmpegVersion(tempPath);
        if (!version) {
          throw new Error("下载的文件无法运行");
        }
        fs.rmSync(managedPath, { force: true });
        fs.renameSync(tempPath, managedPath);
        console.log(`[ffmpeg] 安装完成 v${version} → ${managedPath}`);
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
        console.warn(`[ffmpeg] 下载源失败（${host}），尝试下一个`);
      } finally {
        fs.rmSync(zipPath, { force: true });
      }
    }
    throw new Error(`所有下载源均失败——${failures.join("；")}`);
  } finally {
    installInFlight = false;
  }
}

export function removeManagedFfmpeg(): boolean {
  const managedPath = getManagedFfmpegPath();
  if (!fs.existsSync(managedPath)) {
    return false;
  }
  fs.rmSync(managedPath);
  console.log(`[ffmpeg] 已移除托管版: ${managedPath}`);
  return true;
}
