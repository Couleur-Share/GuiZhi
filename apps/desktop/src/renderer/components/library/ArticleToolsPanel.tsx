import { useEffect, useRef } from "react";
import { XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { AiHandoffButton } from "./AiHandoffButton";
import { AiOcrCard } from "./AiOcrCard";
import { AiSummaryCard } from "./AiSummaryCard";
import { IllustrationCard } from "./IllustrationCard";
import { MediaPreview } from "./MediaPreview";
import { SourceCommentsCard } from "./SourceCommentsCard";
import { TagEditor } from "./TagEditor";
import { WebSourceVersions } from "./WebSourceVersions";
import { formatItemTime, getItemTypeMeta } from "./type-meta";

/** 工具区按需展开；关闭时保留媒体和操作状态，子弹层自行管理焦点。 */
export function ArticleToolsPanel({
  item,
  isOpen,
  onClose,
}: {
  item: KnowledgeItem;
  isOpen: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!isOpen) return;
    const previous = document.activeElement;
    closeRef.current?.focus({ preventScroll: true });
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus({ preventScroll: true });
    };
  }, [isOpen]);
  const updateSelected = useKnowledgeStore((state) => state.updateSelected);
  const type = getItemTypeMeta(item.itemType);
  const media = item.itemType === "audio" || item.itemType === "video";
  return (
    <aside
      id="article-tools-panel"
      role="region"
      aria-label={t("articleReader.tools", "文章信息与工具")}
      hidden={!isOpen}
      className={`${isOpen ? "flex" : "hidden"} absolute bottom-4 right-4 top-4 z-30 w-[26rem] max-w-[calc(100%_-_2rem)] flex-col overflow-hidden rounded-xl border border-border bg-background shadow-xl`}
      onKeyDown={(event) => {
        // 子弹层通过 portal 呈现，不让工具区拦截其 Esc 或键盘焦点。
        if (
          event.key === "Escape" &&
          event.currentTarget.contains(event.target as Node)
        ) {
          event.stopPropagation();
          onClose();
        }
      }}
    >
      <div className="flex shrink-0 items-start gap-3 border-b border-border/60 p-4">
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">
            {t("articleReader.tools", "文章信息与工具")}
          </h2>
          <p className="mt-1 truncate text-xs text-muted-foreground">
            {item.title}
          </p>
        </div>
        <button
          ref={closeRef}
          type="button"
          onClick={onClose}
          aria-label={t("articleReader.closeTools", "收起文章工具")}
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <XIcon className="h-4 w-4" aria-hidden="true" />
        </button>
      </div>
      <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-4">
        <section aria-label={t("articleReader.info", "文章信息")}>
          <dl className="grid grid-cols-3 gap-4 text-sm">
            <div>
              <dt className="mb-1 text-xs text-muted-foreground">
                {t("articleReader.type", "内容类型")}
              </dt>
              <dd>{t(type.labelKey, type.fallback)}</dd>
            </div>
            <div>
              <dt className="mb-1 text-xs text-muted-foreground">
                {t("articleReader.length", "正文长度")}
              </dt>
              <dd>
                {t("library.wordCount", "{{count}} 字", {
                  count: item.content.trim().length,
                })}
              </dd>
            </div>
            <div>
              <dt className="mb-1 text-xs text-muted-foreground">
                {t("articleReader.updated", "最近更新")}
              </dt>
              <dd>{formatItemTime(item.updatedAt)}</dd>
            </div>
          </dl>
          <div className="mt-5 border-t border-border/60 pt-4">
            <h3 className="text-sm font-medium">
              {t("articleReader.tags", "标签")}
            </h3>
            <TagEditor
              item={item}
              onChange={(tagNames) => updateSelected({ tagNames })}
            />
          </div>
        </section>
        <div className="empty:hidden">
          <MediaPreview item={item} />
        </div>
        <section
          className="space-y-3 border-t border-border/60 pt-4"
          aria-label={t("articleReader.processing", "内容处理")}
        >
          <div className="flex items-center justify-between gap-3">
            <h3 className="text-sm font-medium">
              {t("articleReader.processing", "内容处理")}
            </h3>
            <AiHandoffButton item={item} />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {item.itemType === "image" ? <AiOcrCard item={item} /> : null}
            {!media ? <AiSummaryCard item={item} /> : null}
            <IllustrationCard item={item} />
            <SourceCommentsCard />
          </div>
        </section>
        {item.itemType === "webpage" ? (
          <section className="border-t border-border/60 pt-4">
            <h3 className="mb-3 text-sm font-medium">
              {t("articleReader.sourceVersions", "采集的来源版本")}
            </h3>
            <WebSourceVersions item={item} />
          </section>
        ) : null}
      </div>
    </aside>
  );
}
