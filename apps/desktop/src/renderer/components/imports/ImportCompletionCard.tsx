import { FileTextIcon, ImageIcon, MessageCircleIcon, MicIcon, TagsIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { extractAllLocalAssetRefs } from "@guizhi/shared/utils/media-refs";

const ACTION_BASE =
  "inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-xs transition-colors";

function Stat({ icon, children }: { icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
      {icon}
      {children}
    </span>
  );
}

/**
 * 任务完成不该只剩一枚绿色状态：用户需要立即知道哪些内容已经落到本地，
 * 并能从同一个位置继续阅读或发起问答。这里故意只读条目事实，不重新猜测
 * 连接器产物；无论网页、视频还是本地文件都共用同一张结果卡。
 */
export function ImportCompletionCard({
  item,
  onOpen,
  onAsk,
}: {
  item: KnowledgeItem;
  onOpen: () => void;
  onAsk: () => void;
}) {
  const { t } = useTranslation();
  const assetCount = extractAllLocalAssetRefs(item.content).length;
  const bodyLength = item.content.trim().length;
  const transcriptLength = item.transcript?.trim().length ?? 0;

  return (
    <section className="rounded-xl border border-primary/20 bg-primary/[0.045] px-4 py-3">
      <div className="text-xs font-medium text-foreground">
        {t("imports.completionTitle", "内容已沉淀为知识条目")}
      </div>
      <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
        {t(
          "imports.completionHint",
          "已保存在本地，可继续阅读、检索、编辑和关联。",
        )}
      </p>
      <div className="mt-2 truncate text-xs font-medium text-foreground">
        {item.title || t("library.untitled", "无标题")}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1.5">
        {bodyLength > 0 ? (
          <Stat icon={<FileTextIcon className="h-3.5 w-3.5" aria-hidden="true" />}>
            {t("imports.completionBody", "正文 {{count}} 字", { count: bodyLength })}
          </Stat>
        ) : null}
        {transcriptLength > 0 ? (
          <Stat icon={<MicIcon className="h-3.5 w-3.5" aria-hidden="true" />}>
            {t("imports.completionTranscript", "文字稿 {{count}} 字", {
              count: transcriptLength,
            })}
          </Stat>
        ) : null}
        {assetCount > 0 ? (
          <Stat icon={<ImageIcon className="h-3.5 w-3.5" aria-hidden="true" />}>
            {t("imports.completionAssets", "媒体 {{count}} 项", { count: assetCount })}
          </Stat>
        ) : null}
        {item.tags.length > 0 ? (
          <Stat icon={<TagsIcon className="h-3.5 w-3.5" aria-hidden="true" />}>
            {t("imports.completionTags", "标签 {{count}} 个", { count: item.tags.length })}
          </Stat>
        ) : null}
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        <button
          type="button"
          onClick={onOpen}
          className={`${ACTION_BASE} bg-primary text-primary-foreground hover:bg-primary/90`}
        >
          <FileTextIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("imports.openItem", "打开条目")}
        </button>
        <button
          type="button"
          onClick={onAsk}
          className={`${ACTION_BASE} border border-border bg-background/60 text-foreground hover:bg-accent`}
        >
          <MessageCircleIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("imports.askAboutItem", "就此提问")}
        </button>
      </div>
    </section>
  );
}
