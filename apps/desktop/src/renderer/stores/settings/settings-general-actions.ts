import { normalizeNetworkProxySettings } from "@guizhi/shared/utils/network-proxy";
import { changeLanguage } from "../../i18n";
import { isPrereleaseVersion } from "../../../utils/version";
import type {
  SettingsActionContext,
  SettingsActionGroup,
} from "./settings-action-context";
import {
  applyBackgroundImageVars,
  clampBackgroundImageBlur,
  clampBackgroundImageOpacity,
  FONT_SIZES,
  hexToHs,
  MORANDI_THEMES,
  normalizeBackgroundImageFileName,
  normalizeFontSize,
  normalizeMotionPreference,
  normalizeThemeMode,
} from "./settings-appearance";
import {
  normalizeCloseAction,
  normalizeLanguage,
} from "./settings-normalizers";

type GeneralActionKey =
  | "setThemeMode"
  | "setThemeColor"
  | "setCustomThemeHex"
  | "setRenderMarkdown"
  | "setMotionPreference"
  | "setEditorMarkdownPreview"
  | "setFontSize"
  | "applyBackgroundImageSelection"
  | "setBackgroundImageEnabled"
  | "setBackgroundImageFileName"
  | "setBackgroundImageOpacity"
  | "setBackgroundImageBlur"
  | "setAutoSave"
  | "setShowLineNumbers"
  | "setWikiCompileEnabled"
  | "setLaunchAtStartup"
  | "setMinimizeOnLaunch"
  | "setCloseAction"
  | "setDebugMode"
  | "setShortcutMode"
  | "setEnableNotifications"
  | "setShowCopyNotification"
  | "setShowSaveNotification"
  | "setLanguage"
  | "setDataPath"
  | "setAutoCheckUpdate"
  | "setUseUpdateMirror"
  | "setUpdateChannel"
  | "inferUpdateChannel"
  | "setBackupAutoEnabled"
  | "setBackupIntervalHours"
  | "setBackupKeepCount"
  | "setYtDlpPath"
  | "setFfmpegPath"
  | "setNetworkProxy"
  | "applyTheme";

function setDocumentThemeColor(hue: number, saturation: number): void {
  document.documentElement.style.setProperty("--theme-hue", String(hue));
  document.documentElement.style.setProperty(
    "--theme-saturation",
    String(saturation),
  );
}

function setDocumentFontSize(fontSize: string): void {
  const font = FONT_SIZES.find((candidate) => candidate.id === fontSize);
  if (font) {
    document.documentElement.style.setProperty(
      "--base-font-size",
      `${font.value}px`,
    );
  }
}

function resolveIsDark(themeMode: "system" | "light" | "dark"): boolean {
  return themeMode === "system"
    ? window.matchMedia("(prefers-color-scheme: dark)").matches
    : themeMode === "dark";
}

function createThemeModeActions(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    setThemeMode: (mode) => {
      const themeMode = normalizeThemeMode(mode);
      const isDarkMode = resolveIsDark(themeMode);
      setTouched({ themeMode, isDarkMode });
      document.documentElement.classList.toggle("dark", isDarkMode);
    },
    setThemeColor: (colorId) => {
      const theme =
        colorId === "custom"
          ? { id: "custom", ...hexToHs(get().customThemeHex) }
          : MORANDI_THEMES.find((candidate) => candidate.id === colorId);
      if (!theme) return;
      setTouched({
        themeColor: theme.id,
        themeHue: theme.hue,
        themeSaturation: theme.saturation,
      });
      setDocumentThemeColor(theme.hue, theme.saturation);
    },
  } satisfies SettingsActionGroup<"setThemeMode" | "setThemeColor">;
}

function createCustomThemeActions(context: SettingsActionContext) {
  const { setTouched } = context;
  return {
    setCustomThemeHex: (hex) => {
      const color = hexToHs(hex);
      setTouched({
        customThemeHex: `#${hex.replace(/^#/, "")}`,
        themeColor: "custom",
        themeHue: color.hue,
        themeSaturation: color.saturation,
      });
      setDocumentThemeColor(color.hue, color.saturation);
    },
    setRenderMarkdown: (enabled) => setTouched({ renderMarkdown: enabled }),
    setMotionPreference: (preference) =>
      setTouched({ motionPreference: normalizeMotionPreference(preference) }),
    setEditorMarkdownPreview: (enabled) =>
      setTouched({ editorMarkdownPreview: enabled }),
    setFontSize: (size) => {
      const fontSize = normalizeFontSize(size);
      setTouched({ fontSize });
      setDocumentFontSize(fontSize);
    },
  } satisfies SettingsActionGroup<
    | "setCustomThemeHex"
    | "setRenderMarkdown"
    | "setMotionPreference"
    | "setEditorMarkdownPreview"
    | "setFontSize"
  >;
}

