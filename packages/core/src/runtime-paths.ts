import os from "os";
import path from "path";

export interface RuntimePathOverrides {
  appDataPath?: string;
  userDataPath?: string;
  productName?: string;
  exePath?: string;
  isPackaged?: boolean;
  platform?: NodeJS.Platform;
}

const DEFAULT_PRODUCT_NAME = "GuiZhi";

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

  return path.join(getAppDataPath(), getProductName());
}

export function getDataDir(): string {
  return path.join(getUserDataPath(), "data");
}

export function getConfigDir(): string {
  return path.join(getUserDataPath(), "config");
}

export function getLogsDir(): string {
  return path.join(getUserDataPath(), "logs");
}
