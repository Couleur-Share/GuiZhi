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

interface InboxState {
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
    set({ isLoading: true, loadError: null });
    try {
      const [result, wikiPending] = await Promise.all([
        window.api.inbox.list(),
        import("../services/knowledge-ai/wiki-compile").then(
          ({ countPendingWikiItems }) => countPendingWikiItems(),
        ),
      ]);
      const wikiItem: InboxItem | null =
        wikiPending > 0
          ? {
              kind: "wiki-pending",
              id: "aggregate:wiki",
              count: wikiPending,
              createdAt: Date.now(),
            }
          : null;
      const items = [
        ...(wikiItem ? [wikiItem] : []),
        ...result.items.filter((item) => item.kind !== "wiki-pending"),
      ];
      const counts = {
        ...result.counts,
        "wiki-pending": wikiPending > 0 ? 1 : 0,
      };
      const alive = new Set(
        items.flatMap((item) => ("itemId" in item ? [item.itemId] : [])),
      );
      set((state) => ({
        items,
        counts,
        total: Object.values(counts).reduce((sum, count) => sum + count, 0),
        selectionIds: state.selectionIds.filter((id) => alive.has(id)),
      }));
    } catch (error) {
      set({ loadError: describeLoadError(error) });
    } finally {
      set({ isLoading: false });
    }
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
