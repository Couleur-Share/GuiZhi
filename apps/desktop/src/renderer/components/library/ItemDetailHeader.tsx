import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ChevronDownIcon,
  ClockIcon,
  FolderIcon,
  Maximize2Icon,
  Minimize2Icon,
  MoreHorizontalIcon,
  MessageCircleIcon,
  SlidersHorizontalIcon,
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
import { useUIStore } from "../../stores/ui.store";
import { useSourceComments } from "./SourceCommentsContext";
import { ContextMenu } from "../ui/ContextMenu";
import { SourceChip } from "./SourceChip";
import { CHIP_BASE } from "./detail-chips";
import { formatItemTime } from "./type-meta";

/** 「更多」菜单右对齐到按钮：动作区贴着面板右缘，从左缘展开必然开到窗口外再被回弹 */
const MORE_MENU_WIDTH_PX = 160;

const ACTION_BUTTON_BASE =
  "inline-flex h-8 w-8 items-center justify-center rounded-lg transition-colors";
const ACTION_BUTTON_IDLE =
  "text-muted-foreground hover:bg-accent/60 hover:text-foreground";

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
      className={`${ACTION_BUTTON_BASE} ${
        active ? "bg-primary/15 text-primary" : ACTION_BUTTON_IDLE
      }`}
    >
      {children}
    </button>
  );
}

/**
 * 「更多」菜单：置顶、归档、移到回收站三个低频动作收在这里，
 * 动作区因此从五个图标缩到三个——横向空间让给标题与元信息 chip，
 * 五个一字排开时窄详情栏里一行标题只剩七八个字、chip 也会碎成四行。
 * 顺带把破坏性的删除挡在一次点击之后。
 */
function MoreActionsButton({
  item,
  onOpenTools,
}: {
  item: KnowledgeItem;
  onOpenTools?: () => void;
}) {
  const sourceComments = useSourceComments();
  const { t } = useTranslation();
  const setStatus = useKnowledgeStore((state) => state.setStatus);
  const togglePinned = useKnowledgeStore((state) => state.togglePinned);
  const moveToTrash = useKnowledgeStore((state) => state.moveToTrash);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);

  const isArchived = item.status === "archived";
  const moreLabel = t("library.moreActions", "更多操作");

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={(event) => {
          if (anchor) {
            setAnchor(null);
            return;
          }
          const rect = event.currentTarget.getBoundingClientRect();
          setAnchor({
            x: rect.right - MORE_MENU_WIDTH_PX,
            y: rect.bottom + 4,
          });
        }}
        aria-haspopup="menu"
        aria-expanded={anchor !== null}
        aria-label={moreLabel}
        className={`${ACTION_BUTTON_BASE} ${ACTION_BUTTON_IDLE}`}
      >
        <MoreHorizontalIcon className="h-4 w-4" aria-hidden="true" />
      </button>
      {anchor ? (
        <ContextMenu
          x={anchor.x}
          y={anchor.y}
          ignoreRef={buttonRef}
          onClose={() => setAnchor(null)}
          items={[
            ...(sourceComments?.supported
              ? [
                  {
                    label:
                      sourceComments.comments.length > 0
                        ? t("library.viewSourceComments", "查看来源评论")
                        : t("library.collectSourceComments", "采集评论"),
                    icon: (
                      <MessageCircleIcon
                        className="h-4 w-4"
                        aria-hidden="true"
                      />
                    ),
                    onClick: () => {
                      sourceComments.setOpen(true);
                      onOpenTools?.();
                    },
                  },
                ]
              : []),
            {
              label: item.isPinned
                ? t("library.unpin", "取消置顶")
                : t("library.pin", "置顶"),
              icon: <PinIcon className="h-4 w-4" aria-hidden="true" />,
              onClick: () => void togglePinned(item.id),
            },
            {
              label: isArchived
                ? t("library.unarchive", "取消归档")
                : t("library.archive", "归档"),
              icon: isArchived ? (
                <ArchiveRestoreIcon className="h-4 w-4" aria-hidden="true" />
              ) : (
                <ArchiveIcon className="h-4 w-4" aria-hidden="true" />
              ),
              onClick: () =>
                void setStatus([item.id], isArchived ? "active" : "archived"),
            },
            {
              label: t("library.moveToTrash", "移到回收站"),
              icon: <Trash2Icon className="h-4 w-4" aria-hidden="true" />,
              variant: "destructive",
              onClick: () => void moveToTrash([item.id]),
            },
          ]}
        />
      ) : null}
    </>
  );
}

