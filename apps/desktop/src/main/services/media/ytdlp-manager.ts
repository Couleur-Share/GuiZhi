/**
 * yt-dlp 工具管理：应用内一键安装（官方源 + 镜像回退、走网络代理）、
 * 状态探测与移除。托管副本位于 %userData%/tools/。
 *
 * 解析时的生效顺序：自定义路径 → 内置托管版 → 系统 PATH。
 */
import { spawn } from "child_process";
import fs from "fs";
import path from "path";
import type {
  ToolUpdateCheck,
  YtDlpDownloadProgress,
  YtDlpStatus,
} from "@guizhi/shared/types";
import { getToolsDir } from "../../runtime-paths";
import { fetchWithNetworkProxy } from "../network-proxy";
import {
  downloadToFile,
  fetchExpectedSha256,
  sha256File,
} from "./tool-download";

const VERSION_PROBE_TIMEOUT_MS = 10_000;
const LATEST_VERSION_TIMEOUT_MS = 6_000;

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
  const official = `https://github.com/yt-dlp/yt-dlp/releases/latest/download/${getYtDlpAssetName(platform)}`;
  return [
    official,
    `https://ghfast.top/${official}`,
    `https://gh-proxy.com/${official}`,
    `https://hub.gitmirror.com/${official}`,
  ];
}

/** 下载的资产文件名（校验清单按它匹配行） */
export function getYtDlpAssetName(
  platform: NodeJS.Platform = process.platform,
): string {
  return platform === "win32"
    ? "yt-dlp.exe"
    : platform === "darwin"
      ? "yt-dlp_macos"
      : "yt-dlp_linux";
}

/**
 * 官方发布的 SHA2-256SUMS。
 *
 * 二进制可能来自第三方 GitHub 代理，校验和优先走官方源：只要两者不同源，
 * 代理替换掉的文件就对不上。官方不可达时才退到镜像（聊胜于无）。
 */
export function getYtDlpChecksumUrls(): string[] {
  const official =
    "https://github.com/yt-dlp/yt-dlp/releases/latest/download/SHA2-256SUMS";
  return [
    official,
    `https://ghfast.top/${official}`,
    `https://gh-proxy.com/${official}`,
    `https://hub.gitmirror.com/${official}`,
  ];
}

/**
 * GitHub 的 /releases/latest 会 302 到 /releases/tag/<tag>，
 * 而 yt-dlp 的 tag 就是 `--version` 输出的版本号（如 2026.07.04），
 * 因此不下载资产也能判断本地是不是最新。镜像源与下载同一组。
 */
export function getYtDlpLatestReleaseUrls(): string[] {
  const official = "https://github.com/yt-dlp/yt-dlp/releases/latest";
  return [
    official,
    `https://ghfast.top/${official}`,
    `https://gh-proxy.com/${official}`,
    `https://hub.gitmirror.com/${official}`,
  ];
}

/** 从 302 的 Location（或跟随重定向后的终点 URL）里取出 release tag */
export function parseReleaseTag(location: string | null | undefined): string | null {
  const match = location?.match(/\/releases\/tag\/([^/?#]+)/);
  return match ? decodeURIComponent(match[1]) : null;
}

export type LatestVersionFetch = () => Promise<string | null>;

/**
 * 查询远端最新版本号：只读 302 的 Location，不拉正文。
 * 任一源命中即返回；全部失败返回 null（调用方照常走下载流程）。
 */
export async function fetchLatestYtDlpVersion(
  fetchImpl: typeof fetchWithNetworkProxy = fetchWithNetworkProxy,
): Promise<string | null> {
  for (const url of getYtDlpLatestReleaseUrls()) {
    try {
      const response = await fetchImpl(url, {
        redirect: "manual",
        signal: AbortSignal.timeout(LATEST_VERSION_TIMEOUT_MS),
      });
      // 镜像可能自行跟随重定向并回正文，尽早断流
      await response.body?.cancel();
      const tag =
        parseReleaseTag(response.headers.get("location")) ??
        parseReleaseTag(response.url);
      if (tag) {
        return tag;
      }
    } catch {
      // 换下一个源
    }
  }
  console.warn("[ytdlp] 无法获取远端最新版本号，将直接下载");
  return null;
}

/**
 * 检查内置版是否有更新——只查版本号，不下载。
 * 版本比较沿用应用自身更新器的写法（数值感知），避免把 nightly 判成需要回退。
 */
export async function checkYtDlpUpdate(
  current: string | null,
  fetchLatest: LatestVersionFetch = fetchLatestYtDlpVersion,
): Promise<ToolUpdateCheck> {
  const latest = await fetchLatest();
  const updateAvailable = Boolean(
    latest &&
      current &&
      latest.localeCompare(current, undefined, { numeric: true }) > 0,
  );
  return { current, latest, updateAvailable };
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
  // win / mac / linux 均有官方资产；与 ffmpeg / funasr 不同，这里恒为 true
  const installSupported = true;

  const custom = configuredPath?.trim();
  if (custom) {
    const version = await probe(custom);
    return {
      installed: version !== null,
      source: version !== null ? "custom" : null,
      version: version ?? undefined,
      path: custom,
      managedPath,
      installSupported,
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
        installSupported,
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
      installSupported,
    };
  }

  return { installed: false, source: null, managedPath, installSupported };
}

let installInFlight = false;

/**
 * 下载并安装托管版 yt-dlp：逐源尝试 → 临时文件校验可执行 → 原子替换。
 * 返回安装的版本号；全部下载源失败时抛错。
 *
 * 这里不做「是否需要更新」的判断——那是 checkYtDlpUpdate 的职责，
 * UI 只在确认有新版本（或用户主动要求安装）时才调到这里。
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

    // 校验和先取：拿不到也要继续（正需要镜像的网络里官方多半也不通），
    // 但拿到了就必须对上——「跑起来能打印版本号」拦不住被替换的可执行文件
    const expectedSha256 = await fetchExpectedSha256(
      getYtDlpChecksumUrls(),
      getYtDlpAssetName(),
    );
    if (!expectedSha256) {
      console.warn("[ytdlp] 未能获取官方校验和，本次安装跳过哈希校验");
    }

    const failures: string[] = [];
    for (const url of getYtDlpDownloadUrls()) {
      try {
        console.log(`[ytdlp] 开始下载: ${url}`);
        await downloadToFile(url, tempPath, onProgress);
        if (expectedSha256) {
          const actual = await sha256File(tempPath);
          if (actual !== expectedSha256) {
            throw new Error(
              `校验和不匹配（期望 ${expectedSha256.slice(0, 12)}…，实际 ${actual.slice(0, 12)}…）`,
            );
          }
        }
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
