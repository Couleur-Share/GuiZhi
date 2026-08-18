import { contextBridge, ipcRenderer, webUtils } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { aiApi } from "./api/ai";
import { settingsApi } from "./api/settings";
import { collectionApi, knowledgeApi, tagApi } from "./api/knowledge";
import { importApi } from "./api/import";
import { platformCaptureApi } from "./api/platform-capture";
import { wikiApi } from "./api/wiki";
import { migrationApi } from "./api/migration";
import { backupApi } from "./api/backup";
import { configTransferApi } from "./api/config-transfer";
import { askSessionApi } from "./api/ask";
import { semanticApi } from "./api/semantic";
import { ffmpegApi, funasrApi, mediaApi, ytDlpApi } from "./api/media";
import { illustrationApi } from "./api/illustration";
import { logApi } from "./api/log";
import { createBufferedSubscription } from "./app-command-subscription";
import type {
  AppCommand,
  McpInstallResult,
  McpServerConfig,
} from "@guizhi/shared/types";
import type { McpScope } from "@guizhi/shared/utils/mcp-scope";

const listenerMap = new Map<
  (...args: any[]) => void,
  (...args: any[]) => void
>();
const appCommandSubscription = createBufferedSubscription<AppCommand>();

ipcRenderer.on(IPC_CHANNELS.APP_COMMAND, (_event, command: AppCommand) => {
  appCommandSubscription.publish(command);
});

type DataPathChangeAction = "migrate" | "switch" | "overwrite";

/** 更新检查来源与自动检查跳过原因，取值与 main/updater.ts 的白名单一致 */
type UpdateCheckTrigger = "manual" | "startup" | "interval" | "visibility";
type AutoUpdateSkipReason =
  | "disabled"
  | "hidden"
  | "offline"
  | "in-flight"
  | "cooldown";

const api = {
  // Window controls
  // 窗口控制 (Windows)
  minimize: () => ipcRenderer.send("window:minimize"),
  maximize: () => ipcRenderer.send("window:maximize"),
  close: () => ipcRenderer.send("window:close"),

  settings: settingsApi,
  ai: aiApi,
  knowledge: knowledgeApi,
  collection: collectionApi,
  tag: tagApi,
  import: importApi,
  platformCapture: platformCaptureApi,
  wiki: wikiApi,
  migration: migrationApi,
  backup: backupApi,
  config: configTransferApi,
  mcp: {
    getConfig: (): Promise<McpServerConfig> =>
      ipcRenderer.invoke(IPC_CHANNELS.MCP_CONFIG),
    getScope: (): Promise<McpScope> =>
      ipcRenderer.invoke(IPC_CHANNELS.MCP_GET_SCOPE),
    setScope: (scope: McpScope): Promise<McpScope> =>
      ipcRenderer.invoke(IPC_CHANNELS.MCP_SET_SCOPE, scope),
    install: (client: string): Promise<McpInstallResult> =>
      ipcRenderer.invoke(IPC_CHANNELS.MCP_INSTALL, client),
  },
  askSession: askSessionApi,
  semantic: semanticApi,
  media: mediaApi,
  illustration: illustrationApi,
  log: logApi,
  ytdlp: ytDlpApi,
  ffmpeg: ffmpegApi,
  funasr: funasrApi,

  // Listen to main process events (with whitelist)
  // 监听主进程事件（使用白名单）
  on: (channel: string, callback: (...args: any[]) => void) => {
    const ALLOWED_LISTEN_CHANNELS = [
      "updater:status",
      "shortcut:triggered",
      "window:close-action",
      "window:showCloseDialog",
      "window:fullscreen-changed",
      "window:visibility-changed",
      "import:changed",
      "backup:autoStatus",
      "menu:import",
      "menu:export",
      "ytdlp:downloadProgress",
      "ffmpeg:downloadProgress",
      "funasr:installProgress",
      "media:formatProgress",
      "media:transcribeProgress",
    ];

    if (!ALLOWED_LISTEN_CHANNELS.includes(channel)) {
      console.warn(`Blocked listening to unauthorized channel: ${channel}`);
      return;
    }
    const wrapper = (_event: any, ...args: any[]) => callback(...args);
    listenerMap.set(callback, wrapper);
    ipcRenderer.on(channel, wrapper);
  },

  // Remove listener
  // 移除监听
  off: (channel: string, callback: (...args: any[]) => void) => {
    const wrapper = listenerMap.get(callback);
    if (wrapper) {
      ipcRenderer.removeListener(channel, wrapper);
      listenerMap.delete(callback);
    }
  },
};

