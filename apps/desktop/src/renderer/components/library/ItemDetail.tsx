import { LoadErrorState } from "../ui/LoadErrorState";
import { DraftConflictNotice } from "./DraftConflictNotice";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { ItemDetailHeader } from "./ItemDetailHeader";
import { ContentPanel } from "./ContentPanel";
import { ArticleAskReader } from "../ask/ArticleAskReader";
import { SourceCaptureRevisions } from "./SourceCaptureRevisions";
import { ArticleToolsPanel } from "./ArticleToolsPanel";
import { SourceCommentsProvider } from "./SourceCommentsContext";

/**
 * 条目详情：头部（标题 / 元信息 / 标签）+ 正文面板。
 * 音视频的文字稿与总结入口都在正文面板的标签页里，不再挤占正文上方。
 * 编辑内容经 knowledge.store 防抖自动保存；Ctrl+S 立即保存。
 * onClose 仅由详情浮层（列表视图）传入，头部会多出一个关闭按钮。
 * 专注阅读模式下整栏限宽居中，避免超宽视线跳行。
 */
export function ItemDetail({ onClose }: { onClose?: () => void }) {
  const { t } = useTranslation();
  const detailLoading = useKnowledgeStore(s => s.detailLoading);
  const detailError = useKnowledgeStore(s => s.detailError);
  const selectedId = useKnowledgeStore(s => s.selectedId);
  const item = useKnowledgeStore((state) => state.selectedItem);
  const flushPendingSave = useKnowledgeStore((state) => state.flushPendingSave);
  const [toolsItemId, setToolsItemId] = useState<string | null>(null);
  const [askItemId, setAskItemId] = useState<string | null>(null);
  const isFocusReadingMode = useUIStore((state) => state.isFocusReadingMode);

  useEffect(() => {
    if (isFocusReadingMode) setToolsItemId(null);
  }, [isFocusReadingMode]);

  useEffect(() => {
    const openArticleAsk = (event: Event) => {
      if ((event as CustomEvent).detail?.itemId !== item?.id || item?.deletedAt != null) return;
      setToolsItemId(null); setAskItemId(item.id);
    };
    window.addEventListener("article-ask-open", openArticleAsk);
    return () => window.removeEventListener("article-ask-open", openArticleAsk);
  }, [item?.id, item?.deletedAt]);

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

  if (detailError) return <LoadErrorState message={detailError} onRetry={() => void useKnowledgeStore.getState().selectItem(selectedId)} />;
  if (detailLoading) return <div role="status" className="p-6 text-sm text-muted-foreground">正在加载条目…</div>;
  if (!item || item.id !== selectedId) {
    return (
      <div className="flex h-full items-center justify-center px-8 text-center text-sm text-muted-foreground">
        {t("library.noSelection", "在左侧选择一个条目，或新建一个开始记录")}
      </div>
    );
  }

  const isTrashed = item.deletedAt != null;
  const toolsOpen = toolsItemId === item.id;

  return (
    <div
      data-testid="article-detail"
      className={`relative flex h-full min-h-0 flex-col ${
        isFocusReadingMode && askItemId !== item.id ? "mx-auto w-full max-w-4xl" : ""
      }`}
    >
      <SourceCommentsProvider
        key={`${item.id}:${item.sourceUri}:${isTrashed}`}
        item={item}
      >
        <DraftConflictNotice />
      <SourceCaptureRevisions itemId={item.id} />
        <ItemDetailHeader
          item={item}
          isTrashed={isTrashed}
          onClose={onClose}
          toolsOpen={toolsOpen}
          onToggleTools={() => { setAskItemId(null); setToolsItemId(toolsOpen ? null : item.id); }}
          askOpen={askItemId === item.id}
          onToggleAsk={() => { setToolsItemId(null); setAskItemId(askItemId === item.id ? null : item.id); }}
        />

        {!isTrashed ? (
          <ArticleToolsPanel
            isOpen={toolsOpen}
            key={item.id}
            item={item}
            onClose={() => setToolsItemId(null)}
          />
        ) : null}

        <ArticleAskReader key={`ask:${item.id}`} itemId={item.id} open={askItemId === item.id && !isTrashed}
          onOpen={() => { if (!isTrashed) { setToolsItemId(null); setAskItemId(item.id); } }} onClose={() => setAskItemId(null)}>
          <ContentPanel item={item} isTrashed={isTrashed} />
        </ArticleAskReader>
      </SourceCommentsProvider>
    </div>
  );
}
