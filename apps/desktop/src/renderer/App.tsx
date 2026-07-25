import { useCallback, useEffect, useRef, useState, lazy, Suspense } from "react";
import { Sidebar, TopBar, MainContent, TitleBar } from "./components/layout";
import { useSettingsStore } from "./stores/settings.store";
import { useUIStore } from "./stores/ui.store";
import { useImportStore } from "./stores/import.store";
import {
  getRenderedBackgroundImageBlur,
  getRenderedBackgroundImageOpacity,
  loadSettingsFromMainProcess,
} from "./stores/settings.store";
import i18n from "./i18n";
import type { UpdateStatus } from "./components/UpdateDialog";
import { BackgroundImageBackdrop } from "./components/ui/BackgroundImageBackdrop";
import { isWebRuntime } from "./runtime";
import { waitForPersistHydration } from "./utils/persist-hydration";
import { DesktopAppCommandBridge } from "./components/app/DesktopAppCommandBridge";

const SettingsPage = lazy(() =>
  import("./components/settings/SettingsPage").then((m) => ({
    default: m.SettingsPage,
  })),
);
const CaptureDialog = lazy(() =>
  import("./components/capture/CaptureDialog").then((m) => ({
    default: m.CaptureDialog,
  })),
);
const MigrationDialog = lazy(() =>
  import("./components/migration/MigrationDialog").then((m) => ({
    default: m.MigrationDialog,
  })),
);
const UpdateDialog = lazy(() =>
  import("./components/UpdateDialog").then((m) => ({
    default: m.UpdateDialog,
  })),
);
const CloseDialog = lazy(() =>
  import("./components/ui/CloseDialog").then((m) => ({
    default: m.CloseDialog,
  })),
);

// Page type
// 页面类型
type PageType = "home" | "settings";

