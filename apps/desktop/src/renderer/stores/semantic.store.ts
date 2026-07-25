import { create } from "zustand";
import type { SemanticIndexStatus } from "@guizhi/shared/types";
import {
  isSemanticConfigured,
  resolveEmbeddingConfig,
} from "../services/knowledge-ai/embeddings";
import { runSemanticIndexing } from "../services/knowledge-ai/semantic-index";

interface SemanticState {
  status: SemanticIndexStatus | null;
  /** embedding 路由模型已配置（语义检索可用的前提） */
  isConfigured: boolean;
  isIndexing: boolean;
  /** 本轮已完成的条目数（索引进行中展示） */
  indexedThisRun: number;
  refreshStatus: () => Promise<void>;
  runIndexing: () => Promise<void>;
  /** 防抖调度一轮后台索引（导入完成等场景调用） */
  scheduleIndexing: (delayMs?: number) => void;
}

const SCHEDULE_DEFAULT_DELAY_MS = 15_000;

let scheduleTimer: ReturnType<typeof setTimeout> | null = null;

export const useSemanticStore = create<SemanticState>()((set, get) => ({
  status: null,
  isConfigured: false,
  isIndexing: false,
  indexedThisRun: 0,

  refreshStatus: async () => {
    const config = resolveEmbeddingConfig();
    if (!config || !window.api?.semantic) {
      set({ isConfigured: false, status: null });
      return;
    }
    try {
      const status = await window.api.semantic.status(config.model);
      set({ isConfigured: true, status });
    } catch (error) {
      console.error("加载语义索引状态失败:", error);
    }
  },

  runIndexing: async () => {
    if (get().isIndexing || !window.api?.semantic) {
      return;
    }
    set({ isIndexing: true, indexedThisRun: 0 });
    try {
      const result = await runSemanticIndexing((indexed) =>
        set({ indexedThisRun: indexed }),
      );
      if (!result.skipped && result.failed > 0) {
        console.warn(
          `[semantic] 本轮索引完成：成功 ${result.indexed}，失败 ${result.failed}`,
        );
      }
    } catch (error) {
      console.error("语义索引执行失败:", error);
    } finally {
      set({ isIndexing: false });
      await get().refreshStatus();
    }
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
      void get().runIndexing();
    }, delayMs);
  },
}));
