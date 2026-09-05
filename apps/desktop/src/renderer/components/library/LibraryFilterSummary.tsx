import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";
import { ChevronDownIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useTagStore } from "../../stores/tag.store";
import { getSourcePlatformMeta } from "./platform-meta";

interface FilterEntry {
  kind: string;
  name: string;
  remove: () => void;
}

/** 单条件由侧栏高亮表达；组合条件集中管理，不占用工具栏的横向空间。 */
export function LibraryFilterSummary({
  fallbackFocusRef,
}: {
  fallbackFocusRef: RefObject<HTMLDivElement>;
}) {
  const { t } = useTranslation();
  const collectionId = useKnowledgeStore((state) => state.collectionId);
  const tagId = useKnowledgeStore((state) => state.tagId);
  const platform = useKnowledgeStore((state) => state.platform);
  const selectCollection = useKnowledgeStore((state) => state.selectCollection);
  const selectTag = useKnowledgeStore((state) => state.selectTag);
  const selectPlatform = useKnowledgeStore((state) => state.selectPlatform);
  const clearFacetFilters = useKnowledgeStore(
    (state) => state.clearFacetFilters,
  );
  const collections = useCollectionStore((state) => state.collections);
  const tags = useTagStore((state) => state.tags);
  const filters: FilterEntry[] = [];
  if (collectionId) {
    filters.push({
      kind: t("library.collection", "知识库"),
      name:
        collections.find((entry) => entry.id === collectionId)?.name ??
        t("library.collection", "知识库"),
      remove: () => selectCollection(null),
    });
  }
  if (tagId) {
    filters.push({
      kind: t("library.tags", "标签"),
      name:
        tags.find((entry) => entry.id === tagId)?.name ??
        t("library.tags", "标签"),
      remove: () => selectTag(null),
    });
  }
  if (platform) {
    const meta = getSourcePlatformMeta(platform);
    filters.push({
      kind: t("library.platforms", "平台"),
      name: t(meta.labelKey, meta.fallback),
      remove: () => selectPlatform(null),
    });
  }
  return filters.length > 1 ? (
    <FilterSummaryButton
      filters={filters}
      onClear={clearFacetFilters}
      fallbackFocusRef={fallbackFocusRef}
    />
  ) : null;
}

function FilterSummaryButton({
  filters,
  onClear,
  fallbackFocusRef,
}: {
  filters: FilterEntry[];
  onClear: () => void;
  fallbackFocusRef: RefObject<HTMLDivElement>;
}) {
  const { t } = useTranslation();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);
  const [position, setPosition] = useState({ x: 0, y: 0 });

  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (!anchor || !panel) return;
    const rect = panel.getBoundingClientRect();
    setPosition({
      x: Math.max(8, Math.min(anchor.x, window.innerWidth - rect.width - 8)),
      y: Math.max(8, Math.min(anchor.y, window.innerHeight - rect.height - 8)),
    });
    panel.querySelector<HTMLButtonElement>("button")?.focus();
  }, [anchor]);

  useEffect(() => {
    if (!anchor) return;
    const closeOutside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (
        !panelRef.current?.contains(target) &&
        !buttonRef.current?.contains(target)
      )
        setAnchor(null);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      setAnchor(null);
      buttonRef.current?.focus();
    };
    const closeOnResize = () => setAnchor(null);
    document.addEventListener("pointerdown", closeOutside);
    document.addEventListener("keydown", closeOnEscape);
    window.addEventListener("resize", closeOnResize);
    return () => {
      document.removeEventListener("pointerdown", closeOutside);
      document.removeEventListener("keydown", closeOnEscape);
      window.removeEventListener("resize", closeOnResize);
    };
  }, [anchor]);

  const apply = (action: () => void, remaining: number) => {
    // 剩下一项时入口会卸载，把键盘焦点交回工具栏，避免落到页面起点。
    const focusTarget =
      remaining > 1 ? buttonRef.current : fallbackFocusRef.current;
    setAnchor(null);
    action();
    focusTarget?.focus();
  };

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={anchor !== null}
        onClick={() => {
          const rect = buttonRef.current!.getBoundingClientRect();
          setAnchor(anchor ? null : { x: rect.left, y: rect.bottom + 6 });
        }}
        className="inline-flex shrink-0 items-center gap-1 rounded-md bg-primary/10 px-2 py-1 text-xs text-primary transition-colors hover:bg-primary/15 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {t("library.combinedFilters", "组合筛选 · {{count}}", {
          count: filters.length,
        })}
        <ChevronDownIcon
          className={`h-3 w-3 transition-transform ${anchor ? "rotate-180" : ""}`}
          aria-hidden="true"
        />
      </button>
      {anchor
        ? createPortal(
            <div
              ref={panelRef}
              role="dialog"
              aria-label={t("library.activeFilters", "当前筛选")}
              onBlur={(event) => {
                if (
                  !event.currentTarget.contains(event.relatedTarget as Node) &&
                  !buttonRef.current?.contains(event.relatedTarget as Node)
                )
                  setAnchor(null);
              }}
              className="fixed z-[9999] w-72 max-w-[calc(100vw-16px)] rounded-xl border border-border bg-popover p-3 text-popover-foreground shadow-xl"
              style={{ left: position.x, top: position.y }}
            >
              <div className="mb-2 flex items-center justify-between gap-3">
                <span className="text-xs font-medium">
                  {t("library.activeFilters", "当前筛选")}
                </span>
                <button
                  type="button"
                  onClick={() => apply(onClear, 0)}
                  className="rounded px-1 py-0.5 text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
                >
                  {t("library.clearFilters", "清空筛选")}
                </button>
              </div>
              <div className="divide-y divide-border/60">
                {filters.map((filter) => (
                  <div
                    key={filter.kind}
                    className="flex items-start gap-3 py-2.5"
                  >
                    <span className="w-12 shrink-0 pt-1 text-xs text-muted-foreground">
                      {filter.kind}
                    </span>
                    <span className="min-w-0 flex-1 break-words pt-0.5 text-sm">
                      {filter.name}
                    </span>
                    <button
                      type="button"
                      onClick={() => apply(filter.remove, filters.length - 1)}
                      aria-label={t(
                        "library.removeFilter",
                        "取消{{kind}}筛选：{{name}}",
                        { kind: filter.kind, name: filter.name },
                      )}
                      className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    >
                      <XIcon className="h-3.5 w-3.5" aria-hidden="true" />
                    </button>
                  </div>
                ))}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
