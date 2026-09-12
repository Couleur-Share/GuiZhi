import { ArticleHistoryList } from "./ArticleHistoryList";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ArticleSource, ArticleTarget } from "@guizhi/shared/types/article-ask";
import { useArticleAskStore } from "../../stores/article-ask.store";
import { useUIStore } from "../../stores/ui.store";
import { isAiConfiguredForScenario } from "../../services/knowledge-ai/ai-invoke";
import { LoadErrorState } from "../ui/LoadErrorState";
import { useToast } from "../ui/Toast";
import { ChatComposer } from "./ChatComposer";
import { ArticleMessageCard } from "./ArticleMessageCard";

const drafts = new Map<string, { text: string; quote: ArticleTarget | null }>();

export function ArticleConversation({ target, sessionId, prompt, promptKey, resolveTarget, onLocate, onClose, onSubmitted, onSessionChange }: {
  target: ArticleTarget; sessionId?: string; prompt?: string; promptKey?: number;
  resolveTarget?: () => Promise<ArticleTarget>; onLocate?: (source: ArticleSource) => Promise<boolean>; onClose?: () => void; onSubmitted?: () => void; onSessionChange?: (id: string) => void;
}) {
  const { t } = useTranslation(), { showToast } = useToast();
  const state = useArticleAskStore();
  const draftKey = `${target.itemId}:${sessionId ?? "reader"}`;
  const [draft, updateDraft] = useState(drafts.get(draftKey)?.text ?? "");
  const setDraft = useCallback((text: string) => { drafts.set(draftKey, { text, quote: drafts.get(draftKey)?.quote ?? null }); updateDraft(text); }, [draftKey]);
  const [quote, updateQuote] = useState<ArticleTarget | null>(drafts.get(draftKey)?.quote ?? null);
  const setQuote = useCallback((value: ArticleTarget | null) => { drafts.set(draftKey, { text: drafts.get(draftKey)?.text ?? "", quote: value }); updateQuote(value); }, [draftKey]);
  const [history, setHistory] = useState(false), [missingSearch, setMissingSearch] = useState(false), [submitting, setSubmitting] = useState(false);
  const scroll = useRef<HTMLDivElement>(null), follow = useRef(true);
  useEffect(() => { void useArticleAskStore.getState().open(target, sessionId); }, [target, sessionId]);
  const consumedPrompt = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (state.loading || state.itemId !== target.itemId || consumedPrompt.current === promptKey) return;
    consumedPrompt.current = promptKey;
    if (target.selection) { setDraft(prompt ?? ""); setQuote(target); }
  }, [promptKey, prompt, target, state.loading, state.sessionId, state.itemId, draftKey, setDraft, setQuote]);
  const previousView = useRef(`${target.view}:${target.versionId}`);
  useEffect(() => { const key = `${target.view}:${target.versionId}`; if (previousView.current !== key) { previousView.current = key; setQuote(null); } }, [target.view, target.versionId, setQuote]);

  useEffect(() => { if (follow.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight; }, [state.messages]);
  const settings = () => useUIStore.getState().requestSettingsSection("ai");
  const send = async (offline = false) => {
    if (!draft.trim() || submitting) return;
    setSubmitting(true);
    try {
      if (!isAiConfiguredForScenario("qa")) { showToast(t("ask.notConfigured", "尚未配置 AI 服务"), "error"); settings(); return; }
      if (state.webEnabled && !offline) {
        const result = await window.api.themedReading.searchConfig();
        if (!result.success) throw new Error(result.error || "搜索配置读取失败");
        if (!result.search?.configured) { setMissingSearch(true); return; }
      }
      if (offline) state.setWeb(false);
      const current = resolveTarget ? await resolveTarget() : target;
      const questionTarget = quote ? { ...current, selection: quote.selection, floor: quote.floor } : { ...current, selection: undefined };
      if (quote && (quote.view !== current.view || quote.versionId !== current.versionId)) throw new Error("阅读来源已切换，请重新选择段落");
      const question = draft;
      setDraft(""); setQuote(null); setMissingSearch(false); follow.current = true; onSubmitted?.();
      const ok = await state.ask(question, questionTarget);
      if (!ok && !useArticleAskStore.getState().messages.some(m => m.question === question)) setDraft(question);
    } catch (e) { showToast(t("articleAsk.sendFailed", "提问失败"), "error", { detail: e instanceof Error ? e.message : String(e) }); }
    finally { setSubmitting(false); }
  };
  const close = async () => { state.stop(); if (await state.persist()) onClose?.(); };
  return <section className="flex h-full min-h-0 flex-col bg-background" aria-label={t("articleAsk.title", "围绕本文提问")} data-testid="article-ask-panel"
    onKeyDown={e => { if (e.key === "Escape" && e.currentTarget.contains(e.target as Node)) { e.stopPropagation(); void close(); } }}>
    <header className="flex shrink-0 items-center gap-2 border-b border-border p-3">
      <div className="min-w-0 flex-1"><h2 className="text-sm font-semibold">{t("articleAsk.title", "围绕本文提问")}</h2><p className="truncate text-xs text-muted-foreground">{state.title}</p></div>
      <button type="button" disabled={state.loading} onClick={() => { setDraft(""); setQuote(null); void state.newSession(target).then(() => { const id = useArticleAskStore.getState().sessionId; if (id) onSessionChange?.(id); }); }} className="text-xs">{t("articleAsk.new", "新对话")}</button>
      <button type="button" onClick={() => setHistory(!history)} className="text-xs" aria-expanded={history}>{t("articleAsk.history", "历史")}</button>
      {onClose ? <button type="button" onClick={() => void close()} aria-label={t("articleAsk.close", "收起本文问答")} className="rounded px-2 py-1">×</button> : null}
    </header>
    {history ? <ArticleHistoryList itemId={target.itemId} onOpen={async id => {
      await state.open(target, id);
      const current = useArticleAskStore.getState();
      if (current.saveError || current.loadError || current.sessionId !== id) return;
      setDraft(''); setQuote(null); onSessionChange?.(id); setHistory(false);
    }} /> : null}
    {state.saveError ? <div role="alert" className="p-3 text-xs text-destructive">{state.saveError} <button type="button" onClick={() => void state.persist()} className="underline">{t("articleAsk.retrySave", "重试保存")}</button></div> : null}
    <div ref={scroll} onScroll={() => { const node = scroll.current; if (node) follow.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80; }} className="min-h-0 flex-1 space-y-5 overflow-auto p-4">
      {state.loading ? <p role="status">{t("common.loading", "加载中…")}</p> : state.loadError ? <LoadErrorState message={state.loadError} onRetry={() => void state.open(target, sessionId)} /> : state.messages.length ? state.messages.map(message => <ArticleMessageCard key={message.id} message={message} running={state.running} onRetry={() => void state.retry(message.id)} onLocate={onLocate} />) : <p className="text-sm leading-7 text-muted-foreground">{t("articleAsk.empty", "选中不懂的段落，或直接提出问题。回答会结合本文，并展示补充资料的来源。")}</p>}
    </div>
    <footer className="shrink-0 space-y-2 border-t border-border p-3">
      {quote?.selection ? <div className="rounded border border-border p-2 text-xs"><p className="mb-1 text-muted-foreground">{t(`articleAsk.view.${quote.view}`, quote.view)}{quote.floor ? ` · ${quote.floor}` : ""}</p><blockquote className="max-h-24 overflow-auto whitespace-pre-wrap">{quote.selection}</blockquote><button type="button" className="mt-1 text-primary" onClick={() => setQuote(null)}>{t("articleAsk.removeQuote", "移除选段")}</button></div> : null}
      <button type="button" role="switch" aria-checked={state.webEnabled} disabled={state.running} onClick={() => state.setWeb(!state.webEnabled)} className="text-xs text-muted-foreground">{state.webEnabled ? "☑ " : "☐ "}{t("articleAsk.web", "联网查证")}</button>
      {missingSearch ? <div role="alert" className="space-y-2 rounded border border-border p-2 text-xs"><p>{t("articleAsk.noSearch", "尚未配置联网搜索，问题已保留。")}</p><button type="button" className="text-primary" onClick={settings}>{t("articleAsk.configure", "前往配置")}</button><button type="button" className="ml-3 text-primary" onClick={() => void send(true)}>{t("articleAsk.sendOffline", "关闭联网后发送")}</button></div> : null}
      <ChatComposer value={draft} onChange={setDraft} running={state.running} disabled={submitting || state.loading || Boolean(state.loadError)} onSend={() => void send()} onStop={state.stop} />
    </footer>
  </section>;
}
