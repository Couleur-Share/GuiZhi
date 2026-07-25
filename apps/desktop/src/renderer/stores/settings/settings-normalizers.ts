import type { Settings, SyncProviderKind } from "@guizhi/shared/types";
import type { SettingsState, SupportedLanguage } from "./settings-types";
import { SUPPORTED_LANGUAGES } from "./settings-types";

export const DEFAULT_SHORTCUT_MODES: Record<string, "global" | "local"> = {
  showApp: "global",
  newItem: "local",
  search: "local",
  settings: "local",
};

export function normalizeLanguage(lang: string): SupportedLanguage {
  if (SUPPORTED_LANGUAGES.includes(lang as SupportedLanguage)) {
    return lang as SupportedLanguage;
  }
  const lower = (lang || "").toLowerCase();
  if (lower.startsWith("zh")) return "zh";
  return "en";
}

export function normalizeShortcutModes(
  value: unknown,
): Record<string, "global" | "local"> {
  const normalized = { ...DEFAULT_SHORTCUT_MODES };
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return normalized;
  }
  for (const action of Object.keys(DEFAULT_SHORTCUT_MODES)) {
    const mode = (value as Record<string, unknown>)[action];
    if (mode === "global" || mode === "local") normalized[action] = mode;
  }
  return normalized;
}

export function normalizeCloseAction(
  value: unknown,
): SettingsState["closeAction"] {
  return value === "ask" || value === "minimize" || value === "exit"
    ? value
    : "ask";
}

export function normalizeSyncProvider(value: unknown): SyncProviderKind {
  return value === "webdav" || value === "s3" ? value : "manual";
}

function normalizeStartupSyncDelay(value: unknown): number {
  const numeric = typeof value === "number" ? value : Number(value);
  return Math.max(0, Math.min(60, Number.isFinite(numeric) ? numeric : 10));
}

function normalizeAutoSyncInterval(value: unknown): number {
  const numeric = typeof value === "number" ? value : Number(value);
  return Math.max(0, Number.isFinite(numeric) ? numeric : 0);
}

export function normalizeSyncTimingSettings(
  next: Pick<
    SettingsState,
    | "webdavSyncOnStartupDelay"
    | "selfHostedSyncOnStartupDelay"
    | "s3SyncOnStartupDelay"
    | "webdavAutoSyncInterval"
    | "selfHostedAutoSyncInterval"
    | "s3AutoSyncInterval"
  >,
): void {
  next.webdavSyncOnStartupDelay = normalizeStartupSyncDelay(
    next.webdavSyncOnStartupDelay,
  );
  next.selfHostedSyncOnStartupDelay = normalizeStartupSyncDelay(
    next.selfHostedSyncOnStartupDelay,
  );
  next.s3SyncOnStartupDelay = normalizeStartupSyncDelay(
    next.s3SyncOnStartupDelay,
  );
  next.webdavAutoSyncInterval = normalizeAutoSyncInterval(
    next.webdavAutoSyncInterval,
  );
  next.selfHostedAutoSyncInterval = normalizeAutoSyncInterval(
    next.selfHostedAutoSyncInterval,
  );
  next.s3AutoSyncInterval = normalizeAutoSyncInterval(next.s3AutoSyncInterval);
}

export function buildMainProcessSyncSettings(
  provider: SyncProviderKind,
): NonNullable<Settings["sync"]> {
  return {
    enabled: provider !== "manual",
    provider,
    autoSync: provider !== "manual",
  };
}

export function inferLegacySyncProvider(
  state: Partial<SettingsState>,
): SyncProviderKind {
  const active: SyncProviderKind[] = [];
  if (
    state.webdavEnabled &&
    (state.webdavSyncOnStartup ||
      (state.webdavAutoSyncInterval ?? 0) > 0 ||
      state.webdavSyncOnSave)
  )
    active.push("webdav");
  if (
    state.s3StorageEnabled &&
    (state.s3SyncOnStartup ||
      (state.s3AutoSyncInterval ?? 0) > 0 ||
      state.s3SyncOnSave)
  )
    active.push("s3");
  return active.length === 1 ? active[0] : "manual";
}

export function clampSyncProvider(
  provider: SyncProviderKind,
  state: Pick<
    SettingsState,
    "webdavEnabled" | "selfHostedSyncEnabled" | "s3StorageEnabled"
  >,
): SyncProviderKind {
  if (provider === "webdav" && !state.webdavEnabled) return "manual";
  if (provider === "self-hosted") return "manual";
  if (provider === "s3" && !state.s3StorageEnabled) return "manual";
  return provider;
}

export function areStringArraysEqual(left: string[], right: string[]): boolean {
  return (
    left.length === right.length &&
    left.every((entry, index) => entry === right[index])
  );
}