contextBridge.exposeInMainWorld("api", api);

// Expose window control API
// 暴露窗口控制 API
contextBridge.exposeInMainWorld("electron", {
  minimize: () => ipcRenderer.send("window:minimize"),
  maximize: () => ipcRenderer.send("window:maximize"),
  close: () => ipcRenderer.send("window:close"),
  toggleVisibility: () => ipcRenderer.send("window:toggleVisibility"),
  // Fullscreen control
  // 全屏控制
  enterFullscreen: () => ipcRenderer.send("window:enterFullscreen"),
  exitFullscreen: () => ipcRenderer.send("window:exitFullscreen"),
  toggleFullscreen: () => ipcRenderer.send("window:toggleFullscreen"),
  isFullscreen: () => ipcRenderer.invoke("window:isFullscreen"),
  isVisible: () => ipcRenderer.invoke("window:isVisible"),
  setAutoLaunch: (enabled: boolean, minimizeOnLaunch?: boolean) =>
    ipcRenderer.send("app:setAutoLaunch", enabled, minimizeOnLaunch),
  relaunchApp: () => ipcRenderer.invoke(IPC_CHANNELS.APP_RELAUNCH),
  getCacheSize: () =>
    ipcRenderer.invoke(IPC_CHANNELS.APP_GET_CACHE_SIZE) as Promise<{
      size: number;
    }>,
  clearCache: () =>
    ipcRenderer.invoke(IPC_CHANNELS.APP_CLEAR_CACHE) as Promise<{
      success: boolean;
    }>,
  getRuntimePaths: () =>
    ipcRenderer.invoke(IPC_CHANNELS.APP_GET_RUNTIME_PATHS) as Promise<{
      userDataPath: string;
      dataDir: string;
      databasePath: string;
      backupsDir: string;
      logsDir: string;
    }>,
  setDebugMode: (enabled: boolean) =>
    ipcRenderer.send("app:setDebugMode", enabled),
  toggleDevTools: () => ipcRenderer.send("window:toggleDevTools"),
  setMinimizeToTray: (enabled: boolean) =>
    ipcRenderer.send("app:setMinimizeToTray", enabled),
  setCloseAction: (action: "ask" | "minimize" | "exit") =>
    ipcRenderer.send("app:setCloseAction", action),
  // Close dialog callbacks
  // 关闭窗口对话框回调
  onShowCloseDialog: (callback: () => void) => {
    const listener = () => callback();
    ipcRenderer.on("window:showCloseDialog", listener);
    return () => {
      ipcRenderer.removeListener("window:showCloseDialog", listener);
    };
  },
  sendCloseDialogResult: (action: "minimize" | "exit", remember: boolean) => {
    ipcRenderer.send("window:closeDialogResult", { action, remember });
  },
  sendCloseDialogCancel: () => {
    ipcRenderer.send("window:closeDialogCancel");
  },
  selectFolder: () => ipcRenderer.invoke("dialog:selectFolder"),
  getPathForFile: (file: File) => webUtils.getPathForFile(file),
  openPath: (path: string) => ipcRenderer.invoke("shell:openPath", path),
  showNotification: (title: string, body: string) =>
    ipcRenderer.invoke("notification:show", { title, body }),
  // Data directory
  // 数据目录
  getDataPath: () => ipcRenderer.invoke("data:getPath"),
  getDataPathStatus: () => ipcRenderer.invoke("data:getStatus"),
  previewDataPathChange: (newPath: string) =>
    ipcRenderer.invoke("data:previewDataPathChange", newPath),
  applyDataPathChange: (newPath: string, action: DataPathChangeAction) =>
    ipcRenderer.invoke("data:applyDataPathChange", { newPath, action }),
  // Updater
  // 更新器
  updater: {
    check: (
      options?:
        | boolean
        | {
            useMirror?: boolean;
            channel?: "stable" | "preview";
            trigger?: UpdateCheckTrigger;
          },
    ) => ipcRenderer.invoke("updater:check", options),
    logAutoSkip: (payload: {
      trigger: UpdateCheckTrigger;
      reason: AutoUpdateSkipReason;
    }) => ipcRenderer.invoke("updater:logAutoSkip", payload),
    download: (
      options?:
        | boolean
        | { useMirror?: boolean; channel?: "stable" | "preview" },
    ) => ipcRenderer.invoke("updater:download", options),
    install: () => ipcRenderer.invoke("updater:install"),
    getInstallSource: () => ipcRenderer.invoke("updater:installSource"),
    openDownloadedUpdate: () =>
      ipcRenderer.invoke("updater:openDownloadedUpdate"),
    getVersion: () => ipcRenderer.invoke("updater:version"),
    getPlatform: () => ipcRenderer.invoke("updater:platform"),
    openReleases: () => ipcRenderer.invoke("updater:openReleases"),
    onStatus: (callback: (status: any) => void) => {
      const listener = (_event: Electron.IpcRendererEvent, status: any) =>
        callback(status);
      ipcRenderer.on("updater:status", listener);
      return () => {
        ipcRenderer.removeListener("updater:status", listener);
      };
    },
    offStatus: () => {
      ipcRenderer.removeAllListeners("updater:status");
    },
  },
  // Images
  // 图片
  selectImage: () => ipcRenderer.invoke("dialog:selectImage"),
  saveImage: (paths: string[]) => ipcRenderer.invoke("image:save", paths),
  saveImageBuffer: (buffer: ArrayBuffer) =>
    ipcRenderer.invoke("image:save-buffer", Buffer.from(buffer)),
  downloadImage: (url: string) => ipcRenderer.invoke("image:download", url),
  openImage: (fileName: string) => ipcRenderer.invoke("image:open", fileName),
  listImages: () => ipcRenderer.invoke("image:list"),
  getImageSize: (fileName: string) =>
    ipcRenderer.invoke("image:getSize", fileName),
  readImageBase64: (fileName: string) =>
    ipcRenderer.invoke("image:readBase64", fileName),
  saveImageBase64: (fileName: string, base64: string) =>
    ipcRenderer.invoke("image:saveBase64", fileName, base64),
  imageExists: (fileName: string) =>
    ipcRenderer.invoke("image:exists", fileName),
  clearImages: () => ipcRenderer.invoke("image:clear"),
  // Shortcuts
  // 快捷键
  getShortcuts: () => ipcRenderer.invoke("shortcuts:get"),
  setShortcuts: (shortcuts: Record<string, string>) =>
    ipcRenderer.invoke("shortcuts:set", shortcuts),
  setShortcutMode: (modes: Record<string, "global" | "local">) =>
    ipcRenderer.send("shortcuts:setMode", modes),
  // Shortcut trigger events
  // 快捷键触发事件
  onShortcutTriggered: (callback: (action: string) => void) => {
    const listener = (_event: Electron.IpcRendererEvent, action: string) =>
      callback(action);
    ipcRenderer.on("shortcut:triggered", listener);
    return () => {
      ipcRenderer.removeListener("shortcut:triggered", listener);
    };
  },
  onAppCommand: (callback: (command: AppCommand) => void) => {
    return appCommandSubscription.subscribe(callback);
  },
  // Listen for shortcut updates
  // 监听快捷键更新
  onShortcutsUpdated: (
    callback: (shortcuts: Record<string, string>) => void,
  ) => {
    const listener = (
      _event: Electron.IpcRendererEvent,
      shortcuts: Record<string, string>,
    ) => callback(shortcuts);
    ipcRenderer.on("shortcuts:updated", listener);
    return () => {
      ipcRenderer.removeListener("shortcuts:updated", listener);
    };
  },
  // Videos
  // 视频
  selectVideo: () => ipcRenderer.invoke("dialog:selectVideo"),
  saveVideo: (paths: string[]) => ipcRenderer.invoke("video:save", paths),
  openVideo: (fileName: string) => ipcRenderer.invoke("video:open", fileName),
  listVideos: () => ipcRenderer.invoke("video:list"),
  getVideoSize: (fileName: string) =>
    ipcRenderer.invoke("video:getSize", fileName),
  readVideoBase64: (fileName: string) =>
    ipcRenderer.invoke("video:readBase64", fileName),
  saveVideoBase64: (fileName: string, base64: string) =>
    ipcRenderer.invoke("video:saveBase64", fileName, base64),
  videoExists: (fileName: string) =>
    ipcRenderer.invoke("video:exists", fileName),
  getVideoPath: (fileName: string) =>
    ipcRenderer.invoke("video:getPath", fileName),
  clearVideos: () => ipcRenderer.invoke("video:clear"),
});

