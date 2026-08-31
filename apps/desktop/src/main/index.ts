import {
  app,
  BrowserWindow,
  shell,
  ipcMain,
  dialog,
  Notification,
  Tray,
  Menu,
  nativeImage,
  session,
  protocol,
  screen,
} from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import path from "path";
import fs from "fs";
import Database from "./database/sqlite";
import { initDatabase, closeDatabase } from "./database";
import { registerAllIPC } from "./ipc";
import { getMinimizeOnLaunchSetting } from "./settings/settings-readers";
import { readLanguageSetting } from "./settings/language-setting";
import { createMenu } from "./menu";
import {
  registerShortcuts,
  registerShortcutsIPC,
  toggleWindowForShowApp,
} from "./shortcuts";
import { initUpdater, registerUpdaterIPC } from "./updater";
import {
  applyE2ESeed,
  configureE2ETestProfile,
  isE2EEnabled,
  shouldUseDevServer,
} from "./testing/e2e";
import {
  shouldPlaceWindowOffscreen,
  showWindowOffscreen,
} from "./testing/window-mode";
import {
  getHistoricalDefaultUserDataPath,
  inspectDataPath,
  isLinkSafeDataPathRoot,
  readConfiguredDataPath,
  resolveInitialUserDataPath,
  writeConfiguredDataPath,
} from "./data-path";
import {
  configureRuntimePaths,
  getImagesDir,
  getVideosDir,
} from "./runtime-paths";
import { registerAppRuntimeIPC } from "./ipc/app-runtime.ipc";
import { logStartupEvent, scrubPath } from "./diagnostic-log";
import { openDirectoryPath } from "./shell-open-path";
import { shouldOpenStartupDevTools } from "./devtools-policy";
import { resolveLocalMediaProtocolPath } from "./local-media-protocol";
import {
  applySessionSecurity,
  applyWebContentsSecurity,
} from "./window-security";
import { applyNetworkProxySettings } from "./services/network-proxy";
import {
  setAutoBackupNotifier,
} from "./services/backup";
import { createTrayController } from "./tray-controller";
import { BackgroundJobRuntime } from "./services/background-jobs";
import { dispatchTrayAppCommand } from "./tray-command-dispatcher";
import {
  attachWindowStatePersistence,
  DEFAULT_WINDOW_BOUNDS,
  MIN_WINDOW_BOUNDS,
  readWindowLaunchState,
} from "./window-state";

let mainWindow: BrowserWindow | null = null;
let minimizeToTray = false;
// Database instance (module-level for access in createWindow)
// 数据库实例（模块级变量，供 createWindow 访问）
let appDb: Database.Database | null = null;
let backgroundJobRuntime: BackgroundJobRuntime | null = null;
let isQuitting = false;
let quitCleanupRunning = false;
let quitCleanupComplete = false;
// Close action: 'ask' = ask every time, 'minimize' = minimize to tray, 'exit' = exit directly
// 关闭行为: 'ask' = 每次询问, 'minimize' = 最小化到托盘, 'exit' = 直接退出
let closeAction: "ask" | "minimize" | "exit" = "ask";
// Whether we are waiting for the user to choose a close behavior
// 是否正在等待用户选择关闭行为
let pendingCloseAction = false;
let isDebugMode = false;

async function applyStoredNetworkProxySettings(
  db: Database.Database,
): Promise<void> {
  const row = db
    .prepare("SELECT value FROM settings WHERE key = ?")
    .get("networkProxy") as { value: string } | undefined;
  let value: unknown;
  if (row) {
    try {
      value = JSON.parse(row.value);
    } catch {
      value = undefined;
    }
  }
  await applyNetworkProxySettings(value);
}

export function __setMainWindowForTests(windowRef: BrowserWindow | null) {
  mainWindow = windowRef;
}

export function sendToMainWindow(channel: string, ...args: unknown[]) {
  if (
    mainWindow &&
    !mainWindow.isDestroyed() &&
    !mainWindow.webContents.isDestroyed() &&
    mainWindow.webContents.mainFrame &&
    !mainWindow.webContents.mainFrame.isDestroyed()
  ) {
    try {
      mainWindow.webContents.send(channel, ...args);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (
        message.includes("Render frame was disposed") ||
        message.includes("WebFrameMain could be accessed")
      ) {
        return;
      }
      throw error;
    }
  }
}

