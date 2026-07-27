import { BrowserWindow, ipcMain, app, shell } from "electron";
import type { UpdateInfo as ElectronUpdateInfo } from "electron-updater";
import { autoUpdater } from "electron-updater";
import fs from "fs";
import path from "path";
import https from "https";
import { getHttpRequestAgent } from "./services/network-proxy";
import { createBackupSafe } from "./services/backup";
import { logStartupEvent } from "./diagnostic-log";
import { compareVersions, isPrereleaseVersion } from "../utils/version";
import {
  extractChangelogRange,
  extractLatestChangelogSection,
  parseChangelogVersions,
} from "../utils/changelog";

// Simplified update info type (for IPC transmission)
// 简化的更新信息类型（用于 IPC 传输）
interface SimpleUpdateInfo {
  version: string;
  releaseNotes?: string;
  releaseDate?: string;
}

interface ProgressInfo {
  percent: number;
  bytesPerSecond: number;
  total: number;
  transferred: number;
}

export type MacInstallSource = "direct" | "homebrew" | "unknown";

type UpdateChannel = "stable" | "preview";

/** 检查来源：manual 为用户点开更新弹窗，其余为渲染进程的自动检查 */
const UPDATE_CHECK_TRIGGERS = [
  "manual",
  "startup",
  "interval",
  "visibility",
] as const;
type UpdateCheckTrigger = (typeof UPDATE_CHECK_TRIGGERS)[number];

/** 自动检查被跳过的原因，与 renderer/services/update-check.ts 保持一致 */
const AUTO_SKIP_REASONS = [
  "disabled",
  "hidden",
  "offline",
  "in-flight",
  "cooldown",
] as const;

interface UpdateRequestOptions {
  useMirror?: boolean;
  channel?: UpdateChannel;
  trigger?: UpdateCheckTrigger;
}

const OFFICIAL_REPO = {
  provider: "github" as const,
  owner: "Couleur-Share",
  repo: "GuiZhi",
  releaseType: "release" as const,
};

function normalizeUpdateOptions(
  input?: boolean | UpdateRequestOptions,
): Required<UpdateRequestOptions> {
  if (typeof input === "boolean") {
    return { useMirror: input, channel: "stable", trigger: "manual" };
  }
  const trigger = UPDATE_CHECK_TRIGGERS.includes(
    input?.trigger as UpdateCheckTrigger,
  )
    ? (input?.trigger as UpdateCheckTrigger)
    : "manual";
  return {
    useMirror: Boolean(input?.useMirror),
    channel: input?.channel === "preview" ? "preview" : "stable",
    trigger,
  };
}

/**
 * 把每次检查的结果写进 startup.log。
 *
 * 自动检查在界面上是无声的（没有新版本或失败都不改变任何 UI），没有这条日志
 * 就无法在事后回答「那次到底有没有检查、结果是什么」。
 */
function logUpdateCheck(entry: {
  trigger: UpdateCheckTrigger;
  channel?: UpdateChannel;
  useMirror?: boolean;
  result: "available" | "not-available" | "error" | "dev-disabled";
  version?: string;
  durationMs?: number;
  error?: string;
}): void {
  logStartupEvent({
    event: "updater:check",
    currentVersion: app.getVersion(),
    ...entry,
    error: entry.error ? entry.error.slice(0, 300) : undefined,
  });
}

function readCheckedVersion(result: unknown): string | undefined {
  if (!result || typeof result !== "object") {
    return undefined;
  }
  const updateInfo = (result as { updateInfo?: { version?: unknown } })
    .updateInfo;
  return typeof updateInfo?.version === "string" ? updateInfo.version : undefined;
}

function getFeedSuffix(channel: UpdateChannel, releaseTag?: string): string {
  if (channel === "preview" && releaseTag) {
    return `download/${releaseTag}`;
  }
  return "latest/download";
}

