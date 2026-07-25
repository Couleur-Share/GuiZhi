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