export function emitWindowVisibility(isVisible: boolean) {
  sendToMainWindow("window:visibility-changed", isVisible);
  trayController.refresh();
}

// Register privileged schemes (must be called before app is ready)
// 注册特权协议（必须在 app ready 之前调用）
protocol.registerSchemesAsPrivileged([
  {
    scheme: "local-image",
    privileges: {
      secure: true,
      standard: true,
      supportFetchAPI: true,
      stream: true,
    },
  },
  {
    scheme: "local-video",
    privileges: {
      secure: true,
      standard: true,
      supportFetchAPI: true,
      stream: true,
    },
  },
]);

const isE2E = isE2EEnabled();
configureE2ETestProfile();
if (!isE2E) {
  const appDataPath = app.getPath("appData");
  const resolvedUserDataPath = resolveInitialUserDataPath({
    appDataPath,
    defaultUserDataPath: getHistoricalDefaultUserDataPath(
      appDataPath,
      process.platform,
    ),
    exePath: process.execPath,
    isPackaged: app.isPackaged,
    platform: process.platform,
  });
  app.setPath("userData", resolvedUserDataPath);
}
configureRuntimePaths({
  appDataPath: app.getPath("appData"),
  userDataPath: app.getPath("userData"),
  productName: "GuiZhi",
  exePath: process.execPath,
  isPackaged: app.isPackaged,
  platform: process.platform,
});
const isDev = shouldUseDevServer(app.isPackaged);
/** 渲染进程唯一允许停留的远程源；生产构建走 file://，这里为 null */
const devServerUrl = isDev
  ? process.env.GUIZHI_E2E_RENDERER_URL ||
    process.env.VITE_DEV_SERVER_URL ||
    "http://127.0.0.1:5173"
  : null;
/** 打包后的渲染产物目录；file:// 导航只允许停留在这里面 */
const rendererDir = path.join(__dirname, "../renderer");
const windowSecurityOptions = { devServerUrl, rendererDir };

const trayController = createTrayController({
  agentManagementEnabled: false,
  buildMenu: (template) => Menu.buildFromTemplate(template),
  createFromPath: (filePath) => nativeImage.createFromPath(filePath),
  createTray: (icon) => new Tray(icon),
  dirname: __dirname,
  getLocale: () => app.getLocale(),
  getResourcesPath: () => process.resourcesPath,
  getStoredLanguage: () => (appDb ? readLanguageSetting(appDb) : null),
  getWindowVisibility: () => mainWindow?.isVisible() ?? false,
  isDev,
  onCommand: (command) =>
    void dispatchTrayAppCommand({
      command,
      createWindow,
      getWindow: () => mainWindow,
      onWindowShown: () => emitWindowVisibility(true),
      sendCommand: (pendingCommand) =>
        sendToMainWindow(IPC_CHANNELS.APP_COMMAND, pendingCommand),
    }),
  onQuit: () => {
    isQuitting = true;
    // 走窗口关闭路径而不是直接 app.quit()：渲染进程的 beforeunload 要在
    // before-quit 关掉数据库之前把未保存的编辑落盘。窗口销毁后
    // window-all-closed 会接着退出应用。
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.close();
      return;
    }
    app.quit();
  },
  onToggleWindow: () => {
    if (mainWindow && !mainWindow.isDestroyed()) {
      toggleWindowForShowApp(mainWindow, emitWindowVisibility);
    } else {
      void createWindow();
    }
  },
  platform: process.platform,
});

// Single instance lock (prevent multiple instances)
// 单实例锁定（防止多开）
const gotTheLock = isE2E ? true : app.requestSingleInstanceLock();

if (!gotTheLock) {
  // Quit immediately if we fail to acquire the lock (another instance is running)
  // 如果获取不到锁，说明已有实例在运行，直接退出
  app.quit();
} else {
  // When a second instance launches, focus existing window (or recreate if missing)
  // 当第二个实例启动时，聚焦到已有窗口（若窗口已销毁则重建）
  app.on("second-instance", async () => {
    if (mainWindow && !mainWindow.isDestroyed()) {
      if (mainWindow.isMinimized()) {
        mainWindow.restore();
      }
      mainWindow.show();
      mainWindow.focus();
      emitWindowVisibility(true);
    } else {
      await createWindow();
    }
  });
}