function getMirrorSources(
  channel: UpdateChannel,
  releaseTag?: string,
): string[] {
  const suffix = getFeedSuffix(channel, releaseTag);
  return [
    `https://ghfast.top/https://github.com/Couleur-Share/GuiZhi/releases/${suffix}`,
    `https://gh-proxy.com/https://github.com/Couleur-Share/GuiZhi/releases/${suffix}`,
    `https://hub.gitmirror.com/https://github.com/Couleur-Share/GuiZhi/releases/${suffix}`,
    `https://cors.isteed.cc/github.com/Couleur-Share/GuiZhi/releases/${suffix}`,
  ];
}

function getOfficialFeedUrl(
  channel: UpdateChannel,
  releaseTag?: string,
): string {
  return `https://github.com/${OFFICIAL_REPO.owner}/${OFFICIAL_REPO.repo}/releases/${getFeedSuffix(channel, releaseTag)}`;
}

function getOfficialFeedConfig(channel: UpdateChannel, releaseTag?: string) {
  if (channel === "preview") {
    return {
      provider: "generic" as const,
      channel: getGenericChannelName(),
      url: getOfficialFeedUrl(channel, releaseTag),
    };
  }
  return OFFICIAL_REPO;
}

function applyMirrorDownloadSettings(useMirror: boolean) {
  const updater = autoUpdater as unknown as {
    useMultipleRangeRequest?: boolean;
  };
  updater.useMultipleRangeRequest = !useMirror;
}

interface FeedContext {
  channel: UpdateChannel;
  releaseTag?: string;
}

let lastFeedContext: FeedContext = { channel: "stable" };

async function fetchLatestPreviewReleaseTag(): Promise<string | null> {
  return await new Promise<string | null>((resolve, reject) => {
    const request = https.get(
      {
        hostname: "api.github.com",
        path: `/repos/${OFFICIAL_REPO.owner}/${OFFICIAL_REPO.repo}/releases?per_page=20`,
        agent: getHttpRequestAgent("https://api.github.com"),
        headers: {
          Accept: "application/vnd.github+json",
          "User-Agent": "GuiZhi-Updater",
        },
      },
      (response) => {
        let body = "";
        response.setEncoding("utf8");

        response.on("data", (chunk: string) => {
          body += chunk;
        });

        response.on("end", () => {
          if (response.statusCode !== 200) {
            reject(
              new Error(
                `Preview release lookup failed with HTTP ${response.statusCode || 0}`,
              ),
            );
            return;
          }

          try {
            const releases = JSON.parse(body) as Array<{
              draft?: boolean;
              prerelease?: boolean;
              tag_name?: string;
            }>;
            const latestPreview = releases.find(
              (release) =>
                release.prerelease === true &&
                release.draft !== true &&
                typeof release.tag_name === "string" &&
                release.tag_name.length > 0,
            );
            resolve(latestPreview?.tag_name || null);
          } catch (error) {
            reject(error instanceof Error ? error : new Error(String(error)));
          }
        });
      },
    );

    request.on("error", (error) => reject(error));
    request.setTimeout(15000, () => {
      request.destroy(new Error("Preview release lookup timed out"));
    });
  });
}

async function resolveFeedContext(
  channel: UpdateChannel,
): Promise<FeedContext> {
  if (channel !== "preview") {
    return { channel };
  }

  // Preview channel is intentionally prerelease-only. Stable releases are not
  // fallback candidates; users return to stable updates by switching channels.
  const releaseTag = await fetchLatestPreviewReleaseTag();
  if (!releaseTag) {
    throw new Error(
      "Update check failed: No published prerelease preview release is currently available.",
    );
  }

  return { channel, releaseTag };
}

function applyUpdaterPreferences(channel: UpdateChannel): void {
  autoUpdater.allowPrerelease = channel === "preview";
  autoUpdater.allowDowngrade = false;
}

function isRemoteVersionNewer(
  remoteVersion: string,
  currentVersion: string,
): boolean {
  return compareVersions(remoteVersion, currentVersion) > 0;
}

function getGenericChannelName(): string | undefined {
  if (process.platform === "win32" && process.arch === "arm64") {
    return "latest-arm64";
  }

  return undefined;
}

