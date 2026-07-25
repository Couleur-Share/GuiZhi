import { useRef, useState } from "react";
import {
  FolderIcon,
  FolderPlusIcon,
  PencilIcon,
  TagIcon,
  Trash2Icon,
  XCircleIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { Collection, Tag } from "@guizhi/shared/types";
import { TAG_COLOR_KEYS } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useTagStore } from "../../stores/tag.store";
import { ContextMenu, type ContextMenuItem } from "../ui/ContextMenu";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Modal } from "../ui/Modal";
import { Input } from "../ui/Input";
import { useToast } from "../ui/Toast";
import { LibraryScopeTabs } from "./LibraryScopeTabs";
import { LibrarySidebarRow } from "./LibrarySidebarRow";
import { TAG_DOT_CLASSES } from "./type-meta";

type EditModalState =
  | { kind: "create-collection" }
  | { kind: "rename-collection"; collection: Collection }
  | { kind: "rename-tag"; tag: Tag }
  | null;

type ConfirmDeleteState =
  | { kind: "collection"; collection: Collection }
  | { kind: "tag"; tag: Tag }
  | null;

const COLLECTION_ICON_PRESETS = [
  "📚",
  "💡",
  "🧠",
  "💻",
  "🎨",
  "🎬",
  "🎵",
  "🧪",
  "💼",
  "🌱",
  "⭐",
];

/** 菜单锚点：鼠标触发用光标位置，键盘触发用按钮左下角 */
function menuAnchor(event: React.MouseEvent): { x: number; y: number } {
  if (event.clientX !== 0 || event.clientY !== 0) {
    return { x: event.clientX, y: event.clientY };
  }
  const rect = event.currentTarget.getBoundingClientRect();
  return { x: rect.left, y: rect.bottom };
}

function SectionHeading({
  label,
  action,
}: {
  label: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="mb-1 mt-4 flex shrink-0 items-center justify-between gap-2 px-3">
      <span className="truncate text-[11px] font-semibold uppercase tracking-wider text-sidebar-foreground/50">
        {label}
      </span>
      {action}
    </div>
  );
}

function collectionIcon(icon: string | null | undefined): React.ReactNode {
  const emoji = icon?.trim();
  return emoji ? emoji : <FolderIcon className="h-4 w-4" aria-hidden="true" />;
}

/**
 * 知识库模块侧栏：范围分段控件 + 集合 + 标签 + 回收站。
 * 集合/标签支持行尾「更多」按钮与右键菜单（重命名、换色、删除）。
 */
