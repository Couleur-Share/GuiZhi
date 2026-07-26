import { useCallback, useEffect, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { PinIcon, StarIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItemListEntry } from "@guizhi/shared/types";
import { ContextMenu } from "../ui/ContextMenu";
import { Spinner } from "../ui/Spinner";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { ItemBulkBar } from "./ItemBulkBar";
import { ItemConfirmDialog, useItemMenus } from "./item-menus";
import { ItemListToolbar } from "./ItemListToolbar";
import { ItemPagination } from "./ItemPagination";
import { getItemTypeMeta } from "./type-meta";
import { useItemListKeyboard } from "./use-item-keyboard";

/** 卡片预估高度（含行间距）：标题最多两行 */
const ROW_HEIGHT = 64;

function ItemRow({
  entry,
  isSelected,
  isChecked,
  onSelect,
  onContextMenu,
}: {
  entry: KnowledgeItemListEntry;
  isSelected: boolean;
  isChecked: boolean;
  onSelect: (event: React.MouseEvent) => void;
  onContextMenu: (event: React.MouseEvent) => void;
}) {
  const { t } = useTranslation();
  const typeMeta = getItemTypeMeta(entry.itemType);
  const typeLabel = t(typeMeta.labelKey, typeMeta.fallback);
  // 选中卡片为实心主色，内部图标改走反色梯度
  const surface = isSelected
    ? "bg-primary text-primary-foreground shadow-sm"
    : isChecked
      ? "bg-primary/20 text-foreground"
      : "bg-muted/60 text-foreground hover:bg-muted";

  return (
    <button
      type="button"
      onClick={onSelect}
      onContextMenu={onContextMenu}
      className={`flex w-full items-start gap-2 rounded-xl px-3 py-2.5 text-left transition-colors ${surface}`}
    >
      {entry.isPinned ? (
        <PinIcon
          className={`mt-0.5 h-3.5 w-3.5 shrink-0 ${isSelected ? "" : "text-primary"}`}
          aria-hidden="true"
        />
      ) : null}
      <span className="min-w-0 flex-1 text-sm font-medium leading-snug line-clamp-2">
        {entry.title || t("library.untitled", "无标题")}
      </span>
      <span className="mt-0.5 flex shrink-0 items-center gap-1">
        {entry.isFavorite ? (
          <StarIcon
            className="h-3.5 w-3.5 fill-amber-400 text-amber-400"
            aria-hidden="true"
          />
        ) : null}
        <span
          title={typeLabel}
          className={
            isSelected ? "text-primary-foreground/80" : "text-muted-foreground/70"
          }
        >
          {typeMeta.icon}
        </span>
      </span>
    </button>
  );
}

/**
 * 卡片视图的条目列表（虚拟化）。右键菜单与批量动作和列表视图共用同一套实现。
 * Ctrl/Cmd+点击、Shift+点击进入多选，顶部换成批量工具条。
 * 分页与列表视图共用 store 里的同一套状态，两边数据范围始终一致。
 */
export function ItemList() {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);
  const entries = useKnowledgeStore((state) => state.entries);
  const isLoading = useKnowledgeStore((state) => state.isLoading);
  const scope = useKnowledgeStore((state) => state.scope);
  const selectedId = useKnowledgeStore((state) => state.selectedId);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const selectionIds = useKnowledgeStore((state) => state.selectionIds);
  const toggleSelection = useKnowledgeStore((state) => state.toggleSelection);
  const rangeSelectTo = useKnowledgeStore((state) => state.rangeSelectTo);
  const clearSelection = useKnowledgeStore((state) => state.clearSelection);

  const [menuState, setMenuState] = useState<{
    x: number;
    y: number;
    entry: KnowledgeItemListEntry;
  } | null>(null);

  // 不给右键菜单挂「编辑标签」：卡片视图右侧常驻详情栏，标签行的 + 一直可见
  const {
    buildEntryMenu,
    buildMoveMenu,
    trashWithUndo,
    confirmState,
    setConfirmState,
  } = useItemMenus();

  // 回收站里 Delete 的语义是彻底删除，走确认弹窗；其余范围直接删并给撤销
  const handleKeyboardDelete = useCallback(
    (ids: string[]) => {
      if (scope === "trash") {
        setConfirmState({ kind: "delete-forever", ids });
        return;
      }
      void trashWithUndo(ids);
    },
    [scope, setConfirmState, trashWithUndo],
  );

  useItemListKeyboard({ onDelete: handleKeyboardDelete });

  const handleRowClick = (
    entry: KnowledgeItemListEntry,
    event: React.MouseEvent,
  ) => {
    if (event.ctrlKey || event.metaKey) {
      toggleSelection(entry.id);
      return;
    }
    if (event.shiftKey) {
      rangeSelectTo(entry.id);
      return;
    }
    if (selectionIds.length > 0) {
      clearSelection();
    }
    void selectItem(entry.id);
  };

  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
    getItemKey: (index) => entries[index]?.id ?? index,
  });

  // 键盘选中的行可能在视口之外；列表是虚拟化的，只能让虚拟器滚过去
  useEffect(() => {
    if (!selectedId) {
      return;
    }
    const index = entries.findIndex((entry) => entry.id === selectedId);
    if (index >= 0) {
      virtualizer.scrollToIndex(index, { align: "auto" });
    }
  }, [selectedId, entries, virtualizer]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {selectionIds.length > 0 ? (
        <ItemBulkBar
          moveMenuItems={buildMoveMenu()}
          onRequestDeleteForever={(ids) =>
            setConfirmState({ kind: "delete-forever", ids })
          }
        />
      ) : (
        <ItemListToolbar
          onEmptyTrash={
            scope === "trash" && entries.length > 0
              ? () => setConfirmState({ kind: "empty-trash" })
              : undefined
          }
        />
      )}

      <div
        ref={scrollRef}
        data-testid="item-list"
        className="min-h-0 flex-1 overflow-y-auto pb-2"
      >
        {isLoading && entries.length === 0 ? (
          <div className="flex h-32 items-center justify-center">
            <Spinner size="sm" tone="muted" />
          </div>
        ) : entries.length === 0 ? (
          <div className="flex h-32 items-center justify-center px-6 text-center text-sm text-muted-foreground">
            {scope === "trash"
              ? t("library.trashEmpty", "回收站是空的")
              : t("library.listEmpty", "暂无条目，点击右上角 + 新建")}
          </div>
        ) : (
          <div
            className="relative w-full"
            style={{ height: virtualizer.getTotalSize() }}
          >
            {virtualizer.getVirtualItems().map((virtualRow) => {
              const entry = entries[virtualRow.index];
              if (!entry) {
                return null;
              }
              return (
                <div
                  key={virtualRow.key}
                  ref={virtualizer.measureElement}
                  data-index={virtualRow.index}
                  className="absolute left-0 top-0 w-full px-2 pt-1.5"
                  style={{ transform: `translateY(${virtualRow.start}px)` }}
                >
                  <ItemRow
                    entry={entry}
                    isSelected={entry.id === selectedId}
                    isChecked={selectionIds.includes(entry.id)}
                    onSelect={(event) => handleRowClick(entry, event)}
                    onContextMenu={(event) => {
                      event.preventDefault();
                      setMenuState({
                        x: event.clientX,
                        y: event.clientY,
                        entry,
                      });
                    }}
                  />
                </div>
              );
            })}
          </div>
        )}
      </div>

      <ItemPagination />

      {menuState ? (
        <ContextMenu
          x={menuState.x}
          y={menuState.y}
          items={buildEntryMenu(menuState.entry)}
          onClose={() => setMenuState(null)}
        />
      ) : null}

      <ItemConfirmDialog
        state={confirmState}
        onClose={() => setConfirmState(null)}
      />
    </div>
  );
}