function filterDowngradeStatus(info: ElectronUpdateInfo): boolean {
  const currentVersion = app.getVersion();
  const isNewer = isRemoteVersionNewer(info.version, currentVersion);

  if (!isNewer) {
    console.info(
      `[Updater] Ignoring downgrade/non-upgrade candidate ${info.version} for current ${currentVersion}`,
    );
  }

  return isNewer;
}

function applyFeedContext(
  useMirror: boolean,
  context: FeedContext,
  mirrorUrl?: string,
): void {
  if (useMirror) {
    autoUpdater.setFeedURL({
      provider: "generic",
      channel: getGenericChannelName(),
      url:
        mirrorUrl || getMirrorSources(context.channel, context.releaseTag)[0],
    });
    return;
  }

  autoUpdater.setFeedURL(
    getOfficialFeedConfig(context.channel, context.releaseTag),
  );
}

function toCheckResult(result: unknown): {
  success: boolean;
  result?: unknown;
  updateAvailable?: boolean;
} {
  const isUpdateAvailable = Boolean(
    result &&
    typeof result === "object" &&
    "isUpdateAvailable" in result &&
    (result as { isUpdateAvailable?: boolean }).isUpdateAvailable,
  );

  return {
    success: true,
    result,
    updateAvailable: isUpdateAvailable,
  };
}

export { compareVersions };

// Read changelog for specified version range from CHANGELOG.md
// 从 CHANGELOG.md 读取指定版本区间的更新日志
export function getChangelogForVersionRange(
  newVersion: string,
  currentVersion: string,
): string {
  try {
    const isDev = process.env.NODE_ENV === "development" || !app.isPackaged;
    let changelogPath: string;

    if (isDev) {
      changelogPath = path.join(__dirname, "../../../../CHANGELOG.md");
    } else {
      // Check if resourcesPath exists (may be undefined in test environment)
      // 检查 resourcesPath 是否存在（在测试环境中可能为 undefined）
      if (!process.resourcesPath) {
        return "";
      }
      // After packaging, CHANGELOG.md is in resources directory
      // 打包后，CHANGELOG.md 在 resources 目录
      changelogPath = path.join(process.resourcesPath, "CHANGELOG.md");
      // If not exists, try app.asar.unpacked
      // 如果不存在，尝试 app.asar.unpacked
      if (!fs.existsSync(changelogPath)) {
        changelogPath = path.join(
          process.resourcesPath,
          "app.asar.unpacked",
          "CHANGELOG.md",
        );
      }
      // Still not exists, try app directory
      // 还不存在，尝试 app 目录
      if (!fs.existsSync(changelogPath)) {
        changelogPath = path.join(app.getAppPath(), "CHANGELOG.md");
      }
    }

    if (!fs.existsSync(changelogPath)) {
      console.warn("[Updater] CHANGELOG.md not found at:", changelogPath);
      console.warn("[Updater] isDev:", isDev);
      console.warn("[Updater] __dirname:", __dirname);
      console.warn("[Updater] resourcesPath:", process.resourcesPath);
      console.warn("[Updater] appPath:", app.getAppPath());
      return "";
    }

    console.log("[Updater] Reading CHANGELOG from:", changelogPath);

    return extractChangelogRange(
      fs.readFileSync(changelogPath, "utf-8"),
      newVersion,
      currentVersion,
    );
  } catch (error) {
    console.error("Failed to read CHANGELOG.md:", error);
    return "";
  }
}

// 更新清单里的 releaseNotes 可能是字符串，也可能是 { note } 数组
function readManifestNotes(info: ElectronUpdateInfo): string {
  if (typeof info.releaseNotes === "string") {
    return info.releaseNotes;
  }

  if (Array.isArray(info.releaseNotes)) {
    return info.releaseNotes
      .map((entry) => entry.note ?? "")
      .filter(Boolean)
      .join("\n\n");
  }

  return "";
}

