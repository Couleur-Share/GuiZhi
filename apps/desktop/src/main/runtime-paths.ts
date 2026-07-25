import os from "os";
import path from "path";

import { resolveInitialUserDataPath } from "./data-path";

const DEFAULT_PRODUCT_NAME = "GuiZhi";

export interface RuntimePathOverrides {
  appDataPath?: string;
  userDataPath?: string;
  productName?: string;
  exePath?: string;
  isPackaged?: boolean;
  platform?: NodeJS.Platform;
}

let runtimePathOverrides: RuntimePathOverrides = {};

export function configureRuntimePaths(overrides: RuntimePathOverrides): void {
  runtimePathOverrides = {
    ...runtimePathOverrides,
    ...overrides,
  };
}

export function resetRuntimePaths(): void {
  runtimePathOverrides = {};
}

function getPlatform(): NodeJS.Platform {
  return runtimePathOverrides.platform ?? process.platform;
}

function getProductName(): string {
  return runtimePathOverrides.productName ?? DEFAULT_PRODUCT_NAME;
}

function getDefaultAppDataPath(platform: NodeJS.Platform): string {
  const homeDir = os.homedir();

  if (platform === "darwin") {
    return path.join(homeDir, "Library", "Application Support");
  }

  if (platform === "win32") {
    return process.env.APPDATA || path.join(homeDir, "AppData", "Roaming");
  }

  return process.env.XDG_CONFIG_HOME || path.join(homeDir, ".config");
}

export function getAppDataPath(): string {
  return path.resolve(
    runtimePathOverrides.appDataPath ?? getDefaultAppDataPath(getPlatform()),
  );
}

export function getUserDataPath(): string {
  if (runtimePathOverrides.userDataPath) {
    return path.resolve(runtimePathOverrides.userDataPath);
  }

  const appDataPath = getAppDataPath();
  const defaultUserDataPath = path.join(appDataPath, getProductName());

  return resolveInitialUserDataPath({
    appDataPath,
    defaultUserDataPath,
    exePath: runtimePathOverrides.exePath ?? process.execPath,
    isPackaged: runtimePathOverrides.isPackaged ?? false,
    platform: getPlatform(),
  });
}

export function getDataDir(): string {
  return path.join(getUserDataPath(), "data");
}

/**
 * 归知（Electron 版）主数据库。与旧 .NET 版的 data/guizhi.db 同目录并存，
 * 互不干扰；M6 的迁移器会从 guizhi.db 读取旧数据导入本库。
 */
export function getDatabasePath(): string {
  return path.join(getDataDir(), "knowledge.db");
}

/** 旧 .NET 版归知数据库位置（迁移源，只读）。 */
export function getLegacyDotnetDatabasePath(): string {
  return path.join(getUserDataPath(), "data", "guizhi.db");
}

export function getConfigDir(): string {
  return path.join(getUserDataPath(), "config");
}

export function getLogsDir(): string {
  return path.join(getUserDataPath(), "logs");
}

export function getBackupsDir(): string {
  return path.join(getUserDataPath(), "backups");
}

/** 应用托管的外部工具目录（yt-dlp 等） */
export function getToolsDir(): string {
  return path.join(getUserDataPath(), "tools");
}

export function getAssetsDir(): string {
  return path.join(getDataDir(), "assets");
}

export function getAttachmentsDir(): string {
  return path.join(getAssetsDir(), "attachments");
}

export function getImagesDir(): string {
  return path.join(getAssetsDir(), "images");
}

export function getVideosDir(): string {
  return path.join(getAssetsDir(), "videos");
}
