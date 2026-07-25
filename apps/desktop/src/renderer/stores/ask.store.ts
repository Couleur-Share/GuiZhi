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
}

interface AskState {
  sessions: AskSessionMeta[];
  activeSessionId: string | null;
  messages: AskMessage[];
  isRunning: boolean;
  /** 加载会话列表并恢复上次活跃会话（AskWorkspace / 侧栏挂载时调用） */
  initialize: () => Promise<void>;
  newSession: () => void;
  switchSession: (id: string) => Promise<void>;
  deleteSession: (id: string) => Promise<void>;
  ask: (question: string) => Promise<void>;
  stop: () => void;
}

const ACTIVE_SESSION_STORAGE_KEY = "guizhi-ask-active-session";
const SESSION_TITLE_MAX_LENGTH = 30;

let abortController: AbortController | null = null;
let initialized = false;

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
    return [];
  }
  if (!Array.isArray(parsed)) {
    return [];
  }
  const messages: AskMessage[] = [];
  for (const candidate of parsed) {
    if (!candidate || typeof candidate !== "object") {
      continue;
    }
    const message = candidate as AskMessage;
    if (typeof message.question !== "string") {
      continue;
    }
    messages.push({
      id: typeof message.id === "string" ? message.id : createMessageId(),
      question: message.question,
      answer: typeof message.answer === "string" ? message.answer : "",
      sources: Array.isArray(message.sources) ? message.sources : [],
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
    });
  }
  return messages;
}

function buildSessionTitle(messages: AskMessage[]): string {
  const firstQuestion = messages[0]?.question?.trim() ?? "";
  return firstQuestion.slice(0, SESSION_TITLE_MAX_LENGTH) || "新对话";
}

export const useAskStore = create<AskState>()((set, get) => {
  /** 有消息才落盘；空会话不进数据库 */
  const persistActiveSession = async (): Promise<void> => {
    const { activeSessionId, messages } = get();
    if (!activeSessionId || messages.length === 0 || !window.api?.askSession) {
      return;
    }
    try {
      const saved = await window.api.askSession.save({
        id: activeSessionId,
        title: buildSessionTitle(messages),
        messagesJson: JSON.stringify(messages),
      });
      set((state) => {
        const meta: AskSessionMeta = {
          id: saved.id,
          title: saved.title,
          createdAt: saved.createdAt,
          updatedAt: saved.updatedAt,
        };
        const rest = state.sessions.filter((s) => s.id !== saved.id);
        return { sessions: [meta, ...rest] };
      });
    } catch (error) {
      console.error("保存问答会话失败:", error);
    }
  };

  return {
    sessions: [],
    activeSessionId: null,
    messages: [],
    isRunning: false,

    initialize: async () => {
      if (initialized || !window.api?.askSession) {
        return;
      }
      initialized = true;
      try {
        const sessions = await window.api.askSession.list();
        let activeSessionId: string | null = null;
        let messages: AskMessage[] = [];

        const remembered = localStorage.getItem(ACTIVE_SESSION_STORAGE_KEY);
        if (remembered && sessions.some((s) => s.id === remembered)) {
          const record = await window.api.askSession.get(remembered);
          if (record) {
            activeSessionId = record.id;
            messages = parseStoredMessages(record.messagesJson);
          }
        }
        if (!activeSessionId) {
          activeSessionId = createSessionId();
          rememberActiveSession(activeSessionId);
        }
        set({ sessions, activeSessionId, messages });
      } catch (error) {
        initialized = false;
        console.error("加载问答会话失败:", error);
      }
    },

    newSession: () => {
      if (get().isRunning) {
        abortController?.abort();
      }
      // 当前会话为空时直接复用，避免制造一堆空会话
      if (get().messages.length === 0 && get().activeSessionId) {
        return;
      }
      const activeSessionId = createSessionId();
      rememberActiveSession(activeSessionId);
      set({ activeSessionId, messages: [], isRunning: false });
    },

    switchSession: async (id) => {
      if (id === get().activeSessionId) {
        return;
      }
      if (get().isRunning) {
        abortController?.abort();
      }
      await persistActiveSession();
      try {
        const record = await window.api.askSession.get(id);
        if (!record) {
          return;
        }
        rememberActiveSession(record.id);
        set({
          activeSessionId: record.id,
          messages: parseStoredMessages(record.messagesJson),
          isRunning: false,
        });
      } catch (error) {
        console.error("切换问答会话失败:", error);
      }
    },

    deleteSession: async (id) => {
      try {
        await window.api.askSession.delete(id);
      } catch (error) {
        console.error("删除问答会话失败:", error);
        return;
      }
      set((state) => ({
        sessions: state.sessions.filter((s) => s.id !== id),
      }));
      if (get().activeSessionId === id) {
        const activeSessionId = createSessionId();
        rememberActiveSession(activeSessionId);
        set({ activeSessionId, messages: [], isRunning: false });
      }
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

      const patchMessage = (patch: Partial<AskMessage>) => {
        set((state) => ({
          messages: state.messages.map((candidate) =>
            candidate.id === id ? { ...candidate, ...patch } : candidate,
          ),
        }));
      };

      abortController = new AbortController();
      try {
        const answer = await askKnowledgeBase(
          trimmed,
          history,
          createQaDeps(),
          (step) => {
            set((state) => ({
              messages: state.messages.map((candidate) =>
                candidate.id === id
                  ? { ...candidate, steps: [...candidate.steps, step] }
                  : candidate,
              ),
            }));
          },
          abortController.signal,
        );
        patchMessage({
          status: "done",
          answer: answer.text,
          sources: answer.sources,
          model: answer.model,
          usedFallback: answer.usedFallback,
        });
      } catch (error) {
        if (error instanceof DOMException && error.name === "AbortError") {
          patchMessage({ status: "error", error: "已停止", errorKind: "generic" });
        } else if (error instanceof AiNotConfiguredError) {
          patchMessage({ status: "error", errorKind: "not-configured" });
        } else if (error instanceof QaNoSourceError) {
          patchMessage({ status: "error", errorKind: "no-source" });
        } else {
          patchMessage({
            status: "error",
            error: error instanceof Error ? error.message : String(error),
            errorKind: "generic",
          });
        }
      } finally {
        abortController = null;
        set({ isRunning: false });
        void persistActiveSession();
      }
    },

    stop: () => {
      abortController?.abort();
    },
  };
});
