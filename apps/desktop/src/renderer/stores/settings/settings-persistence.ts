import type { Settings } from "@guizhi/shared/types";
import { normalizeNetworkProxySettings } from "@guizhi/shared/utils/network-proxy";
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
  normalizeCloseAction,
  normalizeLanguage,
  normalizeShortcutModes,
} from "./settings-normalizers";
import { createDefaultSettingsValues } from "./settings-defaults";
import type { SettingsState } from "./settings-types";

/**
 * 持久化允许出现的字段集合。
 *
 * v0.4.1 删掉 WebDAV / S3 同步后，老用户 localStorage 里仍留着那 36 个字段——
 * 合并是无过滤展开、partialize 又原样写回，脏键的生命周期等于 localStorage
 * 的生命周期。其中 `webdavPassword` / `s3SecretAccessKey` 是明文凭据，而
 * 界面上已经看不到这些配置项，用户无从知晓、更无从删除。
 *
 * 用默认值的键集合做白名单，读写两个方向都过一遍，未知字段自然消失。
 */
let knownSettingsKeys: Set<string> | null = null;

function getKnownSettingsKeys(): Set<string> {
  // 懒求值：默认值会读 i18n.language，模块加载期取值时它可能还没初始化
  knownSettingsKeys ??= new Set(Object.keys(createDefaultSettingsValues()));
  return knownSettingsKeys;
}

function pickKnownSettings<T extends object>(state: T): T {
  const known = getKnownSettingsKeys();
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(state)) {
    // 保留 actions（函数）：它们不参与序列化，但 merge 后的对象仍需可用
    if (known.has(key) || typeof value === "function") {
      result[key] = value;
    }
  }
  return result as T;
}

export function stripEphemeralSettings(state: SettingsState): SettingsState {
  return pickKnownSettings(state);
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

function normalizeMergedState(next: SettingsState): SettingsState {
  normalizeSharedSettingsState(next);
  normalizeMergedPresentationSettings(next);
  return next;
}

export function mergeSettingsState(
  persistedState: unknown,
  currentState: SettingsState,
): SettingsState {
  const next = {
    ...currentState,
    ...pickKnownSettings((persistedState ?? {}) as Partial<SettingsState>),
  };
  return normalizeMergedState(next);
}

export function migrateSettingsState(
  state: unknown,
  _version: number,
): SettingsState {
  if (!state || typeof state !== "object") return state as SettingsState;
  // 版本差异不做逐版本分支：pickKnownSettings 丢弃已下线字段，
  // normalizeMergedState 对任意历史快照做归一
  return normalizeMergedState(pickKnownSettings({ ...(state as SettingsState) }));
}

export function rehydrateSettingsState(
  state: SettingsState | undefined,
  syncSettingsToMain: (settings: Partial<Settings>) => Promise<void>,
): void {
  applyBackgroundImageVars({
    backgroundImageFileName: state?.backgroundImageFileName,
    backgroundImageOpacity: state?.backgroundImageOpacity,
    backgroundImageBlur: state?.backgroundImageBlur,
  });
  const mainProcessSettings: Partial<Settings> = {
    networkProxy: normalizeNetworkProxySettings(state?.networkProxy),
  };
  if (state?.language) {
    mainProcessSettings.language = state.language;
  }
  void syncSettingsToMain(mainProcessSettings);
}
