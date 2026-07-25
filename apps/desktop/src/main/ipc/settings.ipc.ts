import { ipcMain } from 'electron';
import Database from '../database/sqlite';
import { coreAIConfigService } from '@guizhi/core';
import { IPC_CHANNELS } from '@guizhi/shared/constants';
import type { Settings } from '@guizhi/shared/types';
import { DEFAULT_SETTINGS } from '@guizhi/shared/types';
import type {
  CoreAIConfigFile,
  CoreAIModelConfig,
  CoreAIModelRoute,
  CoreAIProviderConfig,
} from '@guizhi/core';
import { applyNetworkProxySettings } from '../services/network-proxy';

export { getMinimizeOnLaunchSetting } from '../settings/settings-readers';

function mergeSharedAIConfig(settings: Settings): void {
  try {
    const aiConfig = coreAIConfigService.read();
    if (aiConfig.providers.length > 0) {
      (settings as any).aiProviders = aiConfig.providers;
    }
    if (aiConfig.models.length > 0) {
      (settings as any).aiModels = aiConfig.models;
      const isChatModel = (model: CoreAIModelConfig) =>
        model.capabilities?.chat !== false;
      const defaultChatModel =
        aiConfig.models.find((model) => isChatModel(model) && model.isDefault) ??
        aiConfig.models.find(isChatModel);
      if (defaultChatModel) {
        (settings as any).aiProvider = defaultChatModel.provider;
        (settings as any).aiApiProtocol = defaultChatModel.apiProtocol;
        (settings as any).aiApiKey = defaultChatModel.apiKey;
        (settings as any).aiApiUrl = defaultChatModel.apiUrl;
        (settings as any).aiModel = defaultChatModel.model;
      }
    }
    if (Object.keys(aiConfig.modelRouteDefaults).length > 0) {
      (settings as any).modelRouteDefaults = aiConfig.modelRouteDefaults;
    }
  } catch (error) {
    console.warn('Failed to merge shared AI config:', error);
  }
}

type DesktopAISettingsPayload = Partial<Settings> & {
  aiProvider?: string;
  aiApiProtocol?: CoreAIProviderConfig['apiProtocol'];
  aiApiKey?: string;
  aiApiUrl?: string;
  aiModel?: string;
  aiProviders?: CoreAIProviderConfig[];
  aiModels?: CoreAIModelConfig[];
  modelRouteDefaults?: Partial<Record<CoreAIModelRoute, string>>;
};

const AI_SETTINGS_KEYS = new Set([
  'aiProvider',
  'aiApiProtocol',
  'aiApiKey',
  'aiApiUrl',
  'aiModel',
  'aiProviders',
  'aiModels',
  'modelRouteDefaults',
]);

export function hasAISettingsPayload(settings: Partial<Settings>): boolean {
  return Object.keys(settings).some((key) => AI_SETTINGS_KEYS.has(key));
}

export function stripAISettingsPayload(settings: Partial<Settings>): Partial<Settings> {
  return Object.fromEntries(
    Object.entries(settings).filter(([key]) => !AI_SETTINGS_KEYS.has(key)),
  ) as Partial<Settings>;
}

function buildLegacyAIModel(
  payload: DesktopAISettingsPayload,
): CoreAIModelConfig | null {
  if (
    !payload.aiProvider?.trim() ||
    !payload.aiApiProtocol ||
    !payload.aiApiKey?.trim() ||
    !payload.aiApiUrl?.trim() ||
    !payload.aiModel?.trim()
  ) {
    return null;
  }

  return {
    id: 'model_legacy_default',
    provider: payload.aiProvider.trim(),
    apiProtocol: payload.aiApiProtocol,
    apiKey: payload.aiApiKey.trim(),
    apiUrl: payload.aiApiUrl.trim(),
    model: payload.aiModel.trim(),
    isDefault: true,
    capabilities: { chat: true },
  };
}

export function mergeAISettingsPayload(
  payload: DesktopAISettingsPayload,
  current: CoreAIConfigFile,
): Pick<CoreAIConfigFile, 'providers' | 'models' | 'modelRouteDefaults'> {
  const providers = Array.isArray(payload.aiProviders)
    ? payload.aiProviders
    : current.providers;
  const models = Array.isArray(payload.aiModels)
    ? payload.aiModels
    : current.models;
  const legacyModel = buildLegacyAIModel(payload);

  return {
    providers,
    models: models.length > 0 || !legacyModel ? models : [legacyModel],
    modelRouteDefaults:
      payload.modelRouteDefaults && typeof payload.modelRouteDefaults === 'object'
        ? payload.modelRouteDefaults
        : current.modelRouteDefaults,
  };
}

function persistSharedAIConfig(newSettings: Partial<Settings>): void {
  if (!hasAISettingsPayload(newSettings)) {
    return;
  }

  const current = coreAIConfigService.read();
  const next = mergeAISettingsPayload(newSettings as DesktopAISettingsPayload, current);
  coreAIConfigService.replace(next);
}

/**
 * Register settings-related IPC handlers
 */
export function registerSettingsIPC(db: Database.Database): void {
  // Get settings
  ipcMain.handle(IPC_CHANNELS.SETTINGS_GET, async () => {
    const settings: Settings = { ...DEFAULT_SETTINGS };

    const stmt = db.prepare('SELECT key, value FROM settings');
    const rows = stmt.all() as { key: string; value: string }[];

    for (const row of rows) {
      try {
        (settings as any)[row.key] = JSON.parse(row.value);
      } catch {
        (settings as any)[row.key] = row.value;
      }
    }

    mergeSharedAIConfig(settings);
    await applyNetworkProxySettings(settings.networkProxy);

    return settings;
  });

  // Save settings
  ipcMain.handle(IPC_CHANNELS.SETTINGS_SET, async (_event, newSettings: Partial<Settings>) => {
    persistSharedAIConfig(newSettings);
    const dbSettings = stripAISettingsPayload(newSettings);
    const stmt = db.prepare(`
      INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)
    `);

    const transaction = db.transaction(() => {
      for (const [key, value] of Object.entries(dbSettings)) {
        stmt.run(key, JSON.stringify(value));
      }
    });

    transaction();
    if (Object.prototype.hasOwnProperty.call(newSettings, 'networkProxy')) {
      await applyNetworkProxySettings(newSettings.networkProxy);
    }
    return true;
  });
}
