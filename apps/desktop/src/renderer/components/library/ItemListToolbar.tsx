import { useRef, useState } from "react";
import {
  CheckIcon,
  ChevronDownIcon,
  LayoutGridIcon,
  ListIcon,
  Trash2Icon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type {
  KnowledgeSortField,
  KnowledgeSortOrder,
} from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore, type LibraryViewMode } from "../../stores/ui.store";
import { ContextMenu, type ContextMenuItem } from "../ui/ContextMenu";
import { LibraryFilterSummary } from "./LibraryFilterSummary";

interface SortOption {
  labelKey: string;
  fallback: string;
  sortBy: KnowledgeSortField;
  sortOrder: KnowledgeSortOrder;
}

const SORT_OPTIONS: SortOption[] = [
  {
    labelKey: "library.sortUpdatedDesc",
    fallback: "最近更新",
    sortBy: "updatedAt",
    sortOrder: "desc",
  },
  {
    labelKey: "library.sortUpdatedAsc",
    fallback: "最早更新",
    sortBy: "updatedAt",
    sortOrder: "asc",
  },
  {
    labelKey: "library.sortCreatedDesc",
    fallback: "最近创建",
    sortBy: "createdAt",
    sortOrder: "desc",
  },
  {
    labelKey: "library.sortCreatedAsc",
    fallback: "最早创建",
    sortBy: "createdAt",
    sortOrder: "asc",
  },
  {
    labelKey: "library.sortTitleAsc",
    fallback: "标题 A→Z",
    sortBy: "title",
    sortOrder: "asc",
  },
  {
    labelKey: "library.sortTitleDesc",
    fallback: "标题 Z→A",
    sortBy: "title",
    sortOrder: "desc",
  },
];

const VIEW_OPTIONS: Array<{
  mode: LibraryViewMode;
  labelKey: string;
  fallback: string;
  icon: typeof ListIcon;
}> = [
  {
    mode: "card",
    labelKey: "library.viewCard",
    fallback: "卡片视图",
    icon: LayoutGridIcon,
  },
  {
    mode: "list",
    labelKey: "library.viewList",
    fallback: "列表视图",
    icon: ListIcon,
  },
];

/**
 * 列表工具条：条目总数 + 排序下拉 + 视图切换（回收站视图额外提供清空入口）。
 * 卡片视图与列表视图共用这一条，切换按钮位置因此保持一致。
 * 搜索态下排序由 FTS 相关度接管，下拉禁用并显示「相关度」。
 */
export function ItemListToolbar({
  onEmptyTrash,
}: {
  /** 仅回收站视图且非空时传入 */
  onEmptyTrash?: () => void;
}) {
  const { t } = useTranslation();
  const total = useKnowledgeStore((state) => state.total);
  const searchQuery = useKnowledgeStore((state) => state.searchQuery);
  const sortBy = useKnowledgeStore((state) => state.sortBy);
  const sortOrder = useKnowledgeStore((state) => state.sortOrder);
  const setSort = useKnowledgeStore((state) => state.setSort);
  const viewMode = useUIStore((state) => state.libraryViewMode);
  const setViewMode = useUIStore((state) => state.setLibraryViewMode);

  const toolbarRef = useRef<HTMLDivElement>(null);
  const sortButtonRef = useRef<HTMLButtonElement>(null);
  const [menuAnchor, setMenuAnchor] = useState<{ x: number; y: number } | null>(
    null,
  );

  const isSearching = searchQuery.trim().length > 0;
  const current =
    SORT_OPTIONS.find(
      (option) => option.sortBy === sortBy && option.sortOrder === sortOrder,
    ) ?? SORT_OPTIONS[0];
  const sortLabel = isSearching
    ? t("library.sortRelevance", "相关度")
    : t(current.labelKey, current.fallback);

  const sortMenuItems: ContextMenuItem[] = SORT_OPTIONS.map((option) => {
    const selected = option.sortBy === sortBy && option.sortOrder === sortOrder;
    return {
      label: t(option.labelKey, option.fallback),
      icon: selected ? (
        <CheckIcon className="h-4 w-4 text-primary" aria-hidden="true" />
      ) : (
        <span className="h-4 w-4" aria-hidden="true" />
      ),
      onClick: () => setSort(option.sortBy, option.sortOrder),
    };
  });

  return (
    <div
      ref={toolbarRef}
      tabIndex={-1}
      className="library-list-toolbar flex min-h-9 shrink-0 flex-wrap items-center gap-1 border-b border-border px-3 py-1 focus:outline-none"
    >
      <span className="mr-auto text-xs tabular-nums text-muted-foreground">
        {t("library.itemCount", "共 {{count}} 个", { count: total })}
      </span>
      <LibraryFilterSummary fallbackFocusRef={toolbarRef} />

      {onEmptyTrash ? (
        <button
          type="button"
          onClick={onEmptyTrash}
          className="inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-xs text-destructive transition-colors hover:bg-destructive/10"
        >
          <Trash2Icon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("library.emptyTrash", "清空回收站")}
        </button>
      ) : null}

      <button
        ref={sortButtonRef}
        type="button"
        disabled={isSearching}
        onClick={(event) => {
          if (menuAnchor) {
            setMenuAnchor(null);
            return;
          }
          const rect = event.currentTarget.getBoundingClientRect();
          setMenuAnchor({ x: rect.left, y: rect.bottom + 4 });
        }}
        aria-haspopup="menu"
        aria-expanded={menuAnchor !== null}
        aria-label={`${t("library.sortBy", "排序")}: ${sortLabel}`}
        className="inline-flex shrink-0 items-center gap-0.5 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
      >
        <span className="max-w-[7rem] truncate">{sortLabel}</span>
        <ChevronDownIcon
          className={`h-3.5 w-3.5 transition-transform ${menuAnchor ? "rotate-180" : ""}`}
          aria-hidden="true"
        />
      </button>

      <div className="library-list-view-toggle relative flex shrink-0 items-center overflow-hidden rounded-md border border-border bg-muted/30">
        <div
          aria-hidden="true"
          className="absolute h-full w-1/2 rounded-[3px] bg-primary transition-[left] duration-base ease-standard"
          style={{ left: viewMode === "card" ? "0%" : "50%" }}
        />
        {VIEW_OPTIONS.map((option) => {
          const active = viewMode === option.mode;
          const Icon = option.icon;
          return (
            <button
              key={option.mode}
              type="button"
              onClick={() => setViewMode(option.mode)}
              aria-pressed={active}
              title={t(option.labelKey, option.fallback)}
              aria-label={t(option.labelKey, option.fallback)}
              className={`relative z-10 p-1.5 transition-colors duration-base ${
                active
                  ? "text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              <Icon className="h-3.5 w-3.5" aria-hidden="true" />
            </button>
          );
        })}
      </div>

      {menuAnchor ? (
        <ContextMenu
          x={menuAnchor.x}
          y={menuAnchor.y}
          items={sortMenuItems}
          ignoreRef={sortButtonRef}
          onClose={() => setMenuAnchor(null)}
        />
      ) : null}
    </div>
  );
}