async function createWindow() {
  // Ensure single window
  // 确保应用只有一个主窗口
  if (mainWindow && !mainWindow.isDestroyed()) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.show();
    mainWindow.focus();
    emitWindowVisibility(true);
    return;
  }

  const isMac = process.platform === "darwin";
  const isWin = process.platform === "win32";
  const windowIconPath = isWin
    ? isDev
      ? path.join(__dirname, "../../resources/icon.ico")
      : path.join(process.resourcesPath, "icon.ico")
    : undefined;

  // 自动化保持固定视口；真实启动首次最大化，之后恢复上次正常边界与最大化状态。
  const manageWindowState = !shouldPlaceWindowOffscreen();
  const windowStateFile = path.join(app.getPath("userData"), "window-state.json");
  const launchState = manageWindowState
    ? readWindowLaunchState(
        windowStateFile,
        screen.getAllDisplays().map((display) => display.workArea),
      )
    : {
        bounds: DEFAULT_WINDOW_BOUNDS,
        shouldMaximize: false,
      };

  mainWindow = new BrowserWindow({
    ...launchState.bounds,
    minWidth: MIN_WINDOW_BOUNDS.width,
    minHeight: MIN_WINDOW_BOUNDS.height,
    webPreferences: {
      preload: path.join(__dirname, "../preload/index.js"),
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
    },
    // Use frameless window on Windows, native title bar on macOS
    // Windows 使用无边框窗口，macOS 使用原生标题栏
    frame: isWin ? false : true,
    titleBarStyle: isMac ? "hiddenInset" : "default",
    trafficLightPosition: isMac ? { x: 14, y: 22 } : undefined,
    // Dark background for Windows title bar
    // Windows 深色标题栏
    backgroundColor: "#1a1d23",
    icon: windowIconPath,
    // Don't show immediately - wait for ready-to-show to check minimizeOnLaunch setting
    // 不立即显示 - 等待 ready-to-show 事件检查 minimizeOnLaunch 设置
    show: false,
  });

  if (manageWindowState) {
    attachWindowStatePersistence(mainWindow, windowStateFile);
  }

  // Handle window ready-to-show: check if we should minimize on launch
  mainWindow.once("ready-to-show", () => {
    // 自动化截图/e2e：显示但不出现在人眼前，也不抢焦点。
    // 放在最前面是因为它必须盖过 minimizeOnLaunch——种子数据里若开了那个设置，
    // 窗口会停在托盘里不绘制，截图随即全部超时。
    if (shouldPlaceWindowOffscreen()) {
      if (mainWindow) {
        showWindowOffscreen(mainWindow);
      }
      emitWindowVisibility(true);
      return;
    }

    const launchArgs = Array.isArray(process.argv) ? process.argv : [];
    const hasHiddenArg = launchArgs.includes("--hidden");
    let openedAsHiddenByOs: boolean;
    try {
      openedAsHiddenByOs =
        app.getLoginItemSettings().wasOpenedAsHidden === true;
    } catch (error) {
      console.warn(
        "Failed to read login item settings; assuming not opened-as-hidden:",
        error instanceof Error ? error.message : error,
      );
      openedAsHiddenByOs = false;
    }
    const osRequestedHidden = hasHiddenArg || openedAsHiddenByOs;

    const shouldMinimize =
      osRequestedHidden || (appDb ? getMinimizeOnLaunchSetting(appDb) : false);

    if (launchState.shouldMaximize) {
      mainWindow?.maximize();
    }

    if (!appDb && !osRequestedHidden) {
      mainWindow?.show();
      emitWindowVisibility(true);
      return;
    }

    if (shouldMinimize) {
      createTray();
      emitWindowVisibility(false);
    } else {
      mainWindow?.show();
      emitWindowVisibility(true);
    }
  });

  mainWindow.on("show", () => emitWindowVisibility(true));
  mainWindow.on("hide", () => emitWindowVisibility(false));
  mainWindow.on("minimize", () => emitWindowVisibility(false));
  mainWindow.on("restore", () => emitWindowVisibility(true));

  // Notify renderer when OS fullscreen state changes
  // 当操作系统全屏状态变化时通知渲染进程
  mainWindow.on("enter-full-screen", () => {
    sendToMainWindow("window:fullscreen-changed", true);
  });
  mainWindow.on("leave-full-screen", () => {
    sendToMainWindow("window:fullscreen-changed", false);
  });

  // Load renderer page
  // 加载页面
  if (isDev && devServerUrl) {
    console.log("Loading dev server:", devServerUrl);
    try {
      await mainWindow.loadURL(devServerUrl);
      if (shouldOpenStartupDevTools({ isDev, isE2E })) {
        mainWindow.webContents.openDevTools();
      }
    } catch (error) {
      console.error("Failed to load dev server:", error);
    }
  } else {
    await mainWindow.loadFile(path.join(rendererDir, "index.html"));
    // Handle DevTools shortcuts in production
    // 生产环境处理开发者工具快捷键
    mainWindow.webContents.on("before-input-event", (event, input) => {
      const isDevToolsShortcut =
        input.key === "F12" ||
        (input.control && input.shift && input.key.toLowerCase() === "i") ||
        (input.meta && input.alt && input.key.toLowerCase() === "i");

      if (isDevToolsShortcut) {
        if (isDebugMode) {
          mainWindow?.webContents.toggleDevTools();
        }
        event.preventDefault();
      }
    });
  }

  // Close behavior: decide based on settings whether to minimize to tray or close
  // 关闭行为：根据设置决定是最小化到托盘还是关闭
  mainWindow.on("close", (event) => {
    if (isQuitting) return;

    const isWin = process.platform === "win32";

    if (isWin) {
      if (closeAction === "ask" && !pendingCloseAction) {
        event.preventDefault();
        pendingCloseAction = true;
        if (!mainWindow.webContents.isDestroyed()) {
          mainWindow.webContents.send("window:showCloseDialog");
        }
        return false;
      } else if (closeAction === "minimize") {
        event.preventDefault();
        mainWindow?.hide();
        return false;
      }
    } else {
      if (minimizeToTray) {
        event.preventDefault();
        mainWindow?.hide();
        return false;
      }
    }
  });

  mainWindow.on("closed", () => {
    mainWindow = null;
  });
}

