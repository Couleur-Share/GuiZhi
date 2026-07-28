import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItemListEntry } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { ContextMenu } from "../ui/ContextMenu";
import { Checkbox } from "../ui/Checkbox";
import { LoadErrorState } from "../ui/LoadErrorState";
import { Spinner } from "../ui/Spinner";
import { ColumnConfigMenu } from "./ColumnConfigMenu";
import { ItemBulkBar } from "./ItemBulkBar";
import { ItemConfirmDialog, useItemMenus } from "./item-menus";
import { ItemDetailModal } from "./ItemDetailModal";
import { ItemListToolbar } from "./ItemListToolbar";
import { ItemPagination } from "./ItemPagination";
import { ItemTableHeaderCell } from "./ItemTableHeaderCell";
import { ItemTableRow } from "./ItemTableRow";
import { TagPickerPopover } from "./TagPickerPopover";
import { useItemTableConfig } from "./item-table-config";
import { useItemListKeyboard } from "./use-item-keyboard";

/**
 * 列表视图：占满内容区的表格，对齐 PromptHub v0.5.9 的列表视图。
 * 复选框列吸附在左、操作列吸附在右，列宽可拖拽、列可隐藏，底部分页。
 * 分页在服务端做，entries 就是当前页，不再对全量结果切片。
 * 点击标题（或行）打开详情浮层——表格模式下没有常驻详情栏。
 */
