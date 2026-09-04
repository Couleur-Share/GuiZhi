import { SearchIcon, PanelLeftIcon, DownloadIcon, XIcon } from "lucide-react";
import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { UpdateStatus } from "../UpdateDialog";
import { useUIStore } from "../../stores/ui.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { getRuntimeCapabilities } from "../../runtime";
import { NewItemButton } from "./NewItemButton";
import { WindowControls } from "./WindowControls";

const SEARCH_DEBOUNCE_MS = 300;

interface TopBarProps {
  updateAvailable?: UpdateStatus | null;
  onShowUpdateDialog?: () => void;
}

function openCommandPalette(): void {
  window.dispatchEvent(new CustomEvent("shortcut:search"));
}

/**
 * 顶栏：常驻 Omni-Search、更新提示、新建入口与 Windows 窗口控制。
 * 知识库模块下直接过滤条目；其余模块点击/聚焦唤起全局命令面板。
 */
export function TopBar({ updateAvailable, onShowUpdateDialog }: TopBarProps) {
  const { t } = useTranslation();
  const isSidebarCollapsed = useUIStore((state) => state.isSidebarCollapsed);
  const setSidebarCollapsed = useUIStore((state) => state.setSidebarCollapsed);
  const appModule = useUIStore((state) => state.appModule);
  const setKnowledgeSearch = useKnowledgeStore(
    (state) => state.setSearchQuery,
  );
  const runtimeCapabilities = getRuntimeCapabilities();

  const [searchQuery, setSearchQuery] = useState("");
  const searchInputRef = useRef<HTMLInputElement>(null);
  const isLibrarySearch = appModule === "library";

  // 防抖下发到知识库全文检索；离开知识库时不再改过滤条件
  useEffect(() => {
    if (!isLibrarySearch) {
      return;
    }
    const timer = setTimeout(() => {
      setKnowledgeSearch(searchQuery);
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [isLibrarySearch, searchQuery, setKnowledgeSearch]);

  const updateVersion =
    updateAvailable?.status === "available"
      ? updateAvailable.info?.version?.trim()
      : "";
  const updateButtonLabel = updateVersion
    ? t("settings.newVersion", { version: updateVersion })
    : t("settings.updateAvailable");

  const searchPlaceholder = isLibrarySearch
    ? t("header.search")
    : t("header.omniSearch", "搜索知识、Wiki 或输入命令…");

  return (
    <header
      className="flex h-12 shrink-0 select-none items-center border-b border-border app-wallpaper-toolbar pl-4"
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      <div
        className="w-8 shrink-0 titlebar-no-drag"
        style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      >
        <button
          type="button"
          onClick={() => setSidebarCollapsed(!isSidebarCollapsed)}
          className="inline-flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
          title={
            isSidebarCollapsed
              ? t("common.expand", "展开")
              : t("common.collapse", "收起")
          }
          aria-label={
            isSidebarCollapsed
              ? t("common.expand", "展开")
              : t("common.collapse", "收起")
          }
        >
          <PanelLeftIcon aria-hidden="true" className="h-4 w-4" />
        </button>
      </div>

      {/* 常驻 Omni-Search：全模块可见，避免切换时中央大面积空白跳动 */}
      <div className="flex flex-1 justify-center px-3">
        <div className="relative flex w-full max-w-lg items-center titlebar-no-drag">
          <div className="app-wallpaper-search pointer-events-none absolute inset-0 rounded-lg border" />
          <SearchIcon className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            ref={searchInputRef}
            type="text"
            data-testid="topbar-search"
            placeholder={searchPlaceholder}
            value={isLibrarySearch ? searchQuery : ""}
            readOnly={!isLibrarySearch}
            onChange={(event) => {
              if (isLibrarySearch) {
                setSearchQuery(event.target.value);
              }
            }}
            onFocus={() => {
              if (!isLibrarySearch) {
                searchInputRef.current?.blur();
                openCommandPalette();
              }
            }}
            onClick={() => {
              if (!isLibrarySearch) {
                openCommandPalette();
              }
            }}
            onKeyDown={(event) => {
              if (!isLibrarySearch && event.key !== "Tab") {
                event.preventDefault();
                openCommandPalette();
              }
            }}
            className={`relative z-10 h-9 w-full rounded-lg border border-transparent bg-transparent pl-9 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary/30 transition-shadow duration-quick ${
              isLibrarySearch ? "pr-20" : "pr-16"
            } ${isLibrarySearch ? "" : "cursor-pointer"}`}
            style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
          />
          <div
            className="absolute right-2 top-1/2 z-20 flex -translate-y-1/2 items-center gap-1"
            style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
          >
            {isLibrarySearch && searchQuery ? (
              <button
                type="button"
                onClick={() => setSearchQuery("")}
                className="rounded p-1 transition-colors hover:bg-accent/60"
                aria-label={t("header.clearSearch", "清除搜索")}
              >
                <XIcon
                  aria-hidden="true"
                  className="h-3.5 w-3.5 text-muted-foreground"
                />
              </button>
            ) : null}
            <button
              type="button"
              onClick={openCommandPalette}
              className="hidden rounded-md border border-border/70 bg-background/60 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground sm:inline-flex"
              title={t("header.openCommandPalette", "打开命令面板")}
              aria-label={t("header.openCommandPalette", "打开命令面板")}
            >
              Ctrl+K
            </button>
          </div>
        </div>
      </div>

      <div
        className="ml-2 flex items-center gap-1 pr-1"
        style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      >
        {runtimeCapabilities.appUpdate &&
          updateAvailable &&
          updateAvailable.status === "available" && (
            <>
              <button
                type="button"
                onClick={onShowUpdateDialog}
                className="flex h-8 items-center gap-1.5 rounded-lg border border-dashed border-primary/50 bg-primary/10 px-3 text-sm font-medium text-primary transition-colors hover:bg-primary/20"
                aria-label={t("settings.updateAvailable")}
              >
                <DownloadIcon aria-hidden="true" className="h-4 w-4" />
                <span className="hidden sm:inline">{updateButtonLabel}</span>
              </button>
              <div className="mx-1 h-5 w-px bg-border" />
            </>
          )}

        <NewItemButton />
      </div>

      <WindowControls />
    </header>
  );
}
