import { useEffect, useMemo, useState } from "react";
import type { CSSProperties } from "react";
import {
  DownloadIcon,
  InboxIcon,
  LibraryBigIcon,
  MessagesSquareIcon,
  NetworkIcon,
  ScanSearchIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useUIStore, type AppModule } from "../../stores/ui.store";
import { useImportStore } from "../../stores/import.store";
import { useInboxStore } from "../../stores/inbox.store";
import { isWebRuntime } from "../../runtime";
import type { SidebarLayout, PageType } from "./sidebar-controller-types";

const APP_MODULES: readonly AppModule[] = [
  "library",
  "inbox",
  "ask",
  "wiki",
  "research",
  "imports",
];

function useSidebarUiBindings() {
  const appModule = useUIStore((state) => state.appModule);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const isCollapsed = useUIStore((state) => state.isSidebarCollapsed);
  const sidebarPanelWidth = useUIStore((state) => state.sidebarPanelWidth);
  const setSidebarPanelWidth = useUIStore(
    (state) => state.setSidebarPanelWidth,
  );
  return {
    appModule,
    setAppModule,
    isCollapsed,
    sidebarPanelWidth,
    setSidebarPanelWidth,
  };
}

function useSidebarPlatformState() {
  const [isMac, setIsMac] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  useEffect(() => {
    setIsMac(navigator.userAgent.toLowerCase().includes("mac"));
    const check = async () => {
      if (window.electron?.isFullscreen)
        setIsFullscreen(await window.electron.isFullscreen());
    };
    void check();
    window.addEventListener("resize", check);
    return () => window.removeEventListener("resize", check);
  }, []);
  return { isMac, isFullscreen };
}

function getSidebarLayoutStyle(
  layout: SidebarLayout,
  isCollapsed: boolean,
  sidebarPanelWidth: number,
) {
  const railWidthClass = "w-20";
  const combinedWidthClass = "w-[23rem]";
  const panelStyle =
    layout === "panel" && !isCollapsed
      ? ({ "--sidebar-panel-width": `${sidebarPanelWidth}px` } as CSSProperties)
      : undefined;
  const asideClassName =
    layout === "rail"
      ? `${railWidthClass} border-r border-sidebar-border/60 bg-sidebar-accent/25`
      : layout === "panel"
        ? `border-r border-sidebar-border bg-sidebar-background/85 app-wallpaper-panel-strong transition-[opacity,transform] duration-smooth ease-out ${isCollapsed ? "w-0 -translate-x-4 opacity-0 pointer-events-none border-r-0" : "w-[var(--sidebar-panel-width)] translate-x-0 opacity-100"}`
        : `border-r border-sidebar-border app-left-rail-glass app-wallpaper-panel-strong ${isCollapsed ? railWidthClass : combinedWidthClass}`;
  return {
    railWidthClass,
    panelStyle,
    asideClassName,
    showRail: layout !== "panel",
    showPanel: layout !== "rail",
  };
}

function getRailItemLabel(
  module: AppModule,
  t: ReturnType<typeof useTranslation>["t"],
) {
  return module === "library"
    ? t("nav.library", "知识库")
    : module === "inbox"
      ? t("nav.inbox", "处理中心")
    : module === "ask"
      ? t("nav.ask", "AI 问答")
      : module === "wiki"
        ? t("nav.wiki", "Wiki")
        : module === "research"
          ? t("nav.research", "研究")
        : t("nav.imports", "导入");
}

function getRailItemIcon(module: AppModule) {
  return module === "library" ? (
    <LibraryBigIcon className="h-5 w-5" />
  ) : module === "inbox" ? (
    <InboxIcon className="h-5 w-5" />
  ) : module === "ask" ? (
    <MessagesSquareIcon className="h-5 w-5" />
  ) : module === "wiki" ? (
    <NetworkIcon className="h-5 w-5" />
  ) : module === "research" ? (
    <ScanSearchIcon className="h-5 w-5" />
  ) : (
    <DownloadIcon className="h-5 w-5" />
  );
}

function useSidebarRailItems(
  currentPage: PageType,
  onNavigate: (page: PageType) => void,
  ui: ReturnType<typeof useSidebarUiBindings>,
) {
  const { t } = useTranslation();
  const activeImportCount = useImportStore((state) => state.activeCount);
  const inboxCount = useInboxStore((state) => state.total);
  return useMemo(
    () =>
      APP_MODULES.map((module) => ({
        key: module,
        label: getRailItemLabel(module, t),
        icon: getRailItemIcon(module),
        active: ui.appModule === module,
        busy: module === "imports" && activeImportCount > 0,
        badge:
          module === "inbox" && inboxCount > 0
            ? inboxCount
            : module === "imports" && activeImportCount > 0
            ? activeImportCount
            : undefined,
        onClick: () => {
          ui.setAppModule(module);
          if (currentPage !== "home") onNavigate("home");
        },
      })),
    [activeImportCount, inboxCount, currentPage, onNavigate, t, ui],
  );
}

export function useSidebarShellController(
  currentPage: PageType,
  onNavigate: (page: PageType) => void,
  layout: SidebarLayout,
) {
  const { t } = useTranslation();
  const ui = useSidebarUiBindings();
  const platform = useSidebarPlatformState();
  const webRuntime = isWebRuntime();
  const railNavItems = useSidebarRailItems(currentPage, onNavigate, ui);
  return {
    ...ui,
    ...platform,
    ...getSidebarLayoutStyle(layout, ui.isCollapsed, ui.sidebarPanelWidth),
    activeModule: ui.appModule,
    webRuntime,
    t,
    railNavItems,
  };
}
