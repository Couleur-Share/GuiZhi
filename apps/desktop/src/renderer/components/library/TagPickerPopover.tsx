import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { PlusIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { Tag } from "@guizhi/shared/types";
import { useTagStore } from "../../stores/tag.store";
import { AiTagSuggest } from "./AiTagSuggest";
import { TAG_COLOR_CLASSES, TAG_DOT_CLASSES } from "./type-meta";

const PANEL_WIDTH = 272;
const VIEWPORT_MARGIN = 8;

/**
 * 标签编辑浮层：当前标签、新建输入、选择已有标签、AI 建议四段收在一处。
 * 详情页的 + 按钮和列表右键的「编辑标签」共用同一个面板。
 */
export function TagPickerPopover({
  itemId,
  tags,
  anchor,
  ignoreRef,
  onChange,
  onClose,
}: {
  itemId: string;
  tags: Tag[];
  /** 期望的左上角位置（会按视口夹回来） */
  anchor: { x: number; y: number };
  /** 触发按钮：点它时不走「点外部关闭」，交给按钮自己 toggle */
  ignoreRef?: React.RefObject<HTMLElement | null>;
  onChange: (tagNames: string[]) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const allTags = useTagStore((state) => state.tags);
  const panelRef = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState("");
  const [position, setPosition] = useState(anchor);

  const names = tags.map((tag) => tag.name);
  const unusedTags = allTags.filter((tag) => !names.includes(tag.name));

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
      const target = event.target as Node;
      if (
        panelRef.current?.contains(target) ||
        ignoreRef?.current?.contains(target)
      ) {
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
  }, [onClose, ignoreRef]);

  const addTag = (name: string) => {
    const trimmed = name.trim();
    if (
      !trimmed ||
      names.some((existing) => existing.toLowerCase() === trimmed.toLowerCase())
    ) {
      return;
    }
    onChange([...names, trimmed]);
  };

  const commitDraft = () => {
    addTag(draft);
    setDraft("");
  };

  return createPortal(
    <div
      ref={panelRef}
      role="dialog"
      aria-label={t("library.editTags", "编辑标签")}
      className="fixed z-[9999] rounded-xl border border-border bg-popover p-3 shadow-xl"
      style={{ left: position.x, top: position.y, width: PANEL_WIDTH }}
    >
      {tags.length > 0 ? (
        <div className="mb-2.5 flex flex-wrap gap-1">
          {tags.map((tag) => (
            <span
              key={tag.id}
              className={`inline-flex h-6 items-center gap-1 rounded-full px-2.5 text-xs ${TAG_COLOR_CLASSES[tag.colorKey]}`}
            >
              {tag.name}
              <button
                type="button"
                onClick={() =>
                  onChange(names.filter((name) => name !== tag.name))
                }
                aria-label={t("library.removeTag", "移除标签 {{name}}", {
                  name: tag.name,
                })}
                className="rounded-full opacity-60 transition-opacity hover:opacity-100"
              >
                <XIcon className="h-3 w-3" aria-hidden="true" />
              </button>
            </span>
          ))}
        </div>
      ) : null}

      <div className="flex items-center gap-1.5">
        <input
          autoFocus
          type="text"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              commitDraft();
            }
          }}
          placeholder={t("library.tagsNewPlaceholder", "输入新标签后回车")}
          className="h-7 min-w-0 flex-1 rounded-md border border-border bg-background px-2 text-xs text-foreground placeholder:text-muted-foreground/60 focus:border-primary/60 focus:outline-none focus-visible:ring-0"
        />
        <button
          type="button"
          onClick={commitDraft}
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
        {unusedTags.length === 0 ? (
          <p className="mt-1.5 text-xs text-muted-foreground/60">
            {t("library.tagsExistingEmpty", "没有其他已有标签")}
          </p>
        ) : (
          <div className="mt-1.5 flex max-h-28 flex-wrap gap-1 overflow-y-auto">
            {unusedTags.map((tag) => (
              <button
                key={tag.id}
                type="button"
                onClick={() => addTag(tag.name)}
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

      <div className="mt-3 border-t border-border/60 pt-2.5">
        <AiTagSuggest
          itemId={itemId}
          currentNames={names}
          onApply={onChange}
        />
      </div>
    </div>,
    document.body,
  );
}