// Register window control IPC
// 注册窗口控制 IPC
ipcMain.on("window:minimize", () => {
  mainWindow?.minimize();
});

ipcMain.on("window:maximize", () => {
  if (mainWindow?.isMaximized()) {
    mainWindow.unmaximize();
  } else {
    mainWindow?.maximize();
  }
});

ipcMain.on("window:close", () => {
  mainWindow?.close();
});

// Fullscreen control
// 全屏控制
ipcMain.on("window:enterFullscreen", () => {
  mainWindow?.setFullScreen(true);
});

ipcMain.on("window:exitFullscreen", () => {
  mainWindow?.setFullScreen(false);
});

ipcMain.handle("window:isFullscreen", () => {
  return mainWindow?.isFullScreen() ?? false;
});

ipcMain.handle("window:isVisible", () => {
  return mainWindow?.isVisible() ?? false;
});

ipcMain.on("window:toggleVisibility", () => {
  if (mainWindow) {
    toggleWindowForShowApp(mainWindow, emitWindowVisibility);
  }
});

ipcMain.on("window:toggleFullscreen", () => {
  if (mainWindow) {
    mainWindow.setFullScreen(!mainWindow.isFullScreen());
  }
});

// Configure auto launch on login
ipcMain.on(
  "app:setAutoLaunch",
  (_event, enabled: boolean, minimizeOnLaunch?: boolean) => {
    if (typeof enabled !== "boolean") {
      console.error("app:setAutoLaunch requires enabled to be a boolean");
      return;
    }
    const startHidden = enabled && minimizeOnLaunch === true;
    try {
      app.setLoginItemSettings({
        openAtLogin: enabled,
        openAsHidden: startHidden,
        args: startHidden ? ["--hidden"] : [],
      });
    } catch (error) {
      console.error(
        "app:setAutoLaunch failed to apply login item settings:",
        error instanceof Error ? error.message : error,
      );
    }
  },
);

ipcMain.handle(IPC_CHANNELS.APP_RELAUNCH, () => {
  scheduleAppRelaunch();
  return { success: true };
});

registerAppRuntimeIPC();

// Configure minimize-to-tray behavior
// 设置最小化到托盘
ipcMain.on("app:setMinimizeToTray", (_event, enabled: boolean) => {
  minimizeToTray = enabled;
  if (enabled) {
    createTray();
  } else {
    destroyTray();
  }
});

