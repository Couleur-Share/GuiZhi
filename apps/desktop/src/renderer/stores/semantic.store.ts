import { create } from "zustand";
import type { SemanticIndexStatus } from "@guizhi/shared/types";
import {
  isSemanticConfigured,
  resolveEmbeddingConfig,
} from "../services/knowledge-ai/embeddings";
import { runSemanticIndexing } from "../services/knowledge-ai/semantic-index";

/**
 * 一轮索引的结果，供界面一次性提示。
 *
 * 此前这条链路从头到尾只往 console 写：嵌入接口报错、一条都没跑成，
 * 用户看到的都是「点了没反应」。手动触发的索引必须给出回执。
 */
export interface SemanticRunNotice {
  kind: "done" | "partial" | "failed" | "nothing" | "not-configured";
  indexed: number;
  failed: number;
  message?: string;
}

interface SemanticState {
  status: SemanticIndexStatus | null;
  /** embedding 路由模型已配置（语义检索可用的前提） */
  isConfigured: boolean;
  /**
   * 是否已经查过一次配置与索引状态。
   *
   * `isConfigured` 初始是 false，查完才可能翻 true。界面若直接照它渲染
   * 「未配置 embedding」的提示，配好的用户会先看到提示再看它消失。
   */
  hasChecked: boolean;
  isIndexing: boolean;
  /** 本轮已完成的条目数（索引进行中展示） */
  indexedThisRun: number;
  /** 手动触发的那一轮跑完后的回执；界面弹过就清掉 */
  notice: SemanticRunNotice | null;
  /** 取走回执并同时清空：读与清不分开，同一条回执只可能弹一次 */
  consumeNotice: () => SemanticRunNotice | null;
  refreshStatus: () => Promise<void>;
  /** @param silent 后台调度用：不产生界面回执 */
  runIndexing: (silent?: boolean) => Promise<SemanticRunNotice | null>;
  /** 防抖调度一轮后台索引（导入完成等场景调用） */
  scheduleIndexing: (delayMs?: number) => void;
}

const SCHEDULE_DEFAULT_DELAY_MS = 15_000;

let scheduleTimer: ReturnType<typeof setTimeout> | null = null;

export const useSemanticStore = create<SemanticState>()((set, get) => ({
  status: null,
  isConfigured: false,
  hasChecked: false,
  isIndexing: false,
  indexedThisRun: 0,
  notice: null,

  consumeNotice: () => {
    const notice = get().notice;
    if (notice) {
      set({ notice: null });
    }
    return notice;
  },

  refreshStatus: async () => {
    const config = resolveEmbeddingConfig();
    if (!config || !window.api?.semantic) {
      set({ isConfigured: false, status: null, hasChecked: true });
      return;
    }
    try {
      const status = await window.api.semantic.status(config.model);
      set({ isConfigured: true, status });
    } catch (error) {
      console.error("加载语义索引状态失败:", error);
    } finally {
      set({ hasChecked: true });
    }
  },

  runIndexing: async (silent = false) => {
    if (get().isIndexing || !window.api?.semantic) {
      return null;
    }
    set({ isIndexing: true, indexedThisRun: 0, notice: null });
    let runNotice: SemanticRunNotice | null = null;
    const report = (notice: SemanticRunNotice) => {
      runNotice = notice;
      if (!silent) {
        set({ notice });
      }
    };
    try {
      const result = await runSemanticIndexing((indexed) =>
        set({ indexedThisRun: indexed }),
      );
      if (result.skipped) {
        report({ kind: "not-configured", indexed: 0, failed: 0 });
      } else if (result.failed > 0 && result.indexed === 0) {
        report({
          kind: "failed",
          indexed: 0,
          failed: result.failed,
          message: result.lastError,
        });
      } else if (result.failed > 0) {
        report({
          kind: "partial",
          indexed: result.indexed,
          failed: result.failed,
          message: result.lastError,
        });
      } else {
        report({
          kind: result.indexed > 0 ? "done" : "nothing",
          indexed: result.indexed,
          failed: 0,
        });
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      console.error("语义索引执行失败:", error);
      report({ kind: "failed", indexed: 0, failed: 0, message });
    } finally {
      set({ isIndexing: false });
      await get().refreshStatus();
    }
    return runNotice;
  },

  scheduleIndexing: (delayMs = SCHEDULE_DEFAULT_DELAY_MS) => {
    if (!isSemanticConfigured()) {
      return;
    }
    if (scheduleTimer) {
      clearTimeout(scheduleTimer);
    }
    scheduleTimer = setTimeout(() => {
      scheduleTimer = null;
      // 后台轮次静默：失败等下一轮，不打扰当前视图
      void get().runIndexing(true);
    }, delayMs);
  },
}));
