import { DEFAULT_NETWORK_PROXY_SETTINGS } from "@guizhi/shared/types";
import i18n from "../../i18n";
import {
  DEFAULT_BACKGROUND_IMAGE_BLUR,
  DEFAULT_BACKGROUND_IMAGE_OPACITY,
} from "./settings-appearance";
import {
  DEFAULT_SHORTCUT_MODES,
  normalizeLanguage,
} from "./settings-normalizers";
import type { SettingsActions, SettingsValues } from "./settings-types";

function createDefaultAppearanceValues() {
  return {
    themeMode: "system",
    isDarkMode: true,
    themeColor: "royal-blue",
    themeHue: 220,
    themeSaturation: 70,
    customThemeHex: "#3b82f6",
    settingsUpdatedAt: new Date().toISOString(),
    fontSize: "medium",
    backgroundImageEnabled: true,
    backgroundImageFileName: undefined,
    backgroundImageOpacity: DEFAULT_BACKGROUND_IMAGE_OPACITY,
    backgroundImageBlur: DEFAULT_BACKGROUND_IMAGE_BLUR,
    renderMarkdown: true,
    motionPreference: "standard",
    editorMarkdownPreview: true,
    autoSave: true,
    showLineNumbers: false,
    wikiCompileEnabled: false,
    launchAtStartup: false,
    minimizeOnLaunch: true,
    debugMode: false,
    closeAction: "ask",
    shortcutModes: { ...DEFAULT_SHORTCUT_MODES },
    enableNotifications: true,
    showCopyNotification: true,
    showSaveNotification: true,
  } satisfies Partial<SettingsValues>;
}

function createDefaultWebdavValues() {
  return {
    webdavEnabled: false,
    webdavUrl: "",
    webdavUsername: "",
    webdavPassword: "",
    webdavAutoSync: false,
    webdavSyncOnStartup: true,
    webdavSyncOnStartupDelay: 10,
    webdavAutoSyncInterval: 0,
    webdavSyncOnSave: false,
    webdavIncludeImages: true,
    webdavIncrementalSync: true,
    webdavEncryptionEnabled: false,
    webdavEncryptionPassword: "",
  } satisfies Partial<SettingsValues>;
}

function createDefaultSelfHostedSyncValues() {
  return {
    selfHostedSyncEnabled: false,
    selfHostedSyncUrl: "",
    selfHostedSyncUsername: "",
    selfHostedSyncPassword: "",
    selfHostedSyncOnStartup: false,
    selfHostedSyncOnStartupDelay: 10,
    selfHostedAutoSyncInterval: 0,
  } satisfies Partial<SettingsValues>;
}

function createDefaultS3Values() {
  return {
    s3StorageEnabled: false,
    s3Endpoint: "",
    s3Region: "",
    s3Bucket: "",
    s3AccessKeyId: "",
    s3SecretAccessKey: "",
    s3BackupPrefix: "",
    s3SyncOnStartup: false,
    s3SyncOnStartupDelay: 10,
    s3AutoSyncInterval: 0,
    s3SyncOnSave: false,
    s3IncludeImages: true,
    s3IncrementalSync: true,
    s3EncryptionEnabled: false,
    s3EncryptionPassword: "",
  } satisfies Partial<SettingsValues>;
}

function createDefaultWorkspaceValues() {
  return {
    language: normalizeLanguage(i18n.language),
    dataPath: "",
    syncProvider: "manual",
    autoCheckUpdate: true,
    useUpdateMirror: false,
    updateChannel: "stable",
    updateChannelExplicitlySet: false,
    backupAutoEnabled: true,
    backupIntervalHours: 24,
    backupKeepCount: 10,
    ytDlpPath: "",
    ffmpegPath: "",
    networkProxy: { ...DEFAULT_NETWORK_PROXY_SETTINGS },
  } satisfies Partial<SettingsValues>;
}

function createDefaultAiValues() {
  return {
    aiProvider: "openai",
    aiApiProtocol: "openai",
    aiApiKey: "",
    aiApiUrl: "",
    aiModel: "gpt-4o",
    aiProviders: [],
    aiModels: [],
    scenarioModelDefaults: {},
    modelRouteDefaults: {},
  } satisfies Partial<SettingsValues>;
}

export function createDefaultSettingsValues(): SettingsValues {
  return {
    ...createDefaultAppearanceValues(),
    ...createDefaultWebdavValues(),
    ...createDefaultSelfHostedSyncValues(),
    ...createDefaultS3Values(),
    ...createDefaultWorkspaceValues(),
    ...createDefaultAiValues(),
  } satisfies Omit<SettingsValues, keyof SettingsActions>;
}
