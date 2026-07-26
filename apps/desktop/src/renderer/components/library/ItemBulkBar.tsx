import { useEffect, useState, type ReactNode } from "react";
import {
  ArchiveIcon,
  FolderInputIcon,
  RotateCcwIcon,
  StarIcon,
  TagIcon,
  Trash2Icon,
  XCircleIcon,
  XIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { ContextMenu, type ContextMenuItem } from "../ui/ContextMenu";
import { useToast } from "../ui/Toast";
import { BulkTagPopover } from "./BulkTagPopover";

function BulkAction({
  onClick,
  label,
  wide,
  destructive,
  children,
}: {
  onClick: (event: React.MouseEvent) => void;
  label: string;
  wide: boolean;
  destructive?: boolean;
  children: ReactNode;
}) {
  const tone = destructive
    ? "text-destructive hover:bg-destructive/10"
    : "text-foreground hover:bg-muted/60";
  return (
    <button
      type="button"
      onClick={onClick}
      // 宽版把文案摆在按钮里了，再挂 title 只是把同一句话又弹一遍
      title={wide ? undefined : label}
      aria-label={label}
      className={`inline-flex h-7 shrink-0 items-center justify-center gap-1.5 rounded-md transition-colors ${tone} ${
        wide ? "px-2.5 text-xs" : "w-7"
      }`}
    >
      {children}
      {wide ? <span>{label}</span> : null}
    </button>
  );
}

/**
 * 批量操作条。窄列表（卡片视图）只放图标，文案走 tooltip；
 * 全宽表格（列表视图）空间充足，图标带文字。
 */
export function ItemBulkBar({
  wide = false,
  moveMenuItems,
  onRequestDeleteForever,
}: {
  wide?: boolean;
  moveMenuItems: ContextMenuItem[];
  onRequestDeleteForever: (ids: string[]) => void;
}) {
  const { t } = useTranslation();
  const { showUndoToast } = useToast();
  const scope = useKnowledgeStore((state) => state.scope);
  const selectionIds = useKnowledgeStore((state) => state.selectionIds);
  const clearSelection = useKnowledgeStore((state) => state.clearSelection);
  const setStatus = useKnowledgeStore((state) => state.setStatus);
  const moveToTrash = useKnowledgeStore((state) => state.moveToTrash);
  const restoreItems = useKnowledgeStore((state) => state.restoreItems);
  const bulkUpdate = useKnowledgeStore((state) => state.bulkUpdate);
  const [moveAnchor, setMoveAnchor] = useState<{ x: number; y: number } | null>(
    null,
  );
  const [tagAnchor, setTagAnchor] = useState<{ x: number; y: number } | null>(
    null,
  );

  const trashSelected = async () => {
    const ids = [...selectionIds];
    await moveToTrash(ids);
    showUndoToast(
      t("library.movedToTrash", "已移到回收站（{{count}} 项）", {
        count: ids.length,
      }),
      () => void restoreItems(ids),
    );
  };

  // 本条只在有选中项时挂载，Escape 直接退出多选
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        clearSelection();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [clearSelection]);

  return (
    <div
      className={`flex h-9 shrink-0 items-center gap-1 border-b border-border bg-primary/5 ${
        wide ? "px-4" : "px-3"
      }`}
    >
      <button
        type="button"
        onClick={clearSelection}
        title={t("library.clearSelection", "取消选择")}
        aria-label={t("library.clearSelection", "取消选择")}
        className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
      >
        <XIcon className="h-4 w-4" aria-hidden="true" />
      </button>
      <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground">
        {t("library.selectedCount", "已选 {{count}} 项", {
          count: selectionIds.length,
        })}
      </span>

      {scope === "trash" ? (
        <>
          <BulkAction
            wide={wide}
            onClick={() => void restoreItems(selectionIds)}
            label={t("library.restore", "恢复")}
          >
            <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />
          </BulkAction>
          <BulkAction
            wide={wide}
            destructive
            onClick={() => onRequestDeleteForever([...selectionIds])}
            label={t("library.deleteForever", "彻底删除")}
          >
            <XCircleIcon className="h-4 w-4" aria-hidden="true" />
          </BulkAction>
        </>
      ) : (
        <>
          <BulkAction
            wide={wide}
            onClick={(event) =>
              setMoveAnchor({ x: event.clientX, y: event.clientY })
            }
            label={t("library.bulkMoveTitle", "移动到知识库")}
          >
            <FolderInputIcon className="h-4 w-4" aria-hidden="true" />
          </BulkAction>
          <BulkAction
            wide={wide}
            onClick={(event) =>
              setTagAnchor({ x: event.clientX, y: event.clientY })
            }
            label={t("library.bulkAddTag", "批量打标签")}
          >
            <TagIcon className="h-4 w-4" aria-hidden="true" />
          </BulkAction>
          <BulkAction
            wide={wide}
            onClick={() =>
              void bulkUpdate(selectionIds, { isFavorite: true })
            }
            label={t("library.bulkFavorite", "批量收藏")}
          >
            <StarIcon className="h-4 w-4" aria-hidden="true" />
          </BulkAction>
          <BulkAction
            wide={wide}
            onClick={() => void setStatus(selectionIds, "archived")}
            label={t("library.archive", "归档")}
          >
            <ArchiveIcon className="h-4 w-4" aria-hidden="true" />
          </BulkAction>
          <BulkAction
            wide={wide}
            destructive
            onClick={() => void trashSelected()}
            label={t("library.moveToTrash", "移到回收站")}
          >
            <Trash2Icon className="h-4 w-4" aria-hidden="true" />
          </BulkAction>
        </>
      )}

      {moveAnchor ? (
        <ContextMenu
          x={moveAnchor.x}
          y={moveAnchor.y}
          items={moveMenuItems}
          onClose={() => setMoveAnchor(null)}
        />
      ) : null}

      {tagAnchor ? (
        <BulkTagPopover
          count={selectionIds.length}
          anchor={tagAnchor}
          onAdd={(tagName) =>
            void bulkUpdate(selectionIds, { addTagNames: [tagName] })
          }
          onClose={() => setTagAnchor(null)}
        />
      ) : null}
    </div>
  );
}