function applyCurrentBackgroundImage(context: SettingsActionContext): void {
  const state = context.get();
  applyBackgroundImageVars({
    backgroundImageFileName: state.backgroundImageFileName,
    backgroundImageOpacity: state.backgroundImageOpacity,
    backgroundImageBlur: state.backgroundImageBlur,
  });
}

function createBackgroundImageActions(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    applyBackgroundImageSelection: (fileName) => {
      const backgroundImageFileName =
        normalizeBackgroundImageFileName(fileName);
      if (!backgroundImageFileName) return;
      const { backgroundImageOpacity, backgroundImageBlur } = get();
      setTouched({
        backgroundImageEnabled: true,
        backgroundImageFileName,
        backgroundImageOpacity,
        backgroundImageBlur,
      });
      applyBackgroundImageVars({
        backgroundImageFileName,
        backgroundImageOpacity,
        backgroundImageBlur,
      });
    },
    setBackgroundImageEnabled: (backgroundImageEnabled) => {
      if (get().backgroundImageEnabled !== backgroundImageEnabled) {
        setTouched({ backgroundImageEnabled });
      }
    },
    setBackgroundImageFileName: (fileName) => {
      const backgroundImageFileName =
        normalizeBackgroundImageFileName(fileName);
      if (get().backgroundImageFileName === backgroundImageFileName) return;
      setTouched({ backgroundImageFileName });
      applyCurrentBackgroundImage(context);
    },
  } satisfies SettingsActionGroup<
    | "applyBackgroundImageSelection"
    | "setBackgroundImageEnabled"
    | "setBackgroundImageFileName"
  >;
}

function createBackgroundImageTuningActions(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    setBackgroundImageOpacity: (opacity) => {
      const backgroundImageOpacity = clampBackgroundImageOpacity(opacity);
      if (get().backgroundImageOpacity === backgroundImageOpacity) return;
      setTouched({ backgroundImageOpacity });
      applyCurrentBackgroundImage(context);
    },
    setBackgroundImageBlur: (blur) => {
      const backgroundImageBlur = clampBackgroundImageBlur(blur);
      if (get().backgroundImageBlur === backgroundImageBlur) return;
      setTouched({ backgroundImageBlur });
      applyCurrentBackgroundImage(context);
    },
  } satisfies SettingsActionGroup<
    "setBackgroundImageOpacity" | "setBackgroundImageBlur"
  >;
}

function createAppearanceActions(context: SettingsActionContext) {
  return {
    ...createThemeModeActions(context),
    ...createCustomThemeActions(context),
    ...createBackgroundImageActions(context),
    ...createBackgroundImageTuningActions(context),
  };
}

function createEditorActions(context: SettingsActionContext) {
  const { setTouched } = context;
  return {
    setAutoSave: (autoSave) => setTouched({ autoSave }),
    setShowLineNumbers: (showLineNumbers) => setTouched({ showLineNumbers }),
    setWikiCompileEnabled: (wikiCompileEnabled) =>
      setTouched({ wikiCompileEnabled }),
    setEnableNotifications: (enableNotifications) =>
      setTouched({ enableNotifications }),
    setShowCopyNotification: (showCopyNotification) =>
      setTouched({ showCopyNotification }),
    setShowSaveNotification: (showSaveNotification) =>
      setTouched({ showSaveNotification }),
  } satisfies SettingsActionGroup<
    | "setAutoSave"
    | "setShowLineNumbers"
    | "setWikiCompileEnabled"
    | "setEnableNotifications"
    | "setShowCopyNotification"
    | "setShowSaveNotification"
  >;
}

function createDesktopIntegrationActions(context: SettingsActionContext) {
  const { get, setTouched, syncSettingsToMain } = context;
  return {
    setLaunchAtStartup: (launchAtStartup) => {
      setTouched({ launchAtStartup });
      window.electron?.setAutoLaunch?.(launchAtStartup, get().minimizeOnLaunch);
      void syncSettingsToMain({ launchAtStartup });
    },
    setMinimizeOnLaunch: (minimizeOnLaunch) => {
      setTouched({ minimizeOnLaunch });
      window.electron?.setMinimizeToTray?.(minimizeOnLaunch);
      if (get().launchAtStartup) {
        window.electron?.setAutoLaunch?.(true, minimizeOnLaunch);
      }
      void syncSettingsToMain({ minimizeOnLaunch });
    },
    setCloseAction: (action) => {
      const closeAction = normalizeCloseAction(action);
      setTouched({ closeAction });
      window.electron?.setCloseAction?.(closeAction);
    },
    setDebugMode: (debugMode) => {
      setTouched({ debugMode });
      window.electron?.setDebugMode?.(debugMode);
    },
  } satisfies SettingsActionGroup<
    | "setLaunchAtStartup"
    | "setMinimizeOnLaunch"
    | "setCloseAction"
    | "setDebugMode"
  >;
}

function createShortcutActions(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    setShortcutMode: (key, mode) => {
      const shortcutModes = { ...get().shortcutModes, [key]: mode };
      setTouched({ shortcutModes });
      window.electron?.setShortcutMode?.(shortcutModes);
    },
  } satisfies SettingsActionGroup<"setShortcutMode">;
}