function App() {
  const applyTheme = useSettingsStore((state) => state.applyTheme);
  const inferUpdateChannel = useSettingsStore(
    (state) => state.inferUpdateChannel,
  );
  const backgroundImageFileName = useSettingsStore(
    (state) => state.backgroundImageFileName,
  );
  const backgroundImageEnabled = useSettingsStore(
    (state) => state.backgroundImageEnabled,
  );
  const backgroundImageOpacity = useSettingsStore(
    (state) => state.backgroundImageOpacity,
  );
  const backgroundImageBlur = useSettingsStore(
    (state) => state.backgroundImageBlur,
  );
  const debugMode = useSettingsStore((state) => state.debugMode);
  const shortcutModes = useSettingsStore((state) => state.shortcutModes);
  const pendingSettingsSection = useUIStore(
    (state) => state.pendingSettingsSection,
  );
  const [currentPage, setCurrentPage] = useState<PageType>("home");
  const isUpdateCheckInFlightRef = useRef(false);
  const isWindowVisibleRef = useRef(true);

  // OS-level fullscreen state (synced from main process events)
  // OS 级全屏状态（通过主进程事件同步）
  const [isOsFullscreen, setIsOsFullscreen] = useState(false);

  useEffect(() => {
    if (pendingSettingsSection) {
      setCurrentPage("settings");
    }
  }, [pendingSettingsSection]);

  // Update state
  const [showUpdateDialog, setShowUpdateDialog] = useState(false);
  const [initialUpdateStatus, setInitialUpdateStatus] =
    useState<UpdateStatus | null>(null);
  const lastUpdateStatusRef = useRef<UpdateStatus | null>(null);

  const openUpdateDialog = useCallback(() => {
    setInitialUpdateStatus(lastUpdateStatusRef.current);
    setShowUpdateDialog(true);
  }, []);

  // Close dialog state (Windows)
  // 关闭对话框状态（Windows）
  const [showCloseDialog, setShowCloseDialog] = useState(false);

  // Capture dialog (newItem shortcut / tray)
  // 快速采集对话框（newItem 快捷键 / 托盘）
  const [showCaptureDialog, setShowCaptureDialog] = useState(false);

  // 监听「新建条目」事件（本地/全局快捷键与托盘统一走 shortcut:newItem）
  useEffect(() => {
    const handleNewItem = () => setShowCaptureDialog(true);
    window.addEventListener("shortcut:newItem", handleNewItem);
    return () => window.removeEventListener("shortcut:newItem", handleNewItem);
  }, []);

  // 订阅导入任务变更（角标与任务页实时刷新）+ 初始加载
  useEffect(() => {
    const store = useImportStore.getState();
    const unsubscribe = store.subscribeChanges();
    void store.fetchTasks();
    return unsubscribe;
  }, []);

  // 应用菜单「导入 / 导出」（macOS 菜单栏）
  useEffect(() => {
    if (isWebRuntime()) {
      return;
    }
    const handleMenuImport = () =>
      window.dispatchEvent(new CustomEvent("shortcut:newItem"));
    const handleMenuExport = () =>
      useUIStore.getState().requestSettingsSection("data");
    window.api?.on?.("menu:import", handleMenuImport);
    window.api?.on?.("menu:export", handleMenuExport);
    return () => {
      window.api?.off?.("menu:import", handleMenuImport);
      window.api?.off?.("menu:export", handleMenuExport);
    };
  }, []);

  // Wiki 后台自动编译（ADR 0023）：开关开启时每 5 分钟增量编译一轮
  useEffect(() => {
    const WIKI_COMPILE_INTERVAL_MS = 5 * 60 * 1000;
    const timer = setInterval(() => {
      if (useSettingsStore.getState().wikiCompileEnabled) {
        void import("./stores/wiki.store").then(({ runBackgroundWikiCompile }) =>
          runBackgroundWikiCompile(),
        );
      }
    }, WIKI_COMPILE_INTERVAL_MS);
    return () => clearInterval(timer);
  }, []);

  // 语义索引后台循环：embedding 模型配置后自动增量嵌入新内容（未配置时静默跳过）
  useEffect(() => {
    if (isWebRuntime()) {
      return;
    }
    const SEMANTIC_INDEX_INTERVAL_MS = 5 * 60 * 1000;
    const runBackgroundIndexing = () => {
      void import("./stores/semantic.store").then(({ useSemanticStore }) =>
        useSemanticStore.getState().runIndexing(),
      );
    };
    const initialTimer = setTimeout(runBackgroundIndexing, 60 * 1000);
    const timer = setInterval(runBackgroundIndexing, SEMANTIC_INDEX_INTERVAL_MS);
    return () => {
      clearTimeout(initialTimer);
      clearInterval(timer);
    };
  }, []);

  // 旧版归知数据迁移检测（一次性：新库为空且发现旧库时提示）
  const [legacyMigration, setLegacyMigration] = useState<{
    itemCount: number;
  } | null>(null);
  useEffect(() => {
    if (localStorage.getItem("guizhi-migration-dismissed")) {
      return;
    }
    void window.api.migration
      .detectLegacy()
      .then((detected) => {
        if (detected) {
          setLegacyMigration({ itemCount: detected.itemCount });
        }
      })
      .catch(() => {});
  }, []);

  // Update status (used for TopBar indicator)
  // 更新状态（用于顶部栏显示更新提示）
  const [updateAvailable, setUpdateAvailable] = useState<UpdateStatus | null>(
    null,
  );

  // Local shortcuts state
  // 局部快捷键状态
  const [localShortcuts, setLocalShortcuts] = useState<Record<string, string>>(
    {},
  );
  const normalizedBackgroundImageFileName = backgroundImageFileName?.trim();
  const hasBackgroundImage =
    !isWebRuntime() &&
    backgroundImageEnabled &&
    typeof normalizedBackgroundImageFileName === "string";
  const renderedBackgroundBlur =
    getRenderedBackgroundImageBlur(backgroundImageBlur);
  const renderedBackgroundImageOpacity = getRenderedBackgroundImageOpacity(
    backgroundImageOpacity,
  );
  const webRuntime = isWebRuntime();

  useEffect(() => {
    if (webRuntime) {
      return;
    }

    // Initial load local shortcuts
    // 初始化加载局部快捷键
    window.electron?.getShortcuts?.().then((shortcuts) => {
      if (shortcuts) setLocalShortcuts(shortcuts);
    });

    // Listen for updates
    // 监听更新
    const offShortcutsUpdated = window.electron?.onShortcutsUpdated?.(
      (shortcuts) => {
        setLocalShortcuts(shortcuts);
      },
    );

    return () => {
      if (typeof offShortcutsUpdated === "function") {
        offShortcutsUpdated();
      }
    };
  }, [webRuntime]);

  // Global Escape key: exit OS fullscreen regardless of which component entered it
  // 全局 Escape 键：无论哪个组件进入了 OS 全屏，都可以退出
  useEffect(() => {
    if (!isOsFullscreen) return;
    const handleEscapeFullscreen = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        window.electron?.exitFullscreen?.();
      }
    };
    window.addEventListener("keydown", handleEscapeFullscreen);
    return () => window.removeEventListener("keydown", handleEscapeFullscreen);
  }, [isOsFullscreen]);

  // Handle local shortcuts
  // 处理局部快捷键
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const parts = [];
      const isMac = navigator.userAgent.toLowerCase().includes("mac");

      if (isMac ? e.metaKey : e.ctrlKey) parts.push("CommandOrControl");
      if (e.altKey) parts.push("Alt");
      if (e.shiftKey) parts.push("Shift");

      let key = e.key;
      // Ignore modifier keys events
      if (["Control", "Alt", "Shift", "Meta"].includes(key)) return;

      if (key === " ") key = "Space";
      parts.push(key.toUpperCase());

      const pressed = parts.join("+");

      // Check matching
      for (const [action, accelerator] of Object.entries(localShortcuts)) {
        if (accelerator === pressed) {
          const mode = (shortcutModes && shortcutModes[action]) || "local";

          if (mode === "local") {
            e.preventDefault();
            switch (action) {
              case "showApp":
                window.electron?.toggleVisibility?.();
                break;
              case "newItem":
                window.dispatchEvent(new CustomEvent("shortcut:newItem"));
                break;
              case "search":
                window.dispatchEvent(new CustomEvent("shortcut:search"));
                break;
              case "settings":
                setCurrentPage("settings");
                break;
            }
          }
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [shortcutModes, localShortcuts]);

  useEffect(() => {
    if (isWebRuntime()) {
      return;
    }

    // Listen for OS fullscreen state changes from main process
    // 监听主进程发送的 OS 全屏状态变化事件
    const handleFullscreenChanged = (isFullscreen: boolean) => {
      setIsOsFullscreen(isFullscreen);
    };
    window.api?.on?.("window:fullscreen-changed", handleFullscreenChanged);

    const handleWindowVisibilityChanged = (isVisible: boolean) => {
      isWindowVisibleRef.current = isVisible;
    };
    window.api?.on?.(
      "window:visibility-changed",
      handleWindowVisibilityChanged,
    );
    window.electron?.isVisible?.().then((isVisible) => {
      if (typeof isVisible === "boolean") {
        isWindowVisibleRef.current = isVisible;
      }
    });

    // Listen for update status
    const handleStatus = (status: UpdateStatus) => {
      const previousStatus = lastUpdateStatusRef.current;
      const shouldIgnoreTransientChecking =
        status.status === "checking" &&
        (previousStatus?.status === "available" ||
          previousStatus?.status === "downloaded");

      if (shouldIgnoreTransientChecking) {
        return;
      }

      lastUpdateStatusRef.current = status;

      if (status.status === "available" || status.status === "downloaded") {
        setUpdateAvailable(status);
      } else if (status.status === "not-available") {
        setUpdateAvailable(null);
      }
    };

    const offUpdaterStatus = window.electron?.updater?.onStatus(handleStatus);

    // Listen for close dialog trigger (Windows)
    // 监听关闭对话框触发（Windows）
    const handleShowCloseDialog = () => setShowCloseDialog(true);
    const offShowCloseDialog = window.electron?.onShowCloseDialog?.(
      handleShowCloseDialog,
    );

    // Listen for global shortcut triggers
    // 监听全局快捷键触发
    const handleShortcutTriggered = (action: string) => {
      switch (action) {
        case "newItem":
          window.dispatchEvent(new CustomEvent("shortcut:newItem"));
          break;
        case "search":
          window.dispatchEvent(new CustomEvent("shortcut:search"));
          break;
        case "settings":
          setCurrentPage("settings");
          break;
        // showApp is handled in main process
        // showApp 由主进程处理
      }
    };
    const offShortcutTriggered = window.electron?.onShortcutTriggered?.(
      handleShortcutTriggered,
    );

    // Check for updates on startup and periodically
    // 启动时和周期性检查更新
    const UPDATE_CHECK_INTERVAL = 60 * 60 * 1000; // 1 hour
    let updateCheckTimer: NodeJS.Timeout | null = null;
    let startupUpdateCheckTimer: NodeJS.Timeout | null = null;

    const checkForUpdates = () => {
      const settings = useSettingsStore.getState();
      const isVisible = isWindowVisibleRef.current;
      const isOnline = navigator.onLine !== false;
      if (
        !settings.autoCheckUpdate ||
        !isVisible ||
        !isOnline ||
        isUpdateCheckInFlightRef.current
      ) {
        return;
      }

      isUpdateCheckInFlightRef.current = true;
      const p = window.electron?.updater?.check({
        useMirror: settings.useUpdateMirror,
        channel: settings.updateChannel,
      });
      if (p && typeof (p as Promise<unknown>).finally === "function") {
        (p as Promise<unknown>).finally(() => {
          isUpdateCheckInFlightRef.current = false;
        });
      } else {
        isUpdateCheckInFlightRef.current = false;
      }
    };

    // Initial check after 3 seconds
    // 启动后 3 秒进行首次检查
    startupUpdateCheckTimer = setTimeout(checkForUpdates, 3000);

    // Periodic check every hour
    // 每小时周期性检查
    updateCheckTimer = setInterval(checkForUpdates, UPDATE_CHECK_INTERVAL);

    // Listen for manual check trigger - always force a fresh check
    const handleOpenUpdate = () => {
      openUpdateDialog();
    };
    window.addEventListener("open-update-dialog", handleOpenUpdate);

    return () => {
      window.api?.off?.("window:fullscreen-changed", handleFullscreenChanged);
      window.api?.off?.(
        "window:visibility-changed",
        handleWindowVisibilityChanged,
      );
      if (typeof offUpdaterStatus === "function") {
        offUpdaterStatus();
      } else {
        window.electron?.updater?.offStatus?.();
      }
      if (typeof offShowCloseDialog === "function") {
        offShowCloseDialog();
      }
      if (typeof offShortcutTriggered === "function") {
        offShortcutTriggered();
      }

      if (updateCheckTimer) {
        clearInterval(updateCheckTimer);
      }
      if (startupUpdateCheckTimer) {
        clearTimeout(startupUpdateCheckTimer);
      }
      window.removeEventListener("open-update-dialog", handleOpenUpdate);
    };
  }, [openUpdateDialog]);

  // Sync debug mode
  useEffect(() => {
    window.electron?.setDebugMode?.(debugMode);
  }, [debugMode]);

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");

    const syncSystemTheme = () => {
      const settings = useSettingsStore.getState();
      if (settings.themeMode !== "system") {
        return;
      }

      const prefersDark = mediaQuery.matches;
      document.documentElement.classList.toggle("dark", prefersDark);

      if (settings.isDarkMode !== prefersDark) {
        useSettingsStore.setState({ isDarkMode: prefersDark });
      }
    };

    syncSystemTheme();
    mediaQuery.addEventListener("change", syncSystemTheme);
    return () => mediaQuery.removeEventListener("change", syncSystemTheme);
  }, []);

  // Mirror motion preference to <html data-motion>. CSS in globals.css
  // reads this attribute to scale motion durations or disable them entirely.
  // 把动画偏好同步到 <html data-motion>，globals.css 依据该属性缩放或关闭动画。
  useEffect(() => {
    const apply = (preference: "off" | "reduced" | "standard"): void => {
      document.documentElement.dataset.motion = preference;
    };
    apply(useSettingsStore.getState().motionPreference);
    return useSettingsStore.subscribe((state, prev) => {
      if (state.motionPreference !== prev.motionPreference) {
        apply(state.motionPreference);
      }
    });
  }, []);

  useEffect(() => {
    // Apply persisted theme settings
    // 应用保存的主题设置
    applyTheme();

    // Sync language setting: use settings store as the source of truth
    // 同步语言设置：以 settings store 为准
    const languageSettings = useSettingsStore.getState();
    if (
      languageSettings.language &&
      i18n.language !== languageSettings.language
    ) {
      languageSettings.setLanguage(languageSettings.language);
    }

    let disposed = false;

    const waitForSettingsHydration = async (): Promise<void> => {
      const persistController = (
        useSettingsStore as typeof useSettingsStore & {
          persist?: Parameters<typeof waitForPersistHydration>[0];
        }
      ).persist;

      await waitForPersistHydration(persistController);
    };

    void (async () => {
      await waitForSettingsHydration();
      if (disposed) {
        return;
      }

      const installedVersion = await window.electron?.updater?.getVersion?.();
      if (disposed) {
        return;
      }
      if (installedVersion) {
        inferUpdateChannel(installedVersion);
      }

      await loadSettingsFromMainProcess();
    })();

    return () => {
      disposed = true;
    };
  }, [applyTheme, inferUpdateChannel]);

  return (
    <div
      className={`relative flex flex-col h-screen bg-background text-foreground overflow-hidden ${
        hasBackgroundImage ? "app-background-mode-image" : ""
      }`}
    >
      {hasBackgroundImage ? (
        <BackgroundImageBackdrop
          src={normalizedBackgroundImageFileName!}
          alt=""
          opacity={renderedBackgroundImageOpacity}
          blur={renderedBackgroundBlur}
        />
      ) : null}

      <div
        className={`relative z-10 flex flex-col h-screen overflow-hidden ${
          hasBackgroundImage ? "app-wallpaper-shell" : ""
        }`}
      >
        {/* Windows title bar */}
        {/* Windows 标题栏 */}
        {!webRuntime && <TitleBar />}
        {!webRuntime && (
          <DesktopAppCommandBridge
            onNavigate={setCurrentPage}
            onOpenUpdater={openUpdateDialog}
          />
        )}

        <div className="flex flex-1 overflow-y-hidden overflow-x-visible">
          <Sidebar
            currentPage={currentPage}
            onNavigate={setCurrentPage}
            layout="rail"
          />

          <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
            <TopBar
              updateAvailable={updateAvailable}
              onShowUpdateDialog={openUpdateDialog}
            />

            <div className="flex min-h-0 flex-1 overflow-hidden">
              {currentPage === "home" ? (
                <Sidebar
                  currentPage={currentPage}
                  onNavigate={setCurrentPage}
                  layout="panel"
                />
              ) : null}

              <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
                {/* Page content */}
                {/* 页面内容 */}
                {currentPage === "home" ? (
                  <MainContent />
                ) : (
                  <Suspense
                    fallback={
                      <div className="flex-1 flex items-center justify-center" />
                    }
                  >
                    <SettingsPage onBack={() => setCurrentPage("home")} />
                  </Suspense>
                )}
              </div>
            </div>
          </div>
        </div>

        {showUpdateDialog ? (
          <Suspense fallback={null}>
            <UpdateDialog
              isOpen={showUpdateDialog}
              onClose={() => setShowUpdateDialog(false)}
              initialStatus={initialUpdateStatus}
            />
          </Suspense>
        ) : null}

        {/* Windows close dialog */}
        {/* Windows 关闭对话框 */}
        {showCloseDialog ? (
          <Suspense fallback={null}>
            <CloseDialog
              isOpen={showCloseDialog}
              onClose={() => setShowCloseDialog(false)}
            />
          </Suspense>
        ) : null}

        {/* Quick capture dialog */}
        {/* 快速采集对话框 */}
        {showCaptureDialog ? (
          <Suspense fallback={null}>
            <CaptureDialog
              isOpen={showCaptureDialog}
              onClose={() => setShowCaptureDialog(false)}
            />
          </Suspense>
        ) : null}

        {/* Legacy data migration dialog */}
        {/* 旧版数据迁移对话框 */}
        {legacyMigration ? (
          <Suspense fallback={null}>
            <MigrationDialog
              isOpen={Boolean(legacyMigration)}
              itemCount={legacyMigration.itemCount}
              onClose={() => setLegacyMigration(null)}
            />
          </Suspense>
        ) : null}
      </div>
    </div>
  );
}

export default App;