// Convert from electron-updater's UpdateInfo to simplified format
// 从 electron-updater 的 UpdateInfo 转换为简化格式
function toSimpleInfo(info: ElectronUpdateInfo): SimpleUpdateInfo {
  const currentVersion = app.getVersion();

  // 优先从随包分发的 CHANGELOG.md 取版本区间，跨版本升级能看到中间版本
  let releaseNotes = getChangelogForVersionRange(info.version, currentVersion);

  // 读不到时回退到更新清单里的说明；若那里恰好是多版本文档，同样只取区间
  if (!releaseNotes) {
    const manifestNotes = readManifestNotes(info);
    releaseNotes =
      parseChangelogVersions(manifestNotes).length > 1
        ? extractChangelogRange(manifestNotes, info.version, currentVersion) ||
          extractLatestChangelogSection(manifestNotes)
        : manifestNotes;
  }

  return {
    version: info.version,
    releaseNotes,
    releaseDate: info.releaseDate,
  };
}

let mainWindow: BrowserWindow | null = null;
let lastPercent = 0; // Track last progress to prevent regression
// 跟踪上次进度，防止进度回退

function isMacPlatform(): boolean {
  return process.platform === "darwin";
}

function normalizeRealPath(inputPath: string): string {
  try {
    return fs.realpathSync(inputPath);
  } catch {
    return inputPath;
  }
}

export function detectMacInstallSource(
  executablePath: string = process.execPath,
): MacInstallSource {
  if (!isMacPlatform()) {
    return "unknown";
  }

  const resolvedPath = normalizeRealPath(executablePath);
  const normalizedPath = resolvedPath.replace(/\\/g, "/");

  if (
    normalizedPath.includes("/Caskroom/") ||
    normalizedPath.startsWith("/opt/homebrew/Caskroom/") ||
    normalizedPath.startsWith("/usr/local/Caskroom/")
  ) {
    return "homebrew";
  }

  return "direct";
}

function getMacInstallSource(): MacInstallSource {
  return detectMacInstallSource(process.execPath);
}

function getHomebrewUpgradeMessage(): string {
  return (
    "This GuiZhi build appears to be installed via Homebrew. " +
    "Please upgrade it with 'brew upgrade --cask guizhi' instead of using the in-app DMG updater."
  );
}

async function openPathOrError(targetPath: string): Promise<string | null> {
  const error = await shell.openPath(targetPath);
  return typeof error === "string" && error.trim().length > 0 ? error : null;
}

export interface UpdateStatus {
  status:
    | "checking"
    | "available"
    | "not-available"
    | "downloading"
    | "downloaded"
    | "error";
  info?: SimpleUpdateInfo;
  progress?: ProgressInfo;
  error?: string;
}

