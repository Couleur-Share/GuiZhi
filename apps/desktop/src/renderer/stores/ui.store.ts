import { create } from "zustand";
import { persist } from "zustand/middleware";

/**
 * 归知的顶层功能模块（对应侧栏 rail）：
 * - library：知识库（条目/集合/标签/回收站）
 * - ask：AI 问答
 * - wiki：知识 Wiki
 * - imports：导入任务队列
 */
export type AppModule = "library" | "ask" | "wiki" | "imports";

/**
 * 知识库列表视图：
 * - card：窄列表卡片 + 右侧常驻详情栏（默认）
 * - list：全宽表格，点击行打开详情浮层
 */
export type LibraryViewMode = "card" | "list";

export type SettingsSectionId =
  | "general"
  | "appearance"
  | "data"
  | "network"
  | "ai"
  | "illustration"
  | "mcp"
  | "language"
  | "shortcuts"
  | "about";

/**
 * Default and safe bounds for the resizable sidebar panel.
 */
export const SIDEBAR_PANEL_WIDTH_DEFAULT = 288;
export const SIDEBAR_PANEL_WIDTH_MIN = 220;
export const SIDEBAR_PANEL_WIDTH_MAX = 640;

/**
 * Default and safe bounds for the resizable item-list pane.
 */
export const ITEM_LIST_PANE_WIDTH_DEFAULT = 320;
export const ITEM_LIST_PANE_WIDTH_MIN = 240;
export const ITEM_LIST_PANE_WIDTH_MAX = 720;

/**
 * Wiki 目录列宽度。原本写死 288px，窄列里摘要一行就被 truncate 成半句话。
 */
export const WIKI_CATALOG_PANE_WIDTH_DEFAULT = 300;
export const WIKI_CATALOG_PANE_WIDTH_MIN = 220;
export const WIKI_CATALOG_PANE_WIDTH_MAX = 560;

function clamp(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) {
    return min;
  }
  return Math.min(Math.max(value, min), max);
}

function normalizeAppModule(value: unknown): AppModule {
  return value === "ask" || value === "wiki" || value === "imports"
    ? value
    : "library";
}

/** 兼容 v0.3 早期持久化的行密度值：comfortable → card、compact → list */
function normalizeLibraryViewMode(value: unknown): LibraryViewMode {
  return value === "list" || value === "compact" ? "list" : "card";
}

