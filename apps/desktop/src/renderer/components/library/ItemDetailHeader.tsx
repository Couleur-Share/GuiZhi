import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ChevronDownIcon,
  ClockIcon,
  FolderIcon,
  HashIcon,
  InboxIcon,
  PinIcon,
  RotateCcwIcon,
  SaveIcon,
  StarIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useSettingsStore } from "../../stores/settings.store";
import { ContextMenu } from "../ui/ContextMenu";
import { TagEditor } from "./TagEditor";
import { SourceChip } from "./SourceChip";
import { CHIP_BASE } from "./detail-chips";
import { formatItemTime, getItemTypeMeta } from "./type-meta";

/** 标题最多撑到三行（text-xl / leading-snug 约 28px 一行），再长就内部滚动 */
const TITLE_MAX_HEIGHT_PX = 84;

const CHIP_TONES = {
  muted: "border-border/70 text-muted-foreground",
  warning: "border-amber-500/40 text-amber-500",
  danger: "border-destructive/50 text-destructive",
} as const;

function MetaChip({
  icon,
  children,
  tone = "muted",
  title,
}: {
  icon?: ReactNode;
  children: ReactNode;
  tone?: keyof typeof CHIP_TONES;
  title?: string;
}) {
  return (
    <span className={`${CHIP_BASE} ${CHIP_TONES[tone]}`} title={title}>
      {icon}
      <span className="max-w-[12rem] truncate">{children}</span>
    </span>
  );
}

