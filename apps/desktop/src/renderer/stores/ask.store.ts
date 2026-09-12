import { onConversationEvidenceCleared, sanitizeCachedMessages, sanitizeCachedTarget } from "./conversation-evidence";
import { runGuardedMutation } from "./operation-error.store";
import { queueConversationSave } from "./conversation-persistence";
import { useArticleAskStore } from "./article-ask.store";
import { create } from "zustand";
import type { AskSessionMeta } from "@guizhi/shared/types";
import {
  askKnowledgeBase,
  createQaDeps,
  QaNoSourceError,
  type QaSourceRef,
  type QaTurn,
} from "../services/knowledge-ai/qa";
import { AiNotConfiguredError } from "../services/knowledge-ai/ai-invoke";

export type AskErrorKind = "not-configured" | "no-source" | "generic";

export interface AskMessage {
  evidenceSources?: QaSourceRef[];
  warnings?: string[];
  id: string;
  question: string;
  answer: string;
  sources: QaSourceRef[];
  /** Agent 执行步骤（思考过程展示） */
  steps: string[];
  status: "running" | "done" | "error";
  error?: string;
  errorKind?: AskErrorKind;
  model?: string;
  usedFallback?: boolean;
  /** 回答被 max_tokens 截断（界面标注「可能不完整」） */
  truncated?: boolean;
}

interface AskState {
  loadError: string | null;
  retryLoad: () => Promise<void>;
  saveError: string | null;
  persist: () => Promise<boolean>;
  articleSession: AskSessionMeta | null;
  adoptArticleSession: (session: AskSessionMeta) => void;
  refreshSessions: () => Promise<void>;
  sessions: AskSessionMeta[];
  activeSessionId: string | null;
  messages: AskMessage[];
  isRunning: boolean;
  /**
   * 首次加载是否已结束（无论成败）。
   *
   * 界面不能拿 `messages.length === 0` 直接判空态：读会话是异步的，
   * 有历史会话的用户会先看到一整屏空态引导，再被消息列表整块换掉。
   */
  hasLoaded: boolean;
  /** 加载会话列表并恢复上次活跃会话（AskWorkspace / 侧栏挂载时调用） */
  initialize: () => Promise<void>;
  newSession: () => Promise<void>;
  switchSession: (id: string) => Promise<void>;
  deleteSession: (id: string) => Promise<void>;
  ask: (question: string) => Promise<void>;
  /** 重新回答某一轮：丢弃它及其之后的消息，用同一个问题重跑 */
  retry: (messageId: string) => Promise<void>;
  /** 删除单轮问答（问错了、答歪了，不必清空整个会话） */
  removeMessage: (messageId: string) => void;
  stop: () => void;
}

const ACTIVE_SESSION_STORAGE_KEY = "guizhi-ask-active-session";
const SESSION_TITLE_MAX_LENGTH = 30;

let abortController: AbortController | null = null;
let initialized = false;
let sessionGeneration = 0;
let failedSessionId: string | null = null;