export function ItemTableView() {
  const { t } = useTranslation();
  const entries = useKnowledgeStore((state) => state.entries);
  const isLoading = useKnowledgeStore((state) => state.isLoading);
  const loadError = useKnowledgeStore((state) => state.loadError);
  const fetchList = useKnowledgeStore((state) => state.fetchList);
  const scope = useKnowledgeStore((state) => state.scope);
  const selectedId = useKnowledgeStore((state) => state.selectedId);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const selectionIds = useKnowledgeStore((state) => state.selectionIds);
  const toggleSelection = useKnowledgeStore((state) => state.toggleSelection);
  const rangeSelectTo = useKnowledgeStore((state) => state.rangeSelectTo);
  const setSelection = useKnowledgeStore((state) => state.setSelection);
  const clearSelection = useKnowledgeStore((state) => state.clearSelection);
  const setItemTags = useKnowledgeStore((state) => state.setItemTags);

  const { visibleColumns, columns, toggleColumn, resizeColumn, resetColumns } =
    useItemTableConfig();
  const [menuState, setMenuState] = useState<{
    x: number;
    y: number;
    entry: KnowledgeItemListEntry;
  } | null>(null);
  const [tagPicker, setTagPicker] = useState<{
    x: number;
    y: number;
    id: string;
  } | null>(null);

  const {
    buildEntryMenu,
    buildMoveMenu,
    trashWithUndo,
    confirmState,
    setConfirmState,
  } = useItemMenus({
    onEditTags: (entry) =>
      setTagPicker(
        menuState ? { x: menuState.x, y: menuState.y, id: entry.id } : null,
      ),
  });

  const tagPickerEntry = tagPicker
    ? entries.find((entry) => entry.id === tagPicker.id)
    : undefined;
  const [isDetailOpen, setDetailOpen] = useState(false);

  const tableMinWidth = visibleColumns.reduce(
    (sum, column) => sum + column.width,
    0,
  );

  const pageIds = entries.map((entry) => entry.id);
  const isPageAllSelected =
    pageIds.length > 0 && pageIds.every((id) => selectionIds.includes(id));

  const togglePageSelection = () => {
    if (isPageAllSelected) {
      setSelection(selectionIds.filter((id) => !pageIds.includes(id)));
      return;
    }
    setSelection([...new Set([...selectionIds, ...pageIds])]);
  };

  const openDetail = (id: string) => {
    if (selectionIds.length > 0) {
      clearSelection();
    }
    void selectItem(id);
    setDetailOpen(true);
  };

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
    openDetail(entry.id);
  };

  const handleKeyboardOpen = useCallback((id: string) => {
    void useKnowledgeStore.getState().selectItem(id);
    setDetailOpen(true);
  }, []);

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

  // 详情浮层打开时方向键属于浮层内部，不该继续移动列表选中行
  useItemListKeyboard({
    enabled: !isDetailOpen,
    onOpen: handleKeyboardOpen,
    onDelete: handleKeyboardDelete,
  });

  // 键盘移动选中行后滚动跟随
  useEffect(() => {
    if (!selectedId || isDetailOpen) {
      return;
    }
    document
      .querySelector(`[data-item-row="${selectedId}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [selectedId, isDetailOpen]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {selectionIds.length > 0 ? (
        <ItemBulkBar
          wide
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
        data-testid="item-table"
        className="min-h-0 flex-1 overflow-auto px-4 py-3"
      >
        <div className="mb-2 flex items-center justify-end">
          <ColumnConfigMenu
            columns={columns}
            onToggle={toggleColumn}
            onReset={resetColumns}
          />
        </div>

        {isLoading && entries.length === 0 ? (
          <div className="flex h-40 items-center justify-center">
            <Spinner size="sm" tone="muted" />
          </div>
        ) : loadError && entries.length === 0 ? (
          <div className="app-wallpaper-surface rounded-xl border border-border">
            <LoadErrorState
              message={loadError}
              onRetry={() => void fetchList()}
            />
          </div>
        ) : entries.length === 0 ? (
          <div className="app-wallpaper-surface flex h-40 items-center justify-center rounded-xl border border-border px-6 text-center text-sm text-muted-foreground">
            {scope === "trash"
              ? t("library.trashEmpty", "回收站是空的")
              : t("library.listEmpty", "暂无条目，点击右上角「新建」开始")}
          </div>
        ) : (
          <div className="app-wallpaper-panel overflow-x-auto rounded-xl border border-border">
            {/* table-fixed：列宽严格按配置生效，单元格靠自身溢出隐藏截断 */}
            <table
              className="w-full table-fixed border-collapse text-sm"
              style={{ minWidth: tableMinWidth }}
            >
              <thead className="sticky top-0 z-30">
                <tr className="border-b border-border bg-muted/40">
                  {visibleColumns.map((column) => {
                    if (column.id === "checkbox") {
                      return (
                        <th
                          key={column.id}
                          scope="col"
                          className="px-4 py-2.5"
                          style={{ width: column.width }}
                        >
                          <Checkbox
                            checked={isPageAllSelected}
                            onChange={togglePageSelection}
                            ariaLabel={t(
                              "library.selectPageRows",
                              "选择本页全部",
                            )}
                          />
                        </th>
                      );
                    }
                    if (column.id === "actions") {
                      return (
                        <th
                          key={column.id}
                          scope="col"
                          className="library-table-sticky sticky right-0 z-40 bg-card p-0 shadow-[-4px_0_8px_-4px_rgba(0,0,0,0.15)]"
                          style={{ width: column.width }}
                        >
                          <div
                            aria-hidden="true"
                            className="absolute inset-0 bg-muted/40"
                          />
                          <div className="relative px-4 py-2.5 text-center text-xs font-medium text-muted-foreground">
                            {t(column.labelKey, column.fallback)}
                          </div>
                        </th>
                      );
                    }
                    return (
                      <ItemTableHeaderCell
                        key={column.id}
                        column={column}
                        onResize={resizeColumn}
                      >
                        {t(column.labelKey, column.fallback)}
                      </ItemTableHeaderCell>
                    );
                  })}
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <ItemTableRow
                    key={entry.id}
                    entry={entry}
                    columns={visibleColumns}
                    isActive={entry.id === selectedId}
                    isChecked={selectionIds.includes(entry.id)}
                    onOpen={() => openDetail(entry.id)}
                    onRowClick={(event) => handleRowClick(entry, event)}
                    onToggleCheck={() => toggleSelection(entry.id)}
                    onContextMenu={(event) => {
                      event.preventDefault();
                      setMenuState({
                        x: event.clientX,
                        y: event.clientY,
                        entry,
                      });
                    }}
                    onRequestDeleteForever={(ids) =>
                      setConfirmState({ kind: "delete-forever", ids })
                    }
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <ItemPagination wide />

      {menuState ? (
        <ContextMenu
          x={menuState.x}
          y={menuState.y}
          items={buildEntryMenu(menuState.entry)}
          onClose={() => setMenuState(null)}
        />
      ) : null}

      {tagPicker && tagPickerEntry ? (
        <TagPickerPopover
          itemId={tagPickerEntry.id}
          tags={tagPickerEntry.tags}
          anchor={{ x: tagPicker.x, y: tagPicker.y }}
          onChange={(tagNames) =>
            void setItemTags(tagPickerEntry.id, tagNames)
          }
          onClose={() => setTagPicker(null)}
        />
      ) : null}

      <ItemConfirmDialog
        state={confirmState}
        onClose={() => setConfirmState(null)}
      />

      <ItemDetailModal
        isOpen={isDetailOpen}
        onClose={() => setDetailOpen(false)}
      />
    </div>
  );
}
