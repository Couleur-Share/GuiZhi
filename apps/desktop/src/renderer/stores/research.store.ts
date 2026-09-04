import { create } from "zustand";
import type {
  CreateResearchRunInput,
  ResearchRun,
  ResearchRunDetail,
} from "@guizhi/shared/types";
import {
  generateResearchReport,
  RESEARCH_REPORT_PROMPT_VERSION,
} from "../services/knowledge-ai/research-report";

interface ResearchState {
  runs: ResearchRun[];
  selectedRunId: string | null;
  detail: ResearchRunDetail | null;
  loading: boolean;
  busy: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  select: (id: string | null) => Promise<void>;
  create: (input: CreateResearchRunInput) => Promise<ResearchRun>;
  cancel: () => Promise<void>;
  clone: () => Promise<ResearchRun | null>;
  remove: () => Promise<void>;
  generateReport: () => Promise<void>;
  cancelReport: () => void;
  enqueueCandidates: (ids: string[]) => Promise<void>;
  saveToKnowledge: () => Promise<{ itemId: string; updated: boolean }>;
  subscribeChanges: () => () => void;
}

let reportController: AbortController | null = null;

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export const useResearchStore = create<ResearchState>((set, get) => ({
  runs: [],
  selectedRunId: null,
  detail: null,
  loading: false,
  busy: false,
  error: null,
  refresh: async () => {
    set({ loading: true, error: null });
    try {
      const runs = await window.api.research.list();
      set({ runs, loading: false });
    } catch (error) {
      set({ loading: false, error: message(error) });
    }
  },
  select: async (id) => {
    set({ selectedRunId: id, detail: null, loading: Boolean(id), error: null });
    if (!id) return;
    try {
      set({ detail: await window.api.research.get(id), loading: false });
    } catch (error) {
      set({ loading: false, error: message(error) });
    }
  },
  create: async (input) => {
    set({ busy: true, error: null });
    try {
      const run = await window.api.research.create(input);
      set((state) => ({ runs: [run, ...state.runs], selectedRunId: run.id, busy: false }));
      await get().select(run.id);
      return run;
    } catch (error) {
      set({ busy: false, error: message(error) });
      throw error;
    }
  },
  cancel: async () => {
    const id = get().selectedRunId;
    if (!id) return;
    await window.api.research.cancel(id);
  },
  clone: async () => {
    const id = get().selectedRunId;
    if (!id) return null;
    const run = await window.api.research.clone(id);
    await get().refresh();
    await get().select(run.id);
    return run;
  },
  remove: async () => {
    const id = get().selectedRunId;
    if (!id) return;
    await window.api.research.delete(id);
    set({ selectedRunId: null, detail: null });
    await get().refresh();
  },
  generateReport: async () => {
    const id = get().selectedRunId;
    if (!id) return;
    set({ busy: true, error: null });
    reportController?.abort();
    reportController = new AbortController();
    try {
      const packet = await window.api.research.beginReport(id);
      const markdown = await generateResearchReport(packet, reportController.signal);
      const detail = await window.api.research.saveReport(
        id,
        markdown,
        RESEARCH_REPORT_PROMPT_VERSION,
      );
      set({ detail, busy: false });
      await get().refresh();
    } catch (error) {
      await window.api.research.failReport(id, message(error)).catch(() => undefined);
      set({ busy: false, error: message(error) });
      throw error;
    } finally {
      reportController = null;
    }
  },
  cancelReport: () => reportController?.abort(),
  enqueueCandidates: async (ids) => {
    const runId = get().selectedRunId;
    if (!runId || ids.length === 0) return;
    set({ busy: true, error: null });
    try {
      await window.api.research.enqueueCandidates(runId, ids);
      set({ detail: await window.api.research.get(runId), busy: false });
    } catch (error) {
      set({ busy: false, error: message(error) });
      throw error;
    }
  },
  saveToKnowledge: async () => {
    const id = get().selectedRunId;
    if (!id) throw new Error("没有选中的研究");
    const result = await window.api.research.saveToKnowledge(id);
    set({ detail: await window.api.research.get(id) });
    await get().refresh();
    return result;
  },
  subscribeChanges: () => {
    const handler = (detail: ResearchRunDetail) => {
      set((state) => ({
        detail: state.selectedRunId === detail.run.id ? detail : state.detail,
        runs: [detail.run, ...state.runs.filter((run) => run.id !== detail.run.id)]
          .sort((a, b) => b.updatedAt - a.updatedAt),
      }));
    };
    window.api.on("research:changed", handler);
    return () => window.api.off("research:changed", handler);
  },
}));
