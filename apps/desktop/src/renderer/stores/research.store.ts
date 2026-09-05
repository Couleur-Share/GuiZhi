import { create } from "zustand";
import type {
  CreateResearchRunInput,
  ResearchRun,
  ResearchRunDetail,
} from "@guizhi/shared/types";

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
  clone: (replan?: boolean) => Promise<ResearchRun | null>;
  remove: () => Promise<void>;
  generateReport: () => Promise<void>;
  cancelReport: () => void;
  enqueueCandidates: (ids: string[]) => Promise<void>;
  saveToKnowledge: () => Promise<{ itemId: string; updated: boolean }>;
  subscribeChanges: (onCompleted?: (detail: ResearchRunDetail) => void) => () => void;
}

let eventRevision = 0;

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
    const revision = eventRevision;
    set({ loading: true, error: null });
    try {
      const runs = await window.api.research.list();
      set((state) => ({
        runs: revision === eventRevision ? runs : [...new Map([...runs, ...state.runs].map((run) => [run.id, run])).values()].sort((a, b) => b.updatedAt - a.updatedAt),
        loading: false,
      }));
    } catch (error) {
      set({ loading: false, error: message(error) });
    }
  },
  select: async (id) => {
    set({ selectedRunId: id, detail: null, loading: Boolean(id), error: null });
    if (!id) return;
    try {
      const detail = await window.api.research.get(id);
      if (get().selectedRunId !== id) return;
      set((state) => ({ detail: state.detail ?? detail, loading: false, error: detail ? null : "研究记录不存在" }));
    } catch (error) {
      if (get().selectedRunId !== id) return;
      set({ loading: false, error: message(error) });
    }
  },
  create: async (input) => {
    set({ busy: true, error: null });
    try {
      const run = await window.api.research.create(input);
      set((state) => ({ runs: [state.runs.find((item) => item.id === run.id) ?? run, ...state.runs.filter((item) => item.id !== run.id)], selectedRunId: run.id, busy: false }));
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
  clone: async (replan = false) => {
    const id = get().selectedRunId;
    if (!id) return null;
    const run = await window.api.research.clone(id, replan);
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
    try {
      await window.api.research.generateReport(id);
      if (get().selectedRunId === id) await get().select(id);
      set({ busy: false });
    } catch (error) { set({ busy: false, error: message(error) }); throw error; }
  },
  cancelReport: () => {
    const id = get().selectedRunId;
    if (id) void window.api.research.cancelReport(id).catch((error) => set({ error: message(error) }));
  },
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
  subscribeChanges: (onCompleted) => {
    const handler = (detail: ResearchRunDetail) => {
      eventRevision += 1;
      const previous = get().runs.find((run) => run.id === detail.run.id);
      set((state) => ({
        detail: state.selectedRunId === detail.run.id ? detail : state.detail,
        runs: [detail.run, ...state.runs.filter((run) => run.id !== detail.run.id)]
          .sort((a, b) => b.updatedAt - a.updatedAt),
      }));
      if (previous?.status === "collecting" && detail.run.status !== "collecting") onCompleted?.(detail);
    };
    window.api.on("research:changed", handler);
    return () => window.api.off("research:changed", handler);
  },
}));
