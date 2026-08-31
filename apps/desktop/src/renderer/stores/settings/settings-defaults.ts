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
    backgroundTasksEnabled: false,
    debugMode: false,
    closeAction: "ask",
    shortcutModes: { ...DEFAULT_SHORTCUT_MODES },
    enableNotifications: true,
    showCopyNotification: true,
    showSaveNotification: true,
  } satisfies Partial<SettingsValues>;
}

function createDefaultWorkspaceValues() {
  return {
    language: normalizeLanguage(i18n.language),
    dataPath: "",
    autoCheckUpdate: true,
    useUpdateMirror: false,
    updateChannel: "stable",
    updateChannelExplicitlySet: false,
    backupAutoEnabled: true,
    backupIntervalHours: 24,
    backupKeepCount: 10,
    ytDlpPath: "",
    ffmpegPath: "",
    transcribeDiarize: false,
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
    ...createDefaultWorkspaceValues(),
    ...createDefaultAiValues(),
  } satisfies Omit<SettingsValues, keyof SettingsActions>;
}