// Set close action (Windows)
// 设置关闭行为 (Windows)
ipcMain.on(
  "app:setCloseAction",
  (_event, action: "ask" | "minimize" | "exit") => {
    if (action !== "ask" && action !== "minimize" && action !== "exit") {
      console.error(
        "app:setCloseAction requires action to be 'ask', 'minimize', or 'exit'",
      );
      return;
    }
    closeAction = action;
    if (action === "minimize" && process.platform === "win32") {
      createTray();
    }
  },
);

// Set debug mode
// 设置调试模式
ipcMain.on("app:setDebugMode", (_event, enabled: boolean) => {
  isDebugMode = enabled;
});

// Toggle DevTools
// 切换开发者工具。与 F12 快捷键同样受调试模式约束：这条通道任何渲染进程代码
// 都能调用，不设门槛等于给注入脚本留了一个绕过快捷键限制的后门。
ipcMain.on("window:toggleDevTools", () => {
  if (!isDebugMode && !isDev) {
    return;
  }
  mainWindow?.webContents.toggleDevTools();
});

// Handle close dialog result
// 处理关闭对话框结果
ipcMain.on(
  "window:closeDialogResult",
  (_event, data: { action: "minimize" | "exit"; remember: boolean }) => {
    if (!data || typeof data !== "object") {
      console.error("window:closeDialogResult requires a non-null data object");
      pendingCloseAction = false;
      return;
    }
    if (data.action !== "minimize" && data.action !== "exit") {
      console.error(
        "window:closeDialogResult requires action to be 'minimize' or 'exit'",
      );
      pendingCloseAction = false;
      return;
    }
    pendingCloseAction = false;

    if (data.remember) {
      closeAction = data.action;
    }

    if (data.action === "minimize") {
      mainWindow?.hide();
      createTray();
    } else {
      isQuitting = true;
      mainWindow?.close();
    }
  },
);

// User cancelled close dialog (do nothing; allow it to show again next time)
// 用户关闭/取消了关闭对话框（不做任何动作，只允许下次再次弹出）
ipcMain.on("window:closeDialogCancel", () => {
  pendingCloseAction = false;
});

// Create system tray
// 创建系统托盘
function createTray() {
  trayController.create();
}

// Destroy tray
// 销毁托盘
function destroyTray() {
  trayController.destroy();
}

// Select folder dialog
// 选择文件夹对话框
ipcMain.handle("dialog:selectFolder", async () => {
  const result = await dialog.showOpenDialog(mainWindow!, {
    properties: ["openDirectory"],
    title: "选择数据目录",
  });
  if (!result.canceled && result.filePaths.length > 0) {
    return result.filePaths[0];
  }
  return null;
});

// Get current data directory
// 获取当前数据目录
ipcMain.handle("data:getPath", () => {
  return app.getPath("userData");
});

ipcMain.handle("data:getStatus", () => {
  const currentPath = app.getPath("userData");
  const configuredPath = readConfiguredDataPath(app.getPath("appData"));
  const resolvedCurrentPath = path.resolve(currentPath);
  const resolvedConfiguredPath = configuredPath
    ? path.resolve(configuredPath)
    : null;

  return {
    configuredPath,
    currentPath,
    needsRestart:
      !!resolvedConfiguredPath &&
      resolvedConfiguredPath !== resolvedCurrentPath,
  };
});

type DataPathChangeAction = "migrate" | "switch" | "overwrite";

const DATA_PATH_MIGRATION_ITEMS = [
  "data",
  "config",
  "backups",
  "logs",
  "Local Storage",
  "Session Storage",
  "shortcuts.json",
  "shortcut-mode.json",
];

function isSensitiveDataPathTarget(resolvedNewPath: string): string | null {
  const sensitiveRoots = [
    "/etc",
    "/usr",
    "/bin",
    "/sbin",
    "/var",
    "/tmp",
    "/System",
    "/Library",
    "C:\\Windows",
    "C:\\Program Files",
  ];

  return (
    sensitiveRoots.find((root) =>
      resolvedNewPath.toLowerCase().startsWith(root.toLowerCase()),
    ) ?? null
  );
}

function isPathInside(parentPath: string, childPath: string): boolean {
  const resolvedParent = path.resolve(parentPath);
  const resolvedChild = path.resolve(childPath);
  return (
    resolvedChild !== resolvedParent &&
    resolvedChild.startsWith(`${resolvedParent}${path.sep}`)
  );
}

