import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { AskSessionMeta } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useAskStore } from "../../stores/ask.store";
import { useArticleAskStore } from "../../stores/article-ask.store";
import { useToast } from "../ui/Toast";
import { locateArticleSource } from "./article-reader-target";
import { ArticleConversation } from "./ArticleConversation";

export function GlobalArticleConversation({ session }: { session: AskSessionMeta }) {
  const { t } = useTranslation(), { showToast } = useToast();
  useEffect(() => () => useArticleAskStore.getState().stop(), [session.id]);
  const back = async () => {
    try {
      const item = await window.api.knowledge.get(session.itemId!);
      if (!item || item.deletedAt != null) throw new Error("文章不存在或已删除，请先恢复文章");
      useUIStore.getState().setAppModule("library");
      await useKnowledgeStore.getState().selectItem(item.id);
    } catch (e) { showToast(t("articleAsk.openFailed", "打开文章失败"), "error", { detail: String(e) }); }
  };
  return <div className="flex min-h-0 flex-1 flex-col">
    <div className="shrink-0 border-b border-border p-3"><button type="button" onClick={() => void back()} className="text-sm text-primary">{t("articleAsk.back", "返回文章")}</button></div>
    <div className="min-h-0 flex-1"><ArticleConversation target={session.target!} sessionId={session.id} onSessionChange={id => { const state = useArticleAskStore.getState(); useAskStore.getState().adoptArticleSession({ ...session, id, target: state.target ?? session.target, webEnabled: state.webEnabled }); }} onLocate={async source => {
      await back();
      await new Promise(resolve => setTimeout(resolve, 150));
      const root = document.querySelector<HTMLElement>("[data-testid=article-ask-reader]");
      return root ? locateArticleSource(root, source) : false;
    }} /></div>
  </div>;
}
