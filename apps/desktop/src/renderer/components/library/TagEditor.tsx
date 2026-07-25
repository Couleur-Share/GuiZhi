import { useRef, useState } from "react";
import { PlusIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { TagPickerPopover } from "./TagPickerPopover";
import { TAG_COLOR_CLASSES } from "./type-meta";

/**
 * 详情页标签行：只展示条目已有的标签，末尾留一个 + 入口。
 * 新建标签、挑已有标签、AI 建议都收在 + 弹出的浮层里，不占用这一行。
 */
export function TagEditor({
  item,
  onChange,
}: {
  item: KnowledgeItem;
  onChange: (tagNames: string[]) => void;
}) {
  const { t } = useTranslation();
  const addButtonRef = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);

  const tags = item.tags;
  const addTagLabel = t("library.addTag", "添加标签");

  const removeTag = (tagId: string) => {
    onChange(tags.filter((tag) => tag.id !== tagId).map((tag) => tag.name));
  };

  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5">
      {tags.map((tag) => (
        <span
          key={tag.id}
          className={`inline-flex h-6 items-center gap-1 rounded-full px-2.5 text-xs ${TAG_COLOR_CLASSES[tag.colorKey]}`}
        >
          {tag.name}
          <button
            type="button"
            onClick={() => removeTag(tag.id)}
            className="rounded-full opacity-60 transition-opacity hover:opacity-100"
            aria-label={t("library.removeTag", "移除标签 {{name}}", {
              name: tag.name,
            })}
          >
            <XIcon className="h-3 w-3" aria-hidden="true" />
          </button>
        </span>
      ))}

      <button
        ref={addButtonRef}
        type="button"
        onClick={(event) => {
          if (anchor) {
            setAnchor(null);
            return;
          }
          const rect = event.currentTarget.getBoundingClientRect();
          setAnchor({ x: rect.left, y: rect.bottom + 6 });
        }}
        title={addTagLabel}
        aria-label={addTagLabel}
        aria-haspopup="dialog"
        aria-expanded={anchor !== null}
        className="inline-flex h-6 items-center gap-1 rounded-full border border-dashed border-border px-2 text-xs text-muted-foreground transition-colors hover:border-muted-foreground/60 hover:text-foreground"
      >
        <PlusIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
        {tags.length === 0 ? addTagLabel : null}
      </button>

      {anchor ? (
        <TagPickerPopover
          itemId={item.id}
          tags={tags}
          anchor={anchor}
          ignoreRef={addButtonRef}
          onChange={onChange}
          onClose={() => setAnchor(null)}
        />
      ) : null}
    </div>
  );
}