function scheduleAppRelaunch(delayMs = 0): void {
  const relaunch = () => {
    app.relaunch();
    app.quit();
  };

  if (delayMs > 0) {
    setTimeout(relaunch, delayMs);
    return;
  }

  relaunch();
}

function copyDataPathItem(
  sourcePath: string,
  destPath: string,
  overwrite: boolean,
): boolean {
  try {
    fs.cpSync(sourcePath, destPath, {
      recursive: true,
      force: overwrite,
      errorOnExist: false,
    });
    return true;
  } catch (error) {
    console.error(`Failed to copy ${sourcePath}:`, error);
    return false;
  }
}

async function applyDataPathChange(
  newPath: string,
  action: DataPathChangeAction,
): Promise<{
  success: boolean;
  message?: string;
  newPath?: string;
  needsRestart?: boolean;
  error?: string;
}> {
  if (typeof newPath !== "string" || newPath.trim().length === 0) {
    return {
      success: false,
      error: "data path change requires a non-empty newPath string",
    };
  }
  if (action !== "migrate" && action !== "switch" && action !== "overwrite") {
    return {
      success: false,
      error: `Unsupported data path change action: ${action}`,
    };
  }

  const currentPath = app.getPath("userData");
  const resolvedTargetPath = path.resolve(newPath);
  if (path.resolve(currentPath) === resolvedTargetPath) {
    return {
      success: true,
      message: "Data directory is already current",
      newPath: resolvedTargetPath,
      needsRestart: false,
    };
  }

  const sensitiveRoot = isSensitiveDataPathTarget(resolvedTargetPath);
  if (sensitiveRoot) {
    return {
      success: false,
      error: `Cannot use system directory as data directory: ${resolvedTargetPath}`,
    };
  }

  if (
    fs.existsSync(resolvedTargetPath) &&
    !isLinkSafeDataPathRoot(resolvedTargetPath)
  ) {
    return {
      success: false,
      error: `Cannot use symbolic link as data directory: ${resolvedTargetPath}`,
    };
  }

  if (action !== "switch" && isPathInside(currentPath, resolvedTargetPath)) {
    return {
      success: false,
      error:
        "Cannot migrate data into a child directory of the current data directory",
    };
  }

  const targetInspection = inspectDataPath(resolvedTargetPath);
  if (action === "switch") {
    if (!targetInspection.exists) {
      return {
        success: false,
        error: `Cannot switch to a directory that does not exist: ${resolvedTargetPath}`,
      };
    }

    writeConfiguredDataPath(app.getPath("appData"), resolvedTargetPath);
    return {
      success: true,
      message: "Data directory switched",
      newPath: resolvedTargetPath,
      needsRestart: true,
    };
  }

  try {
    if (!fs.existsSync(resolvedTargetPath)) {
      fs.mkdirSync(resolvedTargetPath, { recursive: true });
    }

    let migratedCount = 0;
    for (const item of DATA_PATH_MIGRATION_ITEMS) {
      const sourcePath = path.join(currentPath, item);
      const destPath = path.join(resolvedTargetPath, item);
      if (!fs.existsSync(sourcePath)) {
        continue;
      }

      if (copyDataPathItem(sourcePath, destPath, action === "overwrite")) {
        migratedCount++;
      }
    }

    writeConfiguredDataPath(app.getPath("appData"), resolvedTargetPath);

    return {
      success: true,
      message: `Successfully migrated ${migratedCount} items`,
      newPath: resolvedTargetPath,
      needsRestart: true,
    };
  } catch (error) {
    return {
      success: false,
      error: error instanceof Error ? error.message : "Unknown error",
    };
  }
}

ipcMain.handle(
  "data:previewDataPathChange",
  async (_event, newPath: string) => {
    if (typeof newPath !== "string" || newPath.trim().length === 0) {
      return {
        success: false,
        error: "data:previewDataPathChange requires a non-empty newPath string",
      };
    }

    const currentPath = app.getPath("userData");
    const resolvedTargetPath = path.resolve(newPath);
    if (
      fs.existsSync(resolvedTargetPath) &&
      !isLinkSafeDataPathRoot(resolvedTargetPath)
    ) {
      return {
        success: false,
        error: `Cannot use symbolic link as data directory: ${resolvedTargetPath}`,
      };
    }

    const inspection = inspectDataPath(resolvedTargetPath);
    const isCurrentPath = path.resolve(currentPath) === resolvedTargetPath;

    return {
      success: true,
      targetPath: resolvedTargetPath,
      currentPath,
      exists: inspection.exists,
      isCurrentPath,
      markers: inspection.markers,
    };
  },
);

