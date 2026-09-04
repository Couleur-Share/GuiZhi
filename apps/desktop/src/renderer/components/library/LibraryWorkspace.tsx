import { lazy, Suspense, useEffect } from "react";
import type { CSSProperties } from "react";
import { Minimize2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useTagStore } from "../../stores/tag.store";
import {
  ITEM_LIST_PANE_WIDTH_DEFAULT,
  ITEM_LIST_PANE_WIDTH_MAX,
  ITEM_LIST_PANE_WIDTH_MIN,
  useUIStore,
} from "../../stores/ui.store";
import { ColumnResizer } from "../ui/ColumnResizer";
import { ItemList } from "./ItemList";

const ItemDetail = lazy(() =>
  import("./ItemDetail").then((module) => ({ default: module.ItemDetail })),
);
const ItemTableView = lazy(() =>
  import("./ItemTableView").then((module) => ({ default: module.ItemTableView })),
);

function DetailFallback() {
  return <div className="h-full animate-pulse bg-muted/20" aria-hidden="true" />;
}

/**
 * 知识库工作区（外层 App 已提供侧栏导航），按视图模式给出两种布局：
 * - 卡片视图：窄列表 + 常驻详情栏
 * - 列表视图：全宽表格，详情走浮层
 * 专注阅读模式隐藏列表，详情占满并限宽居中。
 */
export function LibraryWorkspace() {
  const { t } = useTranslation();
  const refreshAll = useKnowledgeStore((state) => state.refreshAll);
  const flushPendingSave = useKnowledgeStore(
    (state) => state.flushPendingSave,
  );
  const fetchCollections = useCollectionStore(
    (state) => state.fetchCollections,
  );
  const fetchTags = useTagStore((state) => state.fetchTags);
  const itemListPaneWidth = useUIStore((state) => state.itemListPaneWidth);
  const setItemListPaneWidth = useUIStore(
    (state) => state.setItemListPaneWidth,
  );
  const viewMode = useUIStore((state) => state.libraryViewMode);
  const isFocusReadingMode = useUIStore((state) => state.isFocusReadingMode);
  const setFocusReadingMode = useUIStore((state) => state.setFocusReadingMode);

  useEffect(() => {
    void refreshAll();
    void fetchCollections();
    void fetchTags();
  }, [refreshAll, fetchCollections, fetchTags]);

  // 卸载（切换模块 / 关闭）前保存未落盘内容；离开模块时退出专注阅读
  useEffect(() => {
    return () => {
      void flushPendingSave();
      useUIStore.getState().setFocusReadingMode(false);
    };
  }, [flushPendingSave]);

  // Esc 退出专注阅读（查找栏等更局部的 Esc 会 stopPropagation）
  useEffect(() => {
    if (!isFocusReadingMode) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        // 查找栏 / 弹层已处理的 Esc 不再退出专注阅读
        if (event.defaultPrevented) {
          return;
        }
        event.preventDefault();
        setFocusReadingMode(false);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [isFocusReadingMode, setFocusReadingMode]);

  // Alt+Z 切换专注阅读（与详情头部按钮同一动作）
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.altKey && !event.ctrlKey && !event.metaKey && event.code === "KeyZ") {
        event.preventDefault();
        useUIStore.getState().toggleFocusReadingMode();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  if (viewMode === "list" && !isFocusReadingMode) {
    return (
      <div className="app-wallpaper-section flex h-full min-h-0 flex-1 flex-col overflow-hidden">
        <Suspense fallback={<DetailFallback />}>
          <ItemTableView />
        </Suspense>
      </div>
    );
  }

  if (isFocusReadingMode) {
    return (
      <div className="relative flex h-full min-h-0 flex-1 overflow-hidden app-wallpaper-section">
        <div className="min-w-0 flex-1">
          <Suspense fallback={<DetailFallback />}>
            <ItemDetail />
          </Suspense>
        </div>
        <button
          type="button"
          onClick={() => setFocusReadingMode(false)}
          className="absolute bottom-5 right-5 z-20 inline-flex h-10 items-center gap-2 rounded-full border border-border bg-background/95 px-4 text-sm font-medium text-foreground shadow-lg backdrop-blur-sm transition-colors hover:bg-accent"
          title={t("library.exitFocusReading", "退出专注阅读 (Esc)")}
          aria-label={t("library.exitFocusReading", "退出专注阅读 (Esc)")}
        >
          <Minimize2Icon className="h-4 w-4" aria-hidden="true" />
          {t("library.exitFocusReadingShort", "退出专注")}
        </button>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-1 overflow-hidden">
      <div
        className="relative shrink-0 border-r border-border app-wallpaper-panel"
        style={
          {
            width: `${itemListPaneWidth}px`,
          } as CSSProperties
        }
      >
        <ItemList />
        <div className="absolute inset-y-0 right-0 z-10 flex">
          <ColumnResizer
            currentWidth={itemListPaneWidth}
            min={ITEM_LIST_PANE_WIDTH_MIN}
            max={ITEM_LIST_PANE_WIDTH_MAX}
            defaultWidth={ITEM_LIST_PANE_WIDTH_DEFAULT}
            onResize={setItemListPaneWidth}
            ariaLabel={t("library.resizeList", "调整列表宽度")}
          />
        </div>
      </div>
      <div className="min-w-0 flex-1 app-wallpaper-section">
        <Suspense fallback={<DetailFallback />}>
          <ItemDetail />
        </Suspense>
      </div>
    </div>
  );
}
