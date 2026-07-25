import type { MenuItemConstructorOptions } from "electron";
import type { AppCommand, Language } from "@guizhi/shared/types";

export const SUPPORTED_TRAY_MENU_LANGUAGES = [
  "en",
  "zh",
] as const satisfies readonly Language[];

export interface TrayMenuLabels {
  newItem: string;
  showApp: string;
  hideApp: string;
  checkUpdates: string;
  settings: string;
  quitApp: string;
}

const LABELS: Record<Language, TrayMenuLabels> = {
  en: {
    newItem: "New Knowledge Item…",
    showApp: "Show GuiZhi",
    hideApp: "Hide GuiZhi",
    checkUpdates: "Check for Updates…",
    settings: "Settings…",
    quitApp: "Quit GuiZhi",
  },
  zh: {
    newItem: "新建知识条目…",
    showApp: "显示归知",
    hideApp: "隐藏归知",
    checkUpdates: "检查更新…",
    settings: "设置…",
    quitApp: "退出归知",
  },
};

export function normalizeTrayMenuLanguage(locale: string): Language {
  const normalized = locale.trim().toLowerCase();
  return normalized.startsWith("zh") ? "zh" : "en";
}

export function getTrayMenuLabels(locale: string): TrayMenuLabels {
  return LABELS[normalizeTrayMenuLanguage(locale)];
}

interface BuildTrayMenuTemplateOptions {
  agentManagementEnabled: boolean;
  isWindowVisible: boolean;
  labels: TrayMenuLabels;
  onCommand: (command: AppCommand) => void;
  onQuit: () => void;
  onToggleWindow: () => void;
}

export function buildTrayMenuTemplate({
  isWindowVisible,
  labels,
  onCommand,
  onQuit,
  onToggleWindow,
}: BuildTrayMenuTemplateOptions): MenuItemConstructorOptions[] {
  return [
    {
      label: labels.newItem,
      click: () => onCommand({ type: "item:new" }),
    },
    { type: "separator" },
    {
      label: isWindowVisible ? labels.hideApp : labels.showApp,
      click: onToggleWindow,
    },
    {
      label: labels.checkUpdates,
      click: () => onCommand({ type: "updater:open" }),
    },
    {
      label: labels.settings,
      click: () => onCommand({ type: "settings:open" }),
    },
    { type: "separator" },
    { label: labels.quitApp, click: onQuit },
  ];
}