export function initUpdater(win: BrowserWindow) {
  mainWindow = win;

  // Disable auto download, let user choose
  // 禁用自动下载，让用户选择
  autoUpdater.autoDownload = false;
  // Disable auto-install on quit across all platforms.
  //
  // Historical context: v0.5.2 enabled this on Windows (`!isMac`), but when
  // combined with the auto-recovery code path in renderer (which called
  // `app.relaunch()+quit()` after copying a recovered database), a pending
  // electron-updater install was triggered on every quit. That install
  // silently re-applied the same NSIS package — and because the package
  // itself could lead the app back into an empty-database state, the loop
  // repeated indefinitely.
  //
  // Requiring an explicit user click to install is worth the minor UX cost.
  // See AGENTS.md §12 and the v0.5.3 regression analysis.
  autoUpdater.autoInstallOnAppQuit = false;

  applyUpdaterPreferences(
    isPrereleaseVersion(app.getVersion()) ? "preview" : "stable",
  );
  console.log(
    `[Updater] Platform: ${process.platform}, Arch: ${process.arch}, currentVersion: ${app.getVersion()}`,
  );

  // Update check error
  // 检查更新出错
  autoUpdater.on("error", (error) => {
    console.error("Update error:", error);
    let message = (error && (error as Error).message) || String(error);
    // Handle 404 error for missing yml files
    // 处理找不到 yml 文件的 404 错误
    if (message.includes("404") || message.includes("Cannot find")) {
      message =
        "Update check failed: Cannot find update manifest file in the latest release.\n" +
        "更新检查失败：无法在最新版本中找到更新配置文件。\n" +
        "请前往 GitHub Releases 页面手动下载安装。";
    } else if (message.includes("ZIP file not provided")) {
      message =
        "Auto update requires ZIP installer, but current Release does not have corresponding ZIP file. Please go to GitHub Releases page to download manually, or wait for next version to fix auto update.";
      // 自动更新需要 ZIP 安装包，但当前版本的 Release 中没有对应的 ZIP 文件。请前往 GitHub Releases 页面手动下载安装，或等待下一个版本修复自动更新。
    }
    if (
      message.toLowerCase().includes("sha512") &&
      message.toLowerCase().includes("mismatch")
    ) {
      console.log(
        "[Updater] SHA512 mismatch, temp directory:",
        app.getPath("temp"),
      );
      message =
        "SHA512 校验失败\n\n" +
        "这通常是由于 CDN 缓存不一致或网络问题导致的。\n" +
        "文件可能已下载完成，您可以点击下方按钮打开文件夹手动尝试安装。";
    }
    sendStatusToWindow({
      status: "error",
      error: message,
    });
  });

  // Checking for update
  // 检查更新中
  autoUpdater.on("checking-for-update", () => {
    console.info("Checking for update...");
    sendStatusToWindow({ status: "checking" });
  });

  // Update available
  // 有可用更新
  autoUpdater.on("update-available", (info) => {
    if (!filterDowngradeStatus(info)) {
      sendStatusToWindow({
        status: "not-available",
        info: toSimpleInfo(info),
      });
      return;
    }

    console.info("Update available:", info.version);
    sendStatusToWindow({
      status: "available",
      info: toSimpleInfo(info),
    });
  });

  // No update available
  // 没有可用更新
  autoUpdater.on("update-not-available", (info) => {
    console.info("Update not available, current version is latest");
    sendStatusToWindow({
      status: "not-available",
      info: toSimpleInfo(info),
    });
  });

  // Download progress
  // 下载进度
  autoUpdater.on("download-progress", (progress: ProgressInfo) => {
    // Prevent progress regression (electron-updater resets progress when downloading multiple files)
    // 防止进度回退（electron-updater 下载多个文件时会重置进度）
    if (progress.percent < lastPercent && lastPercent < 99) {
      // Keep last progress when regression occurs
      // 进度回退时，保持上次进度
      console.info(
        `Download progress (ignored regression): ${progress.percent.toFixed(2)}% -> keeping ${lastPercent.toFixed(2)}%`,
      );
      return;
    }
    lastPercent = progress.percent;
    console.info(`Download progress: ${progress.percent.toFixed(2)}%`);
    sendStatusToWindow({
      status: "downloading",
      progress,
    });
  });

  // Download completed
  // 下载完成
  autoUpdater.on("update-downloaded", (info) => {
    console.info("Update downloaded:", info.version);
    sendStatusToWindow({
      status: "downloaded",
      info: toSimpleInfo(info),
    });
  });
}

function sendStatusToWindow(status: UpdateStatus) {
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send("updater:status", status);
  }
}

// Register IPC handlers
// 注册 IPC 处理程序
const UPDATER_IPC_CHANNELS = [
  "updater:version",
  "updater:installSource",
  "updater:check",
  "updater:logAutoSkip",
  "updater:download",
  "updater:install",
  "updater:platform",
  "updater:openReleases",
  "updater:openDownloadedUpdate",
] as const;

