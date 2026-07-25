import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { Settings } from "@guizhi/shared/types";
import type { AIProtocol } from "@guizhi/shared/types";
import { normalizeNetworkProxySettings } from "@guizhi/shared/utils/network-proxy";
import { createAISettingsActions } from "./settings/settings-ai-actions";
import {
  attachProviderIdsToAIModels,
  buildAISettingsSyncPayload,
  normalizeAIProtocol,
  normalizeModelRouteDefaults,
  normalizePersistedAIModels,
  normalizePersistedAIProviders,
} from "./settings/settings-ai";
import { createDefaultSettingsValues } from "./settings/settings-defaults";
import { createGeneralSettingsActions } from "./settings/settings-general-actions";
import {
  mergeSettingsState,
  migrateSettingsState,
  rehydrateSettingsState,
  stripEphemeralSettings,
} from "./settings/settings-persistence";
import { applyBackgroundImageVars } from "./settings/settings-appearance";
import type {
  AIModelConfig,
  AIProviderConfig,
  ModelRouteDefaults,
  SettingsState,
} from "./settings/settings-types";

export {
  FONT_SIZES,
  getRenderedBackgroundImageBlur,
  getRenderedBackgroundImageOpacity,
  MORANDI_THEMES,
} from "./settings/settings-appearance";
export { AI_SCENARIO_MODEL_ROUTE } from "./settings/settings-ai";
export type {
  AIModelCapabilities,
  AIModelConfig,
  AIModelRoute,
  AIProviderConfig,
  AIUsageScenario,
  ChatModelParams,
  ModelRouteDefaults,
  ScenarioModelDefaults,
  SupportedLanguage,
  ThemeMode,
} from "./settings/settings-types";

function syncSettingsToMain(settings: Partial<Settings>): Promise<void> {
  if (typeof window === "undefined") return Promise.resolve();
  const setSettings = window.api?.settings?.set;
  if (typeof setSettings !== "function") return Promise.resolve();
  return setSettings(settings).catch((error: unknown) =>
    console.warn("Failed to sync settings to main process:", error),
  );
}

export const useSettingsStore = create<SettingsState>()(
  persist(
    (set, get) => {
      const setTouched = (partial: Partial<SettingsState>) =>
        set({
          ...partial,
          settingsUpdatedAt: new Date().toISOString(),
        } as SettingsState);
      const commitAISettings = (partial: Partial<SettingsState>) => {
        setTouched(partial);
        void syncSettingsToMain(buildAISettingsSyncPayload(get()));
      };
      const context = {
        set,
        get,
        setTouched,
        commitAISettings,
        syncSettingsToMain,
      };
      return {
        ...createDefaultSettingsValues(),
        ...createGeneralSettingsActions(context),
        ...createAISettingsActions(context),
      };
    },
    {
      name: "guizhi-settings",
      version: 2,
      partialize: stripEphemeralSettings,
      merge: mergeSettingsState,
      migrate: migrateSettingsState,
      onRehydrateStorage: () => (state, error) => {
        if (error) {
          console.error("Failed to rehydrate settings store:", error);
          return;
        }
        queueMicrotask(() => {
          rehydrateSettingsState(state, syncSettingsToMain);
        });
      },
    },
  ),
);

applyBackgroundImageVars(useSettingsStore.getState());

/** Load settings owned by the Electron main process into the renderer store. */
export async function loadSettingsFromMainProcess(): Promise<void> {
  if (typeof window === "undefined") return;
  const settings = await window.api?.settings?.get?.();
  if (!settings) return;
  const aiSettings = settings as Settings & {
    aiProvider?: string;
    aiApiProtocol?: AIProtocol;
    aiApiKey?: string;
    aiApiUrl?: string;
    aiModel?: string;
    aiProviders?: AIProviderConfig[];
    aiModels?: AIModelConfig[];
    modelRouteDefaults?: ModelRouteDefaults;
  };
  const state = useSettingsStore.getState();
  const launchAtStartup =
    typeof settings.launchAtStartup === "boolean"
      ? settings.launchAtStartup
      : state.launchAtStartup;
  const minimizeOnLaunch =
    typeof settings.minimizeOnLaunch === "boolean"
      ? settings.minimizeOnLaunch
      : state.minimizeOnLaunch;
  const aiProviders = Array.isArray(aiSettings.aiProviders)
    ? normalizePersistedAIProviders(aiSettings.aiProviders)
    : state.aiProviders;
  const aiModels = Array.isArray(aiSettings.aiModels)
    ? attachProviderIdsToAIModels(
        aiProviders,
        normalizePersistedAIModels(aiSettings.aiModels),
      )
    : state.aiModels;
  const aiProvider =
    typeof aiSettings.aiProvider === "string"
      ? aiSettings.aiProvider
      : state.aiProvider;
  const aiApiUrl =
    typeof aiSettings.aiApiUrl === "string"
      ? aiSettings.aiApiUrl
      : state.aiApiUrl;
  const networkProxy = normalizeNetworkProxySettings(
    settings.networkProxy ?? state.networkProxy,
  );

  useSettingsStore.setState({
    launchAtStartup,
    minimizeOnLaunch,
    backupAutoEnabled:
      typeof settings.backupAutoEnabled === "boolean"
        ? settings.backupAutoEnabled
        : state.backupAutoEnabled,
    backupIntervalHours:
      typeof settings.backupIntervalHours === "number"
        ? settings.backupIntervalHours
        : state.backupIntervalHours,
    backupKeepCount:
      typeof settings.backupKeepCount === "number"
        ? settings.backupKeepCount
        : state.backupKeepCount,
    ytDlpPath:
      typeof settings.ytDlpPath === "string"
        ? settings.ytDlpPath
        : state.ytDlpPath,
    ffmpegPath:
      typeof settings.ffmpegPath === "string"
        ? settings.ffmpegPath
        : state.ffmpegPath,
    aiProvider,
    aiApiProtocol: normalizeAIProtocol(
      aiSettings.aiApiProtocol ?? state.aiApiProtocol,
      aiProvider,
      aiApiUrl,
    ),
    aiApiKey:
      typeof aiSettings.aiApiKey === "string"
        ? aiSettings.aiApiKey
        : state.aiApiKey,
    aiApiUrl,
    aiModel:
      typeof aiSettings.aiModel === "string"
        ? aiSettings.aiModel
        : state.aiModel,
    aiProviders,
    aiModels,
    modelRouteDefaults: normalizeModelRouteDefaults(
      aiSettings.modelRouteDefaults ?? state.modelRouteDefaults,
    ),
    networkProxy,
  });

  if (typeof settings.launchAtStartup !== "boolean")
    void syncSettingsToMain({ launchAtStartup });
  if (typeof settings.minimizeOnLaunch !== "boolean")
    void syncSettingsToMain({ minimizeOnLaunch });
  if (!settings.networkProxy) void syncSettingsToMain({ networkProxy });
}