function createMessageId(): string {
  return `ask-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function createSessionId(): string {
  return `session-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function rememberActiveSession(id: string | null): void {
  try {
    if (id) {
      localStorage.setItem(ACTIVE_SESSION_STORAGE_KEY, id);
    } else {
      localStorage.removeItem(ACTIVE_SESSION_STORAGE_KEY);
    }
  } catch {
    // localStorage 不可用时静默降级为不记忆
  }
}

/** 反序列化历史消息：上次未完成的 running 消息标记为已中断 */
function parseStoredMessages(messagesJson: string): AskMessage[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(messagesJson);
  } catch {
    throw new Error("问答记录格式损坏，请恢复备份或重试读取");
  }
  if (!Array.isArray(parsed)) {
    throw new Error("问答记录格式损坏");
  }
  const messages: AskMessage[] = [];
  for (const candidate of parsed) {
    if (!candidate || typeof candidate !== "object") {
      throw new Error("问答消息格式损坏");
    }
    const message = candidate as AskMessage;
    if (typeof message.question !== "string") {
      throw new Error("问答消息格式损坏");
    }
    messages.push({
      id: typeof message.id === "string" ? message.id : createMessageId(),
      question: message.question,
      answer: typeof message.answer === "string" ? message.answer : "",
      sources: Array.isArray(message.sources) ? message.sources : [],
      evidenceSources: Array.isArray(message.evidenceSources) ? message.evidenceSources : undefined,
      steps: Array.isArray(message.steps) ? message.steps : [],
      status: message.status === "done" ? "done" : "error",
      ...(message.status === "running"
        ? { error: "会话在完成前被中断", errorKind: "generic" as const }
        : {
            error: message.error,
            errorKind: message.errorKind,
          }),
      model: message.model,
      usedFallback: message.usedFallback,
      truncated: message.truncated, warnings: message.warnings,
    });
  }
  return sanitizeCachedMessages(messages);
}

function buildSessionTitle(messages: AskMessage[]): string {
  const firstQuestion = messages[0]?.question?.trim() ?? "";
  return firstQuestion.slice(0, SESSION_TITLE_MAX_LENGTH) || "新对话";
}

