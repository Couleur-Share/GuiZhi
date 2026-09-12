import { create } from "zustand";
import type {
  InboxItem,
  InboxItemKind,
  InboxAiClassificationApplyResult,
  InboxListResult,
  InboxOrganizeInput,
} from "@guizhi/shared/types";
import { describeLoadError } from "./load-error";

export type InboxFilter = InboxItemKind | "all";

const EMPTY_COUNTS: InboxListResult["counts"] = {
  "review-required": 0,
  unclassified: 0,
  "import-issue": 0,
  "discovery-candidate": 0,
  "semantic-pending": 0,
  "wiki-pending": 0,
};

let refreshGeneration = 0;
interface InboxState {
  wikiCandidates: { ready: number; review: number; waiting: number; upgrade: number } | null;
  sectionErrors: { wiki?: string; semantic?: string };
  items: InboxItem[];
  counts: InboxListResult["counts"];
  total: number;
  filter: InboxFilter;
  selectionIds: string[];
  isLoading: boolean;
  loadError: string | null;
  setFilter: (filter: InboxFilter) => void;
  toggleSelection: (itemId: string) => void;
  setSelection: (itemIds: string[]) => void;
  clearSelection: () => void;
  refresh: () => Promise<void>;
  organize: (input: Omit<InboxOrganizeInput, "itemIds">) => Promise<number>;
  markReviewed: (itemIds: string[]) => Promise<number>;
  acknowledgeImportWarning: (taskId: string) => Promise<number>;
  smartClassify: (
    itemIds: string[],
    existingCollectionNames: string[],
    options?: {
      signal?: AbortSignal;
      onProgress?: (completedBatches: number, totalBatches: number) => void;
    },
  ) => Promise<InboxAiClassificationApplyResult>;
}

export const useInboxStore = create<InboxState>()((set, get) => ({
  items: [],
  wikiCandidates: null,
  sectionErrors: {},
  counts: { ...EMPTY_COUNTS },
  total: 0,
  filter: "all",
  selectionIds: [],
  isLoading: false,
  loadError: null,
  setFilter: (filter) => set({ filter, selectionIds: [] }),
  toggleSelection: (itemId) =>
    set((state) => ({
      selectionIds: state.selectionIds.includes(itemId)
        ? state.selectionIds.filter((id) => id !== itemId)
        : [...state.selectionIds, itemId],
    })),
  setSelection: (itemIds) => set({ selectionIds: [...new Set(itemIds)] }),
  clearSelection: () => set({ selectionIds: [] }),
  refresh: async () => {
    const generation = ++refreshGeneration;
    set({ isLoading: true, loadError: null, sectionErrors: {} });
    const aggregate = (kind: "wiki-pending" | "semantic-pending", count: number) => {
      if (generation !== refreshGeneration) return;
      set(s => ({ items: [...s.items.filter(item => item.kind !== kind), ...(count ? [{ kind, id: `aggregate:${kind}`, count, createdAt: Date.now() }] : [])], counts: { ...s.counts, [kind]: count } }));
    };
    await Promise.allSettled([
      (async () => {
        try {
          const result = await window.api.inbox.list();
          if (generation !== refreshGeneration) return;
          const entries = result.items.filter(item => !["wiki-pending", "semantic-pending"].includes(item.kind));
          const alive = new Set(entries.flatMap(item => "itemId" in item ? [item.itemId] : []));
          set(s => ({ items: [...entries, ...s.items.filter(item => ["wiki-pending", "semantic-pending"].includes(item.kind))],
            counts: { ...result.counts, "wiki-pending": s.counts["wiki-pending"], "semantic-pending": s.counts["semantic-pending"] },
            total: result.total, selectionIds: s.selectionIds.filter(id => alive.has(id)) }));
        } catch (e) { if (generation === refreshGeneration) set({ loadError: describeLoadError(e) }); }
        finally { if (generation === refreshGeneration) set({ isLoading: false }); }
      })(),
      (async () => {
        try {
          if (window.api?.wiki?.compiler) {
            const { wikiPreview } = await import("../services/knowledge-ai/wiki-v2");
            const preview = await wikiPreview();
            if (generation === refreshGeneration) set({ wikiCandidates: preview.counts });
            aggregate("wiki-pending", preview.counts.ready); return;
          }
          const { countPendingWikiItems } = await import("../services/knowledge-ai/wiki-compile"); aggregate("wiki-pending", await countPendingWikiItems()); }
        catch (e) { aggregate("wiki-pending", 0); if (generation === refreshGeneration) set(s => ({ sectionErrors: { ...s.sectionErrors, wiki: describeLoadError(e) } })); }
      })(),
      (async () => {
        try {
          const { resolveEmbeddingConfig } = await import("../services/knowledge-ai/embeddings");
          const config = resolveEmbeddingConfig();
          const status = config ? await window.api.semantic.status(config.model) : null;
          aggregate("semantic-pending", status ? Math.max(0, status.eligibleItems - status.indexedItems) : 0);
        } catch (e) { aggregate("semantic-pending", 0); if (generation === refreshGeneration) set(s => ({ sectionErrors: { ...s.sectionErrors, semantic: describeLoadError(e) } })); }
      })(),
    ]);
  },
  organize: async (input) => {
    const itemIds = get().selectionIds;
    if (itemIds.length === 0) return 0;
    const changed = await window.api.inbox.organize({ ...input, itemIds });
    set({ selectionIds: [] });
    await get().refresh();
    return changed;
  },
  markReviewed: async (itemIds) => {
    const changed = await window.api.inbox.markReviewed(itemIds);
    await get().refresh();
    return changed;
  },
  acknowledgeImportWarning: async (taskId) => {
    const changed = await window.api.inbox.acknowledgeImportWarning(taskId);
    await get().refresh();
    return changed;
  },
  smartClassify: async (itemIds, existingCollectionNames, options) => {
    const sources = await window.api.inbox.aiClassificationSources(itemIds);
    if (sources.length === 0) {
      return {
        classified: 0,
        skipped: itemIds.length,
        createdCollectionNames: [],
      };
    }
    const { classifyInboxItems } =
      await import("../services/knowledge-ai/classify-collections");
    const assignments = await classifyInboxItems(
      sources,
      existingCollectionNames,
      options,
    );
    const result = await window.api.inbox.applyAiClassification({
      assignments,
    });
    await get().refresh();
    return result;
  },
}));
