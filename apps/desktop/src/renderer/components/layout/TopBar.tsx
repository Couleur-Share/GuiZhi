import { SearchIcon, PanelLeftIcon, DownloadIcon, XIcon } from "lucide-react";
import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { UpdateStatus } from "../UpdateDialog";
import { useUIStore } from "../../stores/ui.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { getRuntimeCapabilities } from "../../runtime";
import { NewItemButton } from "./NewItemButton";

const SEARCH_DEBOUNCE_MS = 300;

interface TopBarProps {
  updateAvailable?: UpdateStatus | null;
  onShowUpdateDialog?: () => void;
}

/**
 * 顶栏：搜索框（M1 接入知识搜索）、更新提示与新建入口。
 * 设置入口只在左侧 rail 底部提供；主题切换属于低频操作，只保留在外观设置里。
 */
export function TopBar({ updateAvailable, onShowUpdateDialog }: TopBarProps) {
  const { t } = useTranslation();
  const isSidebarCollapsed = useUIStore((state) => state.isSidebarCollapsed);
  const setSidebarCollapsed = useUIStore((state) => state.setSidebarCollapsed);
  const appModule = useUIStore((state) => state.appModule);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const setKnowledgeSearch = useKnowledgeStore(
    (state) => state.setSearchQuery,
  );
  const runtimeCapabilities = getRuntimeCapabilities();

  const [searchQuery, setSearchQuery] = useState("");
  const [focusRequested, setFocusRequested] = useState(false);
  const searchInputRef = useRef<HTMLInputElement>(null);

  // 搜索只作用于知识库。在问答/Wiki/导入页显示这个框，用户打了字却什么都不发生，
  // 而知识库的过滤条件已被悄悄改掉——切回去看到的是一个被过滤过的列表。
  const isSearchable = appModule === "library";

  useEffect(() => {
    const focusSearch = () => {
      // 在别的模块按搜索快捷键，先切回知识库再聚焦，否则光标进了一个不起作用的框
      setAppModule("library");
      setFocusRequested(true);
    };
    window.addEventListener("shortcut:search", focusSearch);
    return () => window.removeEventListener("shortcut:search", focusSearch);
  }, [setAppModule]);

  // 聚焦要等切换真正提交到 DOM：visibility: hidden 的元素不可聚焦，
  // 在事件回调里紧接着 setAppModule 调 focus() 只会静默失败。
  useEffect(() => {
    if (!focusRequested || !isSearchable) {
      return;
    }
    setFocusRequested(false);
    searchInputRef.current?.focus();
  }, [focusRequested, isSearchable]);

  // 防抖下发到知识库全文检索
  useEffect(() => {
    if (!isSearchable) {
      return;
    }
    const timer = setTimeout(() => {
      setKnowledgeSearch(searchQuery);
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [isSearchable, searchQuery, setKnowledgeSearch]);

  const updateVersion =
    updateAvailable?.status === "available"
      ? updateAvailable.info?.version?.trim()
      : "";
  const updateButtonLabel = updateVersion
    ? t("settings.newVersion", { version: updateVersion })
    : t("settings.updateAvailable");

  return (
    <header
      className="h-12 app-wallpaper-toolbar border-b border-border flex items-center px-4 shrink-0"
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      <div
        className="w-8 shrink-0"
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

      {/* 搜索框 - 居中；只在知识库模块出现 */}
      <div className="flex-1 flex justify-center px-3">
        <div
          className={`w-full max-w-lg relative flex items-center ${
            isSearchable ? "" : "invisible"
          }`}
          aria-hidden={!isSearchable}
        >
          <div className="app-wallpaper-search absolute inset-0 rounded-lg border pointer-events-none" />
          <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none z-10" />
          {/* 只过渡 box-shadow（焦点环）。transition-all 会把继承自外层的
              visibility 一并纳入过渡：切到问答/Wiki 时外层已 invisible，
              图标与玻璃底框立刻消失，输入框却要等过渡跑完才隐藏，
              占位文字「搜索知识…」于是孤零零地多留一帧多。 */}
          <input
            ref={searchInputRef}
            type="text"
            data-testid="topbar-search"
            placeholder={t("header.search")}
            value={searchQuery}
            tabIndex={isSearchable ? 0 : -1}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="relative z-10 w-full h-9 pl-9 pr-10 rounded-lg border border-transparent bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary/30 transition-shadow duration-quick"
            style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
          />
          {searchQuery && (
            <div
              className="absolute right-2 top-1/2 z-20 -translate-y-1/2 flex items-center gap-1"
              style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
            >
              <button
                type="button"
                onClick={() => setSearchQuery("")}
                className="p-1 rounded hover:bg-accent/60 transition-colors"
                aria-label={t("header.clearSearch", "清除搜索")}
                title={t("header.clearSearch", "清除搜索")}
              >
                <XIcon
                  aria-hidden="true"
                  className="w-3.5 h-3.5 text-muted-foreground"
                />
              </button>
            </div>
          )}
        </div>
      </div>

      {/* 右侧操作按钮 */}
      <div className="flex items-center gap-1 ml-4">
        {runtimeCapabilities.appUpdate &&
          updateAvailable &&
          updateAvailable.status === "available" && (
            <>
              <button
                type="button"
                onClick={onShowUpdateDialog}
                className="flex items-center gap-1.5 h-8 px-3 rounded-lg border border-dashed border-primary/50 bg-primary/10 text-primary text-sm font-medium hover:bg-primary/20 transition-colors"
                style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
                aria-label={t("settings.updateAvailable")}
              >
                <DownloadIcon aria-hidden="true" className="w-4 h-4" />
                <span className="hidden sm:inline">{updateButtonLabel}</span>
              </button>
              <div className="w-px h-5 bg-border mx-1" />
            </>
          )}

        <NewItemButton />
      </div>
    </header>
  );
}