ipcMain.handle(
  "data:applyDataPathChange",
  async (_event, params: { newPath?: unknown; action?: unknown }) => {
    const newPath = typeof params?.newPath === "string" ? params.newPath : "";
    const action =
      params?.action === "switch" ||
      params?.action === "overwrite" ||
      params?.action === "migrate"
        ? params.action
        : "migrate";
    return applyDataPathChange(newPath, action);
  },
);

// Open a folder in the system file manager
// 在文件管理器中打开文件夹
ipcMain.handle("shell:openPath", async (_event, folderPath: string) => {
  const homePath = app.getPath("home");
  return openDirectoryPath(folderPath, {
    appDataPath: app.getPath("appData"),
    homePath,
    localAppDataPath:
      process.env.LOCALAPPDATA || path.join(homePath, "AppData", "Local"),
    lstatSync: fs.lstatSync,
    openPath: (targetPath) => shell.openPath(targetPath),
    showItemInFolder: (targetPath) => shell.showItemInFolder(targetPath),
    statSync: fs.statSync,
  });
});

// Show system notification
// 发送系统通知
ipcMain.handle(
  "notification:show",
  async (_event, options: { title: string; body: string }) => {
    if (!options || typeof options !== "object") {
      throw new Error("notification:show requires a non-null options object");
    }
    if (typeof options.title !== "string" || typeof options.body !== "string") {
      throw new Error(
        "notification:show requires title and body to be strings",
      );
    }
    if (Notification.isSupported()) {
      let iconPath: string;
      if (isDev) {
        iconPath = path.join(__dirname, "../../resources/icon.png");
      } else {
        iconPath = path.join(process.resourcesPath, "icon.png");
      }

      const notification = new Notification({
        title: options.title,
        body: options.body,
        icon: iconPath,
      });
      notification.show();
      return true;
    }
    return false;
  },
);

// 每个 webContents（主窗口、未来可能的子窗口）统一装上导航与开窗拦截
app.on("web-contents-created", (_event, contents) => {
  // 平台采集独立窗口运行在专用 partition 隔离会话中，由 electron-capture-runtime 自行管控网络与导航；
  // 仅对主会话（即应用本体渲染层）应用本地导航与外链保护。
  if (contents.session !== session.defaultSession) {
    return;
  }
  applyWebContentsSecurity(contents, windowSecurityOptions);
});

