import { useEffect, useState } from "react";
import { WebSourceVersions } from "./WebSourceVersions";
import { useTranslation } from "react-i18next";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { ItemDetailHeader } from "./ItemDetailHeader";
import { ContentPanel } from "./ContentPanel";
import { AiSummaryCard } from "./AiSummaryCard";
import { AiOcrCard } from "./AiOcrCard";
import { IllustrationCard } from "./IllustrationCard";
import { MediaPreview } from "./MediaPreview";
import { SourceCommentsProvider } from "./SourceCommentsContext";
import { SourceCommentsCard } from "./SourceCommentsCard";

/**
 * 条目详情：头部（标题 / 元信息 / 标签）+ 正文面板。
 * 音视频的文字稿与总结入口都在正文面板的标签页里，不再挤占正文上方。
 * 编辑内容经 knowledge.store 防抖自动保存；Ctrl+S 立即保存。
 * onClose 仅由详情浮层（列表视图）传入，头部会多出一个关闭按钮。
 * 专注阅读模式下整栏限宽居中，避免超宽视线跳行。
 */
export function ItemDetail({ onClose }: { onClose?: () => void }) {
  const { t } = useTranslation();
  const item = useKnowledgeStore((state) => state.selectedItem);
  const flushPendingSave = useKnowledgeStore((state) => state.flushPendingSave);
  const [toolsItemId, setToolsItemId] = useState<string | null>(null);
  const isFocusReadingMode = useUIStore((state) => state.isFocusReadingMode);

  useEffect(()=>{if(isFocusReadingMode)setToolsItemId(null);},[isFocusReadingMode]);

  // Ctrl+S 立即保存
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        void flushPendingSave();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [flushPendingSave]);

  if (!item) {
    return (
      <div className="flex h-full items-center justify-center px-8 text-center text-sm text-muted-foreground">
        {t("library.noSelection", "在左侧选择一个条目，或新建一个开始记录")}
      </div>
    );
  }

  const isTrashed = item.deletedAt != null;
  const compactReading = item.itemType === "webpage";
  const toolsOpen = toolsItemId === item.id;
  const isMediaItem = item.itemType === "audio" || item.itemType === "video";

  return (
    <div
      className={`flex h-full min-h-0 flex-col ${
        isFocusReadingMode ? "mx-auto w-full max-w-4xl" : ""
      }`}
    >
      <SourceCommentsProvider
        key={`${item.id}:${item.sourceUri}:${isTrashed}`}
        item={item}
      >
        <ItemDetailHeader item={item} isTrashed={isTrashed} onClose={onClose} compactReading={compactReading} toolsOpen={toolsOpen} onToggleTools={()=>setToolsItemId(toolsOpen ? null : item.id)} />

        {!isTrashed && (!compactReading || toolsOpen) ? (
          // 卡片全部不适用时（如在线视频条目）整块折叠，避免留一条空白带
          <div className="max-h-[35vh] shrink-0 overflow-auto space-y-2.5 border-b border-border/60 px-6 py-3 empty:hidden">
            <MediaPreview item={item} />
            {/* 轻动作并排成一行；摘要生成后卡片自己占满整行 */}
            <div className="flex flex-wrap items-center gap-2 empty:hidden">
              {item.itemType === "image" ? <AiOcrCard item={item} /> : null}
              {item.itemType === "webpage" ? <WebSourceVersions item={item} /> : null}
              {/* 音视频条目由正文面板里的「总结」按钮承担总结职能 */}
              {!isMediaItem ? <AiSummaryCard item={item} /> : null}
              <IllustrationCard item={item} />
              <SourceCommentsCard />
            </div>
          </div>
        ) : null}

        <ContentPanel item={item} isTrashed={isTrashed} />
      </SourceCommentsProvider>
    </div>
  );
}