export const useAskStore = create<AskState>()((set, get) => {
  /** 有消息才落盘；空会话不进数据库 */
  const persistActiveSession = async (): Promise<boolean> => {
    if (get().articleSession) return useArticleAskStore.getState().persist();
    const { activeSessionId, messages } = get();
    if (!activeSessionId || messages.length === 0 || !window.api?.askSession) {
      return true;
    }
    try {
      const input = {
        id: activeSessionId,
        title: buildSessionTitle(messages),
        messagesJson: JSON.stringify(messages),
      };
      const saved = await queueConversationSave(activeSessionId, () => window.api.askSession.save(input));
      set((state) => {
        const meta: AskSessionMeta = {
          id: saved.id,
          title: saved.title,
          createdAt: saved.createdAt,
          updatedAt: saved.updatedAt,
        };
        const rest = state.sessions.filter((s) => s.id !== saved.id);
        return { sessions: [meta, ...rest], saveError: null };
      });
      window.dispatchEvent(new Event("article-ask-saved"));
      return true;
    } catch (error) {
      set({ saveError: error instanceof Error ? error.message : String(error) });
      console.error("保存问答会话失败:", error);
      return false;
    }
  };

  return {
    loadError: null, saveError: null, persist: persistActiveSession,
    retryLoad: async () => { if (failedSessionId) await get().switchSession(failedSessionId); else { initialized = false; await get().initialize(); } },
    articleSession: null,
    adoptArticleSession: session => { rememberActiveSession(session.id); set({ activeSessionId: session.id, articleSession: session, messages: [], isRunning: false }); },
    refreshSessions: async () => { try { set({ sessions: await window.api.askSession.list() }); } catch (e) { void window.api.log.appError({ scope: "ask", action: "刷新历史", message: String(e) }); } },
    sessions: [],
    activeSessionId: null,
    messages: [],
    isRunning: false,
    hasLoaded: false,

    initialize: async () => {
      if (initialized) {
        return;
      }
      if (!window.api?.askSession) {
        // 没有持久化能力（web 运行时），没有什么可等的，直接放行空态
        set({ hasLoaded: true });
        return;
      }
      initialized = true;
      set({ loadError: null });
      try {
        const sessions = window.api.askSession.query ? (await window.api.askSession.query({ limit: 50 })).entries : await window.api.askSession.list();
        let activeSessionId: string | null = null;
        let messages: AskMessage[] = [];
        let articleSession: AskSessionMeta | null = null;

        const remembered = localStorage.getItem(ACTIVE_SESSION_STORAGE_KEY);
        if (remembered) {
          const record = await window.api.askSession.get(remembered);
          if (record) {
            activeSessionId = record.id;
            if (record.scope === "article") articleSession = record;
            else messages = parseStoredMessages(record.messagesJson);
          }
        }
        if (!activeSessionId) {
          activeSessionId = createSessionId();
          rememberActiveSession(activeSessionId);
        }
        set({ sessions, activeSessionId, messages, articleSession });
      } catch (error) {
        initialized = false;
        set({ loadError: error instanceof Error ? error.message : String(error) });
        console.error("加载问答会话失败:", error);
      } finally {
        // 失败也要放行：否则界面会一直停在加载态，用户连空态引导都看不到
        set({ hasLoaded: true });
      }
    },

    newSession: async () => {
      const generation = ++sessionGeneration;
      get().stop(); useArticleAskStore.getState().stop();
      if (!(await persistActiveSession()) || generation !== sessionGeneration) return;
      if (get().articleSession) { useArticleAskStore.getState().stop(); set({ articleSession: null, activeSessionId: null }); }
      if (get().isRunning) {
        abortController?.abort();
      }
      // 当前会话为空时直接复用，避免制造一堆空会话
      if (get().messages.length === 0 && get().activeSessionId) {
        return;
      }
      const activeSessionId = createSessionId();
      rememberActiveSession(activeSessionId);
      set({ activeSessionId, messages: [], isRunning: false, loadError: null });
    },

    switchSession: async (id) => {
      if (id === get().activeSessionId) {
        return;
      }
      const generation = ++sessionGeneration;
      get().stop(); useArticleAskStore.getState().stop();
      if (!(await persistActiveSession()) || generation !== sessionGeneration) return;
      set({ loadError: null });
      try {
        const record = await window.api.askSession.get(id);
        if (generation !== sessionGeneration) return;
        if (!record) throw new Error("会话不存在或已被删除");
        // 先完整解析再切换，包括记忆的会话 ID；损坏记录不能变成可覆盖的空会话。
        const messages = record.scope === "article" ? [] : parseStoredMessages(record.messagesJson);
        failedSessionId = null;
        rememberActiveSession(record.id);
        set({
          activeSessionId: record.id,
          articleSession: record.scope === "article" ? record : null,
          messages,
          isRunning: false,
        });
      } catch (error) {
        if (generation !== sessionGeneration) return;
        failedSessionId = id;
        set({ loadError: error instanceof Error ? error.message : String(error) });
        console.error("切换问答会话失败:", error);
      }
    },

    deleteSession: async (id) => {
      const article = useArticleAskStore.getState();
      if (article.sessionId === id) { article.stop(); if (!(await article.persist())) return; }
      if (get().activeSessionId === id) { get().stop(); if (!(await persistActiveSession())) return; }
      const ok = await queueConversationSave(id, () => runGuardedMutation('ask.delete', '删除会话', async () => { await window.api.askSession.delete(id); }));
      if (!ok) return;
      if (useArticleAskStore.getState().sessionId === id) useArticleAskStore.setState({ sessionId: null, itemId: null, messages: [] });
      set(state => ({ sessions: state.sessions.filter(session => session.id !== id) }));
      if (get().activeSessionId === id) {
        const activeSessionId = createSessionId(); rememberActiveSession(activeSessionId);
        set({ activeSessionId, messages: [], isRunning: false, articleSession: null });
      }
      window.dispatchEvent(new Event('article-ask-saved'));
    },

    ask: async (question) => {
      const trimmed = question.trim();
      if (!trimmed || get().isRunning) {
        return;
      }

      // 多轮上下文：已完成的问答作为历史传入
      const history: QaTurn[] = get()
        .messages.filter((message) => message.status === "done")
        .map((message) => ({
          question: message.question,
          answer: message.answer,
        }));

      const id = createMessageId();
      const message: AskMessage = {
        id,
        question: trimmed,
        answer: "",
        sources: [],
        steps: [],
        status: "running",
      };
      set((state) => ({
        messages: [...state.messages, message],
        isRunning: true,
      }));

      const run = new AbortController(), sessionId = get().activeSessionId;
      abortController = run;
      const patchMessage = (patch: Partial<AskMessage>) => {
        if (abortController !== run || run.signal.aborted || get().activeSessionId !== sessionId) return;
        set((state) => ({
          messages: state.messages.map((candidate) =>
            candidate.id === id ? { ...candidate, ...sanitizeCachedMessages([patch])[0] } : candidate,
          ),
        }));
      };

      if (!(await persistActiveSession())) { get().stop(); return; }
      try {
        const answer = await askKnowledgeBase(
          trimmed,
          history,
          createQaDeps({ onEvidence: sources => {
            const previous = get().messages.find(m => m.id === id)?.evidenceSources ?? [];
            const all = [...new Map([...previous, ...sources].map(source => [`${source.kind}:${source.refId}:${source.evidence?.fingerprint}`, source])).values()];
            patchMessage({ evidenceSources: all, sources }); void persistActiveSession();
          }, onWarning: warning => patchMessage({ warnings: [...new Set([...(get().messages.find(m => m.id === id)?.warnings ?? []), warning])] }) }),
          (step) => {
            if (abortController !== run || run.signal.aborted || get().activeSessionId !== sessionId) return;
            set((state) => ({
              messages: state.messages.map((candidate) =>
                candidate.id === id
                  ? { ...candidate, steps: [...candidate.steps, step] }
                  : candidate,
              ),
            }));
          },
          run.signal,
          // 回调给的是到目前为止的全量文本，直接覆盖即可
          (text) => patchMessage({ answer: text }),
        );
        patchMessage({
          status: "done",
          answer: answer.text,
          sources: answer.sources,
          model: answer.model,
          usedFallback: answer.usedFallback,
          truncated: answer.truncated,
        });
      } catch (error) {
        if (error instanceof DOMException && error.name === "AbortError") {
          patchMessage({ status: "error", error: "已停止", errorKind: "generic" });
        } else if (error instanceof AiNotConfiguredError) {
          patchMessage({ status: "error", errorKind: "not-configured" });
        } else if (error instanceof QaNoSourceError) {
          patchMessage({ status: "error", errorKind: "no-source", answer: "" });
        } else {
          patchMessage({
            status: "error",
            error: error instanceof Error ? error.message : String(error),
            errorKind: "generic",
          });
        }
      } finally {
        if (abortController === run && get().activeSessionId === sessionId) {
          abortController = null;
          set({ isRunning: false });
          await persistActiveSession();
        }
      }
    },

    retry: async (messageId) => {
      if (get().isRunning) {
        return;
      }
      const index = get().messages.findIndex(
        (message) => message.id === messageId,
      );
      if (index < 0) {
        return;
      }
      const { question } = get().messages[index];
      // 连同其后的轮次一并丢弃：它们是基于这一轮的回答问出来的，
      // 留着会让多轮上下文对不上
      set((state) => ({ messages: state.messages.slice(0, index) }));
      await get().ask(question);
    },

    removeMessage: async (messageId) => {
      if (get().isRunning) return;
      const previous = get().messages, remaining = previous.filter(message => message.id !== messageId);
      if (!remaining.length) { const id = get().activeSessionId; if (id) await get().deleteSession(id); return; }
      set({ messages: remaining });
      if (!(await persistActiveSession()) && get().messages === remaining) set({ messages: previous });
    },

    stop: () => {
      const active = abortController;
      if (!active) return;
      abortController = null; active.abort();
      set(state => ({ isRunning: false, messages: state.messages.map(m => m.status === "running" ? { ...m, status: "error", error: "已停止", errorKind: "generic" } : m) }));
      void persistActiveSession();
    },
  };
});

onConversationEvidenceCleared(() => useAskStore.setState(state => ({
  messages: sanitizeCachedMessages(state.messages),
  articleSession: state.articleSession ? { ...state.articleSession, target: sanitizeCachedTarget(state.articleSession.target) } : null,
})));
