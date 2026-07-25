import { create } from "zustand";
import type {
  Collection,
  CreateCollectionInput,
  UpdateCollectionInput,
} from "@guizhi/shared/types";

interface CollectionState {
  collections: Collection[];
  isLoading: boolean;
  fetchCollections: () => Promise<void>;
  createCollection: (input: CreateCollectionInput) => Promise<Collection>;
  updateCollection: (
    id: string,
    input: UpdateCollectionInput,
  ) => Promise<Collection | null>;
  deleteCollection: (id: string) => Promise<boolean>;
}

export const useCollectionStore = create<CollectionState>()((set, get) => ({
  collections: [],
  isLoading: false,

  fetchCollections: async () => {
    set({ isLoading: true });
    try {
      const collections = await window.api.collection.list();
      set({ collections });
    } catch (error) {
      console.error("加载集合列表失败:", error);
    } finally {
      set({ isLoading: false });
    }
  },

  createCollection: async (input) => {
    const created = await window.api.collection.create(input);
    await get().fetchCollections();
    return created;
  },

  updateCollection: async (id, input) => {
    const updated = await window.api.collection.update(id, input);
    await get().fetchCollections();
    return updated;
  },

  deleteCollection: async (id) => {
    const removed = await window.api.collection.delete(id);
    await get().fetchCollections();
    return removed;
  },
}));