function changeLanguageSafely(language: string): void {
  void changeLanguage(language).catch((error) => {
    console.error("Failed to change language:", error);
  });
}

function createReleaseActions(context: SettingsActionContext) {
  const { get, setTouched, syncSettingsToMain } = context;
  return {
    setLanguage: (language) => {
      const normalized = normalizeLanguage(language);
      setTouched({ language: normalized });
      changeLanguageSafely(normalized);
      void syncSettingsToMain({ language: normalized });
    },
    setDataPath: (dataPath) => setTouched({ dataPath }),
    setAutoCheckUpdate: (autoCheckUpdate) => setTouched({ autoCheckUpdate }),
    setUseUpdateMirror: (useUpdateMirror) => setTouched({ useUpdateMirror }),
    setUpdateChannel: (updateChannel) =>
      setTouched({ updateChannel, updateChannelExplicitlySet: true }),
    inferUpdateChannel: (version) => {
      const state = get();
      if (state.updateChannelExplicitlySet) return;
      const updateChannel = isPrereleaseVersion(version) ? "preview" : "stable";
      if (state.updateChannel !== updateChannel) setTouched({ updateChannel });
    },
  } satisfies SettingsActionGroup<
    | "setLanguage"
    | "setDataPath"
    | "setAutoCheckUpdate"
    | "setUseUpdateMirror"
    | "setUpdateChannel"
    | "inferUpdateChannel"
  >;
}

function clampBackupNumber(value: number, fallback: number, min: number, max: number): number {
  if (!Number.isFinite(value)) {
    return fallback;
  }
  return Math.min(Math.max(Math.round(value), min), max);
}

/** 备份设置写通到主进程 settings 表，供自动备份调度器读取 */
function createBackupActions(context: SettingsActionContext) {
  const { setTouched, syncSettingsToMain } = context;
  return {
    setBackupAutoEnabled: (backupAutoEnabled) => {
      setTouched({ backupAutoEnabled });
      void syncSettingsToMain({ backupAutoEnabled });
    },
    setBackupIntervalHours: (hours) => {
      const backupIntervalHours = clampBackupNumber(hours, 24, 1, 168);
      setTouched({ backupIntervalHours });
      void syncSettingsToMain({ backupIntervalHours });
    },
    setBackupKeepCount: (count) => {
      const backupKeepCount = clampBackupNumber(count, 10, 1, 100);
      setTouched({ backupKeepCount });
      void syncSettingsToMain({ backupKeepCount });
    },
    setYtDlpPath: (path) => {
      const ytDlpPath = path.trim();
      setTouched({ ytDlpPath });
      void syncSettingsToMain({ ytDlpPath });
    },
    setFfmpegPath: (path) => {
      const ffmpegPath = path.trim();
      setTouched({ ffmpegPath });
      void syncSettingsToMain({ ffmpegPath });
    },
  } satisfies SettingsActionGroup<
    | "setBackupAutoEnabled"
    | "setBackupIntervalHours"
    | "setBackupKeepCount"
    | "setYtDlpPath"
    | "setFfmpegPath"
  >;
}

function createNetworkProxyActions(context: SettingsActionContext) {
  const { get, setTouched, syncSettingsToMain } = context;
  return {
    setNetworkProxy: (updates) => {
      const networkProxy = normalizeNetworkProxySettings({
        ...get().networkProxy,
        ...updates,
      });
      setTouched({ networkProxy });
      void syncSettingsToMain({ networkProxy });
    },
  } satisfies SettingsActionGroup<"setNetworkProxy">;
}

function applyDesktopIntegrations(context: SettingsActionContext): void {
  const state = context.get();
  if (state.minimizeOnLaunch) window.electron?.setMinimizeToTray?.(true);
  if (state.debugMode) window.electron?.setDebugMode?.(true);
  if (state.closeAction) window.electron?.setCloseAction?.(state.closeAction);
}

function createApplyThemeAction(context: SettingsActionContext) {
  const { get } = context;
  return {
    applyTheme: () => {
      const state = get();
      document.documentElement.classList.toggle(
        "dark",
        resolveIsDark(state.themeMode),
      );
      setDocumentThemeColor(state.themeHue, state.themeSaturation);
      setDocumentFontSize(state.fontSize);
      applyBackgroundImageVars(state);
      applyDesktopIntegrations(context);
    },
  } satisfies SettingsActionGroup<"applyTheme">;
}

export function createGeneralSettingsActions(
  context: SettingsActionContext,
): SettingsActionGroup<GeneralActionKey> {
  return Object.assign(
    {},
    createAppearanceActions(context),
    createEditorActions(context),
    createDesktopIntegrationActions(context),
    createShortcutActions(context),
    createReleaseActions(context),
    createBackupActions(context),
    createNetworkProxyActions(context),
    createApplyThemeAction(context),
  );
}
