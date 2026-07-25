import { useEffect, useMemo, useState } from "react";
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItemListEntry } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { ContextMenu } from "../ui/ContextMenu";
import { Checkbox } from "../ui/Checkbox";
import { Spinner } from "../ui/Spinner";
import { ColumnConfigMenu } from "./ColumnConfigMenu";
import { ItemBulkBar } from "./ItemBulkBar";
import { ItemConfirmDialog, useItemMenus } from "./item-menus";
import { ItemDetailModal } from "./ItemDetailModal";
import { ItemListToolbar } from "./ItemListToolbar";
import { ItemTableHeaderCell } from "./ItemTableHeaderCell";
import { ItemTableRow } from "./ItemTableRow";
import { TagPickerPopover } from "./TagPickerPopover";
import { useItemTableConfig } from "./item-table-config";

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];
/** 页码按钮最多显示几个 */
const PAGE_WINDOW = 5;

/** 以当前页为中心的页码窗口 */
function buildPageWindow(current: number, total: number): number[] {
  const size = Math.min(PAGE_WINDOW, total);
  const start =
    total <= PAGE_WINDOW
      ? 1
      : Math.min(Math.max(current - 2, 1), total - PAGE_WINDOW + 1);
  return Array.from({ length: size }, (_, index) => start + index);
}

/**
 * 列表视图：占满内容区的表格，对齐 PromptHub v0.5.9 的列表视图。
 * 复选框列吸附在左、操作列吸附在右，列宽可拖拽、列可隐藏，底部分页。
 * 点击标题（或行）打开详情浮层——表格模式下没有常驻详情栏。
 */
export function ItemTableView() {
  const { t } = useTranslation();
  const entries = useKnowledgeStore((state) => state.entries);
  const isLoading = useKnowledgeStore((state) => state.isLoading);
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
  const [pageSize, setPageSize] = useState(20);
  const [page, setPage] = useState(1);

  const totalPages = Math.max(1, Math.ceil(entries.length / pageSize));
  const pageEntries = useMemo(
    () => entries.slice((page - 1) * pageSize, page * pageSize),
    [entries, page, pageSize],
  );
  const tableMinWidth = visibleColumns.reduce(
    (sum, column) => sum + column.width,
    0,
  );

  // 列表内容变化（切换范围 / 搜索 / 排序）后回到第一页
  const listKey = entries.map((entry) => entry.id).join("|");
  useEffect(() => {
    setPage(1);
  }, [listKey]);

  useEffect(() => {
    setPage((current) => Math.min(current, totalPages));
  }, [totalPages]);

  const pageIds = pageEntries.map((entry) => entry.id);
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
                {pageEntries.map((entry) => (
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

      {entries.length > 0 ? (
        <div className="flex shrink-0 items-center justify-between border-t border-border px-4 py-2.5 text-xs text-muted-foreground">
          <span>
            {t("library.itemCount", "共 {{count}} 个", {
              count: entries.length,
            })}
          </span>

          <div className="flex items-center gap-4">
            <label className="flex items-center gap-2">
              <span>{t("library.pageSize", "每页")}</span>
              <select
                value={pageSize}
                onChange={(event) => {
                  setPageSize(Number(event.target.value));
                  setPage(1);
                }}
                className="rounded-md border border-border bg-muted px-2 py-1 text-xs text-foreground"
              >
                {PAGE_SIZE_OPTIONS.map((size) => (
                  <option key={size} value={size}>
                    {size}
                  </option>
                ))}
              </select>
            </label>

            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => setPage((current) => Math.max(1, current - 1))}
                disabled={page === 1}
                aria-label={t("library.previousPage", "上一页")}
                className="rounded-md p-1.5 transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
              >
                <ChevronLeftIcon className="h-4 w-4" aria-hidden="true" />
              </button>
              {buildPageWindow(page, totalPages).map((candidate) => (
                <button
                  key={candidate}
                  type="button"
                  onClick={() => setPage(candidate)}
                  aria-label={t("library.pageNumber", "第 {{page}} 页", {
                    page: candidate,
                  })}
                  aria-current={candidate === page ? "page" : undefined}
                  className={`h-7 w-7 rounded-md transition-colors ${
                    candidate === page
                      ? "bg-primary text-primary-foreground"
                      : "hover:bg-accent"
                  }`}
                >
                  {candidate}
                </button>
              ))}
              <button
                type="button"
                onClick={() =>
                  setPage((current) => Math.min(totalPages, current + 1))
                }
                disabled={page === totalPages}
                aria-label={t("library.nextPage", "下一页")}
                className="rounded-md p-1.5 transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
              >
                <ChevronRightIcon className="h-4 w-4" aria-hidden="true" />
              </button>
            </div>
          </div>
        </div>
      ) : null}

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
