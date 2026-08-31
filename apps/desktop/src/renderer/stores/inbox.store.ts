import { create } from "zustand";
import type {
  InboxItem,
  InboxItemKind,
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
  clearSelection: () => void;
  refresh: () => Promise<void>;
  organize: (input: Omit<InboxOrganizeInput, "itemIds">) => Promise<number>;
  markReviewed: (itemIds: string[]) => Promise<number>;
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
  clearSelection: () => set({ selectionIds: [] }),
  refresh: async () => {
    set({ isLoading: true, loadError: null });
    try {
      const result = await window.api.inbox.list();
      const alive = new Set(
        result.items.flatMap((item) =>
          "itemId" in item ? [item.itemId] : [],
        ),
      );
      set((state) => ({
        items: result.items,
        counts: result.counts,
        total: result.total,
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
}));
