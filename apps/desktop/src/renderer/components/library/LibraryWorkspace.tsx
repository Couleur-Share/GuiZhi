import { useEffect } from "react";
import type { CSSProperties } from "react";
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
import { ItemDetail } from "./ItemDetail";
import { ItemTableView } from "./ItemTableView";

/**
 * 知识库工作区（外层 App 已提供侧栏导航），按视图模式给出两种布局：
 * - 卡片视图：窄列表 + 常驻详情栏
 * - 列表视图：全宽表格，详情走浮层
 * 挂载时加载数据；卸载前保存未落盘内容。
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

  useEffect(() => {
    void refreshAll();
    void fetchCollections();
    void fetchTags();
  }, [refreshAll, fetchCollections, fetchTags]);

  // 卸载（切换模块 / 关闭）前保存未落盘内容
  useEffect(() => {
    return () => {
      void flushPendingSave();
    };
  }, [flushPendingSave]);

  if (viewMode === "list") {
    return (
      <div className="app-wallpaper-section flex h-full min-h-0 flex-1 flex-col overflow-hidden">
        <ItemTableView />
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
        <ItemDetail />
      </div>
    </div>
  );
}
