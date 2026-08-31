import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { PlusIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useTagStore } from "../../stores/tag.store";
import { TAG_DOT_CLASSES } from "./type-meta";

const PANEL_WIDTH = 260;
const VIEWPORT_MARGIN = 8;

/**
 * 批量打标签浮层。
 *
 * 与单条的 TagPickerPopover 分开：这里是「给选中的 N 条各加一个标签」，
 * 各条原有的标签不一样，不能用整体替换的语义。
 */
export function BulkTagPopover({
  count,
  anchor,
  onAdd,
  onClose,
}: {
  count: number;
  anchor: { x: number; y: number };
  onAdd: (tagName: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const allTags = useTagStore((state) => state.tags);
  const panelRef = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState("");
  const [position, setPosition] = useState(anchor);

  useLayoutEffect(() => {
    const rect = panelRef.current?.getBoundingClientRect();
    if (!rect) {
      return;
    }
    setPosition({
      x: Math.min(
        Math.max(VIEWPORT_MARGIN, anchor.x),
        window.innerWidth - rect.width - VIEWPORT_MARGIN,
      ),
      y: Math.min(
        Math.max(VIEWPORT_MARGIN, anchor.y),
        window.innerHeight - rect.height - VIEWPORT_MARGIN,
      ),
    });
  }, [anchor.x, anchor.y]);

  useEffect(() => {
    const handlePointerDown = (event: MouseEvent) => {
      if (panelRef.current?.contains(event.target as Node)) {
        return;
      }
      onClose();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose]);

  const commit = (name: string) => {
    const trimmed = name.trim();
    if (!trimmed) {
      return;
    }
    onAdd(trimmed);
    onClose();
  };

  return createPortal(
    <div
      ref={panelRef}
      role="dialog"
      aria-label={t("library.bulkAddTag", "批量打标签")}
      className="fixed z-[9999] rounded-xl border border-border bg-popover p-3 shadow-xl"
      style={{ left: position.x, top: position.y, width: PANEL_WIDTH }}
    >
      <p className="mb-2 text-[11px] text-muted-foreground">
        {t("library.bulkAddTagHint", "为选中的 {{count}} 项添加标签", { count })}
      </p>

      <div className="flex items-center gap-1.5">
        <input
          autoFocus
          type="text"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              commit(draft);
            }
          }}
          placeholder={t("library.tagsNewPlaceholder", "输入新标签后回车")}
          className="h-7 min-w-0 flex-1 rounded-md border border-border bg-background px-2 text-xs text-foreground placeholder:text-muted-foreground/60 focus:border-primary/60 focus:outline-none"
        />
        <button
          type="button"
          onClick={() => commit(draft)}
          disabled={draft.trim().length === 0}
          className="inline-flex h-7 shrink-0 items-center gap-1 rounded-md bg-primary px-2 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-40"
        >
          <PlusIcon className="h-3 w-3" aria-hidden="true" />
          {t("common.add", "添加")}
        </button>
      </div>

      <div className="mt-3 border-t border-border/60 pt-2.5">
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t("library.tagsExisting", "选择已有标签")}
        </span>
        {allTags.length === 0 ? (
          <p className="mt-1.5 text-xs text-muted-foreground/60">
            {t("library.tagsExistingEmpty", "没有其他已有标签")}
          </p>
        ) : (
          <div className="mt-1.5 flex max-h-28 flex-wrap gap-1 overflow-y-auto">
            {allTags.map((tag) => (
              <button
                key={tag.id}
                type="button"
                onClick={() => commit(tag.name)}
                className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:text-foreground"
              >
                <span
                  aria-hidden="true"
                  className={`h-1.5 w-1.5 shrink-0 rounded-full ${TAG_DOT_CLASSES[tag.colorKey]}`}
                />
                {tag.name}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}
