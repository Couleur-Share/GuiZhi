import path from "path";
import type {
  Menu,
  MenuItemConstructorOptions,
  NativeImage,
  Tray,
} from "electron";
import type { AppCommand } from "@guizhi/shared/types";

import { loadMacTrayTemplateIcon, resolveMacTrayIconPaths } from "./tray-icon";
import { buildTrayMenuTemplate, getTrayMenuLabels } from "./tray-menu";

interface TrayControllerOptions {
  agentManagementEnabled: boolean;
  buildMenu: (template: MenuItemConstructorOptions[]) => Menu;
  createFromPath: (filePath: string) => NativeImage;
  createTray: (icon: NativeImage) => Tray;
  dirname: string;
  getLocale: () => string;
  getResourcesPath: () => string;
  getStoredLanguage: () => string | null;
  getWindowVisibility: () => boolean;
  isDev: boolean;
  onCommand: (command: AppCommand) => void;
  onQuit: () => void;
  onToggleWindow: () => void;
  platform: NodeJS.Platform;
}

export interface TrayController {
  create: () => void;
  destroy: () => void;
  refresh: () => void;
}

function loadPlatformTrayIcon(options: TrayControllerOptions): NativeImage {
  const resourcesPath = options.getResourcesPath();
  if (options.platform === "darwin") {
    const { templatePath } = resolveMacTrayIconPaths({
      dirname: options.dirname,
      isDev: options.isDev,
      resourcesPath,
    });
    return loadMacTrayTemplateIcon({
      createFromPath: options.createFromPath,
      templatePath,
    });
  }

  const iconPath = options.isDev
    ? path.join(options.dirname, "../../resources/icon.ico")
    : path.join(resourcesPath, "icon.ico");
  let icon = options.createFromPath(iconPath);
  if (icon.isEmpty()) {
    icon = options.createFromPath(
      path.join(resourcesPath, "app.asar.unpacked", "resources", "icon.ico"),
    );
  }
  if (icon.isEmpty()) {
    throw new Error("platform tray icon is missing");
  }
  return icon.resize({ width: 16, height: 16 });
}

function loadFallbackTrayIcon(options: TrayControllerOptions): NativeImage {
  const { fallbackPath } = resolveMacTrayIconPaths({
    dirname: options.dirname,
    isDev: options.isDev,
    resourcesPath: options.getResourcesPath(),
  });
  return options.createFromPath(fallbackPath).resize({ width: 18, height: 18 });
}

export function createTrayController(
  options: TrayControllerOptions,
): TrayController {
  let tray: Tray | null = null;

  const refresh = () => {
    if (!tray) return;
    const locale = options.getStoredLanguage() ?? options.getLocale();
    const template = buildTrayMenuTemplate({
      agentManagementEnabled: options.agentManagementEnabled,
      isWindowVisible: options.getWindowVisibility(),
      labels: getTrayMenuLabels(locale),
      onCommand: options.onCommand,
      onQuit: options.onQuit,
      onToggleWindow: options.onToggleWindow,
    });
    tray.setContextMenu(options.buildMenu(template));
  };

  const create = () => {
    if (tray) return;
    let icon: NativeImage;
    try {
      icon = loadPlatformTrayIcon(options);
    } catch (error) {
      console.error("Failed to load tray icon:", error);
      icon = loadFallbackTrayIcon(options);
    }

    tray = options.createTray(icon);
    tray.setToolTip("归知 GuiZhi");
    refresh();
    if (options.platform === "darwin") {
      tray.on("mouse-down", refresh);
    } else {
      tray.on("click", options.onToggleWindow);
    }
  };

  const destroy = () => {
    tray?.destroy();
    tray = null;
  };

  return { create, destroy, refresh };
}
