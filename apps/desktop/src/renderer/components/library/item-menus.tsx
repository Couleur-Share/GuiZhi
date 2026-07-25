import { useState } from "react";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  InboxIcon,
  PinIcon,
  RotateCcwIcon,
  StarIcon,
  TagIcon,
  Trash2Icon,
  XCircleIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItemListEntry } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useCollectionStore } from "../../stores/collection.store";
import type { ContextMenuItem } from "../ui/ContextMenu";
import { ConfirmDialog } from "../ui/ConfirmDialog";

/** 需要二次确认的不可逆动作 */
export type ItemConfirmState =
  | { kind: "delete-forever"; ids: string[] }
  | { kind: "empty-trash" }
  | null;

/**
 * 条目动作菜单：卡片视图与列表视图共用同一套语义。
 * 菜单按当前 scope 分支——回收站只给恢复 / 彻底删除，其余给收藏、置顶、归档、移到回收站。
 */
export function useItemMenus({
  onEditTags,
}: {
  /** 列表视图传入：打开标签编辑浮层（详情页有自己的 + 入口，不用传） */
  onEditTags?: (entry: KnowledgeItemListEntry) => void;
} = {}) {
  const { t } = useTranslation();
  const scope = useKnowledgeStore((state) => state.scope);
  const selectionIds = useKnowledgeStore((state) => state.selectionIds);
  const setStatus = useKnowledgeStore((state) => state.setStatus);
  const toggleFavorite = useKnowledgeStore((state) => state.toggleFavorite);
  const togglePinned = useKnowledgeStore((state) => state.togglePinned);
  const moveToTrash = useKnowledgeStore((state) => state.moveToTrash);
  const restoreItems = useKnowledgeStore((state) => state.restoreItems);
  const bulkMoveToCollection = useKnowledgeStore(
    (state) => state.bulkMoveToCollection,
  );
  const collections = useCollectionStore((state) => state.collections);
  const [confirmState, setConfirmState] = useState<ItemConfirmState>(null);

  const selectionCount = selectionIds.length;

  const buildBulkMenu = (): ContextMenuItem[] => {
    if (scope === "trash") {
      return [
        {
          label: t("library.bulkRestore", "恢复 {{count}} 项", {
            count: selectionCount,
          }),
          icon: <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />,
          onClick: () => void restoreItems(selectionIds),
        },
        {
          label: t("library.bulkDeleteForever", "彻底删除 {{count}} 项", {
            count: selectionCount,
          }),
          icon: <XCircleIcon className="h-4 w-4" aria-hidden="true" />,
          variant: "destructive",
          onClick: () =>
            setConfirmState({ kind: "delete-forever", ids: [...selectionIds] }),
        },
      ];
    }
    return [
      {
        label: t("library.bulkArchive", "归档 {{count}} 项", {
          count: selectionCount,
        }),
        icon: <ArchiveIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => void setStatus(selectionIds, "archived"),
      },
      {
        label: t("library.bulkMoveToTrash", "移到回收站（{{count}} 项）", {
          count: selectionCount,
        }),
        icon: <Trash2Icon className="h-4 w-4" aria-hidden="true" />,
        variant: "destructive",
        onClick: () => void moveToTrash(selectionIds),
      },
    ];
  };

  const buildMoveMenu = (): ContextMenuItem[] => [
    {
      label: t("library.noCollection", "未分类"),
      onClick: () => void bulkMoveToCollection(selectionIds, null),
    },
    ...collections.map((collection) => ({
      label: collection.name,
      onClick: () => void bulkMoveToCollection(selectionIds, collection.id),
    })),
  ];

  const buildEntryMenu = (
    entry: KnowledgeItemListEntry,
  ): ContextMenuItem[] => {
    // 右键落在多选组上时，动作作用于整组
    if (selectionCount > 1 && selectionIds.includes(entry.id)) {
      return buildBulkMenu();
    }
    if (scope === "trash") {
      return [
        {
          label: t("library.restore", "恢复"),
          icon: <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />,
          onClick: () => void restoreItems([entry.id]),
        },
        {
          label: t("library.deleteForever", "彻底删除"),
          icon: <XCircleIcon className="h-4 w-4" aria-hidden="true" />,
          variant: "destructive",
          onClick: () =>
            setConfirmState({ kind: "delete-forever", ids: [entry.id] }),
        },
      ];
    }

    const items: ContextMenuItem[] = [
      {
        label: entry.isFavorite
          ? t("library.unfavorite", "取消收藏")
          : t("library.favorite", "收藏"),
        icon: <StarIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => void toggleFavorite(entry.id),
      },
      {
        label: entry.isPinned
          ? t("library.unpin", "取消置顶")
          : t("library.pin", "置顶"),
        icon: <PinIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => void togglePinned(entry.id),
      },
    ];

    if (onEditTags) {
      items.push({
        label: t("library.editTags", "编辑标签"),
        icon: <TagIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => onEditTags(entry),
      });
    }

    if (entry.status === "inbox") {
      items.push({
        label: t("library.markReady", "移入知识库"),
        icon: <InboxIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => void setStatus([entry.id], "ready"),
      });
    }
    if (entry.status === "archived") {
      items.push({
        label: t("library.unarchive", "取消归档"),
        icon: <ArchiveRestoreIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => void setStatus([entry.id], "ready"),
      });
    } else {
      items.push({
        label: t("library.archive", "归档"),
        icon: <ArchiveIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => void setStatus([entry.id], "archived"),
      });
    }

    items.push({
      label: t("library.moveToTrash", "移到回收站"),
      icon: <Trash2Icon className="h-4 w-4" aria-hidden="true" />,
      variant: "destructive",
      onClick: () => void moveToTrash([entry.id]),
    });

    return items;
  };

  return {
    buildEntryMenu,
    buildBulkMenu,
    buildMoveMenu,
    confirmState,
    setConfirmState,
  };
}

/** 与 useItemMenus 配套的确认弹窗 */
export function ItemConfirmDialog({
  state,
  onClose,
}: {
  state: ItemConfirmState;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const deleteForever = useKnowledgeStore((store) => store.deleteForever);
  const emptyTrash = useKnowledgeStore((store) => store.emptyTrash);

  return (
    <ConfirmDialog
      isOpen={state !== null}
      onClose={onClose}
      onConfirm={() => {
        if (state?.kind === "delete-forever") {
          void deleteForever(state.ids);
        } else if (state?.kind === "empty-trash") {
          void emptyTrash();
        }
        onClose();
      }}
      title={
        state?.kind === "empty-trash"
          ? t("library.emptyTrash", "清空回收站")
          : t("library.deleteForever", "彻底删除")
      }
      message={
        state?.kind === "empty-trash"
          ? t(
              "library.emptyTrashConfirm",
              "回收站中的所有条目将被永久删除，无法恢复。",
            )
          : state?.kind === "delete-forever" && state.ids.length > 1
            ? t(
                "library.deleteForeverConfirmMany",
                "选中的 {{count}} 个条目将被永久删除，无法恢复。",
                { count: state.ids.length },
              )
            : t(
                "library.deleteForeverConfirm",
                "该条目将被永久删除，无法恢复。",
              )
      }
      confirmText={t("common.confirm", "确认")}
      cancelText={t("common.cancel", "取消")}
      variant="destructive"
    />
  );
}