function ActionButton({
  onClick,
  title,
  active,
  children,
}: {
  onClick: () => void;
  title: string;
  active?: boolean;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-label={title}
      aria-pressed={active}
      className={`inline-flex h-8 w-8 items-center justify-center rounded-lg transition-colors ${
        active
          ? "bg-primary/15 text-primary"
          : "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

/** 所属知识库 chip：点击弹出集合列表 */
function CollectionChip({
  item,
  disabled,
}: {
  item: KnowledgeItem;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const collections = useCollectionStore((state) => state.collections);
  const updateSelected = useKnowledgeStore((state) => state.updateSelected);
  const chipRef = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);

  const current = collections.find(
    (collection) => collection.id === item.collectionId,
  );
  const emoji = current?.icon?.trim();

  return (
    <>
      <button
        ref={chipRef}
        type="button"
        disabled={disabled}
        onClick={(event) => {
          if (anchor) {
            setAnchor(null);
            return;
          }
          const rect = event.currentTarget.getBoundingClientRect();
          setAnchor({ x: rect.left, y: rect.bottom + 4 });
        }}
        aria-haspopup="menu"
        aria-expanded={anchor !== null}
        title={t("library.collection", "知识库")}
        className={`${CHIP_BASE} border-border/70 text-muted-foreground transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground disabled:pointer-events-none disabled:opacity-60`}
      >
        <span aria-hidden="true" className="leading-none">
          {emoji ?? <FolderIcon className="h-3.5 w-3.5" />}
        </span>
        <span className="max-w-[8rem] truncate">
          {current?.name ?? t("library.noCollection", "未分类")}
        </span>
        <ChevronDownIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
      </button>
      {anchor ? (
        <ContextMenu
          x={anchor.x}
          y={anchor.y}
          ignoreRef={chipRef}
          onClose={() => setAnchor(null)}
          items={[
            {
              label: t("library.noCollection", "未分类"),
              onClick: () => updateSelected({ collectionId: null }),
            },
            ...collections.map((collection) => ({
              label: collection.name,
              onClick: () => updateSelected({ collectionId: collection.id }),
            })),
          ]}
        />
      ) : null}
    </>
  );
}

/**
 * 详情头部：标题与动作同排，下面一排元信息 chip（类型 / 知识库 / 时间 / 字数），
 * 再下面是标签行。回收站条目只提供「恢复」。
 * 传入 onClose 时（详情浮层）在动作区末尾追加关闭按钮。
 */
export function ItemDetailHeader({
  item,
  isTrashed,
  onClose,
}: {
  item: KnowledgeItem;
  isTrashed: boolean;
  onClose?: () => void;
}) {
  const { t } = useTranslation();
  const isSaving = useKnowledgeStore((state) => state.isSaving);
  const hasUnsavedChanges = useKnowledgeStore(
    (state) => state.hasUnsavedChanges,
  );
  const saveError = useKnowledgeStore((state) => state.saveError);
  const updateSelected = useKnowledgeStore((state) => state.updateSelected);
  const flushPendingSave = useKnowledgeStore(
    (state) => state.flushPendingSave,
  );
  const setStatus = useKnowledgeStore((state) => state.setStatus);
  const toggleFavorite = useKnowledgeStore((state) => state.toggleFavorite);
  const togglePinned = useKnowledgeStore((state) => state.togglePinned);
  const moveToTrash = useKnowledgeStore((state) => state.moveToTrash);
  const restoreItems = useKnowledgeStore((state) => state.restoreItems);
  const autoSave = useSettingsStore((state) => state.autoSave);

  // 长标题（AI 拟题偶尔较长、导入条目更是整句文案）单行输入框只能看到开头，
  // 这里让标题随内容增高，超过三行再滚动
  const titleRef = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const node = titleRef.current;
    if (!node) {
      return;
    }
    node.style.height = "auto";
    node.style.height = `${Math.min(node.scrollHeight, TITLE_MAX_HEIGHT_PX)}px`;
    node.style.overflowY =
      node.scrollHeight > TITLE_MAX_HEIGHT_PX ? "auto" : "hidden";
  }, [item.title]);

  const typeMeta = getItemTypeMeta(item.itemType);
  // 保存失败与 autoSave 无关：改动已退回待保存队列，此时必须提示未落盘
  const isDirty = hasUnsavedChanges && (!autoSave || saveError !== null);
  const wordCount = item.content.trim().length;

  let saveTone: "muted" | "warning" | "danger" = "muted";
  let saveLabel = t("library.updatedAt", "更新于 {{time}}", {
    time: formatItemTime(item.updatedAt),
  });
  if (isSaving) {
    saveLabel = t("library.saving", "保存中…");
  } else if (saveError) {
    saveTone = "danger";
    saveLabel = t("library.saveFailed", "保存失败，改动未丢失");
  } else if (isDirty) {
    saveTone = "warning";
    saveLabel = t("library.unsaved", "未保存");
  }

  return (
    <div className="shrink-0 border-b border-border/60 px-6 pb-3 pt-4">
      <div className="flex items-start gap-2">
        <textarea
          ref={titleRef}
          rows={1}
          data-testid="item-title-input"
          value={item.title}
          onChange={(event) => updateSelected({ title: event.target.value })}
          // 标题是单行语义，回车不该在里面换行
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
            }
          }}
          readOnly={isTrashed}
          title={item.title}
          placeholder={t("library.titlePlaceholder", "标题")}
          className="min-w-0 flex-1 resize-none bg-transparent pt-0.5 text-xl font-semibold leading-snug text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
        />
        <div className="flex shrink-0 items-center gap-0.5">
          {isTrashed ? (
            <button
              type="button"
              onClick={() => void restoreItems([item.id])}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />
              {t("library.restore", "恢复")}
            </button>
          ) : (
            <>
              <ActionButton
                onClick={() => void toggleFavorite(item.id)}
                title={
                  item.isFavorite
                    ? t("library.unfavorite", "取消收藏")
                    : t("library.favorite", "收藏")
                }
                active={item.isFavorite}
              >
                <StarIcon
                  className={`h-4 w-4 ${item.isFavorite ? "fill-amber-400 text-amber-400" : ""}`}
                  aria-hidden="true"
                />
              </ActionButton>
              <ActionButton
                onClick={() => void togglePinned(item.id)}
                title={
                  item.isPinned
                    ? t("library.unpin", "取消置顶")
                    : t("library.pin", "置顶")
                }
                active={item.isPinned}
              >
                <PinIcon className="h-4 w-4" aria-hidden="true" />
              </ActionButton>
              {item.status === "inbox" ? (
                <ActionButton
                  onClick={() => void setStatus([item.id], "ready")}
                  title={t("library.markReady", "移入知识库")}
                >
                  <InboxIcon className="h-4 w-4" aria-hidden="true" />
                </ActionButton>
              ) : null}
              {item.status === "archived" ? (
                <ActionButton
                  onClick={() => void setStatus([item.id], "ready")}
                  title={t("library.unarchive", "取消归档")}
                >
                  <ArchiveRestoreIcon className="h-4 w-4" aria-hidden="true" />
                </ActionButton>
              ) : (
                <ActionButton
                  onClick={() => void setStatus([item.id], "archived")}
                  title={t("library.archive", "归档")}
                >
                  <ArchiveIcon className="h-4 w-4" aria-hidden="true" />
                </ActionButton>
              )}
              <ActionButton
                onClick={() => void moveToTrash([item.id])}
                title={t("library.moveToTrash", "移到回收站")}
              >
                <Trash2Icon className="h-4 w-4" aria-hidden="true" />
              </ActionButton>
            </>
          )}
          {onClose ? (
            <>
              <span aria-hidden="true" className="mx-1 h-4 w-px bg-border" />
              <ActionButton
                onClick={onClose}
                title={t("library.closeDetail", "关闭详情")}
              >
                <XIcon className="h-4 w-4" aria-hidden="true" />
              </ActionButton>
            </>
          ) : null}
        </div>
      </div>

      <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
        <MetaChip icon={typeMeta.icon}>
          {t(typeMeta.labelKey, typeMeta.fallback)}
        </MetaChip>
        <CollectionChip item={item} disabled={isTrashed} />
        <SourceChip item={item} />
        <MetaChip
          icon={<ClockIcon className="h-3.5 w-3.5" aria-hidden="true" />}
          tone={saveTone}
          title={saveError ?? undefined}
        >
          {saveLabel}
        </MetaChip>
        {wordCount > 0 ? (
          <MetaChip
            icon={<HashIcon className="h-3.5 w-3.5" aria-hidden="true" />}
          >
            {t("library.wordCount", "{{count}} 字", { count: wordCount })}
          </MetaChip>
        ) : null}
        {isDirty && !isTrashed ? (
          <button
            type="button"
            onClick={() => void flushPendingSave()}
            className="inline-flex h-6 items-center gap-1 rounded-md bg-primary px-2 text-[11px] font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            title={t("library.saveNow", "保存 (Ctrl+S)")}
          >
            <SaveIcon className="h-3 w-3" aria-hidden="true" />
            {t("library.save", "保存")}
          </button>
        ) : null}
      </div>

      {!isTrashed ? (
        <TagEditor
          item={item}
          onChange={(tagNames) => updateSelected({ tagNames })}
        />
      ) : null}
    </div>
  );
}