export function SidebarLibraryPanel() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const scope = useKnowledgeStore((state) => state.scope);
  const activeCollectionId = useKnowledgeStore((state) => state.collectionId);
  const activeTagId = useKnowledgeStore((state) => state.tagId);
  const counts = useKnowledgeStore((state) => state.counts);
  const setScope = useKnowledgeStore((state) => state.setScope);
  const selectCollection = useKnowledgeStore(
    (state) => state.selectCollection,
  );
  const selectTag = useKnowledgeStore((state) => state.selectTag);
  const collections = useCollectionStore((state) => state.collections);
  const createCollection = useCollectionStore(
    (state) => state.createCollection,
  );
  const updateCollection = useCollectionStore(
    (state) => state.updateCollection,
  );
  const deleteCollection = useCollectionStore(
    (state) => state.deleteCollection,
  );
  const tags = useTagStore((state) => state.tags);
  const updateTag = useTagStore((state) => state.updateTag);
  const deleteTag = useTagStore((state) => state.deleteTag);
  const refreshAll = useKnowledgeStore((state) => state.refreshAll);

  const menuTriggerRef = useRef<HTMLElement | null>(null);
  const [menuState, setMenuState] = useState<{
    x: number;
    y: number;
    items: ContextMenuItem[];
  } | null>(null);
  const [editModal, setEditModal] = useState<EditModalState>(null);
  const [editName, setEditName] = useState("");
  const [editIcon, setEditIcon] = useState("");
  const [confirmDelete, setConfirmDelete] = useState<ConfirmDeleteState>(null);

  const isTrashActive =
    scope === "trash" && !activeCollectionId && !activeTagId;

  const openEditModal = (state: Exclude<EditModalState, null>) => {
    setEditName(
      state.kind === "rename-collection"
        ? state.collection.name
        : state.kind === "rename-tag"
          ? state.tag.name
          : "",
    );
    setEditIcon(
      state.kind === "rename-collection" ? state.collection.icon ?? "" : "",
    );
    setEditModal(state);
  };

  const submitEditModal = async () => {
    const name = editName.trim();
    if (!editModal || !name) {
      return;
    }
    const icon = editIcon.trim() || null;
    try {
      if (editModal.kind === "create-collection") {
        const created = await createCollection({ name, icon });
        selectCollection(created.id);
      } else if (editModal.kind === "rename-collection") {
        await updateCollection(editModal.collection.id, { name, icon });
      } else if (editModal.kind === "rename-tag") {
        await updateTag(editModal.tag.id, { name });
        await refreshAll();
      }
      setEditModal(null);
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : String(error),
        "error",
      );
    }
  };

  const closeMenu = () => {
    menuTriggerRef.current = null;
    setMenuState(null);
  };

  /** 打开行菜单；「更多」按钮再次点击时收起，右键则始终重开 */
  const openRowMenu = (event: React.MouseEvent, items: ContextMenuItem[]) => {
    event.preventDefault();
    const trigger = event.currentTarget as HTMLElement;
    const fromButton = event.type === "click";
    if (fromButton && menuTriggerRef.current === trigger) {
      closeMenu();
      return;
    }
    menuTriggerRef.current = fromButton ? trigger : null;
    setMenuState({ ...menuAnchor(event), items });
  };

  const openCollectionMenu = (
    event: React.MouseEvent,
    collection: Collection,
  ) => {
    openRowMenu(event, [
      {
        label: t("library.rename", "重命名"),
        icon: <PencilIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => openEditModal({ kind: "rename-collection", collection }),
      },
      {
        label: t("library.deleteCollection", "删除知识库"),
        description: t("library.deleteCollectionDesc", "不会删除其中的条目"),
        icon: <XCircleIcon className="h-4 w-4" aria-hidden="true" />,
        variant: "destructive",
        onClick: () => setConfirmDelete({ kind: "collection", collection }),
      },
    ]);
  };

  const openTagMenu = (event: React.MouseEvent, tag: Tag) => {
    openRowMenu(event, [
      {
        label: t("library.rename", "重命名"),
        icon: <PencilIcon className="h-4 w-4" aria-hidden="true" />,
        onClick: () => openEditModal({ kind: "rename-tag", tag }),
      },
      {
        label: t("library.tagColor", "颜色"),
        icon: <TagIcon className="h-4 w-4" aria-hidden="true" />,
        children: TAG_COLOR_KEYS.map((colorKey) => ({
          label: t(`library.color.${colorKey}`, colorKey),
          icon: (
            <span
              className={`h-3 w-3 rounded-full ${TAG_DOT_CLASSES[colorKey]}`}
              aria-hidden="true"
            />
          ),
          onClick: () => {
            void updateTag(tag.id, { colorKey }).then(() => refreshAll());
          },
        })),
      },
      {
        label: t("library.deleteTag", "删除标签"),
        icon: <XCircleIcon className="h-4 w-4" aria-hidden="true" />,
        variant: "destructive",
        onClick: () => setConfirmDelete({ kind: "tag", tag }),
      },
    ]);
  };

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3">
      {/* 范围分段控件 */}
      <div className="pt-3">
        <LibraryScopeTabs />
      </div>

      {/* 集合 */}
      <SectionHeading
        label={t("library.collections", "知识库")}
        action={
          <button
            type="button"
            onClick={() => openEditModal({ kind: "create-collection" })}
            className="rounded-lg p-1 text-sidebar-foreground/50 transition-colors hover:bg-sidebar-accent hover:text-primary"
            title={t("library.newCollection", "新建知识库")}
            aria-label={t("library.newCollection", "新建知识库")}
          >
            <FolderPlusIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
        }
      />
      {collections.length === 0 ? (
        <p className="px-3 py-1 text-xs text-sidebar-foreground/40">
          {t("library.noCollections", "还没有知识库")}
        </p>
      ) : (
        collections.map((collection) => (
          <LibrarySidebarRow
            key={collection.id}
            icon={collectionIcon(collection.icon)}
            label={collection.name}
            count={counts?.byCollection[collection.id]}
            active={activeCollectionId === collection.id}
            onClick={() => selectCollection(collection.id)}
            onMore={(event) => openCollectionMenu(event, collection)}
            moreLabel={t("library.moreActions", "更多操作")}
            onContextMenu={(event) => openCollectionMenu(event, collection)}
          />
        ))
      )}

      {/* 标签 */}
      <SectionHeading label={t("library.tags", "标签")} />
      {tags.length === 0 ? (
        <p className="px-3 py-1 text-xs text-sidebar-foreground/40">
          {t("library.noTags", "在条目详情中添加标签")}
        </p>
      ) : (
        tags.map((tag) => (
          <LibrarySidebarRow
            key={tag.id}
            icon={
              <span
                className={`h-2.5 w-2.5 rounded-full ${TAG_DOT_CLASSES[tag.colorKey]}`}
                aria-hidden="true"
              />
            }
            label={tag.name}
            count={counts?.byTag[tag.id]}
            active={activeTagId === tag.id}
            onClick={() => selectTag(tag.id)}
            onMore={(event) => openTagMenu(event, tag)}
            moreLabel={t("library.moreActions", "更多操作")}
            onContextMenu={(event) => openTagMenu(event, tag)}
          />
        ))
      )}

      {/* 回收站 */}
      <div className="mt-auto pt-4">
        <LibrarySidebarRow
          icon={<Trash2Icon className="h-4 w-4" aria-hidden="true" />}
          label={t("library.scopeTrash", "回收站")}
          count={counts?.trash}
          active={isTrashActive}
          onClick={() => setScope("trash")}
        />
      </div>

      {menuState ? (
        <ContextMenu
          x={menuState.x}
          y={menuState.y}
          items={menuState.items}
          ignoreRef={menuTriggerRef}
          onClose={closeMenu}
        />
      ) : null}

      <Modal
        isOpen={editModal !== null}
        onClose={() => setEditModal(null)}
        title={
          editModal?.kind === "create-collection"
            ? t("library.newCollection", "新建知识库")
            : t("library.rename", "重命名")
        }
        size="sm"
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void submitEditModal();
          }}
          className="space-y-4"
        >
          <Input
            autoFocus
            value={editName}
            onChange={(event) => setEditName(event.target.value)}
            placeholder={t("library.namePlaceholder", "名称")}
          />
          {editModal && editModal.kind !== "rename-tag" ? (
            <div className="space-y-1.5">
              <span className="text-xs text-muted-foreground">
                {t("library.collectionIcon", "图标")}
              </span>
              <div className="flex flex-wrap gap-1">
                <button
                  type="button"
                  onClick={() => setEditIcon("")}
                  aria-pressed={editIcon === ""}
                  title={t("library.collectionIconDefault", "默认图标")}
                  className={`flex h-8 w-8 items-center justify-center rounded-lg border transition-colors ${
                    editIcon === ""
                      ? "border-primary/50 bg-primary/10 text-primary"
                      : "border-border text-muted-foreground hover:bg-muted/60"
                  }`}
                >
                  <FolderIcon className="h-4 w-4" aria-hidden="true" />
                </button>
                {COLLECTION_ICON_PRESETS.map((emoji) => (
                  <button
                    key={emoji}
                    type="button"
                    onClick={() => setEditIcon(emoji)}
                    aria-pressed={editIcon === emoji}
                    className={`flex h-8 w-8 items-center justify-center rounded-lg border text-base transition-colors ${
                      editIcon === emoji
                        ? "border-primary/50 bg-primary/10"
                        : "border-border hover:bg-muted/60"
                    }`}
                  >
                    {emoji}
                  </button>
                ))}
              </div>
            </div>
          ) : null}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setEditModal(null)}
              className="inline-flex h-9 items-center rounded-lg border border-border px-4 text-sm text-foreground transition-colors hover:bg-muted/60"
            >
              {t("common.cancel", "取消")}
            </button>
            <button
              type="submit"
              disabled={!editName.trim()}
              className="inline-flex h-9 items-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
            >
              {t("common.confirm", "确认")}
            </button>
          </div>
        </form>
      </Modal>

      <ConfirmDialog
        isOpen={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        onConfirm={() => {
          if (confirmDelete?.kind === "collection") {
            void deleteCollection(confirmDelete.collection.id).then(() => {
              if (activeCollectionId === confirmDelete.collection.id) {
                setScope("all");
              }
              void refreshAll();
            });
          } else if (confirmDelete?.kind === "tag") {
            void deleteTag(confirmDelete.tag.id).then(() => {
              if (activeTagId === confirmDelete.tag.id) {
                setScope("all");
              }
              void refreshAll();
            });
          }
          setConfirmDelete(null);
        }}
        title={
          confirmDelete?.kind === "collection"
            ? t("library.deleteCollection", "删除知识库")
            : t("library.deleteTag", "删除标签")
        }
        message={
          confirmDelete?.kind === "collection"
            ? t(
                "library.deleteCollectionConfirm",
                "删除知识库「{{name}}」？其中的条目会保留并变为未分类。",
                { name: confirmDelete.collection.name },
              )
            : t(
                "library.deleteTagConfirm",
                "删除标签「{{name}}」？相关条目不会被删除。",
                { name: confirmDelete?.kind === "tag" ? confirmDelete.tag.name : "" },
              )
        }
        confirmText={t("common.confirm", "确认")}
        cancelText={t("common.cancel", "取消")}
        variant="destructive"
      />
    </div>
  );
}