interface UIState {
  appModule: AppModule;
  setAppModule: (mode: AppModule) => void;
  isSidebarCollapsed: boolean;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  sidebarPanelWidth: number;
  itemListPaneWidth: number;
  wikiCatalogPaneWidth: number;
  setSidebarPanelWidth: (width: number) => void;
  setItemListPaneWidth: (width: number) => void;
  setWikiCatalogPaneWidth: (width: number) => void;
  resetColumnWidths: () => void;
  libraryViewMode: LibraryViewMode;
  setLibraryViewMode: (mode: LibraryViewMode) => void;
  pendingSettingsSection: SettingsSectionId | null;
  requestSettingsSection: (section: SettingsSectionId) => void;
  consumeSettingsSectionRequest: () => SettingsSectionId | null;
  /** 从条目等上下文入口带进 AI 问答的草稿；不持久化，避免下次启动意外发送旧问题。 */
  pendingAskDraft: string | null;
  requestAskDraft: (draft: string) => void;
  consumeAskDraft: () => string | null;
  /** 关于页等强制打开首次设置清单（不持久化） */
  setupChecklistRequest: boolean;
  requestSetupChecklist: () => void;
  consumeSetupChecklistRequest: () => boolean;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      appModule: "library",
      setAppModule: (mode) => set({ appModule: normalizeAppModule(mode) }),
      isSidebarCollapsed: false,
      toggleSidebar: () =>
        set((state) => ({ isSidebarCollapsed: !state.isSidebarCollapsed })),
      setSidebarCollapsed: (collapsed) =>
        set({ isSidebarCollapsed: collapsed }),
      sidebarPanelWidth: SIDEBAR_PANEL_WIDTH_DEFAULT,
      itemListPaneWidth: ITEM_LIST_PANE_WIDTH_DEFAULT,
      wikiCatalogPaneWidth: WIKI_CATALOG_PANE_WIDTH_DEFAULT,
      setSidebarPanelWidth: (width) =>
        set({
          sidebarPanelWidth: clamp(
            width,
            SIDEBAR_PANEL_WIDTH_MIN,
            SIDEBAR_PANEL_WIDTH_MAX,
          ),
        }),
      setItemListPaneWidth: (width) =>
        set({
          itemListPaneWidth: clamp(
            width,
            ITEM_LIST_PANE_WIDTH_MIN,
            ITEM_LIST_PANE_WIDTH_MAX,
          ),
        }),
      setWikiCatalogPaneWidth: (width) =>
        set({
          wikiCatalogPaneWidth: clamp(
            width,
            WIKI_CATALOG_PANE_WIDTH_MIN,
            WIKI_CATALOG_PANE_WIDTH_MAX,
          ),
        }),
      resetColumnWidths: () =>
        set({
          sidebarPanelWidth: SIDEBAR_PANEL_WIDTH_DEFAULT,
          itemListPaneWidth: ITEM_LIST_PANE_WIDTH_DEFAULT,
          wikiCatalogPaneWidth: WIKI_CATALOG_PANE_WIDTH_DEFAULT,
        }),
      libraryViewMode: "card",
      setLibraryViewMode: (mode) =>
        set({ libraryViewMode: normalizeLibraryViewMode(mode) }),
      pendingSettingsSection: null,
      requestSettingsSection: (section) =>
        set({ pendingSettingsSection: section }),
      consumeSettingsSectionRequest: () => {
        const section = get().pendingSettingsSection;
        set({ pendingSettingsSection: null });
        return section;
      },
      pendingAskDraft: null,
      requestAskDraft: (draft) => set({ pendingAskDraft: draft.trim() || null }),
      consumeAskDraft: () => {
        const draft = get().pendingAskDraft;
        set({ pendingAskDraft: null });
        return draft;
      },
      setupChecklistRequest: false,
      requestSetupChecklist: () => set({ setupChecklistRequest: true }),
      consumeSetupChecklistRequest: () => {
        const requested = get().setupChecklistRequest;
        set({ setupChecklistRequest: false });
        return requested;
      },
    }),
    {
      name: "ui-storage",
      partialize: (state) => ({
        appModule: state.appModule,
        isSidebarCollapsed: state.isSidebarCollapsed,
        sidebarPanelWidth: state.sidebarPanelWidth,
        itemListPaneWidth: state.itemListPaneWidth,
        wikiCatalogPaneWidth: state.wikiCatalogPaneWidth,
        libraryViewMode: state.libraryViewMode,
      }),
      merge: (persisted, current) => {
        const saved = (persisted ?? {}) as Partial<UIState> & {
          libraryListDensity?: unknown;
        };
        const merged = { ...current, ...saved };
        return {
          ...merged,
          appModule: normalizeAppModule(merged.appModule),
          sidebarPanelWidth: clamp(
            merged.sidebarPanelWidth ?? SIDEBAR_PANEL_WIDTH_DEFAULT,
            SIDEBAR_PANEL_WIDTH_MIN,
            SIDEBAR_PANEL_WIDTH_MAX,
          ),
          itemListPaneWidth: clamp(
            merged.itemListPaneWidth ?? ITEM_LIST_PANE_WIDTH_DEFAULT,
            ITEM_LIST_PANE_WIDTH_MIN,
            ITEM_LIST_PANE_WIDTH_MAX,
          ),
          wikiCatalogPaneWidth: clamp(
            merged.wikiCatalogPaneWidth ?? WIKI_CATALOG_PANE_WIDTH_DEFAULT,
            WIKI_CATALOG_PANE_WIDTH_MIN,
            WIKI_CATALOG_PANE_WIDTH_MAX,
          ),
          libraryViewMode: normalizeLibraryViewMode(
            saved.libraryViewMode ?? saved.libraryListDensity,
          ),
        } as UIState;
      },
    },
  ),
);
