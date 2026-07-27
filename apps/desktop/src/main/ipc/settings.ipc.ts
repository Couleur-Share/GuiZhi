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
import { isAcceptableBinaryPath } from '../services/picked-binary-paths';

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

/**
 * 允许在 settings 表里读写的键。
 *
 * settings 表同时装着主进程的内部状态（master_password、备份时间戳等），
 * 早先 get 是整表回传、set 是遍历入参对象写入，两个方向都没有边界：
 * 机密会流进渲染进程，渲染进程也能往表里写任意键——包括会被 spawn 的
 * 工具路径。这里用 Settings 的字段集合做双向白名单。
 *
 * `security` 不在其中：它是 securityStatus() 的派生值，不接受写入。
 * AI 相关键走 config/ai-models.json，由 stripAISettingsPayload 单独剥离。
 */
const PERSISTED_SETTINGS_KEYS = new Set<string>([
  "theme",
  "language",
  "autoSave",
  "backgroundImageFileName",
  "backgroundImageOpacity",
  "backgroundImageBlur",
  "updateChannel",
  "launchAtStartup",
  "minimizeOnLaunch",
  "networkProxy",
  "backupAutoEnabled",
  "backupIntervalHours",
  "backupKeepCount",
  "ytDlpPath",
  "ffmpegPath",
  "transcribeDiarize",
]);

export function isPersistedSettingKey(key: string): boolean {
  return PERSISTED_SETTINGS_KEYS.has(key);
}

/** 会被主进程 spawn 的可执行文件路径，只接受文件选择器返回值 */
const EXECUTABLE_PATH_KEYS = new Set(["ytDlpPath", "ffmpegPath"]);

/** 过滤掉不允许写入的键，返回保留项与被拒项 */
export function filterWritableSettings(
  settings: Partial<Settings>,
  isAcceptablePath: (value: unknown) => boolean,
): { accepted: Partial<Settings>; rejected: string[] } {
  const accepted: Record<string, unknown> = {};
  const rejected: string[] = [];

  for (const [key, value] of Object.entries(settings)) {
    if (!PERSISTED_SETTINGS_KEYS.has(key)) {
      rejected.push(key);
      continue;
    }
    if (EXECUTABLE_PATH_KEYS.has(key) && !isAcceptablePath(value)) {
      rejected.push(key);
      continue;
    }
    accepted[key] = value;
  }

  return { accepted: accepted as Partial<Settings>, rejected };
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
      // 表里还存着 master_password 等主进程内部状态，不能随设置一起回传
      if (!isPersistedSettingKey(row.key)) {
        continue;
      }
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
    const { accepted, rejected } = filterWritableSettings(
      stripAISettingsPayload(newSettings),
      isAcceptableBinaryPath,
    );
    if (rejected.length > 0) {
      console.warn(`[settings] 拒绝写入非白名单键: ${rejected.join(", ")}`);
    }
    const stmt = db.prepare(`
      INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)
    `);

    const transaction = db.transaction(() => {
      for (const [key, value] of Object.entries(accepted)) {
        stmt.run(key, JSON.stringify(value));
      }
    });

    transaction();
    if (Object.prototype.hasOwnProperty.call(accepted, 'networkProxy')) {
      await applyNetworkProxySettings(accepted.networkProxy);
    }
    return true;
  });
}