export function registerUpdaterIPC() {
  const isDev = process.env.NODE_ENV === "development" || !app.isPackaged;

  if (typeof ipcMain.removeHandler === "function") {
    for (const channel of UPDATER_IPC_CHANNELS) {
      ipcMain.removeHandler(channel);
    }
  }

  // Get current version - always available
  // 获取当前版本 - 总是可用
  ipcMain.handle("updater:version", () => {
    return app.getVersion();
  });

  ipcMain.handle("updater:installSource", () => {
    return isMacPlatform() ? getMacInstallSource() : "unknown";
  });

  // 检查更新
  // Check for updates - respect user's mirror preference
  ipcMain.handle(
    "updater:check",
    async (_event, request?: boolean | UpdateRequestOptions) => {
      const { useMirror, channel, trigger } = normalizeUpdateOptions(request);

      if (isDev) {
        logUpdateCheck({ trigger, channel, result: "dev-disabled" });
        // devDisabled 让渲染进程区分「真的失败」与「开发模式本就不检查」，
        // 否则每次 electron:dev 启动都会弹一次自动检查失败
        return {
          success: false,
          devDisabled: true,
          error: "Update check disabled in development mode",
        };
      }

      const startedAt = Date.now();
      applyUpdaterPreferences(channel);
      applyMirrorDownloadSettings(useMirror);
      let context: FeedContext;

      try {
        context = await resolveFeedContext(channel);
        lastFeedContext = context;
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        logUpdateCheck({
          trigger,
          channel,
          useMirror,
          result: "error",
          durationMs: Date.now() - startedAt,
          error: message,
        });
        return { success: false, error: message };
      }

      // If mirror is enabled, use mirror sources directly
      // 如果启用了镜像，直接使用镜像源（不先尝试官方）
      if (useMirror) {
        for (const mirrorUrl of getMirrorSources(
          context.channel,
          context.releaseTag,
        )) {
          try {
            console.log(
              `[Updater] Using ${context.channel} mirror for check: ${mirrorUrl}`,
            );
            applyFeedContext(true, context, mirrorUrl);
            const result = await autoUpdater.checkForUpdates();
            console.log(`[Updater] Mirror check succeeded: ${mirrorUrl}`);
            const checkResult = toCheckResult(result);
            logUpdateCheck({
              trigger,
              channel: context.channel,
              useMirror,
              result: checkResult.updateAvailable ? "available" : "not-available",
              version: readCheckedVersion(result),
              durationMs: Date.now() - startedAt,
            });
            return checkResult;
          } catch (mirrorError) {
            console.warn(`[Updater] Mirror check failed: ${mirrorUrl}`);
          }
        }
        // All mirrors failed
        const mirrorError =
          "All mirror sources failed. Please try disabling mirror acceleration.";
        logUpdateCheck({
          trigger,
          channel: context.channel,
          useMirror,
          result: "error",
          durationMs: Date.now() - startedAt,
          error: mirrorError,
        });
        return { success: false, error: mirrorError };
      }

      // Mirror disabled, use official source
      // 未启用镜像，使用官方源
      try {
        console.log(
          `[Updater] Using official ${context.channel} source for check`,
        );
        applyFeedContext(false, context);
        const result = await autoUpdater.checkForUpdates();
        const checkResult = toCheckResult(result);
        logUpdateCheck({
          trigger,
          channel: context.channel,
          useMirror,
          result: checkResult.updateAvailable ? "available" : "not-available",
          version: readCheckedVersion(result),
          durationMs: Date.now() - startedAt,
        });
        return checkResult;
      } catch (officialError) {
        const errMsg =
          (officialError as Error).message || String(officialError);
        logUpdateCheck({
          trigger,
          channel: context.channel,
          useMirror,
          result: "error",
          durationMs: Date.now() - startedAt,
          error: errMsg,
        });
        return { success: false, error: `Update check failed: ${errMsg}` };
      }
    },
  );

  // 记录被跳过的自动检查（窗口隐藏 / 离线 / 开关关闭等）
  // 渲染进程只上报枚举值，不接受自由文本，避免日志被写入任意内容
  ipcMain.handle(
    "updater:logAutoSkip",
    (_event, payload?: { trigger?: unknown; reason?: unknown }) => {
      const trigger = UPDATE_CHECK_TRIGGERS.includes(
        payload?.trigger as UpdateCheckTrigger,
      )
        ? (payload?.trigger as UpdateCheckTrigger)
        : null;
      const reason = AUTO_SKIP_REASONS.includes(
        payload?.reason as (typeof AUTO_SKIP_REASONS)[number],
      )
        ? (payload?.reason as (typeof AUTO_SKIP_REASONS)[number])
        : null;
      if (!trigger || !reason) {
        return { success: false };
      }

      logStartupEvent({ event: "updater:auto_skip", trigger, reason });
      return { success: true };
    },
  );

  // Start downloading update
  // 开始下载更新 - respect user's mirror preference
  ipcMain.handle(
    "updater:download",
    async (_event, request?: boolean | UpdateRequestOptions) => {
      if (isDev) {
        return {
          success: false,
          error: "Download disabled in development mode",
        };
      }

      const { useMirror, channel } = normalizeUpdateOptions(request);
      applyUpdaterPreferences(channel);
      lastPercent = 0;
      let context: FeedContext;

      try {
        context =
          lastFeedContext.channel === channel
            ? lastFeedContext
            : await resolveFeedContext(channel);
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }

      lastFeedContext = context;

      if (isMacPlatform() && getMacInstallSource() === "homebrew") {
        return {
          success: false,
          error: getHomebrewUpgradeMessage(),
          installSource: "homebrew",
        };
      }

      // All direct installs use electron-updater's verified package workflow.
      applyMirrorDownloadSettings(useMirror);

      // If mirror is enabled, use mirror sources directly
      // 如果启用了镜像，直接使用镜像源
      if (useMirror) {
        for (const mirrorUrl of getMirrorSources(
          context.channel,
          context.releaseTag,
        )) {
          try {
            console.log(
              `[Updater] Using ${context.channel} mirror for download: ${mirrorUrl}`,
            );
            applyFeedContext(true, context, mirrorUrl);
            await autoUpdater.downloadUpdate();
            return { success: true };
          } catch (mirrorError) {
            console.warn(`[Updater] Mirror download failed: ${mirrorUrl}`);
            lastPercent = 0; // Reset progress for next attempt
          }
        }
        // All mirrors failed
        return {
          success: false,
          error:
            "All mirror sources failed. Please try disabling mirror acceleration.",
        };
      }

      // Mirror disabled, use official source
      // 未启用镜像，使用官方源
      try {
        console.log(
          `[Updater] Using official ${context.channel} source for download`,
        );
        applyFeedContext(false, context);
        await autoUpdater.downloadUpdate();
        return { success: true };
      } catch (officialError) {
        const errMsg =
          (officialError as Error).message || String(officialError);
        return { success: false, error: `Download update failed: ${errMsg}` };
      }
    },
  );

  // Install update and restart
  // 安装更新并重启：先做一次升级前数据快照（尽力而为，失败不阻断安装）
  ipcMain.handle("updater:install", async () => {
    if (isDev) {
      return { success: false, error: "Install disabled in development mode" };
    }

    try {
      if (isMacPlatform() && getMacInstallSource() === "homebrew") {
        return {
          success: false,
          manual: true,
          installSource: "homebrew",
          error: getHomebrewUpgradeMessage(),
        };
      }

      createBackupSafe("pre-update");
      autoUpdater.quitAndInstall(false, true);
      return {
        success: true,
        manual: false,
      };
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      console.error("[Updater] Failed to install update:", error);
      return {
        success: false,
        error: `Install update failed: ${message}`,
      };
    }
  });

  // Get platform info
  // 获取平台信息
  ipcMain.handle("updater:platform", () => {
    return process.platform;
  });

  // Open GitHub Releases page
  // 打开 GitHub Releases 页面
  ipcMain.handle("updater:openReleases", () => {
    void shell.openExternal("https://github.com/Couleur-Share/GuiZhi/releases");
  });

  ipcMain.handle("updater:openDownloadedUpdate", async () => {
    // Reveal the package only when electron-updater exposes a durable path.
    const installerPath = (autoUpdater as unknown as { installerPath?: string })
      .installerPath;
    if (installerPath && fs.existsSync(installerPath)) {
      shell.showItemInFolder(installerPath);
      return { success: true, path: installerPath };
    }

    const downloadDir = app.getPath("downloads");
    const openError = await openPathOrError(downloadDir);
    const baseError = installerPath
      ? "Downloaded update file is missing"
      : "No downloaded update file is available";
    return {
      success: false,
      path: downloadDir,
      error: openError
        ? `${baseError}; failed to open Downloads folder: ${openError}`
        : baseError,
    };
  });
}