// Type declarations
// 类型声明
export type API = typeof api;

declare global {
  interface Window {
    api: API;
    electron?: {
      minimize?: () => void;
      maximize?: () => void;
      close?: () => void;
      toggleVisibility?: () => void;
      enterFullscreen?: () => void;
      exitFullscreen?: () => void;
      isFullscreen?: () => Promise<boolean>;
      isVisible?: () => Promise<boolean>;
      toggleFullscreen?: () => void;
      setAutoLaunch?: (enabled: boolean, minimizeOnLaunch?: boolean) => void;
      relaunchApp?: () => Promise<{ success: boolean }>;
      getCacheSize?: () => Promise<{ size: number }>;
      clearCache?: () => Promise<{ success: boolean }>;
      getRuntimePaths?: () => Promise<{
        userDataPath: string;
        dataDir: string;
        databasePath: string;
        backupsDir: string;
        logsDir: string;
      }>;
      setDebugMode?: (enabled: boolean) => void;
      toggleDevTools?: () => void;
      setMinimizeToTray?: (enabled: boolean) => void;
      setCloseAction?: (action: "ask" | "minimize" | "exit") => void;
      onShowCloseDialog?: (callback: () => void) => void | (() => void);
      sendCloseDialogResult?: (
        action: "minimize" | "exit",
        remember: boolean,
      ) => void;
      sendCloseDialogCancel?: () => void;
      selectFolder?: () => Promise<string | null>;
      getPathForFile?: (file: File) => string;
      openPath?: (
        path: string,
      ) => Promise<{ success: boolean; error?: string }>;
      showNotification?: (title: string, body: string) => Promise<boolean>;
      getDataPath?: () => Promise<string>;
      getDataPathStatus?: () => Promise<{
        currentPath: string;
        configuredPath?: string | null;
        needsRestart: boolean;
      }>;
      previewDataPathChange?: (newPath: string) => Promise<unknown>;
      applyDataPathChange?: (
        newPath: string,
        action: DataPathChangeAction,
      ) => Promise<unknown>;
      updater?: {
        check: (
          options?:
            | boolean
            | {
                useMirror?: boolean;
                channel?: "stable" | "preview";
                trigger?: UpdateCheckTrigger;
              },
        ) => Promise<{
          success: boolean;
          result?: any;
          error?: string;
          devDisabled?: boolean;
        }>;
        logAutoSkip?: (payload: {
          trigger: UpdateCheckTrigger;
          reason: AutoUpdateSkipReason;
        }) => Promise<{ success: boolean }>;
        download: (
          options?:
            | boolean
            | { useMirror?: boolean; channel?: "stable" | "preview" },
        ) => Promise<{ success: boolean; error?: string }>;
        install: () => Promise<{
          success: boolean;
          manual?: boolean;
          error?: string;
        } | void>;
        openDownloadedUpdate: () => Promise<{
          success: boolean;
          path?: string;
        }>;
        getInstallSource: () => Promise<"direct" | "homebrew" | "unknown">;
        getVersion: () => Promise<string>;
        getPlatform: () => Promise<string>;
        openReleases: () => Promise<void>;
        onStatus: (callback: (status: any) => void) => void | (() => void);
        offStatus: () => void;
      };
      selectImage?: () => Promise<string[]>;
      saveImage?: (paths: string[]) => Promise<string[]>;
      saveImageBuffer?: (buffer: ArrayBuffer) => Promise<string | null>;
      downloadImage?: (url: string) => Promise<string | null>;
      openImage?: (fileName: string) => Promise<boolean>;
      listImages?: () => Promise<string[]>;
      getImageSize?: (fileName: string) => Promise<number | null>;
      readImageBase64?: (fileName: string) => Promise<string | null>;
      saveImageBase64?: (fileName: string, base64: string) => Promise<boolean>;
      imageExists?: (fileName: string) => Promise<boolean>;
      clearImages?: () => Promise<boolean>;
      getShortcuts?: () => Promise<Record<string, string> | null>;
      setShortcuts?: (shortcuts: Record<string, string>) => Promise<boolean>;
      setShortcutMode?: (modes: Record<string, "global" | "local">) => void;
      onShortcutTriggered?: (
        callback: (action: string) => void,
      ) => void | (() => void);
      onAppCommand?: (
        callback: (command: AppCommand) => void,
      ) => void | (() => void);
      onShortcutsUpdated?: (
        callback: (shortcuts: Record<string, string>) => void,
      ) => void | (() => void);
      selectVideo?: () => Promise<string[]>;
      saveVideo?: (paths: string[]) => Promise<string[]>;
      openVideo?: (fileName: string) => Promise<boolean>;
      listVideos?: () => Promise<string[]>;
      getVideoSize?: (fileName: string) => Promise<number | null>;
      readVideoBase64?: (fileName: string) => Promise<string | null>;
      saveVideoBase64?: (fileName: string, base64: string) => Promise<boolean>;
      videoExists?: (fileName: string) => Promise<boolean>;
      getVideoPath?: (fileName: string) => Promise<string | null>;
      clearVideos?: () => Promise<boolean>;
    };
  }
}
