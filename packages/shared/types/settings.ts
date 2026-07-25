/**
 * Settings type definitions
 */

export type NetworkProxyMode = "system" | "direct" | "manual";

export type NetworkProxyProtocol = "http" | "https" | "socks5";

export interface NetworkProxySettings {
  mode: NetworkProxyMode;
  protocol: NetworkProxyProtocol;
  host: string;
  port: number;
  username: string;
  password: string;
  bypass: string;
}

export interface Settings {
  theme: Theme;
  language: Language;
  autoSave: boolean;
  backgroundImageFileName?: string;
  backgroundImageOpacity?: number;
  backgroundImageBlur?: number;
  sync?: SyncSettings;
  updateChannel?: UpdateChannel;
  // Startup behavior — main process reads these to honor "minimize on launch"
  launchAtStartup?: boolean;
  minimizeOnLaunch?: boolean;
  // Global desktop network proxy.
  networkProxy?: NetworkProxySettings;
  // Security
  security?: {
    masterPasswordConfigured: boolean;
    unlocked: boolean;
  };
  // 本地定时备份（主进程调度器读取）
  backupAutoEnabled?: boolean;
  backupIntervalHours?: number;
  backupKeepCount?: number;
  // 在线视频采集：yt-dlp 可执行文件路径（空 = 查系统 PATH）
  ytDlpPath?: string;
  // 转写前音频转码：ffmpeg 可执行文件路径（空 = 托管版 / 系统 PATH）
  ffmpegPath?: string;
}

export interface SyncSettings {
  enabled: boolean;
  provider: SyncProviderKind;
  endpoint?: string;
  username?: string;
  password?: string;
  remotePath?: string;
  autoSync?: boolean;
  lastSyncAt?: string;
}

export type SyncProviderKind = "manual" | "webdav" | "self-hosted" | "s3";

export type AutoSyncProviderKind = Exclude<SyncProviderKind, "manual">;

export type AutoSyncReason = "startup" | "startup-resume" | "interval";

export type AutoSyncStatus = "success" | "failed" | "skipped";

export interface AutoSyncHistoryEntry {
  id: string;
  provider: AutoSyncProviderKind;
  reason: AutoSyncReason;
  status: AutoSyncStatus;
  startedAt: string;
  finishedAt: string;
  message: string;
  localChanged?: boolean;
}

export type Theme = "light" | "dark" | "system";
export type Language = "en" | "zh";
export type UpdateChannel = "stable" | "preview";

export const DEFAULT_NETWORK_PROXY_SETTINGS: NetworkProxySettings = {
  mode: "system",
  protocol: "http",
  host: "",
  port: 7890,
  username: "",
  password: "",
  bypass: "<local>,localhost,127.0.0.1,::1",
};

export const DEFAULT_SETTINGS: Settings = {
  theme: "system",
  language: "zh",
  autoSave: true,
  backgroundImageOpacity: 0.22,
  backgroundImageBlur: 14,
  sync: {
    enabled: false,
    provider: "manual",
    autoSync: false,
  },
  updateChannel: "stable",
  networkProxy: DEFAULT_NETWORK_PROXY_SETTINGS,
  backupAutoEnabled: true,
  backupIntervalHours: 24,
  backupKeepCount: 10,
};
