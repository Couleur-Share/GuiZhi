import type { Settings } from "@guizhi/shared/types";
import { normalizeNetworkProxySettings } from "@guizhi/shared/utils/network-proxy";
import type { StoreApi } from "zustand";
import {
  applyBackgroundImageVars,
  clampBackgroundImageBlur,
  clampBackgroundImageOpacity,
  DEFAULT_BACKGROUND_IMAGE_BLUR,
  DEFAULT_BACKGROUND_IMAGE_OPACITY,
  normalizeAppearanceSettings,
  normalizeBackgroundImageFileName,
} from "./settings-appearance";
import {
  normalizeAIModelDefaults,
  normalizeAIProtocol,
  normalizePersistedAIModels,
  normalizePersistedAIProviders,
} from "./settings-ai";
import {
  buildMainProcessSyncSettings,
  clampSyncProvider,
  normalizeCloseAction,
  normalizeLanguage,
  normalizeShortcutModes,
  normalizeSyncProvider,
  normalizeSyncTimingSettings,
} from "./settings-normalizers";
import type { SettingsState } from "./settings-types";

export function stripEphemeralSettings(state: SettingsState): SettingsState {
  return state;
}

function normalizeSharedSettingsState(next: SettingsState): void {
  normalizeAppearanceSettings(next, normalizeLanguage);
  next.closeAction = normalizeCloseAction(next.closeAction);
  next.aiApiProtocol = normalizeAIProtocol(
    next.aiApiProtocol,
    next.aiProvider,
    next.aiApiUrl,
  );
  next.aiProviders = normalizePersistedAIProviders(next.aiProviders);
  next.aiModels = normalizePersistedAIModels(next.aiModels);
  normalizeAIModelDefaults(next);
  next.shortcutModes = normalizeShortcutModes(next.shortcutModes);
  next.networkProxy = normalizeNetworkProxySettings(next.networkProxy);
}

function normalizeMergedPresentationSettings(next: SettingsState): void {
  if (typeof next.backgroundImageEnabled !== "boolean") {
    next.backgroundImageEnabled = true;
  }
  // Markdown 渲染视图是默认行为：该设置目前没有 UI 入口，
  // 历史持久化里的 false 都是旧默认值残留，一律翻转（加设置界面时移除此强制）
  next.editorMarkdownPreview = true;
  next.backgroundImageFileName = normalizeBackgroundImageFileName(
    next.backgroundImageFileName,
  );
  next.backgroundImageOpacity = clampBackgroundImageOpacity(
    typeof next.backgroundImageOpacity === "number"
      ? next.backgroundImageOpacity
      : DEFAULT_BACKGROUND_IMAGE_OPACITY,
  );
  next.backgroundImageBlur = clampBackgroundImageBlur(
    typeof next.backgroundImageBlur === "number"
      ? next.backgroundImageBlur
      : DEFAULT_BACKGROUND_IMAGE_BLUR,
  );
  if (typeof next.updateChannelExplicitlySet !== "boolean") {
    next.updateChannelExplicitlySet = false;
  }
}

function normalizeSyncProviderState(next: SettingsState): void {
  normalizeSyncTimingSettings(next);
  next.syncProvider = clampSyncProvider(
    normalizeSyncProvider(next.syncProvider),
    {
      webdavEnabled: next.webdavEnabled === true,
      selfHostedSyncEnabled: next.selfHostedSyncEnabled === true,
      s3StorageEnabled: next.s3StorageEnabled === true,
    },
  );
}

function normalizeMergedState(next: SettingsState): SettingsState {
  normalizeSharedSettingsState(next);
  normalizeMergedPresentationSettings(next);
  normalizeSyncProviderState(next);
  return next;
}

export function mergeSettingsState(
  persistedState: unknown,
  currentState: SettingsState,
): SettingsState {
  const next = {
    ...currentState,
    ...(persistedState as Partial<SettingsState>),
  };
  return normalizeMergedState(next);
}

export function migrateSettingsState(
  state: unknown,
  _version: number,
): SettingsState {
  if (!state || typeof state !== "object") return state as SettingsState;
  // 版本差异不做逐版本分支：normalizeMergedState 对任意历史快照做归一
  return normalizeMergedState({ ...(state as SettingsState) });
}

export function rehydrateSettingsState(
  state: SettingsState | undefined,
  setState: StoreApi<SettingsState>["setState"],
  syncSettingsToMain: (settings: Partial<Settings>) => Promise<void>,
): void {
  const syncProvider = clampSyncProvider(
    normalizeSyncProvider(state?.syncProvider),
    {
      webdavEnabled: state?.webdavEnabled === true,
      selfHostedSyncEnabled: state?.selfHostedSyncEnabled === true,
      s3StorageEnabled: state?.s3StorageEnabled === true,
    },
  );
  if (state && state.syncProvider !== syncProvider) setState({ syncProvider });
  applyBackgroundImageVars({
    backgroundImageFileName: state?.backgroundImageFileName,
    backgroundImageOpacity: state?.backgroundImageOpacity,
    backgroundImageBlur: state?.backgroundImageBlur,
  });
  const mainProcessSettings: Partial<Settings> = {
    networkProxy: normalizeNetworkProxySettings(state?.networkProxy),
    sync: buildMainProcessSyncSettings(syncProvider),
  };
  if (state?.language) {
    mainProcessSettings.language = state.language;
  }
  void syncSettingsToMain(mainProcessSettings);
}
