import { onConversationEvidenceCleared, sanitizeCachedMessages, sanitizeCachedTarget } from "./conversation-evidence";
import { queueConversationSave } from "./conversation-persistence";
import { create } from "zustand";
import type { AskSessionMeta } from "@guizhi/shared/types";
import type { ArticleContext, ArticleMessage, ArticleTarget } from "@guizhi/shared/types/article-ask";
import { askArticle } from "../services/knowledge-ai/article-qa";
import { runGuardedMutation } from "./operation-error.store";

interface ArticleAskState {
  sessionId: string | null;
  itemId: string | null;
  title: string;
  target: ArticleTarget | null;
  sessions: AskSessionMeta[];
  messages: ArticleMessage[];
  webEnabled: boolean;
  loading: boolean;
  running: boolean;
  loadError: string | null;
  saveError: string | null;
  open: (target: ArticleTarget, sessionId?: string) => Promise<void>;
  newSession: (target: ArticleTarget) => Promise<void>;
  setWeb: (enabled: boolean) => void;
  ask: (question: string, target: ArticleTarget, context?: ArticleContext) => Promise<boolean>;
  retry: (id: string) => Promise<boolean>;
  stop: () => void;
  persist: () => Promise<boolean>;
}
let controller: AbortController | null = null;
let loadingGeneration = 0;
const makeId = () => crypto.randomUUID();
const errorText = (e: unknown) => e instanceof Error ? e.message : String(e);

export function parseArticleMessages(json: string): ArticleMessage[] {
  const messages: unknown = JSON.parse(json);
  if (!Array.isArray(messages)) throw new Error("问答记录格式损坏");
  return sanitizeCachedMessages(messages.map(m => {
    if (!m || typeof m.id !== "string" || typeof m.question !== "string" || typeof m.answer !== "string") throw new Error("问答消息格式损坏");
    return { ...m, sources: Array.isArray(m.sources) ? m.sources : [], warnings: Array.isArray(m.warnings) ? m.warnings : [],
      ...(m.status === "running" ? { status: "error", error: "会话在完成前被中断", step: undefined } : {}) };
  }));
}

export const useArticleAskStore = create<ArticleAskState>((set, get) => ({
  sessionId: null, itemId: null, title: "", target: null, sessions: [], messages: [], webEnabled: true,
  loading: false, running: false, loadError: null, saveError: null,
  persist: async () => {
    const snapshot = get();
    if (!snapshot.sessionId || !snapshot.target || !snapshot.messages.length) return true;
    const input = { id: snapshot.sessionId, scope: "article" as const, itemId: snapshot.itemId!, articleTitle: snapshot.title,
      title: snapshot.messages[0].question.slice(0, 30), messagesJson: JSON.stringify(snapshot.messages),
      target: snapshot.target, webEnabled: snapshot.webEnabled };
    const job = queueConversationSave(input.id, async () => {
      const ok = await runGuardedMutation("articleAsk.save", "保存本文问答", async () => {
        const saved = await window.api.askSession.save(input);
        if (get().sessionId === input.id) set({ saveError: null, sessions: [saved, ...get().sessions.filter(s => s.id !== saved.id)] });
        window.dispatchEvent(new Event("article-ask-saved"));
      });
      if (!ok && get().sessionId === input.id) set({ saveError: "问答尚未保存，请重试保存" });
      return ok;
    });
    return job;
  },
  open: async (target, sessionId) => {
    if (get().itemId === target.itemId && get().sessionId && (!sessionId || sessionId === get().sessionId) && !get().loadError) return;
    const generation = ++loadingGeneration;
    get().stop();
    if (!(await get().persist()) || generation !== loadingGeneration) return;
    set({ loading: true, loadError: null });
    try {
      const sessions = await window.api.askSession.list({ scope: "article", itemId: target.itemId });
      const id = sessionId ?? sessions[0]?.id;
      const record = id ? await window.api.askSession.get(id) : null;
      if (id && (!record || record.scope !== "article" || record.itemId !== target.itemId)) throw new Error("本文会话不存在或关联不一致");
      const item = await window.api.knowledge.get(target.itemId);
      if (generation !== loadingGeneration) return;
      set({ sessionId: record?.id ?? makeId(), itemId: target.itemId, target: sanitizeCachedTarget(record?.target ?? target),
        title: item?.title ?? record?.articleTitle ?? "文章已删除", sessions,
        messages: record ? parseArticleMessages(record.messagesJson) : [], webEnabled: record?.webEnabled !== false,
        loading: false, running: false, saveError: null });
    } catch (e) {
      if (generation === loadingGeneration) set({ loading: false, loadError: errorText(e) });
      void window.api.log.appError({ scope: "articleAsk", action: "加载会话", message: errorText(e) });
    }
  },
  newSession: async target => {
    get().stop();
    if (!(await get().persist())) return;
    ++loadingGeneration;
    set({ sessionId: makeId(), itemId: target.itemId, target, messages: [], webEnabled: true, loadError: null, loading: false, saveError: null });
  },
  setWeb: enabled => { set({ webEnabled: enabled }); void get().persist(); },
  stop: () => {
    const active = controller;
    if (!active) return;
    controller = null; active.abort();
    set(s => ({ running: false, messages: s.messages.map(m => m.status === "running" ? { ...m, status: "error", error: "已停止", step: undefined } : m) }));
    void get().persist();
  },
  ask: async (question, target, context) => {
    if (get().running || get().loading || get().loadError || !question.trim() || get().itemId !== target.itemId) return false;
    const sessionId = get().sessionId, id = makeId(), local = new AbortController();
    controller = local;
    const history = [...get().messages];
    const message: ArticleMessage = { id, question: question.trim(), answer: "", sources: [], warnings: [], status: "running", webEnabled: get().webEnabled, webStatus: get().webEnabled ? "searching" : "off" };
    set({ target: sanitizeCachedTarget(target), messages: [...history, message], running: true });
    let lastCheckpoint = 0;
    const patch = (value: Partial<ArticleMessage>) => {
      if (controller !== local || get().sessionId !== sessionId || local.signal.aborted) return;
      set(s => ({ messages: s.messages.map(m => m.id === id ? { ...m, ...sanitizeCachedMessages([value])[0] } : m) }));
      if (value.context || (value.answer !== undefined && Date.now() - lastCheckpoint >= 500)) { lastCheckpoint = Date.now(); void get().persist(); }
    };
    if (!(await get().persist())) { get().stop(); return false; }
    try {
      await askArticle({ question: question.trim(), target, history, context, webEnabled: message.webEnabled, signal: local.signal, requestId: id, patch });
      return true;
    } catch (e) {
      patch({ status: "error", error: local.signal.aborted ? "已停止" : errorText(e), step: undefined });
      if (!local.signal.aborted) void window.api.log.appError({ scope: "articleAsk", action: "回答", message: errorText(e) });
      return false;
    } finally {
      if (controller === local && get().sessionId === sessionId) { controller = null; set({ running: false }); await get().persist(); }
    }
  },
  retry: async id => {
    if (get().running) return false;
    const index = get().messages.findIndex(m => m.id === id), message = get().messages[index];
    if (!message || !get().target) return false;
    set(s => ({ messages: s.messages.slice(0, index), webEnabled: message.webEnabled }));
    return get().ask(message.question, message.context?.target ?? get().target!, message.context);
  },
}));

onConversationEvidenceCleared(() => useArticleAskStore.setState(state => ({
  messages: sanitizeCachedMessages(state.messages), target: sanitizeCachedTarget(state.target),
})));