// App startup
// 应用启动
void app.whenReady().then(async () => {
  try {
    // A second packaged instance on Windows may still reach whenReady() before quit
    // if we only call app.quit() after failing the single-instance lock.
    // Windows 上第二个实例在拿不到单实例锁后，仍可能先进入 whenReady() 再退出，这里再次拦截。
    if (!gotTheLock && !isE2E) {
      app.quit();
      return;
    }

    // Register updater IPC as early as possible so renderer calls do not depend on
    // later startup work completing.
    registerUpdaterIPC();

    // CSP 响应头 + 拒绝全部 web 权限请求
    applySessionSecurity(session.defaultSession, windowSecurityOptions);

    // Register local-image protocol
    // 注册 local-image 协议
    session.defaultSession.protocol.registerFileProtocol(
      "local-image",
      (request, callback) => {
        const imagePath = resolveLocalMediaProtocolPath(
          request.url,
          "local-image",
          getImagesDir(),
        );
        if (imagePath) {
          callback({ path: imagePath });
          return;
        }
        console.warn("Blocked local-image protocol path:", request.url);
        callback({ path: "" });
      },
    );

    // Register local-video protocol
    // 注册 local-video 协议
    session.defaultSession.protocol.registerFileProtocol(
      "local-video",
      (request, callback) => {
        const videoPath = resolveLocalMediaProtocolPath(
          request.url,
          "local-video",
          getVideosDir(),
        );
        if (videoPath) {
          callback({ path: videoPath });
          return;
        }
        console.warn("Blocked local-video protocol path:", request.url);
        callback({ path: "" });
      },
    );

    // Initialize database
    // 初始化数据库
    const db = initDatabase();
    applyE2ESeed(db);
    try {
      await applyStoredNetworkProxySettings(db);
      logStartupEvent({
        event: "startup:network_proxy_applied",
        status: "ok",
      });
    } catch (error) {
      console.warn(
        "[startup] applyStoredNetworkProxySettings failed, continuing:",
        error,
      );
      logStartupEvent({
        event: "startup:network_proxy_failed",
        error: error instanceof Error ? error.message : String(error),
      });
    }
    try {
      logStartupEvent({
        event: "startup:db_initialized",
        userDataPath: scrubPath(app.getPath("userData")),
        appDataPath: scrubPath(app.getPath("appData")),
      });
    } catch {
      // ignore
    }
    appDb = db; // Save to module-level variable for createWindow access
    backgroundJobRuntime = new BackgroundJobRuntime(db, {
      sendRendererJob: (job) =>
        sendToMainWindow(IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE, job),
    });
    registerAllIPC(
      db,
      (nextDb) => {
        appDb = nextDb;
      },
      {
        broadcastImportChanged: (task) =>
          sendToMainWindow(IPC_CHANNELS.IMPORT_CHANGED, task),
        backgroundJobs: backgroundJobRuntime,
        discoveryOptions: {
          notify: (view, count, loginRequired) => {
            if (!Notification.isSupported()) return;
            const notification = new Notification({
              title: loginRequired
                ? `${view.name} 需要重新登录`
                : `${view.name} 发现了新内容`,
              body: loginRequired
                ? "登录状态已失效，定时发现已暂停。"
                : `新增 ${count} 条候选，确认后才会导入知识库。`,
            });
            notification.on("click", () => {
              if (mainWindow && !mainWindow.isDestroyed()) {
                mainWindow.show();
                mainWindow.focus();
                emitWindowVisibility(true);
              } else {
                void createWindow();
              }
              sendToMainWindow(IPC_CHANNELS.DISCOVERY_OPEN_VIEW, view.id);
            });
            notification.show();
          },
        },
      },
    );

    // Create application menu
    // 创建菜单
    createMenu();

    // Register global shortcuts
    // 注册快捷键
    registerShortcuts({ skipGlobal: isE2E });

    // Register shortcuts IPC
    // 注册快捷键 IPC
    registerShortcutsIPC();

    // Create main window
    // 创建窗口
    await createWindow();
    backgroundJobRuntime.start();

    // Init updater (production only)
    // 初始化更新器（仅在生产环境）
    if (!isDev && !isE2E && mainWindow) {
      initUpdater(mainWindow);
    }

    // 启动本地自动备份调度（E2E 环境不做后台备份，避免干扰断言）
    if (!isE2E) {
      setAutoBackupNotifier((phase, message) =>
        sendToMainWindow(IPC_CHANNELS.BACKUP_AUTO_STATUS, phase, message),
      );
    }

    // macOS: show window when clicking Dock icon
    // macOS: 点击 dock 图标时显示窗口
    app.on("activate", async () => {
      await createWindow();
    });
  } catch (error) {
    console.error("Failed to initialize app:", error);
    dialog.showErrorBox(
      "Startup Error / 启动错误",
      `An error occurred during application startup:\n\n${error instanceof Error ? error.message : String(error)}\n\nStack:\n${error instanceof Error ? error.stack : ""}`,
    );
    app.quit();
  }
});

// Quit when all windows are closed (Windows & Linux)
// 所有窗口关闭时退出（Windows & Linux）
app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

// Cleanup before quitting
// 应用退出前清理
app.on("before-quit", (event) => {
  isQuitting = true;
  backgroundJobRuntime?.stop();
  if (quitCleanupComplete) {
    closeDatabase();
    return;
  }
  event.preventDefault();
  if (quitCleanupRunning) return;
  quitCleanupRunning = true;
  void import("./services/platform-capture/browser-capture")
    .then(({ closeBrowserCaptureService }) => Promise.race([
      closeBrowserCaptureService(),
      new Promise<void>((resolve) => setTimeout(resolve, 5_000)),
    ]))
    .catch(() => undefined)
    .finally(() => {
      closeDatabase();
      quitCleanupComplete = true;
      app.quit();
    });
});

// Export main window reference (used by other modules)
// 导出主窗口引用（供其他模块使用）
export function getMainWindow() {
  return mainWindow;
}
