import { type ReactNode } from "react";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  PinIcon,
  RotateCcwIcon,
  StarIcon,
  Trash2Icon,
  XCircleIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItemListEntry } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useCollectionStore } from "../../stores/collection.store";
import { Checkbox } from "../ui/Checkbox";
import type { LibraryColumn } from "./item-table-config";
import {
  TAG_COLOR_CLASSES,
  formatItemTime,
  getItemStatusMeta,
  getItemTypeMeta,
} from "./type-meta";

const CELL = "px-4 py-2.5 align-middle";

function RowAction({
  onClick,
  label,
  destructive,
  active,
  children,
}: {
  onClick: () => void;
  label: string;
  destructive?: boolean;
  active?: boolean;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
      title={label}
      aria-label={label}
      aria-pressed={active}
      className={`inline-flex h-7 w-7 items-center justify-center rounded-lg transition-colors ${
        destructive
          ? "text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
          : active
            ? "bg-primary/15 text-primary"
            : "text-muted-foreground hover:bg-accent hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

/** 表格一行：单元格按可见列动态渲染，操作列吸附在右侧 */
export function ItemTableRow({
  entry,
  columns,
  isActive,
  isChecked,
  onOpen,
  onRowClick,
  onToggleCheck,
  onContextMenu,
  onRequestDeleteForever,
}: {
  entry: KnowledgeItemListEntry;
  columns: LibraryColumn[];
  isActive: boolean;
  isChecked: boolean;
  onOpen: () => void;
  onRowClick: (event: React.MouseEvent) => void;
  onToggleCheck: () => void;
  onContextMenu: (event: React.MouseEvent) => void;
  onRequestDeleteForever: (ids: string[]) => void;
}) {
  const { t } = useTranslation();
  const scope = useKnowledgeStore((state) => state.scope);
  const setStatus = useKnowledgeStore((state) => state.setStatus);
  const toggleFavorite = useKnowledgeStore((state) => state.toggleFavorite);
  const togglePinned = useKnowledgeStore((state) => state.togglePinned);
  const moveToTrash = useKnowledgeStore((state) => state.moveToTrash);
  const restoreItems = useKnowledgeStore((state) => state.restoreItems);
  const collectionName = useCollectionStore(
    (state) =>
      state.collections.find(
        (collection) => collection.id === entry.collectionId,
      )?.name,
  );

  const typeMeta = getItemTypeMeta(entry.itemType);
  const typeLabel = t(typeMeta.labelKey, typeMeta.fallback);
  const statusMeta = getItemStatusMeta(entry.status);
  const title = entry.title || t("library.untitled", "无标题");

  const renderCell = (column: LibraryColumn) => {
    const style = { width: column.width };

    switch (column.id) {
      case "checkbox":
        return (
          <td key={column.id} className={CELL} style={style}>
            <span
              className="inline-flex"
              onClick={(event) => event.stopPropagation()}
            >
              <Checkbox
                checked={isChecked}
                onChange={onToggleCheck}
                ariaLabel={t("library.selectRow", "选择「{{title}}」", {
                  title,
                })}
              />
            </span>
          </td>
        );

      case "title":
        return (
          <td key={column.id} className={CELL} style={style}>
            <div className="flex min-w-0 items-center gap-1.5">
              <span
                title={typeLabel}
                className="shrink-0 text-muted-foreground/70"
              >
                {typeMeta.icon}
              </span>
              {entry.isPinned ? (
                <PinIcon
                  className="h-3.5 w-3.5 shrink-0 text-primary"
                  aria-hidden="true"
                />
              ) : null}
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation();
                  onOpen();
                }}
                title={title}
                className="min-w-0 truncate text-left text-sm font-medium text-primary transition-colors hover:text-primary/80 hover:underline"
              >
                {title}
              </button>
              {entry.isFavorite ? (
                <StarIcon
                  className="h-3.5 w-3.5 shrink-0 fill-amber-400 text-amber-400"
                  aria-hidden="true"
                />
              ) : null}
            </div>
          </td>
        );

      case "snippet":
        return (
          <td key={column.id} className={CELL} style={style}>
            <span
              className="block truncate text-xs text-muted-foreground"
              title={entry.snippet || undefined}
            >
              {entry.snippet || (
                <span className="text-muted-foreground/40">-</span>
              )}
            </span>
          </td>
        );

      case "tags":
        return (
          <td key={column.id} className={CELL} style={style}>
            <div className="flex items-center gap-1 overflow-hidden">
              {entry.tags.length === 0 ? (
                <span className="text-xs text-muted-foreground/40">-</span>
              ) : (
                entry.tags.slice(0, 2).map((tag) => (
                  <span
                    key={tag.id}
                    title={tag.name}
                    className={`max-w-[5rem] truncate rounded px-1.5 py-0.5 text-[10px] leading-none ${TAG_COLOR_CLASSES[tag.colorKey]}`}
                  >
                    {tag.name}
                  </span>
                ))
              )}
              {entry.tags.length > 2 ? (
                <span className="shrink-0 text-[10px] text-muted-foreground/60">
                  +{entry.tags.length - 2}
                </span>
              ) : null}
            </div>
          </td>
        );

      case "type":
        return (
          <td
            key={column.id}
            className={`${CELL} text-center text-xs text-muted-foreground`}
            style={style}
          >
            {typeLabel}
          </td>
        );

      case "collection":
        return (
          <td key={column.id} className={CELL} style={style}>
            <span className="block truncate text-xs text-muted-foreground">
              {collectionName ?? t("library.noCollection", "未分类")}
            </span>
          </td>
        );

      case "status":
        return (
          <td key={column.id} className={`${CELL} text-center`} style={style}>
            <span className="inline-flex items-center rounded border border-border/70 bg-muted/40 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
              {t(statusMeta.labelKey, statusMeta.fallback)}
            </span>
          </td>
        );

      case "createdAt":
      case "updatedAt": {
        const timestamp =
          column.id === "createdAt" ? entry.createdAt : entry.updatedAt;
        return (
          <td
            key={column.id}
            className={`${CELL} text-xs text-muted-foreground`}
            style={style}
          >
            <span title={new Date(timestamp).toLocaleString()}>
              {formatItemTime(timestamp)}
            </span>
          </td>
        );
      }

      case "actions":
        return (
          <td
            key={column.id}
            className="library-table-sticky sticky right-0 z-20 bg-card p-0 shadow-[-4px_0_8px_-4px_rgba(0,0,0,0.15)]"
            style={style}
          >
            {/* 吸附列自带背景，行级的选中 / hover 底色要在这里补回来 */}
            <div
              aria-hidden="true"
              className={`absolute inset-0 transition-colors ${
                isActive
                  ? "bg-primary/10"
                  : isChecked
                    ? "bg-primary/5"
                    : "group-hover:bg-accent/50"
              }`}
            />
            <div className="relative flex items-center justify-center gap-0.5 px-2 py-2">
              {scope === "trash" ? (
                <>
                  <RowAction
                    onClick={() => void restoreItems([entry.id])}
                    label={t("library.restore", "恢复")}
                  >
                    <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />
                  </RowAction>
                  <RowAction
                    destructive
                    onClick={() => onRequestDeleteForever([entry.id])}
                    label={t("library.deleteForever", "彻底删除")}
                  >
                    <XCircleIcon className="h-4 w-4" aria-hidden="true" />
                  </RowAction>
                </>
              ) : (
                <>
                  <RowAction
                    active={entry.isFavorite}
                    onClick={() => void toggleFavorite(entry.id)}
                    label={
                      entry.isFavorite
                        ? t("library.unfavorite", "取消收藏")
                        : t("library.favorite", "收藏")
                    }
                  >
                    <StarIcon
                      className={`h-4 w-4 ${entry.isFavorite ? "fill-amber-400 text-amber-400" : ""}`}
                      aria-hidden="true"
                    />
                  </RowAction>
                  <RowAction
                    active={entry.isPinned}
                    onClick={() => void togglePinned(entry.id)}
                    label={
                      entry.isPinned
                        ? t("library.unpin", "取消置顶")
                        : t("library.pin", "置顶")
                    }
                  >
                    <PinIcon className="h-4 w-4" aria-hidden="true" />
                  </RowAction>
                  {entry.status === "archived" ? (
                    <RowAction
                      onClick={() => void setStatus([entry.id], "ready")}
                      label={t("library.unarchive", "取消归档")}
                    >
                      <ArchiveRestoreIcon
                        className="h-4 w-4"
                        aria-hidden="true"
                      />
                    </RowAction>
                  ) : (
                    <RowAction
                      onClick={() => void setStatus([entry.id], "archived")}
                      label={t("library.archive", "归档")}
                    >
                      <ArchiveIcon className="h-4 w-4" aria-hidden="true" />
                    </RowAction>
                  )}
                  <RowAction
                    destructive
                    onClick={() => void moveToTrash([entry.id])}
                    label={t("library.moveToTrash", "移到回收站")}
                  >
                    <Trash2Icon className="h-4 w-4" aria-hidden="true" />
                  </RowAction>
                </>
              )}
            </div>
          </td>
        );

      default:
        return null;
    }
  };

  return (
    <tr
      data-item-row={entry.id}
      onClick={onRowClick}
      onContextMenu={onContextMenu}
      className={`group cursor-pointer border-b border-border/50 transition-colors last:border-b-0 hover:bg-accent/50 ${
        isActive ? "bg-primary/10" : isChecked ? "bg-primary/5" : ""
      }`}
    >
      {columns.map(renderCell)}
    </tr>
  );
}