/**
 * 所属知识库 chip：点击弹出集合列表。
 *
 * 走 bulkMoveToCollection 而不是防抖保存队列——分类是过滤条件，
 * 改完侧栏的「未分类」与各知识库计数、以及当前列表都要立刻跟上；
 * 混进正文的 800ms 防抖里只会让侧栏读数停留在旧值。
 */
function CollectionChip({
  item,
  disabled,
}: {
  item: KnowledgeItem;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const collections = useCollectionStore((state) => state.collections);
  const bulkMoveToCollection = useKnowledgeStore(
    (state) => state.bulkMoveToCollection,
  );
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
              onClick: () => void bulkMoveToCollection([item.id], null),
            },
            ...collections.map((collection) => ({
              label: collection.name,
              onClick: () =>
                void bulkMoveToCollection([item.id], collection.id),
            })),
          ]}
        />
      ) : null}
    </>
  );
}

/**
 * 详情头部：标题独占一行，下面一排元信息 chip（类型 / 知识库 / 来源 / 时间 / 字数）
 * 与右对齐的动作区，再下面是标签行。回收站条目只提供「恢复」。
 * 传入 onClose 时（详情浮层）在动作区末尾追加关闭按钮。
 */
export function ItemDetailHeader({
  item,
  isTrashed,
  onClose,
  askOpen = false,
  onToggleAsk,
  toolsOpen = false,
  onToggleTools,
}: {
  item: KnowledgeItem;
  isTrashed: boolean;
  onClose?: () => void;
  askOpen?: boolean;
  onToggleAsk?: () => void;
  toolsOpen?: boolean;
  onToggleTools?: () => void;
}) {
  const { t } = useTranslation();
  const isSaving = useKnowledgeStore((state) => state.isSaving);
  const hasUnsavedChanges = useKnowledgeStore(
    (state) => state.hasUnsavedChanges,
  );
  const saveError = useKnowledgeStore((state) => state.saveError);
  const updateSelected = useKnowledgeStore((state) => state.updateSelected);
  const flushPendingSave = useKnowledgeStore((state) => state.flushPendingSave);
  const toggleFavorite = useKnowledgeStore((state) => state.toggleFavorite);
  const restoreItems = useKnowledgeStore((state) => state.restoreItems);
  const autoSave = useSettingsStore((state) => state.autoSave);
  const isFocusReadingMode = useUIStore((state) => state.isFocusReadingMode);
  const toggleFocusReadingMode = useUIStore(
    (state) => state.toggleFocusReadingMode,
  );

  // 长标题（AI 拟题偶尔较长、导入条目更是整句文案）单行输入框只能看到开头，
  // 这里让标题随内容增高，超过三行再滚动
  const titleRef = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const node = titleRef.current;
    if (!node) {
      return;
    }
    const resize = () => {
      const style = getComputedStyle(node);
      const limit =
        (parseFloat(style.lineHeight) || 28) * 3 +
        (parseFloat(style.paddingTop) || 0) +
        (parseFloat(style.paddingBottom) || 0);
      node.style.height = "auto";
      node.style.height = `${Math.min(node.scrollHeight, limit)}px`;
      node.style.overflowY = node.scrollHeight > limit ? "auto" : "hidden";
    };
    resize();
    // 侧栏展开和窗口缩放也会让标题换行，只监听宽度以免高度更新触发循环。
    if (typeof ResizeObserver === "undefined") return;
    let width = node.clientWidth;
    const observer = new ResizeObserver(() => {
      if (width === node.clientWidth) return;
      width = node.clientWidth;
      resize();
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [item.title]);

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
    <header className="shrink-0 px-6 pb-3 pt-5" data-testid="article-header">
      {/* 标题独占整行：与动作区同排时，窄详情栏里长标题会被挤成三行还看不全 */}
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
        spellCheck={false}
        autoCorrect="off"
        autoCapitalize="off"
        aria-label={t("library.titlePlaceholder", "标题")}
        placeholder={t("library.titlePlaceholder", "标题")}
        className="block w-full resize-none border-none bg-transparent rounded-sm pt-0.5 text-[1.375rem] font-semibold leading-snug text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-0 focus-visible:outline-none focus-visible:ring-0"
      />

      {/* 动作区并进元信息行右侧：不新增一行高度，与 Wiki 页面详情同形态 */}
      <div className="mt-2.5 flex items-start gap-2">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
          <CollectionChip item={item} disabled={isTrashed} />
          <SourceChip item={item} />
          {wordCount > 0 ? (
            <span className="px-1 text-xs text-muted-foreground">
              {t("library.wordCount", "{{count}} 字", { count: wordCount })}
            </span>
          ) : null}
          {/* 置顶与归档改由菜单切换，状态就得由 chip 说出来——原先只靠按钮
              的高亮底色与两个长得极像的归档图标区分，本就看不出来 */}
          {item.isPinned ? (
            <MetaChip
              icon={<PinIcon className="h-3.5 w-3.5" aria-hidden="true" />}
            >
              {t("library.pinnedBadge", "已置顶")}
            </MetaChip>
          ) : null}
          {item.status === "archived" ? (
            <MetaChip
              icon={<ArchiveIcon className="h-3.5 w-3.5" aria-hidden="true" />}
            >
              {t("library.archivedBadge", "已归档")}
            </MetaChip>
          ) : null}
          {isDirty || isSaving || saveError ? (
            <MetaChip
              icon={<ClockIcon className="h-3.5 w-3.5" aria-hidden="true" />}
              tone={saveTone}
              title={saveError ?? undefined}
            >
              {saveLabel}
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

        {/* 按钮 32px、chip 24px，上提 4px 才与第一行 chip 对齐 */}
        <div className="-mt-1 flex shrink-0 items-center gap-0.5">
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
              {onToggleAsk ? <button type="button" onClick={onToggleAsk} aria-expanded={askOpen} className="mr-2 rounded-lg border border-border px-2 py-1.5 text-xs text-foreground hover:bg-accent">{t("articleAsk.title", "围绕本文提问")}</button> : null}
              {onToggleTools ? (
                <button
                  type="button"
                  onClick={onToggleTools}
                  aria-controls="article-tools-panel"
                  aria-expanded={toolsOpen}
                  className={`${ACTION_BUTTON_BASE} ${ACTION_BUTTON_IDLE}`}
                  aria-label={t("articleReader.tools", "文章信息与工具")}
                  title={t(
                    "articleReader.toolsHint",
                    "标签、配图、内容处理与来源版本",
                  )}
                >
                  <SlidersHorizontalIcon
                    className="h-4 w-4"
                    aria-hidden="true"
                  />
                </button>
              ) : null}
              <ActionButton
                onClick={() => toggleFocusReadingMode()}
                title={
                  isFocusReadingMode
                    ? t("library.exitFocusReading", "退出专注阅读 (Esc)")
                    : t("library.enterFocusReading", "专注阅读 (Alt+Z)")
                }
                active={isFocusReadingMode}
              >
                {isFocusReadingMode ? (
                  <Minimize2Icon className="h-4 w-4" aria-hidden="true" />
                ) : (
                  <Maximize2Icon className="h-4 w-4" aria-hidden="true" />
                )}
              </ActionButton>
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
              <MoreActionsButton item={item} onOpenTools={onToggleTools} />
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
    </header>
  );
}
